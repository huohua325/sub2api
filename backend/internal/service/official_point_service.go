package service

import (
	"context"
	"encoding/json"
	"math/big"
	"strconv"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
)

// OfficialPointStore loads parent snapshots and one window aggregate.
type OfficialPointStore interface {
	ListOfficialParents(ctx context.Context) ([]UsageMonitorParentRow, error)
	SumOfficialWindowCost(ctx context.Context, userID int64, windows []UsageMonitorWindowQuery, resetGap time.Duration) ([]OfficialWindowCost, error)
}

// OfficialPointService reads personal point ledgers and meters upstream changes
// in the background. Authentication and dashboard reads perform no upstream I/O.
type OfficialPointService struct {
	settings   *SettingService
	store      OfficialPointStore
	grok       *GrokQuotaService
	codexQuota *OpenAIQuotaService
	now        func() time.Time

	mu           sync.Mutex
	probed       map[int64]time.Time
	workerMu     sync.Mutex
	workerCancel context.CancelFunc
	workerDone   chan struct{}
}

func NewOfficialPointService(settings *SettingService, store OfficialPointStore, grok *GrokQuotaService) *OfficialPointService {
	return &OfficialPointService{
		settings: settings,
		store:    store,
		grok:     grok,
		now:      time.Now,
		probed:   map[int64]time.Time{},
	}
}

func (s *OfficialPointService) Enabled(ctx context.Context) bool {
	// Coding groups always use personal point ledgers. The retired switch no
	// longer enables a non-accumulating yuan-wallet fallback.
	return s != nil
}

func (s *OfficialPointService) Budget(ctx context.Context) OfficialPointBudget {
	if s == nil || s.settings == nil {
		return DefaultOfficialPointBudget()
	}
	return s.settings.GetOfficialPointBudget(ctx)
}

// PrepareParents refreshes a stale Grok billing snapshot in memory. A failed
// probe leaves the row that was loaded before the call.
func (s *OfficialPointService) PrepareParents(ctx context.Context, rows []UsageMonitorParentRow) []UsageMonitorParentRow {
	if s == nil {
		return rows
	}
	now := time.Now()
	if s.now != nil {
		now = s.now()
	}
	out := append([]UsageMonitorParentRow(nil), rows...)
	for i := range out {
		if out[i].Platform != PlatformGrok {
			continue
		}
		if !s.grokNeedsProbe(out[i], now) || !s.allowProbe(out[i].ID, now) || s.grok == nil {
			continue
		}
		result, err := s.grok.ProbeBilling(ctx, out[i].ID)
		if err != nil || result == nil || result.Billing == nil {
			continue
		}
		applyGrokBilling(&out[i], result.Billing)
	}
	return out
}

func (s *OfficialPointService) grokNeedsProbe(row UsageMonitorParentRow, now time.Time) bool {
	_, _, observed, source, _ := grokOfficial(grokSnapshotExtra(row), now)
	if source == "grok_billing_no_weekly_percent" || source == "none" || observed == nil {
		return true
	}
	return now.Sub(*observed) >= openAIProbeCacheTTL
}

func (s *OfficialPointService) allowProbe(accountID int64, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.probed == nil {
		s.probed = map[int64]time.Time{}
	}
	if last, ok := s.probed[accountID]; ok && now.Sub(last) < openAIProbeCacheTTL {
		return false
	}
	s.probed[accountID] = now
	return true
}

func applyGrokBilling(row *UsageMonitorParentRow, billing *xai.BillingSummary) {
	raw, err := json.Marshal(billing)
	if err != nil {
		return
	}
	var snap map[string]any
	if err := json.Unmarshal(raw, &snap); err != nil {
		return
	}
	row.GrokUsagePercent = snapshotString(snap["usage_percent"])
	row.GrokPeriodStart = snapshotString(snap["period_start"])
	row.GrokPeriodEnd = snapshotString(snap["period_end"])
	row.GrokPeriodType = snapshotString(snap["period_type"])
	row.GrokPlan = snapshotString(snap["plan"])
	row.GrokWeeklyUpdatedAt = snapshotString(snap["weekly_updated_at"])
	row.GrokUpdatedAt = snapshotString(snap["updated_at"])
	row.GrokFetchedAt = snapshotString(snap["fetched_at"])
}

