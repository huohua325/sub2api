package repository

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func fallbackCodingTestCharge(t *testing.T, db *sql.DB, in service.APIFallbackCodingCharge) error {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := recordAPIFallbackCodingCharge(context.Background(), tx, in); err != nil {
		return err
	}
	return tx.Commit()
}

func TestCodingQuotaDBAPIFallbackFixedChargeConcurrentReplay(t *testing.T) {
	db, r, base := quotaTestDB(t, "")
	in := service.APIFallbackCodingCharge{RequestID: "api-fixed", APIKeyID: 1, UserID: 1, AccountID: 10,
		Purpose: "openai", Points: "0.130000004", StartedAt: base.Add(time.Minute), CompletedAt: base.Add(2 * time.Minute)}
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for range 12 {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- fallbackCodingTestCharge(t, db, in) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, "0.13000000", quotaUsed(t, r, 1, base.Add(time.Hour), "openai").Used)
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM coding_quota_requests WHERE request_id='api-fixed'`).Scan(&count))
	require.Equal(t, 1, count)
	for _, table := range []string{"coding_quota_observations", "coding_quota_meters"} {
		require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM `+table).Scan(&count))
		require.Zero(t, count)
	}
	changed := in
	changed.Points = "0.2"
	require.ErrorContains(t, fallbackCodingTestCharge(t, db, changed), "identity conflict")
	changed = in
	changed.UserID = 2
	require.ErrorContains(t, fallbackCodingTestCharge(t, db, changed), "identity conflict")
	_, err := db.Exec(`UPDATE coding_quota_requests SET points=0 WHERE request_id='api-fixed'`)
	require.ErrorContains(t, err, "append-only")
	_, err = db.Exec(`DELETE FROM coding_quota_requests WHERE request_id='api-fixed'`)
	require.ErrorContains(t, err, "append-only")
}

