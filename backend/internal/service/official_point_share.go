package service

import (
	"math/big"
	"time"
)

// OfficialPointInput is one parent account in its current official window.
type OfficialPointInput struct {
	Platform string
	U        string
	S        string
	P        *string
}

// OfficialPointResult is one user's points on one subscription after same-platform parents are summed.
type OfficialPointResult struct {
	Points  *string
	Percent *int
	Reset   *time.Time
	// OfficialResets is retained for legacy store adapters. Personal ledger
	// countdowns use Reset and never derive a cycle from these parent clocks.
	OfficialResets []time.Time
	// Blocked is true only when this window's points are at least the per-person cap.
	// A card can already read 100% while Blocked is still false.
	Blocked     bool
	PeriodStart *time.Time
	Pending     *string
	DataStatus  string
	UpdatedAt   *time.Time
	ResetReason string
}

// OfficialPointsForPurpose sums already-rounded points from each parent of one
// platform, then divides by that person's cap. A missing P is skipped. A present
// P with no gateway cost is zero points, so an unused person still has a reading.
// The purpose is null only when every parent is skipped.
func OfficialPointsForPurpose(rows []OfficialPointInput, budget string, reset *time.Time) OfficialPointResult {
	var sum *big.Rat
	for _, row := range rows {
		share := officialPointShare(row.U, row.S, row.P)
		if share == nil {
			continue
		}
		part, ok := parseDecimal(*share)
		if !ok {
			continue
		}
		if sum == nil {
			sum = new(big.Rat)
		}
		sum.Add(sum, part)
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
	return OfficialPointResult{
		Points:  &text,
		Percent: &percent,
		Reset:   reset,
		Blocked: sum.Cmp(limit) >= 0,
	}
}

func officialPointShare(uText, sText string, pText *string) *string {
	if pText == nil || pointTrim(*pText) == "" {
		return nil
	}
	s, ok := parseDecimal(sText)
	if !ok || s.Sign() <= 0 {
		zero := "0"
		return &zero
	}
	u, ok := parseDecimal(uText)
	if !ok {
		u = new(big.Rat)
	}
	p, ok := parseDecimal(*pText)
	if !ok {
		return nil
	}
	return observedShare(u, s, p)
}

func pointTrim(value string) string {
	start, end := 0, len(value)
	for start < end && (value[start] == ' ' || value[start] == '\n' || value[start] == '\t' || value[start] == '\r') {
		start++
	}
	for end > start && (value[end-1] == ' ' || value[end-1] == '\n' || value[end-1] == '\t' || value[end-1] == '\r') {
		end--
	}
	return value[start:end]
}
