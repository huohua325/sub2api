package repository

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func experimentGuardTestDB(t *testing.T) (*sql.DB, *UsageMonitorRepository) {
	t.Helper()
	db, r, _ := quotaTestDB(t, "")
	_, err := db.Exec(`INSERT INTO user_purpose_allowances(user_id,purpose,limit_amount,used_amount,period,window_start)
 VALUES(1,'experiment',500,0,'month',date_trunc('month',now() AT TIME ZONE 'Asia/Shanghai') AT TIME ZONE 'Asia/Shanghai'),
 (2,'experiment',500,0,'month',date_trunc('month',now() AT TIME ZONE 'Asia/Shanghai') AT TIME ZONE 'Asia/Shanghai')`)
	require.NoError(t, err)
	return db, r
}

func experimentAdmissionInput(db *sql.DB, t *testing.T) service.ExperimentAdmissionInput {
	t.Helper()
	return service.ExperimentAdmissionInput{RequestID: uuid.NewString(), UserID: 1, APIKeyID: 1, AccountID: 10, StartedAt: quotaDBNow(t, db).Truncate(time.Microsecond)}
}

func experimentEvidence(account int64, upstream string) service.ExperimentChargeEvidence {
	return service.ExperimentChargeEvidence{Source: "test_supplier_export", SHA256: strings.Repeat("a", 64), UpstreamRequestID: upstream, AccountID: account}
}

func TestCodingQuotaDBExperimentStrictAdmissionAcrossKeysAndRestart(t *testing.T) {
	db, r := experimentGuardTestDB(t)
	db.SetMaxOpenConns(20)
	base := experimentAdmissionInput(db, t)
	var wg sync.WaitGroup
	accepted := make(chan service.ExperimentAdmissionInput, 20)
	failures := make(chan error, 20)
	for i := range 20 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			in := base
			in.RequestID = uuid.NewString()
			in.APIKeyID = int64(1 + i%2)
			if err := r.ReserveExperimentRequest(context.Background(), in); err != nil {
				failures <- err
			} else {
				accepted <- in
			}
		}(i)
	}
	wg.Wait()
	close(accepted)
	close(failures)
	var successful []service.ExperimentAdmissionInput
	for in := range accepted {
		successful = append(successful, in)
	}
	require.Len(t, successful, 1)
	for err := range failures {
		require.ErrorIs(t, err, service.ErrExperimentRequestInFlight)
	}
	in := successful[0]
	require.NoError(t, r.ReserveExperimentRequest(t.Context(), in), "only unsent identical reservation is idempotent")
	require.NoError(t, r.MarkExperimentRequestSent(t.Context(), in.RequestID, in.APIKeyID))
	require.ErrorIs(t, r.MarkExperimentRequestSent(t.Context(), in.RequestID, in.APIKeyID), service.ErrExperimentRequestInFlight)
	require.ErrorIs(t, r.ReleaseExperimentRequest(t.Context(), in.RequestID, in.APIKeyID, "client canceled"), service.ErrExperimentRequestInFlight)
	require.NoError(t, r.RecordExperimentResponse(t.Context(), in.RequestID, in.APIKeyID, "receipt", 403))
	require.NoError(t, r.RecordExperimentResponse(t.Context(), in.RequestID, in.APIKeyID, "receipt", 403))
	require.ErrorIs(t, r.RecordExperimentResponse(t.Context(), in.RequestID, in.APIKeyID, "different", 403), service.ErrExperimentAccountingUnavailable)
	r = NewUsageMonitorRepository(db)
	newInput := experimentAdmissionInput(db, t)
	newInput.APIKeyID = 3
	require.ErrorIs(t, r.ReserveExperimentRequest(t.Context(), newInput), service.ErrExperimentRequestInFlight)
	var state, receipt string
	var status int
	require.NoError(t, db.QueryRow(`SELECT state,response_request_id,response_status FROM experiment_quota_attempts`).Scan(&state, &receipt, &status))
	require.Equal(t, "unknown", state)
	require.Equal(t, "receipt", receipt)
	require.Equal(t, 403, status)
	// A request that another user owns has an independent gate.
	newInput.UserID = 2
	require.NoError(t, r.ReserveExperimentRequest(t.Context(), newInput))
}

