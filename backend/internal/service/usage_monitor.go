package service

import (
	"context"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	usageMonitorDisclaimer        = "gateway_observed_only"
	usageMonitorPercentLog        = "usage_log"
	usageMonitorPercentSnapshot   = "snapshot"
	usageMonitorOfficialNone      = "none"
	usageMonitorDiscoverWindow    = 21 * 24 * time.Hour
	usageMonitorCodexWindow       = 10080 * time.Minute
	usageMonitorGrokWindow        = 7 * 24 * time.Hour
	usageMonitorGrokPeriodMax     = 14 * 24 * time.Hour
	usageMonitorNoteNewerSnapshot = "账号上有更新的快照（时间、百分比）。本页按最近一笔带戳用量计算，刷新不会向上游拉取。"
	usageMonitorNoteResetMoved    = "官方窗口已切换，新窗口还没有带戳的网关用量"
	usageMonitorNoteNoStamp       = "本窗口还没有带戳的网关用量"
)

// UsageMonitorView is the admin usage-monitor payload inside response.Success.
type UsageMonitorView struct {
	GeneratedAt time.Time            `json:"generated_at"`
	Disclaimer  string               `json:"disclaimer"`
	Parents     []UsageMonitorParent `json:"parents"`
	Users       []UsageMonitorUser   `json:"users"`
}

// UsageMonitorParent is one non-shadow OpenAI or Grok subscription account.
type UsageMonitorParent struct {
	AccountID                int64                     `json:"account_id"`
	Name                     string                    `json:"name"`
	Platform                 string                    `json:"platform"`
	Purpose                  string                    `json:"purpose"`
	OfficialUsedPercent      *string                   `json:"official_used_percent"`
	OfficialRemainingPercent *string                   `json:"official_remaining_percent"`
	OfficialResetAt          *time.Time                `json:"official_reset_at"`
	OfficialObservedAt       *time.Time                `json:"official_observed_at"`
	OfficialSource           *string                   `json:"official_source"`
	PercentFrom              *string                   `json:"percent_from"`
	WindowStart              *time.Time                `json:"window_start"`
	GatewayActualCost        string                    `json:"gateway_actual_cost"`
	SnapshotNote             *string                   `json:"snapshot_note"`
	Slope                    *UsageMonitorSlope        `json:"slope"`
	Users                    []UsageMonitorParentUser  `json:"users"`
	HiddenUserActualCost     string                    `json:"hidden_user_actual_cost"`
	WalletRemainingYuan      string                    `json:"wallet_remaining_yuan"`
	PointBudget              *string                   `json:"point_budget,omitempty"`
	Oversubscribe            UsageMonitorOversubscribe `json:"oversubscribe"`
	// Inactive means new requests are not scheduled here. The current official
	// window still counts toward each person's points until that window ends.
	Inactive bool `json:"inactive,omitempty"`
}

// UsageMonitorParentUser is one live user's gateway cost inside a parent window.
type UsageMonitorParentUser struct {
	UserID            int64   `json:"user_id"`
	GatewayActualCost string  `json:"gateway_actual_cost"`
	ObservedShare     *string `json:"observed_share"`
	SlopeShare        *string `json:"slope_share"`
}

// UsageMonitorSlope is the recent official-percent slope. It is absent when
// the window has no earlier lower reading.
type UsageMonitorSlope struct {
	DeltaPoints    string    `json:"delta_points"`
	SampleReady    bool      `json:"sample_ready"`
	FromObservedAt time.Time `json:"from_observed_at"`
	ToObservedAt   time.Time `json:"to_observed_at"`
	CostBetween    string    `json:"cost_between"`
	YuanPerPoint   *string   `json:"yuan_per_point"`
}

// UsageMonitorOversubscribe is the shared-window warning. Numeric points are
// present only when the slope sample spans at least 10 points and the wallets
// would exceed the remaining official percent.
type UsageMonitorOversubscribe struct {
	Numeric                  bool    `json:"numeric"`
	WalletPoints             *string `json:"wallet_points"`
	RemainingOfficialPercent *string `json:"remaining_official_percent"`
}

// UsageMonitorUser is one non-deleted user, including administrators.
type UsageMonitorUser struct {
	ID       int64                `json:"id"`
	Email    string               `json:"email"`
	Username string               `json:"username"`
	Role     string               `json:"role"`
	Wallets  []UsageMonitorWallet `json:"wallets"`
}

