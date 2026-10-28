package repository

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func reservationInput(user, parent int64) service.CodingReservationInput {
	return service.CodingReservationInput{ID: uuid.NewString(), UserID: user, APIKeyID: user,
		AccountID: parent, Purpose: "openai", StartedAt: time.Now().UTC(), Now: time.Now().UTC(),
		Mode: "enforce", Limit: "50", Weight: "1", Model: "gpt-5", MinSamples: 20, MaxAge: 20 * time.Minute}
}

func TestCodingQuotaDBReservationConcurrentSingleMode(t *testing.T) {
	db, r, _ := quotaTestDB(t, "")
	db.SetMaxOpenConns(20)
	var wg sync.WaitGroup
	outcomes := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := r.ReserveCodingQuota(context.Background(), reservationInput(1, int64(10+10*(i%2))))
			outcomes <- err
		}(i)
	}
	wg.Wait()
	close(outcomes)
	accepted, busy := 0, 0
	for err := range outcomes {
		if err == nil {
			accepted++
		} else {
			require.ErrorIs(t, err, service.ErrCodingQuotaBusy)
			busy++
		}
	}
	require.Equal(t, 1, accepted)
	require.Equal(t, 19, busy)
	other, err := r.ReserveCodingQuota(context.Background(), reservationInput(2, 10))
	require.NoError(t, err)
	require.True(t, other.WouldAllow)
}

func TestCodingQuotaDBReservationShadowNeverRejectsOrChangesPercent(t *testing.T) {
	_, r, _ := quotaTestDB(t, "")
	before := quotaUsed(t, r, 1, time.Now(), "openai")
	for i := 0; i < 3; i++ {
		in := reservationInput(1, 10)
		in.Mode = "shadow"
		got, err := r.ReserveCodingQuota(context.Background(), in)
		require.NoError(t, err)
		require.Equal(t, i == 0, got.WouldAllow)
	}
	after := quotaUsed(t, r, 1, time.Now(), "openai")
	require.Equal(t, before.Used, after.Used)
	require.Equal(t, before.Pending, after.Pending)
}

func TestCodingQuotaDBReservationConfirmedExhaustionAndEstimatedSingleMode(t *testing.T) {
	db, r, base := quotaTestDB(t, "")
	_, err := db.Exec(`INSERT INTO coding_quota_requests(request_id,api_key_id,user_id,purpose,started_at,completed_at,weight,points,estimated_points,state)
 VALUES('true-exhaustion',1,1,'openai',$1,$1,1,50.5,0,'settled'),('estimate-only',2,2,'openai',$1,$1,1,0,90,'pending')`, base.Add(time.Hour))
	require.NoError(t, err)
	_, err = r.ReserveCodingQuota(context.Background(), reservationInput(1, 10))
	require.ErrorIs(t, err, service.ErrCodingQuotaExhausted)
	got, err := r.ReserveCodingQuota(context.Background(), reservationInput(2, 10))
	require.NoError(t, err)
	require.Equal(t, "single", got.AdmissionMode)
	_, err = r.ReserveCodingQuota(context.Background(), reservationInput(2, 20))
	require.ErrorIs(t, err, service.ErrCodingQuotaBusy)
}

