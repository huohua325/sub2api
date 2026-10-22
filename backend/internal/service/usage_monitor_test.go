package service

import (
	"context"
	"encoding/json"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUsageMonitorWindowMembership(t *testing.T) {
	end := time.Date(2026, 10, 26, 2, 0, 0, 0, time.UTC)
	start := end.Add(-usageMonitorCodexWindow)
	parent := int64(42)
	inside := end.Add(-time.Hour)

	require.True(t, sameOfficialWindow(end, end.Add(time.Second)))
	require.True(t, sameOfficialWindow(end, end.Add(officialWindowResetGap)))
	require.False(t, sameOfficialWindow(end, end.Add(6*time.Minute)))
	require.False(t, sameOfficialWindow(end, end.Add(officialWindowResetGap+time.Second)))

	require.True(t, officialUsageInWindow(parent, int64Ptr(parent), monitorTime(end.Add(time.Second)), inside, start, end, true))
	require.True(t, officialUsageInWindow(parent, int64Ptr(parent), monitorTime(end.Add(officialWindowResetGap)), inside, start, end, false))
	require.False(t, officialUsageInWindow(parent, int64Ptr(parent), monitorTime(end.Add(6*time.Minute)), inside, start, end, true))
	require.False(t, officialUsageInWindow(parent, int64Ptr(parent), monitorTime(end.Add(4*time.Minute).Add(4*time.Minute)), inside, start, end, false))
	require.True(t, officialUsageInWindow(parent, int64Ptr(parent), monitorTime(end.Add(4*time.Minute)), inside, start, end, false))

	require.False(t, officialUsageInWindow(parent, nil, monitorTime(end), inside, start, end, false))
	require.False(t, officialUsageInWindow(parent, int64Ptr(parent), nil, inside, start, end, false))
	require.False(t, officialUsageInWindow(parent, int64Ptr(99), monitorTime(end), inside, start, end, false))
	require.True(t, officialUsageInWindow(parent, int64Ptr(parent), monitorTime(end), start, start, end, false))
	require.False(t, officialUsageInWindow(parent, int64Ptr(parent), monitorTime(end), end, start, end, false))
	require.False(t, officialUsageInWindow(parent, int64Ptr(parent), monitorTime(end), start.Add(-time.Second), start, end, false))
}

func TestUsageMonitorChoosesOpenLogWithoutSnapshot(t *testing.T) {
	now := time.Date(2026, 10, 21, 2, 0, 0, 0, time.UTC)
	logPoint := &officialPoint{Reset: now.Add(24 * time.Hour), PercentText: "41.2500"}
	decision := decideOfficialWindow(now, logPoint, nil)
	require.True(t, decision.HasWindow)
	require.Equal(t, usageMonitorPercentLog, decision.From)
	require.Equal(t, "41.2500", decision.Point.PercentText)
	require.Nil(t, decision.IgnoredSnap)
	require.Nil(t, usageWindowNote(decision, big.NewRat(1, 1)))

	near := &officialPoint{Reset: logPoint.Reset.Add(time.Second), PercentText: "50"}
	same := decideOfficialWindow(now, logPoint, near)
	require.Equal(t, usageMonitorPercentLog, same.From)
	require.NotNil(t, same.IgnoredSnap)
	require.Equal(t, usageMonitorNoteNewerSnapshot, *usageWindowNote(same, big.NewRat(1, 1)))

	moved := &officialPoint{Reset: logPoint.Reset.Add(6 * time.Minute), PercentText: "12"}
	switched := decideOfficialWindow(now, logPoint, moved)
	require.Equal(t, usageMonitorPercentSnapshot, switched.From)
	require.Equal(t, "12", switched.Point.PercentText)
	require.True(t, switched.ResetMoved)
	require.Equal(t, usageMonitorNoteResetMoved, *usageWindowNote(switched, new(big.Rat)))
	require.Nil(t, usageWindowNote(switched, big.NewRat(1, 1)))

	onlySnap := decideOfficialWindow(now, nil, moved)
	require.Equal(t, usageMonitorPercentSnapshot, onlySnap.From)
	require.True(t, onlySnap.NoLog)
	require.Equal(t, usageMonitorNoteNoStamp, *usageWindowNote(onlySnap, new(big.Rat)))

	expired := &officialPoint{Reset: now.Add(-time.Minute), PercentText: "41.2500"}
	none := decideOfficialWindow(now, expired, nil)
	require.False(t, none.HasWindow)
}

func TestUsageMonitorObservedShare(t *testing.T) {
	u := mustRat("0.0003905")
	s := mustRat("3.125")
	p := mustRat("41.25")
	require.Equal(t, "0.0052", *observedShare(u, s, p))
	require.Equal(t, "0", *observedShare(new(big.Rat), s, p))
	require.Equal(t, "0.0000", *observedShare(big.NewRat(1, 1), big.NewRat(100000, 1), big.NewRat(1, 1)))
	require.Equal(t, "0.0001", *observedShare(big.NewRat(1, 1), big.NewRat(20000, 1), big.NewRat(1, 1)))
	require.Nil(t, observedShare(u, new(big.Rat), p))
	require.Nil(t, observedShare(u, mustRat("-1"), p))
	require.Nil(t, observedShare(u, s, nil))
}

func TestUsageMonitorSlopeShareKeepsSmallSample(t *testing.T) {
	small := slopeShare(big.NewRat(14, 1), big.NewRat(5, 1), big.NewRat(9, 1), big.NewRat(2, 1))
	require.True(t, small.OK)
	require.False(t, small.SampleReady)
	require.Equal(t, "9.0000", small.DeltaPoints)
	require.Equal(t, "1.00000000", *small.YuanPerPoint)
	require.Equal(t, "2.0000", *small.Share)

	ready := slopeShare(big.NewRat(15, 1), big.NewRat(5, 1), big.NewRat(10, 1), big.NewRat(1, 1))
	require.True(t, ready.SampleReady)
	require.Equal(t, "1.0000", *ready.Share)

	flat := slopeShare(big.NewRat(5, 1), big.NewRat(5, 1), big.NewRat(1, 1), big.NewRat(1, 1))
	require.False(t, flat.OK)
	require.Nil(t, flat.Share)
	missing := slopeShare(big.NewRat(5, 1), nil, big.NewRat(1, 1), big.NewRat(1, 1))
	require.False(t, missing.OK)
	require.Nil(t, missing.Share)

	numeric, points := oversubscribePoints(big.NewRat(200, 1), small.Yuan, big.NewRat(50, 1), small.SampleReady)
	require.False(t, numeric)
	require.Nil(t, points)
	numeric, points = oversubscribePoints(big.NewRat(200, 1), ready.Yuan, big.NewRat(50, 1), ready.SampleReady)
	require.True(t, numeric)
	require.Equal(t, "200.0000", *points)
	numeric, points = oversubscribePoints(new(big.Rat), ready.Yuan, big.NewRat(50, 1), true)
	require.False(t, numeric)
	require.Nil(t, points)
}

func TestUsageMonitorStudentFacingPercent(t *testing.T) {
	require.Equal(t, 0, StudentFacingPercent(mustRat("0.49"), mustRat("100")))
	require.Equal(t, 1, StudentFacingPercent(mustRat("0.5"), mustRat("100")))
	require.Equal(t, 0, StudentFacingPercent(mustRat("1"), new(big.Rat)))
	require.Equal(t, 0, StudentFacingPercent(mustRat("1"), mustRat("-1")))
	require.Equal(t, 0, StudentFacingPercent(mustRat("-1"), mustRat("100")))
	require.Equal(t, 100, StudentFacingPercent(mustRat("150"), mustRat("100")))
}

func TestUsageMonitorExpiredWindowEffectiveUsedIsZero(t *testing.T) {
	now := time.Date(2026, 10, 21, 2, 0, 0, 0, time.UTC)
	start := now.Add(-8 * 24 * time.Hour)
	text, resets, expired := effectivePurposeUsed("week", start, "4.00", now)
	require.True(t, expired)
	require.Equal(t, "0", text)
	require.True(t, resets.Equal(PurposeResetsAt("week", RenewedPurposeWindow("week", start, now))))

	text, resets, expired = effectivePurposeUsed("week", now.Add(-time.Hour), "0.0003905", now)
	require.False(t, expired)
	require.Equal(t, "0.0003905", text)
	require.True(t, resets.Equal(PurposeResetsAt("week", now.Add(-time.Hour))))
}

func TestUsageMonitorOfficialWindowStart(t *testing.T) {
	reset := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
	periodStart := reset.Add(-10 * 24 * time.Hour)
	periodEnd := reset.Add(time.Minute)
	require.True(t, officialWindowStart(PlatformOpenAI, reset, &periodStart, &periodEnd).Equal(reset.Add(-usageMonitorCodexWindow)))

	require.True(t, officialWindowStart(PlatformGrok, reset, &periodStart, &periodEnd).Equal(periodStart))
	farEnd := reset.Add(6 * time.Minute)
	require.True(t, officialWindowStart(PlatformGrok, reset, &periodStart, &farEnd).Equal(reset.Add(-usageMonitorGrokWindow)))
	longStart := reset.Add(-15 * 24 * time.Hour)
	require.True(t, officialWindowStart(PlatformGrok, reset, &longStart, &periodEnd).Equal(reset.Add(-usageMonitorGrokWindow)))
	require.True(t, officialWindowStart(PlatformGrok, reset, &reset, &periodEnd).Equal(reset.Add(-usageMonitorGrokWindow)))
	require.True(t, officialWindowStart(PlatformGrok, reset, nil, nil).Equal(reset.Add(-usageMonitorGrokWindow)))
}

func TestUsageMonitorReadOpenLog(t *testing.T) {
	now := time.Date(2026, 10, 21, 2, 0, 0, 0, time.UTC)
	store := &fakeUsageMonitorStore{
		t:           t,
		now:         now,
		openaiReset: now.Add(5 * 24 * time.Hour),
		grokReset:   now.Add(3 * 24 * time.Hour),
	}
	svc := NewUsageMonitorService(store)
	svc.now = func() time.Time { return now }

	view, err := svc.Read(context.Background())
	require.NoError(t, err)
	require.Equal(t, now, view.GeneratedAt)
	require.Equal(t, usageMonitorDisclaimer, view.Disclaimer)
	require.Equal(t, now.Add(-usageMonitorDiscoverWindow), store.since)
	require.True(t, store.monthStart.Equal(ShanghaiMonthStart(now)))
	require.True(t, store.monthEnd.Equal(nextShanghaiMonthStart(now)))
	require.Equal(t, officialWindowResetGap, store.gap)

	require.Len(t, view.Parents, 2)
	openai := view.Parents[0]
	require.Equal(t, int64(42), openai.AccountID)
	require.Equal(t, PlatformOpenAI, openai.Platform)
	require.Equal(t, PurposeOpenAI, openai.Purpose)
	require.Equal(t, "41.2500", *openai.OfficialUsedPercent)
	require.Equal(t, "58.0000", *openai.OfficialRemainingPercent)
	require.Equal(t, usageMonitorPercentLog, *openai.PercentFrom)
	require.Nil(t, openai.SnapshotNote)
	require.True(t, openai.WindowStart.Equal(store.openaiReset.Add(-usageMonitorCodexWindow)))
	require.Equal(t, "3.125", openai.GatewayActualCost)
	require.Equal(t, "3.1246095", openai.HiddenUserActualCost)
	require.Equal(t, "199.5", openai.WalletRemainingYuan)
	require.Len(t, openai.Users, 1)
	require.Equal(t, int64(12), openai.Users[0].UserID)
	require.Equal(t, "0.0003905", openai.Users[0].GatewayActualCost)
	require.Equal(t, "0.0052", *openai.Users[0].ObservedShare)
	require.Equal(t, "0.0022", *openai.Users[0].SlopeShare)
	require.NotNil(t, openai.Slope)
	require.False(t, openai.Slope.SampleReady)
	require.Equal(t, "4.5000", openai.Slope.DeltaPoints)
	require.Equal(t, "0.17777778", *openai.Slope.YuanPerPoint)
	require.Equal(t, "0.8000000000", openai.Slope.CostBetween)
	require.False(t, openai.Oversubscribe.Numeric)
	require.Nil(t, openai.Oversubscribe.WalletPoints)
	require.Equal(t, "58.0000", *openai.Oversubscribe.RemainingOfficialPercent)

	grok := view.Parents[1]
	require.Equal(t, int64(43), grok.AccountID)
	require.Equal(t, "10.5000", *grok.OfficialUsedPercent)
	require.Equal(t, "89.5000", *grok.OfficialRemainingPercent)
	require.True(t, grok.WindowStart.Equal(store.grokReset.Add(-usageMonitorGrokWindow)))
	require.Equal(t, "0", grok.GatewayActualCost)
	require.Equal(t, "200", grok.WalletRemainingYuan)
	require.Len(t, grok.Users, 1)
	require.Nil(t, grok.Users[0].ObservedShare)
	require.Nil(t, grok.Users[0].SlopeShare)
	require.Nil(t, grok.Slope)
	require.False(t, grok.Oversubscribe.Numeric)
	require.Nil(t, grok.Oversubscribe.WalletPoints)

	require.Len(t, view.Users, 2)
	require.Equal(t, int64(3), view.Users[0].ID)
	require.Equal(t, "admin", view.Users[0].Role)
	student := view.Users[1]
	require.Equal(t, int64(12), student.ID)
	require.Equal(t, []string{PurposeOpenAI, PurposeGrok, PurposeExperiment}, walletPurposes(student.Wallets))
	require.Equal(t, "0.5", student.Wallets[0].EffectiveUsedAmount)
	require.Equal(t, 1, *student.Wallets[0].StudentPercent)
	require.Nil(t, student.Wallets[0].Official)
	require.Equal(t, "4.00", student.Wallets[1].StoredUsedAmount)
	require.Equal(t, "0", student.Wallets[1].EffectiveUsedAmount)
	require.Equal(t, 0, *student.Wallets[1].StudentPercent)
	require.True(t, student.Wallets[1].LedgerUnrolled)
	require.True(t, student.Wallets[1].ResetsAt.After(now))
	require.Equal(t, "0.49", student.Wallets[2].StoredUsedAmount)
	require.Equal(t, "0.49", student.Wallets[2].EffectiveUsedAmount)
	require.Equal(t, 0, *student.Wallets[2].StudentPercent)
	require.Equal(t, usageMonitorOfficialNone, *student.Wallets[2].Official)
	require.Equal(t, []UsageMonitorRequestCount{{Platform: "deepseek", Count: 2}}, *student.Wallets[2].RequestCounts)

	encoded, err := json.Marshal(view)
	require.NoError(t, err)
	body := string(encoded)
	require.NotContains(t, body, "rate_multiplier")
	require.NotContains(t, body, "access_token")
	require.NotContains(t, body, "globalai")
}

type fakeUsageMonitorStore struct {
	t           *testing.T
	now         time.Time
	openaiReset time.Time
	grokReset   time.Time
	since       time.Time
	monthStart  time.Time
	monthEnd    time.Time
	gap         time.Duration
}

func (f *fakeUsageMonitorStore) ListOfficialParents(context.Context) ([]UsageMonitorParentRow, error) {
	return []UsageMonitorParentRow{
		{ID: 42, Name: "codex parent", Platform: PlatformOpenAI, Schedulable: true},
		{ID: 43, Name: "grok parent", Platform: PlatformGrok, Schedulable: true},
	}, nil
}

func (f *fakeUsageMonitorStore) ListStampedParentIDs(_ context.Context, since time.Time) ([]int64, error) {
	f.since = since
	return []int64{42, 43}, nil
}

func (f *fakeUsageMonitorStore) ListLatestStamps(_ context.Context, accountIDs []int64, since time.Time) ([]UsageMonitorStamp, error) {
	require.ElementsMatch(f.t, []int64{42, 43}, accountIDs)
	require.True(f.t, since.Equal(f.now.Add(-usageMonitorDiscoverWindow)))
	remaining := "58.0000"
	observed := f.now.Add(-10 * time.Minute)
	return []UsageMonitorStamp{
		{AccountID: 42, ResetAt: f.openaiReset, UsedPercent: "41.2500", RemainingPercent: &remaining, ObservedAt: &observed, Source: "codex_7d"},
		{AccountID: 43, ResetAt: f.grokReset, UsedPercent: "10.5000", ObservedAt: &observed, Source: "grok_billing"},
	}, nil
}

func (f *fakeUsageMonitorStore) ListWindowUsage(_ context.Context, windows []UsageMonitorWindowQuery, resetGap time.Duration) ([]UsageMonitorWindowUsage, error) {
	f.gap = resetGap
	require.Len(f.t, windows, 2)
	for _, window := range windows {
		switch window.ParentID {
		case 42:
			require.True(f.t, window.Reset.Equal(f.openaiReset))
			require.True(f.t, window.Start.Equal(f.openaiReset.Add(-usageMonitorCodexWindow)))
		case 43:
			require.True(f.t, window.Reset.Equal(f.grokReset))
			require.True(f.t, window.Start.Equal(f.grokReset.Add(-usageMonitorGrokWindow)))
		default:
			f.t.Fatalf("unexpected parent %d", window.ParentID)
		}
	}
	p0 := "36.7500"
	observed0 := f.now.Add(-48 * time.Hour)
	observed1 := f.now.Add(-time.Hour)
	between := "0.8000000000"
	return []UsageMonitorWindowUsage{
		{
			ParentID: 42,
			Users: []UsageMonitorWindowUserCost{
				{UserID: 12, ActualCost: "0.0003905"},
				{UserID: 99, ActualCost: "3.1246095"},
			},
			Slope: &UsageMonitorSlopeRow{P1: "41.2500", Observed1: observed1, P0: &p0, Observed0: &observed0, CostBetween: &between},
		},
		{
			ParentID: 43,
			Users:    []UsageMonitorWindowUserCost{{UserID: 12, ActualCost: "0"}},
		},
	}, nil
}

func (f *fakeUsageMonitorStore) ListLiveUsers(context.Context) ([]UsageMonitorUserRow, error) {
	month := ShanghaiMonthStart(f.now)
	return []UsageMonitorUserRow{
		{ID: 3, Email: "admin@example", Username: "admin", Role: "admin", HasWallet: true, Purpose: PurposeOpenAI, LimitAmount: "100", UsedAmount: "0", Period: "week", WindowStart: f.now.Add(-time.Hour)},
		{ID: 3, Email: "admin@example", Username: "admin", Role: "admin", HasWallet: true, Purpose: PurposeGrok, LimitAmount: "100", UsedAmount: "0", Period: "week", WindowStart: f.now.Add(-time.Hour)},
		{ID: 3, Email: "admin@example", Username: "admin", Role: "admin", HasWallet: true, Purpose: PurposeExperiment, LimitAmount: "500", UsedAmount: "0", Period: "month", WindowStart: month},
		{ID: 12, Email: "student@example", Username: "student", Role: "user", HasWallet: true, Purpose: PurposeGrok, LimitAmount: "100", UsedAmount: "4.00", Period: "week", WindowStart: f.now.Add(-8 * 24 * time.Hour)},
		{ID: 12, Email: "student@example", Username: "student", Role: "user", HasWallet: true, Purpose: PurposeOpenAI, LimitAmount: "100", UsedAmount: "0.5", Period: "week", WindowStart: f.now.Add(-time.Hour)},
		{ID: 12, Email: "student@example", Username: "student", Role: "user", HasWallet: true, Purpose: PurposeExperiment, LimitAmount: "100", UsedAmount: "0.49", Period: "month", WindowStart: month},
	}, nil
}

func (f *fakeUsageMonitorStore) ListExperimentGroupIDs(context.Context) ([]int64, error) {
	return []int64{8}, nil
}

func (f *fakeUsageMonitorStore) AdjustPurposeWallet(context.Context, PurposeWalletAdjust) (PurposeWalletAdjustResult, error) {
	return PurposeWalletAdjustResult{}, nil
}

func (f *fakeUsageMonitorStore) ListPurposeAdjustments(context.Context, PurposeAdjustmentQuery) ([]PurposeAdjustmentAudit, error) {
	return nil, nil
}

func (f *fakeUsageMonitorStore) ListExperimentRequestCounts(_ context.Context, groupIDs []int64, start, end time.Time) ([]UsageMonitorExperimentCount, error) {
	f.monthStart = start
	f.monthEnd = end
	require.Equal(f.t, []int64{8}, groupIDs)
	return []UsageMonitorExperimentCount{{UserID: 12, Platform: "deepseek", Count: 2}}, nil
}

func walletPurposes(wallets []UsageMonitorWallet) []string {
	out := make([]string, len(wallets))
	for i, wallet := range wallets {
		out[i] = wallet.Purpose
	}
	return out
}

func monitorTime(value time.Time) *time.Time { return &value }

func mustRat(text string) *big.Rat {
	parsed, ok := new(big.Rat).SetString(text)
	if !ok {
		panic(text)
	}
	return parsed
}

var _ UsageMonitorStore = (*fakeUsageMonitorStore)(nil)
