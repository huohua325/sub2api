package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func fallbackTestDB(t *testing.T) (*sql.DB, *UsageMonitorRepository) {
	t.Helper()
	db, r, _ := quotaTestDB(t, "")
	_, err := db.Exec(`ALTER TABLE users ADD COLUMN role text NOT NULL DEFAULT 'user';
 ALTER TABLE users ADD COLUMN status text NOT NULL DEFAULT 'active';
 ALTER TABLE groups ADD COLUMN platform text NOT NULL DEFAULT 'composite';
 ALTER TABLE groups ADD COLUMN status text NOT NULL DEFAULT 'active';
 ALTER TABLE accounts ADD COLUMN status text NOT NULL DEFAULT 'active';
 ALTER TABLE accounts ADD COLUMN updated_at timestamptz NOT NULL DEFAULT 'epoch';
 UPDATE users SET role='admin' WHERE id=1;
 CREATE TABLE api_keys(id bigint PRIMARY KEY,user_id bigint,group_id bigint,deleted_at timestamptz,status text DEFAULT 'active',quota numeric DEFAULT 0,quota_used numeric DEFAULT 0,expires_at timestamptz,rate_limit_5h numeric DEFAULT 0,rate_limit_1d numeric DEFAULT 0,rate_limit_7d numeric DEFAULT 0,usage_5h numeric DEFAULT 0,usage_1d numeric DEFAULT 0,usage_7d numeric DEFAULT 0,window_5h_start timestamptz,window_1d_start timestamptz,window_7d_start timestamptz,updated_at timestamptz DEFAULT now());
 INSERT INTO api_keys(id,user_id,group_id) VALUES(1,1,1),(2,2,1);
 INSERT INTO accounts(id,name,platform,type) VALUES(30,'backup','openai','apikey');
 INSERT INTO account_groups VALUES(30,1);`)
	require.NoError(t, err)
	raw, err := migrations.FS.ReadFile("271_api_fallback_control.sql")
	require.NoError(t, err)
	require.NoError(t, applyMigrationsFS(t.Context(), db, fstest.MapFS{"271_api_fallback_control.sql": &fstest.MapFile{Data: raw}}))
	return db, r
}

func fallbackTestPolicy(t *testing.T, r *UsageMonitorRepository, edit func(*service.APIFallbackPolicy)) *service.APIFallbackPolicy {
	t.Helper()
	expiry := time.Now().UTC().Add(time.Hour)
	p := service.APIFallbackPolicy{GroupID: 1, Platform: "openai", Model: "gpt-test", PrimaryAccountIDs: []int64{10, 20}, BackupAccountIDs: []int64{30}, Enabled: true, CapabilitiesVerified: true, ExpiresAt: &expiry, PointsPerUSD: "1", SupplierMultiplier: "1", InputPricePerMillion: "1", CachedInputPricePerMillion: "0.1", OutputPricePerMillion: "2", MaxInputTokens: 1000, MaxOutputTokens: 1000, PerRequestBudgetUSD: "1", DailyBudgetUSD: "10", TotalBudgetUSD: "20", Note: "test verified supplier"}
	if edit != nil {
		edit(&p)
	}
	out, err := r.SaveAPIFallbackPolicy(t.Context(), service.APIFallbackPolicyInput{APIFallbackPolicy: p, ActorUserID: 1, Confirm: true})
	require.NoError(t, err)
	return out
}

func fallbackReserveInput(p *service.APIFallbackPolicy) service.APIFallbackReservationInput {
	return service.APIFallbackReservationInput{ID: uuid.NewString(), RequestID: uuid.NewString(), PolicyID: p.ID, PolicyVersion: p.Version, UserID: 1, APIKeyID: 1, AccountID: 30, GroupID: 1, Platform: "openai", Model: p.Model, EstimatedInputTokens: 100, MaxOutputTokens: 100, PersonalLimit: "50", StartedAt: time.Now().UTC().Truncate(time.Microsecond), Now: time.Now().UTC()}
}

