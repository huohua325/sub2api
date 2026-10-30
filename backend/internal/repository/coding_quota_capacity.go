package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// CodingCapacityChange is explicit operator-confirmed evidence of an upstream
// capacity increase. It never changes a personal quota or reset event.
type CodingCapacityChange struct {
	ID               string    `json:"id"`
	AccountID        int64     `json:"account_id"`
	Purpose          string    `json:"purpose"`
	BeforeObservedAt time.Time `json:"before_observed_at"`
	EffectiveAt      time.Time `json:"effective_at"`
	WindowEnd        time.Time `json:"window_end"`
	Before           string    `json:"before"`
	After            string    `json:"after"`
	Factor           string    `json:"factor"`
	Actor            string    `json:"actor"`
	Note             string    `json:"note"`
}

// ConfirmCodingCapacityChange records independently confirmed capacity evidence.
func (r *UsageMonitorRepository) ConfirmCodingCapacityChange(ctx context.Context, change CodingCapacityChange) error {
	before, be := decimal.NewFromString(change.Before)
	after, ae := decimal.NewFromString(change.After)
	factor, fe := decimal.NewFromString(change.Factor)
	if len(change.ID) == 0 || len(change.ID) > 256 || change.AccountID <= 0 ||
		(change.Purpose != "openai" && change.Purpose != "grok") ||
		change.BeforeObservedAt.IsZero() || !change.EffectiveAt.After(change.BeforeObservedAt) ||
		!change.WindowEnd.After(change.EffectiveAt) || change.EffectiveAt.After(time.Now().Add(time.Minute)) ||
		strings.TrimSpace(change.Actor) == "" || len(change.Actor) > 128 || strings.TrimSpace(change.Note) == "" || len(change.Note) > 2000 ||
		be != nil || ae != nil || fe != nil || !factor.GreaterThan(decimal.NewFromInt(1)) || factor.GreaterThan(decimal.NewFromInt(1000)) ||
		!before.GreaterThan(after) || before.GreaterThan(decimal.NewFromInt(100)) || after.IsNegative() ||
		before.Div(factor).Sub(after).Abs().GreaterThan(decimal.NewFromInt(1)) {
		return fmt.Errorf("explicit capacity evidence, a matching scale factor, operator and reason are required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err = lockCodingMeter(ctx, tx, change.AccountID); err != nil {
		return err
	}
	var matches bool
	err = tx.QueryRowContext(ctx, `SELECT account_id=$2 AND purpose=$3 AND effective_at=$4 AND before_observed_at=$5
 AND window_end=$6 AND before_used=$7 AND after_used=$8 AND capacity_factor=$9 AND actor=$10 AND note=$11
 FROM coding_quota_capacity_events WHERE id=$1`, change.ID, change.AccountID, change.Purpose, change.EffectiveAt.UTC(), change.BeforeObservedAt.UTC(), change.WindowEnd.UTC(), before.String(), after.String(), factor.String(), change.Actor, change.Note).Scan(&matches)
	if err == nil {
		if !matches {
			return fmt.Errorf("capacity event identity conflicts with retained evidence")
		}
		return tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	err = tx.QueryRowContext(ctx, `SELECT
 EXISTS(SELECT 1 FROM coding_quota_observations WHERE account_id=$1 AND purpose=$2 AND observed_at=$3 AND used=$4 AND processed AND window_end BETWEEN $7::timestamptz-INTERVAL '5 minutes' AND $7::timestamptz+INTERVAL '5 minutes')
 AND EXISTS(SELECT 1 FROM coding_quota_observations WHERE account_id=$1 AND purpose=$2 AND observed_at=$5 AND used=$6 AND processed AND window_end BETWEEN $7::timestamptz-INTERVAL '5 minutes' AND $7::timestamptz+INTERVAL '5 minutes')
 AND NOT EXISTS(SELECT 1 FROM coding_quota_reset_events WHERE $2='openai' AND status='confirmed' AND effective_at>$3 AND effective_at<=$5)`, change.AccountID, change.Purpose, change.BeforeObservedAt.UTC(), before.String(), change.EffectiveAt.UTC(), after.String(), change.WindowEnd.UTC()).Scan(&matches)
	if err != nil {
		return err
	}
	if !matches {
		return fmt.Errorf("capacity change needs processed same-window observations and must not span a confirmed reset")
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO coding_quota_capacity_events(id,account_id,purpose,effective_at,before_observed_at,window_end,before_used,after_used,capacity_factor,previous_rate,actor,note)
 SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9,COALESCE(
 (SELECT rate FROM coding_quota_reservations WHERE account_id=$2 AND purpose=$3 AND started_at<$4 AND rate>0
 AND window_end BETWEEN $6::timestamptz-INTERVAL '5 minutes' AND $6::timestamptz+INTERVAL '5 minutes' ORDER BY started_at DESC,id DESC LIMIT 1),
 (SELECT points_per_weight FROM coding_quota_meters WHERE account_id=$2 AND observed_at<=$4)), $10,$11`, change.ID, change.AccountID, change.Purpose, change.EffectiveAt.UTC(), change.BeforeObservedAt.UTC(), change.WindowEnd.UTC(), before.String(), after.String(), factor.String(), change.Actor, change.Note)
	if err != nil {
		return err
	}
	// Late confirmation changes calibration scope, never rewinds a later raw
	// counter and never replays previously settled billing commands.
	result, err := tx.ExecContext(ctx, `UPDATE coding_quota_meters SET calibration_started_at=$3,
 used=CASE WHEN observed_at<=$3 THEN $4::numeric ELSE used END,
 points_per_weight=CASE WHEN observed_at<=$3 THEN points_per_weight/$5::numeric ELSE points_per_weight END,
 observed_at=GREATEST(observed_at,$3),updated_at=NOW()
 WHERE account_id=$1 AND purpose=$2 AND window_end BETWEEN $6::timestamptz-INTERVAL '5 minutes' AND $6::timestamptz+INTERVAL '5 minutes'
 AND (calibration_started_at IS NULL OR calibration_started_at<$3)`, change.AccountID, change.Purpose, change.EffectiveAt.UTC(), after.String(), factor.String(), change.WindowEnd.UTC())
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n > 0 {
		_, err = tx.ExecContext(ctx, `UPDATE coding_quota_requests SET state='unmetered',updated_at=NOW()
 WHERE account_id=$1 AND purpose=$2 AND state='pending' AND started_at<$3`, change.AccountID, change.Purpose, change.EffectiveAt.UTC())
		if err != nil {
			return err
		}
		if err = refreshCodingCalibration(ctx, tx, change.AccountID, change.WindowEnd); err != nil {
			return err
		}
	}
	return tx.Commit()
}