func TestCodingQuotaDBExperimentUnsentReleaseAndIdentityConflict(t *testing.T) {
	db, r := experimentGuardTestDB(t)
	in := experimentAdmissionInput(db, t)
	require.NoError(t, r.ReserveExperimentRequest(t.Context(), in))
	changed := in
	changed.AccountID = 20
	require.ErrorIs(t, r.ReserveExperimentRequest(t.Context(), changed), service.ErrExperimentAccountingUnavailable)
	require.NoError(t, r.ReleaseExperimentRequest(t.Context(), in.RequestID, in.APIKeyID, "no upstream send"))
	require.NoError(t, r.ReleaseExperimentRequest(t.Context(), in.RequestID, in.APIKeyID, "same release"))
	require.ErrorIs(t, r.MarkExperimentRequestSent(t.Context(), in.RequestID, in.APIKeyID), service.ErrExperimentRequestInFlight)
	other := experimentAdmissionInput(db, t)
	require.NoError(t, r.ReserveExperimentRequest(t.Context(), other))
}

func TestCodingQuotaDBExperimentPendingBlocksWithoutInventingPrice(t *testing.T) {
	db, r := experimentGuardTestDB(t)
	_, err := db.Exec(`INSERT INTO experiment_quota_charges(request_id,api_key_id,user_id,account_id,upstream_request_id,started_at,amount,state,billing_effects)
 VALUES('unknown-cost',2,1,10,'upstream-unknown',clock_timestamp()-interval '1 minute',NULL,'pending','null')`)
	require.NoError(t, err)
	require.ErrorIs(t, r.ReserveExperimentRequest(t.Context(), experimentAdmissionInput(db, t)), service.ErrExperimentAccountingPending)
	var amount sql.NullFloat64
	require.NoError(t, db.QueryRow(`SELECT amount FROM experiment_quota_charges`).Scan(&amount))
	require.False(t, amount.Valid)
	_, err = db.Exec(`UPDATE user_purpose_allowances SET used_amount=500 WHERE user_id=1 AND purpose='experiment'`)
	require.NoError(t, err)
	require.ErrorIs(t, r.ReserveExperimentRequest(t.Context(), experimentAdmissionInput(db, t)), service.ErrExperimentQuotaExhausted)
}

