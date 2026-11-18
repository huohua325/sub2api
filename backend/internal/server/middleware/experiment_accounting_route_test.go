package middleware

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type experimentRouteStore struct {
	service.OfficialPointStore
	reserveErr error
	reserved   atomic.Int32
	sent       atomic.Int32
	receipts   atomic.Int32
	keyID      atomic.Int64
}

func (s *experimentRouteStore) ReserveExperimentRequest(_ context.Context, in service.ExperimentAdmissionInput) error {
	s.reserved.Add(1)
	s.keyID.Store(in.APIKeyID)
	return s.reserveErr
}

type experimentStandbyTestKeys struct {
	fakeAPIKeyRepo
	key *service.APIKey
}

func (r experimentStandbyTestKeys) GetByID(_ context.Context, id int64) (*service.APIKey, error) {
	if id != r.key.ID {
		return nil, service.ErrAPIKeyNotFound
	}
	return r.key, nil
}

type experimentStandbyTestGroups struct {
	service.GroupRepository
	group *service.Group
}

func (r experimentStandbyTestGroups) GetByID(_ context.Context, id int64) (*service.Group, error) {
	if id != r.group.ID {
		return nil, service.ErrGroupNotFound
	}
	return r.group, nil
}

func TestExperimentAccountStandbyUsesSourceIdentityWithoutChangingKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, role := range []string{service.RoleAdmin, service.RoleUser} {
		t.Run(role, func(t *testing.T) {
			cfg := &config.Config{RunMode: config.RunModeStandard}
			store := &experimentRouteStore{}
			source := &service.Group{ID: 8, Platform: service.PlatformOpenAI, UsageKind: service.UsageKindCoding, Status: service.StatusActive, Hydrated: true}
			target := &service.Group{ID: 9, Platform: service.PlatformOpenAI, UsageKind: service.UsageKindExperiment, Status: service.StatusActive, Hydrated: true}
			key := &service.APIKey{ID: 40, UserID: 3, User: &service.User{ID: 3, Status: service.StatusActive, Role: role, Balance: 10},
				GroupID: &source.ID, Group: source, Key: "test-only", Status: service.StatusActive}
			keys := experimentStandbyTestKeys{key: key, fakeAPIKeyRepo: fakeAPIKeyRepo{
				getByKey: func(context.Context, string) (*service.APIKey, error) { return key, nil },
			}}
			svc := service.NewAPIKeyService(keys, nil, experimentStandbyTestGroups{group: target}, nil, nil, nil, cfg)
			svc.SetOfficialPoints(service.NewOfficialPointService(nil, store, nil))
			router := gin.New()
			router.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(svc, nil, cfg)), GroupModelAllowlist())
			router.POST("/v1/responses", func(c *gin.Context) {
				got, _ := GetAPIKeyFromContext(c)
				require.NotSame(t, key, got)
				require.Equal(t, target.ID, *got.GroupID)
				require.Equal(t, service.PurposeExperiment, service.PurposeForRequest(c.Request.Context(), got.Group))
				out, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, "https://globalai.vip/v1/responses", nil)
				require.NoError(t, err)
				require.NoError(t, service.BeginExperimentHTTPAttempt(out, 20))
				c.Status(http.StatusOK)
			})
			request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-test","input":"hello"}`))
			request.Header.Set("Authorization", "Bearer "+key.Key)
			standby := &service.ExperimentStandby{UserID: 3, KeyID: 40, SourceKeyID: 40, SourceGroupID: 8, GroupID: 9,
				AccountID: 20, Platform: service.PlatformOpenAI, Model: "gpt-test", BeforeSend: func(context.Context) error { return nil }}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request.WithContext(service.WithExperimentStandby(request.Context(), standby)))
			require.Equal(t, source.ID, *key.GroupID)
			if role == service.RoleAdmin {
				require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
				require.Equal(t, key.ID, store.keyID.Load())
				require.Equal(t, int32(1), store.sent.Load())
			} else {
				require.Equal(t, http.StatusForbidden, recorder.Code, recorder.Body.String())
				require.Zero(t, store.reserved.Load())
			}
		})
	}
}