func TestCodingQuotaDBAPIFallbackExcludedFromSubscriptionCalibrationAndCapacity(t *testing.T) {
	db, r, base := quotaTestDB(t, "")
	ctx := context.Background()
	end := base.Add(6 * 24 * time.Hour)
	_, err := db.Exec(`INSERT INTO coding_quota_meters(account_id,purpose,window_end,observed_at,used,points_per_weight)
 VALUES(10,'openai',$1,$2,10,0.5)`, end, base)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO coding_quota_requests(request_id,api_key_id,user_id,purpose,account_id,started_at,completed_at,official_window_end,weight,points,state)
 VALUES('subscription-fixed',1,1,'openai',10,$2,$2,$1,20,10,'settled')`, end, base.Add(time.Minute))
	require.NoError(t, err)
	in := service.APIFallbackCodingCharge{RequestID: "api-capacity", APIKeyID: 1, UserID: 1, AccountID: 10,
		Purpose: "openai", Points: "7", StartedAt: base.Add(2 * time.Minute), CompletedAt: base.Add(3 * time.Minute)}
	require.NoError(t, fallbackCodingTestCharge(t, db, in))
	seedCapacityProjectionEvent(t, db, "subscription-doubled", 10, base.Add(10*time.Minute), end, "2", "0.5")
	require.Equal(t, "12.00000000", quotaUsed(t, r, 1, base.Add(time.Hour), "openai").Used,
		"subscription 10/2 plus fixed API 7; API must not be divided by capacity factor")
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	require.NoError(t, refreshCodingCalibration(ctx, tx, 10, end))
	require.NoError(t, tx.Commit())
	var rate string
	require.NoError(t, db.QueryRow(`SELECT points_per_weight::text FROM coding_quota_meters WHERE account_id=10`).Scan(&rate))
	require.Equal(t, "0.500000000000", rate, "API points with zero weight must never increase subscription calibration")
	snapshot, err := r.CodingQuotaAuditSnapshot(ctx, 10, base, base.Add(time.Hour))
	require.NoError(t, err)
	require.Len(t, snapshot.Requests, 1, "subscription reconciliation cannot create refunds for API charges")
	var source, points, state string
	require.NoError(t, db.QueryRow(`SELECT metering_source,points::text,state FROM coding_quota_requests WHERE request_id='api-capacity'`).Scan(&source, &points, &state))
	require.Equal(t, "api_fallback", source)
	require.Equal(t, "7.00000000", points)
	require.Equal(t, "settled", state)
	quotaObserve(t, r, 10, base.Add(20*time.Minute), end, "11")
	require.NoError(t, r.ReconcileCodingQuota(ctx, base.Add(time.Hour)))
	require.Equal(t, "12.00000000", quotaUsed(t, r, 1, base.Add(time.Hour), "openai").Used)
}

func TestCodingQuotaDBAPIFallbackUsesOriginalStartAcrossPeriod(t *testing.T) {
	db, r, base := quotaTestDB(t, "")
	before := service.APIFallbackCodingCharge{RequestID: "api-before-week", APIKeyID: 1, UserID: 1, AccountID: 10,
		Purpose: "openai", Points: "4", StartedAt: base.Add(168*time.Hour - time.Minute), CompletedAt: base.Add(168*time.Hour + time.Minute)}
	require.NoError(t, fallbackCodingTestCharge(t, db, before))
	require.Equal(t, "4.00000000", quotaUsed(t, r, 1, base.Add(168*time.Hour-time.Second), "openai").Used)
	require.Equal(t, "0", quotaUsed(t, r, 1, base.Add(169*time.Hour), "openai").Used)
}

func TestCodingQuotaDBAPIFallbackGlobalResetKeepsHistoricalCharges(t *testing.T) {
	db, r, base := quotaTestDB(t, "")
	for _, purpose := range []string{"openai", "grok"} {
		in := service.APIFallbackCodingCharge{RequestID: "api-reset-" + purpose, APIKeyID: 1, UserID: 1, AccountID: 10,
			Purpose: purpose, Points: "3", StartedAt: base.Add(time.Minute), CompletedAt: base.Add(11 * time.Minute)}
		require.NoError(t, fallbackCodingTestCharge(t, db, in))
	}
	event := service.CodingQuotaReset{ID: "manual:api-existing-event", EffectiveAt: base.Add(10 * time.Minute), Note: "verified global event"}
	_, err := r.ConfirmCodingReset(context.Background(), event, 1)
	require.NoError(t, err)
	require.Equal(t, "0", quotaUsed(t, r, 1, base.Add(time.Hour), "openai").Used)
	require.Equal(t, "3.00000000", quotaUsed(t, r, 1, base.Add(time.Hour), "grok").Used)
	var raw string
	require.NoError(t, db.QueryRow(`SELECT SUM(points)::text FROM coding_quota_requests WHERE metering_source='api_fallback'`).Scan(&raw))
	require.Equal(t, "6.00000000", raw, "global reset changes personal window, not historical API charges")
}

func TestCodingQuotaDBAPIFallbackPersonalWaiverRetainsLateChargeAndClock(t *testing.T) {
	db, r, _ := quotaTestDB(t, "")
	_, err := db.Exec(`ALTER TABLE users ADD COLUMN role text NOT NULL DEFAULT 'user'; UPDATE users SET role='admin' WHERE id=2`)
	require.NoError(t, err)
	now := quotaDBNow(t, db)
	before := quotaUsed(t, r, 1, now, "openai")
	in := service.APIFallbackCodingCharge{RequestID: "api-waived", APIKeyID: 1, UserID: 1, AccountID: 10,
		Purpose: "openai", Points: "8", StartedAt: now.Add(-2 * time.Minute), CompletedAt: now.Add(-time.Minute)}
	require.NoError(t, fallbackCodingTestCharge(t, db, in))
	_, err = r.ResetPersonalQuota(context.Background(), service.PersonalQuotaResetInput{ID: uuid.NewString(), ActorUserID: 2, UserID: 1,
		Purpose: "openai", PeriodEnd: before.End, Note: "explicit personal credit", Confirm: true})
	require.NoError(t, err)
	require.Equal(t, "0", quotaUsed(t, r, 1, quotaDBNow(t, db), "openai").Used)
	in.RequestID, in.Points = "api-late", "2"
	require.NoError(t, fallbackCodingTestCharge(t, db, in), "a request still unbilled at personal reset is not waived")
	after := quotaUsed(t, r, 1, quotaDBNow(t, db), "openai")
	require.Equal(t, "2.00000000", after.Used)
	require.Equal(t, before.Start, after.Start)
	require.Equal(t, before.End, after.End)
	var raw string
	require.NoError(t, db.QueryRow(`SELECT SUM(points)::text FROM coding_quota_requests WHERE metering_source='api_fallback'`).Scan(&raw))
	require.Equal(t, "10.00000000", raw)
}

func TestCodingQuotaDBAPIFallbackAdmissionSeesAllExistingOccupancy(t *testing.T) {
	db, _, base := quotaTestDB(t, "")
	now := base.Add(time.Hour)
	in := service.APIFallbackCodingCharge{RequestID: "api-paid", APIKeyID: 1, UserID: 1, AccountID: 10,
		Purpose: "openai", Points: "3", StartedAt: base.Add(time.Minute), CompletedAt: base.Add(2 * time.Minute)}
	require.NoError(t, fallbackCodingTestCharge(t, db, in))
	_, err := db.Exec(`INSERT INTO coding_quota_reservations(id,user_id,api_key_id,account_id,purpose,model,mode,state,admission_mode,reason,estimated_points,rate,sample_count,reliable,would_allow,started_at,heartbeat_at,expires_at)
 VALUES('existing-key',1,2,10,'openai','test-model','shadow','sent','normal','within_budget',5,0.5,20,true,true,$1,$2,$2::timestamptz+interval '1 hour')`, base.Add(time.Minute), now)
	require.NoError(t, err)
	read := func() service.APIFallbackAdmissionUsage {
		tx, err := db.BeginTx(context.Background(), nil)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback() }()
		require.NoError(t, lockCodingPerson(context.Background(), tx, 1, "openai"))
		usage, err := readAPIFallbackAdmissionUsage(context.Background(), tx, 1, "openai", now, now)
		require.NoError(t, err)
		return usage
	}
	usage := read()
	require.Equal(t, "3.00000000", usage.Used)
	require.Equal(t, "5.00000000", usage.Reserved)
	require.False(t, usage.Uncertain)
	_, err = db.Exec(`UPDATE coding_quota_reservations SET state='unresolved' WHERE id='existing-key'`)
	require.NoError(t, err)
	require.True(t, read().Uncertain, "unknown requests on another Key must block new paid admission")
}