func TestCodingQuotaDBAPIFallbackAdminAndExplicitApproval(t *testing.T) {
	db, r := fallbackTestDB(t)
	_, err := r.ReadAPIFallback(t.Context(), 2)
	require.ErrorContains(t, err, "administrator")
	p := fallbackTestPolicy(t, r, nil)
	input := service.APIFallbackPolicyInput{APIFallbackPolicy: *p, ExpectedVersion: p.Version, Confirm: true, ActorUserID: 2}
	_, err = r.SaveAPIFallbackPolicy(t.Context(), input)
	require.ErrorContains(t, err, "administrator")
	in := fallbackReserveInput(p)
	in.UserID = 2
	in.APIKeyID = 2
	_, err = r.ReserveAPIFallback(t.Context(), in)
	require.ErrorContains(t, err, "administrator")
	input.ActorUserID = 1
	input.Confirm = false
	_, err = r.SaveAPIFallbackPolicy(t.Context(), input)
	require.ErrorContains(t, err, "confirmation")
	p.PointsPerUSD = "0"
	input.APIFallbackPolicy = *p
	input.Confirm = true
	_, err = r.SaveAPIFallbackPolicy(t.Context(), input)
	require.ErrorContains(t, err, "positive")
	_, err = db.Exec(`UPDATE users SET status='disabled' WHERE id=1`)
	require.NoError(t, err)
	_, err = r.ReadAPIFallback(t.Context(), 1)
	require.ErrorContains(t, err, "administrator")
	_, err = db.Exec(`UPDATE users SET status='active' WHERE id=1; UPDATE groups SET usage_kind='experiment' WHERE id=1`)
	require.NoError(t, err)
	p.Enabled = false
	input.APIFallbackPolicy = *p
	_, err = r.SaveAPIFallbackPolicy(t.Context(), input)
	require.ErrorContains(t, err, "coding group")
}

func TestCodingQuotaDBAPIFallbackConcurrentBudgetAndUnknownRetention(t *testing.T) {
	db, r := fallbackTestDB(t)
	db.SetMaxOpenConns(20)
	p := fallbackTestPolicy(t, r, func(p *service.APIFallbackPolicy) {
		p.PerRequestBudgetUSD = "0.0003"
		p.DailyBudgetUSD = "0.0006"
		p.TotalBudgetUSD = "0.0006"
	})
	var wg sync.WaitGroup
	results := make(chan *service.APIFallbackAttempt, 20)
	failures := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a, e := r.ReserveAPIFallback(context.Background(), fallbackReserveInput(p))
			if e != nil {
				failures <- e
			} else {
				results <- a
			}
		}()
	}
	wg.Wait()
	close(results)
	close(failures)
	accepted := make([]*service.APIFallbackAttempt, 0)
	for a := range results {
		accepted = append(accepted, a)
	}
	require.Len(t, accepted, 2)
	for err := range failures {
		require.ErrorContains(t, err, "budget exhausted")
	}
	for _, a := range accepted {
		require.NoError(t, r.MarkAPIFallbackSent(t.Context(), a.ID))
		require.NoError(t, r.MarkAPIFallbackUnknown(t.Context(), a.ID, "stream interrupted"))
		require.Error(t, r.CancelAPIFallbackUnsent(t.Context(), a.ID, "not known free"))
	}
	// Restarting a repository or disabling/re-enabling never frees unknown spend.
	r = NewUsageMonitorRepository(db)
	view, err := r.ReadAPIFallback(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, int64(2), view.Summary.UnresolvedCount)
	require.True(t, decimal.RequireFromString(view.Summary.UnresolvedUSD).Equal(decimal.RequireFromString("0.0006")))
	_, err = r.ReserveAPIFallback(t.Context(), fallbackReserveInput(p))
	require.ErrorContains(t, err, "budget exhausted")
}