func TestCodingQuotaDBExperimentHandoffEvidenceAndAppendOnlyAudit(t *testing.T) {
	db, r := experimentGuardTestDB(t)
	in := experimentAdmissionInput(db, t)
	require.NoError(t, r.ReserveExperimentRequest(t.Context(), in))
	require.NoError(t, r.MarkExperimentRequestSent(t.Context(), in.RequestID, in.APIKeyID))
	require.NoError(t, r.RecordExperimentResponse(t.Context(), in.RequestID, in.APIKeyID, "supplier-receipt", 200))
	_, err := db.Exec(`INSERT INTO usage_logs(id,request_id,api_key_id,actual_cost,input_cost,total_cost) VALUES(1,$1,1,0,0,0)`, in.RequestID)
	require.NoError(t, err)
	repo := &usageBillingRepository{db: db}
	_, err = repo.Apply(t.Context(), &service.UsageBillingCommand{RequestID: in.RequestID, APIKeyID: in.APIKeyID, UserID: in.UserID, AccountID: in.AccountID,
		Purpose: "experiment", StartedAt: in.StartedAt, ExperimentPending: true, ExperimentUpstreamRequestID: "supplier-receipt"})
	require.NoError(t, err)
	var state string
	require.NoError(t, db.QueryRow(`SELECT state FROM experiment_quota_attempts`).Scan(&state))
	require.Equal(t, "handed_off", state)
	require.ErrorIs(t, r.ReserveExperimentRequest(t.Context(), experimentAdmissionInput(db, t)), service.ErrExperimentAccountingPending)
	var chargeID int64
	require.NoError(t, db.QueryRow(`SELECT id FROM experiment_quota_charges WHERE request_id=$1`, in.RequestID).Scan(&chargeID))
	evidence := experimentEvidence(10, "supplier-receipt")
	wrong := evidence
	wrong.AccountID = 20
	require.ErrorContains(t, r.CompleteExperimentChargeWithEvidence(t.Context(), chargeID, 2.5, wrong), "identity conflict")
	require.NoError(t, r.CompleteExperimentChargeWithEvidence(t.Context(), chargeID, 2.5, evidence))
	require.NoError(t, r.CompleteExperimentChargeWithEvidence(t.Context(), chargeID, 2.5, evidence))
	require.ErrorContains(t, r.CompleteExperimentChargeWithEvidence(t.Context(), chargeID, 3, evidence), "conflicts with prior settlement")
	var used, recorded float64
	var eventCount int
	var original string
	require.NoError(t, db.QueryRow(`SELECT used_amount FROM user_purpose_allowances WHERE user_id=1 AND purpose='experiment'`).Scan(&used))
	require.NoError(t, db.QueryRow(`SELECT actual_cost FROM usage_logs WHERE id=1`).Scan(&recorded))
	require.NoError(t, db.QueryRow(`SELECT count(*),min(original_usage_costs::text) FROM experiment_quota_settlement_events`).Scan(&eventCount, &original))
	require.Equal(t, 2.5, used)
	require.Equal(t, 2.5, recorded)
	require.Equal(t, 1, eventCount)
	require.Contains(t, original, `"actual_cost": 0`)
	_, err = db.Exec(`UPDATE experiment_quota_settlement_events SET amount=9`)
	require.ErrorContains(t, err, "append-only")
	_, err = db.Exec(`DELETE FROM experiment_quota_settlement_events`)
	require.ErrorContains(t, err, "append-only")
	require.NoError(t, r.ReserveExperimentRequest(t.Context(), experimentAdmissionInput(db, t)))
	// Reapplying the additive migration preserves the event and all attempts.
	raw, err := migrations.FS.ReadFile("272_experiment_accounting_guard.sql")
	require.NoError(t, err)
	require.NoError(t, applyMigrationsFS(t.Context(), db, fstest.MapFS{"272_experiment_accounting_guard.sql": &fstest.MapFile{Data: raw}}))
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM experiment_quota_settlement_events`).Scan(&eventCount))
	require.Equal(t, 1, eventCount)
}

func TestCodingQuotaDBExperimentEvidenceOriginalPeriodAndPersonalReset(t *testing.T) {
	db, r := experimentGuardTestDB(t)
	month := service.ShanghaiMonthStart(quotaDBNow(t, db))
	_, err := db.Exec(`UPDATE user_purpose_allowances SET used_amount=20 WHERE user_id=1 AND purpose='experiment'`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO personal_quota_resets(id,actor_user_id,target_user_id,purpose,period_start,period_end,note,experiment_baseline)
 VALUES('test-experiment-reset',1,1,'experiment',$1,$2,'confirmed test reset',20)`, month, month.AddDate(0, 1, 0))
	require.NoError(t, err)
	repo := &usageBillingRepository{db: db}
	for _, tc := range []struct {
		id string
		at time.Time
	}{{"current-late", month.Add(time.Minute)}, {"prior-late", month.Add(-time.Second)}} {
		_, err := repo.Apply(t.Context(), &service.UsageBillingCommand{RequestID: tc.id, APIKeyID: 1, UserID: 1, AccountID: 10, Purpose: "experiment", StartedAt: tc.at, ExperimentPending: true, ExperimentUpstreamRequestID: tc.id})
		require.NoError(t, err)
		var id int64
		require.NoError(t, db.QueryRow(`SELECT id FROM experiment_quota_charges WHERE request_id=$1`, tc.id).Scan(&id))
		require.NoError(t, r.CompleteExperimentChargeWithEvidence(t.Context(), id, 5, experimentEvidence(10, tc.id)))
	}
	var rawUsed, effective float64
	require.NoError(t, db.QueryRow(`SELECT used_amount,`+personalExperimentUsageSQL("w")+` FROM user_purpose_allowances w WHERE user_id=1 AND purpose='experiment'`).Scan(&rawUsed, &effective))
	require.Equal(t, 25.0, rawUsed)
	require.Equal(t, 5.0, effective, "pending charges were not waived by the earlier personal reset")
	var unchangedStart time.Time
	require.NoError(t, db.QueryRow(`SELECT window_start FROM user_purpose_allowances WHERE user_id=1 AND purpose='experiment'`).Scan(&unchangedStart))
	require.True(t, unchangedStart.Equal(month))
}

