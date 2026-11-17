package repository

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestCodingQuotaDBAPIFallbackSupportedCodingScopes(t *testing.T) {
	for _, tt := range []struct {
		name     string
		group    string
		platform string
	}{
		{"Codex", service.PlatformOpenAI, service.PlatformOpenAI},
		{"Grok Build", service.PlatformGrok, service.PlatformGrok},
		{"Unified Codex", service.PlatformComposite, service.PlatformOpenAI},
		{"Unified Grok", service.PlatformComposite, service.PlatformGrok},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db, r := fallbackTestDB(t)
			_, err := db.Exec(`ALTER TABLE users ADD COLUMN balance numeric NOT NULL DEFAULT 7`)
			require.NoError(t, err)
			_, err = db.Exec(`UPDATE groups SET platform=$1 WHERE id=1`, tt.group)
			require.NoError(t, err)
			_, err = db.Exec(`UPDATE accounts SET platform=$1`, tt.platform)
			require.NoError(t, err)
			readAt := time.Now().UTC()
			before := quotaUsed(t, r, 1, readAt, tt.platform)
			other := service.PurposeOpenAI
			if tt.platform == service.PlatformOpenAI {
				other = service.PurposeGrok
			}
			otherBefore := quotaUsed(t, r, 1, readAt, other)
			p := fallbackTestPolicy(t, r, func(p *service.APIFallbackPolicy) {
				p.Platform = tt.platform
				p.Enabled = false
			})
			require.False(t, p.Enabled, "saving the expanded scope must not enable payments")
			p.Enabled = true
			p, err = r.SaveAPIFallbackPolicy(t.Context(), service.APIFallbackPolicyInput{
				APIFallbackPolicy: *p, ExpectedVersion: p.Version, ActorUserID: 1, Confirm: true,
			})
			require.NoError(t, err)
			in := fallbackReserveInput(p)
			in.Platform = tt.platform
			ordinary := in
			ordinary.UserID, ordinary.APIKeyID = 2, 2
			_, err = r.ReserveAPIFallback(t.Context(), ordinary)
			require.ErrorContains(t, err, "administrator")
			a, err := r.ReserveAPIFallback(t.Context(), in)
			require.NoError(t, err)
			require.NoError(t, r.MarkAPIFallbackSent(t.Context(), a.ID))
			a, err = r.SettleAPIFallback(t.Context(), service.APIFallbackSettlementInput{
				ID: a.ID, InputTokens: 100, CachedInputTokens: 50, OutputTokens: 20,
				Usage: []byte(`{"input_tokens":100,"output_tokens":20}`), Now: time.Now().UTC(),
			})
			require.NoError(t, err)
			var purpose, source, balance string
			require.NoError(t, db.QueryRow(`SELECT purpose,metering_source FROM coding_quota_requests WHERE request_id=$1`, in.RequestID).Scan(&purpose, &source))
			require.Equal(t, tt.platform, purpose)
			require.Equal(t, "api_fallback", source)
			require.NoError(t, db.QueryRow(`SELECT balance::text FROM users WHERE id=1`).Scan(&balance))
			require.True(t, decimal.RequireFromString(balance).Equal(decimal.NewFromInt(7)), "coding points must not also charge the dollar wallet")
			after := quotaUsed(t, r, 1, time.Now().UTC(), tt.platform)
			require.True(t, decimal.RequireFromString(after.Used).Equal(decimal.RequireFromString(before.Used).Add(decimal.RequireFromString(a.Points))))
			require.True(t, after.Start.Equal(before.Start))
			require.True(t, after.End.Equal(before.End), "a backup request must preserve the original weekly clock")
			otherAfter := quotaUsed(t, r, 1, time.Now().UTC(), other)
			require.Equal(t, otherBefore.Used, otherAfter.Used)
			require.True(t, otherBefore.Start.Equal(otherAfter.Start))
			require.True(t, otherBefore.End.Equal(otherAfter.End))
		})
	}
}

