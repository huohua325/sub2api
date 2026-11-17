package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAPIFallbackPreviewCannotAuthorizeDisabledOrExpiredPolicy(t *testing.T) {
	now := time.Now().UTC()
	future := now.Add(time.Hour)
	past := now.Add(-time.Minute)
	for _, tt := range []struct {
		name              string
		enabled, verified bool
		expiry            *time.Time
		reason            string
		eligible          bool
	}{
		{"disabled", false, true, &future, "policy_disabled", false},
		{"expired", true, true, &past, "policy_expired", false},
		{"missing expiry", true, true, nil, "policy_expired", false},
		{"unverified capabilities", true, false, &future, "capabilities_unverified", false},
		{"active still requires request checks", true, true, &future, "all_subscriptions_unavailable", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := &APIFallbackPolicy{Enabled: tt.enabled, CapabilitiesVerified: tt.verified, ExpiresAt: tt.expiry}
			original := APIFallbackCheck{Name: "account:10", Passed: true, Detail: "auth_invalid"}
			view := applyAPIFallbackPolicyPreview(&APIFallbackPreview{Eligible: true, Reason: "all_subscriptions_unavailable", Checks: []APIFallbackCheck{original}}, p, now)
			require.Equal(t, tt.reason, view.Reason)
			require.Equal(t, tt.eligible, view.Eligible)
			require.True(t, view.RequestChecksPending)
			require.Equal(t, original, view.Checks[0])
			require.Len(t, view.Checks, 5)
		})
	}
}

func TestAPIFallbackPreviewHonorsCostCircuit(t *testing.T) {
	now := time.Now().UTC()
	future := now.Add(time.Hour)
	view := applyAPIFallbackPolicyPreview(&APIFallbackPreview{Eligible: true, Reason: "all_subscriptions_unavailable"}, &APIFallbackPolicy{Enabled: true, ExpiresAt: &future, CapabilitiesVerified: true, CostCircuitOpen: true}, now)
	require.False(t, view.Eligible)
	require.Equal(t, "cost_bound_exceeded", view.Reason)
}
