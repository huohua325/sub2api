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

// OfficialPointService turns official subscription points into a student percent.
// Codex is read from the stored snapshot. Grok may call ProbeBilling, which does
// not send a model request.
type OfficialPointService struct {
	settings *SettingService
	store    OfficialPointStore
	grok     *GrokQuotaService
	now      func() time.Time

	mu     sync.Mutex
	probed map[int64]time.Time
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
	return s != nil && s.settings != nil && s.settings.OfficialPointStudentPercentEnabled(ctx)
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
	now := s.now()
	if s.now == nil {
		now = time.Now()
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
func (s *OfficialPointService) CardsForUser(ctx context.Context, userID int64) map[string]OfficialPointResult {
	return s.pointsForUser(ctx, userID, true)
}

// Blocks reports whether this person's summed Codex or Grok points have reached
// the per-person cap. checked is false when the switch is off. A missing
// reading does not block. This path does not call ProbeBilling.
func (s *OfficialPointService) Blocks(ctx context.Context, userID int64, purpose string) (blocked bool, checked bool) {
	if s == nil || s.store == nil {
		return false, false
	}
	if purpose != PurposeOpenAI && purpose != PurposeGrok {
		return false, false
	}
	card := s.pointsForUser(ctx, userID, false)[purpose]
	if card.Points == nil {
		return false, true
	}
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
		if current, ok := resets[purpose]; !ok || current == nil || reset.Before(*current) {
			resets[purpose] = &reset
		}
	}
	for _, purpose := range []string{PurposeOpenAI, PurposeGrok} {
		out[purpose] = OfficialPointsForPurpose(grouped[purpose], budget.ForPurpose(purpose), resets[purpose])
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
