package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

const (
	purposeAdjustUserSQL = `
		SELECT 1
		FROM users
		WHERE id = $1 AND deleted_at IS NULL`

	purposeAdjustLockSQL = `
		SELECT limit_amount::text, used_amount::text, period, window_start
		FROM user_purpose_allowances
		WHERE user_id = $1 AND purpose = $2
		FOR UPDATE`

	purposeAdjustLimitSQL = `
		UPDATE user_purpose_allowances
		SET limit_amount = $3::numeric, updated_at = NOW()
		WHERE user_id = $1 AND purpose = $2
		RETURNING limit_amount::text, used_amount::text, period, window_start`

	purposeAdjustUsedSQL = `
		UPDATE user_purpose_allowances
		SET used_amount = $3::numeric, updated_at = NOW()
		WHERE user_id = $1 AND purpose = $2
		RETURNING limit_amount::text, used_amount::text, period, window_start`

	purposeAdjustWindowSQL = `
		UPDATE user_purpose_allowances
		SET window_start = $3, updated_at = NOW()
		WHERE user_id = $1 AND purpose = $2
		RETURNING limit_amount::text, used_amount::text, period, window_start`

	purposeAdjustEnsureSQL = `
		INSERT INTO user_purpose_allowances (user_id, purpose, limit_amount, used_amount, period, window_start)
		VALUES
			($1, 'openai', $2, 0, $3, $4),
			($1, 'grok', $5, 0, $6, $7),
			($1, 'experiment', $8, 0, $9, $10)
		ON CONFLICT (user_id, purpose) DO NOTHING`

	purposeAdjustAuditSQL = `
		INSERT INTO purpose_allowance_adjustments (
			actor_user_id, target_user_id, purpose, field, origin, old_value, new_value, note
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at`
)

// Same classification as billing and the experiment-pool count: current
// groups.usage_kind, including groups that have since been deleted.
const purposeAdjustSumSQL = `
	SELECT COALESCE(ROUND(SUM(ul.actual_cost), 8), 0)::text
	FROM usage_logs ul
	JOIN groups g ON g.id = ul.group_id
	WHERE ul.user_id = $1
	  AND ul.created_at >= $2
	  AND ul.created_at < $3
	  AND (
	    CASE
	        WHEN btrim(g.platform) = '' THEN ''
	        WHEN COALESCE(NULLIF(lower(btrim(g.usage_kind)), ''), 'experiment') = 'experiment'
	            THEN 'experiment'
	        WHEN lower(btrim(g.usage_kind)) = 'coding' AND lower(btrim(g.platform)) = 'openai'
	            THEN 'openai'
	        WHEN lower(btrim(g.usage_kind)) = 'coding' AND lower(btrim(g.platform)) = 'grok'
	            THEN 'grok'
	        ELSE ''
	    END
	  ) = $4`