// UsageMonitorWallet is one purpose ledger. Amounts are numeric text.
type UsageMonitorWallet struct {
	Purpose             string                      `json:"purpose"`
	LimitAmount         string                      `json:"limit_amount"`
	StoredUsedAmount    string                      `json:"stored_used_amount"`
	EffectiveUsedAmount string                      `json:"effective_used_amount"`
	StudentPercent      *int                        `json:"student_percent"`
	OfficialPoints      *string                     `json:"official_points,omitempty"`
	PointBudget         *string                     `json:"point_budget,omitempty"`
	ManualPoints        string                      `json:"manual_points,omitempty"`
	PointsObservedAt    *time.Time                  `json:"points_observed_at,omitempty"`
	Period              string                      `json:"period"`
	WindowStart         time.Time                   `json:"window_start"`
	ResetsAt            time.Time                   `json:"resets_at"`
	Official            *string                     `json:"official,omitempty"`
	RequestCounts       *[]UsageMonitorRequestCount `json:"request_counts,omitempty"`
	LedgerUnrolled      bool                        `json:"ledger_unrolled,omitempty"`
}

// UsageMonitorRequestCount is one platform's experiment requests this Beijing month.
type UsageMonitorRequestCount struct {
	Platform string `json:"platform"`
	Count    int64  `json:"count"`
}

// UsageMonitorParentRow is a parent account with only the jsonb keys the
// official snapshot readers need. The repository must not load credentials.
type UsageMonitorParentRow struct {
	ID                     int64
	Name                   string
	Platform               string
	CodexUsedPercent       *string
	CodexResetAt           *string
	CodexUsageUpdatedAt    *string
	CodexWindowMinutes     *string
	CodexResetAfterSeconds *string
	GrokUsagePercent       *string
	GrokPeriodStart        *string
	GrokPeriodEnd          *string
	GrokPeriodType         *string
	GrokPlan               *string
	GrokWeeklyUpdatedAt    *string
	GrokUpdatedAt          *string
	GrokFetchedAt          *string
	Schedulable            bool
}

// UsageMonitorStamp is the latest stamped usage row for one parent.
type UsageMonitorStamp struct {
	AccountID        int64
	ResetAt          time.Time
	UsedPercent      string
	RemainingPercent *string
	ObservedAt       *time.Time
	Source           string
}

// UsageMonitorWindowQuery is one parent's current official window.
type UsageMonitorWindowQuery struct {
	ParentID int64
	Reset    time.Time
	Start    time.Time
}

// OfficialWindowCost is one parent's gateway total and one user's cost.
type OfficialWindowCost struct {
	ParentID  int64
	TotalCost string
	UserCost  string
}

// UsageMonitorWindowUserCost is one user's summed actual_cost inside a window.
type UsageMonitorWindowUserCost struct {
	UserID     int64
	ActualCost string
}

// UsageMonitorSlopeRow is the lateral slope reading for one window.
type UsageMonitorSlopeRow struct {
	P1          string
	Observed1   time.Time
	P0          *string
	Observed0   *time.Time
	CostBetween *string
}

// UsageMonitorWindowUsage is the single window aggregate for one parent.
type UsageMonitorWindowUsage struct {
	ParentID int64
	Users    []UsageMonitorWindowUserCost
	Slope    *UsageMonitorSlopeRow
}

// UsageMonitorUserRow is a live user and, when HasWallet, one ledger row.
type UsageMonitorUserRow struct {
	ID          int64
	Email       string
	Username    string
	Role        string
	Purpose     string
	LimitAmount string
	UsedAmount  string
	Period      string
	WindowStart time.Time
	HasWallet   bool
}

// UsageMonitorExperimentCount is one user's experiment requests on a platform.
type UsageMonitorExperimentCount struct {
	UserID   int64
	Platform string
	Count    int64
}

// UsageMonitorStore loads the monitor without per-user usage scans.
type UsageMonitorStore interface {
	ListOfficialParents(ctx context.Context) ([]UsageMonitorParentRow, error)
	ListStampedParentIDs(ctx context.Context, since time.Time) ([]int64, error)
	ListLatestStamps(ctx context.Context, accountIDs []int64, since time.Time) ([]UsageMonitorStamp, error)
	ListWindowUsage(ctx context.Context, windows []UsageMonitorWindowQuery, resetGap time.Duration) ([]UsageMonitorWindowUsage, error)
	ListLiveUsers(ctx context.Context) ([]UsageMonitorUserRow, error)
	ListExperimentGroupIDs(ctx context.Context) ([]int64, error)
	ListExperimentRequestCounts(ctx context.Context, groupIDs []int64, start, end time.Time) ([]UsageMonitorExperimentCount, error)
	AdjustPurposeWallet(ctx context.Context, cmd PurposeWalletAdjust) (PurposeWalletAdjustResult, error)
	ListPurposeAdjustments(ctx context.Context, query PurposeAdjustmentQuery) ([]PurposeAdjustmentAudit, error)
}

// UsageMonitorService builds the admin read model from stamped usage and wallets.
type UsageMonitorService struct {
	store  UsageMonitorStore
	now    func() time.Time
	points *OfficialPointService
}

