package repository

import (
	"context"
	"database/sql"
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
		SELECT purpose, limit_amount, used_amount, window_start
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
		if err := rows.Scan(&item.Purpose, &item.LimitAmount, &item.UsedAmount, &item.WindowStart); err != nil {
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
	now := time.Now()
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO user_purpose_allowances (user_id, purpose, limit_amount, used_amount, window_start)
		VALUES
			($1, 'openai', 100, 0, $2),
			($1, 'grok', 100, 0, $2),
			($1, 'experiment', 500, 0, $3)
		ON CONFLICT (user_id, purpose) DO NOTHING
	`, userID, now, service.ShanghaiMonthStart(now))
	return err
}
