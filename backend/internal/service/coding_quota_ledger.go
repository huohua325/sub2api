package service

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/shopspring/decimal"
)

const codingWeek = 7 * 24 * time.Hour

// CodingQuotaPeriod returns a personal, half-open seven-day period. A confirmed
// platform reset starts a new sequence without changing another purpose's clock.
func CodingQuotaPeriod(anchor, reset, at time.Time) (time.Time, time.Time) {
	anchor, reset, at = anchor.UTC(), reset.UTC(), at.UTC()
	if !reset.After(at) && reset.After(anchor) {
		anchor = reset
	}
	if anchor.IsZero() {
		anchor = at
	}
	if at.After(anchor) {
		anchor = anchor.Add(time.Duration(at.Sub(anchor)/codingWeek) * codingWeek)
	}
	return anchor, anchor.Add(codingWeek)
}

// CodingQuotaReading is the durable personal ledger, independent of current
// parent availability and official billing windows. Pending points are estimates.
type CodingQuotaReading struct {
	UserID         int64
	Purpose        string
	Used           string
	Pending        string
	PendingCount   int
	UnmeteredCount int
	Imported       bool
	Start          time.Time
	End            time.Time
	UpdatedAt      time.Time
	ResetReason    string
}

// OfficialQuotaObservation is a cumulative reading, never a per-request charge.
type OfficialQuotaObservation struct {
	AccountID  int64
	Purpose    string
	WindowEnd  time.Time
	ObservedAt time.Time
	Used       string
	Source     string
}

// CodingQuotaCharge is written in the same transaction as billing deduplication.
// Weight is a pre-discount model cost, not a user's selling-price multiplier.
type CodingQuotaCharge struct {
	AccountID   int64
	Purpose     string
	Weight      string
	StartedAt   time.Time
	CompletedAt time.Time
	Observation *OfficialQuotaObservation
	Baseline    *OfficialQuotaObservation
}

// CodingQuotaReset is an auditable, idempotent platform-wide event. An account
// reset alone is only evidence and never replenishes student quotas.
type CodingQuotaReset struct {
	ID          string    `json:"id"`
	EffectiveAt time.Time `json:"effective_at"`
	DetectedAt  time.Time `json:"detected_at"`
	Status      string    `json:"status"`
	Source      string    `json:"source"`
	Note        string    `json:"note"`
	Evidence    string    `json:"evidence"`
	ActorUserID *int64    `json:"actor_user_id,omitempty"`
}

// CodingQuotaLedger is implemented by the SQL store. Reads perform no probes.
type CodingQuotaLedger interface {
	ReadCodingQuotas(context.Context, []int64, time.Time) ([]CodingQuotaReading, error)
	ObserveCodingQuota(context.Context, OfficialQuotaObservation) error
	ReconcileCodingQuota(context.Context, time.Time) error
	ListCodingResets(context.Context) ([]CodingQuotaReset, error)
	ConfirmCodingReset(context.Context, CodingQuotaReset, int64) (CodingQuotaReset, error)
}

// PointAllocation is a deterministic share of one official increment.
type PointAllocation struct {
	ID     int64
	Weight string
	Points string
}

// AllocateOfficialIncrement conserves the increment at eight decimal places.
// Stable ordering also makes replays independent of database result order.
func AllocateOfficialIncrement(increment string, requests []PointAllocation) ([]PointAllocation, error) {
	delta, err := decimal.NewFromString(increment)
	if err != nil || delta.IsNegative() {
		return nil, fmt.Errorf("invalid official increment %q", increment)
	}
	delta = delta.Round(8)
	out := append([]PointAllocation(nil), requests...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	weights := make([]decimal.Decimal, len(out))
	total := decimal.Zero
	for i := range out {
		weights[i], err = decimal.NewFromString(out[i].Weight)
		if err != nil || !weights[i].IsPositive() {
			return nil, fmt.Errorf("invalid quota weight for request %d", out[i].ID)
		}
		total = total.Add(weights[i])
	}
	if total.IsZero() {
		return out, nil
	}
	remaining := delta
	for i := range out {
		share := delta.Mul(weights[i]).DivRound(total, 8)
		if share.GreaterThan(remaining) {
			share = remaining
		}
		if i == len(out)-1 {
			share = remaining
		}
		out[i].Points = share.StringFixed(8)
		remaining = remaining.Sub(share)
	}
	return out, nil
}