func TestCodingQuotaDBExperimentRecentMatchIsFairAndEvidenceExact(t *testing.T) {
	db, r := experimentGuardTestDB(t)
	_, err := db.Exec(`INSERT INTO experiment_quota_charges(request_id,api_key_id,user_id,account_id,upstream_request_id,started_at,amount,state,billing_effects)
 SELECT 'busy-'||i,1,1,10,'busy-up-'||i,now()-interval '1 minute',NULL,'pending','null' FROM generate_series(1,20)i;
 INSERT INTO experiment_quota_charges(request_id,api_key_id,user_id,account_id,upstream_request_id,started_at,amount,state,billing_effects)
 VALUES('other-account',1,1,20,'other-up',now(),NULL,'pending','null')`)
	require.NoError(t, err)
	triggers, err := r.ListPendingExperimentCharges(t.Context(), quotaDBNow(t, db))
	require.NoError(t, err)
	require.Len(t, triggers, 2)
	matched, err := r.MatchPendingExperimentCharges(t.Context(), 10, []string{"busy-up-2", "busy-up-4", "other-up", "not-found"})
	require.NoError(t, err)
	require.Len(t, matched, 2)
	for _, item := range matched {
		require.Equal(t, int64(10), item.AccountID)
		require.NoError(t, r.CompleteExperimentChargeWithEvidence(t.Context(), item.ID, 0, experimentEvidence(10, item.UpstreamRequestID)))
	}
	matched, err = r.MatchPendingExperimentCharges(t.Context(), 10, []string{"busy-up-2", "busy-up-4"})
	require.NoError(t, err)
	require.Empty(t, matched)
	var amount string
	require.NoError(t, db.QueryRow(`SELECT used_amount::text FROM user_purpose_allowances WHERE user_id=1 AND purpose='experiment'`).Scan(&amount))
	require.Equal(t, "0.00000000", amount)
}

func TestCodingQuotaDBExperimentInitialSettledHasAudit(t *testing.T) {
	db, r := experimentGuardTestDB(t)
	in := experimentAdmissionInput(db, t)
	require.NoError(t, r.ReserveExperimentRequest(t.Context(), in))
	require.NoError(t, r.MarkExperimentRequestSent(t.Context(), in.RequestID, in.APIKeyID))
	repo := &usageBillingRepository{db: db}
	_, err := repo.Apply(t.Context(), &service.UsageBillingCommand{RequestID: in.RequestID, APIKeyID: in.APIKeyID, UserID: in.UserID, AccountID: in.AccountID,
		Purpose: "experiment", PurposeCost: 1.25, StartedAt: in.StartedAt, ExperimentUpstreamRequestID: "sync-upstream"})
	require.NoError(t, err)
	var source, previous, state string
	var amount float64
	require.NoError(t, db.QueryRow(`SELECT evidence_source,previous_state,amount FROM experiment_quota_settlement_events`).Scan(&source, &previous, &amount))
	require.Equal(t, "synchronous_billing", source)
	require.Equal(t, "new", previous)
	require.Equal(t, 1.25, amount)
	require.NoError(t, db.QueryRow(`SELECT state FROM experiment_quota_attempts`).Scan(&state))
	require.Equal(t, "settled", state)
	var used float64
	require.NoError(t, db.QueryRow(`SELECT used_amount FROM user_purpose_allowances WHERE user_id=1 AND purpose='experiment'`).Scan(&used))
	require.Equal(t, 1.25, used)
}

