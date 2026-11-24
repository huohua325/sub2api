package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type fallbackGatewayAccounts struct {
	AccountRepository
	rows    []Account
	parents map[int64]*Account
}

func (r *fallbackGatewayAccounts) ListByGroup(context.Context, int64) ([]Account, error) {
	return r.rows, nil
}
func (r *fallbackGatewayAccounts) GetByID(_ context.Context, id int64) (*Account, error) {
	for i := range r.rows {
		if r.rows[i].ID == id {
			return &r.rows[i], nil
		}
	}
	if parent := r.parents[id]; parent != nil {
		return parent, nil
	}
	return nil, errors.New("account missing")
}

type fallbackGatewayControl struct {
	UsageMonitorStore
	APIFallbackStore
	p                                           *APIFallbackPolicy
	reserved, sent, settled, unknown, cancelled int
	lineage                                     int64
	receipt                                     APIFallbackSettlementInput
}

func (r *fallbackGatewayControl) FindAPIFallbackPolicy(context.Context, int64, string, string) (*APIFallbackPolicy, error) {
	return r.p, nil
}
func (r *fallbackGatewayControl) ResolveAPIFallbackLineage(context.Context, int64, int64, int64, string, []string) (int64, error) {
	return r.lineage, nil
}
func (r *fallbackGatewayControl) ReserveAPIFallback(_ context.Context, in APIFallbackReservationInput) (*APIFallbackAttempt, error) {
	r.reserved++
	return &APIFallbackAttempt{ID: in.ID}, nil
}
func (r *fallbackGatewayControl) MarkAPIFallbackSent(context.Context, string) error {
	r.sent++
	return nil
}
func (r *fallbackGatewayControl) SettleAPIFallback(_ context.Context, in APIFallbackSettlementInput) (*APIFallbackAttempt, error) {
	r.settled++
	r.receipt = in
	return &APIFallbackAttempt{ID: in.ID}, nil
}
func (r *fallbackGatewayControl) MarkAPIFallbackUnknown(context.Context, string, string) error {
	r.unknown++
	return nil
}
func (r *fallbackGatewayControl) CancelAPIFallbackUnsent(context.Context, string, string) error {
	r.cancelled++
	return nil
}

type fallbackGatewayHTTP struct {
	HTTPUpstream
	status            int
	body, contentType string
	calls             int
	fail              error
	request           *http.Request
}

func (u *fallbackGatewayHTTP) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.calls++
	u.request = req
	if u.fail != nil {
		return nil, u.fail
	}
	return &http.Response{StatusCode: u.status, Header: http.Header{"Content-Type": {u.contentType}, "X-Request-Id": {"supplier-test"}}, Body: io.NopCloser(strings.NewReader(u.body))}, nil
}

func fallbackGatewayFixture() (*OpenAIGatewayService, *fallbackGatewayControl, *APIKey, *APIFallbackPolicy) {
	now := time.Now().UTC()
	end := now.Add(time.Hour)
	p := &APIFallbackPolicy{ID: "policy", Version: 1, GroupID: 17, Platform: "openai", Model: "gpt-6.1-sol", Enabled: true, CapabilitiesVerified: true, ExpiresAt: &end, PrimaryAccountIDs: []int64{10}, BackupAccountIDs: []int64{20}, MaxInputTokens: 32768, MaxOutputTokens: 4096, PrimaryEvidence: []APIFallbackPrimaryEvidence{{AccountID: 10, Reason: "auth_invalid", ObservedAt: now.Add(-time.Minute), ValidUntil: end, Note: "verified"}}}
	accounts := &fallbackGatewayAccounts{rows: []Account{
		{ID: 10, Platform: "openai", Type: "oauth", Status: "active", Schedulable: true, GroupIDs: []int64{17}},
		{ID: 20, Platform: "openai", Type: "apikey", Status: "active", Schedulable: true, GroupIDs: []int64{17}, Credentials: map[string]any{"api_key": "supplier-secret-invalid", "base_url": "https://supplier.example.invalid"}},
	}}
	control := &fallbackGatewayControl{p: p}
	s := &OpenAIGatewayService{accountRepo: accounts, codingPoints: &OfficialPointService{}, concurrencyService: &ConcurrencyService{}, cfg: &config.Config{}}
	key := &APIKey{ID: 1, UserID: 2, User: &User{ID: 2, Role: RoleAdmin}, GroupID: &p.GroupID, Group: &Group{ID: 17, Platform: PlatformComposite, UsageKind: UsageKindCoding}}
	return s, control, key, p
}