func (s *UsageMonitorService) SetOfficialPoints(points *OfficialPointService) {
	if s == nil {
		return
	}
	s.points = points
}

// NewUsageMonitorService creates the read model. It does not call upstreams.
func NewUsageMonitorService(store UsageMonitorStore) *UsageMonitorService {
	return &UsageMonitorService{store: store, now: time.Now}
}

// Read assembles the monitor. Wallet amounts stay numeric text.
func (s *UsageMonitorService) Read(ctx context.Context) (*UsageMonitorView, error) {
	if s == nil || s.store == nil {
		return nil, infraerrors.ServiceUnavailable("USAGE_MONITOR_UNAVAILABLE", "用量监控尚未就绪")
	}
	nowFn := s.now
	if nowFn == nil {
		nowFn = time.Now
	}
	now := nowFn().UTC()
	parents, err := s.store.ListOfficialParents(ctx)
	if err != nil {
		return nil, err
	}
	usePoints := s.points != nil
	if usePoints {
		parents = s.points.PrepareParents(ctx, parents)
	}
	since := now.Add(-usageMonitorDiscoverWindow)
	stampedIDs, err := s.store.ListStampedParentIDs(ctx, since)
	if err != nil {
		return nil, err
	}
	stamped := make(map[int64]struct{}, len(stampedIDs))
	for _, id := range stampedIDs {
		stamped[id] = struct{}{}
	}

	type candidate struct {
		row         UsageMonitorParentRow
		snap        *officialPoint
		periodStart *time.Time
		periodEnd   *time.Time
	}
	candidates := make([]candidate, 0, len(parents))
	stampQuery := make([]int64, 0, len(parents))
	for _, row := range parents {
		snap, periodStart, periodEnd := readParentSnapshot(row, now)
		_, hasStamp := stamped[row.ID]
		if snap == nil && !hasStamp && (periodEnd == nil || !periodEnd.After(now)) {
			continue
		}
		candidates = append(candidates, candidate{row: row, snap: snap, periodStart: periodStart, periodEnd: periodEnd})
		if hasStamp {
			stampQuery = append(stampQuery, row.ID)
		}
	}
	stamps, err := s.store.ListLatestStamps(ctx, stampQuery, since)
	if err != nil {
		return nil, err
	}
	stampByID := make(map[int64]*officialPoint, len(stamps))
	for _, stamp := range stamps {
		if point := stampPoint(stamp); point != nil {
			stampByID[stamp.AccountID] = point
		}
	}

	userRows, err := s.store.ListLiveUsers(ctx)
	if err != nil {
		return nil, err
	}
	groupIDs, err := s.store.ListExperimentGroupIDs(ctx)
	if err != nil {
		return nil, err
	}
	monthStart := ShanghaiMonthStart(now)
	monthEnd := nextShanghaiMonthStart(now)
	experimentCounts, err := s.store.ListExperimentRequestCounts(ctx, groupIDs, monthStart, monthEnd)
	if err != nil {
		return nil, err
	}

	type decided struct {
		candidate
		decision windowDecision
		start    time.Time
	}
	decisions := make([]decided, 0, len(candidates))
	windows := make([]UsageMonitorWindowQuery, 0, len(candidates))
	for _, item := range candidates {
		decision := decideOfficialWindow(now, stampByID[item.row.ID], item.snap)
		start := time.Time{}
		if usePoints {
			if snapDecision, snapStart, ok := snapshotWindow(item.row, now); ok {
				decision = snapDecision
				start = snapStart
			} else {
				decision = windowDecision{}
			}
		}
		built := decided{candidate: item, decision: decision, start: start}
		if decision.HasWindow && !usePoints {
			built.start = officialWindowStart(item.row.Platform, decision.Point.Reset, item.periodStart, item.periodEnd)
		}
		if decision.HasWindow {
			windows = append(windows, UsageMonitorWindowQuery{
				ParentID: item.row.ID,
				Reset:    decision.Point.Reset,
				Start:    built.start,
			})
		}
		decisions = append(decisions, built)
	}
	windowUsage, err := s.store.ListWindowUsage(ctx, windows, officialWindowResetGap)
	if err != nil {
		return nil, err
	}
	usageByParent := make(map[int64]UsageMonitorWindowUsage, len(windowUsage))
	for _, usage := range windowUsage {
		usageByParent[usage.ParentID] = usage
	}

	users, walletRemain, err := assembleMonitorUsers(userRows, experimentCounts, now)
	if err != nil {
		return nil, err
	}
	liveIDs := make(map[int64]struct{}, len(users))
	for _, user := range users {
		liveIDs[user.ID] = struct{}{}
	}

	view := &UsageMonitorView{
		GeneratedAt: now,
		Disclaimer:  usageMonitorDisclaimer,
		Parents:     make([]UsageMonitorParent, 0, len(decisions)),
		Users:       users,
	}
	for _, item := range decisions {
		parent, err := assembleMonitorParent(item.row, item.decision, item.start, usageByParent[item.row.ID], liveIDs, walletRemain)
		if err != nil {
			return nil, err
		}
		if parent.OfficialResetAt == nil && item.periodEnd != nil && item.periodEnd.After(now) {
			reset := item.periodEnd.UTC()
			parent.OfficialResetAt = &reset
		}
		view.Parents = append(view.Parents, parent)
	}
	sort.Slice(view.Parents, func(i, j int) bool { return view.Parents[i].AccountID < view.Parents[j].AccountID })
	sort.Slice(view.Users, func(i, j int) bool { return view.Users[i].ID < view.Users[j].ID })
	if usePoints && s.points != nil {
		s.points.ApplyMonitor(ctx, view)
	}
	return view, nil
}

