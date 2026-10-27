package service

import (
	"context"
	"fmt"
	"time"
)

// ExperimentBillingEffects snapshots which counters a delayed upstream price
// must update. A later configuration change cannot change the original bill.
type ExperimentBillingEffects struct {
	KeyQuota          bool
	KeyRateLimits     bool
	SubscriptionID    *int64
	AccountQuota      bool
	AccountMultiplier float64
}

// PendingExperimentCharge retains an upstream-log lookup until it can be billed.
type PendingExperimentCharge struct {
	ID                int64
	AccountID         int64
	UpstreamRequestID string
	Attempts          int
}

// ExperimentQuotaReconciler atomically captures each delayed amount once.
type ExperimentQuotaReconciler interface {
	ListPendingExperimentCharges(context.Context, time.Time) ([]PendingExperimentCharge, error)
	CompleteExperimentCharge(context.Context, int64, float64) error
	RetryExperimentCharge(context.Context, int64, string, time.Time) error
	ExperimentQuotaState(context.Context, int64, time.Time) (string, error)
}

func (s *OfficialPointService) reconcileExperiment(ctx context.Context) error {
	store, ok := s.store.(ExperimentQuotaReconciler)
	if !ok || s.grok == nil || s.grok.accountRepo == nil {
		return nil
	}
	items, err := store.ListPendingExperimentCharges(ctx, time.Now())
	if err != nil {
		return err
	}
	for _, item := range items {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		account, lookupErr := s.grok.accountRepo.GetByID(ctx, item.AccountID)
		var quota int
		var found bool
		if lookupErr == nil {
			quota, found, lookupErr = lookupGlobalAIQuota(ctx, account, item.UpstreamRequestID)
		}
		if lookupErr == nil && found {
			if err := store.CompleteExperimentCharge(ctx, item.ID, globalAIYuan(quota)); err != nil {
				return err
			}
			continue
		}
		reason := "upstream consume log not available"
		if lookupErr != nil {
			reason = fmt.Sprintf("%v", lookupErr)
		}
		delay := time.Minute * time.Duration(1<<min(item.Attempts, 6))
		if err := store.RetryExperimentCharge(ctx, item.ID, reason, time.Now().Add(delay)); err != nil {
			return err
		}
	}
	return nil
}

// ExperimentState reports unfinished charges in the current allowance period.
func (s *OfficialPointService) ExperimentState(ctx context.Context, user int64, now time.Time) (string, error) {
	if s == nil {
		return "current", nil
	}
	store, ok := s.store.(ExperimentQuotaReconciler)
	if !ok {
		return "current", nil
	}
	return store.ExperimentQuotaState(ctx, user, now)
}
