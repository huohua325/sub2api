package admin

import (
	"context"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type apiFallbackAdminService interface {
	APIFallback(context.Context, int64) (*service.APIFallbackView, error)
	SaveAPIFallbackPolicy(context.Context, service.APIFallbackPolicyInput) (*service.APIFallbackPolicy, error)
	DisableAPIFallbackPolicy(context.Context, string, int64, int64, string) (*service.APIFallbackPolicy, error)
	PreviewAPIFallback(context.Context, int64, string) (*service.APIFallbackPreview, error)
	ResolveAPIFallback(context.Context, service.APIFallbackResolveInput) (*service.APIFallbackAttempt, error)
}

func (h *UsageMonitorHandler) fallbackAdmin(c *gin.Context) (apiFallbackAdminService, int64, bool) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.ErrorFrom(c, infraerrors.Unauthorized("API_FALLBACK_ACTOR_REQUIRED", "administrator authentication required"))
		return nil, 0, false
	}
	svc, ok := h.svc.(apiFallbackAdminService)
	if !ok {
		response.Error(c, 503, "API standby is unavailable")
		return nil, 0, false
	}
	c.Header("Cache-Control", "no-store")
	return svc, subject.UserID, true
}

// GetAPIFallback returns administrator-only policy and cost records.
func (h *UsageMonitorHandler) GetAPIFallback(c *gin.Context) {
	svc, actor, ok := h.fallbackAdmin(c)
	if !ok {
		return
	}
	out, err := svc.APIFallback(c.Request.Context(), actor)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, out)
}

// SaveAPIFallbackPolicy creates a policy or an immutable approved revision.
func (h *UsageMonitorHandler) SaveAPIFallbackPolicy(c *gin.Context) {
	svc, actor, ok := h.fallbackAdmin(c)
	if !ok {
		return
	}
	var in service.APIFallbackPolicyInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.BadRequest(c, "invalid standby policy")
		return
	}
	in.ActorUserID = actor
	out, err := svc.SaveAPIFallbackPolicy(c.Request.Context(), in)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, out)
}

// DisableAPIFallbackPolicy stops new admissions without releasing real costs.
func (h *UsageMonitorHandler) DisableAPIFallbackPolicy(c *gin.Context) {
	svc, actor, ok := h.fallbackAdmin(c)
	if !ok {
		return
	}
	var in struct {
		ExpectedVersion int64  `json:"expected_version"`
		Confirm         bool   `json:"confirm"`
		Note            string `json:"note"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || !in.Confirm || strings.TrimSpace(in.Note) == "" {
		response.BadRequest(c, "confirmation, current version and reason are required")
		return
	}
	out, err := svc.DisableAPIFallbackPolicy(c.Request.Context(), c.Param("id"), in.ExpectedVersion, actor, in.Note)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, out)
}

// PreviewAPIFallback never sends a paid request.
func (h *UsageMonitorHandler) PreviewAPIFallback(c *gin.Context) {
	svc, actor, ok := h.fallbackAdmin(c)
	if !ok {
		return
	}
	out, err := svc.PreviewAPIFallback(c.Request.Context(), actor, c.Param("id"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, out)
}

// ResolveAPIFallback appends supplier evidence for a previously unknown attempt.
func (h *UsageMonitorHandler) ResolveAPIFallback(c *gin.Context) {
	svc, actor, ok := h.fallbackAdmin(c)
	if !ok {
		return
	}
	var in service.APIFallbackResolveInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.BadRequest(c, "invalid standby reconciliation")
		return
	}
	in.ActorUserID = actor
	in.ID = c.Param("id")
	out, err := svc.ResolveAPIFallback(c.Request.Context(), in)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, out)
}