func assembleMonitorParent(row UsageMonitorParentRow, decision windowDecision, start time.Time, usage UsageMonitorWindowUsage, liveIDs map[int64]struct{}, walletRemain map[string]*big.Rat) (UsageMonitorParent, error) {
	purpose := monitorPurpose(row.Platform)
	parent := UsageMonitorParent{
		AccountID:            row.ID,
		Name:                 row.Name,
		Platform:             row.Platform,
		Purpose:              purpose,
		GatewayActualCost:    "0",
		Users:                []UsageMonitorParentUser{},
		HiddenUserActualCost: "0",
		WalletRemainingYuan:  formatTrimmed(walletRemain[purpose]),
		Oversubscribe:        UsageMonitorOversubscribe{},
		Inactive:             !row.Schedulable,
	}
	if !decision.HasWindow {
		return parent, nil
	}
	percentText := strings.TrimSpace(decision.Point.PercentText)
	parent.OfficialUsedPercent = &percentText
	parent.OfficialRemainingPercent = remainingPercentText(percentText, decision.Point.Remaining)
	reset := decision.Point.Reset
	parent.OfficialResetAt = &reset
	if decision.Point.Observed != nil {
		observed := *decision.Point.Observed
		parent.OfficialObservedAt = &observed
	}
	if source := strings.TrimSpace(decision.Point.Source); source != "" {
		parent.OfficialSource = &source
	}
	from := decision.From
	parent.PercentFrom = &from
	windowStart := start
	parent.WindowStart = &windowStart

	visibleText := make([]string, 0, len(usage.Users))
	hiddenText := make([]string, 0, len(usage.Users))
	allText := make([]string, 0, len(usage.Users))
	visible := make([]UsageMonitorWindowUserCost, 0, len(usage.Users))
	for _, userCost := range usage.Users {
		allText = append(allText, userCost.ActualCost)
		if _, ok := liveIDs[userCost.UserID]; ok {
			visible = append(visible, userCost)
			visibleText = append(visibleText, userCost.ActualCost)
			continue
		}
		hiddenText = append(hiddenText, userCost.ActualCost)
	}
	totalText, total, err := sumCostTexts(allText)
	if err != nil {
		return UsageMonitorParent{}, err
	}
	hiddenCost, _, err := sumCostTexts(hiddenText)
	if err != nil {
		return UsageMonitorParent{}, err
	}
	parent.GatewayActualCost = totalText
	parent.HiddenUserActualCost = hiddenCost
	parent.SnapshotNote = usageWindowNote(decision, total)

	var yuan *big.Rat
	if slope, slopeYuan := slopeFromRow(usage.Slope); slope != nil {
		parent.Slope = slope
		yuan = slopeYuan
	}
	percent, _ := parseDecimal(percentText)
	sort.Slice(visible, func(i, j int) bool { return visible[i].UserID < visible[j].UserID })
	for _, userCost := range visible {
		u, ok := parseDecimal(userCost.ActualCost)
		if !ok {
			return UsageMonitorParent{}, fmt.Errorf("usage monitor cost %q", userCost.ActualCost)
		}
		parent.Users = append(parent.Users, UsageMonitorParentUser{
			UserID:            userCost.UserID,
			GatewayActualCost: strings.TrimSpace(userCost.ActualCost),
			ObservedShare:     observedShare(u, total, percent),
			SlopeShare:        slopeShareText(u, yuan),
		})
	}
	var remainingOfficial *big.Rat
	if parent.OfficialRemainingPercent != nil {
		if parsed, ok := parseDecimal(*parent.OfficialRemainingPercent); ok {
			remainingOfficial = parsed
		}
	}
	ready := parent.Slope != nil && parent.Slope.SampleReady
	numeric, points := oversubscribePoints(walletRemain[purpose], yuan, remainingOfficial, ready)
	parent.Oversubscribe = UsageMonitorOversubscribe{
		Numeric:                  numeric,
		WalletPoints:             points,
		RemainingOfficialPercent: parent.OfficialRemainingPercent,
	}
	return parent, nil
}

