package repository

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// These tests can also run without Docker using an explicitly isolated local
// PostgreSQL. Every test creates and drops its own random schema.
func quotaTestDB(t *testing.T, seed string) (*sql.DB, *UsageMonitorRepository, time.Time) {
	t.Helper()
	dsn := os.Getenv("SUB2API_QUOTA_TEST_DSN")
	if dsn == "" {
		t.Skip("SUB2API_QUOTA_TEST_DSN is not configured")
	}
	admin, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	schema := "quota_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = admin.Exec(`CREATE SCHEMA ` + schema)
	require.NoError(t, err)
	db, err := sql.Open("postgres", dsn+" search_path="+schema)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
		_, e := admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
		require.NoError(t, e)
		require.NoError(t, admin.Close())
	})
	_, err = db.Exec(`CREATE TABLE users(id BIGINT PRIMARY KEY,created_at timestamptz NOT NULL,deleted_at timestamptz);
 CREATE TABLE accounts(id BIGINT PRIMARY KEY,name text,platform text,type text,credentials jsonb NOT NULL DEFAULT '{}',extra jsonb NOT NULL DEFAULT '{}',parent_account_id bigint,schedulable boolean NOT NULL DEFAULT TRUE,deleted_at timestamptz);
 CREATE TABLE groups(id BIGINT PRIMARY KEY,usage_kind text,deleted_at timestamptz);
 CREATE TABLE account_groups(account_id bigint,group_id bigint);
 CREATE TABLE settings(key text PRIMARY KEY,value text,updated_at timestamptz NOT NULL DEFAULT NOW());
 CREATE TABLE user_purpose_allowances(user_id bigint,purpose text,limit_amount numeric(20,8),used_amount numeric(20,8),period text,window_start timestamptz,updated_at timestamptz NOT NULL DEFAULT NOW(),PRIMARY KEY(user_id,purpose));
 CREATE TABLE usage_logs(id bigint,user_id bigint,official_account_id bigint,official_reset_at timestamptz,actual_cost numeric,created_at timestamptz,request_id text,api_key_id bigint,input_cost numeric,total_cost numeric);
 CREATE TABLE usage_billing_dedup(id bigint GENERATED ALWAYS AS IDENTITY,request_id text,api_key_id bigint,request_fingerprint text,UNIQUE(request_id,api_key_id));
 CREATE TABLE usage_billing_dedup_archive(request_id text,api_key_id bigint,request_fingerprint text);
 INSERT INTO settings VALUES('official_point_budget','{"openai":"50","grok":"80"}',NOW());
 INSERT INTO users VALUES(1,NOW()-INTERVAL '2 days',NULL),(2,NOW()-INTERVAL '3 days',NULL);
 INSERT INTO groups VALUES(1,'coding',NULL);
 INSERT INTO accounts(id,name,platform,type,credentials) VALUES(10,'A','openai','oauth','{"chatgpt_account_id":"account-a"}'),(20,'B','openai','oauth','{"chatgpt_account_id":"account-b"}');
 INSERT INTO account_groups VALUES(10,1),(20,1);`)
	require.NoError(t, err)
	if seed != "" {
		_, err = db.Exec(seed)
		require.NoError(t, err)
	}
	migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "254_coding_quota_ledger.sql"))
	require.NoError(t, err)
	_, err = db.Exec(string(migration))
	require.NoError(t, err)
	var base time.Time
	require.NoError(t, db.QueryRow(`SELECT window_start FROM user_purpose_allowances WHERE user_id=1 AND purpose='openai'`).Scan(&base))
	return db, NewUsageMonitorRepository(db), base
}

func quotaObserve(t *testing.T, r *UsageMonitorRepository, account int64, at, end time.Time, used string) {
	t.Helper()
	require.NoError(t, r.ObserveCodingQuota(context.Background(), service.OfficialQuotaObservation{AccountID: account, Purpose: service.PurposeOpenAI, ObservedAt: at, WindowEnd: end, Used: used, Source: "codex_7d"}))
}

func quotaRequest(t *testing.T, db *sql.DB, key string, user, account int64, at time.Time, weight string, purpose string) {
	t.Helper()
	repo := &usageBillingRepository{db: db}
	_, err := repo.Apply(context.Background(), &service.UsageBillingCommand{RequestID: key, APIKeyID: user, UserID: user, AccountID: account,
		CodingQuota: &service.CodingQuotaCharge{AccountID: account, Purpose: purpose, Weight: weight, StartedAt: at.Add(-time.Second), CompletedAt: at}})
	require.NoError(t, err)
}

