package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAPIFallbackAdminRoutesDenyOrdinaryAndUnauthenticated(t *testing.T) {
	for _, role := range []string{"", "user"} {
		for _, path := range []string{"", "/policies", "/policies/policy/preview", "/policies/policy/disable", "/attempts/attempt/resolve"} {
			t.Run(role+path, func(t *testing.T) {
				engine := gin.New()
				admin := engine.Group("/api/v1/admin", func(c *gin.Context) {
					if role != "" {
						c.Set(string(middleware.ContextKeyUserRole), role)
					}
				})
				h := &handler.Handlers{Admin: &handler.AdminHandlers{UsageMonitor: adminhandler.NewUsageMonitorHandler(nil)}}
				registerUsageRoutes(admin, h)
				method := http.MethodPost
				if path == "" {
					method = http.MethodGet
				}
				w := httptest.NewRecorder()
				engine.ServeHTTP(w, httptest.NewRequest(method, "/api/v1/admin/api-fallback"+path, strings.NewReader("{}")))
				if role == "" {
					require.Equal(t, http.StatusUnauthorized, w.Code)
				} else {
					require.Equal(t, http.StatusForbidden, w.Code)
				}
			})
		}
	}
}

func TestAPIFallbackScopedKeysDoNotInheritSpendingControls(t *testing.T) {
	policy := adminaccess.Policy{GlobalPermissions: []string{"users.quotas.write", "groups.read", "usage.read", "accounts.read"}}
	for _, route := range []struct{ method, path string }{{"GET", "/api/v1/admin/api-fallback"}, {"POST", "/api/v1/admin/api-fallback/policies"}, {"POST", "/api/v1/admin/api-fallback/policies/:id/disable"}, {"POST", "/api/v1/admin/api-fallback/policies/:id/preview"}, {"POST", "/api/v1/admin/api-fallback/attempts/:id/resolve"}} {
		require.False(t, adminaccess.RouteAllowed(policy, route.method, route.path), route.path)
	}
}
