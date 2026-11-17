//go:build unit

package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type standaloneFallbackHandlerAccounts struct {
	service.AccountRepository
	rows []service.Account
}

func (r *standaloneFallbackHandlerAccounts) ListByGroup(context.Context, int64) ([]service.Account, error) {
	return r.rows, nil
}

func (r *standaloneFallbackHandlerAccounts) GetByID(_ context.Context, id int64) (*service.Account, error) {
	for i := range r.rows {
		if r.rows[i].ID == id {
			return &r.rows[i], nil
		}
	}
	return nil, nil
}

func (*standaloneFallbackHandlerAccounts) ListSchedulableByGroupIDAndPlatform(context.Context, int64, string) ([]service.Account, error) {
	// These cases verify that declining takeover returns to normal scheduling.
	// No normal account is offered, so an accidental paid send is observable.
	return nil, nil
}

func (*standaloneFallbackHandlerAccounts) ListAPIFallbackBackupAccountIDs(context.Context, int64) ([]int64, error) {
	return []int64{20}, nil
}

type standaloneFallbackHandlerStore struct {
	service.UsageMonitorStore
	service.APIFallbackStore
	policy                          *service.APIFallbackPolicy
	reserved, sent, settled         int
	reservation                     service.APIFallbackReservationInput
	observedGroup                   int64
	observedPlatform, observedModel string
}

func (s *standaloneFallbackHandlerStore) FindAPIFallbackPolicy(_ context.Context, group int64, platform, model string) (*service.APIFallbackPolicy, error) {
	s.observedGroup, s.observedPlatform, s.observedModel = group, platform, model
	if s.policy.GroupID == group && s.policy.Platform == platform && s.policy.Model == model {
		return s.policy, nil
	}
	return nil, nil
}

func (s *standaloneFallbackHandlerStore) ReserveAPIFallback(_ context.Context, input service.APIFallbackReservationInput) (*service.APIFallbackAttempt, error) {
	s.reserved++
	s.reservation = input
	return &service.APIFallbackAttempt{ID: input.ID}, nil
}

func (s *standaloneFallbackHandlerStore) MarkAPIFallbackSent(context.Context, string) error {
	s.sent++
	return nil
}

func (s *standaloneFallbackHandlerStore) SettleAPIFallback(_ context.Context, input service.APIFallbackSettlementInput) (*service.APIFallbackAttempt, error) {
	s.settled++
	return &service.APIFallbackAttempt{ID: input.ID}, nil
}

type standaloneFallbackHandlerHTTP struct {
	service.HTTPUpstream
	model string
	calls int
}

func (s *standaloneFallbackHandlerHTTP) Do(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	s.calls++
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"standalone-paid","model":"` + s.model + `","usage":{"input_tokens":10,"output_tokens":2}}`))}, nil
}

func TestAPIFallbackStandaloneHandlerExistingResponsesEntrances(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformGrok} {
		for _, mode := range []string{"admin", "ordinary", "disabled", "subscription_healthy"} {
			t.Run(platform+"/"+mode, func(t *testing.T) {
				model := "gpt-6.1-sol"
				if platform == service.PlatformGrok {
					model = "grok-4.7"
				}
				end := time.Now().Add(time.Hour)
				policy := &service.APIFallbackPolicy{ID: "standalone", GroupID: 17, Platform: platform, Model: model, Version: 1,
					Enabled: mode != "disabled", CapabilitiesVerified: true, ExpiresAt: &end,
					PrimaryAccountIDs: []int64{10}, BackupAccountIDs: []int64{20}, MaxInputTokens: 32768, MaxOutputTokens: 1024,
					PrimaryEvidence: []service.APIFallbackPrimaryEvidence{{AccountID: 10, Reason: "auth_invalid", ObservedAt: time.Now().Add(-time.Minute), ValidUntil: end, Note: "synthetic evidence"}}}
				if mode == "subscription_healthy" {
					policy.PrimaryEvidence = nil
				}
				accounts := &standaloneFallbackHandlerAccounts{rows: []service.Account{
					{ID: 10, Platform: platform, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, GroupIDs: []int64{17}},
					{ID: 20, Platform: platform, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, GroupIDs: []int64{17}, Credentials: map[string]any{"api_key": "synthetic-never-valid", "base_url": "https://supplier.example.invalid"}},
				}}
				store := &standaloneFallbackHandlerStore{policy: policy}
				upstream := &standaloneFallbackHandlerHTTP{model: model}
				cfg := &config.Config{RunMode: config.RunModeSimple}
				billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
				t.Cleanup(billing.Stop)
				cache := &concurrencyCacheMock{
					acquireUserSlotFn:    func(context.Context, int64, int, string) (bool, error) { return true, nil },
					acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil },
				}
				concurrency := service.NewConcurrencyService(cache)
				gateway := service.NewOpenAIGatewayService(accounts, nil, nil, nil, nil, nil, nil, cfg, nil, concurrency,
					service.NewBillingService(cfg, nil), nil, billing, upstream, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil)
				gateway.SetCodingPoints(service.NewOfficialPointService(nil, nil, nil))
				h := NewOpenAIGatewayHandler(gateway, concurrency, billing, &service.APIKeyService{}, nil, nil, nil, nil, cfg)
				h.apiFallback = service.NewUsageMonitorService(store)
				key := &service.APIKey{ID: 1, UserID: 2, GroupID: &policy.GroupID,
					User:  &service.User{ID: 2, Role: service.RoleAdmin, Status: service.StatusActive},
					Group: &service.Group{ID: 17, Platform: platform, UsageKind: service.UsageKindCoding, Status: service.StatusActive}}
				if mode == "ordinary" {
					key.User.Role = service.RoleUser
				}
				router := gin.New()
				router.Use(func(c *gin.Context) {
					c.Set(string(middleware.ContextKeyAPIKey), key)
					c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: key.UserID, Concurrency: 1})
					c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.Group, key.Group))
					c.Next()
				})
				router.POST("/v1/responses", h.Responses)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"`+model+`","input":"existing client"}`)))
				if mode == "admin" {
					require.Equal(t, http.StatusOK, w.Code, w.Body.String())
					require.Equal(t, "true", w.Header().Get("X-Mile-API-Fallback"))
					require.Equal(t, platform, store.observedPlatform)
					require.Equal(t, platform, store.reservation.Platform)
					require.Equal(t, 1, upstream.calls)
					require.Equal(t, 1, store.sent)
					require.Equal(t, 1, store.settled)
				} else {
					require.Zero(t, upstream.calls, w.Body.String())
					require.Zero(t, store.reserved)
					require.Empty(t, w.Header().Get("X-Mile-API-Fallback"))
				}
			})
		}
	}
}
