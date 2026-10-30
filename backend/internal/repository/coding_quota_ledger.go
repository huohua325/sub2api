package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/shopspring/decimal"
)

var _ service.CodingQuotaLedger = (*UsageMonitorRepository)(nil)

// ReadCodingQuotas only scans the requested people's current indexed periods.
// Deleted/unavailable parents never remove an already recorded charge.
func (r *UsageMonitorRepository) ReadCodingQuotas(ctx context.Context, users []int64, now time.Time) ([]service.CodingQuotaReading, error) {
	return readCodingQuotas(ctx, r.db, users, now)
}

type codingQuotaQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func readCodingQuotas(ctx context.Context, db codingQuotaQueryer, users []int64, now time.Time) ([]service.CodingQuotaReading, error) {
	rows, err := db.QueryContext(ctx, `
WITH anchors AS (
 SELECT u.id,p.purpose,GREATEST(COALESCE(a.window_start,u.created_at),
   CASE WHEN p.purpose='openai' THEN COALESCE((SELECT MAX(effective_at) FROM coding_quota_reset_events
     WHERE status='confirmed' AND effective_at<=$2),'-infinity'::timestamptz) ELSE '-infinity'::timestamptz END) AS anchor,
   CASE WHEN p.purpose='openai' AND EXISTS(SELECT 1 FROM coding_quota_reset_events
     WHERE status='confirmed' AND effective_at>COALESCE(a.window_start,u.created_at) AND effective_at<=$2)
     THEN 'platform_reset' ELSE 'personal_week' END AS reason
 FROM users u CROSS JOIN(VALUES('openai'),('grok')) p(purpose)
 LEFT JOIN user_purpose_allowances a ON a.user_id=u.id AND a.purpose=p.purpose
 WHERE u.id=ANY($1::bigint[]) AND u.deleted_at IS NULL
), windows AS (
 SELECT *,anchor+FLOOR(GREATEST(EXTRACT(EPOCH FROM($2-anchor)),0)/604800)*INTERVAL '168 hours' AS start FROM anchors
)
SELECT w.id,w.purpose,w.start,w.start+INTERVAL '168 hours',w.reason,
 COALESCE(q.used,0)::text,COALESCE(q.pending,0)::text,COALESCE(q.pending_count,0),
 COALESCE(q.unmetered_count,0),COALESCE(q.imported,FALSE),q.updated_at
FROM windows w LEFT JOIN LATERAL(
 SELECT SUM(q.points+COALESCE(c.delta,0)) AS used,SUM(q.estimated_points) FILTER(WHERE q.state IN('pending','unmetered')) AS pending,
 COUNT(*) FILTER(WHERE q.state='pending') AS pending_count,COUNT(*) FILTER(WHERE q.state='unmetered') AS unmetered_count,
 BOOL_OR(q.state='imported') AS imported,MAX(GREATEST(q.updated_at,c.created_at)) AS updated_at
 FROM coding_quota_requests q LEFT JOIN coding_quota_corrections c ON c.request_row_id=q.id
 WHERE q.user_id=w.id AND q.purpose=w.purpose
 AND q.started_at>=w.start AND q.started_at<w.start+INTERVAL '168 hours'
) q ON TRUE ORDER BY w.id,w.purpose`, pq.Array(users), now.UTC())
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]service.CodingQuotaReading, 0, len(users)*2)
	for rows.Next() {
		var v service.CodingQuotaReading
		var updatedAt sql.NullTime
		if err := rows.Scan(&v.UserID, &v.Purpose, &v.Start, &v.End, &v.ResetReason, &v.Used, &v.Pending,
			&v.PendingCount, &v.UnmeteredCount, &v.Imported, &updatedAt); err != nil {
			return nil, err
		}
		if updatedAt.Valid {
			v.UpdatedAt = updatedAt.Time
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

type codingQuotaExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func insertCodingObservation(ctx context.Context, db codingQuotaExecer, v service.OfficialQuotaObservation) error {
	if v.AccountID <= 0 || v.WindowEnd.IsZero() || v.ObservedAt.IsZero() {
		return nil
	}
	if v.Purpose != service.PurposeOpenAI && v.Purpose != service.PurposeGrok {
		return nil
	}
	p, err := decimal.NewFromString(v.Used)
	if err != nil || p.IsNegative() || p.GreaterThan(decimal.NewFromInt(100)) {
		return fmt.Errorf("invalid official observation")
	}
	_, err = db.ExecContext(ctx, `INSERT INTO coding_quota_observations(account_id,purpose,window_end,observed_at,used,source)
 VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, v.AccountID, v.Purpose, v.WindowEnd.UTC(), v.ObservedAt.UTC(), p.String(), v.Source)
	return err
}

func (r *UsageMonitorRepository) ObserveCodingQuota(ctx context.Context, v service.OfficialQuotaObservation) error {
	return insertCodingObservation(ctx, r.db, v)
}

// recordCodingQuotaCharge runs under the existing billing idempotency claim.
// Durable requests are independent of the best-effort usage log writer.
func recordCodingQuotaCharge(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand) error {
	v := cmd.CodingQuota
	if v == nil || v.AccountID <= 0 {
		return nil
	}
	weight, err := decimal.NewFromString(v.Weight)
	if err != nil || weight.IsNegative() {
		return fmt.Errorf("invalid coding quota weight")
	}
	// Use the same lock order as reconciliation, before inserting observations.
	// A charge either enters before repricing or reads the new rate afterwards.
	if err := lockCodingMeter(ctx, tx, v.AccountID); err != nil {
		return err
	}
	if v.Baseline != nil {
		if err := insertCodingObservation(ctx, tx, *v.Baseline); err != nil {
			return err
		}
	}
	if v.Observation != nil {
		if err := insertCodingObservation(ctx, tx, *v.Observation); err != nil {
			return err
		}
	}
	var window any
	if v.Observation != nil {
		window = v.Observation.WindowEnd.UTC()
	} else if v.Baseline != nil {
		window = v.Baseline.WindowEnd.UTC()
	}
	purpose := v.Purpose
	if purpose == "" {
		purpose = "other"
	}
	state := "pending"
	if weight.IsZero() {
		state = "unmetered"
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO coding_quota_requests(request_id,api_key_id,user_id,purpose,account_id,
 started_at,completed_at,official_window_end,weight,estimated_points,state)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,COALESCE((SELECT CASE WHEN $6<calibration_started_at THEN
 (SELECT previous_rate FROM coding_quota_capacity_events e WHERE e.account_id=$5 AND e.purpose=$4 AND e.effective_at>$6
 AND e.window_end BETWEEN m.window_end-INTERVAL '5 minutes' AND m.window_end+INTERVAL '5 minutes' ORDER BY e.effective_at LIMIT 1)
 ELSE points_per_weight END FROM coding_quota_meters m WHERE account_id=$5
 AND ($8::timestamptz IS NULL OR ABS(EXTRACT(EPOCH FROM(window_end-$8::timestamptz)))<=300)),0)*$9,
 CASE WHEN EXISTS(SELECT 1 FROM coding_quota_meters WHERE account_id=$5 AND $6<calibration_started_at) THEN 'unmetered' ELSE $10 END)
 ON CONFLICT(request_id,api_key_id) DO NOTHING`, cmd.RequestID, cmd.APIKeyID, cmd.UserID, purpose, v.AccountID,
		v.StartedAt.UTC(), v.CompletedAt.UTC(), window, weight.String(), state)
	return err
}

type codingMeter struct {
	AccountID  int64
	WindowEnd  time.Time
	ObservedAt time.Time
	Used       decimal.Decimal
}

func lockCodingMeter(ctx context.Context, tx *sql.Tx, accountID int64) error {
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fmt.Sprintf("coding-meter:%d", accountID))
	return err
}

// ReconcileCodingQuota waits for the billing queue to catch up, then closes
// disjoint increments. PostgreSQL locks serialize workers across instances.
func (r *UsageMonitorRepository) ReconcileCodingQuota(ctx context.Context, cutoff time.Time) error {
	rows, err := r.db.QueryContext(ctx, `SELECT account_id,purpose,window_end,observed_at,used::text,source
 FROM coding_quota_observations WHERE NOT processed AND observed_at<=$1 ORDER BY observed_at,id LIMIT 512`, cutoff.UTC())
	if err != nil {
		return err
	}
	var observations []service.OfficialQuotaObservation
	for rows.Next() {
		var v service.OfficialQuotaObservation
		if err := rows.Scan(&v.AccountID, &v.Purpose, &v.WindowEnd, &v.ObservedAt, &v.Used, &v.Source); err != nil {
			_ = rows.Close()
			return err
		}
		observations = append(observations, v)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	for _, v := range observations {
		if err := r.reconcileCodingObservation(ctx, v); err != nil {
			return err
		}
	}
	return r.detectCodingReset(ctx, cutoff)
}

func (r *UsageMonitorRepository) reconcileCodingObservation(ctx context.Context, v service.OfficialQuotaObservation) (err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) && err == nil {
			err = rollbackErr
		}
	}()
	if err = lockCodingMeter(ctx, tx, v.AccountID); err != nil {
		return err
	}
	var processed bool
	if err = tx.QueryRowContext(ctx, `SELECT processed FROM coding_quota_observations WHERE account_id=$1 AND window_end=$2 AND observed_at=$3 AND used=$4 FOR UPDATE`, v.AccountID, v.WindowEnd, v.ObservedAt, v.Used).Scan(&processed); err != nil {
		return err
	}
	if processed {
		return tx.Commit()
	}
	var m codingMeter
	var used string
	err = tx.QueryRowContext(ctx, `SELECT account_id,window_end,observed_at,used::text FROM coding_quota_meters WHERE account_id=$1 FOR UPDATE`, v.AccountID).Scan(&m.AccountID, &m.WindowEnd, &m.ObservedAt, &used)
	p, parseErr := decimal.NewFromString(v.Used)
	if parseErr != nil {
		return parseErr
	}
	switch {
	case errors.Is(err, sql.ErrNoRows):
		_, err = tx.ExecContext(ctx, `INSERT INTO coding_quota_meters(account_id,purpose,window_end,observed_at,used) VALUES($1,$2,$3,$4,$5)`, v.AccountID, v.Purpose, v.WindowEnd, v.ObservedAt, v.Used)
		if err != nil {
			return err
		}
		// No trustworthy pre-request baseline: retain an explicit unresolved entry.
		_, err = tx.ExecContext(ctx, `UPDATE coding_quota_requests SET state='unmetered',updated_at=NOW() WHERE account_id=$1 AND state='pending' AND completed_at<=$2`, v.AccountID, v.ObservedAt)
	case err != nil:
		return err
	case !v.ObservedAt.After(m.ObservedAt): // stale/replayed snapshots cannot move the cursor backwards.
		err = nil
	default:
		m.Used, err = decimal.NewFromString(used)
		if err != nil {
			return err
		}
		moved := v.WindowEnd.Sub(m.WindowEnd)
		if moved < 0 {
			moved = -moved
		}
		switch {
		case moved > 5*time.Minute && v.Purpose == service.PurposeOpenAI && v.ObservedAt.Before(m.WindowEnd) &&
			(v.WindowEnd.Add(-7*24*time.Hour).Before(m.ObservedAt.Add(-5*time.Minute)) ||
				v.WindowEnd.Add(-7*24*time.Hour).After(v.ObservedAt.Add(time.Minute))):
			// A new weekly cycle must have started since our last trustworthy
			// counter. A drifting reset-after header cannot erase the high-water
			// mark of an ongoing week, even if the drift persists or later returns.
			err = nil
		case moved <= 5*time.Minute && p.LessThan(m.Used):
			// Concurrent upstream replicas can report a lower cumulative value
			// with a newer HTTP Date. Preserve the high-water mark and interval
			// cursor: recovering this value must never charge the same points
			// twice or create platform-reset evidence.
			err = nil
		case moved > 5*time.Minute:
			// Changing a parent's meter does not reset any personal ledger.
			abnormal := v.Purpose == service.PurposeOpenAI && v.ObservedAt.Before(m.WindowEnd.Add(-5*time.Minute)) && p.LessThan(m.Used) && v.Source != "codex_local_reset"
			if abnormal {
				var local sql.NullString
				lookupErr := tx.QueryRowContext(ctx, `SELECT extra->>'codex_local_reset_at' FROM accounts WHERE id=$1`, v.AccountID).Scan(&local)
				if lookupErr != nil && !errors.Is(lookupErr, sql.ErrNoRows) {
					return lookupErr
				}
				if local.Valid {
					if at, e := time.Parse(time.RFC3339, local.String); e == nil && !v.ObservedAt.Before(at.Add(-time.Minute)) && v.ObservedAt.Before(at.Add(10*time.Minute)) {
						abnormal = false
					}
				}
			}
			evidence, _ := json.Marshal(map[string]any{"account_id": v.AccountID, "before": m.Used.String(), "after": p.String(), "previous_reset": m.WindowEnd, "new_reset": v.WindowEnd, "observed_at": v.ObservedAt, "source": v.Source})
			_, err = tx.ExecContext(ctx, `UPDATE coding_quota_meters SET window_end=$2,observed_at=$3,used=$4,points_per_weight=NULL,calibration_started_at=NULL,
 anomaly_at=CASE WHEN $5 THEN $3 ELSE anomaly_at END,anomaly_evidence=CASE WHEN $5 THEN $6::jsonb ELSE anomaly_evidence END,updated_at=NOW() WHERE account_id=$1`, v.AccountID, v.WindowEnd, v.ObservedAt, v.Used, abnormal, string(evidence))
			if err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `UPDATE coding_quota_requests SET state='unmetered',updated_at=NOW() WHERE account_id=$1 AND state='pending' AND completed_at<=$2`, v.AccountID, v.ObservedAt)
		case p.Equal(m.Used):
			// Keep the original baseline until a visible positive increment arrives.
			// A flat read can still repair a legacy estimate; recovery must not
			// depend on an exhausted user being able to make another paid call.
			err = refreshCodingCalibration(ctx, tx, v.AccountID, m.WindowEnd)
		default:
			err = settleCodingIncrement(ctx, tx, v, m, p.Sub(m.Used))
		}
	}
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE coding_quota_observations SET processed=TRUE WHERE account_id=$1 AND window_end=$2 AND observed_at=$3 AND used=$4`, v.AccountID, v.WindowEnd, v.ObservedAt, v.Used); err != nil {
		return err
	}
	return tx.Commit()
}

func settleCodingIncrement(ctx context.Context, tx *sql.Tx, v service.OfficialQuotaObservation, m codingMeter, delta decimal.Decimal) error {
	rows, err := tx.QueryContext(ctx, `SELECT id,weight::text FROM coding_quota_requests WHERE account_id=$1 AND state='pending'
 AND completed_at>$2 AND completed_at<=$3 AND (official_window_end IS NULL OR ABS(EXTRACT(EPOCH FROM(official_window_end-$4::timestamptz)))<=300)
 AND started_at>=COALESCE((SELECT calibration_started_at FROM coding_quota_meters WHERE account_id=$1),'-infinity'::timestamptz)
 ORDER BY id FOR UPDATE`, v.AccountID, m.ObservedAt, v.ObservedAt, v.WindowEnd)
	if err != nil {
		return err
	}
	var requests []service.PointAllocation
	for rows.Next() {
		var req service.PointAllocation
		if err := rows.Scan(&req.ID, &req.Weight); err != nil {
			_ = rows.Close()
			return err
		}
		requests = append(requests, req)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	allocations, err := service.AllocateOfficialIncrement(delta.String(), requests)
	if err != nil {
		return err
	}
	for _, a := range allocations {
		if _, err = tx.ExecContext(ctx, `UPDATE coding_quota_requests SET points=$2,estimated_points=0,state='settled',official_window_end=$3,updated_at=NOW() WHERE id=$1`, a.ID, a.Points, v.WindowEnd); err != nil {
			return err
		}
	}
	// Empty increments are recorded as unallocated evidence, not charged to the
	// next gateway caller. This includes known probe and outside-gateway usage.
	_, err = tx.ExecContext(ctx, `UPDATE coding_quota_meters SET window_end=$2,observed_at=$3,used=$4,
 updated_at=NOW() WHERE account_id=$1`, v.AccountID, v.WindowEnd, v.ObservedAt, v.Used)
	if err != nil {
		return err
	}
	// Requests that arrived after an already closed observation remain visible
	// as unresolved; they must not dilute charges already assigned to others.
	if _, err = tx.ExecContext(ctx, `UPDATE coding_quota_requests SET state='unmetered',updated_at=NOW() WHERE account_id=$1 AND state='pending' AND completed_at<=$2`, v.AccountID, v.ObservedAt); err != nil {
		return err
	}
	return refreshCodingCalibration(ctx, tx, v.AccountID, v.WindowEnd)
}

func refreshCodingCalibration(ctx context.Context, tx *sql.Tx, accountID int64, windowEnd time.Time) error {
	// One rounded official tick may contain consumption from several in-flight
	// streams. Avoid extrapolating only the most recent completed requests.
	// Pool all settled weights in this official window and refresh outstanding
	// estimates together, so a transient spike cannot stay frozen on one key.
	_, err := tx.ExecContext(ctx, `UPDATE coding_quota_meters m SET points_per_weight=s.rate,updated_at=NOW()
 FROM (SELECT ROUND(SUM(q.points+COALESCE(c.delta,0))/NULLIF(SUM(q.weight),0),12) AS rate
 FROM coding_quota_requests q LEFT JOIN coding_quota_corrections c ON c.request_row_id=q.id
 WHERE q.account_id=$1 AND q.state='settled'
 AND q.started_at>=COALESCE((SELECT calibration_started_at FROM coding_quota_meters WHERE account_id=$1),'-infinity'::timestamptz)
 AND q.official_window_end BETWEEN $2::timestamptz-INTERVAL '5 minutes' AND $2::timestamptz+INTERVAL '5 minutes') s
 WHERE m.account_id=$1 AND m.points_per_weight IS DISTINCT FROM s.rate`, accountID, windowEnd)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE coding_quota_requests q SET
 estimated_points=q.weight*COALESCE(m.points_per_weight,0),updated_at=NOW()
 FROM coding_quota_meters m WHERE m.account_id=$1 AND q.account_id=m.account_id
 AND q.state='pending'
 AND q.started_at>=COALESCE(m.calibration_started_at,'-infinity'::timestamptz)
 AND (q.official_window_end IS NULL OR ABS(EXTRACT(EPOCH FROM(q.official_window_end-m.window_end)))<=300)
 AND q.estimated_points IS DISTINCT FROM ROUND(q.weight*COALESCE(m.points_per_weight,0),8)`, accountID)
	return err
}