func TestAPIFallbackPrimaryPoolStrictReasons(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*fallbackGatewayAccounts, *APIFallbackPolicy)
		want   bool
	}{
		{"confirmed", func(*fallbackGatewayAccounts, *APIFallbackPolicy) {}, true},
		{"no evidence", func(_ *fallbackGatewayAccounts, p *APIFallbackPolicy) { p.PrimaryEvidence = nil }, false},
		{"expired evidence", func(_ *fallbackGatewayAccounts, p *APIFallbackPolicy) {
			p.PrimaryEvidence[0].ValidUntil = time.Now().Add(-time.Second)
		}, false},
		{"busy is not failure", func(_ *fallbackGatewayAccounts, p *APIFallbackPolicy) { p.PrimaryEvidence[0].Reason = "busy" }, false},
		{"administrator paused", func(a *fallbackGatewayAccounts, _ *APIFallbackPolicy) { a.rows[0].Schedulable = false }, false},
		{"fresh official exhaustion", func(a *fallbackGatewayAccounts, p *APIFallbackPolicy) {
			p.PrimaryEvidence = nil
			a.rows[0].Extra = map[string]any{"codex_7d_used_percent": float64(100), "codex_7d_reset_at": time.Now().Add(time.Hour).Format(time.RFC3339), "codex_usage_updated_at": time.Now().Format(time.RFC3339)}
		}, true},
		{"stale official exhaustion", func(a *fallbackGatewayAccounts, p *APIFallbackPolicy) {
			p.PrimaryEvidence = nil
			a.rows[0].Extra = map[string]any{"codex_7d_used_percent": float64(100), "codex_7d_reset_at": time.Now().Add(time.Hour).Format(time.RFC3339), "codex_usage_updated_at": time.Now().Add(-time.Hour).Format(time.RFC3339)}
		}, false},
		{"new healthy member", func(a *fallbackGatewayAccounts, _ *APIFallbackPolicy) {
			a.rows = append(a.rows, Account{ID: 30, Platform: "openai", Type: "oauth", Status: "active", Schedulable: true})
		}, false},
		{"empty pool", func(a *fallbackGatewayAccounts, _ *APIFallbackPolicy) { a.rows = nil }, false},
		{"healthy official override", func(a *fallbackGatewayAccounts, _ *APIFallbackPolicy) {
			a.rows[0].Extra = map[string]any{"codex_7d_used_percent": float64(10), "codex_7d_reset_at": time.Now().Add(time.Hour).Format(time.RFC3339), "codex_usage_updated_at": time.Now().Format(time.RFC3339)}
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, _, p := fallbackGatewayFixture()
			accounts, ok := s.accountRepo.(*fallbackGatewayAccounts)
			require.True(t, ok)
			tc.mutate(accounts, p)
			v, e := s.EvaluateAPIFallbackPrimaryPool(t.Context(), p)
			require.NoError(t, e)
			require.Equal(t, tc.want, v.Eligible)
		})
	}
}

func TestAPIFallbackExperimentDispatch(t *testing.T) {
	for _, tc := range []struct {
		name         string
		expired      bool
		owner        int64
		body         string
		wantDispatch bool
		wantErr      bool
	}{
		{"expired", true, 2, `"input":"test","include":["reasoning.encrypted_content"]`, true, false},
		{"healthy", false, 2, `"input":"test"`, false, false},
		{"other owner", true, 3, `"input":"test"`, false, false},
		{"opaque state", true, 2, `"input":[{"type":"reasoning","encrypted_content":"opaque"}]`, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, store, key, p := fallbackGatewayFixture()
			p.ExperimentKeyID, p.ExperimentGroupID, p.ApprovedBy = 7, 8, tc.owner
			p.PrimaryAccountIDs, p.PrimaryEvidence, p.BackupAccountIDs, p.ExpiresAt = nil, nil, nil, nil
			if tc.expired {
				expiry := time.Now().Add(-time.Hour)
				accounts, ok := s.accountRepo.(*fallbackGatewayAccounts)
				require.True(t, ok)
				primary := &accounts.rows[0]
				primary.ExpiresAt, primary.AutoPauseOnExpired = &expiry, true
			}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			ctx := WithResolvedTargetPlatform(c.Request.Context(), PlatformOpenAI)
			raw := []byte(`{"model":"gpt-6.1-sol",` + tc.body + `}`)
			dispatched := false
			handled, err := s.TryAPIFallback(ctx, c, NewUsageMonitorService(store), key, p.Model, raw, time.Now(), func(policy *APIFallbackPolicy, prepared []byte) error {
				dispatched = true
				require.Equal(t, int64(7), policy.ExperimentKeyID)
				require.False(t, gjson.GetBytes(prepared, "include").Exists())
				require.False(t, gjson.GetBytes(prepared, "store").Bool())
				return nil
			})
			require.Equal(t, tc.wantDispatch, dispatched)
			require.Equal(t, tc.wantDispatch || tc.wantErr, handled)
			require.Equal(t, tc.wantErr, err != nil)
			require.Zero(t, store.reserved)
		})
	}
}

