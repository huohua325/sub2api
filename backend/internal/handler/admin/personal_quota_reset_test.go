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