// AdjustPurposeWallet locks one wallet, updates one column, and inserts an audit
// row. Equal values commit without an audit. GET must not call this.
func (r *UsageMonitorRepository) AdjustPurposeWallet(ctx context.Context, cmd service.PurposeWalletAdjust) (service.PurposeWalletAdjustResult, error) {
	if err := r.ready(); err != nil {
		return service.PurposeWalletAdjustResult{}, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return service.PurposeWalletAdjustResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	var exists int
	if err := tx.QueryRowContext(ctx, purposeAdjustUserSQL, cmd.UserID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return service.PurposeWalletAdjustResult{}, infraerrors.NotFound("USER_NOT_FOUND", "user not found")
		}
		return service.PurposeWalletAdjustResult{}, err
	}
	row, err := lockPurposeWallet(ctx, tx, cmd.UserID, cmd.Purpose)
	if errors.Is(err, sql.ErrNoRows) {
		// Defaults have to land in this transaction so the lock below sees them.
		if err := ensurePurposeWalletsTx(ctx, tx, cmd.UserID, cmd.Now); err != nil {
			return service.PurposeWalletAdjustResult{}, err
		}
		row, err = lockPurposeWallet(ctx, tx, cmd.UserID, cmd.Purpose)
	}
	if err != nil {
		return service.PurposeWalletAdjustResult{}, err
	}

	// An expired used_amount is replaced by the next bill, so do not sum logs
	// that this request is about to reject.
	if err := service.PurposeAdjustExpired(cmd.Action, row.Purpose, row.Period, row.WindowStart, cmd.Now); err != nil {
		return service.PurposeWalletAdjustResult{}, err
	}
	rebuilt := ""
	if cmd.Action == service.PurposeAdjustRebuildUsed {
		period := purposeAdjustPeriod(row.Purpose, row.Period)
		end := service.PurposeResetsAt(period, row.WindowStart)
		if err := tx.QueryRowContext(ctx, purposeAdjustSumSQL, cmd.UserID, row.WindowStart, end, cmd.Purpose).Scan(&rebuilt); err != nil {
			return service.PurposeWalletAdjustResult{}, err
		}
	}
	effect, err := service.PurposeAdjustmentEffect(row, cmd, rebuilt)
	if err != nil {
		return service.PurposeWalletAdjustResult{}, err
	}
	if !effect.Changed {
		if err := tx.Commit(); err != nil {
			return service.PurposeWalletAdjustResult{}, err
		}
		committed = true
		return service.PurposeWalletAdjustResult{Changed: false, Row: row}, nil
	}

	updated, err := updatePurposeWallet(ctx, tx, cmd, effect)
	if err != nil {
		return service.PurposeWalletAdjustResult{}, err
	}
	newValue := effect.NewValue
	if cmd.Action != service.PurposeAdjustAlignWindow {
		if effect.Field == service.PurposeAdjustFieldLimit {
			newValue = updated.LimitAmount
		} else {
			newValue = updated.UsedAmount
		}
	}
	audit, err := insertPurposeAdjustment(ctx, tx, cmd, effect, newValue)
	if err != nil {
		return service.PurposeWalletAdjustResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return service.PurposeWalletAdjustResult{}, err
	}
	committed = true
	return service.PurposeWalletAdjustResult{Changed: true, Row: updated, Audit: audit}, nil
}