func TestCodingQuotaDBReservationReliableBudgetRetryAndNoUserMultiplier(t *testing.T) {
	db, r, _ := quotaTestDB(t, "")
	now := time.Now().UTC()
	end := now.Add(time.Hour)
	_, err := db.Exec(`INSERT INTO coding_quota_meters(account_id,purpose,window_end,observed_at,used,points_per_weight) VALUES(10,'openai',$1,$2,20,1)`, end, now)
	require.NoError(t, err)
	for i := 0; i < 20; i++ {
		_, err = db.Exec(`INSERT INTO coding_quota_requests(request_id,api_key_id,user_id,purpose,account_id,started_at,completed_at,official_window_end,weight,points,state)
 VALUES($1,2,2,'openai',10,$2,$2,$3,1,1,'settled')`, fmt.Sprintf("sample-%d", i), now, end)
		require.NoError(t, err)
	}
	in := reservationInput(1, 10)
	in.Weight = "2"
	a, err := r.ReserveCodingQuota(context.Background(), in)
	require.NoError(t, err)
	require.Equal(t, "normal", a.AdmissionMode)
	require.Equal(t, "2.00000000", a.EstimatedPoints)
	again, err := r.ReserveCodingQuota(context.Background(), in)
	require.NoError(t, err)
	require.Equal(t, a.ID, again.ID)
	b, err := r.ReserveCodingQuota(context.Background(), reservationInput(1, 10))
	require.NoError(t, err)
	require.True(t, b.WouldAllow)
	var count int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM coding_quota_reservations`).Scan(&count))
	require.Equal(t, 2, count)
	in.ID = uuid.NewString()
	in.Now = now.Add(21 * time.Minute)
	// Keep active leases alive while advancing the observation clock.
	for at := now.Add(time.Minute); !at.After(in.Now); at = at.Add(time.Minute) {
		require.NoError(t, r.RenewCodingReservation(context.Background(), a.ID, at))
		require.NoError(t, r.RenewCodingReservation(context.Background(), b.ID, at))
	}
	_, err = r.ReserveCodingQuota(context.Background(), in)
	require.ErrorIs(t, err, service.ErrCodingQuotaBusy, "stale observations cannot retain normal mode")
}

func TestCodingQuotaDBReservationUnknownExpiryAndLateResult(t *testing.T) {
	_, r, _ := quotaTestDB(t, "")
	in := reservationInput(1, 10)
	hold, err := r.ReserveCodingQuota(context.Background(), in)
	require.NoError(t, err)
	require.NoError(t, r.MarkCodingReservationSent(context.Background(), hold.ID, in.Now))
	require.NoError(t, r.RecoverCodingReservations(context.Background(), in.Now.Add(121*time.Second)))
	rows, err := r.ListCodingReservations(context.Background(), service.CodingReservationQuery{UserID: 1, Limit: 50})
	require.NoError(t, err)
	require.Len(t, rows.Items, 1)
	require.Equal(t, "unresolved", rows.Items[0].State)
	next := reservationInput(1, 20)
	next.Now = in.Now.Add(122 * time.Second)
	_, err = r.ReserveCodingQuota(context.Background(), next)
	require.NoError(t, err, "unknown expired stream must not permanently occupy execution slot")
	require.NoError(t, r.FinishCodingReservation(context.Background(), hold.ID, true, in.Now.Add(123*time.Second)))
}

func TestCodingQuotaDBReservationCannotReuseTerminalIdentity(t *testing.T) {
	_, r, _ := quotaTestDB(t, "")
	ctx := context.Background()
	in := reservationInput(1, 10)
	_, err := r.ReserveCodingQuota(ctx, in)
	require.NoError(t, err)
	require.NoError(t, r.FinishCodingReservation(ctx, in.ID, true, in.Now))
	_, err = r.ReserveCodingQuota(ctx, in)
	require.Error(t, err)
}

func TestCodingQuotaDBReservationUsesOriginalStartAcrossBoundary(t *testing.T) {
	db, r, base := quotaTestDB(t, "")
	ctx := context.Background()
	_, err := db.Exec(`UPDATE coding_quota_requests SET points=50 WHERE user_id=1 AND purpose='openai' AND state='imported'`)
	require.NoError(t, err)
	// Existing opening balance is imported only at migration; add a real opening entry.
	_, err = db.Exec(`INSERT INTO coding_quota_requests(request_id,api_key_id,user_id,account_id,purpose,weight,points,state,started_at,completed_at) VALUES('opening',1,1,10,'openai',0,50,'imported',$1,$1)`, base)
	require.NoError(t, err)
	in := reservationInput(1, 10)
	in.StartedAt = base.Add(7*24*time.Hour - time.Second)
	in.Now = base.Add(7*24*time.Hour + time.Second)
	_, err = r.ReserveCodingQuota(ctx, in)
	require.ErrorIs(t, err, service.ErrCodingQuotaExhausted, "a retry cannot escape the request's exhausted original cycle")
}

func TestCodingQuotaDBReservationAdmissionLatency(t *testing.T) {
	_, r, _ := quotaTestDB(t, "")
	ctx := context.Background()
	durations := make([]time.Duration, 200)
	for i := range durations {
		in := reservationInput(1, 10)
		start := time.Now()
		_, err := r.ReserveCodingQuota(ctx, in)
		durations[i] = time.Since(start)
		require.NoError(t, err)
		require.NoError(t, r.FinishCodingReservation(ctx, in.ID, true, time.Now()))
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	t.Logf("ADMISSION_LATENCY samples=200 p95=%s p99=%s", durations[189], durations[197])
}

func TestCodingQuotaDBReservationUnknownActiveEstimateBlocksNormal(t *testing.T) {
	db, r, _ := quotaTestDB(t, "")
	ctx := context.Background()
	now := time.Now().UTC()
	end := now.Add(time.Hour)
	_, err := db.Exec(`INSERT INTO coding_quota_meters(account_id,purpose,window_end,observed_at,used,points_per_weight) VALUES(10,'openai',$1,$2,20,1)`, end, now)
	require.NoError(t, err)
	for i := 0; i < 20; i++ {
		_, err = db.Exec(`INSERT INTO coding_quota_requests(request_id,api_key_id,user_id,purpose,account_id,started_at,completed_at,official_window_end,weight,points,state) VALUES($1,2,2,'openai',10,$2,$2,$3,1,1,'settled')`, fmt.Sprint("rate-", i), now, end)
		require.NoError(t, err)
	}
	in := reservationInput(1, 10)
	in.Weight = ""
	_, err = r.ReserveCodingQuota(ctx, in)
	require.NoError(t, err)
	_, err = r.ReserveCodingQuota(ctx, reservationInput(1, 10))
	require.ErrorIs(t, err, service.ErrCodingQuotaBusy)
}
func TestCodingQuotaDBReservationExpiredCannotResumeSending(t *testing.T) {
	_, r, _ := quotaTestDB(t, "")
	ctx := context.Background()
	in := reservationInput(1, 10)
	_, err := r.ReserveCodingQuota(ctx, in)
	require.NoError(t, err)
	at := in.Now.Add(121 * time.Second)
	require.NoError(t, r.RecoverCodingReservations(ctx, at))
	other := reservationInput(1, 20)
	other.Now = at
	_, err = r.ReserveCodingQuota(ctx, other)
	require.NoError(t, err)
	require.Error(t, r.MarkCodingReservationSent(ctx, in.ID, at))
}
func TestCodingQuotaDBReservationExpiredHandoffFreesSlotButKeepsTask(t *testing.T) {
	db, r, _ := quotaTestDB(t, "")
	ctx := context.Background()
	in := reservationInput(1, 10)
	_, err := r.ReserveCodingQuota(ctx, in)
	require.NoError(t, err)
	cmd := &service.UsageBillingCommand{RequestID: "handoff-expiry", APIKeyID: 1, UserID: 1, AccountID: 10, CodingQuota: &service.CodingQuotaCharge{AccountID: 10, Purpose: "openai", Weight: "1", StartedAt: in.StartedAt, CompletedAt: in.Now}}
	require.NoError(t, r.EnqueueCodingBilling(ctx, in.ID, cmd))
	at := in.Now.Add(121 * time.Second)
	require.NoError(t, r.RecoverCodingReservations(ctx, at))
	var state string
	require.NoError(t, db.QueryRow(`SELECT state FROM coding_quota_reservations WHERE id=$1`, in.ID).Scan(&state))
	require.Equal(t, "unresolved", state)
	other := reservationInput(1, 20)
	other.Now = at
	_, err = r.ReserveCodingQuota(ctx, other)
	require.NoError(t, err)
	n, err := r.ReplayCodingBilling(ctx, at, 50)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.NoError(t, db.QueryRow(`SELECT state FROM coding_quota_reservations WHERE id=$1`, in.ID).Scan(&state))
	require.Equal(t, "captured", state)
}

func TestCodingQuotaDBReservationExpiredHeartbeatCannotRevive(t *testing.T) {
	_, r, _ := quotaTestDB(t, "")
	ctx := context.Background()
	in := reservationInput(1, 10)
	_, err := r.ReserveCodingQuota(ctx, in)
	require.NoError(t, err)
	at := in.Now.Add(121 * time.Second)
	require.Error(t, r.RenewCodingReservation(ctx, in.ID, at))
	require.Error(t, r.MarkCodingReservationSent(ctx, in.ID, at))
}
