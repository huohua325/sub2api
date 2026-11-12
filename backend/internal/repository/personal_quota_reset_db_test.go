package repository

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// A personal waiver must affect only the personal projection, never raw charges.
func TestCodingQuotaDBPersonalWaiverProjection(t *testing.T) {
	db, r, base := quotaTestDB(t, "")
	_, err := db.Exec(`INSERT INTO coding_quota_requests(request_id,api_key_id,user_id,purpose,account_id,started_at,completed_at,weight,points,state)
 VALUES('waived',1,1,'openai',10,$1,$1,1,8,'settled'),('retained',1,1,'openai',10,$1,$1,1,3,'settled'),('other-user',2,2,'openai',10,$1,$1,1,4,'settled');`, base.Add(time.Minute))
	require.NoError(t, err)
	before := quotaUsed(t, r, 1, base.Add(time.Hour), "openai")
	require.Equal(t, "11.00000000", before.Used)
	_, err = db.Exec(`INSERT INTO personal_quota_resets(id,actor_user_id,target_user_id,purpose,period_start,period_end,note,created_at) VALUES('test-reset',2,1,'openai',$1,$2,'operator reason',$3)`, before.Start, before.End, base.Add(30*time.Minute))
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO personal_quota_reset_requests SELECT id,'test-reset' FROM coding_quota_requests WHERE request_id='waived'`)
	require.NoError(t, err)
	after := quotaUsed(t, r, 1, base.Add(time.Hour), "openai")
	require.Equal(t, "3.00000000", after.Used)
	require.Equal(t, before.Start, after.Start)
	require.Equal(t, before.End, after.End)
	require.Equal(t, "4.00000000", quotaUsed(t, r, 2, base.Add(time.Hour), "openai").Used)
	var raw string
	require.NoError(t, db.QueryRowContext(context.Background(), `SELECT SUM(points)::text FROM coding_quota_requests WHERE user_id=1`).Scan(&raw))
	require.Equal(t, "11.00000000", raw)
}

func TestCodingQuotaDBPersonalExperimentResetKeepsRawCounter(t *testing.T) {
	db, r, _ := quotaTestDB(t, "")
	_, err := db.Exec(`ALTER TABLE users ADD COLUMN role text NOT NULL DEFAULT 'user'; UPDATE users SET role='admin' WHERE id=2`)
	require.NoError(t, err)
	now := quotaDBNow(t, db)
	start := service.ShanghaiMonthStart(now)
	end := service.PurposeResetsAt("month", start)
	_, err = db.Exec(`INSERT INTO user_purpose_allowances(user_id,purpose,limit_amount,used_amount,period,window_start) VALUES(1,'experiment',500,120,'month',$1)`, start)
	require.NoError(t, err)
	in := service.PersonalQuotaResetInput{ID: uuid.NewString(), ActorUserID: 2, UserID: 1, Purpose: "experiment", PeriodEnd: end, Note: "monthly reset", Confirm: true}
	_, err = r.ResetPersonalQuota(context.Background(), in)
	require.NoError(t, err)
	rows, err := NewPurposeAllowanceRepository(db).List(context.Background(), 1)
	require.NoError(t, err)
	for _, v := range rows {
		if v.Purpose == "experiment" {
			require.Zero(t, v.UsedAmount)
			require.True(t, v.WindowStart.Equal(start))
		}
	}
	tx, err := db.Begin()
	require.NoError(t, err)
	require.NoError(t, incrementUsageBillingPurposeAt(context.Background(), tx, 1, "experiment", 7, now))
	require.NoError(t, tx.Commit())
	rows, err = NewPurposeAllowanceRepository(db).List(context.Background(), 1)
	require.NoError(t, err)
	for _, v := range rows {
		if v.Purpose == "experiment" {
			require.Equal(t, float64(7), v.UsedAmount)
		}
	}
	var raw string
	require.NoError(t, db.QueryRow(`SELECT used_amount::text FROM user_purpose_allowances WHERE user_id=1 AND purpose='experiment'`).Scan(&raw))
	require.Equal(t, "127.00000000", raw)
	// Retry must not reset the seven newly billed yuan.
	_, err = r.ResetPersonalQuota(context.Background(), in)
	require.NoError(t, err)
	view, err := r.ReadPersonalQuotaResets(context.Background(), 1)
	require.NoError(t, err)
	require.Len(t, view.Events, 1)
	for _, v := range view.Quotas {
		if v.Purpose == "experiment" {
			require.Equal(t, "7.00000000", v.Used)
		}
	}
	// A natural rollover cannot inherit the old credit.
	_, err = db.Exec(`UPDATE user_purpose_allowances SET window_start=$1,used_amount=5 WHERE user_id=1 AND purpose='experiment'`, start.AddDate(0, 1, 0))
	require.NoError(t, err)
	rows, err = NewPurposeAllowanceRepository(db).List(context.Background(), 1)
	require.NoError(t, err)
	for _, v := range rows {
		if v.Purpose == "experiment" {
			require.Equal(t, float64(5), v.UsedAmount)
		}
	}
}

func TestCodingQuotaDBPersonalResetConcurrentReplayAndLateBilling(t *testing.T) {
	db, r, _ := quotaTestDB(t, "")
	_, err := db.Exec(`ALTER TABLE users ADD COLUMN role text NOT NULL DEFAULT 'user'; UPDATE users SET role='admin' WHERE id=2`)
	require.NoError(t, err)
	now := quotaDBNow(t, db)
	before := quotaUsed(t, r, 1, now, "openai")
	_, err = db.Exec(`INSERT INTO coding_quota_requests(request_id,api_key_id,user_id,purpose,account_id,started_at,completed_at,weight,estimated_points,state)
 VALUES('pending-before',1,1,'openai',10,$1,$1,1,6,'pending')`, now.Add(-time.Minute))
	require.NoError(t, err)
	in := service.PersonalQuotaResetInput{ID: uuid.NewString(), ActorUserID: 2, UserID: 1, Purpose: "openai", PeriodEnd: before.End, Note: "retry race", Confirm: true}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := r.ResetPersonalQuota(context.Background(), in); errs <- e }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		require.NoError(t, e)
	}
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM personal_quota_resets`).Scan(&count))
	require.Equal(t, 1, count)
	// Settlement of a captured pending row remains waived, but a delayed insert is not.
	_, err = db.Exec(`UPDATE coding_quota_requests SET state='settled',points=9 WHERE request_id='pending-before'`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO coding_quota_requests(request_id,api_key_id,user_id,purpose,account_id,started_at,completed_at,weight,points,state)
 VALUES('late-insert',1,1,'openai',10,$1,$1,1,2,'settled')`, now.Add(-2*time.Minute))
	require.NoError(t, err)
	after := quotaUsed(t, r, 1, quotaDBNow(t, db), "openai")
	require.Equal(t, "2.00000000", after.Used)
	require.Equal(t, "0.00000000", after.Pending)
	require.Zero(t, after.PendingCount)
	require.True(t, after.End.Equal(before.End))
}

func TestCodingQuotaDBPersonalResetRejectsInvalidAndAdminTargets(t *testing.T) {
	db, r, _ := quotaTestDB(t, "")
	_, err := db.Exec(`ALTER TABLE users ADD COLUMN role text NOT NULL DEFAULT 'user'; UPDATE users SET role='admin' WHERE id=2`)
	require.NoError(t, err)
	now := quotaDBNow(t, db)
	period := quotaUsed(t, r, 1, now, "openai")
	base := service.PersonalQuotaResetInput{ID: uuid.NewString(), ActorUserID: 2, UserID: 1, Purpose: "openai", PeriodEnd: period.End, Note: "reason", Confirm: true}
	for _, change := range []func(*service.PersonalQuotaResetInput){
		func(v *service.PersonalQuotaResetInput) { v.UserID = 2 },
		func(v *service.PersonalQuotaResetInput) { v.ActorUserID = 1 },
		func(v *service.PersonalQuotaResetInput) { v.Note = "  " },
		func(v *service.PersonalQuotaResetInput) { v.Confirm = false },
		func(v *service.PersonalQuotaResetInput) { v.Purpose = "other" },
		func(v *service.PersonalQuotaResetInput) { v.PeriodEnd = v.PeriodEnd.Add(time.Second) },
	} {
		in := base
		change(&in)
		_, err := r.ResetPersonalQuota(context.Background(), in)
		require.Error(t, err)
	}
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM personal_quota_resets`).Scan(&count))
	require.Zero(t, count)
}