// ListPurposeAdjustments reads the audit table only. It does not insert wallets.
func (r *UsageMonitorRepository) ListPurposeAdjustments(ctx context.Context, query service.PurposeAdjustmentQuery) ([]service.PurposeAdjustmentAudit, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	statement, args := purposeAdjustmentListSQL(query)
	rows, err := r.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]service.PurposeAdjustmentAudit, 0)
	for rows.Next() {
		var item service.PurposeAdjustmentAudit
		if err := rows.Scan(
			&item.ID,
			&item.ActorUserID,
			&item.TargetUserID,
			&item.Purpose,
			&item.Field,
			&item.Origin,
			&item.OldValue,
			&item.NewValue,
			&item.Note,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func lockPurposeWallet(ctx context.Context, tx *sql.Tx, userID int64, purpose string) (service.UsageMonitorUserRow, error) {
	row, err := scanPurposeWallet(tx.QueryRowContext(ctx, purposeAdjustLockSQL, userID, purpose))
	if err != nil {
		return service.UsageMonitorUserRow{}, err
	}
	row.ID = userID
	row.Purpose = purpose
	row.HasWallet = true
	return row, nil
}

func updatePurposeWallet(ctx context.Context, tx *sql.Tx, cmd service.PurposeWalletAdjust, effect service.PurposeWalletEffect) (service.UsageMonitorUserRow, error) {
	var query scanner
	switch cmd.Action {
	case service.PurposeAdjustSetLimit:
		query = tx.QueryRowContext(ctx, purposeAdjustLimitSQL, cmd.UserID, cmd.Purpose, effect.Amount)
	case service.PurposeAdjustSetUsed, service.PurposeAdjustRebuildUsed:
		query = tx.QueryRowContext(ctx, purposeAdjustUsedSQL, cmd.UserID, cmd.Purpose, effect.Amount)
	case service.PurposeAdjustAlignWindow:
		query = tx.QueryRowContext(ctx, purposeAdjustWindowSQL, cmd.UserID, cmd.Purpose, effect.Window)
	default:
		return service.UsageMonitorUserRow{}, infraerrors.BadRequest("INVALID_ADJUSTMENT_ACTION", "action must be set_limit, set_used, rebuild_used, or align_window")
	}
	row, err := scanPurposeWallet(query)
	if err != nil {
		return service.UsageMonitorUserRow{}, err
	}
	row.ID = cmd.UserID
	row.Purpose = cmd.Purpose
	row.HasWallet = true
	return row, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanPurposeWallet(row scanner) (service.UsageMonitorUserRow, error) {
	var out service.UsageMonitorUserRow
	var period sql.NullString
	if err := row.Scan(&out.LimitAmount, &out.UsedAmount, &period, &out.WindowStart); err != nil {
		return service.UsageMonitorUserRow{}, err
	}
	if period.Valid {
		out.Period = period.String
	}
	return out, nil
}

func ensurePurposeWalletsTx(ctx context.Context, tx *sql.Tx, userID int64, now time.Time) error {
	if now.IsZero() {
		now = time.Now()
	}
	policy := loadPurposeAllowancePolicy(ctx, tx)
	_, err := tx.ExecContext(ctx, purposeAdjustEnsureSQL, userID,
		policy.OpenAI.Limit, policy.OpenAI.Period, policyWindowStart(policy.OpenAI.Period, now),
		policy.Grok.Limit, policy.Grok.Period, policyWindowStart(policy.Grok.Period, now),
		policy.Experiment.Limit, policy.Experiment.Period, policyWindowStart(policy.Experiment.Period, now))
	return err
}

func insertPurposeAdjustment(ctx context.Context, tx *sql.Tx, cmd service.PurposeWalletAdjust, effect service.PurposeWalletEffect, newValue string) (*service.PurposeAdjustmentAudit, error) {
	audit := service.PurposeAdjustmentAudit{
		ActorUserID:  cmd.ActorUserID,
		TargetUserID: cmd.UserID,
		Purpose:      cmd.Purpose,
		Field:        effect.Field,
		Origin:       effect.Origin,
		OldValue:     effect.OldValue,
		NewValue:     newValue,
		Note:         cmd.Note,
	}
	if err := tx.QueryRowContext(ctx, purposeAdjustAuditSQL,
		audit.ActorUserID,
		audit.TargetUserID,
		audit.Purpose,
		audit.Field,
		audit.Origin,
		audit.OldValue,
		audit.NewValue,
		audit.Note,
	).Scan(&audit.ID, &audit.CreatedAt); err != nil {
		return nil, err
	}
	return &audit, nil
}

func purposeAdjustmentListSQL(query service.PurposeAdjustmentQuery) (string, []any) {
	limit := query.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	statement := `
		SELECT id, actor_user_id, target_user_id, purpose, field, origin,
		       old_value, new_value, note, created_at
		FROM purpose_allowance_adjustments
		WHERE 1 = 1`
	args := make([]any, 0, 3)
	if query.UserID != nil {
		args = append(args, *query.UserID)
		statement += fmt.Sprintf(" AND target_user_id = $%d", len(args))
	}
	if query.BeforeID != nil {
		args = append(args, *query.BeforeID)
		statement += fmt.Sprintf(" AND id < $%d", len(args))
	}
	args = append(args, limit)
	statement += fmt.Sprintf(" ORDER BY id DESC LIMIT $%d", len(args))
	return statement, args
}

func purposeAdjustPeriod(purpose, period string) string {
	if period == "week" || period == "month" {
		return period
	}
	return service.PurposePeriod(purpose)
}
