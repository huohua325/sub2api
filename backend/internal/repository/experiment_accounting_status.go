package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// ReadExperimentAccountingStatus uses the admission policy and disjoint counts
// in one consistent snapshot, without querying supplier logs.
func (r *UsageMonitorRepository) ReadExperimentAccountingStatus(ctx context.Context, userID int64, now time.Time) (service.ExperimentAccountingStatus, error) {
	var out service.ExperimentAccountingStatus
	if r == nil || r.db == nil || userID <= 0 || now.IsZero() {
		return out, service.ErrExperimentAccountingUnavailable
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return out, fmt.Errorf("%w: %v", service.ErrExperimentAccountingUnavailable, err)
	}
	defer func() { _ = tx.Rollback() }() // Successful read ends with Commit.
	policy, err := readExperimentAdmissionPolicy(ctx, tx, userID)
	if err == nil {
		out, err = readExperimentAdmissionCounts(ctx, tx, userID, now.Add(-time.Duration(policy.MaxPendingAgeSeconds)*time.Second))
	}
	if err != nil {
		return out, fmt.Errorf("%w: %v", service.ErrExperimentAccountingUnavailable, err)
	}
	out.ActiveLimit = policy.MaxActive
	return out, tx.Commit()
}