func TestCodingQuotaDBPersonalResetAfterBackfilledGlobalReset(t *testing.T) {
	db, r, _ := quotaTestDB(t, "")
	_, err := db.Exec(`ALTER TABLE users ADD COLUMN role text NOT NULL DEFAULT 'user'; UPDATE users SET role='admin' WHERE id=2`)
	require.NoError(t, err)
	now := quotaDBNow(t, db)
	old := quotaUsed(t, r, 1, now, "openai")
	_, err = db.Exec(`INSERT INTO coding_quota_requests(request_id,api_key_id,user_id,purpose,account_id,started_at,completed_at,weight,points,state)
 VALUES('overlapping',1,1,'openai',10,$1,$1,1,8,'settled')`, now.Add(-time.Minute))
	require.NoError(t, err)
	in := service.PersonalQuotaResetInput{ID: uuid.NewString(), ActorUserID: 2, UserID: 1, Purpose: "openai", PeriodEnd: old.End, Note: "before confirmation", Confirm: true}
	_, err = r.ResetPersonalQuota(context.Background(), in)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO coding_quota_reset_events(id,effective_at,status,source) VALUES('later-confirmed',$1,'confirmed','test')`, now.Add(-time.Hour))
	require.NoError(t, err)
	current := quotaUsed(t, r, 1, quotaDBNow(t, db), "openai")
	require.Equal(t, "8.00000000", current.Used)
	in.ID = uuid.NewString()
	in.PeriodEnd = current.End
	in.Note = "reset new period"
	_, err = r.ResetPersonalQuota(context.Background(), in)
	require.NoError(t, err)
	require.Equal(t, "0", quotaUsed(t, r, 1, quotaDBNow(t, db), "openai").Used)
}

func TestCodingQuotaDBPersonalResetAppliesToQueuedAdmission(t *testing.T) {
	db, r, _ := quotaTestDB(t, "")
	_, err := db.Exec(`ALTER TABLE users ADD COLUMN role text NOT NULL DEFAULT 'user'; UPDATE users SET role='admin' WHERE id=2`)
	require.NoError(t, err)
	now := quotaDBNow(t, db)
	period := quotaUsed(t, r, 1, now, "openai")
	_, err = db.Exec(`INSERT INTO coding_quota_requests(request_id,api_key_id,user_id,purpose,account_id,started_at,completed_at,weight,points,state)
 VALUES('old-usage',1,1,'openai',10,$1,$1,1,80,'settled')`, now.Add(-time.Minute))
	require.NoError(t, err)
	_, err = r.ResetPersonalQuota(context.Background(), service.PersonalQuotaResetInput{ID: uuid.NewString(), ActorUserID: 2, UserID: 1, Purpose: "openai", PeriodEnd: period.End, Note: "restore admission", Confirm: true})
	require.NoError(t, err)
	readings, err := readCodingQuotasAt(context.Background(), db, []int64{1}, now.Add(-30*time.Second), quotaDBNow(t, db))
	require.NoError(t, err)
	for _, reading := range readings {
		if reading.Purpose == "openai" {
			require.Equal(t, "0", reading.Used)
		}
	}
}

func TestCodingQuotaDBPersonalResetDoesNotWaiveUncommittedBilling(t *testing.T) {
	db, r, _ := quotaTestDB(t, "")
	_, err := db.Exec(`ALTER TABLE users ADD COLUMN role text NOT NULL DEFAULT 'user'; UPDATE users SET role='admin' WHERE id=2`)
	require.NoError(t, err)
	now := quotaDBNow(t, db)
	period := quotaUsed(t, r, 1, now, "openai")
	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	// The real billing writer has inserted a charge but its durable transaction
	// has not committed when the administrator resets the visible current usage.
	err = recordCodingQuotaCharge(context.Background(), tx, &service.UsageBillingCommand{RequestID: "in-flight-billing", APIKeyID: 1, UserID: 1, AccountID: 10,
		CodingQuota: &service.CodingQuotaCharge{AccountID: 10, Purpose: "openai", Weight: "1", StartedAt: now.Add(-time.Minute), CompletedAt: now}})
	require.NoError(t, err)
	_, err = r.ResetPersonalQuota(context.Background(), service.PersonalQuotaResetInput{ID: uuid.NewString(), ActorUserID: 2, UserID: 1, Purpose: "openai", PeriodEnd: period.End, Note: "overlap with billing", Confirm: true})
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	reading := quotaUsed(t, r, 1, quotaDBNow(t, db), "openai")
	require.Equal(t, 1, reading.PendingCount)
	var linked int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM personal_quota_reset_requests pr JOIN coding_quota_requests q ON q.id=pr.request_row_id WHERE q.request_id='in-flight-billing'`).Scan(&linked))
	require.Zero(t, linked, "an invisible in-flight charge must not be refunded")
}