func TestCodingQuotaDBExperimentInvalidEvidenceRollsBack(t *testing.T) {
	db, r := experimentGuardTestDB(t)
	_, err := db.Exec(`INSERT INTO experiment_quota_charges(request_id,api_key_id,user_id,account_id,upstream_request_id,started_at,amount,state,billing_effects)
 VALUES('bad-evidence',1,1,10,'bad-up',now(),NULL,'pending','null')`)
	require.NoError(t, err)
	var id int64
	require.NoError(t, db.QueryRow(`SELECT id FROM experiment_quota_charges`).Scan(&id))
	bad := experimentEvidence(10, "bad-up")
	bad.SHA256 = "not-a-digest"
	require.Error(t, r.CompleteExperimentChargeWithEvidence(t.Context(), id, 1, bad))
	_, err = db.Exec(`INSERT INTO usage_logs(id,request_id,api_key_id,actual_cost) VALUES(1,'bad-evidence',1,9)`)
	require.NoError(t, err)
	require.ErrorContains(t, r.CompleteExperimentChargeWithEvidence(t.Context(), id, 1, experimentEvidence(10, "bad-up")), "cost conflicts")
	var state string
	require.NoError(t, db.QueryRow(`SELECT state FROM experiment_quota_charges`).Scan(&state))
	require.Equal(t, "pending", state)
	var count int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM experiment_quota_settlement_events`).Scan(&count))
	require.Zero(t, count)
}

func TestCodingQuotaDBExperimentAccountingStatusDoesNotDoubleCount(t *testing.T) {
	db, r := experimentGuardTestDB(t)
	in := experimentAdmissionInput(db, t)
	require.NoError(t, r.ReserveExperimentRequest(t.Context(), in))
	status, err := r.ReadExperimentAccountingStatus(t.Context(), 1, quotaDBNow(t, db))
	require.NoError(t, err)
	require.Equal(t, int64(1), status.InFlightRequests)
	require.Zero(t, status.PendingRequests)
	require.NotNil(t, status.UpdatedAt)
	require.NoError(t, r.MarkExperimentRequestSent(t.Context(), in.RequestID, in.APIKeyID))
	repo := &usageBillingRepository{db: db}
	_, err = repo.Apply(t.Context(), &service.UsageBillingCommand{RequestID: in.RequestID, APIKeyID: in.APIKeyID, UserID: in.UserID, AccountID: in.AccountID,
		Purpose: "experiment", StartedAt: in.StartedAt, ExperimentPending: true, ExperimentUpstreamRequestID: "status-upstream"})
	require.NoError(t, err)
	status, err = r.ReadExperimentAccountingStatus(t.Context(), 1, quotaDBNow(t, db))
	require.NoError(t, err)
	require.Equal(t, int64(1), status.PendingRequests)
	require.Zero(t, status.InFlightRequests, "charge and handed-off attempt describe one request")
	_, err = db.Exec(`INSERT INTO experiment_quota_charges(request_id,api_key_id,user_id,account_id,upstream_request_id,started_at,amount,state,billing_effects)
 VALUES('missing-receipt',2,1,20,'',now(),NULL,'unresolved','null')`)
	require.NoError(t, err)
	status, err = r.ReadExperimentAccountingStatus(t.Context(), 1, quotaDBNow(t, db))
	require.NoError(t, err)
	require.Equal(t, int64(1), status.PendingRequests)
	require.Equal(t, int64(1), status.UnresolvedRequests)
	require.Zero(t, status.InFlightRequests)
	// An outstanding sent attempt is still visible after the personal month changes.
	future := service.ShanghaiMonthStart(quotaDBNow(t, db)).AddDate(0, 1, 0).Add(time.Hour)
	status, err = r.ReadExperimentAccountingStatus(t.Context(), 1, future)
	require.NoError(t, err)
	require.Zero(t, status.PendingRequests)
	require.Zero(t, status.UnresolvedRequests)
	require.Equal(t, int64(1), status.InFlightRequests)
}

func TestCodingQuotaDBExperimentConcurrentEvidenceSettlementIsOnce(t *testing.T) {
	db, r := experimentGuardTestDB(t)
	db.SetMaxOpenConns(16)
	_, err := db.Exec(`INSERT INTO experiment_quota_charges(request_id,api_key_id,user_id,account_id,upstream_request_id,started_at,amount,state,billing_effects)
 VALUES('concurrent-settle',1,1,10,'concurrent-up',now(),NULL,'pending','null')`)
	require.NoError(t, err)
	var id int64
	require.NoError(t, db.QueryRow(`SELECT id FROM experiment_quota_charges`).Scan(&id))
	var wg sync.WaitGroup
	failures := make(chan error, 16)
	for range 16 {
		wg.Go(func() {
			if err := r.CompleteExperimentChargeWithEvidence(context.Background(), id, 1.23456789, experimentEvidence(10, "concurrent-up")); err != nil {
				failures <- err
			}
		})
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	var used string
	var events int
	require.NoError(t, db.QueryRow(`SELECT used_amount::text FROM user_purpose_allowances WHERE user_id=1 AND purpose='experiment'`).Scan(&used))
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM experiment_quota_settlement_events`).Scan(&events))
	require.Equal(t, "1.23456789", used)
	require.Equal(t, 1, events)
}

