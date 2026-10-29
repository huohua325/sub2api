package repository

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
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

func TestCodingQuotaDBSummaryUsesFreshObservationAndPreservesPeriodCounts(t *testing.T) {
	db, r, _ := quotaTestDB(t, "")
	now := time.Now().UTC().Truncate(time.Second)
	end := now.Add(time.Hour)
	old := now.Add(-time.Hour)
	_, err := db.Exec(`INSERT INTO coding_quota_meters(account_id,purpose,window_end,observed_at,used,points_per_weight) VALUES(10,'openai',$1,$2,20,1)`, end, old)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO coding_quota_requests(request_id,api_key_id,user_id,purpose,account_id,started_at,completed_at,official_window_end,weight,points,state) SELECT 's-'||i,2,2,'openai',10,$1,$1,$2,1,1,'settled' FROM generate_series(1,20) i`, old, end)
	require.NoError(t, err)
	in := reservationInput(1, 10)
	in.Now = old
	in.StartedAt = old
	got, err := r.ReserveCodingQuota(context.Background(), in)
	require.NoError(t, err)
	require.True(t, got.Reliable)
	_, err = db.Exec(`INSERT INTO coding_quota_observations(account_id,purpose,window_end,observed_at,used,source,processed) VALUES(10,'openai',$1,$2,20,'codex_7d',true)`, end, now.Add(-time.Minute))
	require.NoError(t, err)
	rows, err := r.ReadCodingReservationSummaries(context.Background(), []int64{1, 2}, now)
	require.NoError(t, err)
	require.Len(t, rows, 4)
	require.Equal(t, int64(1), rows[1].UserID)
	require.Equal(t, "openai", rows[1].Purpose)
	require.True(t, rows[1].Reliable, "a flat current observation is not stale")
	require.Equal(t, 1, rows[1].Active)
	require.Equal(t, "1.00000000", rows[1].Reserved)
	require.WithinDuration(t, now.Add(-time.Minute), *rows[1].ObservedAt, time.Millisecond)
	require.Equal(t, "0", rows[0].Reserved)
}

func TestCodingQuotaDBSummaryKeepsOldSlotsAndTasksButNotOldAmounts(t *testing.T) {
	db, r, _ := quotaTestDB(t, "")
	now := time.Now().UTC()
	period := quotaUsed(t, r, 1, now, "openai")
	_, err := db.Exec(`INSERT INTO coding_quota_reservations(id,user_id,api_key_id,account_id,purpose,model,mode,state,admission_mode,reason,estimated_points,reliable,would_allow,started_at,heartbeat_at,expires_at)
 SELECT id,u,1,10,p,'gpt-5','shadow',s,'single','fixture',e,false,true,at,$1,$1::timestamptz+INTERVAL '2 minutes'
 FROM(VALUES('current',1,'openai','reserved',2,$1::timestamptz),('old-sent',1,'openai','sent',99,$2::timestamptz),
 ('old-unresolved',1,'openai','unresolved',77,$2::timestamptz),('current-unresolved',1,'openai','unresolved',3,$1::timestamptz),
 ('other-user',2,'openai','reserved',5,$1::timestamptz),('other-purpose',1,'grok','reserved',7,$1::timestamptz)) v(id,u,p,s,e,at)`, now, period.Start.Add(-time.Hour))
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO coding_quota_billing_tasks(reservation_id,request_id,api_key_id,fingerprint,command,state) VALUES('current','b-current',1,'f','{}','pending'),('old-sent','b-old',1,'f','{}','pending'),('current-unresolved','b-failed',1,'f','{}','failed')`)
	require.NoError(t, err)
	rows, err := r.ReadCodingReservationSummaries(context.Background(), []int64{1, 2}, now)
	require.NoError(t, err)
	for _, row := range rows {
		switch {
		case row.UserID == 1 && row.Purpose == "openai":
			require.Equal(t, "5.00000000", row.Reserved)
			require.Equal(t, 2, row.Active)
			require.Equal(t, 1, row.Unresolved)
			require.Equal(t, 2, row.PendingTasks)
			require.Equal(t, 1, row.FailedTasks)
		case row.UserID == 1 && row.Purpose == "grok":
			require.Equal(t, "7.00000000", row.Reserved)
			require.Equal(t, 1, row.Active)
			require.Zero(t, row.Unresolved)
			require.Zero(t, row.PendingTasks)
		case row.UserID == 2 && row.Purpose == "openai":
			require.Equal(t, "5.00000000", row.Reserved)
			require.Equal(t, 1, row.Active)
			require.Zero(t, row.PendingTasks)
		}
	}
}