func TestCodingQuotaDBPersonalResetPreservesAndReplays(t *testing.T) {
	db, r, _ := quotaTestDB(t, "")
	_, err := db.Exec(`ALTER TABLE users ADD COLUMN role text NOT NULL DEFAULT 'user'; UPDATE users SET role='admin' WHERE id=2`)
	require.NoError(t, err)
	now := quotaDBNow(t, db)
	before := quotaUsed(t, r, 1, now, "openai")
	_, err = db.Exec(`INSERT INTO coding_quota_requests(request_id,api_key_id,user_id,purpose,account_id,started_at,completed_at,weight,points,state)
 VALUES('before-reset',1,1,'openai',10,$1,$1,1,8,'settled')`, now.Add(-time.Minute))
	require.NoError(t, err)
	in := service.PersonalQuotaResetInput{ID: uuid.NewString(), ActorUserID: 2, UserID: 1, Purpose: "openai", PeriodEnd: before.End, Note: "support request", Confirm: true}
	event, err := r.ResetPersonalQuota(context.Background(), in)
	require.NoError(t, err)
	require.True(t, event.PeriodEnd.Equal(before.End))
	require.Equal(t, "0", quotaUsed(t, r, 1, quotaDBNow(t, db), "openai").Used)
	_, err = db.Exec(`INSERT INTO coding_quota_requests(request_id,api_key_id,user_id,purpose,account_id,started_at,completed_at,weight,points,state)
 VALUES('after-reset',1,1,'openai',10,$1,$1,1,3,'settled')`, now)
	require.NoError(t, err)
	again, err := r.ResetPersonalQuota(context.Background(), in)
	require.NoError(t, err)
	require.Equal(t, event.ID, again.ID)
	require.Equal(t, "3.00000000", quotaUsed(t, r, 1, quotaDBNow(t, db), "openai").Used)
	var raw string
	require.NoError(t, db.QueryRow(`SELECT SUM(points)::text FROM coding_quota_requests WHERE user_id=1`).Scan(&raw))
	require.Equal(t, "11.00000000", raw)
	in.Note = "different request"
	_, err = r.ResetPersonalQuota(context.Background(), in)
	require.Error(t, err)
	in.ID = uuid.NewString()
	in.PeriodEnd = in.PeriodEnd.Add(time.Second)
	_, err = r.ResetPersonalQuota(context.Background(), in)
	require.Error(t, err)
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM personal_quota_resets`).Scan(&count))
	require.Equal(t, 1, count)
	_, err = db.Exec(`DELETE FROM personal_quota_resets`)
	require.Error(t, err, "audit must be append only")
}
