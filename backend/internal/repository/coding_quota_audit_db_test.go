package repository

import (
	"context"
	"database/sql"
	"os"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/quotaudit"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func quotaAuditFixture(t *testing.T) (*sql.DB, *UsageMonitorRepository, quotaudit.Plan, time.Time) {
	t.Helper()
	db, r, base := quotaTestDB(t, "")
	ctx := context.Background()
	end := base.Add(7 * 24 * time.Hour)
	for i, used := range []string{"69", "68", "69"} {
		quotaObserve(t, r, 10, base.Add(time.Duration(i)*time.Minute), end, used)
	}
	_, err := db.Exec(`UPDATE coding_quota_observations SET processed=TRUE`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO coding_quota_requests(request_id,api_key_id,user_id,purpose,account_id,started_at,completed_at,official_window_end,weight,points,state,updated_at)
 VALUES('duplicate-a',1,1,'openai',10,$1,$2,$4,1,.25,'settled',$5),
 ('duplicate-b',2,2,'openai',10,$1,$3,$4,3,.75,'settled',$5),
 ('real-exhausted',1,1,'openai',20,$1,$1,$4,1,50.5,'settled',$1),
 ('real-grok',1,1,'grok',NULL,$1,$1,NULL,1,13,'settled',$1)`,
		base.Add(40*time.Second), base.Add(80*time.Second), base.Add(90*time.Second), end, base.Add(3*time.Minute))
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO coding_quota_meters(account_id,purpose,window_end,observed_at,used,points_per_weight) VALUES(10,'openai',$1,$2,69,.25)`, end, base.Add(2*time.Minute))
	require.NoError(t, err)
	_, err = r.ConfirmCodingReset(ctx, service.CodingQuotaReset{ID: "confirmed:before-recovery", EffectiveAt: base.Add(-time.Hour), Note: "owner confirmed older reset"}, 1)
	require.NoError(t, err)
	snapshot, err := r.CodingQuotaAuditSnapshot(ctx, 10, base, base.Add(time.Hour))
	require.NoError(t, err)
	plan, err := quotaudit.Build(snapshot)
	require.NoError(t, err)
	require.Len(t, plan.Episodes, 1)
	return db, r, plan, base
}

func TestCodingQuotaDBAuditedRecoveryPreservesRawLedgerAndClocks(t *testing.T) {
	db, r, plan, base := quotaAuditFixture(t)
	ctx := context.Background()
	before, err := r.ReadCodingQuotas(ctx, []int64{1, 2}, base.Add(time.Hour))
	require.NoError(t, err)
	var raw string
	require.NoError(t, db.QueryRow(`SELECT json_agg(q ORDER BY id)::text FROM coding_quota_requests q`).Scan(&raw))
	n, err := r.ApplyCodingQuotaAudit(ctx, plan, quotaudit.Digest(plan), "operator:test", "owner requested historical reconciliation")
	require.NoError(t, err)
	require.Equal(t, 2, n)
	require.Equal(t, "50.50000000", quotaUsed(t, r, 1, base.Add(time.Hour), "openai").Used)
	require.Equal(t, "0.00000000", quotaUsed(t, r, 2, base.Add(time.Hour), "openai").Used)
	require.Equal(t, "13.00000000", quotaUsed(t, r, 1, base.Add(time.Hour), "grok").Used)
	var rate string
	require.NoError(t, db.QueryRow(`SELECT points_per_weight::text FROM coding_quota_meters WHERE account_id=10`).Scan(&rate))
	require.Equal(t, "0.000000000000", rate, "refunded duplicate points must not train future estimates")
	blocked, checked := service.NewOfficialPointService(nil, r, nil).Blocks(ctx, 1, "openai")
	require.True(t, checked)
	require.True(t, blocked, "real exhaustion must survive a refund of only duplicate points")
	var afterRaw string
	require.NoError(t, db.QueryRow(`SELECT json_agg(q ORDER BY id)::text FROM coding_quota_requests q`).Scan(&afterRaw))
	require.Equal(t, raw, afterRaw)
	after, err := r.ReadCodingQuotas(ctx, []int64{1, 2}, base.Add(time.Hour))
	require.NoError(t, err)
	for i, row := range after {
		require.Equal(t, before[i].Start, row.Start)
		require.Equal(t, before[i].End, row.End)
		require.Equal(t, before[i].ResetReason, row.ResetReason)
	}
	n, err = r.ApplyCodingQuotaAudit(ctx, plan, quotaudit.Digest(plan), "operator:test", "retry")
	require.NoError(t, err)
	require.Zero(t, n)
	var schema string
	require.NoError(t, db.QueryRow(`SELECT current_schema()`).Scan(&schema))
	reopened, err := sql.Open("postgres", os.Getenv("SUB2API_QUOTA_TEST_DSN")+" search_path="+schema)
	require.NoError(t, err)
	defer func() { require.NoError(t, reopened.Close()) }()
	migration, err := migrations.FS.ReadFile("256_coding_quota_corrections.sql")
	require.NoError(t, err)
	require.NoError(t, applyMigrationsFS(ctx, reopened, fstest.MapFS{"256_coding_quota_corrections.sql": &fstest.MapFile{Data: migration}}))
	fresh := NewUsageMonitorRepository(reopened)
	require.Equal(t, "50.50000000", quotaUsed(t, fresh, 1, base.Add(time.Hour), "openai").Used)
	require.Equal(t, "0", quotaUsed(t, fresh, 1, base.Add(169*time.Hour), "openai").Used, "credits belong to the original request period")
	_, err = db.Exec(`UPDATE coding_quota_corrections SET delta=0`)
	require.Error(t, err, "audit lines must be append-only")
	_, err = db.Exec(`DELETE FROM coding_quota_reconciliations`)
	require.Error(t, err, "audit evidence cannot be erased")
}

