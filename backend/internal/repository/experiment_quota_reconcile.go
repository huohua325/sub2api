package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

func recordExperimentQuotaCharge(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand) error {
	state := "settled"
	var amount any = cmd.PurposeCost
	if cmd.ExperimentPending {
		state = "pending"
		amount = nil
		if cmd.ExperimentUpstreamRequestID == "" {
			state = "unresolved"
		}
	}
	started := cmd.StartedAt
	if started.IsZero() {
		started = time.Now()
	}
	effects, err := json.Marshal(cmd.ExperimentEffects)
	if err != nil {
		return err
	}
	var chargeID int64
	err = tx.QueryRowContext(ctx, `INSERT INTO experiment_quota_charges(request_id,api_key_id,user_id,account_id,upstream_request_id,started_at,amount,state,billing_effects)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb) ON CONFLICT(request_id,api_key_id) DO NOTHING RETURNING id`, cmd.RequestID, cmd.APIKeyID, cmd.UserID, cmd.AccountID, cmd.ExperimentUpstreamRequestID, started, amount, state, string(effects)).Scan(&chargeID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && state == "settled" && cmd.ExperimentUpstreamRequestID != "" {
		if err := recordInitialExperimentSettlement(ctx, tx, chargeID, cmd, started); err != nil {
			return err
		}
	}
	return finishExperimentAttemptForBilling(ctx, tx, cmd, state)
}

func (r *UsageMonitorRepository) ListPendingExperimentCharges(ctx context.Context, now time.Time) ([]service.PendingExperimentCharge, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,account_id,upstream_request_id,attempts FROM
 (SELECT DISTINCT ON(account_id) id,account_id,upstream_request_id,attempts,next_retry_at FROM experiment_quota_charges
 WHERE state='pending' AND next_retry_at<=$1 ORDER BY account_id,next_retry_at,id) candidates
 ORDER BY next_retry_at,id LIMIT 8`, now)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]service.PendingExperimentCharge, 0)
	for rows.Next() {
		var v service.PendingExperimentCharge
		if err := rows.Scan(&v.ID, &v.AccountID, &v.UpstreamRequestID, &v.Attempts); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// MatchPendingExperimentCharges matches a cached supplier page without issuing
// one database or upstream lookup for every old charge in the backlog.
func (r *UsageMonitorRepository) MatchPendingExperimentCharges(ctx context.Context, accountID int64, upstreamRequestIDs []string) ([]service.PendingExperimentCharge, error) {
	if len(upstreamRequestIDs) == 0 {
		return nil, nil
	}
	if r == nil || r.db == nil || accountID <= 0 || len(upstreamRequestIDs) > 10000 {
		return nil, service.ErrExperimentAccountingUnavailable
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id,account_id,upstream_request_id,attempts FROM experiment_quota_charges
 WHERE account_id=$1 AND state='pending' AND upstream_request_id=ANY($2) ORDER BY id`, accountID, pq.Array(upstreamRequestIDs))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]service.PendingExperimentCharge, 0)
	for rows.Next() {
		var v service.PendingExperimentCharge
		if err := rows.Scan(&v.ID, &v.AccountID, &v.UpstreamRequestID, &v.Attempts); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *UsageMonitorRepository) CompleteExperimentCharge(ctx context.Context, id int64, amount float64) error {
	return r.completeExperimentChargeAutomatically(ctx, id, amount)
}

