package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func seedCapacityProjectionEvent(t *testing.T, db *sql.DB, id string, account int64, at, end time.Time, factor, previous string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO coding_quota_capacity_events(id,account_id,purpose,effective_at,before_observed_at,window_end,before_used,after_used,capacity_factor,previous_rate,actor,note)
 VALUES($1,$2,'openai',$3,$3::timestamptz-interval '1 second',$4,30,30/$5::numeric,$5,$6,'test-owner','confirmed test capacity')`, id, account, at, end, factor, previous)
	require.NoError(t, err)
}

func TestCodingQuotaDBCapacityProjectionNormalizesWithoutRewriting(t *testing.T) {
	db, r, base := quotaTestDB(t, "")
	ctx := context.Background()
	end := base.Add(6 * 24 * time.Hour)
	_, err := db.Exec(`INSERT INTO coding_quota_requests(request_id,api_key_id,user_id,purpose,account_id,started_at,completed_at,official_window_end,weight,points,state)
 VALUES('old-a',1,1,'openai',10,$1,$1,$3,40,20,'settled'),('old-b',2,2,'openai',10,$1,$1,$3,24,12,'settled'),
 ('new-a',1,1,'openai',10,$2,$2,$3,16,4,'settled'),('other-account',1,1,'openai',20,$1,$1,$3,10,5,'settled'),
 ('grok-independent',1,1,'grok',20,$1,$1,$3,7,7,'settled'),('imported-no-account',2,2,'grok',NULL,$1,$1,NULL,0,3,'imported')`, base.Add(time.Minute), base.Add(12*time.Minute), end)
	require.NoError(t, err)
	seedCapacityProjectionEvent(t, db, "double", 10, base.Add(10*time.Minute), end, "2", "0.5")
	seedCapacityProjectionEvent(t, db, "later-triple", 10, base.Add(20*time.Minute), end, "3", "0.25")
	var before string
	require.NoError(t, db.QueryRow(`SELECT jsonb_agg(to_jsonb(q) ORDER BY id)::text FROM coding_quota_requests q`).Scan(&before))
	for range 2 {
		a := quotaUsed(t, r, 1, base.Add(15*time.Minute), "openai")
		require.Equal(t, "19.00000000", a.Used, "old 20/2 + new 4 + independent account 5")
		require.Equal(t, "6.00000000", quotaUsed(t, r, 2, base.Add(15*time.Minute), "openai").Used)
		require.Equal(t, "7.00000000", quotaUsed(t, r, 1, base.Add(15*time.Minute), "grok").Used)
		require.Equal(t, "3.00000000", quotaUsed(t, r, 2, base.Add(15*time.Minute), "grok").Used)
		require.True(t, a.Start.Equal(base))
		require.True(t, a.End.Equal(base.Add(168*time.Hour)))
	}
	require.Equal(t, "9.66666667", quotaUsed(t, r, 1, base.Add(25*time.Minute), "openai").Used, "multiple confirmed factors compound only once")
	var after string
	require.NoError(t, db.QueryRow(`SELECT jsonb_agg(to_jsonb(q) ORDER BY id)::text FROM coding_quota_requests q`).Scan(&after))
	require.Equal(t, before, after, "capacity reads must never rewrite original charges")
	_, err = r.ReadCodingQuotas(ctx, []int64{1, 2}, base.Add(25*time.Minute))
	require.NoError(t, err)
}

func TestCodingQuotaDBCapacityProjectionReestimatesLegacyUnknownWithMatchingStage(t *testing.T) {
	db, r, base := quotaTestDB(t, "")
	end := base.Add(6 * 24 * time.Hour)
	seedCapacityProjectionEvent(t, db, "double", 10, base.Add(10*time.Minute), end, "2", "0.5")
	_, err := db.Exec(`INSERT INTO coding_quota_meters(account_id,purpose,window_end,observed_at,used,points_per_weight,calibration_started_at)
 VALUES(10,'openai',$1,$2,20,0.25,$3)`, end, base.Add(15*time.Minute), base.Add(10*time.Minute))
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO coding_quota_requests(request_id,api_key_id,user_id,purpose,account_id,started_at,completed_at,official_window_end,weight,estimated_points,state)
 VALUES('old-unknown',1,1,'openai',10,$1,$1,$3,4,3,'unmetered'),('post-unknown',1,1,'openai',10,$2,$2,$3,8,4,'unmetered'),
 ('different-window',1,1,'openai',10,$2,$2,$3::timestamptz+interval '7 days',5,3,'unmetered'),
 ('different-account',1,1,'openai',20,$2,$2,$3,2,1,'unmetered')`, base.Add(time.Minute), base.Add(15*time.Minute), end)
	require.NoError(t, err)
	v := quotaUsed(t, r, 1, base.Add(16*time.Minute), "openai")
	require.Equal(t, "7.00000000", v.Pending, "old stage 4*.5/2 + new stage 8*.25 + unmatched 3 + other account 1")
	require.Equal(t, 4, v.UnmeteredCount, "re-estimation must not claim historical attribution is settled")
	var original string
	require.NoError(t, db.QueryRow(`SELECT estimated_points::text FROM coding_quota_requests WHERE request_id='post-unknown'`).Scan(&original))
	require.Equal(t, "4.00000000", original)
}