func TestCodingQuotaDBAPIFallbackRejectsWrongCodingScope(t *testing.T) {
	db, r := fallbackTestDB(t)
	for _, tt := range []struct {
		name     string
		group    string
		kind     string
		platform string
		deleted  bool
	}{
		{"Codex cannot use Grok policy", "openai", "coding", "grok", false},
		{"Grok cannot use Codex policy", "grok", "coding", "openai", false},
		{"Codex experiment", "openai", "experiment", "openai", false},
		{"Grok experiment", "grok", "experiment", "grok", false},
		{"Composite experiment", "composite", "experiment", "openai", false},
		{"Missing explicit coding use", "openai", "", "openai", false},
		{"Unsupported provider", "anthropic", "coding", "openai", false},
		{"Deleted Codex group", "openai", "coding", "openai", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := db.Exec(`UPDATE groups SET platform=$1,usage_kind=$2,deleted_at=CASE WHEN $3::boolean THEN clock_timestamp() ELSE NULL END WHERE id=1`, tt.group, tt.kind, tt.deleted)
			require.NoError(t, err)
			_, err = r.SaveAPIFallbackPolicy(t.Context(), service.APIFallbackPolicyInput{
				APIFallbackPolicy: service.APIFallbackPolicy{GroupID: 1, Platform: tt.platform, Model: "exact-test", Note: "disabled scope validation"},
				ActorUserID:       1, Confirm: true,
			})
			require.ErrorContains(t, err, "coding group", "even disabled drafts must have a valid, matching coding scope")
		})
	}
}

func TestCodingQuotaDBAPIFallbackStandaloneScopeRecheckedBeforeSend(t *testing.T) {
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformGrok} {
		t.Run(platform, func(t *testing.T) {
			db, r := fallbackTestDB(t)
			_, err := db.Exec(`UPDATE groups SET platform=$1 WHERE id=1`, platform)
			require.NoError(t, err)
			_, err = db.Exec(`UPDATE accounts SET platform=$1`, platform)
			require.NoError(t, err)
			p := fallbackTestPolicy(t, r, func(p *service.APIFallbackPolicy) { p.Platform = platform })
			in := fallbackReserveInput(p)
			in.Platform = platform
			a, err := r.ReserveAPIFallback(t.Context(), in)
			require.NoError(t, err)
			other := service.PlatformGrok
			if platform == service.PlatformGrok {
				other = service.PlatformOpenAI
			}
			_, err = db.Exec(`UPDATE groups SET platform=$1 WHERE id=1`, other)
			require.NoError(t, err)
			require.ErrorContains(t, r.MarkAPIFallbackSent(t.Context(), a.ID), "coding group")
			next := fallbackReserveInput(p)
			next.Platform = platform
			_, err = r.ReserveAPIFallback(t.Context(), next)
			require.ErrorContains(t, err, "coding group")
			var state string
			require.NoError(t, db.QueryRow(`SELECT state FROM api_fallback_attempts WHERE id=$1`, a.ID).Scan(&state))
			require.Equal(t, "reserved", state, "a group identity change must prevent paid transmission")
		})
	}
}

