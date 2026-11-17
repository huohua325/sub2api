package repository

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAPIFallbackPolicyValidation(t *testing.T) {
	now := time.Now().UTC()
	future := now.Add(time.Hour)
	for _, tt := range []struct {
		name  string
		edit  func(*service.APIFallbackPolicy)
		valid bool
	}{
		{"disabled draft", func(p *service.APIFallbackPolicy) {
			p.Enabled = false
			p.PrimaryAccountIDs = nil
			p.BackupAccountIDs = nil
			p.PointsPerUSD = ""
			p.ExpiresAt = nil
			p.MaxInputTokens = 0
		}, true},
		{"verified policy", func(*service.APIFallbackPolicy) {}, true},
		{"unconfirmed capabilities", func(p *service.APIFallbackPolicy) { p.CapabilitiesVerified = false }, false},
		{"no point rate", func(p *service.APIFallbackPolicy) { p.PointsPerUSD = "0" }, false},
		{"missing cached input price", func(p *service.APIFallbackPolicy) { p.CachedInputPricePerMillion = "" }, false},
		{"explicit verified free cache", func(p *service.APIFallbackPolicy) { p.CachedInputPricePerMillion = "0" }, true},
		{"overlapping pools", func(p *service.APIFallbackPolicy) { p.BackupAccountIDs = []int64{10} }, false},
		{"unknown model wildcard", func(p *service.APIFallbackPolicy) { p.Model = "gpt-*" }, false},
		{"excessive input tier", func(p *service.APIFallbackPolicy) { p.MaxInputTokens = 200000 }, false},
		{"missing output bound", func(p *service.APIFallbackPolicy) { p.MaxOutputTokens = 0 }, false},
		{"negative cost", func(p *service.APIFallbackPolicy) { p.SupplierMultiplier = "-1" }, false},
		{"oversized budget", func(p *service.APIFallbackPolicy) { p.PerRequestBudgetUSD = "20" }, false},
		{"unknown failure reason", func(p *service.APIFallbackPolicy) {
			p.PrimaryEvidence = []service.APIFallbackPrimaryEvidence{{AccountID: 10, Reason: "network_timeout", ObservedAt: now, ValidUntil: future, Note: "timeout"}}
		}, false},
		{"permanent stale evidence", func(p *service.APIFallbackPolicy) {
			p.PrimaryEvidence = []service.APIFallbackPrimaryEvidence{{AccountID: 10, Reason: "auth_invalid", ObservedAt: now, ValidUntil: now.Add(25 * time.Hour), Note: "old auth"}}
		}, false},
		{"documented auth failure", func(p *service.APIFallbackPolicy) {
			p.PrimaryEvidence = []service.APIFallbackPrimaryEvidence{{AccountID: 10, Reason: "auth_invalid", ObservedAt: now, ValidUntil: future, Note: "provider confirms invalid refresh credential"}}
		}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := service.APIFallbackPolicy{GroupID: 1, Platform: "openai", Model: "gpt-test", PrimaryAccountIDs: []int64{10}, BackupAccountIDs: []int64{20}, Enabled: true, CapabilitiesVerified: true, ExpiresAt: &future, PointsPerUSD: "0.13", SupplierMultiplier: "0.3", InputPricePerMillion: "1", CachedInputPricePerMillion: "0.1", OutputPricePerMillion: "2", MaxInputTokens: 1000, MaxOutputTokens: 1000, PerRequestBudgetUSD: "1", DailyBudgetUSD: "10", TotalBudgetUSD: "20", Note: "documented and approved"}
			tt.edit(&p)
			err := normalizeFallbackPolicy(&p, now)
			if tt.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