func TestExperimentStandbyRoutePreservesKeyAuthorizationAndBillingIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name   string
		edit   func(*service.APIKey, *service.ExperimentStandby)
		status int
	}{
		{"admitted", func(*service.APIKey, *service.ExperimentStandby) {}, http.StatusOK},
		{"rebound", func(_ *service.APIKey, s *service.ExperimentStandby) { s.GroupID++ }, http.StatusForbidden},
		{"other administrator", func(_ *service.APIKey, s *service.ExperimentStandby) { s.UserID++ }, http.StatusForbidden},
		{"restricted client IP", func(k *service.APIKey, _ *service.ExperimentStandby) { k.IPWhitelist = []string{"203.0.113.42"} }, http.StatusForbidden},
		{"disabled", func(k *service.APIKey, _ *service.ExperimentStandby) { k.Status = service.StatusAPIKeyDisabled }, http.StatusForbidden},
		{"pool changed before send", func(_ *service.APIKey, s *service.ExperimentStandby) {
			s.BeforeSend = func(context.Context) error { return service.ErrExperimentAccountingUnavailable }
		}, http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{RunMode: config.RunModeStandard}
			store := &experimentRouteStore{}
			group := &service.Group{ID: 4, Platform: service.PlatformOpenAI, UsageKind: service.UsageKindExperiment, Status: service.StatusActive, Hydrated: true, SubscriptionType: service.SubscriptionTypeStandard}
			user := &service.User{ID: 3, Status: service.StatusActive, Role: service.RoleAdmin, Balance: 10}
			key := &service.APIKey{ID: 40, UserID: 3, User: user, GroupID: &group.ID, Group: group, Key: "synthetic-test-only", Status: service.StatusActive}
			standby := &service.ExperimentStandby{UserID: 3, KeyID: 40, GroupID: group.ID, Platform: service.PlatformOpenAI, BeforeSend: func(context.Context) error { return nil }}
			tc.edit(key, standby)
			repo := experimentStandbyTestKeys{key: key, fakeAPIKeyRepo: fakeAPIKeyRepo{getByKey: func(context.Context, string) (*service.APIKey, error) { return key, nil }}}
			svc := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, cfg)
			svc.SetOfficialPoints(service.NewOfficialPointService(nil, store, nil))
			router := gin.New()
			router.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(svc, nil, cfg)), GroupModelAllowlist())
			router.POST("/v1/responses", func(c *gin.Context) {
				ctx := c.Request.Context()
				got, _ := GetAPIKeyFromContext(c)
				require.Same(t, key, got)
				require.Equal(t, group, ctx.Value(ctxkey.Group))
				require.Equal(t, service.PurposeExperiment, service.PurposeForRequest(ctx, got.Group))
				req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://globalai.vip/v1/responses", nil)
				require.NoError(t, err)
				if service.BeginExperimentHTTPAttempt(req, 20) == nil {
					c.Status(http.StatusOK)
				}
			})
			parent := service.WithResolvedTargetPlatform(context.Background(), service.PlatformGrok)
			r := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-test","input":"hello"}`))
			r.Header.Set("Authorization", "Bearer "+key.Key)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r.WithContext(service.WithExperimentStandby(parent, standby)))
			require.Equal(t, tc.status, w.Code, w.Body.String())
			if tc.status == http.StatusOK {
				require.Equal(t, int32(1), store.sent.Load())
				require.Equal(t, key.ID, store.keyID.Load())
			} else {
				require.Zero(t, store.reserved.Load())
			}
		})
	}
}
func (s *experimentRouteStore) MarkExperimentRequestSent(context.Context, string, int64) error {
	s.sent.Add(1)
	return nil
}
func (s *experimentRouteStore) ReleaseExperimentRequest(context.Context, string, int64, string) error {
	return nil
}
func (s *experimentRouteStore) RecordExperimentResponse(context.Context, string, int64, string, int) error {
	s.receipts.Add(1)
	return nil
}

