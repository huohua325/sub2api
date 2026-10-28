package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// Concurrent upstream replicas can return lower cumulative readings even when
// their HTTP Date is newer. Recovering to the high-water mark is not new usage.
func TestCodingQuotaDBConcurrentSnapshotRegressionDoesNotRecharge(t *testing.T) {
	for _, purpose := range []string{service.PurposeOpenAI, service.PurposeGrok} {
		t.Run(purpose, func(t *testing.T) {
			db, r, base := quotaTestDB(t, "")
			ctx := context.Background()
			end := base.Add(7 * 24 * time.Hour)
			observe := func(at time.Time, used string) {
				require.NoError(t, r.ObserveCodingQuota(ctx, service.OfficialQuotaObservation{
					AccountID: 10, Purpose: purpose, WindowEnd: end, ObservedAt: at, Used: used, Source: "response_headers",
				}))
				require.NoError(t, r.ReconcileCodingQuota(ctx, at))
			}
			observe(base, "68")
			for i, used := range []string{"69", "68", "69", "70"} {
				at := base.Add(time.Duration(i+1) * time.Minute)
				// Two users and two keys share one upstream meter.
				quotaRequest(t, db, "parallel-a-"+used+at.String(), 1, 10, at.Add(-time.Second), "1", purpose)
				quotaRequest(t, db, "parallel-b-"+used+at.String(), 2, 10, at.Add(-time.Second), "1", purpose)
				observe(at, used)
			}
			// Eight-place rounding assigns the residual to the last request;
			// both users together must consume exactly two official points.
			require.Equal(t, "1.00000001", quotaUsed(t, r, 1, base.Add(time.Hour), purpose).Used)
			require.Equal(t, "0.99999999", quotaUsed(t, r, 2, base.Add(time.Hour), purpose).Used)
			events, err := r.ListCodingResets(ctx)
			require.NoError(t, err)
			require.Empty(t, events, "a same-window regression is not reset evidence")
		})
	}
}

// A tiny completed request must not make the last one-percent tick the rate
// for every other key. All pending estimates must use the same pooled sample.
func TestCodingQuotaDBConcurrentCalibrationRepricesPending(t *testing.T) {
	for _, tc := range []struct{ name, firstWeight, secondWeight string }{
		{"small last sample", "1", "0.01"},
		{"recover from small first sample", "0.01", "1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, r, base := quotaTestDB(t, "")
			ctx := context.Background()
			end := base.Add(7 * 24 * time.Hour)
			quotaObserve(t, r, 10, base, end, "10")
			require.NoError(t, r.ReconcileCodingQuota(ctx, base))
			quotaRequest(t, db, "sample-1", 1, 10, base.Add(time.Minute), tc.firstWeight, "openai")
			quotaObserve(t, r, 10, base.Add(2*time.Minute), end, "11")
			require.NoError(t, r.ReconcileCodingQuota(ctx, base.Add(2*time.Minute)))
			// Already queued when the next observation is reconciled, but beyond
			// its boundary, so this request must remain pending, not settled.
			quotaRequest(t, db, "pending-old-rate", 2, 10, base.Add(7*time.Minute), "1", "openai")
			quotaRequest(t, db, "sample-2", 1, 10, base.Add(5*time.Minute), tc.secondWeight, "openai")
			quotaObserve(t, r, 10, base.Add(6*time.Minute), end, "12")
			require.NoError(t, r.ReconcileCodingQuota(ctx, base.Add(6*time.Minute)))
			quotaRequest(t, db, "pending-new-rate", 2, 10, base.Add(8*time.Minute), "1", "openai")
			reading := quotaUsed(t, r, 2, base.Add(time.Hour), "openai")
			require.Equal(t, 2, reading.PendingCount)
			require.Equal(t, "3.96039604", reading.Pending, "2 points / 1.01 total weight, applied to each weight-1 pending request")
			require.Equal(t, "2.00000000", quotaUsed(t, r, 1, base.Add(time.Hour), "openai").Used)
		})
	}
}

func TestCodingQuotaDBNewOfficialWindowDoesNotReuseOldCalibration(t *testing.T) {
	db, r, base := quotaTestDB(t, "")
	ctx := context.Background()
	end := base.Add(7 * 24 * time.Hour)
	quotaObserve(t, r, 10, base, end, "50")
	require.NoError(t, r.ReconcileCodingQuota(ctx, base))
	quotaRequest(t, db, "old-window", 1, 10, base.Add(time.Minute), "0.01", "openai")
	quotaObserve(t, r, 10, base.Add(2*time.Minute), end, "51")
	require.NoError(t, r.ReconcileCodingQuota(ctx, base.Add(2*time.Minute)))
	quotaObserve(t, r, 10, base.Add(3*time.Minute), end.Add(24*time.Hour), "0")
	require.NoError(t, r.ReconcileCodingQuota(ctx, base.Add(3*time.Minute)))
	quotaRequest(t, db, "new-window", 2, 10, base.Add(4*time.Minute), "1", "openai")
	reading := quotaUsed(t, r, 2, base.Add(time.Hour), "openai")
	require.Equal(t, "0.00000000", reading.Pending, "unknown new-window rate cannot inherit an old spike")
	require.Equal(t, 1, reading.PendingCount, "zero estimate is explicitly unresolved, not free settled usage")
}