func TestCodingQuotaDBAPIFallbackPersonalReservationAcrossKeys(t *testing.T) {
	db, r := fallbackTestDB(t)
	_, err := db.Exec(`INSERT INTO api_keys(id,user_id,group_id) VALUES(3,1,1)`)
	require.NoError(t, err)
	p := fallbackTestPolicy(t, r, nil)
	in := fallbackReserveInput(p)
	in.PersonalLimit = "0.0004"
	_, err = r.ReserveAPIFallback(t.Context(), in)
	require.NoError(t, err)
	second := fallbackReserveInput(p)
	second.APIKeyID = 3
	second.PersonalLimit = "0.0004"
	_, err = r.ReserveAPIFallback(t.Context(), second)
	require.ErrorIs(t, err, service.ErrCodingQuotaExhausted)
	require.NoError(t, r.CancelAPIFallbackUnsent(t.Context(), in.ID, "not sent"))
	_, err = r.ReserveAPIFallback(t.Context(), second)
	require.NoError(t, err)
}

func TestCodingQuotaDBAPIFallbackFrozenReceiptAndExactlyOnce(t *testing.T) {
	db, r := fallbackTestDB(t)
	p := fallbackTestPolicy(t, r, nil)
	in := fallbackReserveInput(p)
	a, err := r.ReserveAPIFallback(t.Context(), in)
	require.NoError(t, err)
	_, err = r.ReserveAPIFallback(t.Context(), in)
	require.NoError(t, err)
	require.NoError(t, r.MarkAPIFallbackSent(t.Context(), a.ID))
	p.PointsPerUSD = "20"
	p.SupplierMultiplier = "0.3"
	p, err = r.SaveAPIFallbackPolicy(t.Context(), service.APIFallbackPolicyInput{APIFallbackPolicy: *p, ExpectedVersion: p.Version, ActorUserID: 1, Confirm: true})
	require.NoError(t, err)
	receipt := service.APIFallbackSettlementInput{ID: a.ID, InputTokens: 100, CachedInputTokens: 50, OutputTokens: 20, Usage: []byte(`{"input_tokens":100,"output_tokens":20}`), Now: time.Now().UTC()}
	a, err = r.SettleAPIFallback(t.Context(), receipt)
	require.NoError(t, err)
	require.Equal(t, "settled", a.State)
	require.True(t, decimal.RequireFromString(a.Points).Equal(decimal.RequireFromString("0.000095")))
	receipt.Now = receipt.Now.Add(time.Minute)
	_, err = r.SettleAPIFallback(t.Context(), receipt)
	require.NoError(t, err)
	var count int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM coding_quota_requests WHERE metering_source='api_fallback'`).Scan(&count))
	require.Equal(t, 1, count)
	receipt.OutputTokens++
	_, err = r.SettleAPIFallback(t.Context(), receipt)
	require.Error(t, err)
	var balanceCount int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM api_fallback_policy_versions WHERE policy_id=$1`, p.ID).Scan(&balanceCount))
	require.Equal(t, 2, balanceCount)
	// Costs were frozen at version 1, unaffected by the revised multiplier.
	require.True(t, decimal.RequireFromString(a.SupplierUSD).Equal(decimal.RequireFromString(a.OfficialUSD)))
}

