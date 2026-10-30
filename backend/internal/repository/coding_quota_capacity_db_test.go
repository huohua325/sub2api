package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCodingQuotaDBCapacityChangeKeepsHistoryAndStartsNewCalibration(t *testing.T) {
	db, r, base := quotaTestDB(t, "")
	ctx := context.Background()
	end := base.Add(6 * 24 * time.Hour)
	observe := func(at time.Time, used string) {
		quotaObserve(t, r, 10, at, end, used)
		require.NoError(t, r.ReconcileCodingQuota(ctx, at))
	}
	observe(base, "30")
	quotaRequest(t, db, "old-settled", 1, 10, base.Add(time.Minute), "2", "openai")
	observe(base.Add(2*time.Minute), "31") // Old rate is 1 / 2.
	quotaRequest(t, db, "old-estimate", 2, 10, base.Add(3*time.Minute), "2", "openai")
	before := quotaUsed(t, r, 2, base.Add(3*time.Minute), "openai")
	observe(base.Add(4*time.Minute), "16")
	change := CodingCapacityChange{ID: "owner:capacity:example", AccountID: 10, Purpose: "openai", BeforeObservedAt: base.Add(2 * time.Minute), EffectiveAt: base.Add(4 * time.Minute), WindowEnd: end, Before: "31", After: "16", Factor: "2", Actor: "owner", Note: "owner confirmed capacity increase; matching observations"}
	require.NoError(t, r.ConfirmCodingCapacityChange(ctx, change))
	quotaRequest(t, db, "new-settled", 1, 10, base.Add(5*time.Minute), "4", "openai")
	observe(base.Add(6*time.Minute), "17") // New rate is 1 / 4; the 31% high-water belongs to the prior capacity.
	after := quotaUsed(t, r, 2, base.Add(6*time.Minute), "openai")
	require.Equal(t, before.Start, after.Start)
	require.Equal(t, before.End, after.End)
	require.Equal(t, "1.00000000", after.Pending, "old usage must not be repriced to 0.5 after capacity doubles")
	require.Equal(t, "2.00000000", quotaUsed(t, r, 1, base.Add(time.Hour), "openai").Used, "old settled 1 + new settled 1")
	var rate string
	require.NoError(t, db.QueryRow(`SELECT points_per_weight::text FROM coding_quota_meters WHERE account_id=10`).Scan(&rate))
	require.Equal(t, "0.250000000000", rate, "old and new capacity samples must not mix")
	reservation, err := r.ReserveCodingQuota(ctx, service.CodingReservationInput{ID: "new-capacity-sample-check", UserID: 1, APIKeyID: 1, AccountID: 10, Purpose: "openai", Mode: "shadow", Limit: "50", Weight: "4", StartedAt: base.Add(7 * time.Minute), Now: base.Add(7 * time.Minute), MinSamples: 2})
	require.NoError(t, err)
	require.Equal(t, 1, reservation.SampleCount)
	require.False(t, reservation.Reliable, "one sample on either side of expansion is not two post-expansion samples")
	quotaRequest(t, db, "new-estimate", 2, 10, base.Add(7*time.Minute), "4", "openai")
	require.Equal(t, "2.00000000", quotaUsed(t, r, 2, base.Add(time.Hour), "openai").Pending)
	// A retry after new usage must neither rewind the raw counter nor grant usage.
	require.NoError(t, NewUsageMonitorRepository(db).ConfirmCodingCapacityChange(ctx, change))
	var raw string
	require.NoError(t, db.QueryRow(`SELECT used::text FROM coding_quota_meters WHERE account_id=10`).Scan(&raw))
	require.Equal(t, "17.00000000", raw)
	require.Equal(t, "2.00000000", quotaUsed(t, r, 1, base.Add(time.Hour), "openai").Used)
	change.Factor = "3"
	require.Error(t, r.ConfirmCodingCapacityChange(ctx, change), "stable event IDs cannot be reused with edited evidence")
	events, err := r.ListCodingResets(ctx)
	require.NoError(t, err)
	require.Empty(t, events, "capacity expansion is not a personal/global reset")
	require.Equal(t, "0", quotaUsed(t, r, 1, base.Add(time.Hour), service.PurposeGrok).Used)
	// A late billing result that started before expansion retains its prior
	// estimate and remains unresolved, rather than taking a post-expansion tick.
	_, err = (&usageBillingRepository{db: db}).Apply(ctx, &service.UsageBillingCommand{RequestID: "late-old-request", APIKeyID: 2, UserID: 2, AccountID: 10,
		CodingQuota: &service.CodingQuotaCharge{AccountID: 10, Purpose: "openai", Weight: "2", StartedAt: base.Add(3 * time.Minute), CompletedAt: base.Add(8 * time.Minute)}})
	require.NoError(t, err)
	var state, estimate string
	require.NoError(t, db.QueryRow(`SELECT state,estimated_points::text FROM coding_quota_requests WHERE request_id='late-old-request'`).Scan(&state, &estimate))
	require.Equal(t, "unmetered", state)
	require.Equal(t, "1.00000000", estimate)
}

func TestCodingQuotaDBLateCapacityConfirmationPreservesCurrentCounter(t *testing.T) {
	db, r, base := quotaTestDB(t, "")
	ctx := context.Background()
	end := base.Add(6 * 24 * time.Hour)
	quotaObserve(t, r, 10, base, end, "31")
	require.NoError(t, r.ReconcileCodingQuota(ctx, base))
	quotaObserve(t, r, 10, base.Add(time.Minute), end, "16")
	require.NoError(t, r.ReconcileCodingQuota(ctx, base.Add(time.Minute)))
	// A persisted legacy process has already advanced its raw counter. Registering
	// the historical capacity boundary must never replay these later increments.
	_, err := db.Exec(`UPDATE coding_quota_meters SET used=29,observed_at=$1,points_per_weight=0.3 WHERE account_id=10`, base.Add(time.Hour))
	require.NoError(t, err)
	change := CodingCapacityChange{ID: "owner:capacity:late", AccountID: 10, Purpose: "openai", BeforeObservedAt: base, EffectiveAt: base.Add(time.Minute), WindowEnd: end, Before: "31", After: "16", Factor: "2", Actor: "owner", Note: "late evidence confirmation"}
	require.NoError(t, r.ConfirmCodingCapacityChange(ctx, change))
	var raw string
	var observed time.Time
	require.NoError(t, db.QueryRow(`SELECT used::text,observed_at FROM coding_quota_meters WHERE account_id=10`).Scan(&raw, &observed))
	require.Equal(t, "29.00000000", raw)
	require.True(t, observed.Equal(base.Add(time.Hour)))
	require.NoError(t, r.ConfirmCodingCapacityChange(ctx, change))
	change.ID = "owner:capacity:missing-evidence"
	change.EffectiveAt = base.Add(2 * time.Minute)
	require.Error(t, r.ConfirmCodingCapacityChange(ctx, change))
	var count int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM coding_quota_capacity_events`).Scan(&count))
	require.Equal(t, 1, count)
}