func TestCodingQuotaDBParallelKeysAndReconcilerShareCalibration(t *testing.T) {
	db, r, base := quotaTestDB(t, "")
	ctx := context.Background()
	end := base.Add(7 * 24 * time.Hour)
	quotaObserve(t, r, 10, base, end, "10")
	require.NoError(t, r.ReconcileCodingQuota(ctx, base))
	quotaRequest(t, db, "sample-1", 1, 10, base.Add(time.Minute), "1", "openai")
	quotaObserve(t, r, 10, base.Add(2*time.Minute), end, "11")
	require.NoError(t, r.ReconcileCodingQuota(ctx, base.Add(2*time.Minute)))
	quotaRequest(t, db, "sample-2", 1, 10, base.Add(5*time.Minute), "0.01", "openai")
	quotaObserve(t, r, 10, base.Add(6*time.Minute), end, "12")
	start := make(chan struct{})
	failures := make(chan error, 21)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(key int64) {
			defer wg.Done()
			<-start
			cmd := &service.UsageBillingCommand{RequestID: "shared-id", APIKeyID: key, UserID: 2, AccountID: 10,
				CodingQuota: &service.CodingQuotaCharge{AccountID: 10, Purpose: "openai", Weight: "1",
					StartedAt: base.Add(7 * time.Minute), CompletedAt: base.Add(8 * time.Minute)}}
			billing := &usageBillingRepository{db: db}
			for attempt := 0; attempt < 2; attempt++ { // Billing replay on each key.
				if _, err := billing.Apply(ctx, cmd); err != nil {
					failures <- fmt.Errorf("key %d: %w", key, err)
					return
				}
			}
		}(int64(100 + i))
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		failures <- r.ReconcileCodingQuota(ctx, base.Add(6*time.Minute))
	}()
	close(start)
	wg.Wait()
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	reading := quotaUsed(t, r, 2, base.Add(time.Hour), "openai")
	require.Equal(t, 20, reading.PendingCount)
	require.Equal(t, "39.60396040", reading.Pending)
	var keys int
	require.NoError(t, db.QueryRow(`SELECT count(DISTINCT api_key_id) FROM coding_quota_requests WHERE user_id=2`).Scan(&keys))
	require.Equal(t, 20, keys)
}

func TestCodingQuotaDBFlatObservationRepairsLegacyPendingRate(t *testing.T) {
	db, r, base := quotaTestDB(t, "")
	ctx := context.Background()
	end := base.Add(7 * 24 * time.Hour)
	quotaObserve(t, r, 10, base, end, "10")
	require.NoError(t, r.ReconcileCodingQuota(ctx, base))
	quotaRequest(t, db, "sample-1", 1, 10, base.Add(time.Minute), "1", "openai")
	quotaObserve(t, r, 10, base.Add(2*time.Minute), end, "11")
	require.NoError(t, r.ReconcileCodingQuota(ctx, base.Add(2*time.Minute)))
	quotaRequest(t, db, "sample-2", 1, 10, base.Add(3*time.Minute), "0.01", "openai")
	quotaObserve(t, r, 10, base.Add(4*time.Minute), end, "12")
	require.NoError(t, r.ReconcileCodingQuota(ctx, base.Add(4*time.Minute)))
	// Persisted values from the previous program: latest-tick rate and a
	// frozen pending estimate. No new paid usage should be needed to recover.
	_, err := db.Exec(`UPDATE coding_quota_meters SET points_per_weight=100 WHERE account_id=10`)
	require.NoError(t, err)
	quotaRequest(t, db, "legacy-pending", 2, 10, base.Add(5*time.Minute), "1", "openai")
	require.Equal(t, "100.00000000", quotaUsed(t, r, 2, base.Add(time.Hour), "openai").Pending)
	quotaObserve(t, r, 10, base.Add(6*time.Minute), end, "12")
	require.NoError(t, r.ReconcileCodingQuota(ctx, base.Add(6*time.Minute)))
	reading := quotaUsed(t, r, 2, base.Add(time.Hour), "openai")
	require.Equal(t, "1.98019802", reading.Pending)
	require.Equal(t, 1, reading.PendingCount)
	require.Equal(t, "2.00000000", quotaUsed(t, r, 1, base.Add(time.Hour), "openai").Used)
}