// Exercise the real auth middleware, detached request context and HTTP upstream
// boundary. A local proxy is the only possible network destination.
func TestExperimentAccountingNativeAndCompatibleRoutesReachRealSendGate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, native := range []bool{false, true} {
		for _, scenario := range []struct {
			name    string
			blocked bool
			simple  bool
		}{{name: "allowed"}, {name: "blocked", blocked: true}, {name: "simple-mode", blocked: true, simple: true}} {
			blocked := scenario.blocked && !scenario.simple
			name := "compatible"
			if native {
				name = "native-gemini"
			}
			name += "-" + scenario.name
			t.Run(name, func(t *testing.T) {
				cfg := &config.Config{RunMode: config.RunModeStandard}
				if scenario.simple {
					cfg.RunMode = config.RunModeSimple
				}
				cfg.Security.URLAllowlist.AllowPrivateHosts = true
				cfg.Security.URLAllowlist.AllowInsecureHTTP = true
				var networkCalls atomic.Int32
				proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					networkCalls.Add(1)
					w.Header().Set("X-Oneapi-Request-Id", "test-supplier-receipt")
					_, _ = io.WriteString(w, `{}`)
				}))
				defer proxy.Close()
				store := &experimentRouteStore{}
				if scenario.blocked {
					store.reserveErr = service.ErrExperimentRequestInFlight
				}
				group := &service.Group{ID: 4, Platform: service.PlatformGemini, UsageKind: service.UsageKindExperiment, Status: service.StatusActive, Hydrated: true, SubscriptionType: service.SubscriptionTypeStandard}
				user := &service.User{ID: 3, Status: service.StatusActive, Role: service.RoleUser, Balance: 10, Concurrency: 10}
				key := &service.APIKey{ID: 40, UserID: 3, User: user, GroupID: &group.ID, Group: group, Key: "test-auth-key", Status: service.StatusActive}
				repo := fakeAPIKeyRepo{getByKey: func(context.Context, string) (*service.APIKey, error) { return key, nil }}
				svc := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, cfg)
				svc.SetOfficialPoints(service.NewOfficialPointService(nil, store, nil))
				router := gin.New()
				path, outgoingPath := "/v1/chat/completions", "/v1/chat/completions"
				if native {
					path, outgoingPath = "/v1beta/models/gemini-test:generateContent", "/v1beta/models/gemini-test:generateContent"
					router.Use(APIKeyAuthWithSubscriptionGoogle(svc, nil, cfg))
				} else {
					router.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(svc, nil, cfg)))
				}
				upstream := repository.NewHTTPUpstream(cfg)
				router.POST(path, func(c *gin.Context) {
					// Streaming detachment must retain the same admission handle.
					out, err := http.NewRequestWithContext(context.WithoutCancel(c.Request.Context()), http.MethodPost, "http://globalai.vip"+outgoingPath, strings.NewReader(`{}`))
					require.NoError(t, err)
					resp, err := upstream.Do(out, proxy.URL, 13, 100)
					if blocked {
						require.ErrorIs(t, err, service.ErrExperimentRequestInFlight)
						return
					}
					require.NoError(t, err)
					defer func() { _ = resp.Body.Close() }()
					c.Status(resp.StatusCode)
				})
				request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
				request.Header.Set("Authorization", "Bearer test-auth-key")
				request.Header.Set("x-goog-api-key", "test-auth-key")
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				if scenario.simple {
					require.Equal(t, http.StatusOK, response.Code)
					require.Zero(t, store.reserved.Load(), "simple mode retains its existing non-wallet billing semantics")
					require.Zero(t, store.sent.Load())
					require.Zero(t, store.receipts.Load())
					require.Equal(t, int32(1), networkCalls.Load())
					return
				}
				require.Equal(t, int32(1), store.reserved.Load())
				if blocked {
					require.Equal(t, http.StatusTooManyRequests, response.Code)
					require.Contains(t, response.Body.String(), "EXPERIMENT_REQUEST_IN_FLIGHT")
					require.Zero(t, store.sent.Load())
					require.Zero(t, networkCalls.Load())
				} else {
					require.Equal(t, http.StatusOK, response.Code)
					require.Equal(t, int32(1), store.sent.Load())
					require.Equal(t, int32(1), networkCalls.Load())
					require.Equal(t, int32(1), store.receipts.Load())
				}
			})
		}
	}
}

var _ service.ExperimentAdmissionStore = (*experimentRouteStore)(nil)