func assembleMonitorUsers(rows []UsageMonitorUserRow, counts []UsageMonitorExperimentCount, now time.Time) ([]UsageMonitorUser, map[string]*big.Rat, error) {
	countsByUser := make(map[int64][]UsageMonitorRequestCount, len(counts))
	for _, count := range counts {
		countsByUser[count.UserID] = append(countsByUser[count.UserID], UsageMonitorRequestCount{
			Platform: count.Platform,
			Count:    count.Count,
		})
	}
	order := make([]int64, 0, len(rows))
	byID := map[int64]*struct {
		user    UsageMonitorUser
		wallets map[string]UsageMonitorWallet
	}{}
	remain := map[string]*big.Rat{
		PurposeOpenAI:     new(big.Rat),
		PurposeGrok:       new(big.Rat),
		PurposeExperiment: new(big.Rat),
	}
	for _, row := range rows {
		acc, ok := byID[row.ID]
		if !ok {
			acc = &struct {
				user    UsageMonitorUser
				wallets map[string]UsageMonitorWallet
			}{
				user: UsageMonitorUser{
					ID: row.ID, Email: row.Email, Username: row.Username, Role: row.Role,
					Wallets: []UsageMonitorWallet{},
				},
				wallets: map[string]UsageMonitorWallet{},
			}
			byID[row.ID] = acc
			order = append(order, row.ID)
		}
		if !row.HasWallet {
			continue
		}
		if _, exists := acc.wallets[row.Purpose]; exists {
			continue
		}
		wallet, walletRemain, err := monitorWallet(row, countsByUser[row.ID], now)
		if err != nil {
			return nil, nil, err
		}
		acc.wallets[wallet.Purpose] = wallet
		if bucket, ok := remain[wallet.Purpose]; ok && walletRemain != nil {
			bucket.Add(bucket, walletRemain)
		}
	}
	users := make([]UsageMonitorUser, 0, len(order))
	for _, id := range order {
		acc := byID[id]
		for _, purpose := range []string{PurposeOpenAI, PurposeGrok, PurposeExperiment} {
			wallet, ok := acc.wallets[purpose]
			if !ok {
				continue
			}
			acc.user.Wallets = append(acc.user.Wallets, wallet)
		}
		users = append(users, acc.user)
	}
	return users, remain, nil
}

func monitorWallet(row UsageMonitorUserRow, counts []UsageMonitorRequestCount, now time.Time) (UsageMonitorWallet, *big.Rat, error) {
	period := AllowancePeriod(row.Purpose, row.Period)
	effective, resets, expired := effectivePurposeUsed(period, row.WindowStart, row.UsedAmount, now)
	used, ok := parseDecimal(effective)
	if !ok {
		return UsageMonitorWallet{}, nil, fmt.Errorf("usage monitor used amount %q", effective)
	}
	limit, ok := parseDecimal(row.LimitAmount)
	if !ok {
		return UsageMonitorWallet{}, nil, fmt.Errorf("usage monitor limit amount %q", row.LimitAmount)
	}
	percent := StudentFacingPercent(used, limit)
	wallet := UsageMonitorWallet{
		Purpose:             row.Purpose,
		LimitAmount:         row.LimitAmount,
		StoredUsedAmount:    row.UsedAmount,
		EffectiveUsedAmount: effective,
		StudentPercent:      &percent,
		Period:              period,
		WindowStart:         row.WindowStart,
		ResetsAt:            resets,
		LedgerUnrolled:      expired && effective != row.UsedAmount,
	}
	if row.Purpose == PurposeExperiment {
		none := usageMonitorOfficialNone
		wallet.Official = &none
		copied := make([]UsageMonitorRequestCount, 0, len(counts))
		copied = append(copied, counts...)
		wallet.RequestCounts = &copied
	}
	remain := new(big.Rat).Sub(limit, used)
	if remain.Sign() < 0 {
		remain = new(big.Rat)
	}
	return wallet, remain, nil
}

type officialPoint struct {
	Reset       time.Time
	PercentText string
	Observed    *time.Time
	Source      string
	Remaining   *string
}

type windowDecision struct {
	HasWindow   bool
	From        string
	Point       officialPoint
	IgnoredSnap *officialPoint
	NoLog       bool
	ResetMoved  bool
}

