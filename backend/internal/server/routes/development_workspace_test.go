package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestDevelopmentWorkspaceAdminSession(t *testing.T) {
	t.Setenv("DEV_WORKSPACE_ENABLED", "false")
	for _, tc := range []struct {
		name   string
		role   string
		method string
		want   int
	}{
		{"no session", "", "", http.StatusUnauthorized},
		{"ordinary user", service.RoleUser, service.AuditAuthMethodJWT, http.StatusForbidden},
		{"admin API key", service.RoleAdmin, service.AuditAuthMethodAdminAPIKey, http.StatusForbidden},
		{"MCP grant", service.RoleAdmin, service.AuditAuthMethodMCPOAuth, http.StatusForbidden},
		{"admin browser session", service.RoleAdmin, service.AuditAuthMethodJWT, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := gin.New()
			admin := engine.Group("/api/v1/admin", func(c *gin.Context) {
				if tc.role != "" {
					c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 10})
					c.Set(string(middleware.ContextKeyUserRole), tc.role)
					c.Set("auth_method", tc.method)
				}
				c.Next()
			})
			registerDevelopmentWorkspaceRoutes(admin)

			w := httptest.NewRecorder()
			engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/admin/development-workspace/status", nil))
			require.Equal(t, tc.want, w.Code, w.Body.String())
			require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
			if tc.want == http.StatusOK {
				require.Contains(t, w.Body.String(), `"enabled":false`)
			}
		})
	}
}
