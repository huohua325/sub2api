package service

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStampOfficialQuota_CodexHeaderOnParentAndShadow(t *testing.T) {
	now := time.Date(2026, 10, 19, 2, 0, 0, 0, time.UTC)
	storedReset := now.Add(3 * 24 * time.Hour).Format(time.RFC3339)
	parent := &Account{
		ID:       5,
		Platform: PlatformOpenAI,
		Extra:    map[string]any{"codex_7d_reset_at": storedReset, "codex_7d_used_percent": 40.0},
	}
	shadowID := parent.ID
	shadow := &Account{ID: 9, Platform: PlatformOpenAI, ParentAccountID: &shadowID}
	headers := http.Header{}
	headers.Set("x-codex-primary-used-percent", "50")
	headers.Set("x-codex-primary-window-minutes", "10080")
	headers.Set("x-codex-primary-reset-after-seconds", "259200")

	log := &UsageLog{AccountID: shadow.ID}
	tracker := newOfficialWindowTracker()
	stampOfficialQuota(log, shadow, parent, headers, now, tracker)

	require.NotNil(t, log.OfficialAccountID)
	require.Equal(t, parent.ID, *log.OfficialAccountID)
	require.NotNil(t, log.OfficialUsedPercent)
	require.InDelta(t, 50, *log.OfficialUsedPercent, 0.0001)
	require.NotNil(t, log.OfficialRemainingPercent)
	require.InDelta(t, 50, *log.OfficialRemainingPercent, 0.0001)
	require.Equal(t, "codex_7d", *log.OfficialSource)
	require.NotNil(t, log.OfficialWindowReset)
	require.False(t, *log.OfficialWindowReset)
	require.Equal(t, shadow.ID, log.AccountID)
}

func TestStampOfficialQuota_CodexWindowResetThenStable(t *testing.T) {
	now := time.Date(2026, 10, 19, 2, 0, 0, 0, time.UTC)
	oldReset := now.Add(-time.Hour).Format(time.RFC3339)
	parent := &Account{
		ID:       5,
		Platform: PlatformOpenAI,
		Extra:    map[string]any{"codex_7d_reset_at": oldReset},
	}
	headers := http.Header{}
	headers.Set("x-codex-primary-used-percent", "2")
	headers.Set("x-codex-primary-window-minutes", "10080")
	headers.Set("x-codex-primary-reset-after-seconds", "604800")
	tracker := newOfficialWindowTracker()

	first := &UsageLog{}
	stampOfficialQuota(first, parent, parent, headers, now, tracker)
	require.NotNil(t, first.OfficialWindowReset)
	require.True(t, *first.OfficialWindowReset)
	require.InDelta(t, 2, *first.OfficialUsedPercent, 0.0001)
	require.InDelta(t, 98, *first.OfficialRemainingPercent, 0.0001)

	second := &UsageLog{}
	stampOfficialQuota(second, parent, parent, headers, now.Add(time.Minute), tracker)
	require.NotNil(t, second.OfficialWindowReset)
	require.False(t, *second.OfficialWindowReset)
	require.True(t, first.OfficialResetAt.Equal(*second.OfficialResetAt))
}

func TestStampOfficialQuota_CodexCarriesUnexpiredExtra(t *testing.T) {
	now := time.Date(2026, 10, 19, 2, 0, 0, 0, time.UTC)
	reset := now.Add(48 * time.Hour)
	observed := now.Add(-time.Minute)
	parent := &Account{
		ID:       5,
		Platform: PlatformOpenAI,
		Extra: map[string]any{
			"codex_7d_used_percent":        12.5,
			"codex_7d_reset_after_seconds": 172800,
			"codex_7d_window_minutes":      10080,
			"codex_7d_reset_at":            reset.Format(time.RFC3339),
			"codex_usage_updated_at":       observed.Format(time.RFC3339),
		},
	}
	log := &UsageLog{}
	stampOfficialQuota(log, parent, parent, nil, now, newOfficialWindowTracker())
	require.NotNil(t, log.OfficialUsedPercent)
	require.InDelta(t, 12.5, *log.OfficialUsedPercent, 0.0001)
	require.InDelta(t, 87.5, *log.OfficialRemainingPercent, 0.0001)
	require.Equal(t, "codex_7d", *log.OfficialSource)
	require.NotNil(t, log.OfficialObservedAt)
	require.True(t, log.OfficialObservedAt.Equal(observed))
	require.NotNil(t, log.OfficialWindowReset)
	require.False(t, *log.OfficialWindowReset)
}

