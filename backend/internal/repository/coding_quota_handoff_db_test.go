package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCodingQuotaDBReservationDurableHandoffAndRestart(t *testing.T) {
	db, r, _ := quotaTestDB(t, "")
	ctx := context.Background()
	in := reservationInput(1, 10)
	hold, err := r.ReserveCodingQuota(ctx, in)
	require.NoError(t, err)
	require.NoError(t, r.MarkCodingReservationSent(ctx, hold.ID, in.Now))
	cmd := &service.UsageBillingCommand{RequestID: "durable-reply", APIKeyID: 1, UserID: 1, AccountID: 10,
		CodingQuota: &service.CodingQuotaCharge{AccountID: 10, Purpose: "openai", Weight: "3", StartedAt: in.StartedAt, CompletedAt: in.Now.Add(time.Second)}}
	require.NoError(t, r.EnqueueCodingBilling(ctx, hold.ID, cmd))
	var state string
	require.NoError(t, db.QueryRow(`SELECT state FROM coding_quota_reservations WHERE id=$1`, hold.ID).Scan(&state))
	require.Equal(t, "handoff", state)
	require.NoError(t, r.FinishCodingReservation(ctx, hold.ID, true, time.Now()))
	require.NoError(t, db.QueryRow(`SELECT state FROM coding_quota_reservations WHERE id=$1`, hold.ID).Scan(&state))
	require.Equal(t, "handoff", state, "handler cleanup cannot undo durable handoff")
	fresh := NewUsageMonitorRepository(db)
	n, err := fresh.ReplayCodingBilling(ctx, time.Now().Add(time.Minute), 50)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	n, err = fresh.ReplayCodingBilling(ctx, time.Now().Add(2*time.Minute), 50)
	require.NoError(t, err)
	require.Zero(t, n)
	require.NoError(t, db.QueryRow(`SELECT state FROM coding_quota_reservations WHERE id=$1`, hold.ID).Scan(&state))
	require.Equal(t, "captured", state)
	var count int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM coding_quota_requests WHERE request_id='durable-reply'`).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, r.EnqueueCodingBilling(ctx, hold.ID, cmd), "same exact command remains idempotent after replay")
	changed := *cmd
	charge := *cmd.CodingQuota
	charge.Weight = "9"
	changed.CodingQuota = &charge
	require.Error(t, r.EnqueueCodingBilling(ctx, hold.ID, &changed), "same billing key cannot overwrite command evidence")
}

func TestCodingQuotaDBReservationFailedCaptureHasNoGap(t *testing.T) {
	db, r, _ := quotaTestDB(t, "")
	ctx := context.Background()
	in := reservationInput(1, 10)
	hold, err := r.ReserveCodingQuota(ctx, in)
	require.NoError(t, err)
	cmd := &service.UsageBillingCommand{RequestID: "retry-reply", APIKeyID: 1, UserID: 1, AccountID: 10,
		CodingQuota: &service.CodingQuotaCharge{AccountID: 10, Purpose: "openai", Weight: "1", StartedAt: in.StartedAt, CompletedAt: in.Now}}
	require.NoError(t, r.EnqueueCodingBilling(ctx, hold.ID, cmd))
	_, err = db.Exec(`CREATE FUNCTION reject_charge() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'simulated database write failure'; END $$;
 CREATE TRIGGER reject_charge BEFORE INSERT ON coding_quota_requests FOR EACH ROW EXECUTE FUNCTION reject_charge()`)
	require.NoError(t, err)
	n, err := r.ReplayCodingBilling(ctx, in.Now.Add(time.Minute), 50)
	require.NoError(t, err)
	require.Zero(t, n)
	_, err = r.ReserveCodingQuota(ctx, reservationInput(1, 20))
	require.ErrorIs(t, err, service.ErrCodingQuotaBusy)
	var state string
	require.NoError(t, db.QueryRow(`SELECT state FROM coding_quota_reservations WHERE id=$1`, hold.ID).Scan(&state))
	require.Equal(t, "handoff", state)
	_, err = db.Exec(`DROP TRIGGER reject_charge ON coding_quota_requests`)
	require.NoError(t, err)
	n, err = r.ReplayCodingBilling(ctx, in.Now.Add(time.Hour), 50)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	_, err = r.ReserveCodingQuota(ctx, reservationInput(1, 20))
	require.NoError(t, err)
}

func TestCodingQuotaDBAsyncMediaEvidenceSurvivesCacheLoss(t *testing.T) {
	_, r, _ := quotaTestDB(t, "")
	ctx := context.Background()
	in := reservationInput(1, 10)
	_, err := r.ReserveCodingQuota(ctx, in)
	require.NoError(t, err)
	v := service.GrokVideoPendingBilling{Model: "grok-imagine-video", CodingReservationID: in.ID, VideoDurationSeconds: 8, VideoResolution: "720p"}
	require.NoError(t, r.StoreCodingAsyncJob(ctx, "job-1", 1, 1, v))
	got, err := r.LoadCodingAsyncJob(ctx, "job-1", 1, 1)
	require.NoError(t, err)
	require.Equal(t, in.ID, got.CodingReservationID)
	other, err := r.LoadCodingAsyncJob(ctx, "job-1", 2, 2)
	require.NoError(t, err)
	require.Nil(t, other)
	v.VideoDurationSeconds = 30
	require.Error(t, r.StoreCodingAsyncJob(ctx, "job-1", 1, 1, v))
}