func applyDeferredExperimentEffects(ctx context.Context, tx *sql.Tx, key, account int64, started time.Time, amount float64, e service.ExperimentBillingEffects) error {
	if e.KeyQuota {
		if _, err := incrementUsageBillingAPIKeyQuota(ctx, tx, key, amount); err != nil && !errors.Is(err, service.ErrAPIKeyNotFound) {
			return err
		}
	}
	if e.KeyRateLimits {
		// Old requests must not consume a newly opened rate-limit window.
		_, err := tx.ExecContext(ctx, `UPDATE api_keys SET
 usage_5h=usage_5h+CASE WHEN $3>=COALESCE(window_5h_start,$3) AND COALESCE(window_5h_start,$3)+INTERVAL '5 hours'>NOW() THEN $2 ELSE 0 END,
 usage_1d=usage_1d+CASE WHEN $3>=COALESCE(window_1d_start,date_trunc('day',$3::timestamptz)) AND COALESCE(window_1d_start,date_trunc('day',$3::timestamptz))+INTERVAL '24 hours'>NOW() THEN $2 ELSE 0 END,
 usage_7d=usage_7d+CASE WHEN $3>=COALESCE(window_7d_start,date_trunc('day',$3::timestamptz)) AND COALESCE(window_7d_start,date_trunc('day',$3::timestamptz))+INTERVAL '168 hours'>NOW() THEN $2 ELSE 0 END,
 window_5h_start=CASE WHEN window_5h_start IS NULL AND $3+INTERVAL '5 hours'>NOW() THEN $3 ELSE window_5h_start END,
 window_1d_start=CASE WHEN window_1d_start IS NULL AND date_trunc('day',$3::timestamptz)+INTERVAL '24 hours'>NOW() THEN date_trunc('day',$3::timestamptz) ELSE window_1d_start END,
 window_7d_start=CASE WHEN window_7d_start IS NULL AND date_trunc('day',$3::timestamptz)+INTERVAL '168 hours'>NOW() THEN date_trunc('day',$3::timestamptz) ELSE window_7d_start END,
 updated_at=NOW() WHERE id=$1 AND deleted_at IS NULL`, key, amount, started)
		if err != nil {
			return err
		}
	}
	if e.SubscriptionID != nil {
		_, err := tx.ExecContext(ctx, `UPDATE user_subscriptions SET
 daily_usage_usd=daily_usage_usd+CASE WHEN $3>=daily_window_start THEN $2 ELSE 0 END,
 weekly_usage_usd=weekly_usage_usd+CASE WHEN $3>=weekly_window_start THEN $2 ELSE 0 END,
 monthly_usage_usd=monthly_usage_usd+CASE WHEN $3>=monthly_window_start THEN $2 ELSE 0 END,
 updated_at=NOW() WHERE id=$1 AND deleted_at IS NULL`, *e.SubscriptionID, amount, started)
		if err != nil {
			return err
		}
	}
	if e.AccountQuota {
		// Lifetime account accounting survives delayed billing; expired daily
		// and weekly windows are intentionally not reopened by an old request.
		_, err := tx.ExecContext(ctx, `UPDATE accounts SET extra=COALESCE(extra,'{}'::jsonb)||jsonb_build_object(
 'quota_used',COALESCE((extra->>'quota_used')::numeric,0)+$2,
 'quota_daily_used',COALESCE((extra->>'quota_daily_used')::numeric,0)+CASE WHEN COALESCE((extra->>'quota_daily_limit')::numeric,0)>0 AND $3>=(extra->>'quota_daily_start')::timestamptz AND NOT (`+dailyExpiredExpr+`) THEN $2 ELSE 0 END,
 'quota_weekly_used',COALESCE((extra->>'quota_weekly_used')::numeric,0)+CASE WHEN COALESCE((extra->>'quota_weekly_limit')::numeric,0)>0 AND $3>=(extra->>'quota_weekly_start')::timestamptz AND NOT (`+weeklyExpiredExpr+`) THEN $2 ELSE 0 END),updated_at=NOW()
 WHERE id=$1 AND deleted_at IS NULL`, account, service.QuantizeUsageBillingAmount(amount*e.AccountMultiplier), started)
		if err != nil {
			return err
		}
		if err := enqueueSchedulerOutbox(ctx, tx, service.SchedulerOutboxEventAccountChanged, &account, nil, nil); err != nil {
			return err
		}
	}
	return nil
}

func (r *UsageMonitorRepository) RetryExperimentCharge(ctx context.Context, id int64, reason string, next time.Time) error {
	if len(reason) > 500 {
		reason = reason[:500]
	}
	_, err := r.db.ExecContext(ctx, `UPDATE experiment_quota_charges SET attempts=attempts+1,last_error=$2,next_retry_at=$3,updated_at=NOW()
 WHERE id=$1 AND state='pending'`, id, reason, next)
	return err
}

func (r *UsageMonitorRepository) ExperimentQuotaState(ctx context.Context, user int64, now time.Time) (string, error) {
	status, err := r.ReadExperimentAccountingStatus(ctx, user, now)
	if err != nil {
		return "error", err
	}
	if status.PendingRequests+status.UnresolvedRequests+status.InFlightRequests > 0 {
		return "pending", nil
	}
	return "current", nil
}
