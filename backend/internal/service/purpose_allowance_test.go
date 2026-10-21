//go:build unit

package service

import (
	"testing"
	"time"
)

func TestPurposeOfGroup(t *testing.T) {
	cases := []struct {
		platform string
		kind     string
		want     string
	}{
		{"openai", UsageKindCoding, PurposeOpenAI},
		{"openai", UsageKindExperiment, PurposeExperiment},
		{"grok", UsageKindCoding, PurposeGrok},
		{"grok", UsageKindExperiment, PurposeExperiment},
		{"qwen", UsageKindExperiment, PurposeExperiment},
		{"anthropic", UsageKindExperiment, PurposeExperiment},
		{"gemini", "", PurposeExperiment},
		{"anthropic", UsageKindCoding, ""},
		{"", UsageKindExperiment, ""},
	}
	for _, tc := range cases {
		if got := PurposeOfGroup(tc.platform, tc.kind); got != tc.want {
			t.Errorf("PurposeOfGroup(%q, %q) = %q, want %q", tc.platform, tc.kind, got, tc.want)
		}
	}
}

func TestPurposeWindow(t *testing.T) {
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, shanghai)
	inside := start.Add(10 * 24 * time.Hour)
	if PurposeWindowExpired(PurposeExperiment, start, inside) {
		t.Fatal("experiment window should stay open through October")
	}
	november := time.Date(2026, 11, 1, 0, 0, 0, 0, shanghai)
	if !PurposeWindowExpired(PurposeExperiment, start, november) {
		t.Fatal("experiment window should expire at the next Beijing month")
	}
	weekStart := time.Date(2026, 10, 20, 13, 26, 0, 0, shanghai)
	if PurposeWindowExpired(PurposeOpenAI, weekStart, weekStart.Add(6*24*time.Hour)) {
		t.Fatal("coding window should last seven days")
	}
	if !PurposeWindowExpired(PurposeOpenAI, weekStart, weekStart.Add(purposeWeek)) {
		t.Fatal("coding window should expire at seven days")
	}
	renewed := RenewedPurposeWindow(PurposeOpenAI, weekStart, weekStart.Add(8*24*time.Hour))
	if !renewed.Equal(weekStart.Add(purposeWeek)) {
		t.Fatalf("renewed week = %s", renewed)
	}
}

func TestPurposeLimit(t *testing.T) {
	if PurposeLimit(PurposeOpenAI) != 100 || PurposeLimit(PurposeGrok) != 100 || PurposeLimit(PurposeExperiment) != 500 {
		t.Fatalf("unexpected defaults: openai=%v grok=%v experiment=%v", PurposeLimit(PurposeOpenAI), PurposeLimit(PurposeGrok), PurposeLimit(PurposeExperiment))
	}
}
