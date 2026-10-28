package repository

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/shopspring/decimal"
)

func lockCodingPerson(ctx context.Context, tx *sql.Tx, user int64, purpose string) error {
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fmt.Sprintf("coding-person:%d:%s", user, purpose))
	return err
}

const reservationJSON = `to_jsonb(r)-'api_key_id'||jsonb_build_object('estimated_points',r.estimated_points::text,'rate',r.rate::text)`

func reservationRow(ctx context.Context, tx *sql.Tx, id string) (service.CodingReservation, error) {
	var raw []byte
	var out service.CodingReservation
	err := tx.QueryRowContext(ctx, `SELECT `+reservationJSON+` FROM coding_quota_reservations r WHERE id=$1`, id).Scan(&raw)
	if err == nil {
		err = json.Unmarshal(raw, &out)
	}
	return out, err
}

// ReserveCodingQuota atomically checks current personal usage and all-key reservations.
func (r *UsageMonitorRepository) ReserveCodingQuota(ctx context.Context, in service.CodingReservationInput) (service.CodingReservation, error) {
	var out service.CodingReservation
	limit, e := decimal.NewFromString(in.Limit)
	if e != nil || !limit.IsPositive() || in.ID == "" || in.UserID <= 0 || in.APIKeyID <= 0 || in.AccountID <= 0 || in.StartedAt.IsZero() || (in.Purpose != "openai" && in.Purpose != "grok") || (in.Mode != "shadow" && in.Mode != "enforce") {
		return out, fmt.Errorf("invalid reservation admission")
	}
	if in.Now.IsZero() {
		in.Now = time.Now().UTC()
	}
	if in.MinSamples <= 0 {
		in.MinSamples = 20
	}
	if in.MaxAge <= 0 {
		in.MaxAge = 20 * time.Minute
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback() }()
	// Admission never acquires a parent lock after the person lock. Meter reads are MVCC snapshots.
	if err = lockCodingPerson(ctx, tx, in.UserID, in.Purpose); err != nil {
		return out, err
	}
	var oldUser, oldKey int64
	var oldPurpose, oldState string
	err = tx.QueryRowContext(ctx, `SELECT user_id,api_key_id,purpose,state FROM coding_quota_reservations WHERE id=$1`, in.ID).Scan(&oldUser, &oldKey, &oldPurpose, &oldState)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	if err == nil && (oldUser != in.UserID || oldKey != in.APIKeyID || oldPurpose != in.Purpose) {
		return out, fmt.Errorf("reservation identity conflict")
	}
	if err == nil && oldState != "reserved" && oldState != "sent" {
		return out, fmt.Errorf("reservation is already %s", oldState)
	}
	if err = recoverCodingPerson(ctx, tx, in.Now, in.UserID, in.Purpose); err != nil {
		return out, err
	}
	readings, err := readCodingQuotas(ctx, tx, []int64{in.UserID}, in.Now)
	if err != nil {
		return out, err
	}
	var reading *service.CodingQuotaReading
	for i := range readings {
		if readings[i].Purpose == in.Purpose {
			reading = &readings[i]
			break
		}
	}
	if reading == nil {
		return out, service.ErrCodingQuotaUnavailable
	}
	used, err := decimal.NewFromString(reading.Used)
	if err != nil {
		return out, err
	}
	pending, err := decimal.NewFromString(reading.Pending)
	if err != nil {
		return out, err
	}
	var reserved string
	var active, unknown int
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(estimated_points) FILTER(WHERE started_at>=$3 AND started_at<$4),0)::text,
 COUNT(*) FILTER(WHERE state IN('reserved','sent','handoff')),
 COUNT(*) FILTER(WHERE state='unresolved' AND started_at>=$3 AND started_at<$4)
 FROM coding_quota_reservations WHERE user_id=$1 AND purpose=$2 AND id<>$5 AND state IN('reserved','sent','handoff','unresolved')`, in.UserID, in.Purpose, reading.Start, reading.End, in.ID).Scan(&reserved, &active, &unknown)
	if err != nil {
		return out, err
	}
	var rate sql.NullString
	var observed, end sql.NullTime
	var samples int
	err = tx.QueryRowContext(ctx, `SELECT m.points_per_weight::text,m.observed_at,m.window_end,
 (SELECT count(*) FROM coding_quota_requests q WHERE q.account_id=m.account_id AND q.state='settled' AND q.official_window_end BETWEEN m.window_end-INTERVAL '5 minutes' AND m.window_end+INTERVAL '5 minutes')
 FROM coding_quota_meters m WHERE m.account_id=$1 AND m.purpose=$2`, in.AccountID, in.Purpose).Scan(&rate, &observed, &end, &samples)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	weight, we := decimal.NewFromString(in.Weight)
	multiplier, re := decimal.NewFromString(rate.String)
	reliable := we == nil && weight.IsPositive() && re == nil && multiplier.IsPositive() && samples >= in.MinSamples && observed.Valid && !observed.Time.After(in.Now.Add(time.Minute)) && in.Now.Sub(observed.Time) <= in.MaxAge && end.Valid && end.Time.After(in.Now) && unknown == 0
	var estimate any
	reason := "uncalibrated"
	mode := "single"
	allowed := active == 0
	if we == nil && weight.IsPositive() && re == nil && multiplier.IsPositive() && end.Valid && end.Time.After(in.Now) {
		estimate = weight.Mul(multiplier).Round(8).StringFixed(8)
	}
	if reliable {
		reserve, _ := decimal.NewFromString(reserved)
		amount, _ := decimal.NewFromString(estimate.(string))
		if used.Add(pending).Add(reserve).Add(amount).LessThanOrEqual(limit) {
			mode = "normal"
			reason = "within_budget"
			allowed = true
		} else {
			reason = "estimated_capacity"
		}
	}
	if unknown > 0 {
		reason = "unresolved_request"
	}
	if used.GreaterThanOrEqual(limit) {
		mode = "exhausted"
		reason = "settled_exhausted"
		allowed = false
	}
	if !allowed && in.Mode == "enforce" {
		if mode == "exhausted" {
			return out, service.ErrCodingQuotaExhausted
		}
		return out, service.ErrCodingQuotaBusy
	}
	var observedArg, endArg any
	if observed.Valid {
		observedArg = observed.Time
	}
	if end.Valid {
		endArg = end.Time
	}
	var rateArg any
	if re == nil {
		rateArg = multiplier.String()
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO coding_quota_reservations(id,user_id,api_key_id,account_id,purpose,model,mode,admission_mode,reason,estimated_points,rate,sample_count,reliable,would_allow,started_at,observed_at,window_end,heartbeat_at,expires_at,updated_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$18)
 ON CONFLICT(id) DO UPDATE SET account_id=EXCLUDED.account_id,model=EXCLUDED.model,estimated_points=EXCLUDED.estimated_points,
 rate=EXCLUDED.rate,sample_count=EXCLUDED.sample_count,reliable=EXCLUDED.reliable,admission_mode=EXCLUDED.admission_mode,reason=EXCLUDED.reason,
 would_allow=EXCLUDED.would_allow,observed_at=EXCLUDED.observed_at,window_end=EXCLUDED.window_end,heartbeat_at=EXCLUDED.heartbeat_at,expires_at=EXCLUDED.expires_at,updated_at=EXCLUDED.updated_at
 WHERE coding_quota_reservations.state IN('reserved','sent')`, in.ID, in.UserID, in.APIKeyID, in.AccountID, in.Purpose, in.Model, in.Mode, mode, reason, estimate, rateArg, samples, reliable, allowed, in.StartedAt, observedArg, endArg, in.Now, in.Now.Add(120*time.Second))
	if err != nil {
		return out, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO coding_quota_reservation_events(reservation_id,state,reason,created_at) VALUES($1,'reserved',$2,$3)`, in.ID, reason, in.Now)
	if err != nil {
		return out, err
	}
	out, err = reservationRow(ctx, tx, in.ID)
	if err != nil {
		return out, err
	}
	return out, tx.Commit()
}

func recoverCodingPerson(ctx context.Context, tx *sql.Tx, now time.Time, user int64, purpose string) error {
	_, err := tx.ExecContext(ctx, `WITH changed AS(UPDATE coding_quota_reservations SET state=CASE WHEN state='reserved' THEN 'released' ELSE 'unresolved' END,
 reason='lease_expired',updated_at=$1 WHERE user_id=$2 AND purpose=$3 AND state IN('reserved','sent') AND expires_at<=$1 RETURNING id,state)
 INSERT INTO coding_quota_reservation_events(reservation_id,state,reason,created_at) SELECT id,state,'lease_expired',$1 FROM changed`, now, user, purpose)
	return err
}

func (r *UsageMonitorRepository) transitionReservation(ctx context.Context, id, target string, now time.Time) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var user int64
	var purpose string
	if err = tx.QueryRowContext(ctx, `SELECT user_id,purpose FROM coding_quota_reservations WHERE id=$1`, id).Scan(&user, &purpose); err != nil {
		return err
	}
	if err = lockCodingPerson(ctx, tx, user, purpose); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `WITH changed AS(UPDATE coding_quota_reservations SET state=$2,heartbeat_at=$3,expires_at=$3::timestamptz+INTERVAL '120 seconds',updated_at=$3
 WHERE id=$1 AND ((state IN('reserved','sent') AND $2 IN('sent','released','unresolved')) OR (state='unresolved' AND $2='released')) AND state<>$2 RETURNING id,state)
 INSERT INTO coding_quota_reservation_events(reservation_id,state,reason,created_at) SELECT id,state,'request_lifecycle',$3 FROM changed`, id, target, now)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (r *UsageMonitorRepository) MarkCodingReservationSent(ctx context.Context, id string, now time.Time) error {
	return r.transitionReservation(ctx, id, "sent", now)
}
func (r *UsageMonitorRepository) FinishCodingReservation(ctx context.Context, id string, noUsage bool, now time.Time) error {
	state := "unresolved"
	if noUsage {
		state = "released"
	}
	return r.transitionReservation(ctx, id, state, now)
}
func (r *UsageMonitorRepository) RenewCodingReservation(ctx context.Context, id string, now time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE coding_quota_reservations SET heartbeat_at=$2,expires_at=$2::timestamptz+INTERVAL '120 seconds' WHERE id=$1 AND state IN('reserved','sent')`, id, now)
	return err
}
func (r *UsageMonitorRepository) RecoverCodingReservations(ctx context.Context, now time.Time) error {
	rows, err := r.db.QueryContext(ctx, `SELECT DISTINCT user_id,purpose FROM coding_quota_reservations WHERE state IN('reserved','sent') AND expires_at<=$1 LIMIT 512`, now)
	if err != nil {
		return err
	}
	type person struct {
		user    int64
		purpose string
	}
	var people []person
	for rows.Next() {
		var p person
		if err = rows.Scan(&p.user, &p.purpose); err != nil {
			_ = rows.Close()
			return err
		}
		people = append(people, p)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	for _, p := range people {
		tx, e := r.db.BeginTx(ctx, nil)
		if e != nil {
			return e
		}
		e = lockCodingPerson(ctx, tx, p.user, p.purpose)
		if e == nil {
			e = recoverCodingPerson(ctx, tx, now, p.user, p.purpose)
		}
		if e == nil {
			e = tx.Commit()
		}
		_ = tx.Rollback()
		if e != nil {
			return e
		}
	}
	return nil
}

// ListCodingReservations reads bounded diagnostics with a stable timestamp+ID cursor.
func (r *UsageMonitorRepository) ListCodingReservations(ctx context.Context, q service.CodingReservationQuery) (service.CodingReservationPage, error) {
	out := service.CodingReservationPage{Items: []service.CodingReservation{}}
	if q.Limit <= 0 {
		q.Limit = 50
	}
	if q.Limit > 100 {
		q.Limit = 100
	}
	if q.Purpose != "" && q.Purpose != "openai" && q.Purpose != "grok" {
		return out, fmt.Errorf("invalid purpose")
	}
	if q.State != "" && !strings.Contains("|reserved|sent|handoff|captured|released|unresolved|", "|"+q.State+"|") {
		return out, fmt.Errorf("invalid state")
	}
	at := time.Now().Add(100 * 365 * 24 * time.Hour)
	id := "~"
	if q.Before != "" {
		b, e := base64.RawURLEncoding.DecodeString(q.Before)
		if e != nil {
			return out, fmt.Errorf("invalid cursor")
		}
		parts := strings.SplitN(string(b), "|", 2)
		if len(parts) != 2 {
			return out, fmt.Errorf("invalid cursor")
		}
		at, e = time.Parse(time.RFC3339Nano, parts[0])
		if e != nil {
			return out, fmt.Errorf("invalid cursor")
		}
		id = parts[1]
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+reservationJSON+` FROM coding_quota_reservations r WHERE ($1::bigint=0 OR user_id=$1) AND ($2='' OR purpose=$2) AND ($3='' OR state=$3)
 AND (started_at,id)<($4,$5) ORDER BY started_at DESC,id DESC LIMIT $6`, q.UserID, q.Purpose, q.State, at, id, q.Limit+1)
	if err != nil {
		return out, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var raw []byte
		var v service.CodingReservation
		if err = rows.Scan(&raw); err != nil {
			return out, err
		}
		if err = json.Unmarshal(raw, &v); err != nil {
			return out, err
		}
		out.Items = append(out.Items, v)
	}
	if len(out.Items) > q.Limit {
		out.Items = out.Items[:q.Limit]
		last := out.Items[q.Limit-1]
		out.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(last.StartedAt.UTC().Format(time.RFC3339Nano) + "|" + last.ID))
	}
	return out, rows.Err()
}
