package service

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/shopspring/decimal"
)

func codingChargeForUsage(log *UsageLog, p *postUsageBillingParams) *CodingQuotaCharge {
	if log == nil || p == nil || p.Account == nil || log.OfficialAccountID == nil {
		return nil
	}
	if !p.Account.IsOAuth() && log.OfficialResetAt == nil {
		return nil
	}
	purpose := PurposeOfGroup(p.Account.Platform, UsageKindCoding)
	if purpose != PurposeOpenAI && purpose != PurposeGrok {
		return nil
	}
	weight := p.Cost.TotalCost
	if weight < 0 {
		return nil
	}
	started := log.QuotaStartedAt
	if started.IsZero() {
		started = log.CreatedAt
		if log.DurationMs != nil {
			started = started.Add(-time.Duration(*log.DurationMs) * time.Millisecond)
		}
	}
	chargePurpose := "other"
	if p.APIKey != nil && p.APIKey.Group != nil {
		chargePurpose = PurposeOfGroup(p.APIKey.Group.Platform, p.APIKey.Group.UsageKind)
	}
	if chargePurpose == "" {
		chargePurpose = "other"
	}
	v := &CodingQuotaCharge{AccountID: *log.OfficialAccountID, Purpose: chargePurpose,
		Weight: decimal.NewFromFloat(weight).String(), StartedAt: started, CompletedAt: log.CreatedAt}
	if log.OfficialUsedPercent != nil && log.OfficialResetAt != nil && log.OfficialObservedAt != nil {
		source := "unknown"
		if log.OfficialSource != nil {
			source = *log.OfficialSource
		}
		v.Observation = &OfficialQuotaObservation{AccountID: v.AccountID, Purpose: purpose, WindowEnd: *log.OfficialResetAt,
			ObservedAt: *log.OfficialObservedAt, Used: strconv.FormatFloat(*log.OfficialUsedPercent, 'f', -1, 64), Source: source}
	}
	// A stored observation retains its real timestamp. Do not manufacture a
	// fresh observation from cached percentages.
	if meter := officialMeterAccount(p.Account, nil); meter != nil {
		used, reset, observed, source := readCodexOfficial(meter.Extra, nil, log.CreatedAt)
		if purpose == PurposeGrok {
			used, reset, observed, source, _ = grokOfficial(meter.Extra, log.CreatedAt)
		}
		if used != nil && reset != nil && observed != nil {
			v.Baseline = &OfficialQuotaObservation{AccountID: v.AccountID, Purpose: purpose,
				WindowEnd: *reset, ObservedAt: *observed, Used: strconv.FormatFloat(*used, 'f', -1, 64), Source: source}
		}
	}
	return v
}

func codingQuotaResult(v CodingQuotaReading, budget string) OfficialPointResult {
	used, ok := parseDecimal(v.Used)
	if !ok {
		return OfficialPointResult{Blocked: true, DataStatus: "error"}
	}
	pending, ok := parseDecimal(v.Pending)
	if !ok {
		return OfficialPointResult{Blocked: true, DataStatus: "error"}
	}
	settled := new(big.Rat).Set(used)
	used.Add(used, pending)
	limit, err := ParseOfficialPointBudget(budget)
	if err != nil {
		return OfficialPointResult{Blocked: true, DataStatus: "error"}
	}
	blocked := used.Cmp(limit) >= 0
	if CodingControlMode(v.Purpose) == "enforce" {
		blocked = settled.Cmp(limit) >= 0
	}
	text := formatFixedHalfAway(used, 8)
	percent := StudentFacingPercent(used, limit)
	status := "current"
	if v.Imported {
		status = "estimated"
	}
	if v.PendingCount > 0 {
		status = "pending"
	}
	if v.UnmeteredCount > 0 {
		status = "partial"
	}
	var updatedAt *time.Time
	if !v.UpdatedAt.IsZero() {
		updatedAt = &v.UpdatedAt
	}
	return OfficialPointResult{Points: &text, Percent: &percent, Blocked: blocked, Reset: &v.End,
		PeriodStart: &v.Start, Pending: &v.Pending, DataStatus: status, UpdatedAt: updatedAt, ResetReason: v.ResetReason}
}