func TestCodingQuotaDBCapacityProjectionCorrectionsFollowOriginalBasis(t *testing.T) {
	db, r, base := quotaTestDB(t, "")
	end := base.Add(6 * 24 * time.Hour)
	seedCapacityProjectionEvent(t, db, "double", 10, base.Add(10*time.Minute), end, "2", "0.5")
	_, err := db.Exec(`INSERT INTO coding_quota_requests(request_id,api_key_id,user_id,purpose,account_id,started_at,completed_at,weight,points,state)
 VALUES('refunded-old',1,1,'openai',10,$1,$1,40,20,'settled')`, base.Add(time.Minute))
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO coding_quota_reconciliations(id,algorithm,account_id,plan_digest,actor,note,evidence)
 VALUES('known-duplicate','same-window-recovery-v1',10,repeat('a',64),'test-owner','evidence','{}');
 INSERT INTO coding_quota_corrections(reconciliation_id,request_row_id,before_points,delta)
 SELECT 'known-duplicate',id,20,-20 FROM coding_quota_requests WHERE request_id='refunded-old'`)
	require.NoError(t, err)
	require.Equal(t, "0.00000000", quotaUsed(t, r, 1, base.Add(15*time.Minute), "openai").Used)
	rows, err := r.ReadCodingReservationSummaries(context.Background(), []int64{1}, base.Add(15*time.Minute))
	require.NoError(t, err)
	for _, row := range rows {
		if row.Purpose == "openai" {
			require.Equal(t, "-10.00000000", row.Corrections)
		}
	}
}

func TestCodingQuotaDBCapacityProjectionDoesNotSkipUnknownStageRate(t *testing.T) {
	db, r, base := quotaTestDB(t, "")
	end := base.Add(6 * 24 * time.Hour)
	seedCapacityProjectionEvent(t, db, "first-no-rate", 10, base.Add(10*time.Minute), end, "2", "0")
	seedCapacityProjectionEvent(t, db, "next-valid-rate", 10, base.Add(20*time.Minute), end, "3", "0.25")
	_, err := db.Exec(`INSERT INTO coding_quota_requests(request_id,api_key_id,user_id,purpose,account_id,started_at,completed_at,official_window_end,weight,estimated_points,state)
 VALUES('old-unknown-stage',1,1,'openai',10,$1,$1,$2,4,6,'unmetered')`, base.Add(time.Minute), end)
	require.NoError(t, err)
	reading := quotaUsed(t, r, 1, base.Add(25*time.Minute), "openai")
	require.Equal(t, "1.00000000", reading.Pending, "retain raw estimate 6 divided by 2*3, never borrow an intermediate stage rate")
	require.Equal(t, 1, reading.UnmeteredCount)
}

func TestCodingQuotaDBCapacityProjectionAdmissionAndReservationRefresh(t *testing.T) {
	db, r, base := quotaTestDB(t, "")
	ctx := context.Background()
	end := base.Add(6 * 24 * time.Hour)
	now := base.Add(15 * time.Minute)
	seedCapacityProjectionEvent(t, db, "double", 10, base.Add(10*time.Minute), end, "2", "0.5")
	_, err := db.Exec(`INSERT INTO coding_quota_meters(account_id,purpose,window_end,observed_at,used,points_per_weight,calibration_started_at)
 VALUES(10,'openai',$1,$2,26,0.25,$3)`, end, now, base.Add(10*time.Minute))
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO coding_quota_requests(request_id,api_key_id,user_id,purpose,account_id,started_at,completed_at,official_window_end,weight,points,state)
 VALUES('old-charge',1,1,'openai',10,$1,$1,$3,104,52,'settled'),('new-sample',2,2,'openai',10,$2,$2,$3,4,1,'settled')`, base.Add(time.Minute), now, end)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO coding_quota_reservations(id,user_id,api_key_id,account_id,purpose,model,mode,state,admission_mode,reason,estimated_points,rate,sample_count,reliable,would_allow,started_at,heartbeat_at,expires_at)
 VALUES('old-stream',1,1,10,'openai','test-model','shadow','sent','normal','within_budget',20,0.5,20,true,true,$1,$2,$2::timestamptz+interval '1 hour')`, base.Add(time.Minute), now)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO coding_quota_reservation_events(reservation_id,state,reason,created_at) VALUES('old-stream','reserved','within_budget',$1)`, base.Add(time.Minute))
	require.NoError(t, err)
	rows, err := r.ReadCodingReservationSummaries(ctx, []int64{1}, now)
	require.NoError(t, err)
	for _, row := range rows {
		if row.Purpose == "openai" {
			require.Equal(t, "10.00000000", row.Reserved)
			require.Equal(t, "26.00000000", row.Settled)
		}
	}
	a, err := r.ReserveCodingQuota(ctx, service.CodingReservationInput{ID: "new-admission", UserID: 1, APIKeyID: 2, AccountID: 10, Purpose: "openai", Mode: "enforce", Limit: "40", Weight: "8", StartedAt: now, Now: now, MinSamples: 1})
	require.NoError(t, err)
	require.Equal(t, "normal", a.AdmissionMode, "26 normalized settled + 10 normalized old reservation + 2 new estimate fits 40")
	_, err = db.Exec(`UPDATE coding_quota_reservations SET state='released' WHERE id='new-admission';
 UPDATE coding_quota_reservations SET estimated_points=6,rate=.25 WHERE id='old-stream'`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO coding_quota_reservation_events(reservation_id,state,reason,created_at) VALUES('old-stream','reserved','account_switch',$1)`, now)
	require.NoError(t, err)
	rows, err = r.ReadCodingReservationSummaries(ctx, []int64{1}, now)
	require.NoError(t, err)
	for _, row := range rows {
		if row.Purpose == "openai" {
			require.Equal(t, "6.00000000", row.Reserved, "a refreshed post-expansion estimate must not be discounted again")
		}
	}
}
