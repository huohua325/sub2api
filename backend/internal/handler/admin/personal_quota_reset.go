package admin

import (
	"context"
	"strconv"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type personalQuotaResetService interface {
	PersonalQuotaResets(context.Context, int64) (*service.PersonalQuotaResetView, error)
	ResetPersonalQuota(context.Context, service.PersonalQuotaResetInput) (*service.PersonalQuotaReset, error)
}

func (h *UsageMonitorHandler) GetPersonalQuotaResets(c *gin.Context) {
	svc, ok := h.svc.(personalQuotaResetService)
	if !ok {
		response.Error(c, 503, "personal quota service unavailable")
		return
	}
	user, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || user <= 0 {
		response.BadRequest(c, "invalid user ID")
		return
	}
	out, err := svc.PersonalQuotaResets(c.Request.Context(), user)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, out)
}

func (h *UsageMonitorHandler) ResetPersonalQuota(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.ErrorFrom(c, infraerrors.Unauthorized("RESET_ACTOR_REQUIRED", "authentication required"))
		return
	}
	svc, ok := h.svc.(personalQuotaResetService)
	if !ok {
		response.Error(c, 503, "personal quota service unavailable")
		return
	}
	user, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || user <= 0 {
		response.BadRequest(c, "invalid user ID")
		return
	}
	var in service.PersonalQuotaResetInput
	if err = c.ShouldBindJSON(&in); err != nil {
		response.BadRequest(c, "invalid personal reset")
		return
	}
	in.ActorUserID = subject.UserID
	in.UserID = user
	out, err := svc.ResetPersonalQuota(c.Request.Context(), in)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, out)
}
