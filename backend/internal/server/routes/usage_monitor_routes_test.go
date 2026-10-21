package routes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type usageMonitorStub struct{}

func (usageMonitorStub) Read(context.Context) (*service.UsageMonitorView, error) {
	return &service.UsageMonitorView{
		Disclaimer: "gateway_observed_only",
		Parents:    []service.UsageMonitorParent{},
		Users:      []service.UsageMonitorUser{},
	}, nil
}

func TestUsageMonitorRouteRequiresAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handlers := &handler.Handlers{Admin: &handler.AdminHandlers{
		UsageMonitor: adminhandler.NewUsageMonitorHandler(usageMonitorStub{}),
	}}
	adminAuth := servermiddleware.AdminAuthMiddleware(func(c *gin.Context) {
		if c.GetHeader("Authorization") != "Bearer admin" {
			servermiddleware.AbortWithError(c, http.StatusForbidden, "FORBIDDEN", "Admin access required")
			return
		}
		c.Next()
	})
	auditLog := servermiddleware.AuditLogMiddleware(func(c *gin.Context) { c.Next() })
	stepUp := servermiddleware.StepUpAuthMiddleware(func(c *gin.Context) { c.Next() })
	RegisterAdminRoutes(router.Group("/api/v1"), handlers, adminAuth, auditLog, stepUp, nil, nil)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/usage-monitor", nil)
	request.Header.Set("Authorization", "Bearer user-token")
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusForbidden, recorder.Code)
	var forbidden map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &forbidden))
	require.Equal(t, "FORBIDDEN", forbidden["code"])
	require.Equal(t, "Admin access required", forbidden["message"])

	missing := httptest.NewRecorder()
	router.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/api/v1/admin/usage/usage-monitor", nil))
	require.Equal(t, http.StatusNotFound, missing.Code)

	ok := httptest.NewRecorder()
	adminRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/usage-monitor", nil)
	adminRequest.Header.Set("Authorization", "Bearer admin")
	router.ServeHTTP(ok, adminRequest)
	require.Equal(t, http.StatusOK, ok.Code)
	var success map[string]any
	require.NoError(t, json.Unmarshal(ok.Body.Bytes(), &success))
	require.Equal(t, float64(0), success["code"])
	require.Equal(t, "success", success["message"])
}
