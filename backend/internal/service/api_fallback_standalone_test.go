package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/gin-gonic/gin"
	gocache "github.com/patrickmn/go-cache"
	"github.com/stretchr/testify/require"
)

func TestAPIFallbackStandaloneScope(t *testing.T) {
	for _, tc := range []struct {
		name, groupPlatform, kind, resolved, want string
		allowed                                   bool
	}{
		{"codex existing client", PlatformOpenAI, UsageKindCoding, "", PlatformOpenAI, true},
		{"grok build existing client", PlatformGrok, UsageKindCoding, "", PlatformGrok, true},
		{"codex matching resolved target", PlatformOpenAI, UsageKindCoding, PlatformOpenAI, PlatformOpenAI, true},
		{"grok matching resolved target", PlatformGrok, UsageKindCoding, PlatformGrok, PlatformGrok, true},
		{"codex cannot charge grok", PlatformOpenAI, UsageKindCoding, PlatformGrok, "", false},
		{"grok cannot charge codex", PlatformGrok, UsageKindCoding, PlatformOpenAI, "", false},
		{"unified codex", PlatformComposite, UsageKindCoding, PlatformOpenAI, PlatformOpenAI, true},
		{"unified grok", PlatformComposite, UsageKindCoding, PlatformGrok, PlatformGrok, true},
		{"unified needs explicit target", PlatformComposite, UsageKindCoding, "", "", false},
		{"experiment", PlatformOpenAI, UsageKindExperiment, PlatformOpenAI, "", false},
		{"legacy unspecified use", PlatformGrok, "", PlatformGrok, "", false},
		{"other provider", PlatformAnthropic, UsageKindCoding, PlatformOpenAI, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			platform, ok := APIFallbackPlatformForGroup(&Group{Platform: tc.groupPlatform, UsageKind: tc.kind}, tc.resolved)
			require.Equal(t, tc.allowed, ok)
			require.Equal(t, tc.want, platform)
		})
	}
	_, ok := APIFallbackPlatformForGroup(nil, PlatformOpenAI)
	require.False(t, ok)
}

func TestAPIFallbackStandaloneResponsesUseExistingClientPaths(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformGrok} {
		for _, path := range []string{"/v1/responses", "/responses", "/backend-api/codex/responses"} {
			t.Run(platform+path, func(t *testing.T) {
				s, store, key, policy := fallbackGatewayFixture()
				model := "gpt-6.1-sol"
				if platform == PlatformGrok {
					model = "grok-4.7"
				}
				key.Group.Platform, policy.Platform, policy.Model = platform, platform, model
				for i := range s.accountRepo.(*fallbackGatewayAccounts).rows {
					s.accountRepo.(*fallbackGatewayAccounts).rows[i].Platform = platform
				}
				upstream := &fallbackGatewayHTTP{status: http.StatusOK, contentType: "application/json", body: `{"id":"paid","model":"` + model + `","usage":{"input_tokens":10,"output_tokens":2}}`}
				s.httpUpstream = upstream
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, path, nil)
				raw := []byte(`{"model":"` + model + `","input":"existing client"}`)
				handled, err := s.TryAPIFallback(c.Request.Context(), c, NewUsageMonitorService(store), key, model, raw, time.Now())
				require.NoError(t, err)
				require.True(t, handled)
				require.Equal(t, 1, store.sent)
				require.Equal(t, 1, store.settled)
				require.Equal(t, 1, upstream.calls)
				require.Equal(t, "/v1/responses", upstream.request.URL.Path)
			})
		}
	}
}

type fallbackRoleAccounts struct {
	*fallbackGatewayAccounts
	roles map[int64][]int64
	err   error
}

func (r *fallbackRoleAccounts) ListAPIFallbackBackupAccountIDs(_ context.Context, group int64) ([]int64, error) {
	return r.roles[group], r.err
}

func (r *fallbackRoleAccounts) ListSchedulableByGroupID(context.Context, int64) ([]Account, error) {
	return r.rows, nil
}

func (r *fallbackRoleAccounts) ListModelAvailabilityCandidates(context.Context, *int64, []string, bool) ([]Account, error) {
	return r.rows, nil
}

func TestAPIFallbackStandaloneNormalForwardCannotUseDeclaredBackup(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformGrok} {
		for _, method := range []string{"responses", "compact", "chat", "messages", "http_send", "websocket", "websocket_mode_off"} {
			t.Run(platform+"/"+method, func(t *testing.T) {
				s, _, key, _ := fallbackGatewayFixture()
				key.Group.Platform = platform
				repo := &fallbackRoleAccounts{fallbackGatewayAccounts: s.accountRepo.(*fallbackGatewayAccounts), roles: map[int64][]int64{key.Group.ID: {20}}}
				s.accountRepo = repo
				account := repo.rows[1]
				account.Platform = platform
				upstream := &fallbackGatewayHTTP{status: http.StatusOK, body: `{}`, contentType: "application/json"}
				s.httpUpstream = upstream
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				c.Set("api_key", key)
				raw := []byte(`{"model":"gpt-6.1-sol","input":"test"}`)
				var err error
				switch method {
				case "responses", "compact":
					if method == "compact" {
						c.Request.URL.Path = "/v1/responses/compact"
					}
					_, err = s.Forward(c.Request.Context(), c, &account, raw)
				case "chat":
					_, err = s.ForwardAsChatCompletions(c.Request.Context(), c, &account, raw, "", "")
				case "messages":
					_, err = s.ForwardAsAnthropic(c.Request.Context(), c, &account, raw, "", "")
				case "http_send":
					ctx := context.WithValue(c.Request.Context(), ctxkey.Group, key.Group)
					req := httptest.NewRequest(http.MethodPost, "https://supplier.invalid/v1/responses", nil).WithContext(ctx)
					_, err = s.doOpenAIUpstream(req, "", &account)
				case "websocket", "websocket_mode_off":
					if method == "websocket_mode_off" {
						s.codingPoints = nil
					}
					_, cleanup, hookErr := s.codingWSTurnHooks(c.Request.Context(), key, &account, raw, nil)
					defer cleanup()
					err = hookErr
				}
				require.ErrorIs(t, err, ErrCodingQuotaUnavailable)
				require.Zero(t, upstream.calls, "a stale normal selection cannot bypass paid admission")
			})
		}
	}
}

