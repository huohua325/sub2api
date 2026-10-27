package service

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type codingReadTestStore struct {
	CodingQuotaLedger
	rows []CodingQuotaReading
	err  error
}

func (s codingReadTestStore) ReadCodingQuotas(context.Context, []int64, time.Time) ([]CodingQuotaReading, error) {
	return s.rows, s.err
}
func (codingReadTestStore) ListOfficialParents(context.Context) ([]UsageMonitorParentRow, error) {
	panic("personal reads must not load parents")
}
func (codingReadTestStore) SumOfficialWindowCost(context.Context, int64, []UsageMonitorWindowQuery, time.Duration) ([]OfficialWindowCost, error) {
	panic("personal reads must not recalculate parent shares")
}

func TestCodingQuotaReadsUseOnlyPersonalLedger(t *testing.T) {
	for _, tc := range []struct {
		name    string
		store   codingReadTestStore
		blocked bool
	}{
		{"ledger error", codingReadTestStore{err: errors.New("database unavailable")}, true},
		{"missing ledger row", codingReadTestStore{}, true},
		{"under cap", codingReadTestStore{rows: []CodingQuotaReading{{Purpose: PurposeOpenAI, Used: "49.8", Pending: "0"}}}, false},
		{"at cap", codingReadTestStore{rows: []CodingQuotaReading{{Purpose: PurposeOpenAI, Used: "50", Pending: "0"}}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewOfficialPointService(nil, tc.store, nil)
			card := s.CardsForUserStored(context.Background(), 1)[PurposeOpenAI]
			require.Equal(t, tc.blocked, card.Blocked)
			blocked, checked := s.Blocks(context.Background(), 1, PurposeOpenAI)
			require.True(t, checked)
			require.Equal(t, tc.blocked, blocked)
		})
	}
}

func TestCodingQuotaLongStreamUsesHeaderTimeForReset(t *testing.T) {
	observed := time.Date(2026, 10, 27, 3, 0, 0, 0, time.UTC)
	headers := http.Header{}
	headers.Set("Date", observed.Format(http.TimeFormat))
	headers.Set("x-codex-primary-used-percent", "20")
	headers.Set("x-codex-primary-window-minutes", "10080")
	headers.Set("x-codex-primary-reset-after-seconds", "604800")
	_, reset, stamp, _ := readCodexOfficial(nil, headers, observed.Add(90*time.Minute))
	require.NotNil(t, reset)
	require.True(t, reset.Equal(observed.Add(codingWeek)))
	require.True(t, stamp.Equal(observed))
}

func TestCodingQuotaPersonalPeriod(t *testing.T) {
	anchor := time.Date(2026, 10, 1, 11, 17, 0, 0, time.FixedZone("CST", 8*3600))
	for _, tc := range []struct {
		name            string
		at, reset, want time.Time
	}{
		{"before boundary", anchor.Add(codingWeek - time.Nanosecond), time.Time{}, anchor},
		{"exact boundary", anchor.Add(codingWeek), time.Time{}, anchor.Add(codingWeek)},
		{"inactive for weeks", anchor.Add(3*codingWeek + time.Hour), time.Time{}, anchor.Add(3 * codingWeek)},
		{"exception starts seven days", anchor.Add(3 * 24 * time.Hour), anchor.Add(2 * 24 * time.Hour), anchor.Add(2 * 24 * time.Hour)},
		{"exception second week", anchor.Add(10 * 24 * time.Hour), anchor.Add(2 * 24 * time.Hour), anchor.Add(9 * 24 * time.Hour)},
		{"future event ignored", anchor.Add(time.Hour), anchor.Add(2 * time.Hour), anchor},
		{"event before activation ignored", anchor.Add(time.Hour), anchor.Add(-time.Hour), anchor},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start, end := CodingQuotaPeriod(anchor, tc.reset, tc.at)
			require.True(t, start.Equal(tc.want))
			require.Equal(t, codingWeek, end.Sub(start))
		})
	}
}

