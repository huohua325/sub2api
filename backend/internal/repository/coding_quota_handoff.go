package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/lib/pq"
	"sort"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// EnqueueCodingBilling stores the exact command before any asynchronous execution.
func (r *UsageMonitorRepository) EnqueueCodingBilling(ctx context.Context, id string, cmd *service.UsageBillingCommand) error {
	if cmd == nil || cmd.RequestID == "" {
		return fmt.Errorf("coding billing command required")
	}
	cmd.CodingReservationID = id
	cmd.Normalize()
	raw, err := json.Marshal(cmd)
	if err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var purpose string
	if err = tx.QueryRowContext(ctx, `SELECT purpose FROM coding_quota_reservations WHERE id=$1`, id).Scan(&purpose); err != nil {
		return err
	}
	if cmd.CodingQuota != nil && cmd.CodingQuota.Purpose != purpose {
		return fmt.Errorf("coding purpose mismatch")
	}
	if err = lockCodingPerson(ctx, tx, cmd.UserID, purpose); err != nil {
		return err
	}
	var matches bool
	err = tx.QueryRowContext(ctx, `SELECT user_id=$2 AND api_key_id=$3 AND purpose=$4 FROM coding_quota_reservations WHERE id=$1 FOR UPDATE`, id, cmd.UserID, cmd.APIKeyID, purpose).Scan(&matches)
	if err != nil {
		return err
	}
	if !matches {
		return fmt.Errorf("coding handoff identity mismatch")
	}
	ids := codingBillingReservationIDs(cmd)
	for _, holdID := range ids {
		if err := tx.QueryRowContext(ctx, `SELECT user_id=$2 AND api_key_id=$3 AND purpose=$4 FROM coding_quota_reservations WHERE id=$1 FOR UPDATE`, holdID, cmd.UserID, cmd.APIKeyID, purpose).Scan(&matches); err != nil {
			return err
		}
		if !matches {
			return fmt.Errorf("coding companion identity mismatch")
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO coding_quota_billing_tasks(reservation_id,request_id,api_key_id,fingerprint,command) VALUES($1,$2,$3,$4,$5::jsonb)
 ON CONFLICT(request_id,api_key_id) DO NOTHING`, id, cmd.RequestID, cmd.APIKeyID, cmd.RequestFingerprint, string(raw))
	if err != nil {
		return err
	}
	err = tx.QueryRowContext(ctx, `SELECT fingerprint=$3 AND command=$4::jsonb FROM coding_quota_billing_tasks WHERE request_id=$1 AND api_key_id=$2`, cmd.RequestID, cmd.APIKeyID, cmd.RequestFingerprint, string(raw)).Scan(&matches)
	if err != nil {
		return err
	}
	if !matches {
		return service.ErrUsageBillingRequestConflict
	}
	_, err = tx.ExecContext(ctx, `WITH changed AS(UPDATE coding_quota_reservations SET state='handoff',reason='billing_persisted',updated_at=NOW()
 WHERE id=ANY($1) AND state IN('reserved','sent','unresolved') RETURNING id,state)
 INSERT INTO coding_quota_reservation_events(reservation_id,state,reason) SELECT id,state,'billing_persisted' FROM changed`, pq.Array(ids))
	if err != nil {
		return err
	}
	return tx.Commit()
}

// completeCodingReservationTx atomically moves occupancy into the existing ledger.
func completeCodingReservationTx(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand) error {
	if cmd.CodingReservationID == "" {
		return nil
	}
	var purpose string
	if err := tx.QueryRowContext(ctx, `SELECT purpose FROM coding_quota_reservations WHERE id=$1`, cmd.CodingReservationID).Scan(&purpose); err != nil {
		return err
	}
	parentID := cmd.AccountID
	if cmd.CodingQuota != nil {
		parentID = cmd.CodingQuota.AccountID
		if cmd.CodingQuota.Purpose != purpose {
			return fmt.Errorf("coding purpose mismatch")
		}
	}
	if err := lockCodingMeter(ctx, tx, parentID); err != nil {
		return err
	}
	if err := lockCodingPerson(ctx, tx, cmd.UserID, purpose); err != nil {
		return err
	}
	var matches bool
	err := tx.QueryRowContext(ctx, `SELECT r.user_id=$2 AND r.api_key_id=$3 AND r.purpose=$4
 AND EXISTS(SELECT 1 FROM coding_quota_billing_tasks t WHERE t.reservation_id=r.id AND t.request_id=$5 AND t.api_key_id=$3 AND t.fingerprint=$6)
 FROM coding_quota_reservations r WHERE r.id=$1 FOR UPDATE`, cmd.CodingReservationID, cmd.UserID, cmd.APIKeyID, purpose, cmd.RequestID, cmd.RequestFingerprint).Scan(&matches)
	if err != nil {
		return err
	}
	if !matches {
		return fmt.Errorf("capture has no matching durable handoff")
	}
	_, err = tx.ExecContext(ctx, `UPDATE coding_quota_billing_tasks SET state='applied',last_error='',applied_at=NOW() WHERE request_id=$1 AND api_key_id=$2`, cmd.RequestID, cmd.APIKeyID)
	if err != nil {
		return err
	}
	state, reason := "captured", "ledger_recorded"
	if cmd.CodingQuota == nil {
		state, reason = "unresolved", "unmetered_result"
	}
	_, err = tx.ExecContext(ctx, `WITH changed AS(UPDATE coding_quota_reservations SET state=$2,reason=$3,updated_at=NOW()
 WHERE id=ANY($1) AND state<>'captured' AND NOT EXISTS(SELECT 1 FROM coding_quota_billing_tasks WHERE reservation_id=coding_quota_reservations.id AND state<>'applied') RETURNING id,state)
 INSERT INTO coding_quota_reservation_events(reservation_id,state,reason) SELECT id,state,$3 FROM changed`, pq.Array(codingBillingReservationIDs(cmd)), state, reason)
	return err
}

// ReplayCodingBilling claims bounded work without holding row locks during billing.
func (r *UsageMonitorRepository) ReplayCodingBilling(ctx context.Context, now time.Time, limit int) (int, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx, `WITH candidates AS(SELECT id FROM coding_quota_billing_tasks WHERE state='pending' AND next_retry_at<=$1 ORDER BY next_retry_at,id LIMIT $2 FOR UPDATE SKIP LOCKED)
 UPDATE coding_quota_billing_tasks t SET next_retry_at=$1::timestamptz+INTERVAL '120 seconds',attempts=attempts+1 FROM candidates c WHERE t.id=c.id RETURNING t.id,t.command`, now, limit)
	if err != nil {
		return 0, err
	}
	type task struct {
		id  int64
		cmd service.UsageBillingCommand
	}
	var tasks []task
	for rows.Next() {
		var t task
		var raw []byte
		if err = rows.Scan(&t.id, &raw); err != nil {
			_ = rows.Close()
			return 0, err
		}
		if err = json.Unmarshal(raw, &t.cmd); err != nil {
			_ = rows.Close()
			return 0, err
		}
		tasks = append(tasks, t)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return 0, err
	}
	completed := 0
	repo := &usageBillingRepository{db: r.db}
	for _, t := range tasks {
		_, e := repo.Apply(ctx, &t.cmd)
		if e == nil {
			completed++
			continue
		}
		state := "pending"
		if errors.Is(e, service.ErrUsageBillingRequestConflict) {
			state = "failed"
		}
		message := e.Error()
		if len(message) > 1000 {
			message = message[:1000]
		}
		tx, e := r.db.BeginTx(ctx, nil)
		if e != nil {
			return completed, e
		}
		var purpose string
		e = tx.QueryRowContext(ctx, `SELECT purpose FROM coding_quota_reservations WHERE id=$1`, t.cmd.CodingReservationID).Scan(&purpose)
		if e == nil {
			e = lockCodingPerson(ctx, tx, t.cmd.UserID, purpose)
		}
		if e == nil {
			_, e = tx.ExecContext(ctx, `UPDATE coding_quota_billing_tasks SET state=$2,last_error=$3,next_retry_at=$4::timestamptz+INTERVAL '60 seconds' WHERE id=$1 AND state='pending'`, t.id, state, message, now)
		}
		if e == nil && state == "failed" {
			_, e = tx.ExecContext(ctx, `WITH changed AS(UPDATE coding_quota_reservations SET state='unresolved',reason='billing_conflict',updated_at=NOW() WHERE id=ANY($1) AND state='handoff' RETURNING id,state) INSERT INTO coding_quota_reservation_events(reservation_id,state,reason) SELECT id,state,'billing_conflict' FROM changed`, pq.Array(codingBillingReservationIDs(&t.cmd)))
		}
		if e != nil {
			_ = tx.Rollback()
			return completed, e
		}
		if e = tx.Commit(); e != nil {
			return completed, e
		}
	}
	return completed, nil
}

func codingBillingReservationIDs(cmd *service.UsageBillingCommand) []string {
	seen := map[string]bool{cmd.CodingReservationID: true}
	for _, id := range cmd.CodingReservationIDs {
		if id != "" {
			seen[id] = true
		}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
