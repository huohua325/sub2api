package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"strconv"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// ReadExperimentConcurrency returns the saved grant, without calling upstreams.
func (r *UsageMonitorRepository) ReadExperimentConcurrency(ctx context.Context, userID int64) (service.ExperimentConcurrencyConfig, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND deleted_at IS NULL)`, userID).Scan(&exists)
	if err != nil {
		return service.ExperimentConcurrencyConfig{}, err
	}
	if !exists {
		return service.ExperimentConcurrencyConfig{}, infraerrors.NotFound("USER_NOT_FOUND", "user not found")
	}
	policy, err := readExperimentAdmissionPolicy(ctx, r.db, userID)
	return service.ExperimentConcurrencyConfig{Enabled: policy.Version == 1, MaxActive: policy.MaxActive}, err
}

// SetExperimentConcurrency atomically edits one grant while preserving all other
// users and outstanding liabilities. No active request is cancelled or released.
func (r *UsageMonitorRepository) SetExperimentConcurrency(ctx context.Context, actor, userID int64, in service.ExperimentConcurrencyConfig) (service.ExperimentConcurrencyConfig, error) {
	if actor <= 0 || userID <= 0 || in.MaxActive < 1 || in.MaxActive > 1000 {
		return in, infraerrors.BadRequest("INVALID_EXPERIMENT_CONCURRENCY", "Experiment concurrency must be an integer from 1 to 1000")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return in, err
	}
	defer func() { _ = tx.Rollback() }() // Commit is the only successful mutation.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext('experiment_async_admission_policy'))`); err != nil {
		return in, err
	}
	var target int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id=$1 AND deleted_at IS NULL FOR KEY SHARE`, userID).Scan(&target); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return in, infraerrors.NotFound("USER_NOT_FOUND", "user not found")
		}
		return in, err
	}
	var raw string
	err = tx.QueryRowContext(ctx, `SELECT value FROM settings WHERE key='experiment_async_admission_policy' FOR UPDATE`).Scan(&raw)
	policy := experimentAdmissionPolicy{Version: 1, MaxActive: 10, MaxOutstanding: 100, MaxPendingAgeSeconds: 900}
	if err == nil {
		policy, err = decodeExperimentAdmissionPolicy(raw)
	} else if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	if err != nil {
		return in, err
	}
	if policy.UserLimits == nil {
		policy.UserLimits = map[string]int64{}
	}
	before := service.ExperimentConcurrencyConfig{Enabled: slices.Contains(policy.UserIDs, userID), MaxActive: policy.MaxActive}
	id := strconv.FormatInt(userID, 10)
	if prior, ok := policy.UserLimits[id]; ok {
		before.MaxActive = prior
	}
	policy.UserIDs = slices.DeleteFunc(policy.UserIDs, func(id int64) bool { return id == userID })
	delete(policy.UserLimits, id)
	if in.Enabled {
		policy.UserIDs = append(policy.UserIDs, userID)
		policy.UserLimits[id] = in.MaxActive
	}
	slices.Sort(policy.UserIDs)
	encoded, err := json.Marshal(policy)
	if err != nil {
		return in, err
	}
	if _, err = decodeExperimentAdmissionPolicy(string(encoded)); err != nil {
		return in, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO settings(key,value,updated_at) VALUES('experiment_async_admission_policy',$1,clock_timestamp())
 ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value,updated_at=EXCLUDED.updated_at`, string(encoded))
	if err != nil {
		return in, err
	}
	if err := tx.Commit(); err != nil {
		return in, err
	}
	slog.Info("admin.experiment_concurrency_updated", "actor_admin_id", actor, "target_user_id", userID, "before", before, "after", in)
	if !in.Enabled {
		in.MaxActive = 1
	}
	return in, nil
}
