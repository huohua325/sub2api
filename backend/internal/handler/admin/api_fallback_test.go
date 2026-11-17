package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAPIFallbackAdminHandlersRequireAuthenticatedActor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewUsageMonitorHandler(service.NewUsageMonitorService(handlerAdjustStore{t: t}))
	for name, handler := range map[string]gin.HandlerFunc{"list": h.GetAPIFallback, "save": h.SaveAPIFallbackPolicy, "disable": h.DisableAPIFallbackPolicy, "preview": h.PreviewAPIFallback, "resolve": h.ResolveAPIFallback} {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Params = gin.Params{{Key: "id", Value: "private-policy"}}
			c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/api-fallback", strings.NewReader(`{"confirm":true,"actor_user_id":1}`))
			handler(c)
			require.Equal(t, http.StatusUnauthorized, rec.Code)
		})
	}
}
