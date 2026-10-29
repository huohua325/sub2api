package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCodingQuotaDBFlatObservationRefreshesAdmissionWithoutMovingSettlement(t *testing.T) {
	db, r, _ := quotaTestDB(t, "")
	now := time.Now().UTC().Truncate(time.Second)
	baseline, end := now.Add(-2*time.Hour), now.Add(24*time.Hour)
	_, err := db.Exec(`INSERT INTO coding_quota_meters(account_id,purpose,window_end,observed_at,used,points_per_weight) VALUES(10,'openai',$1,$2,20,1)`, end, baseline)
	require.NoError(t, err)
	for i := 0; i < 20; i++ {
		_, err = db.Exec(`INSERT INTO coding_quota_requests(request_id,api_key_id,user_id,purpose,account_id,started_at,completed_at,official_window_end,weight,points,state) VALUES($1,2,2,'openai',10,$2,$2,$3,1,1,'settled')`, fmt.Sprintf("sample-%d", i), baseline.Add(-time.Minute), end)
		require.NoError(t, err)
	}
	_, err = db.Exec(`INSERT INTO coding_quota_requests(request_id,api_key_id,user_id,purpose,account_id,started_at,completed_at,official_window_end,weight,state) VALUES('waiting',1,1,'openai',10,$1,$1,$2,2,'pending')`, now.Add(-40*time.Minute), end)
	require.NoError(t, err)
	fresh := now.Add(-time.Minute)
	require.NoError(t, r.ObserveCodingQuota(context.Background(), service.OfficialQuotaObservation{AccountID: 10, Purpose: "openai", WindowEnd: end, ObservedAt: fresh, Used: "20", Source: "codex_7d"}))
	require.NoError(t, r.ReconcileCodingQuota(context.Background(), now))
	in := reservationInput(1, 10)
	in.Now = now
	in.StartedAt = now
	got, err := r.ReserveCodingQuota(context.Background(), in)
	require.NoError(t, err)
	require.True(t, got.Reliable, "a current flat official observation is fresh evidence")
	require.Equal(t, "normal", got.AdmissionMode)
	require.WithinDuration(t, fresh, *got.ObservedAt, time.Millisecond)
	var cursor time.Time
	require.NoError(t, db.QueryRow(`SELECT observed_at FROM coding_quota_meters WHERE account_id=10`).Scan(&cursor))
	require.True(t, baseline.Equal(cursor), "freshness must not skip the unsettled interval")
	next := now.Add(-time.Second)
	require.NoError(t, r.ObserveCodingQuota(context.Background(), service.OfficialQuotaObservation{AccountID: 10, Purpose: "openai", WindowEnd: end, ObservedAt: next, Used: "22", Source: "codex_7d"}))
	require.NoError(t, r.ReconcileCodingQuota(context.Background(), now))
	var state, points string
	require.NoError(t, db.QueryRow(`SELECT state,points::text FROM coding_quota_requests WHERE request_id='waiting'`).Scan(&state, &points))
	require.Equal(t, "settled", state)
	require.Equal(t, "2.00000000", points)
}

func TestCodingQuotaDBUntrustedObservationsCannotRefreshAdmission(t *testing.T) {
	for _, tc := range []struct {
		name, used    string
		shift         time.Duration
		processed     bool
		observedShift time.Duration
	}{
		{"lower_reading", "19", 0, true, -time.Minute},
		{"different_window", "20", -24 * time.Hour, true, -time.Minute},
		{"unprocessed", "20", 0, false, -time.Minute},
		{"future_reading", "20", 0, true, 5 * time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, r, _ := quotaTestDB(t, "")
			now := time.Now().UTC().Truncate(time.Second)
			end := now.Add(time.Hour)
			_, err := db.Exec(`INSERT INTO coding_quota_meters(account_id,purpose,window_end,observed_at,used,points_per_weight) VALUES(10,'openai',$1,$2,20,1)`, end, now.Add(-time.Hour))
			require.NoError(t, err)
			_, err = db.Exec(`INSERT INTO coding_quota_requests(request_id,api_key_id,user_id,purpose,account_id,started_at,completed_at,official_window_end,weight,points,state) SELECT 'sample-'||i,2,2,'openai',10,$1,$1,$2,1,1,'settled' FROM generate_series(1,20) i`, now.Add(-time.Hour), end)
			require.NoError(t, err)
			_, err = db.Exec(`INSERT INTO coding_quota_observations(account_id,purpose,window_end,observed_at,used,source,processed) VALUES(10,'openai',$1,$2,$3,'codex_7d',$4)`, end.Add(tc.shift), now.Add(tc.observedShift), tc.used, tc.processed)
			require.NoError(t, err)
			in := reservationInput(1, 10)
			in.Now = now
			got, err := r.ReserveCodingQuota(context.Background(), in)
			require.NoError(t, err)
			require.False(t, got.Reliable)
			require.Equal(t, "single", got.AdmissionMode)
			require.Equal(t, "observation_stale", got.Reason)
		})
	}
}