func snapshotString(value any) *string {
	if value == nil {
		return nil
	}
	switch typed := value.(type) {
	case string:
		if typed == "" {
			return nil
		}
		return &typed
	case float64:
		text := strconv.FormatFloat(typed, 'f', -1, 64)
		return &text
	default:
		return nil
	}
}

// CardsForUser returns one Codex result and one Grok result. Several parent
// accounts of the same platform are added into that one person total. A parent
// that is no longer scheduled still counts while its official window is open.
// It may refresh a stale Grok billing snapshot. Student page reads use
// CardsForUserStored so a dashboard refresh does not probe upstream.
func (s *OfficialPointService) CardsForUser(ctx context.Context, userID int64) map[string]OfficialPointResult {
	return s.pointsForUser(ctx, userID, true)
}

// CardsForUserStored reads the durable personal ledger in production.
func (s *OfficialPointService) CardsForUserStored(ctx context.Context, userID int64) map[string]OfficialPointResult {
	if s != nil {
		if ledger, ok := s.store.(CodingQuotaLedger); ok {
			now := time.Now().UTC()
			if s.now != nil {
				now = s.now()
			}
			cards := map[string]OfficialPointResult{
				PurposeOpenAI: {Blocked: true, DataStatus: "error"}, PurposeGrok: {Blocked: true, DataStatus: "error"},
			}
			readings, err := ledger.ReadCodingQuotas(ctx, []int64{userID}, now)
			if err != nil {
				return cards
			}
			budget := s.Budget(ctx)
			for _, reading := range readings {
				cards[reading.Purpose] = codingQuotaResult(reading, budget.ForPurpose(reading.Purpose))
			}
			s.applyCodingActivitiesForReadings(ctx, userID, now, cards, readings)
			return cards
		}
	}
	cards := s.pointsForUser(ctx, userID, false)
	if s != nil {
		s.applyCodingActivities(ctx, userID, time.Now().UTC(), cards)
	}
	return cards
}

// Blocks reports whether this person's summed Codex or Grok points have reached
// the per-person cap. A ledger failure blocks instead of granting free usage.
func (s *OfficialPointService) Blocks(ctx context.Context, userID int64, purpose string) (blocked bool, checked bool) {
	if s == nil || s.store == nil {
		return false, false
	}
	if purpose != PurposeOpenAI && purpose != PurposeGrok {
		return false, false
	}
	card := s.pointsForUser(ctx, userID, false)[purpose]
	if card.DataStatus == "error" && CodingControlMode(purpose) == "enforce" {
		return false, true
	} // Selected-parent admission returns the authoritative 503.
	return card.Blocked, true
}

