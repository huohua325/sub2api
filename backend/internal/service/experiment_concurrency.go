package service

import (
	"context"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// ExperimentConcurrencyConfig is one user's explicitly granted async capacity.
type ExperimentConcurrencyConfig struct {
	Enabled   bool  `json:"enabled"`
	MaxActive int64 `json:"max_active"`
}

// ExperimentConcurrencyStore persists per-user grants independently of billing.
type ExperimentConcurrencyStore interface {
	ReadExperimentConcurrency(context.Context, int64) (ExperimentConcurrencyConfig, error)
	SetExperimentConcurrency(context.Context, int64, int64, ExperimentConcurrencyConfig) (ExperimentConcurrencyConfig, error)
}

// ExperimentConcurrency reads the administrator's per-user setting.
func (s *UsageMonitorService) ExperimentConcurrency(ctx context.Context, userID int64) (ExperimentConcurrencyConfig, error) {
	store, ok := s.store.(ExperimentConcurrencyStore)
	if !ok {
		return ExperimentConcurrencyConfig{}, ErrExperimentAccountingUnavailable
	}
	return store.ReadExperimentConcurrency(ctx, userID)
}

// SetExperimentConcurrency changes admission for new requests, never running work.
func (s *UsageMonitorService) SetExperimentConcurrency(ctx context.Context, actor, userID int64, in ExperimentConcurrencyConfig) (ExperimentConcurrencyConfig, error) {
	if actor <= 0 || userID <= 0 || in.MaxActive < 1 || in.MaxActive > 1000 {
		return ExperimentConcurrencyConfig{}, infraerrors.BadRequest("INVALID_EXPERIMENT_CONCURRENCY", "Experiment concurrency must be an integer from 1 to 1000")
	}
	store, ok := s.store.(ExperimentConcurrencyStore)
	if !ok {
		return ExperimentConcurrencyConfig{}, ErrExperimentAccountingUnavailable
	}
	return store.SetExperimentConcurrency(ctx, actor, userID, in)
}