func TestCodingQuotaDBExperimentUnsentRecoveryRacesSendCheckpoint(t *testing.T) {
	db, r := experimentGuardTestDB(t)
	db.SetMaxOpenConns(8)
	for i := range 12 {
		in := experimentAdmissionInput(db, t)
		in.UserID = int64(10 + i)
		_, err := db.Exec(`INSERT INTO user_purpose_allowances(user_id,purpose,limit_amount,used_amount,period,window_start)
 VALUES($1,'experiment',500,0,'month',date_trunc('month',now() AT TIME ZONE 'Asia/Shanghai') AT TIME ZONE 'Asia/Shanghai')`, in.UserID)
		require.NoError(t, err)
		require.NoError(t, r.ReserveExperimentRequest(t.Context(), in))
		_, err = db.Exec(`UPDATE experiment_quota_attempts SET created_at=now()-interval '10 minutes' WHERE request_id=$1`, in.RequestID)
		require.NoError(t, err)
		start := make(chan struct{})
		var sentErr, recoveredErr error
		var recovered int64
		var wg sync.WaitGroup
		wg.Go(func() {
			<-start
			sentErr = r.MarkExperimentRequestSent(context.Background(), in.RequestID, in.APIKeyID)
		})
		wg.Go(func() {
			<-start
			recovered, recoveredErr = r.RecoverExperimentUnsentRequests(context.Background(), quotaDBNow(t, db).Add(-5*time.Minute))
		})
		close(start)
		wg.Wait()
		require.NoError(t, recoveredErr)
		var state string
		require.NoError(t, db.QueryRow(`SELECT state FROM experiment_quota_attempts WHERE request_id=$1`, in.RequestID).Scan(&state))
		if sentErr == nil {
			require.Equal(t, "sent", state)
			require.Zero(t, recovered)
		} else {
			require.ErrorIs(t, sentErr, service.ErrExperimentRequestInFlight)
			require.Equal(t, "released", state)
			require.Equal(t, int64(1), recovered)
		}
	}
	fresh := experimentAdmissionInput(db, t)
	require.NoError(t, r.ReserveExperimentRequest(t.Context(), fresh))
	n, err := r.RecoverExperimentUnsentRequests(t.Context(), time.Now().Add(time.Hour))
	require.NoError(t, err)
	require.Zero(t, n, "callers cannot release reservations younger than five minutes")
	var sentCount int64
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM experiment_quota_attempts WHERE state='sent'`).Scan(&sentCount))
	n, err = r.RecoverExperimentUnsentRequests(t.Context(), time.Now().Add(time.Hour))
	require.NoError(t, err)
	require.Zero(t, n)
	var stillSent int64
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM experiment_quota_attempts WHERE state='sent'`).Scan(&stillSent))
	require.Equal(t, sentCount, stillSent)
}