func TestCodingQuotaDBAPIFallbackBackupRoleSurvivesPolicyChanges(t *testing.T) {
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformGrok} {
		t.Run(platform, func(t *testing.T) {
			db, r := fallbackTestDB(t)
			_, err := db.Exec(`UPDATE groups SET platform=$1 WHERE id=1`, platform)
			require.NoError(t, err)
			_, err = db.Exec(`UPDATE accounts SET platform=$1`, platform)
			require.NoError(t, err)
			accounts := newAccountRepositoryWithSQL(nil, db, nil)
			assertRoles := func(group int64, expected []int64) {
				t.Helper()
				ids, e := r.ListAPIFallbackBackupAccountIDs(t.Context(), group)
				require.NoError(t, e)
				require.Equal(t, expected, ids)
				accountIDs, e := accounts.ListAPIFallbackBackupAccountIDs(t.Context(), group)
				require.NoError(t, e)
				require.Equal(t, expected, accountIDs, "scheduler and administration must read the same durable roles")
			}
			assertRoles(1, []int64{})
			p := fallbackTestPolicy(t, r, func(p *service.APIFallbackPolicy) {
				p.Platform = platform
				p.Enabled = false
			})
			assertRoles(1, []int64{30})
			_, err = db.Exec(`INSERT INTO accounts(id,name,platform,type) VALUES(40,'rotated backup',$1,'apikey'),(50,'unrelated group backup',$1,'apikey')`, platform)
			require.NoError(t, err)
			_, err = db.Exec(`INSERT INTO groups(id,platform,status,usage_kind) VALUES(2,$1,'active','coding')`, platform)
			require.NoError(t, err)
			_, err = db.Exec(`INSERT INTO account_groups(account_id,group_id) VALUES(40,1),(50,2)`)
			require.NoError(t, err)
			past := time.Now().UTC().Add(-time.Hour)
			p.ExpiresAt = &past
			p.BackupAccountIDs = []int64{40}
			p, err = r.SaveAPIFallbackPolicy(t.Context(), service.APIFallbackPolicyInput{
				APIFallbackPolicy: *p, ExpectedVersion: p.Version, ActorUserID: 1, Confirm: true,
			})
			require.NoError(t, err)
			assertRoles(1, []int64{30, 40})
			p.BackupAccountIDs = nil
			_, err = r.SaveAPIFallbackPolicy(t.Context(), service.APIFallbackPolicyInput{
				APIFallbackPolicy: *p, ExpectedVersion: p.Version, ActorUserID: 1, Confirm: true,
			})
			require.NoError(t, err)
			_, err = r.SaveAPIFallbackPolicy(t.Context(), service.APIFallbackPolicyInput{
				APIFallbackPolicy: service.APIFallbackPolicy{GroupID: 1, Platform: platform, Model: "another-model", BackupAccountIDs: []int64{30}, Note: "role independent of exact model"},
				ActorUserID:       1, Confirm: true,
			})
			require.NoError(t, err)
			_, err = r.SaveAPIFallbackPolicy(t.Context(), service.APIFallbackPolicyInput{
				APIFallbackPolicy: service.APIFallbackPolicy{GroupID: 2, Platform: platform, Model: p.Model, BackupAccountIDs: []int64{50}, Note: "other group role"},
				ActorUserID:       1, Confirm: true,
			})
			require.NoError(t, err)
			assertRoles(1, []int64{30, 40})
			assertRoles(2, []int64{50})
			selected, err := r.FindAPIFallbackPolicy(t.Context(), 1, platform, p.Model)
			require.NoError(t, err)
			require.Nil(t, selected, "disabled or expired policies do not allow paid admission")
			_, err = db.Exec(`DELETE FROM account_groups WHERE group_id=1 AND account_id=30`)
			require.NoError(t, err)
			assertRoles(1, []int64{30, 40})
			_, err = db.Exec(`INSERT INTO account_groups(account_id,group_id) VALUES(30,1)`)
			require.NoError(t, err)
			assertRoles(1, []int64{30, 40})
			assertRoles(3, []int64{})
		})
	}
}

func TestCodingQuotaDBAPIFallbackDraftCannotAssignInvalidRoles(t *testing.T) {
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformGrok} {
		t.Run(platform, func(t *testing.T) {
			db, r := fallbackTestDB(t)
			_, err := db.Exec(`UPDATE groups SET platform=$1 WHERE id=1`, platform)
			require.NoError(t, err)
			_, err = db.Exec(`UPDATE accounts SET platform=$1`, platform)
			require.NoError(t, err)
			other := service.PlatformGrok
			if platform == service.PlatformGrok {
				other = service.PlatformOpenAI
			}
			_, err = db.Exec(`INSERT INTO accounts(id,name,platform,type,deleted_at,parent_account_id) VALUES
 (40,'wrong platform backup',$1,'apikey',NULL,NULL),
 (50,'unbound backup',$2,'apikey',NULL,NULL),
 (60,'deleted backup',$2,'apikey',clock_timestamp(),NULL),
 (70,'wrong platform primary',$1,'oauth',NULL,NULL),
 (80,'unbound primary',$2,'oauth',NULL,NULL),
 (90,'shadow not parent',$2,'oauth',NULL,10)`, other, platform)
			require.NoError(t, err)
			_, err = db.Exec(`INSERT INTO account_groups(account_id,group_id) VALUES(40,1),(60,1),(70,1),(90,1)`)
			require.NoError(t, err)
			for _, tt := range []struct {
				name    string
				primary []int64
				backup  []int64
			}{
				{"OAuth cannot be a backup", nil, []int64{10}},
				{"Other platform cannot be a backup", nil, []int64{40}},
				{"Unbound account cannot be a backup", nil, []int64{50}},
				{"Deleted account cannot be a backup", nil, []int64{60}},
				{"API account cannot be a primary", []int64{30}, nil},
				{"Other platform cannot be a primary", []int64{70}, nil},
				{"Unbound account cannot be a primary", []int64{80}, nil},
				{"Shadow identity cannot be a primary", []int64{90}, nil},
			} {
				t.Run(tt.name, func(t *testing.T) {
					_, e := r.SaveAPIFallbackPolicy(t.Context(), service.APIFallbackPolicyInput{
						APIFallbackPolicy: service.APIFallbackPolicy{GroupID: 1, Platform: platform, Model: "invalid-draft", PrimaryAccountIDs: tt.primary, BackupAccountIDs: tt.backup, Note: "invalid selection must not persist"},
						ActorUserID:       1, Confirm: true,
					})
					require.ErrorContains(t, e, "must belong")
					var policies, versions, events int
					require.NoError(t, db.QueryRow(`SELECT
 (SELECT COUNT(*) FROM api_fallback_policies),
 (SELECT COUNT(*) FROM api_fallback_policy_versions),
 (SELECT COUNT(*) FROM api_fallback_events)`).Scan(&policies, &versions, &events))
					require.Zero(t, policies)
					require.Zero(t, versions)
					require.Zero(t, events)
					roles, e := r.ListAPIFallbackBackupAccountIDs(t.Context(), 1)
					require.NoError(t, e)
					require.Empty(t, roles)
				})
			}
			// A valid incomplete draft still works; an invalid edit cannot append
			// history that would assign a durable backup role.
			p := fallbackTestPolicy(t, r, func(p *service.APIFallbackPolicy) {
				p.Platform = platform
				p.Enabled = false
				p.PrimaryAccountIDs, p.BackupAccountIDs = nil, nil
			})
			p.BackupAccountIDs = []int64{10}
			_, err = r.SaveAPIFallbackPolicy(t.Context(), service.APIFallbackPolicyInput{
				APIFallbackPolicy: *p, ExpectedVersion: p.Version, ActorUserID: 1, Confirm: true,
			})
			require.ErrorContains(t, err, "must belong")
			view, err := r.ReadAPIFallback(t.Context(), 1)
			require.NoError(t, err)
			require.Len(t, view.Policies, 1)
			require.Equal(t, int64(1), view.Policies[0].Version)
			require.Empty(t, view.Policies[0].BackupAccountIDs)
			var versions, events int
			require.NoError(t, db.QueryRow(`SELECT (SELECT COUNT(*) FROM api_fallback_policy_versions),(SELECT COUNT(*) FROM api_fallback_events)`).Scan(&versions, &events))
			require.Equal(t, 1, versions)
			require.Equal(t, 1, events)
			roles, err := r.ListAPIFallbackBackupAccountIDs(t.Context(), 1)
			require.NoError(t, err)
			require.Empty(t, roles)
		})
	}
}