func TestCodingQuotaDBAPIFallbackDisableAndEvidenceResolution(t *testing.T) {
	db, r := fallbackTestDB(t)
	p := fallbackTestPolicy(t, r, nil)
	in := fallbackReserveInput(p)
	a, err := r.ReserveAPIFallback(t.Context(), in)
	require.NoError(t, err)
	require.NoError(t, r.MarkAPIFallbackSent(t.Context(), a.ID))
	require.NoError(t, r.MarkAPIFallbackUnknown(t.Context(), a.ID, "no receipt"))
	p, err = r.DisableAPIFallbackPolicy(t.Context(), p.ID, p.Version, 1, "administrator stopped")
	require.NoError(t, err)
	_, err = r.ReserveAPIFallback(t.Context(), fallbackReserveInput(p))
	require.Error(t, err)
	resolve := service.APIFallbackResolveInput{ActorUserID: 1, ID: a.ID, Confirm: true, Evidence: "supplier request receipt confirms not billed", Outcome: "not_billed"}
	resolve.Evidence = ""
	_, err = r.ResolveAPIFallback(t.Context(), resolve)
	require.Error(t, err)
	resolve.Evidence = "supplier request receipt confirms not billed"
	a, err = r.ResolveAPIFallback(t.Context(), resolve)
	require.NoError(t, err)
	require.Equal(t, "settled", a.State)
	require.True(t, decimal.RequireFromString(a.SupplierUSD).IsZero())
	_, err = r.ResolveAPIFallback(t.Context(), resolve)
	require.NoError(t, err)
	resolve.Evidence = "different retrospective evidence"
	_, err = r.ResolveAPIFallback(t.Context(), resolve)
	require.Error(t, err)
	var n int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM api_fallback_events WHERE event_type='admin_resolved'`).Scan(&n))
	require.Equal(t, 1, n)
	_, err = db.Exec(`UPDATE api_fallback_events SET evidence='overwritten' WHERE event_type='admin_resolved'`)
	require.Error(t, err)
	_, err = db.Exec(`DELETE FROM api_fallback_policy_versions WHERE policy_id=$1`, p.ID)
	require.Error(t, err)
}

func TestCodingQuotaDBAPIFallbackDraftAndSendTimeRevocation(t *testing.T) {
	db, r := fallbackTestDB(t)
	draft, err := r.SaveAPIFallbackPolicy(t.Context(), service.APIFallbackPolicyInput{APIFallbackPolicy: service.APIFallbackPolicy{GroupID: 1, Platform: "openai", Model: "draft-model", Note: "draft only"}, Confirm: true, ActorUserID: 1})
	require.NoError(t, err)
	require.False(t, draft.Enabled)
	require.Equal(t, "0", draft.PointsPerUSD)
	p := fallbackTestPolicy(t, r, nil)
	in := fallbackReserveInput(p)
	a, err := r.ReserveAPIFallback(t.Context(), in)
	require.NoError(t, err)
	_, err = db.Exec(`DELETE FROM account_groups WHERE account_id=30`)
	require.NoError(t, err)
	require.Error(t, r.MarkAPIFallbackSent(t.Context(), a.ID))
	// Even broken/deleted membership cannot prevent emergency disable.
	_, err = r.DisableAPIFallbackPolicy(t.Context(), p.ID, p.Version, 1, "stop broken configuration")
	require.NoError(t, err)
	require.NoError(t, r.CancelAPIFallbackUnsent(t.Context(), a.ID, "never sent"))
	view, err := r.ReadAPIFallback(t.Context(), 1)
	require.NoError(t, err)
	require.Len(t, view.Policies, 2)
	_, err = r.FindAPIFallbackPolicy(t.Context(), 1, "openai", fmt.Sprint("not-existing"))
	require.NoError(t, err)
}

func TestCodingQuotaDBAPIFallbackLineageNeverCrossesIdentity(t *testing.T) {
	db, r := fallbackTestDB(t)
	p := fallbackTestPolicy(t, r, nil)
	longHistory := make([]string, 101)
	for i := range longHistory {
		sum := sha256.Sum256([]byte(fmt.Sprintf("subscription reasoning %d", i)))
		longHistory[i] = hex.EncodeToString(sum[:])
	}
	id, err := r.ResolveAPIFallbackLineage(t.Context(), 1, 1, 1, p.Model, longHistory)
	require.NoError(t, err, "long subscription history must pass when no standby state exists")
	require.Zero(t, id)
	in := fallbackReserveInput(p)
	a, err := r.ReserveAPIFallback(t.Context(), in)
	require.NoError(t, err)
	require.NoError(t, r.MarkAPIFallbackSent(t.Context(), a.ID))
	sum := sha256.Sum256([]byte("opaque provider response reasoning"))
	digest := hex.EncodeToString(sum[:])
	usage, err := json.Marshal(map[string]any{"encrypted_digests": []string{digest}, "response_id": "resp-first"})
	require.NoError(t, err)
	_, err = r.SettleAPIFallback(t.Context(), service.APIFallbackSettlementInput{ID: a.ID, InputTokens: 100, OutputTokens: 10, Usage: usage})
	require.NoError(t, err)
	id, err = r.ResolveAPIFallbackLineage(t.Context(), 1, 1, 1, p.Model, []string{digest})
	require.NoError(t, err)
	require.Equal(t, int64(30), id)
	longHistory[0] = digest
	_, err = r.ResolveAPIFallbackLineage(t.Context(), 1, 1, 1, p.Model, longHistory)
	require.Error(t, err, "unverified long history may contain standby state")
	id, err = r.ResolveAPIFallbackLineage(t.Context(), 1, 1, 1, "another-model", longHistory)
	require.NoError(t, err)
	require.Zero(t, id)
	for _, scope := range []struct {
		user, key, group int64
		model            string
	}{{2, 1, 1, p.Model}, {1, 2, 1, p.Model}, {1, 1, 2, p.Model}, {1, 1, 1, "different-model"}} {
		id, err = r.ResolveAPIFallbackLineage(t.Context(), scope.user, scope.key, scope.group, scope.model, []string{digest})
		require.NoError(t, err)
		require.Zero(t, id)
	}
	missing := sha256.Sum256([]byte("different response"))
	id, err = r.ResolveAPIFallbackLineage(t.Context(), 1, 1, 1, p.Model, []string{digest, hex.EncodeToString(missing[:])})
	require.NoError(t, err)
	require.Zero(t, id)
	var stored string
	require.NoError(t, db.QueryRow(`SELECT usage->>'response_id' FROM api_fallback_attempts WHERE id=$1`, a.ID).Scan(&stored))
	require.Equal(t, "resp-first", stored)
	_, err = db.Exec(`UPDATE accounts SET updated_at=clock_timestamp() WHERE id=30`)
	require.NoError(t, err)
	_, err = r.ResolveAPIFallbackLineage(t.Context(), 1, 1, 1, p.Model, []string{digest})
	require.Error(t, err, "old encrypted state cannot follow an edited endpoint or credential")
}

func TestCodingQuotaDBAPIFallbackAccountEditsRequireApproval(t *testing.T) {
	db, r := fallbackTestDB(t)
	p := fallbackTestPolicy(t, r, nil)
	in := fallbackReserveInput(p)
	a, err := r.ReserveAPIFallback(t.Context(), in)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE accounts SET updated_at=clock_timestamp() WHERE id=30`)
	require.NoError(t, err)
	require.Error(t, r.MarkAPIFallbackSent(t.Context(), a.ID))
	_, err = r.ReserveAPIFallback(t.Context(), fallbackReserveInput(p))
	require.Error(t, err)
	p, err = r.SaveAPIFallbackPolicy(t.Context(), service.APIFallbackPolicyInput{APIFallbackPolicy: *p, ExpectedVersion: p.Version, Confirm: true, ActorUserID: 1})
	require.NoError(t, err)
	_, err = r.ReserveAPIFallback(t.Context(), fallbackReserveInput(p))
	require.NoError(t, err, "new explicit approval authorizes the updated destination")
}