func quotaUsed(t *testing.T, r *UsageMonitorRepository, user int64, at time.Time, purpose string) service.CodingQuotaReading {
	t.Helper()
	rows, err := r.ReadCodingQuotas(context.Background(), []int64{user}, at)
	require.NoError(t, err)
	for _, row := range rows {
		if row.Purpose == purpose {
			return row
		}
	}
	t.Fatalf("missing %s quota", purpose)
	return service.CodingQuotaReading{}
}

func TestCodingQuotaDBEmptyLedgerHasNoInventedUpdateTime(t *testing.T) {
	db, r, base := quotaTestDB(t, "")
	_, err := db.Exec(`DELETE FROM coding_quota_requests`)
	require.NoError(t, err)
	readings, err := r.ReadCodingQuotas(context.Background(), []int64{1, 2}, base.Add(time.Hour))
	require.NoError(t, err)
	require.NotEmpty(t, readings)
	for _, reading := range readings {
		require.True(t, reading.UpdatedAt.IsZero(), "page reads must not invent ledger freshness")
	}
}

func TestCodingQuotaDBMultiUserMultiAccount(t *testing.T) {
	db, r, base := quotaTestDB(t, "")
	ctx := context.Background()
	end := base.Add(7 * 24 * time.Hour)
	quotaObserve(t, r, 10, base, end, "20")
	quotaObserve(t, r, 20, base, end, "10")
	require.NoError(t, r.ReconcileCodingQuota(ctx, base))
	quotaRequest(t, db, "a", 1, 10, base.Add(time.Minute), "1", "openai")
	quotaRequest(t, db, "b", 2, 10, base.Add(time.Minute), "2", "openai")
	quotaRequest(t, db, "c", 1, 20, base.Add(time.Minute), "2", "openai")
	quotaObserve(t, r, 10, base.Add(2*time.Minute), end, "26")
	quotaObserve(t, r, 20, base.Add(2*time.Minute), end, "13")
	require.NoError(t, r.ReconcileCodingQuota(ctx, base.Add(3*time.Minute)))
	require.Equal(t, "5.00000000", quotaUsed(t, r, 1, base.Add(time.Hour), "openai").Used)
	require.Equal(t, "4.00000000", quotaUsed(t, r, 2, base.Add(time.Hour), "openai").Used)
	quotaRequest(t, db, "a", 1, 10, base.Add(time.Minute), "1", "openai") // billing replay
	require.NoError(t, r.ReconcileCodingQuota(ctx, base.Add(3*time.Minute)))
	_, err := db.Exec(`UPDATE accounts SET deleted_at=NOW() WHERE id=10`)
	require.NoError(t, err)
	require.Equal(t, "5.00000000", quotaUsed(t, r, 1, base.Add(time.Hour), "openai").Used)
	require.Equal(t, "0", quotaUsed(t, r, 1, base.Add(7*24*time.Hour), "openai").Used)
	// Another user's anchor remains independent.
	require.Equal(t, "0", quotaUsed(t, r, 2, base.Add(6*24*time.Hour), "openai").Used)
}

func TestCodingQuotaDBResetOnlyAffectsCodexAndIsIdempotent(t *testing.T) {
	db, r, base := quotaTestDB(t, "")
	ctx := context.Background()
	_, err := db.Exec(`INSERT INTO coding_quota_requests(request_id,api_key_id,user_id,purpose,started_at,completed_at,weight,points,state)
 VALUES('before',1,1,'openai',$1,$1,1,20,'settled'),('grok',1,1,'grok',$1,$1,1,12,'settled'),('inflight',1,1,'openai',$2,$3,1,5,'settled'),('after',1,1,'openai',$3,$3,1,2,'settled')`, base.Add(time.Minute), base.Add(9*time.Minute), base.Add(11*time.Minute))
	require.NoError(t, err)
	event := service.CodingQuotaReset{ID: "manual:test-event", EffectiveAt: base.Add(10 * time.Minute), Note: "confirmed upstream event"}
	_, err = r.ConfirmCodingReset(ctx, event, 1)
	require.NoError(t, err)
	reading := quotaUsed(t, r, 1, base.Add(time.Hour), "openai")
	require.Equal(t, "2.00000000", reading.Used)
	require.True(t, reading.End.Equal(event.EffectiveAt.Add(7*24*time.Hour)))
	require.Equal(t, "12.00000000", quotaUsed(t, r, 1, base.Add(time.Hour), "grok").Used)
	event.EffectiveAt = base.Add(12 * time.Minute)
	_, err = r.ConfirmCodingReset(ctx, event, 1)
	require.NoError(t, err)
	require.True(t, quotaUsed(t, r, 1, base.Add(time.Hour), "openai").End.Equal(base.Add(10*time.Minute+7*24*time.Hour)))
}