func TestAPIFallbackNativeAttemptAndRecovery(t *testing.T) {
	usage := `{"id":"resp_test","model":"gpt-6.1-sol","service_tier":"default","usage":{"input_tokens":100,"input_tokens_details":{"cached_tokens":20},"output_tokens":30},"output":[{"type":"reasoning","encrypted_content":"own-encrypted-state"}]}`
	for _, tc := range []struct {
		name, body, contentType string
		status                  int
		unknown, settled        int
	}{
		{"json", usage, "application/json", 200, 0, 1},
		{"stream", "data: {\"type\":\"response.completed\",\"response\":" + usage + "}\n\n", "text/event-stream", 200, 0, 1},
		{"missing usage", `{"id":"resp_test","model":"gpt-6.1-sol"}`, "application/json", 200, 1, 0},
		{"wrong model", strings.ReplaceAll(usage, "gpt-6.1-sol", "other-model"), "application/json", 200, 1, 0},
		{"supplier error", `{"error":"unknown"}`, "application/json", 502, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, store, key, _ := fallbackGatewayFixture()
			upstream := &fallbackGatewayHTTP{status: tc.status, body: tc.body, contentType: tc.contentType}
			s.httpUpstream = upstream
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			ctx := WithResolvedTargetPlatform(c.Request.Context(), PlatformOpenAI)
			handled, _ := s.TryAPIFallback(ctx, c, NewUsageMonitorService(store), key, "gpt-6.1-sol", []byte(`{"model":"gpt-6.1-sol","input":"test","stream":true}`), time.Now())
			require.True(t, handled)
			require.Equal(t, 1, upstream.calls)
			require.Equal(t, 1, store.sent)
			require.Equal(t, tc.unknown, store.unknown)
			require.Equal(t, tc.settled, store.settled)
			require.Equal(t, "Bearer supplier-secret-invalid", upstream.request.Header.Get("Authorization"))
			require.Empty(t, upstream.request.Header.Get("X-Api-Key"))
			if tc.settled > 0 {
				require.EqualValues(t, 100, store.receipt.InputTokens)
				require.Contains(t, string(store.receipt.Usage), "supplier-test")
				require.NotContains(t, string(store.receipt.Usage), "own-encrypted-state")
				var evidence map[string]any
				require.NoError(t, json.Unmarshal(store.receipt.Usage, &evidence))
				require.Len(t, evidence["encrypted_digests"], 1)
			}
		})
	}
}

func TestAPIFallbackNoUnauthorizedOrNormalSpend(t *testing.T) {
	for _, mode := range []string{"ordinary", "disabled", "healthy", "key_cost_cap", "other_platform", "recovered_encrypted"} {
		t.Run(mode, func(t *testing.T) {
			s, store, key, p := fallbackGatewayFixture()
			upstream := &fallbackGatewayHTTP{}
			s.httpUpstream = upstream
			raw := []byte(`{"model":"gpt-6.1-sol","input":"test"}`)
			platform := PlatformOpenAI
			switch mode {
			case "ordinary":
				key.User.Role = RoleUser
			case "disabled":
				p.Enabled = false
			case "healthy":
				p.PrimaryEvidence = nil
			case "key_cost_cap":
				key.Quota = 1
			case "other_platform":
				platform = PlatformGrok
				store.p = nil
			case "recovered_encrypted":
				p.PrimaryEvidence = nil
				store.lineage = 20
				raw = []byte(`{"model":"gpt-6.1-sol","input":[{"type":"reasoning","encrypted_content":"own"}]}`)
			}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			_, _ = s.TryAPIFallback(WithResolvedTargetPlatform(c.Request.Context(), platform), c, NewUsageMonitorService(store), key, "gpt-6.1-sol", raw, time.Now())
			require.Zero(t, upstream.calls)
			require.Zero(t, store.reserved)
		})
	}
}