func TestCodingQuotaIncrementConservesPoints(t *testing.T) {
	for _, tc := range []struct {
		name, delta string
		weights     []string
		want        []string
	}{
		{"two users", "6", []string{"1", "2"}, []string{"2.00000000", "4.00000000"}},
		{"small increment", "0.00000001", []string{"1", "1", "1"}, []string{"0.00000000", "0.00000000", "0.00000001"}},
		{"uneven", "1", []string{"1", "1", "1"}, []string{"0.33333333", "0.33333333", "0.33333334"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests []PointAllocation
			for i, w := range tc.weights {
				requests = append(requests, PointAllocation{ID: int64(i + 1), Weight: w})
			}
			got, err := AllocateOfficialIncrement(tc.delta, requests)
			require.NoError(t, err)
			total := decimal.Zero
			for i, p := range got {
				require.Equal(t, tc.want[i], p.Points)
				d, e := decimal.NewFromString(p.Points)
				require.NoError(t, e)
				total = total.Add(d)
			}
			require.Equal(t, tc.delta, total.String())
		})
	}
	_, err := AllocateOfficialIncrement("-1", nil)
	require.Error(t, err)
	_, err = AllocateOfficialIncrement("1", []PointAllocation{{ID: 1, Weight: "0"}})
	require.Error(t, err)
}

func TestCodingQuotaRoundedPercentDoesNotBlock(t *testing.T) {
	reading := CodingQuotaReading{Used: "49.8", Pending: "0", End: time.Now().Add(time.Hour)}
	card := codingQuotaResult(reading, "50")
	require.Nil(t, card.UpdatedAt, "reading the page must not manufacture ledger freshness")
	updatedAt := time.Now().Add(-time.Minute)
	reading.UpdatedAt = updatedAt
	card = codingQuotaResult(reading, "50")
	require.Equal(t, updatedAt, *card.UpdatedAt)
	require.Equal(t, 100, *card.Percent)
	require.False(t, card.Blocked)
	reading.Pending = "0.2"
	reading.PendingCount = 1
	card = codingQuotaResult(reading, "50")
	require.True(t, card.Blocked)
	require.Equal(t, "pending", card.DataStatus)
	reading.Pending = "0"
	reading.Used = "50"
	reading.UnmeteredCount = 1
	card = codingQuotaResult(reading, "50")
	require.True(t, card.Blocked)
	require.Equal(t, "partial", card.DataStatus)
}

func TestCodingQuotaUsesPreDiscountWeight(t *testing.T) {
	now := time.Now()
	id := int64(8)
	log := &UsageLog{OfficialAccountID: &id, CreatedAt: now, QuotaStartedAt: now.Add(-time.Minute)}
	p := &postUsageBillingParams{Account: &Account{ID: 8, Platform: PlatformOpenAI, Type: AccountTypeOAuth},
		APIKey: &APIKey{Group: &Group{Platform: PlatformOpenAI, UsageKind: UsageKindCoding}}, Cost: &CostBreakdown{TotalCost: 10, ActualCost: 1}}
	a := codingChargeForUsage(log, p)
	p.Cost.ActualCost = 20
	b := codingChargeForUsage(log, p)
	require.Equal(t, "10", a.Weight)
	require.Equal(t, a.Weight, b.Weight)
	require.Equal(t, log.QuotaStartedAt, a.StartedAt)
}

func TestCodingQuotaCodexProbeSelectsOnlyMainSevenDayBucket(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	usage := &OpenAIQuotaUsage{FetchedAt: now.Unix(), RateLimit: &OpenAIRateLimit{
		PrimaryWindow:   &OpenAIRateLimitWindow{UsedPercent: 90, LimitWindowSeconds: 18000, ResetAt: now.Add(time.Hour).Unix()},
		SecondaryWindow: &OpenAIRateLimitWindow{UsedPercent: 30, LimitWindowSeconds: 604800, ResetAt: now.Add(codingWeek).Unix()}},
		AdditionalRateLimits: []OpenAIAdditionalRateLimit{{MeteredFeature: "codex_bengalfox", RateLimit: &OpenAIRateLimit{SecondaryWindow: &OpenAIRateLimitWindow{UsedPercent: 99, LimitWindowSeconds: 604800}}}}}
	obs := codexUsageObservation(1, usage, now)
	require.NotNil(t, obs)
	require.Equal(t, "30", obs.Used)
	usage.FetchedAt = now.Add(-10 * time.Minute).Unix()
	usage.RateLimit.SecondaryWindow.ResetAt = 0
	usage.RateLimit.SecondaryWindow.ResetAfterSeconds = 3600
	obs = codexUsageObservation(1, usage, now)
	require.True(t, obs.WindowEnd.Equal(now.Add(50*time.Minute)), "cached reset-after is relative to its fetch time")
	usage.RateLimit.SecondaryWindow = nil
	require.Nil(t, codexUsageObservation(1, usage, now))
	usage.RateLimit = nil
	require.Nil(t, codexUsageObservation(1, usage, now))
}