func TestStampOfficialQuota_ExpiredCodexLeavesPercentEmpty(t *testing.T) {
	now := time.Date(2026, 10, 19, 2, 0, 0, 0, time.UTC)
	parent := &Account{
		ID:       5,
		Platform: PlatformOpenAI,
		Extra: map[string]any{
			"codex_7d_used_percent":   0.0,
			"codex_7d_reset_at":       now.Add(-time.Hour).Format(time.RFC3339),
			"codex_usage_updated_at":  now.Add(-2 * time.Hour).Format(time.RFC3339),
			"codex_7d_window_minutes": 10080,
		},
	}
	log := &UsageLog{}
	stampOfficialQuota(log, parent, parent, nil, now, newOfficialWindowTracker())
	require.Equal(t, parent.ID, *log.OfficialAccountID)
	require.Nil(t, log.OfficialUsedPercent)
	require.Nil(t, log.OfficialRemainingPercent)
	require.Equal(t, "codex_7d_expired", *log.OfficialSource)
	require.Nil(t, log.OfficialWindowReset)
}

func TestStampOfficialQuota_GrokIgnoresRateLimitHeaders(t *testing.T) {
	now := time.Date(2026, 10, 19, 2, 0, 0, 0, time.UTC)
	periodEnd := now.Add(48 * time.Hour).Format(time.RFC3339)
	account := &Account{
		ID:       1,
		Platform: PlatformGrok,
		Extra: map[string]any{
			"grok_billing_snapshot": map[string]any{
				"usage_percent":     30.0,
				"period_end":        periodEnd,
				"weekly_updated_at": now.Add(-10 * time.Minute).Format(time.RFC3339),
			},
		},
	}
	headers := http.Header{}
	headers.Set("x-ratelimit-remaining-tokens", "10")
	log := &UsageLog{}
	stampOfficialQuota(log, account, account, headers, now, newOfficialWindowTracker())
	require.Equal(t, account.ID, *log.OfficialAccountID)
	require.InDelta(t, 30, *log.OfficialUsedPercent, 0.0001)
	require.InDelta(t, 70, *log.OfficialRemainingPercent, 0.0001)
	require.Equal(t, "grok_billing", *log.OfficialSource)
	require.False(t, *log.OfficialWindowReset)
}

func TestStampOfficialQuota_GrokWindowReset(t *testing.T) {
	now := time.Date(2026, 10, 19, 2, 0, 0, 0, time.UTC)
	account := &Account{
		ID:       1,
		Platform: PlatformGrok,
		Extra: map[string]any{
			"grok_billing_snapshot": map[string]any{
				"usage_percent": 4.0,
				"period_end":    now.Add(6 * 24 * time.Hour).Format(time.RFC3339),
			},
		},
	}
	tracker := newOfficialWindowTracker()
	tracker.last[account.ID] = now.Add(-24 * time.Hour)
	log := &UsageLog{}
	stampOfficialQuota(log, account, account, nil, now, tracker)
	require.True(t, *log.OfficialWindowReset)
	require.InDelta(t, 96, *log.OfficialRemainingPercent, 0.0001)
}

func TestStampOfficialQuota_OtherPlatformUntouched(t *testing.T) {
	log := &UsageLog{}
	stampOfficialQuota(log, &Account{ID: 3, Platform: PlatformAnthropic}, nil, nil, time.Now(), newOfficialWindowTracker())
	require.Nil(t, log.OfficialAccountID)
	require.Nil(t, log.OfficialUsedPercent)
	require.Nil(t, log.OfficialSource)
}