func TestAPIFallbackStandaloneRoleQueryFailsClosedAndKeepsOtherGroups(t *testing.T) {
	s, _, key, _ := fallbackGatewayFixture()
	repo := &fallbackRoleAccounts{fallbackGatewayAccounts: s.accountRepo.(*fallbackGatewayAccounts), roles: map[int64][]int64{key.Group.ID: {20}}}
	s.accountRepo = repo
	account := repo.rows[1]
	require.ErrorIs(t, guardAPIFallbackNormalAccount(t.Context(), repo, key.Group, &account), ErrCodingQuotaUnavailable)
	require.NoError(t, guardAPIFallbackNormalAccount(t.Context(), repo, &Group{ID: 99, Platform: PlatformOpenAI, UsageKind: UsageKindCoding}, &account))
	existing := map[int64]struct{}{10: {}}
	merged, err := s.apiFallbackSchedulingExclusions(t.Context(), key.GroupID, existing)
	require.NoError(t, err)
	require.Contains(t, merged, int64(10))
	require.Contains(t, merged, int64(20))
	require.NotContains(t, existing, int64(20), "do not mutate caller's failover state")
	repo.err = errors.New("role database unavailable")
	_, _, err = s.SelectAccountWithScheduler(t.Context(), key.GroupID, "stale-response", "stale-session", "gpt-6.1-sol", nil, OpenAIUpstreamTransportHTTPSSE, false)
	require.ErrorIs(t, err, ErrCodingQuotaUnavailable)
	_, err = s.SelectAccountByPreviousResponseID(t.Context(), key.GroupID, "stale-response", "gpt-6.1-sol", nil, false)
	require.ErrorIs(t, err, ErrCodingQuotaUnavailable)
	require.ErrorIs(t, guardAPIFallbackNormalAccount(t.Context(), repo, key.Group, &account), ErrCodingQuotaUnavailable)
}

func TestAPIFallbackStandaloneRoleChangesInvalidateCatalogCache(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformGrok} {
		t.Run(platform, func(t *testing.T) {
			group := &Group{ID: 17, Platform: platform, UsageKind: UsageKindCoding}
			primary, backup := fallbackCatalogAccounts()
			rows := append(append([]Account(nil), primary...), backup...)
			repo := &fallbackRoleAccounts{fallbackGatewayAccounts: &fallbackGatewayAccounts{rows: rows}, roles: map[int64][]int64{}}
			gateway := &GatewayService{accountRepo: repo, groupRepo: fallbackCatalogGroupRepo{group: group}, modelsListCache: gocache.New(time.Minute, time.Minute), modelsListCacheTTL: time.Minute}
			before := gateway.GetAvailableModels(t.Context(), &group.ID, platform)
			apiName, backupID := "api-only-model", int64(3)
			if platform == PlatformGrok {
				apiName, backupID = "api-only-grok", 4
			}
			require.Contains(t, before, apiName, "no-policy API members retain their old catalog semantics")
			repo.roles[group.ID] = []int64{backupID}
			after := gateway.GetAvailableModels(t.Context(), &group.ID, platform)
			require.NotContains(t, after, apiName)
			visible, catalog, err := loadCodexGroupCatalogAccounts(t.Context(), repo, group.ID)
			require.NoError(t, err)
			for _, set := range [][]Account{visible, catalog} {
				for _, account := range set {
					require.NotEqual(t, backupID, account.ID)
				}
			}
			repo.err = errors.New("database unavailable")
			require.Empty(t, gateway.GetAvailableModels(t.Context(), &group.ID, platform), "stale catalog cannot hide a role lookup failure")
		})
	}
}

func TestAPIFallbackStandaloneNormalUnconfiguredAPIStillForwards(t *testing.T) {
	s, _, key, _ := fallbackGatewayFixture()
	key.Group.Platform = PlatformOpenAI
	repo := &fallbackRoleAccounts{fallbackGatewayAccounts: s.accountRepo.(*fallbackGatewayAccounts), roles: map[int64][]int64{}}
	s.accountRepo = repo
	upstream := &fallbackGatewayHTTP{status: http.StatusOK, body: `{"ok":true}`, contentType: "application/json"}
	s.httpUpstream = upstream
	req := httptest.NewRequest(http.MethodPost, "https://supplier.invalid/v1/responses", strings.NewReader(`{}`)).WithContext(context.WithValue(t.Context(), ctxkey.Group, key.Group))
	resp, err := s.doOpenAIUpstream(req, "", &repo.rows[1])
	require.NoError(t, err)
	require.Equal(t, 1, upstream.calls)
	_, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
}
