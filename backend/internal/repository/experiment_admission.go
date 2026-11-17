package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/shopspring/decimal"
)

// ReserveExperimentRequest durably serializes unknown-cost GlobalAI requests
// across every key belonging to the same user. No upstream I/O runs in the transaction.
func (r *UsageMonitorRepository) ReserveExperimentRequest(ctx context.Context, in service.ExperimentAdmissionInput) error {
	if r == nil || r.db == nil || strings.TrimSpace(in.RequestID) == "" || len(in.RequestID) > 256 || in.UserID <= 0 || in.APIKeyID <= 0 || in.AccountID <= 0 || in.StartedAt.IsZero() {
		return service.ErrExperimentAccountingUnavailable
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%w: %v", service.ErrExperimentAccountingUnavailable, err)
	}
	defer func() { _ = tx.Rollback() }() // Commit below is the only successful exit.
	var limitText, usedText, period string
	var start, now time.Time
	err = tx.QueryRowContext(ctx, `SELECT limit_amount::text,`+personalExperimentUsageSQL("w")+`::text,period,window_start,clock_timestamp()
 FROM user_purpose_allowances w WHERE user_id=$1 AND purpose='experiment' FOR UPDATE`, in.UserID).Scan(&limitText, &usedText, &period, &start, &now)
	if err != nil {
		return fmt.Errorf("%w: %v", service.ErrExperimentAccountingUnavailable, err)
	}
	limit, err := decimal.NewFromString(limitText)
	if err != nil {
		return service.ErrExperimentAccountingUnavailable
	}
	used, err := decimal.NewFromString(usedText)
	if err != nil {
		return service.ErrExperimentAccountingUnavailable
	}
	period = service.AllowancePeriod(service.PurposeExperiment, period)
	if service.PurposeWindowExpired(period, start, now) {
		start = service.RenewedPurposeWindow(period, start, now)
		used = decimal.Zero
	}
	end := service.PurposeResetsAt(period, start)
	if in.StartedAt.Before(start) || !in.StartedAt.Before(end) {
		return service.ErrExperimentAccountingUnavailable
	}
	var oldUser, oldAccount int64
	var oldStarted time.Time
	var oldState string
	err = tx.QueryRowContext(ctx, `SELECT user_id,account_id,started_at,state FROM experiment_quota_attempts
 WHERE request_id=$1 AND api_key_id=$2 FOR UPDATE`, in.RequestID, in.APIKeyID).Scan(&oldUser, &oldAccount, &oldStarted, &oldState)
	if err == nil {
		if oldUser != in.UserID || oldAccount != in.AccountID || !oldStarted.Equal(in.StartedAt.Truncate(time.Microsecond)) {
			return fmt.Errorf("%w: experiment request identity conflict", service.ErrExperimentAccountingUnavailable)
		}
		if oldState != "reserved" {
			return service.ErrExperimentRequestInFlight
		}
		return tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %v", service.ErrExperimentAccountingUnavailable, err)
	}
	if !limit.IsNegative() && used.GreaterThanOrEqual(limit) {
		return service.ErrExperimentQuotaExhausted
	}
	var pending bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM experiment_quota_charges
 WHERE user_id=$1 AND state IN ('pending','unresolved') AND started_at>=$2 AND started_at<$3)`, in.UserID, start, end).Scan(&pending)
	if err != nil {
		return fmt.Errorf("%w: %v", service.ErrExperimentAccountingUnavailable, err)
	}
	if pending {
		return service.ErrExperimentAccountingPending
	}
	var inFlight bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM experiment_quota_attempts
 WHERE user_id=$1 AND state IN ('reserved','sent','handed_off','unknown'))`, in.UserID).Scan(&inFlight)
	if err != nil {
		return fmt.Errorf("%w: %v", service.ErrExperimentAccountingUnavailable, err)
	}
	if inFlight {
		return service.ErrExperimentRequestInFlight
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO experiment_quota_attempts(request_id,api_key_id,user_id,account_id,started_at)
 VALUES($1,$2,$3,$4,$5)`, in.RequestID, in.APIKeyID, in.UserID, in.AccountID, in.StartedAt.Truncate(time.Microsecond))
	if err != nil {
		return fmt.Errorf("%w: %v", service.ErrExperimentAccountingUnavailable, err)
	}
	return tx.Commit()
}

// MarkExperimentRequestSent is a one-way checkpoint immediately before upstream I/O.
// A repeated sent checkpoint is rejected rather than authorizing a second send.
func (r *UsageMonitorRepository) MarkExperimentRequestSent(ctx context.Context, requestID string, keyID int64) error {
	if r == nil || r.db == nil {
		return service.ErrExperimentAccountingUnavailable
	}
	result, err := r.db.ExecContext(ctx, `UPDATE experiment_quota_attempts SET state='sent',sent_at=clock_timestamp(),updated_at=clock_timestamp()
 WHERE request_id=$1 AND api_key_id=$2 AND state='reserved'`, requestID, keyID)
	if err != nil {
		return fmt.Errorf("%w: %v", service.ErrExperimentAccountingUnavailable, err)
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		return service.ErrExperimentRequestInFlight
	}
	return nil
}

// ReleaseExperimentRequest releases only a request that has never passed the send checkpoint.
func (r *UsageMonitorRepository) ReleaseExperimentRequest(ctx context.Context, requestID string, keyID int64, reason string) error {
	if r == nil || r.db == nil {
		return service.ErrExperimentAccountingUnavailable
	}
	if len(reason) > 500 {
		reason = reason[:500]
	}
	var state string
	err := r.db.QueryRowContext(ctx, `UPDATE experiment_quota_attempts SET state='released',reason=$3,released_at=clock_timestamp(),updated_at=clock_timestamp()
 WHERE request_id=$1 AND api_key_id=$2 AND state IN ('reserved','released') RETURNING state`, requestID, keyID, reason).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrExperimentRequestInFlight
	}
	if err != nil {
		return fmt.Errorf("%w: %v", service.ErrExperimentAccountingUnavailable, err)
	}
	return nil
}

// RecordExperimentResponse stores the upstream receipt without treating errors
// or missing usage as proof that a sent request was free.
func (r *UsageMonitorRepository) RecordExperimentResponse(ctx context.Context, requestID string, keyID int64, upstreamRequestID string, status int) error {
	if r == nil || r.db == nil || requestID == "" || keyID <= 0 || status < 100 || status > 599 || len(upstreamRequestID) > 1024 {
		return service.ErrExperimentAccountingUnavailable
	}
	result, err := r.db.ExecContext(ctx, `UPDATE experiment_quota_attempts SET response_request_id=$3,response_status=$4,
 response_recorded_at=COALESCE(response_recorded_at,clock_timestamp()),updated_at=clock_timestamp(),
 state=CASE WHEN state='sent' AND $4>=400 THEN 'unknown' ELSE state END
 WHERE request_id=$1 AND api_key_id=$2 AND state IN ('sent','handed_off','unknown','settled')
 AND (response_status IS NULL OR (response_status=$4 AND response_request_id=$3))`, requestID, keyID, upstreamRequestID, status)
	if err != nil {
		return fmt.Errorf("%w: %v", service.ErrExperimentAccountingUnavailable, err)
	}
	n, err := result.RowsAffected()
	if err != nil || n != 1 {
		return fmt.Errorf("%w: upstream receipt identity conflict", service.ErrExperimentAccountingUnavailable)
	}
	return nil
}

// RecoverExperimentUnsentRequests frees a crashed pre-send reservation while
// keeping every sent, handed-off or unknown attempt durably occupied.
func (r *UsageMonitorRepository) RecoverExperimentUnsentRequests(ctx context.Context, before time.Time) (int64, error) {
	if r == nil || r.db == nil || before.IsZero() {
		return 0, service.ErrExperimentAccountingUnavailable
	}
	result, err := r.db.ExecContext(ctx, `UPDATE experiment_quota_attempts
 SET state='released',reason='recovered stale reservation before durable send checkpoint',released_at=clock_timestamp(),updated_at=clock_timestamp()
 WHERE state='reserved' AND created_at<LEAST($1::timestamptz,clock_timestamp()-interval '5 minutes')`, before)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", service.ErrExperimentAccountingUnavailable, err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("%w: %v", service.ErrExperimentAccountingUnavailable, err)
	}
	return count, nil
}

func finishExperimentAttemptForBilling(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand, chargeState string) error {
	var userID, accountID int64
	var state string
	err := tx.QueryRowContext(ctx, `SELECT user_id,account_id,state FROM experiment_quota_attempts
 WHERE request_id=$1 AND api_key_id=$2 FOR UPDATE`, cmd.RequestID, cmd.APIKeyID).Scan(&userID, &accountID, &state)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // Legacy requests have no admission record and must still settle.
	}
	if err != nil {
		return err
	}
	if userID != cmd.UserID || accountID != cmd.AccountID || state != "sent" {
		return errors.New("experiment billing admission identity or state conflict")
	}
	next := "handed_off"
	switch chargeState {
	case "settled":
		next = "settled"
	case "unresolved":
		next = "unknown"
	}
	_, err = tx.ExecContext(ctx, `UPDATE experiment_quota_attempts SET state=$3,handed_off_at=clock_timestamp(),
 settled_at=CASE WHEN $3='settled' THEN clock_timestamp() ELSE settled_at END,updated_at=clock_timestamp()
 WHERE request_id=$1 AND api_key_id=$2`, cmd.RequestID, cmd.APIKeyID, next)
	return err
}
