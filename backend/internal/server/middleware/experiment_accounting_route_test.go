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
}

func (s *experimentRouteStore) ReserveExperimentRequest(context.Context, service.ExperimentAdmissionInput) error {
	s.reserved.Add(1)
	return s.reserveErr
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