func TestAPIFallbackReceiptRejectsUnpricedUsage(t *testing.T) {
	for _, raw := range []string{`{"model":"x","usage":{"input_tokens":1,"output_tokens":1}}`, `{"model":"gpt-6.1-sol","service_tier":"priority","usage":{"input_tokens":1,"output_tokens":1}}`, `{"model":"gpt-6.1-sol","usage":{"input_tokens":1,"output_tokens":1,"input_tokens_details":{"cached_tokens":2}}}`, `{"model":"gpt-6.1-sol","usage":{"input_tokens":1.5,"output_tokens":1}}`} {
		require.Nil(t, fallbackReceipt([]byte(raw), "gpt-6.1-sol"))
	}
	digests := apiFallbackEncryptedDigests(gjson.Parse(`{"input":[{"type":"reasoning","encrypted_content":"one"},{"type":"reasoning","encrypted_content":"one"},{"type":"reasoning","encrypted_content":"two"}],"tools":[{"type":"function","parameters":{"default":{"encrypted_content":"not a history item"}}}]}`))
	require.Len(t, digests, 2)
}

func TestAPIFallbackNativeBothPlatformsAndPinnedContinuation(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformGrok} {
		t.Run(platform, func(t *testing.T) {
			s, store, key, p := fallbackGatewayFixture()
			model := "gpt-6.1-sol"
			if platform == PlatformGrok {
				model = "grok-4.7"
			}
			p.Platform, p.Model = platform, model
			accounts, ok := s.accountRepo.(*fallbackGatewayAccounts)
			require.True(t, ok)
			for i := range accounts.rows {
				accounts.rows[i].Platform = platform
			}
			accounts.rows = append(accounts.rows, Account{ID: 30, Platform: platform, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, GroupIDs: []int64{17}, Credentials: map[string]any{"api_key": "other-invalid", "base_url": "https://other.example.invalid"}})
			p.BackupAccountIDs = []int64{30, 20}
			store.lineage = 20
			upstream := &fallbackGatewayHTTP{status: 200, contentType: "application/json", body: `{"id":"resp_test","model":"` + model + `","usage":{"input_tokens":100,"output_tokens":30}}`}
			s.httpUpstream = upstream
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			body := []byte(`{"model":"` + model + `","input":[{"type":"reasoning","id":"rs_owned","summary":[],"encrypted_content":"owned-state"},{"role":"user","content":"continue"}]}`)
			handled, err := s.TryAPIFallback(WithResolvedTargetPlatform(c.Request.Context(), platform), c, NewUsageMonitorService(store), key, model, body, time.Now())
			require.NoError(t, err)
			require.True(t, handled)
			require.Equal(t, 1, store.settled)
			require.Equal(t, "supplier.example.invalid", upstream.request.URL.Host, "encrypted continuation must stay on its original supplier")
			require.Equal(t, "/v1/responses", upstream.request.URL.Path)
		})
	}
}

func TestAPIFallbackPausedShadowCannotAuthorizePaidParent(t *testing.T) {
	for _, state := range []string{"active", "disabled", "unschedulable"} {
		t.Run(state, func(t *testing.T) {
			s, _, _, p := fallbackGatewayFixture()
			r, ok := s.accountRepo.(*fallbackGatewayAccounts)
			require.True(t, ok)
			parent := r.rows[0]
			shadow := Account{ID: 11, ParentAccountID: &parent.ID, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true}
			if state == "disabled" {
				shadow.Status = StatusDisabled
			}
			if state == "unschedulable" {
				shadow.Schedulable = false
			}
			r.rows = []Account{shadow}
			r.parents = map[int64]*Account{parent.ID: &parent}
			preview, err := s.EvaluateAPIFallbackPrimaryPool(t.Context(), p)
			require.NoError(t, err)
			require.Equal(t, state == "active", preview.Eligible)
		})
	}
}