func TestCodingQuotaDBAPIFallbackRecoveryKeepsUnknownSpend(t *testing.T) {
	db, r := fallbackTestDB(t)
	p := fallbackTestPolicy(t, r, nil)
	reserved, err := r.ReserveAPIFallback(t.Context(), fallbackReserveInput(p))
	require.NoError(t, err)
	sent, err := r.ReserveAPIFallback(t.Context(), fallbackReserveInput(p))
	require.NoError(t, err)
	require.NoError(t, r.MarkAPIFallbackSent(t.Context(), sent.ID))
	_, err = db.Exec(`UPDATE api_fallback_attempts SET updated_at=clock_timestamp()-INTERVAL '11 minutes' WHERE id IN($1,$2)`, reserved.ID, sent.ID)
	require.NoError(t, err)
	require.NoError(t, r.RecoverAPIFallback(t.Context(), time.Now().UTC().Add(-10*time.Minute)))
	require.NoError(t, r.RecoverAPIFallback(t.Context(), time.Now().UTC().Add(-10*time.Minute)))
	view, err := r.ReadAPIFallback(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, int64(2), view.Summary.UnresolvedCount)
	require.True(t, decimal.RequireFromString(view.Summary.UnresolvedUSD).Equal(decimal.RequireFromString("0.0006")))
	require.Error(t, r.MarkAPIFallbackSent(t.Context(), reserved.ID))
	require.Error(t, r.CancelAPIFallbackUnsent(t.Context(), sent.ID, "cannot erase unknown"))
	var events int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM api_fallback_events WHERE evidence='recovered_after_interruption'`).Scan(&events))
	require.Equal(t, 2, events)
}

func TestCodingQuotaDBAPIFallbackRestrictedKeyCannotBypassDatabaseGate(t *testing.T) {
	db, r := fallbackTestDB(t)
	p := fallbackTestPolicy(t, r, nil)
	_, err := db.Exec(`UPDATE api_keys SET quota=1 WHERE id=1`)
	require.NoError(t, err)
	_, err = r.ReserveAPIFallback(t.Context(), fallbackReserveInput(p))
	require.Error(t, err)
	_, err = db.Exec(`UPDATE api_keys SET quota=0 WHERE id=1`)
	require.NoError(t, err)
	a, err := r.ReserveAPIFallback(t.Context(), fallbackReserveInput(p))
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE api_keys SET rate_limit_5h=1 WHERE id=1`)
	require.NoError(t, err)
	require.Error(t, r.MarkAPIFallbackSent(t.Context(), a.ID))
	require.NoError(t, r.CancelAPIFallbackUnsent(t.Context(), a.ID, "restricted before send"))
	_, err = db.Exec(`UPDATE api_keys SET rate_limit_5h=0 WHERE id=1; UPDATE groups SET status='disabled' WHERE id=1`)
	require.NoError(t, err)
	_, err = r.ReserveAPIFallback(t.Context(), fallbackReserveInput(p))
	require.Error(t, err)
	_, err = db.Exec(`UPDATE groups SET status='active' WHERE id=1; UPDATE accounts SET status='disabled' WHERE id=30`)
	require.NoError(t, err)
	_, err = r.ReserveAPIFallback(t.Context(), fallbackReserveInput(p))
	require.Error(t, err)
	_, err = db.Exec(`UPDATE accounts SET status='active',extra='{"quota_limit":1}' WHERE id=30`)
	require.NoError(t, err)
	_, err = r.ReserveAPIFallback(t.Context(), fallbackReserveInput(p))
	require.Error(t, err)
}