func TestCodingQuotaDBAPIFallbackEmergencyDisableKeepsExistingRoles(t *testing.T) {
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformGrok} {
		t.Run(platform, func(t *testing.T) {
			db, r := fallbackTestDB(t)
			_, err := db.Exec(`UPDATE groups SET platform=$1 WHERE id=1`, platform)
			require.NoError(t, err)
			_, err = db.Exec(`UPDATE accounts SET platform=$1`, platform)
			require.NoError(t, err)
			p := fallbackTestPolicy(t, r, func(p *service.APIFallbackPolicy) { p.Platform = platform })
			_, err = db.Exec(`DELETE FROM account_groups WHERE account_id=30;
 UPDATE accounts SET deleted_at=clock_timestamp() WHERE id=30;
 UPDATE groups SET status='inactive',deleted_at=clock_timestamp() WHERE id=1`)
			require.NoError(t, err)
			_, err = r.DisableAPIFallbackPolicy(t.Context(), p.ID, p.Version, 2, "ordinary user cannot stop policy")
			require.ErrorContains(t, err, "administrator")
			stopped, err := r.DisableAPIFallbackPolicy(t.Context(), p.ID, p.Version, 1, "emergency stop broken configuration")
			require.NoError(t, err)
			require.False(t, stopped.Enabled)
			require.Equal(t, p.Version+1, stopped.Version)
			require.Equal(t, p.GroupID, stopped.GroupID)
			require.Equal(t, p.Platform, stopped.Platform)
			require.Equal(t, p.Model, stopped.Model)
			require.Equal(t, p.PrimaryAccountIDs, stopped.PrimaryAccountIDs)
			require.Equal(t, p.BackupAccountIDs, stopped.BackupAccountIDs)
			roles, err := r.ListAPIFallbackBackupAccountIDs(t.Context(), 1)
			require.NoError(t, err)
			require.Equal(t, []int64{30}, roles)
			_, err = r.DisableAPIFallbackPolicy(t.Context(), p.ID, p.Version, 1, "stale stop cannot overwrite new version")
			require.Error(t, err)
			var versions, events int
			require.NoError(t, db.QueryRow(`SELECT (SELECT COUNT(*) FROM api_fallback_policy_versions),(SELECT COUNT(*) FROM api_fallback_events)`).Scan(&versions, &events))
			require.Equal(t, 2, versions)
			require.Equal(t, 2, events)
		})
	}
}