// Start runs bounded background metering. Neither authentication nor dashboard
// reads query the upstream. All work can be replayed after cancellation/restart.
func (s *OfficialPointService) Start(ctx context.Context) {
	if s == nil {
		return
	}
	ledger, ok := s.store.(CodingQuotaLedger)
	if !ok {
		return
	}
	s.workerMu.Lock()
	defer s.workerMu.Unlock()
	if s.workerCancel != nil {
		return
	}
	workerCtx, cancel := context.WithCancel(ctx)
	s.workerCancel = cancel
	s.workerDone = make(chan struct{})
	go func() {
		defer close(s.workerDone)
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			cycleCtx, cycleCancel := context.WithTimeout(workerCtx, 30*time.Second)
			if store, ok := s.store.(CodingBillingStore); ok {
				if err := store.RecoverCodingReservations(cycleCtx, time.Now().UTC()); err != nil {
					slog.Error("coding_reservation_recovery_failed", "error", err)
				}
				if _, err := store.ReplayCodingBilling(cycleCtx, time.Now().UTC(), 50); err != nil {
					slog.Error("coding_billing_replay_failed", "error", err)
				}
			}
			if err := s.reconcileLedger(cycleCtx, ledger); err != nil && workerCtx.Err() == nil {
				slog.Warn("coding_quota_reconcile_failed", "error", err)
			}
			cycleCancel()
			select {
			case <-workerCtx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// Stop cancels upstream I/O before database infrastructure shuts down.
func (s *OfficialPointService) Stop() {
	if s == nil {
		return
	}
	s.workerMu.Lock()
	cancel, done := s.workerCancel, s.workerDone
	s.workerMu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}

func (s *OfficialPointService) reconcileLedger(ctx context.Context, ledger CodingQuotaLedger) error {
	rows, err := s.store.ListOfficialParents(ctx)
	if err != nil {
		return err
	}
	rows = s.PrepareParents(ctx, rows)
	now := time.Now()
	for _, row := range rows {
		if row.Platform == PlatformOpenAI && s.codexQuota != nil {
			observed := parseOptionalTime(row.CodexUsageUpdatedAt)
			if (observed == nil || now.Sub(*observed) >= openAIProbeCacheTTL) && s.allowProbe(row.ID, now) {
				usage, probeErr := s.codexQuota.QueryUsage(ctx, row.ID)
				if probeErr == nil {
					if observation := codexUsageObservation(row.ID, usage, now); observation != nil {
						if err := ledger.ObserveCodingQuota(ctx, *observation); err != nil {
							return err
						}
						if s.codexQuota.accountRepo != nil {
							p, _ := strconv.ParseFloat(observation.Used, 64)
							if err := s.codexQuota.accountRepo.UpdateExtra(ctx, row.ID, map[string]any{"codex_7d_used_percent": p,
								"codex_7d_reset_at": observation.WindowEnd.Format(time.RFC3339), "codex_7d_window_minutes": 10080,
								"codex_usage_updated_at": observation.ObservedAt.Format(time.RFC3339)}); err != nil {
								return err
							}
						}
						continue
					}
				}
			}
		}
		snap, _, _ := readParentSnapshot(row, now)
		if snap == nil || snap.Observed == nil {
			continue
		}
		source := snap.Source
		if row.CodexLocalResetAt != nil {
			if local, e := time.Parse(time.RFC3339, *row.CodexLocalResetAt); e == nil && !snap.Observed.Before(local.Add(-time.Minute)) && snap.Observed.Before(local.Add(10*time.Minute)) {
				source = "codex_local_reset"
			}
		}
		if err := ledger.ObserveCodingQuota(ctx, OfficialQuotaObservation{AccountID: row.ID, Purpose: monitorPurpose(row.Platform),
			WindowEnd: snap.Reset, ObservedAt: *snap.Observed, Used: snap.PercentText, Source: source}); err != nil {
			return err
		}
	}
	// Allow in-flight async billing records to enter before closing an interval.
	if err := ledger.ReconcileCodingQuota(ctx, now.Add(-30*time.Second)); err != nil {
		return err
	}
	return s.reconcileExperiment(ctx)
}

// SetCodexQuota enables a read-only billing endpoint for idle/exhausted pools.
// It never sends model/probe conversations and never uses Spark's extra bucket.
func (s *OfficialPointService) SetCodexQuota(quota *OpenAIQuotaService) {
	if s != nil {
		s.codexQuota = quota
	}
}

func codexUsageObservation(accountID int64, usage *OpenAIQuotaUsage, now time.Time) *OfficialQuotaObservation {
	if usage == nil || usage.RateLimit == nil {
		return nil
	}
	for _, window := range []*OpenAIRateLimitWindow{usage.RateLimit.PrimaryWindow, usage.RateLimit.SecondaryWindow} {
		if window == nil || window.LimitWindowSeconds != 604800 || window.UsedPercent < 0 || window.UsedPercent > 100 {
			continue
		}
		observed := now
		if usage.FetchedAt > 0 {
			observed = time.Unix(usage.FetchedAt, 0)
		}
		end := time.Unix(window.ResetAt, 0)
		if window.ResetAt <= 0 {
			end = observed.Add(time.Duration(window.ResetAfterSeconds) * time.Second)
		}
		if !end.After(now) {
			continue
		}
		return &OfficialQuotaObservation{AccountID: accountID, Purpose: PurposeOpenAI, WindowEnd: end, ObservedAt: observed,
			Used: strconv.FormatFloat(window.UsedPercent, 'f', -1, 64), Source: "codex_wham_7d"}
	}
	return nil
}

func (s *UsageMonitorService) ListCodingResets(ctx context.Context) ([]CodingQuotaReset, error) {
	if s == nil || s.points == nil {
		return nil, infraerrors.ServiceUnavailable("QUOTA_LEDGER_UNAVAILABLE", "quota ledger unavailable")
	}
	ledger, ok := s.points.store.(CodingQuotaLedger)
	if !ok {
		return nil, infraerrors.ServiceUnavailable("QUOTA_LEDGER_UNAVAILABLE", "quota ledger unavailable")
	}
	return ledger.ListCodingResets(ctx)
}

// ConfirmCodingReset is the explicit single-account/global-event escape hatch.
func (s *UsageMonitorService) ConfirmCodingReset(ctx context.Context, v CodingQuotaReset, actor int64) (CodingQuotaReset, error) {
	if actor <= 0 {
		return CodingQuotaReset{}, infraerrors.Unauthorized("RESET_ACTOR_REQUIRED", "authentication required")
	}
	if len(v.ID) < 8 || len(v.ID) > 160 || strings.TrimSpace(v.Note) == "" || len(v.Note) > 2000 {
		return CodingQuotaReset{}, infraerrors.BadRequest("INVALID_RESET_EVENT", "event id and reason are required")
	}
	now := time.Now()
	if v.EffectiveAt.IsZero() || v.EffectiveAt.After(now.Add(5*time.Second)) || v.EffectiveAt.Before(now.Add(-7*24*time.Hour)) {
		return CodingQuotaReset{}, infraerrors.BadRequest("INVALID_RESET_TIME", "reset time must be within the past seven days")
	}
	if s == nil || s.points == nil {
		return CodingQuotaReset{}, infraerrors.ServiceUnavailable("QUOTA_LEDGER_UNAVAILABLE", "quota ledger unavailable")
	}
	ledger, ok := s.points.store.(CodingQuotaLedger)
	if !ok {
		return CodingQuotaReset{}, fmt.Errorf("quota ledger unavailable")
	}
	return ledger.ConfirmCodingReset(ctx, v, actor)
}
