package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type purposeAllowanceRepository struct {
	db *sql.DB
}

func NewPurposeAllowanceRepository(db *sql.DB) service.PurposeAllowanceRepository {
	return &purposeAllowanceRepository{db: db}
}

func (r *purposeAllowanceRepository) List(ctx context.Context, userID int64) ([]service.PurposeAllowance, error) {
	if r == nil || r.db == nil || userID <= 0 {
		return nil, nil
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT purpose, limit_amount, used_amount, period, window_start
		FROM user_purpose_allowances
		WHERE user_id = $1
		ORDER BY purpose
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]service.PurposeAllowance, 0, 3)
	for rows.Next() {
		var item service.PurposeAllowance
		item.UserID = userID
		if err := rows.Scan(&item.Purpose, &item.LimitAmount, &item.UsedAmount, &item.Period, &item.WindowStart); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *purposeAllowanceRepository) EnsureDefaults(ctx context.Context, userID int64) error {
	if r == nil || r.db == nil || userID <= 0 {
		return nil
	}
	policy := loadPurposeAllowancePolicy(ctx, r.db)
	now := time.Now()
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO user_purpose_allowances (user_id, purpose, limit_amount, used_amount, period, window_start)
		VALUES
			($1, 'openai', $2, 0, $3, $4),
			($1, 'grok', $5, 0, $6, $7),
			($1, 'experiment', $8, 0, $9, $10)
		ON CONFLICT (user_id, purpose) DO NOTHING
	`, userID,
		policy.OpenAI.Limit, policy.OpenAI.Period, policyWindowStart(policy.OpenAI.Period, now),
		policy.Grok.Limit, policy.Grok.Period, policyWindowStart(policy.Grok.Period, now),
		policy.Experiment.Limit, policy.Experiment.Period, policyWindowStart(policy.Experiment.Period, now))
	return err
}

func (r *purposeAllowanceRepository) ApplyPolicy(ctx context.Context, policy service.PurposeAllowancePolicy) error {
	if r == nil || r.db == nil {
		return nil
	}
	normalized, err := service.NormalizePurposeAllowancePolicy(policy)
	if err != nil {
		return err
	}
	for _, item := range []struct {
		purpose string
		rule    service.PurposeAllowanceRule
	}{
		{service.PurposeExperiment, normalized.Experiment},
	} {
		if _, err := r.db.ExecContext(ctx, `
			UPDATE user_purpose_allowances
			SET limit_amount = $2, period = $3, updated_at = NOW()
			WHERE purpose = $1
		`, item.purpose, item.rule.Limit, item.rule.Period); err != nil {
			return err
		}
	}
	return nil
}

func policyWindowStart(period string, now time.Time) time.Time {
	if period == "month" {
		return service.ShanghaiMonthStart(now)
	}
	return now
}

type settingValueQuery interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func loadPurposeAllowancePolicy(ctx context.Context, db settingValueQuery) service.PurposeAllowancePolicy {
	fallback := service.DefaultPurposeAllowancePolicy()
	if db == nil {
		return fallback
	}
	var raw string
	err := db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = $1`, service.SettingKeyPurposeAllowancePolicy).Scan(&raw)
	if err != nil || strings.TrimSpace(raw) == "" {
		return fallback
	}
	var policy service.PurposeAllowancePolicy
	if err := json.Unmarshal([]byte(raw), &policy); err != nil {
		return fallback
	}
	normalized, err := service.NormalizePurposeAllowancePolicy(policy)
	if err != nil {
		return fallback
	}
	return normalized
}