func (r *UsageMonitorRepository) ListCodingResets(ctx context.Context) ([]service.CodingQuotaReset, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,effective_at,detected_at,status,source,note,evidence::text,actor_user_id FROM coding_quota_reset_events ORDER BY detected_at DESC LIMIT 30`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]service.CodingQuotaReset, 0)
	for rows.Next() {
		var v service.CodingQuotaReset
		if err := rows.Scan(&v.ID, &v.EffectiveAt, &v.DetectedAt, &v.Status, &v.Source, &v.Note, &v.Evidence, &v.ActorUserID); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *UsageMonitorRepository) ConfirmCodingReset(ctx context.Context, v service.CodingQuotaReset, actor int64) (service.CodingQuotaReset, error) {
	var out service.CodingQuotaReset
	err := r.db.QueryRowContext(ctx, `INSERT INTO coding_quota_reset_events(id,effective_at,status,source,note,evidence,actor_user_id,confirmed_at)
 VALUES($1,$2,'confirmed','admin_confirmed',$3,'{}',$4,NOW()) ON CONFLICT(id) DO UPDATE SET
 status='confirmed',effective_at=CASE WHEN coding_quota_reset_events.status='confirmed' THEN coding_quota_reset_events.effective_at ELSE EXCLUDED.effective_at END,
 actor_user_id=COALESCE(coding_quota_reset_events.actor_user_id,EXCLUDED.actor_user_id),confirmed_at=COALESCE(coding_quota_reset_events.confirmed_at,NOW()),
 note=CASE WHEN coding_quota_reset_events.status='confirmed' THEN coding_quota_reset_events.note ELSE EXCLUDED.note END
 RETURNING id,effective_at,detected_at,status,source,note,evidence::text,actor_user_id`, v.ID, v.EffectiveAt.UTC(), v.Note, actor).Scan(&out.ID, &out.EffectiveAt, &out.DetectedAt, &out.Status, &out.Source, &out.Note, &out.Evidence, &out.ActorUserID)
	return out, err
}

// A missing member never shrinks the denominator. Single-account evidence is
// presented for confirmation; only corroborated whole-pool changes auto-confirm.
func (r *UsageMonitorRepository) detectCodingReset(ctx context.Context, now time.Time) error {
	_, err := r.db.ExecContext(ctx, `WITH members AS(
 SELECT a.id,NULLIF(a.credentials->>'chatgpt_account_id','') AS upstream_id,m.anomaly_at,m.observed_at,m.anomaly_evidence
 FROM accounts a LEFT JOIN coding_quota_meters m ON m.account_id=a.id
 WHERE a.deleted_at IS NULL AND a.parent_account_id IS NULL AND a.platform='openai' AND a.type IN('oauth','setup-token')
 AND EXISTS(SELECT 1 FROM account_groups ag JOIN groups g ON g.id=ag.group_id
 WHERE ag.account_id=a.id AND g.deleted_at IS NULL AND g.usage_kind='coding')
), evidence AS(
 SELECT COUNT(*) AS members,COUNT(DISTINCT upstream_id) AS identities,COUNT(anomaly_at) AS changed,MIN(anomaly_at) AS first_at,MAX(anomaly_at) AS last_at,
 BOOL_AND(observed_at>anomaly_at+INTERVAL '1 second') AS corroborated,
 JSONB_AGG(JSONB_BUILD_OBJECT('account_id',id,'change',anomaly_evidence) ORDER BY id) AS detail,
 MD5(STRING_AGG(id::text||':'||COALESCE(anomaly_at::text,'missing'),',' ORDER BY id)) AS key FROM members
)
INSERT INTO coding_quota_reset_events(id,effective_at,status,source,evidence)
SELECT 'detected:'||key,last_at,
 CASE WHEN members>=2 AND identities=members AND changed=members AND last_at-first_at<=INTERVAL '10 minutes' AND corroborated
 THEN 'confirmed' ELSE 'candidate' END,'account_observations',detail
FROM evidence WHERE changed>0 AND last_at>$1::timestamptz-INTERVAL '1 day'
ON CONFLICT(id) DO UPDATE SET status=CASE WHEN coding_quota_reset_events.status='confirmed' OR EXCLUDED.status='confirmed' THEN 'confirmed' ELSE 'candidate' END`, now.UTC())
	return err
}
