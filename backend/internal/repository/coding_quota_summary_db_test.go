package repository

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestCodingQuotaDBReservationMonitorDoesNotChangeStudentPercent(t *testing.T) {
	_, r, _ := quotaTestDB(t, "")
	ctx := context.Background()
	in := reservationInput(1, 10)
	before := quotaUsed(t, r, 1, in.Now, "openai")
	_, err := r.ReserveCodingQuota(ctx, in)
	require.NoError(t, err)
	rows, err := r.ReadCodingReservationSummaries(ctx, []int64{1}, in.Now)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	var got service.CodingReservationSummary
	for _, v := range rows {
		if v.Purpose == "openai" {
			got = v
		}
	}
	require.Equal(t, 1, got.Active)
	require.Equal(t, "single", got.AdmissionMode)
	require.Equal(t, before.Used, got.Settled)
	after := quotaUsed(t, r, 1, in.Now, "openai")
	require.Equal(t, before.Pending, after.Pending)
	for i := 0; i < 2; i++ {
		other := reservationInput(2, 10)
		other.Mode = "shadow"
		_, err = r.ReserveCodingQuota(ctx, other)
		require.NoError(t, err)
	}
	page, err := r.ListCodingReservations(ctx, service.CodingReservationQuery{UserID: 2, Limit: 1})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.NotEmpty(t, page.NextCursor)
	next, err := r.ListCodingReservations(ctx, service.CodingReservationQuery{UserID: 2, Limit: 1, Before: page.NextCursor})
	require.NoError(t, err)
	require.Len(t, next.Items, 1)
	require.NotEqual(t, page.Items[0].ID, next.Items[0].ID)
	require.NoError(t, r.MarkCodingReservationSent(ctx, in.ID, in.Now))
	require.NoError(t, r.RecoverCodingReservations(ctx, in.Now.Add(3*time.Minute)))
	rows, err = r.ReadCodingReservationSummaries(ctx, []int64{1}, in.Now.Add(3*time.Minute))
	require.NoError(t, err)
	for _, v := range rows {
		if v.Purpose == "openai" {
			require.Zero(t, v.Active)
			require.Equal(t, 1, v.Unresolved)
			require.Equal(t, "single", v.AdmissionMode)
		}
	}
}
