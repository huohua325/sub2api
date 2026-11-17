package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
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
		var logs []globalAIConsumeLog
		if lookupErr == nil {
			origin, key := globalAIOrigin(account), strings.TrimSpace(account.GetCredential("api_key"))
			if origin == "" || key == "" {
				lookupErr = fmt.Errorf("GlobalAI accounting identity is unavailable")
			} else {
				logs, lookupErr = globalAILogs.load(ctx, key, origin+globalAILogPath)
			}
		}
		triggerSettled := false
		if lookupErr == nil {
			matches := []PendingExperimentCharge{item}
			if matcher, ok := s.store.(ExperimentRecentChargeMatcher); ok {
				ids := make([]string, 0, len(logs))
				for _, row := range logs {
					if row.RequestID != "" && (row.Type == 0 || row.Type == globalAIConsumeLogType) {
						ids = append(ids, row.RequestID)
					}
				}
				matches, err = matcher.MatchPendingExperimentCharges(ctx, item.AccountID, ids)
				if err != nil {
					return err
				}
			}
			for _, pending := range matches {
				row, found := matchGlobalAIConsumeLog(logs, pending.UpstreamRequestID)
				if !found || row.Quota < 0 {
					continue
				}
				if evidenceStore, ok := s.store.(ExperimentEvidenceReconciler); ok {
					encoded, encodeErr := json.Marshal(row)
					if encodeErr != nil {
						return encodeErr
					}
					digest := sha256.Sum256(encoded)
					evidence := ExperimentChargeEvidence{Source: "globalai_consume_log", SHA256: hex.EncodeToString(digest[:]), AccountID: pending.AccountID, UpstreamRequestID: pending.UpstreamRequestID}
					err = evidenceStore.CompleteExperimentChargeWithEvidence(ctx, pending.ID, globalAIYuan(row.Quota), evidence)
				} else {
					err = store.CompleteExperimentCharge(ctx, pending.ID, globalAIYuan(row.Quota))
				}
				if err != nil {
					return err
				}
				triggerSettled = triggerSettled || pending.ID == item.ID
			}
		}
		if triggerSettled {
			continue
		}
		reason := "upstream consume log not available"
		if lookupErr != nil {
			reason = fmt.Sprintf("%v", lookupErr)
		}
		next := time.Now().Add(time.Minute * time.Duration(1<<min(item.Attempts, 6)))
		var deferred *globalAILogDeferred
		var failed *globalAILogHTTPError
		if errors.As(lookupErr, &deferred) {
			next = deferred.until
		} else if errors.As(lookupErr, &failed) && failed.status == http.StatusTooManyRequests {
			delay := 5 * time.Minute
			if failed.retryAfter > delay {
				delay = failed.retryAfter
			}
			next = time.Now().Add(delay)
		}
		if err := store.RetryExperimentCharge(ctx, item.ID, reason, next); err != nil {
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

// ExperimentAccountingStatus reads unknown charges and in-flight attempts
// without querying the supplier or describing unknown amounts as available.
func (s *OfficialPointService) ExperimentAccountingStatus(ctx context.Context, user int64, now time.Time) (ExperimentAccountingStatus, error) {
	if s == nil {
		return ExperimentAccountingStatus{}, ErrExperimentAccountingUnavailable
	}
	reader, ok := s.store.(ExperimentAccountingStatusReader)
	if !ok {
		return ExperimentAccountingStatus{}, ErrExperimentAccountingUnavailable
	}
	return reader.ReadExperimentAccountingStatus(ctx, user, now)
}