func (s *OfficialPointService) pointsForUser(ctx context.Context, userID int64, probe bool) map[string]OfficialPointResult {
	out := map[string]OfficialPointResult{}
	if s == nil || s.store == nil {
		return out
	}
	now := time.Now()
	if s.now != nil {
		now = s.now()
	}
	if ledger, ok := s.store.(CodingQuotaLedger); ok {
		for _, purpose := range []string{PurposeOpenAI, PurposeGrok} {
			out[purpose] = OfficialPointResult{Blocked: true, DataStatus: "error"}
		}
		readings, err := ledger.ReadCodingQuotas(ctx, []int64{userID}, now)
		if err != nil {
			return out
		}
		budget := s.Budget(ctx)
		for _, reading := range readings {
			out[reading.Purpose] = codingQuotaResult(reading, budget.ForPurpose(reading.Purpose))
		}
		return out
	}
	rows, err := s.store.ListOfficialParents(ctx)
	if err != nil {
		return out
	}
	if probe {
		rows = s.PrepareParents(ctx, rows)
	}
	budget := s.Budget(ctx)
	windows := make([]UsageMonitorWindowQuery, 0, len(rows))
	meta := make([]snapshotParent, 0, len(rows))
	for _, row := range rows {
		decision, start, ok := snapshotWindow(row, now)
		if !ok {
			meta = append(meta, snapshotParent{row: row})
			continue
		}
		windows = append(windows, UsageMonitorWindowQuery{ParentID: row.ID, Reset: decision.Point.Reset, Start: start})
		meta = append(meta, snapshotParent{row: row, decision: decision, start: start, ok: true})
	}
	costs, err := s.store.SumOfficialWindowCost(ctx, userID, windows, officialWindowResetGap)
	if err != nil {
		return out
	}
	byParent := map[int64]OfficialWindowCost{}
	for _, cost := range costs {
		byParent[cost.ParentID] = cost
	}
	grouped := map[string][]OfficialPointInput{}
	resets := map[string]*time.Time{}
	allResets := map[string][]time.Time{}
	for _, item := range meta {
		if !item.ok {
			continue
		}
		purpose := monitorPurpose(item.row.Platform)
		if purpose == "" {
			continue
		}
		cost := byParent[item.row.ID]
		percent := item.decision.Point.PercentText
		grouped[purpose] = append(grouped[purpose], OfficialPointInput{
			Platform: item.row.Platform,
			U:        cost.UserCost,
			S:        cost.TotalCost,
			P:        &percent,
		})
		if item.decision.Point.Reset.IsZero() {
			continue
		}
		reset := item.decision.Point.Reset
		allResets[purpose] = append(allResets[purpose], reset)
		if current, ok := resets[purpose]; !ok || current == nil || reset.Before(*current) {
			resets[purpose] = &reset
		}
	}
	for _, purpose := range []string{PurposeOpenAI, PurposeGrok} {
		card := OfficialPointsForPurpose(grouped[purpose], budget.ForPurpose(purpose), resets[purpose])
		card.OfficialResets = allResets[purpose]
		out[purpose] = card
	}
	return out
}

type snapshotParent struct {
	row      UsageMonitorParentRow
	decision windowDecision
	start    time.Time
	ok       bool
}

func snapshotWindow(row UsageMonitorParentRow, now time.Time) (windowDecision, time.Time, bool) {
	snap, periodStart, periodEnd := readParentSnapshot(row, now)
	if snap == nil {
		return windowDecision{}, time.Time{}, false
	}
	start := officialWindowStart(row.Platform, snap.Reset, periodStart, periodEnd)
	return windowDecision{HasWindow: true, From: usageMonitorPercentSnapshot, Point: *snap}, start, true
}