func TestCodingQuotaDBAuditRejectsEditedPlanAndMissingOperator(t *testing.T) {
	db, r, plan, _ := quotaAuditFixture(t)
	ctx := context.Background()
	_, err := r.ApplyCodingQuotaAudit(ctx, plan, "", "operator:test", "missing digest")
	require.Error(t, err)
	_, err = r.ApplyCodingQuotaAudit(ctx, plan, quotaudit.Digest(plan), "", "missing actor")
	require.Error(t, err)
	plan.Episodes[0].Entries[0].Delta = "-0.50000000"
	_, err = r.ApplyCodingQuotaAudit(ctx, plan, quotaudit.Digest(plan), "operator:test", "edited amount")
	require.Error(t, err)
	var count int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM coding_quota_reconciliations`).Scan(&count))
	require.Zero(t, count)
}

func TestCodingQuotaDBAuditRejectsChangedEvidenceAtomically(t *testing.T) {
	db, r, plan, _ := quotaAuditFixture(t)
	_, err := db.Exec(`UPDATE coding_quota_requests SET points=.9 WHERE request_id='duplicate-b'`)
	require.NoError(t, err)
	_, err = r.ApplyCodingQuotaAudit(context.Background(), plan, quotaudit.Digest(plan), "operator:test", "stale plan")
	require.Error(t, err)
	var count int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM coding_quota_corrections`).Scan(&count))
	require.Zero(t, count)
}

func TestCodingQuotaDBConcurrentAuditDoesNotRefundTwice(t *testing.T) {
	db, r, plan, base := quotaAuditFixture(t)
	var wg sync.WaitGroup
	errors := make(chan error, 4)
	counts := make(chan int, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n, err := r.ApplyCodingQuotaAudit(context.Background(), plan, quotaudit.Digest(plan), "operator:test", "retry concurrently")
			errors <- err
			counts <- n
		}()
	}
	wg.Wait()
	close(errors)
	close(counts)
	for err := range errors {
		require.NoError(t, err)
	}
	total := 0
	for n := range counts {
		total += n
	}
	require.Equal(t, 2, total)
	var credit string
	require.NoError(t, db.QueryRow(`SELECT sum(delta)::text FROM coding_quota_corrections`).Scan(&credit))
	require.Equal(t, "-1.00000000", credit)
	require.Equal(t, "50.50000000", quotaUsed(t, r, 1, base.Add(time.Hour), "openai").Used)
}
