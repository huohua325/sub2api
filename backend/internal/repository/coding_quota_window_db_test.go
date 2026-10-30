package repository

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCodingQuotaDBEndDriftPreservesCounterHighWater(t *testing.T) {
	for _, returns := range []bool{false, true} {
		t.Run(map[bool]string{false: "persistent drift", true: "returns to original"}[returns], func(t *testing.T) {
			db, r, base := quotaTestDB(t, "")
			ctx := context.Background()
			end := base.Add(6 * 24 * time.Hour)
			observe := func(at, until time.Time, used string) {
				quotaObserve(t, r, 10, at, until, used)
				require.NoError(t, r.ReconcileCodingQuota(ctx, at))
			}
			observe(base, end, "31")
			quotaRequest(t, db, "regression", 1, 10, base.Add(time.Minute), "1", "openai")
			observe(base.Add(2*time.Minute), end, "16")
			observe(base.Add(3*time.Minute), end.Add(8*time.Minute), "21")
			until := end.Add(8 * time.Minute)
			if returns {
				until = end
			}
			observe(base.Add(4*time.Minute), until, "21")
			quotaRequest(t, db, "below-old-water", 2, 10, base.Add(5*time.Minute), "1", "openai")
			observe(base.Add(6*time.Minute), until, "22")
			require.Equal(t, "0.00000000", quotaUsed(t, r, 2, base.Add(time.Hour), "openai").Used)
			var high string
			require.NoError(t, db.QueryRow(`SELECT used::text FROM coding_quota_meters WHERE account_id=10`).Scan(&high))
			require.Equal(t, "31.00000000", high)
			events, err := r.ListCodingResets(ctx)
			require.NoError(t, err)
			require.Empty(t, events, "a reset whose implied start predates the known counter is not a new cycle")
		})
	}
}