func decideOfficialWindow(now time.Time, logPoint, snapPoint *officialPoint) windowDecision {
	logOpen := openOfficialPoint(logPoint, now)
	snapOpen := openOfficialPoint(snapPoint, now)
	if logOpen && (!snapOpen || !officialResetMoved(snapPoint.Reset, logPoint.Reset)) {
		decision := windowDecision{HasWindow: true, From: usageMonitorPercentLog, Point: *logPoint}
		if snapOpen {
			copied := *snapPoint
			decision.IgnoredSnap = &copied
		}
		return decision
	}
	if snapOpen {
		decision := windowDecision{HasWindow: true, From: usageMonitorPercentSnapshot, Point: *snapPoint}
		if !pointHasPercent(logPoint) {
			decision.NoLog = true
		} else if officialResetMoved(snapPoint.Reset, logPoint.Reset) {
			decision.ResetMoved = true
		}
		return decision
	}
	return windowDecision{}
}

func openOfficialPoint(point *officialPoint, now time.Time) bool {
	return pointHasPercent(point) && point.Reset.After(now)
}

func pointHasPercent(point *officialPoint) bool {
	if point == nil {
		return false
	}
	_, ok := parseDecimal(point.PercentText)
	return ok
}

func usageWindowNote(decision windowDecision, gatewayCost *big.Rat) *string {
	if decision.From == usageMonitorPercentLog && decision.IgnoredSnap != nil {
		text := usageMonitorNoteNewerSnapshot
		return &text
	}
	if decision.From == usageMonitorPercentSnapshot && (gatewayCost == nil || gatewayCost.Sign() <= 0) {
		if decision.ResetMoved {
			text := usageMonitorNoteResetMoved
			return &text
		}
		text := usageMonitorNoteNoStamp
		return &text
	}
	return nil
}

func sameOfficialWindow(a, b time.Time) bool {
	return !officialResetMoved(a, b)
}

// officialUsageInWindow reports whether one usage row belongs to (parent, R).
// windowReset is not an exclusion: the first charge of a window is still inside the ball.
// Rows are not chained across balls, and a null stamp cannot be filled from account_id.
func officialUsageInWindow(parentID int64, accountID *int64, resetAt *time.Time, createdAt, windowStart, windowEnd time.Time, windowReset bool) bool {
	_ = windowReset
	if accountID == nil || *accountID != parentID || resetAt == nil {
		return false
	}
	if officialResetMoved(*resetAt, windowEnd) {
		return false
	}
	if createdAt.Before(windowStart) || !createdAt.Before(windowEnd) {
		return false
	}
	return true
}

func officialWindowStart(platform string, reset time.Time, periodStart, periodEnd *time.Time) time.Time {
	if strings.EqualFold(strings.TrimSpace(platform), PlatformGrok) {
		if periodStart != nil && periodEnd != nil &&
			!officialResetMoved(*periodEnd, reset) &&
			periodStart.Before(reset) &&
			reset.Sub(*periodStart) <= usageMonitorGrokPeriodMax {
			return *periodStart
		}
		return reset.Add(-usageMonitorGrokWindow)
	}
	// The stamped header is the 7-day window. A missing or different
	// codex_7d_window_minutes value does not select the 5-hour window.
	return reset.Add(-usageMonitorCodexWindow)
}

func readParentSnapshot(row UsageMonitorParentRow, now time.Time) (snap *officialPoint, periodStart, periodEnd *time.Time) {
	if strings.EqualFold(strings.TrimSpace(row.Platform), PlatformGrok) {
		periodStart = parseOptionalTime(row.GrokPeriodStart)
		periodEnd = parseOptionalTime(row.GrokPeriodEnd)
		percent, reset, observed, source, _ := grokOfficial(grokSnapshotExtra(row), now)
		if source == "grok_period_end_expired" || percent == nil || reset == nil || !reset.After(now) {
			return nil, periodStart, periodEnd
		}
		return snapshotPoint(*percent, *reset, observed, source), periodStart, periodEnd
	}
	percent, reset, observed, source := codexOfficial(codexSnapshotExtra(row), now)
	if source == "codex_7d_expired" || percent == nil || reset == nil || !reset.After(now) {
		return nil, nil, nil
	}
	return snapshotPoint(*percent, *reset, observed, source), nil, nil
}

func snapshotPoint(percent float64, reset time.Time, observed *time.Time, source string) *officialPoint {
	point := &officialPoint{
		Reset:       reset.UTC(),
		PercentText: strconv.FormatFloat(percent, 'f', 4, 64),
		Source:      source,
	}
	if observed != nil {
		copied := observed.UTC()
		point.Observed = &copied
	}
	return point
}