// A platform event can predate deployment of the ledger. Confirming it later
// must replace the old personal clock, including when that clock already rolled
// into another week, without deleting post-event usage or resetting Grok.
func TestCodingQuotaDBHistoricalResetRestoresExhaustionAfterPersonalRollover(t *testing.T) {
	db, r, _ := quotaTestDB(t, "")
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	anchor := now.Add(-7*24*time.Hour - 2*time.Hour)
	effective := now.Add(-30 * time.Hour)
	opening := now.Add(-7 * time.Hour)
	_, err := db.Exec(`UPDATE users SET created_at=$1`, anchor)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE user_purpose_allowances SET window_start=$1`, anchor)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO coding_quota_requests(request_id,api_key_id,user_id,purpose,started_at,completed_at,weight,points,state)
 VALUES('opening:1:openai',0,1,'openai',$1,$1,0,51.2975,'imported'),
 ('opening:2:openai',0,2,'openai',$1,$1,0,4.4418,'imported'),
 ('post-event',2,2,'openai',$2,$2,1,3,'settled'),
 ('pre-event',1,1,'openai',$3,$3,1,40,'settled'),
 ('grok',1,1,'grok',$4,$4,1,12,'settled')`,
		opening, now.Add(-time.Hour), effective.Add(-time.Hour), now.Add(-time.Minute))
	require.NoError(t, err)
	before := quotaUsed(t, r, 1, now, service.PurposeOpenAI)
	require.Equal(t, "0", before.Used, "old personal clock has already excluded the exhausted opening entry")
	points := service.NewOfficialPointService(nil, r, nil)
	blocked, checked := points.Blocks(ctx, 1, service.PurposeOpenAI)
	require.True(t, checked)
	require.False(t, blocked)

	event := service.CodingQuotaReset{ID: "historical:official-global-reset", EffectiveAt: effective, Note: "operator confirmed historical platform-wide reset"}
	_, err = r.ConfirmCodingReset(ctx, event, 1)
	require.NoError(t, err)
	readings, err := r.ReadCodingQuotas(ctx, []int64{1, 2}, now)
	require.NoError(t, err)
	for _, reading := range readings {
		if reading.Purpose == service.PurposeOpenAI {
			require.True(t, reading.Start.Equal(effective))
			require.True(t, reading.End.Equal(effective.Add(7*24*time.Hour)))
			require.Equal(t, "platform_reset", reading.ResetReason)
		}
	}
	require.Equal(t, "51.29750000", quotaUsed(t, r, 1, now, service.PurposeOpenAI).Used)
	require.Equal(t, "7.44180000", quotaUsed(t, r, 2, now, service.PurposeOpenAI).Used)
	blocked, checked = points.Blocks(ctx, 1, service.PurposeOpenAI)
	require.True(t, checked)
	require.True(t, blocked, "dashboard ledger and authentication must both retain exhaustion")
	require.Equal(t, "12.00000000", quotaUsed(t, r, 1, now, service.PurposeGrok).Used)

	// Replaying confirmation with a later timestamp cannot postpone the reset.
	event.EffectiveAt = now
	_, err = r.ConfirmCodingReset(ctx, event, 1)
	require.NoError(t, err)
	require.True(t, quotaUsed(t, r, 1, now, service.PurposeOpenAI).End.Equal(effective.Add(7*24*time.Hour)))
	require.Equal(t, "0", quotaUsed(t, r, 1, effective.Add(7*24*time.Hour), service.PurposeOpenAI).Used)
	var count int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM coding_quota_requests`).Scan(&count))
	require.Equal(t, 5, count, "confirmation must preserve every charge")
}

func TestCodingQuotaDBUpgradeAndRestartPreserveConfirmedReset(t *testing.T) {
	db, r, base := quotaTestDB(t, "")
	ctx := context.Background()
	effective := base.Add(2 * time.Hour)
	now := base.Add(4 * time.Hour)
	_, err := db.Exec(`INSERT INTO coding_quota_requests(request_id,api_key_id,user_id,purpose,started_at,completed_at,weight,points,state)
 VALUES('exhausted',1,1,'openai',$1,$1,1,51,'settled'),
 ('other-user',2,2,'openai',$1,$1,1,7,'settled'),
 ('grok',1,1,'grok',$1,$1,1,12,'settled')`, base.Add(3*time.Hour))
	require.NoError(t, err)
	event, err := r.ConfirmCodingReset(ctx, service.CodingQuotaReset{
		ID: "confirmed:preserve-across-upgrades", EffectiveAt: effective, Note: "owner confirmed global reset",
	}, 1)
	require.NoError(t, err)
	before, err := r.ReadCodingQuotas(ctx, []int64{1, 2}, now)
	require.NoError(t, err)
	var anchorsBefore string
	require.NoError(t, db.QueryRow(`SELECT json_agg(a ORDER BY user_id,purpose)::text FROM user_purpose_allowances a`).Scan(&anchorsBefore))

	// Run the real additive migration through the startup runner, then replay
	// startup with a new DB connection and new repository/service instances.
	const name = "248_group_image_count_billing.sql"
	content, err := migrations.FS.ReadFile(name)
	require.NoError(t, err)
	upgrade := fstest.MapFS{name: &fstest.MapFile{Data: content}}
	const calibrationIndex = "255_coding_quota_calibration_index_notx.sql"
	content, err = migrations.FS.ReadFile(calibrationIndex)
	require.NoError(t, err)
	upgrade[calibrationIndex] = &fstest.MapFile{Data: content}
	require.NoError(t, applyMigrationsFS(ctx, db, upgrade))
	var schema string
	require.NoError(t, db.QueryRow(`SELECT current_schema()`).Scan(&schema))
	reopened, err := sql.Open("postgres", os.Getenv("SUB2API_QUOTA_TEST_DSN")+" search_path="+schema)
	require.NoError(t, err)
	defer func() { require.NoError(t, reopened.Close()) }()
	require.NoError(t, applyMigrationsFS(ctx, reopened, upgrade))
	fresh := NewUsageMonitorRepository(reopened)
	events, err := fresh.ListCodingResets(ctx)
	require.NoError(t, err)
	require.Equal(t, []service.CodingQuotaReset{event}, events)
	after, err := fresh.ReadCodingQuotas(ctx, []int64{1, 2}, now)
	require.NoError(t, err)
	require.Equal(t, before, after, "upgrade must preserve both purposes and both users at the same reference time")
	var anchorsAfter string
	require.NoError(t, reopened.QueryRow(`SELECT json_agg(a ORDER BY user_id,purpose)::text FROM user_purpose_allowances a`).Scan(&anchorsAfter))
	require.Equal(t, anchorsBefore, anchorsAfter)
	points := service.NewOfficialPointService(nil, fresh, nil)
	blocked, checked := points.Blocks(ctx, 1, service.PurposeOpenAI)
	require.True(t, checked)
	require.True(t, blocked, "a new process must continue blocking an exhausted user")
	var applications int
	require.NoError(t, reopened.QueryRow(`SELECT count(*) FROM schema_migrations WHERE filename=$1`, name).Scan(&applications))
	require.Equal(t, 1, applications, "startup replay must not run the migration twice")
}

func TestCodingQuotaDBWholePoolDetection(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		missing, local, single bool
		confirmed              bool
	}{
		{"corroborated whole pool", false, false, false, true},
		{"missing member", true, false, false, false},
		{"local reset cards", false, true, false, false},
		{"single account", false, false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, r, base := quotaTestDB(t, "")
			ctx := context.Background()
			end := base.Add(7 * 24 * time.Hour)
			accounts := []int64{10, 20}
			if tc.single {
				_, err := db.Exec(`DELETE FROM account_groups WHERE account_id=20`)
				require.NoError(t, err)
				accounts = []int64{10}
			}
			if tc.missing {
				_, err := db.Exec(`INSERT INTO accounts(id,name,platform,type,credentials) VALUES(30,'offline','openai','oauth','{"chatgpt_account_id":"account-c"}');INSERT INTO account_groups VALUES(30,1)`)
				require.NoError(t, err)
			}
			for _, id := range accounts {
				quotaObserve(t, r, id, base, end, "50")
			}
			require.NoError(t, r.ReconcileCodingQuota(ctx, base))
			if tc.local {
				_, err := db.Exec(`UPDATE accounts SET extra=jsonb_build_object('codex_local_reset_at',$1::text)`, base.Add(2*time.Minute).Format(time.RFC3339))
				require.NoError(t, err)
			}
			for _, id := range accounts {
				quotaObserve(t, r, id, base.Add(2*time.Minute), end.Add(24*time.Hour), "1")
			}
			require.NoError(t, r.ReconcileCodingQuota(ctx, base.Add(2*time.Minute)))
			for _, id := range accounts {
				quotaObserve(t, r, id, base.Add(3*time.Minute), end.Add(24*time.Hour), "2")
			}
			require.NoError(t, r.ReconcileCodingQuota(ctx, base.Add(3*time.Minute)))
			events, err := r.ListCodingResets(ctx)
			require.NoError(t, err)
			confirmed := false
			for _, e := range events {
				confirmed = confirmed || e.Status == "confirmed"
			}
			require.Equal(t, tc.confirmed, confirmed)
		})
	}
}

func TestCodingQuotaDBConcurrentWorkersConserveIncrement(t *testing.T) {
	db, r, base := quotaTestDB(t, "")
	ctx := context.Background()
	end := base.Add(7 * 24 * time.Hour)
	quotaObserve(t, r, 10, base, end, "0")
	require.NoError(t, r.ReconcileCodingQuota(ctx, base))
	for i := 0; i < 20; i++ {
		quotaRequest(t, db, fmt.Sprintf("parallel:%d", i), 1, 10, base.Add(time.Minute), "1", "openai")
	}
	quotaObserve(t, r, 10, base.Add(2*time.Minute), end, "10")
	var wg sync.WaitGroup
	failures := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); failures <- r.ReconcileCodingQuota(ctx, base.Add(3*time.Minute)) }()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	require.Equal(t, "10.00000000", quotaUsed(t, r, 1, base.Add(time.Hour), "openai").Used)
}

func TestCodingQuotaDBMigrationPreservesOpeningAndDefaults(t *testing.T) {
	db, r, _ := quotaTestDB(t, `UPDATE accounts SET extra=jsonb_build_object('codex_7d_used_percent',40,'codex_7d_reset_at',TO_CHAR(NOW()+INTERVAL '5 days','YYYY-MM-DD"T"HH24:MI:SSOF'),'codex_usage_updated_at',TO_CHAR(NOW()-INTERVAL '1 hour','YYYY-MM-DD"T"HH24:MI:SSOF')) WHERE id=10;
 INSERT INTO usage_logs(id,user_id,official_account_id,actual_cost,created_at) VALUES(1,1,10,10,NOW()-INTERVAL '1 day'),(2,2,10,30,NOW()-INTERVAL '1 day');`)
	reading := quotaUsed(t, r, 1, time.Now(), "openai")
	require.Equal(t, "10.00000000", reading.Used)
	require.True(t, reading.Imported)
	var budget, backup string
	require.NoError(t, db.QueryRow(`SELECT value FROM settings WHERE key='official_point_budget'`).Scan(&budget))
	require.Contains(t, budget, `"30"`)
	require.NoError(t, db.QueryRow(`SELECT value FROM settings WHERE key='quota_ledger_previous_point_budget'`).Scan(&backup))
	require.Contains(t, backup, `"80"`)
}

func TestCodingQuotaDBFixedWeekAcrossDST(t *testing.T) {
	db, r, _ := quotaTestDB(t, "")
	db.SetMaxOpenConns(1)
	anchor := time.Date(2026, 3, 7, 17, 0, 0, 0, time.UTC)
	_, err := db.Exec(`SET timezone='America/New_York'; UPDATE user_purpose_allowances SET window_start='2026-03-07T17:00:00Z' WHERE user_id=1`)
	require.NoError(t, err)
	reading := quotaUsed(t, r, 1, anchor.Add(4*24*time.Hour), "openai")
	require.True(t, reading.Start.Equal(anchor))
	require.Equal(t, 168*time.Hour, reading.End.Sub(reading.Start))
	reading = quotaUsed(t, r, 1, anchor.Add(168*time.Hour), "openai")
	require.True(t, reading.Start.Equal(anchor.Add(168*time.Hour)))
}

func TestCodingQuotaDBMigrationToleratesInvalidLegacyMetadata(t *testing.T) {
	db, _, _ := quotaTestDB(t, `UPDATE accounts SET extra='{"codex_7d_used_percent":20,"codex_7d_reset_at":"invalid","codex_usage_updated_at":"2026-99-99"}';UPDATE settings SET value='invalid-json' WHERE key='official_point_budget';`)
	var backup string
	require.NoError(t, db.QueryRow(`SELECT value FROM settings WHERE key='quota_ledger_previous_point_budget'`).Scan(&backup))
	require.Equal(t, "invalid-json", backup)
}

func TestCodingQuotaDBExperimentDelayedChargeAndClosedMonth(t *testing.T) {
	db, r, _ := quotaTestDB(t, "")
	ctx := context.Background()
	month := service.ShanghaiMonthStart(time.Now())
	_, err := db.Exec(`INSERT INTO user_purpose_allowances(user_id,purpose,limit_amount,used_amount,period,window_start) VALUES(1,'experiment',500,0,'month',$1)`, month)
	require.NoError(t, err)
	repo := &usageBillingRepository{db: db}
	for _, tc := range []struct {
		id string
		at time.Time
	}{{"late-current", month.Add(time.Minute)}, {"late-previous", month.Add(-time.Second)}} {
		_, err := repo.Apply(ctx, &service.UsageBillingCommand{RequestID: tc.id, APIKeyID: 1, UserID: 1, AccountID: 10, Purpose: "experiment", StartedAt: tc.at, ExperimentPending: true, ExperimentUpstreamRequestID: tc.id})
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO usage_logs(request_id,api_key_id,actual_cost) VALUES($1,1,0)`, tc.id)
		require.NoError(t, err)
	}
	state, err := r.ExperimentQuotaState(ctx, 1, time.Now())
	require.NoError(t, err)
	require.Equal(t, "pending", state)
	items, err := r.ListPendingExperimentCharges(ctx, time.Now())
	require.NoError(t, err)
	require.Len(t, items, 2)
	for _, item := range items {
		require.NoError(t, r.CompleteExperimentCharge(ctx, item.ID, 4.73))
		require.NoError(t, r.CompleteExperimentCharge(ctx, item.ID, 4.73))
	}
	var used string
	require.NoError(t, db.QueryRow(`SELECT used_amount::text FROM user_purpose_allowances WHERE user_id=1 AND purpose='experiment'`).Scan(&used))
	require.Equal(t, "4.73000000", used)
	var logTotal float64
	require.NoError(t, db.QueryRow(`SELECT SUM(actual_cost) FROM usage_logs`).Scan(&logTotal))
	require.InDelta(t, 9.46, logTotal, 1e-8)
	state, err = r.ExperimentQuotaState(ctx, 1, time.Now())
	require.NoError(t, err)
	require.Equal(t, "current", state)
}