// ApplyMonitor overlays snapshot points onto an assembled admin view.
func (s *OfficialPointService) ApplyMonitor(ctx context.Context, view *UsageMonitorView) {
	if s == nil || view == nil {
		return
	}
	budget := DefaultOfficialPointBudget()
	if s.settings != nil {
		budget = s.settings.GetOfficialPointBudget(ctx)
	}
	if ledger, ok := s.store.(CodingQuotaLedger); ok {
		ids := make([]int64, 0, len(view.Users))
		for _, user := range view.Users {
			ids = append(ids, user.ID)
		}
		readings, err := ledger.ReadCodingQuotas(ctx, ids, view.GeneratedAt)
		byUser := map[int64]map[string]OfficialPointResult{}
		for _, reading := range readings {
			if byUser[reading.UserID] == nil {
				byUser[reading.UserID] = map[string]OfficialPointResult{}
			}
			byUser[reading.UserID][reading.Purpose] = codingQuotaResult(reading, budget.ForPurpose(reading.Purpose))
		}
		for i := range view.Parents {
			text := budget.ForPurpose(view.Parents[i].Purpose)
			if text != "" {
				view.Parents[i].PointBudget = &text
			}
		}
		summaries := map[int64]map[string]CodingReservationSummary{}
		if store, ok := s.store.(CodingSummaryStore); ok {
			var rows []CodingReservationSummary
			var summaryErr error
			if batch, ok := s.store.(CodingSummaryReadingStore); ok && err == nil {
				rows, summaryErr = batch.ReadCodingReservationSummariesForReadings(ctx, readings, view.GeneratedAt)
			} else {
				rows, summaryErr = store.ReadCodingReservationSummaries(ctx, ids, view.GeneratedAt)
			}
			if summaryErr == nil {
				for _, v := range rows {
					if summaries[v.UserID] == nil {
						summaries[v.UserID] = map[string]CodingReservationSummary{}
					}
					summaries[v.UserID][v.Purpose] = v
				}
			}
		}
		for i := range view.Users {
			for j := range view.Users[i].Wallets {
				wallet := &view.Users[i].Wallets[j]
				if wallet.Purpose != PurposeOpenAI && wallet.Purpose != PurposeGrok {
					continue
				}
				card := byUser[view.Users[i].ID][wallet.Purpose]
				if summary, ok := summaries[view.Users[i].ID][wallet.Purpose]; ok {
					wallet.CodingControl = &summary
				}
				wallet.StudentPercent, wallet.OfficialPoints = card.Percent, card.Points
				wallet.PointBudget = stringPtr(budget.ForPurpose(wallet.Purpose))
				wallet.Blocked, wallet.PendingPoints, wallet.DataStatus = card.Blocked, card.Pending, card.DataStatus
				if err != nil {
					wallet.DataStatus = "error"
					wallet.Blocked = true
				}
				wallet.ManualPoints = "0"
				wallet.Period = "week"
				if card.PeriodStart != nil {
					wallet.WindowStart = *card.PeriodStart
				}
				if card.Reset != nil {
					wallet.ResetsAt = *card.Reset
				}
				wallet.PointsObservedAt = card.UpdatedAt
			}
		}
		return
	}
	for i := range view.Parents {
		text := budget.ForPurpose(view.Parents[i].Purpose)
		if text == "" {
			continue
		}
		view.Parents[i].PointBudget = &text
	}
	for i := range view.Users {
		for j := range view.Users[i].Wallets {
			wallet := &view.Users[i].Wallets[j]
			if wallet.Purpose != PurposeOpenAI && wallet.Purpose != PurposeGrok {
				continue
			}
			result := pointsFromParents(view.Parents, view.Users[i].ID, wallet.Purpose, budget.ForPurpose(wallet.Purpose))
			wallet.StudentPercent = result.Percent
			wallet.OfficialPoints = result.Points
			wallet.PointBudget = stringPtr(budget.ForPurpose(wallet.Purpose))
			wallet.ManualPoints = "0"
		}
	}
}

func pointsFromParents(parents []UsageMonitorParent, userID int64, purpose, budget string) OfficialPointResult {
	var sum *big.Rat
	var earliest *time.Time
	for _, parent := range parents {
		if parent.Purpose != purpose || parent.OfficialUsedPercent == nil {
			continue
		}
		var share *string
		for _, row := range parent.Users {
			if row.UserID == userID {
				share = row.ObservedShare
			}
		}
		if share == nil {
			share = stringPtr("0")
		}
		part, ok := parseDecimal(*share)
		if !ok {
			continue
		}
		if sum == nil {
			sum = new(big.Rat)
		}
		sum.Add(sum, part)
		if parent.OfficialResetAt != nil && (earliest == nil || parent.OfficialResetAt.Before(*earliest)) {
			reset := *parent.OfficialResetAt
			earliest = &reset
		}
	}
	if sum == nil {
		return OfficialPointResult{}
	}
	text := formatObservedShare(sum, sum)
	limit, err := ParseOfficialPointBudget(budget)
	if err != nil {
		return OfficialPointResult{Points: &text}
	}
	percent := StudentFacingPercent(sum, limit)
	return OfficialPointResult{Points: &text, Percent: &percent, Reset: earliest, Blocked: sum.Cmp(limit) >= 0}
}

func stringPtr(value string) *string {
	return &value
}