func stampPoint(stamp UsageMonitorStamp) *officialPoint {
	if _, ok := parseDecimal(stamp.UsedPercent); !ok || stamp.ResetAt.IsZero() {
		return nil
	}
	point := &officialPoint{
		Reset:       stamp.ResetAt.UTC(),
		PercentText: strings.TrimSpace(stamp.UsedPercent),
		Source:      stamp.Source,
	}
	if stamp.ObservedAt != nil {
		copied := stamp.ObservedAt.UTC()
		point.Observed = &copied
	}
	if stamp.RemainingPercent != nil && strings.TrimSpace(*stamp.RemainingPercent) != "" {
		text := strings.TrimSpace(*stamp.RemainingPercent)
		point.Remaining = &text
	}
	return point
}

func codexSnapshotExtra(row UsageMonitorParentRow) map[string]any {
	extra := map[string]any{}
	putSnapshot(extra, "codex_7d_used_percent", row.CodexUsedPercent)
	putSnapshot(extra, "codex_7d_reset_at", row.CodexResetAt)
	putSnapshot(extra, "codex_usage_updated_at", row.CodexUsageUpdatedAt)
	putSnapshot(extra, "codex_7d_window_minutes", row.CodexWindowMinutes)
	putSnapshot(extra, "codex_7d_reset_after_seconds", row.CodexResetAfterSeconds)
	return extra
}

func grokSnapshotExtra(row UsageMonitorParentRow) map[string]any {
	snap := map[string]any{}
	putSnapshot(snap, "usage_percent", row.GrokUsagePercent)
	putSnapshot(snap, "period_start", row.GrokPeriodStart)
	putSnapshot(snap, "period_end", row.GrokPeriodEnd)
	putSnapshot(snap, "period_type", row.GrokPeriodType)
	putSnapshot(snap, "plan", row.GrokPlan)
	putSnapshot(snap, "weekly_updated_at", row.GrokWeeklyUpdatedAt)
	putSnapshot(snap, "updated_at", row.GrokUpdatedAt)
	putSnapshot(snap, "fetched_at", row.GrokFetchedAt)
	if len(snap) == 0 {
		return map[string]any{}
	}
	return map[string]any{"grok_billing_snapshot": snap}
}

func putSnapshot(dst map[string]any, key string, value *string) {
	if value == nil {
		return
	}
	text := strings.TrimSpace(*value)
	if text == "" {
		return
	}
	dst[key] = text
}

func parseOptionalTime(value *string) *time.Time {
	if value == nil {
		return nil
	}
	parsed, err := parseTime(strings.TrimSpace(*value))
	if err != nil {
		return nil
	}
	return &parsed
}

func monitorPurpose(platform string) string {
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case PlatformOpenAI:
		return PurposeOpenAI
	case PlatformGrok:
		return PurposeGrok
	default:
		return ""
	}
}

func observedShare(u, s, p *big.Rat) *string {
	if p == nil || s == nil || s.Sign() <= 0 {
		return nil
	}
	if u == nil {
		u = new(big.Rat)
	}
	quotient := new(big.Rat).Mul(new(big.Rat).Quo(u, s), p)
	text := formatObservedShare(u, quotient)
	return &text
}

func formatObservedShare(u, quotient *big.Rat) string {
	if u == nil || u.Sign() == 0 {
		return "0"
	}
	return formatFixedHalfAway(quotient, 4)
}

func slopeShareText(u, yuan *big.Rat) *string {
	if yuan == nil || yuan.Sign() == 0 || u == nil {
		return nil
	}
	text := formatFixedHalfAway(new(big.Rat).Quo(u, yuan), 4)
	return &text
}

type usageSlope struct {
	OK           bool
	SampleReady  bool
	DeltaPoints  string
	YuanPerPoint *string
	Yuan         *big.Rat
	Share        *string
}

func slopeShare(p1, p0, costBetween, u *big.Rat) usageSlope {
	if p1 == nil || p0 == nil {
		return usageSlope{}
	}
	delta := new(big.Rat).Sub(p1, p0)
	if delta.Sign() <= 0 {
		return usageSlope{}
	}
	out := usageSlope{
		OK:          true,
		SampleReady: delta.Cmp(big.NewRat(10, 1)) >= 0,
		DeltaPoints: formatFixedHalfAway(delta, 4),
	}
	if costBetween == nil || costBetween.Sign() <= 0 {
		return out
	}
	yuan := new(big.Rat).Quo(costBetween, delta)
	text := formatFixedHalfAway(yuan, 8)
	out.Yuan = yuan
	out.YuanPerPoint = &text
	out.Share = slopeShareText(u, yuan)
	return out
}