func TestCodingQuotaDBDeferredKeyCountersKeepClosedWindows(t *testing.T) {
	db, r, _ := quotaTestDB(t, "")
	_, err := db.Exec(`CREATE TABLE api_keys(id bigint PRIMARY KEY,quota numeric NOT NULL,quota_used numeric NOT NULL,status text,deleted_at timestamptz,updated_at timestamptz,usage_5h numeric,usage_1d numeric,usage_7d numeric,window_5h_start timestamptz,window_1d_start timestamptz,window_7d_start timestamptz);
 CREATE TABLE user_subscriptions(id bigint PRIMARY KEY,daily_usage_usd numeric,weekly_usage_usd numeric,monthly_usage_usd numeric,daily_window_start timestamptz,weekly_window_start timestamptz,monthly_window_start timestamptz,deleted_at timestamptz,updated_at timestamptz);
 INSERT INTO user_subscriptions VALUES(1,0,0,0,NOW()-INTERVAL '1 hour',NOW()-INTERVAL '1 hour',NOW()-INTERVAL '1 hour',NULL,NOW());
 ALTER TABLE accounts ADD COLUMN updated_at timestamptz;
 UPDATE accounts SET extra=jsonb_build_object('quota_used',0,'quota_daily_limit',100,'quota_weekly_limit',100,'quota_daily_start',NOW()-INTERVAL '1 hour','quota_weekly_start',NOW()-INTERVAL '1 hour');
 CREATE TABLE scheduler_outbox(event_type text,account_id bigint,group_id bigint,payload jsonb,dedup_key text);
 CREATE UNIQUE INDEX scheduler_outbox_dedup ON scheduler_outbox(dedup_key) WHERE dedup_key IS NOT NULL;
 INSERT INTO api_keys VALUES(1,5,1,'active',NULL,NOW(),0,0,0,NOW()-INTERVAL '1 hour',NOW()-INTERVAL '1 hour',NOW()-INTERVAL '1 hour');
 INSERT INTO user_purpose_allowances VALUES(1,'experiment',500,0,'month',DATE_TRUNC('month',NOW() AT TIME ZONE 'Asia/Shanghai') AT TIME ZONE 'Asia/Shanghai',NOW());`)
	require.NoError(t, err)
	repo := &usageBillingRepository{db: db}
	subscriptionID := int64(1)
	for _, tc := range []struct {
		id string
		at time.Time
	}{{"open", time.Now().Add(-time.Minute)}, {"closed", time.Now().Add(-2 * time.Hour)}} {
		_, err = repo.Apply(context.Background(), &service.UsageBillingCommand{RequestID: tc.id, APIKeyID: 1, UserID: 1, AccountID: 10, Purpose: "experiment", StartedAt: tc.at, ExperimentPending: true, ExperimentUpstreamRequestID: tc.id, ExperimentEffects: &service.ExperimentBillingEffects{KeyQuota: true, KeyRateLimits: true, SubscriptionID: &subscriptionID, AccountQuota: true, AccountMultiplier: 1.5}})
		require.NoError(t, err)
	}
	items, err := r.ListPendingExperimentCharges(context.Background(), time.Now())
	require.NoError(t, err)
	for _, item := range items {
		require.NoError(t, r.CompleteExperimentCharge(context.Background(), item.ID, 2))
		require.NoError(t, r.CompleteExperimentCharge(context.Background(), item.ID, 2))
	}
	var lifetime, hour, day, week float64
	var status string
	require.NoError(t, db.QueryRow(`SELECT quota_used,usage_5h,usage_1d,usage_7d,status FROM api_keys WHERE id=1`).Scan(&lifetime, &hour, &day, &week, &status))
	require.Equal(t, float64(5), lifetime)
	require.Equal(t, float64(2), hour)
	require.Equal(t, float64(2), day)
	require.Equal(t, float64(2), week)
	require.Equal(t, service.StatusAPIKeyQuotaExhausted, status)
	var subDay, subWeek, subMonth, accountTotal, accountDay, accountWeek float64
	require.NoError(t, db.QueryRow(`SELECT daily_usage_usd,weekly_usage_usd,monthly_usage_usd FROM user_subscriptions WHERE id=1`).Scan(&subDay, &subWeek, &subMonth))
	require.Equal(t, float64(2), subDay)
	require.Equal(t, float64(2), subWeek)
	require.Equal(t, float64(2), subMonth)
	require.NoError(t, db.QueryRow(`SELECT (extra->>'quota_used')::numeric,(extra->>'quota_daily_used')::numeric,(extra->>'quota_weekly_used')::numeric FROM accounts WHERE id=10`).Scan(&accountTotal, &accountDay, &accountWeek))
	require.Equal(t, float64(6), accountTotal)
	require.Equal(t, float64(3), accountDay)
	require.Equal(t, float64(3), accountWeek)
	_, err = db.Exec(`INSERT INTO api_keys VALUES(2,5,0,'active',NULL,NOW(),0,0,0,NULL,NULL,NULL)`)
	require.NoError(t, err)
	_, err = repo.Apply(context.Background(), &service.UsageBillingCommand{RequestID: "first-pending", APIKeyID: 2, UserID: 1, AccountID: 10, Purpose: "experiment", StartedAt: time.Now().Add(-time.Minute), ExperimentPending: true, ExperimentUpstreamRequestID: "first-pending", ExperimentEffects: &service.ExperimentBillingEffects{KeyRateLimits: true}})
	require.NoError(t, err)
	items, err = r.ListPendingExperimentCharges(context.Background(), time.Now())
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.NoError(t, r.CompleteExperimentCharge(context.Background(), items[0].ID, 2))
	require.NoError(t, db.QueryRow(`SELECT usage_5h,usage_1d,usage_7d FROM api_keys WHERE id=2`).Scan(&hour, &day, &week))
	require.Equal(t, float64(2), hour)
	require.Equal(t, float64(2), day)
	require.Equal(t, float64(2), week)
}