func TestCodingQuotaDBAPIFallbackPrimaryIdentityUsesShadowParent(t *testing.T) {
	db, r := fallbackTestDB(t)
	_, err := db.Exec(`DELETE FROM account_groups WHERE account_id=10;
 INSERT INTO accounts(id,name,platform,type,parent_account_id) VALUES(11,'credential shadow','openai','oauth',10);
 INSERT INTO account_groups VALUES(11,1);`)
	require.NoError(t, err)
	p := fallbackTestPolicy(t, r, nil)
	require.Equal(t, []int64{10, 20}, p.PrimaryAccountIDs)
	_, err = r.ReserveAPIFallback(t.Context(), fallbackReserveInput(p))
	require.NoError(t, err)
	p.PrimaryAccountIDs = []int64{11, 20}
	_, err = r.SaveAPIFallbackPolicy(t.Context(), service.APIFallbackPolicyInput{APIFallbackPolicy: *p, ExpectedVersion: p.Version, ActorUserID: 1, Confirm: true})
	require.Error(t, err)
}

func TestCodingQuotaDBAPIFallbackCostBoundCircuitRequiresNewApproval(t *testing.T) {
	_, r := fallbackTestDB(t)
	p := fallbackTestPolicy(t, r, nil)
	first, err := r.ReserveAPIFallback(t.Context(), fallbackReserveInput(p))
	require.NoError(t, err)
	waiting, err := r.ReserveAPIFallback(t.Context(), fallbackReserveInput(p))
	require.NoError(t, err)
	late, err := r.ReserveAPIFallback(t.Context(), fallbackReserveInput(p))
	require.NoError(t, err)
	require.NoError(t, r.MarkAPIFallbackSent(t.Context(), late.ID))
	require.NoError(t, r.MarkAPIFallbackSent(t.Context(), first.ID))
	first, err = r.SettleAPIFallback(t.Context(), service.APIFallbackSettlementInput{ID: first.ID, InputTokens: 1000000, OutputTokens: 100, Usage: []byte(`{"supplier_receipt":"actual exceeds its declared bound"}`)})
	require.NoError(t, err)
	require.Equal(t, "cost_bound_exceeded", first.Reason)
	_, err = r.ReserveAPIFallback(t.Context(), fallbackReserveInput(p))
	require.ErrorContains(t, err, "exceeded its approved cost bound")
	require.ErrorContains(t, r.MarkAPIFallbackSent(t.Context(), waiting.ID), "exceeded its approved cost bound")
	require.NoError(t, r.CancelAPIFallbackUnsent(t.Context(), waiting.ID, "cost circuit opened before send"))
	view, err := r.ReadAPIFallback(t.Context(), 1)
	require.NoError(t, err)
	require.True(t, view.Policies[0].CostCircuitOpen)
	p.Note = "administrator reviewed supplier incident and approved recovery"
	p, err = r.SaveAPIFallbackPolicy(t.Context(), service.APIFallbackPolicyInput{APIFallbackPolicy: *p, ExpectedVersion: p.Version, ActorUserID: 1, Confirm: true})
	require.NoError(t, err)
	require.Equal(t, int64(2), p.Version)
	resumed, err := r.ReserveAPIFallback(t.Context(), fallbackReserveInput(p))
	require.NoError(t, err)
	view, err = r.ReadAPIFallback(t.Context(), 1)
	require.NoError(t, err)
	require.False(t, view.Policies[0].CostCircuitOpen)
	require.True(t, decimal.RequireFromString(view.Summary.SettledUSD).GreaterThan(decimal.NewFromInt(1)), "approving a new version cannot reset old spend")
	// An old in-flight receipt arriving after approval is a new incident. It
	// cannot hide behind an older policy version and escape the current circuit.
	_, err = r.SettleAPIFallback(t.Context(), service.APIFallbackSettlementInput{ID: late.ID, InputTokens: 1000000, OutputTokens: 100, Usage: []byte(`{"supplier_receipt":"late old-version overrun"}`)})
	require.NoError(t, err)
	_, err = r.ReserveAPIFallback(t.Context(), fallbackReserveInput(p))
	require.ErrorContains(t, err, "exceeded its approved cost bound")
	require.ErrorContains(t, r.MarkAPIFallbackSent(t.Context(), resumed.ID), "exceeded its approved cost bound")
	view, err = r.ReadAPIFallback(t.Context(), 1)
	require.NoError(t, err)
	require.True(t, view.Policies[0].CostCircuitOpen)
}