func slopeFromRow(row *UsageMonitorSlopeRow) (*UsageMonitorSlope, *big.Rat) {
	if row == nil || row.P0 == nil || row.Observed0 == nil {
		return nil, nil
	}
	p1, ok1 := parseDecimal(row.P1)
	p0, ok0 := parseDecimal(*row.P0)
	if !ok1 || !ok0 {
		return nil, nil
	}
	var cost *big.Rat
	costText := "0"
	if row.CostBetween != nil && strings.TrimSpace(*row.CostBetween) != "" {
		costText = strings.TrimSpace(*row.CostBetween)
		parsed, ok := parseDecimal(costText)
		if !ok {
			return nil, nil
		}
		cost = parsed
	}
	slope := slopeShare(p1, p0, cost, nil)
	if !slope.OK {
		return nil, nil
	}
	return &UsageMonitorSlope{
		DeltaPoints:    slope.DeltaPoints,
		SampleReady:    slope.SampleReady,
		FromObservedAt: row.Observed0.UTC(),
		ToObservedAt:   row.Observed1.UTC(),
		CostBetween:    costText,
		YuanPerPoint:   slope.YuanPerPoint,
	}, slope.Yuan
}

func oversubscribePoints(remainWallet, yuan, remainOfficial *big.Rat, sampleReady bool) (bool, *string) {
	if remainWallet == nil || remainWallet.Sign() <= 0 || !sampleReady || yuan == nil || yuan.Sign() <= 0 || remainOfficial == nil {
		return false, nil
	}
	points := new(big.Rat).Quo(remainWallet, yuan)
	if points.Cmp(remainOfficial) <= 0 {
		return false, nil
	}
	text := formatFixedHalfAway(points, 4)
	return true, &text
}

func effectivePurposeUsed(period string, windowStart time.Time, stored string, now time.Time) (string, time.Time, bool) {
	if PurposeWindowExpired(period, windowStart, now) {
		renewed := RenewedPurposeWindow(period, windowStart, now)
		return "0", PurposeResetsAt(period, renewed), true
	}
	return stored, PurposeResetsAt(period, windowStart), false
}

// StudentFacingPercent matches the student card: positive half-up, capped at 100.
// A non-positive limit is 0 so an empty wallet is not shown as fully used.
func StudentFacingPercent(used, limit *big.Rat) int {
	if limit.Sign() <= 0 {
		return 0
	}
	raw := new(big.Rat).Quo(used, limit)
	raw.Mul(raw, big.NewRat(100, 1))
	half := big.NewRat(1, 2)
	if raw.Sign() < 0 {
		return 0
	}
	bumped := new(big.Rat).Add(raw, half)
	whole := new(big.Int).Quo(bumped.Num(), bumped.Denom())
	if whole.Cmp(big.NewInt(100)) > 0 {
		return 100
	}
	return int(whole.Int64())
}

func remainingPercentText(percentText string, stored *string) *string {
	if stored != nil && strings.TrimSpace(*stored) != "" {
		text := strings.TrimSpace(*stored)
		return &text
	}
	percent, ok := parseDecimal(percentText)
	if !ok || percent.Sign() < 0 || percent.Cmp(big.NewRat(100, 1)) > 0 {
		return nil
	}
	text := new(big.Rat).Sub(big.NewRat(100, 1), percent).FloatString(decimalScale(percentText))
	return &text
}

func sumCostTexts(parts []string) (string, *big.Rat, error) {
	total := new(big.Rat)
	if len(parts) == 0 {
		return "0", total, nil
	}
	for _, part := range parts {
		parsed, ok := parseDecimal(part)
		if !ok {
			return "", nil, fmt.Errorf("usage monitor cost %q", part)
		}
		total.Add(total, parsed)
	}
	if len(parts) == 1 {
		return strings.TrimSpace(parts[0]), total, nil
	}
	return formatTrimmed(total), total, nil
}

func parseDecimal(text string) (*big.Rat, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, false
	}
	return new(big.Rat).SetString(text)
}

func roundHalfAway(value *big.Rat, places int) *big.Rat {
	if value == nil {
		return new(big.Rat)
	}
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(places)), nil)
	scaled := new(big.Rat).Mul(value, new(big.Rat).SetInt(scale))
	half := big.NewRat(1, 2)
	if scaled.Sign() < 0 {
		scaled.Sub(scaled, half)
	} else {
		scaled.Add(scaled, half)
	}
	whole := new(big.Int).Quo(scaled.Num(), scaled.Denom())
	return new(big.Rat).SetFrac(whole, scale)
}

func formatFixedHalfAway(value *big.Rat, places int) string {
	return roundHalfAway(value, places).FloatString(places)
}

func formatTrimmed(value *big.Rat) string {
	if value == nil || value.Sign() == 0 {
		return "0"
	}
	text := value.FloatString(12)
	if strings.Contains(text, ".") {
		text = strings.TrimRight(text, "0")
		text = strings.TrimRight(text, ".")
	}
	if text == "-0" || text == "" {
		return "0"
	}
	return text
}

func decimalScale(text string) int {
	text = strings.TrimSpace(text)
	dot := strings.IndexByte(text, '.')
	if dot < 0 {
		return 0
	}
	return len(text) - dot - 1
}
