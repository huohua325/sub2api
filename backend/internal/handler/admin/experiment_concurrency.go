package admin

import (
	"context"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type experimentConcurrencyService interface {
	ExperimentConcurrency(context.Context, int64) (service.ExperimentConcurrencyConfig, error)
	SetExperimentConcurrency(context.Context, int64, int64, service.ExperimentConcurrencyConfig) (service.ExperimentConcurrencyConfig, error)
}

// GetExperimentConcurrency reads one user's explicitly granted execution limit.
func (h *UsageMonitorHandler) GetExperimentConcurrency(c *gin.Context) {
	user, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || user <= 0 {
		response.BadRequest(c, "invalid user ID")
		return
	}
	svc, ok := h.svc.(experimentConcurrencyService)
	if !ok {
		response.Error(c, 503, "experiment concurrency service unavailable")
		return
	}
	out, err := svc.ExperimentConcurrency(c.Request.Context(), user)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, out)
}

// SetExperimentConcurrency grants or revokes async admission for one user.
func (h *UsageMonitorHandler) SetExperimentConcurrency(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Error(c, 401, "authentication required")
		return
	}
	user, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || user <= 0 {
		response.BadRequest(c, "invalid user ID")
		return
	}
	var body struct {
		Enabled   *bool `json:"enabled" binding:"required"`
		MaxActive int64 `json:"max_active" binding:"required,min=1,max=1000"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "enabled and integer max_active (1-1000) are required")
		return
	}
	svc, ok := h.svc.(experimentConcurrencyService)
	if !ok {
		response.Error(c, 503, "experiment concurrency service unavailable")
		return
	}
	out, err := svc.SetExperimentConcurrency(c.Request.Context(), subject.UserID, user,
		service.ExperimentConcurrencyConfig{Enabled: *body.Enabled, MaxActive: body.MaxActive})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, out)
}
