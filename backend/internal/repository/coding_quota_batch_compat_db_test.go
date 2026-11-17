package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCodingQuotaDBBatchLanePreservesDurableHandoff(t *testing.T) {
	db, store, _ := quotaTestDB(t, "")
	ctx := context.Background()
	in := reservationInput(1, 10)
	hold, err := store.ReserveCodingQuota(ctx, in)
	require.NoError(t, err)
	require.NoError(t, store.MarkCodingReservationSent(ctx, hold.ID, in.Now))
	cmd := &service.UsageBillingCommand{RequestID: "batch-compatible-charge", APIKeyID: 1, UserID: 1, AccountID: 10,
		Purpose: service.PurposeOpenAI,
		CodingQuota: &service.CodingQuotaCharge{AccountID: 10, Purpose: service.PurposeOpenAI,
			Weight: "3", StartedAt: in.StartedAt, CompletedAt: in.Now},
	}
	require.NoError(t, store.EnqueueCodingBilling(ctx, hold.ID, cmd))
	repo := NewUsageBillingRepository(nil, db)
	_, err = repo.ApplyBatch(ctx, []*service.UsageBillingCommand{cmd})
	require.ErrorIs(t, err, errUsageBillingBatchIndividual)
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM usage_billing_dedup WHERE request_id=$1`, cmd.RequestID).Scan(&count))
	require.Zero(t, count, "rejected batches must not consume the dedup key")
	lanes := service.NewUsageBillingLaneCombiner(repo)
	t.Cleanup(lanes.Stop)
	result, err := lanes.Apply(ctx, cmd)
	require.NoError(t, err)
	require.True(t, result.Applied)
	result, err = lanes.Apply(ctx, cmd)
	require.NoError(t, err)
	require.False(t, result.Applied)
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM coding_quota_requests WHERE request_id=$1`, cmd.RequestID).Scan(&count))
	require.Equal(t, 1, count)
	var state string
	require.NoError(t, db.QueryRow(`SELECT state FROM coding_quota_reservations WHERE id=$1`, hold.ID).Scan(&state))
	require.Equal(t, "captured", state)
}
