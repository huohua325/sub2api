package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type personalResetHandlerStore struct {
	service.UsageMonitorStore
	readTarget, readSelf int64
	reset                service.PersonalQuotaResetInput
}

func (s *personalResetHandlerStore) ReadPersonalQuotaResets(_ context.Context, target, self int64) (*service.PersonalQuotaResetView, error) {
	s.readTarget, s.readSelf = target, self
	return &service.PersonalQuotaResetView{}, nil
}

func (s *personalResetHandlerStore) ResetPersonalQuota(_ context.Context, in service.PersonalQuotaResetInput) (*service.PersonalQuotaReset, error) {
	s.reset = in
	return &service.PersonalQuotaReset{}, nil
}

func TestPersonalQuotaResetOnlySessionGrantsAdminSelf(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, method := range []string{service.AuditAuthMethodJWT, service.AuditAuthMethodAdminAPIKey, ""} {
		t.Run(method, func(t *testing.T) {
			store := &personalResetHandlerStore{}
			h := NewUsageMonitorHandler(service.NewUsageMonitorService(store))
			for _, verb := range []string{http.MethodGet, http.MethodPost} {
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Params = gin.Params{{Key: "id", Value: "12"}}
				c.Set("auth_method", method)
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 12})
				c.Request = httptest.NewRequest(verb, "/api/v1/admin/users/12/purpose-quota-resets",
					strings.NewReader(`{"confirm":true,"allow_admin_self":true,"actor_user_id":99,"target_user_id":99}`))
				if verb == http.MethodGet {
					h.GetPersonalQuotaResets(c)
				} else {
					h.ResetPersonalQuota(c)
				}
				require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			}
			require.Equal(t, int64(12), store.readTarget)
			require.Equal(t, int64(12), store.reset.ActorUserID)
			require.Equal(t, int64(12), store.reset.UserID)
			require.Equal(t, method == service.AuditAuthMethodJWT, store.reset.AllowAdminSelf)
			if store.reset.AllowAdminSelf {
				require.Equal(t, int64(12), store.readSelf)
			} else {
				require.Zero(t, store.readSelf)
			}
		})
	}
}

func TestPersonalQuotaResetRequiresAuthenticatedActor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewUsageMonitorHandler(service.NewUsageMonitorService(handlerAdjustStore{t: t}))
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Params = gin.Params{{Key: "id", Value: "12"}}
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/12/purpose-quota-resets", strings.NewReader(`{"confirm":true}`))
	h.ResetPersonalQuota(c)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}
