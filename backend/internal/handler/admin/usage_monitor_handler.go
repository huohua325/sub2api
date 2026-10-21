package admin

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

type usageMonitorReader interface {
	Read(ctx context.Context) (*service.UsageMonitorView, error)
	Adjust(ctx context.Context, in service.PurposeAdjustInput) (*service.PurposeAdjustResult, error)
	ListAdjustments(ctx context.Context, query service.PurposeAdjustmentQuery) (*service.PurposeAdjustmentList, error)
}

// UsageMonitorHandler serves the admin usage-monitor read model.
type UsageMonitorHandler struct {
	svc usageMonitorReader
}

func NewUsageMonitorHandler(svc usageMonitorReader) *UsageMonitorHandler {
	return &UsageMonitorHandler{svc: svc}
}

// Get returns the current monitor.
// GET /api/v1/admin/usage-monitor
func (h *UsageMonitorHandler) Get(c *gin.Context) {
	if h == nil || h.svc == nil {
		response.ErrorFrom(c, infraerrors.ServiceUnavailable("USAGE_MONITOR_UNAVAILABLE", "用量监控尚未就绪"))
		return
	}
	started := time.Now()
	view, err := h.svc.Read(c.Request.Context())
	if elapsed := time.Since(started); elapsed > time.Second {
		parents := 0
		if view != nil {
			parents = len(view.Parents)
		}
		slog.Warn("usage_monitor_slow", "duration_ms", elapsed.Milliseconds(), "parent_count", parents)
	}
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, view)
}

type purposeAdjustBody struct {
	UserID  int64  `json:"user_id"`
	Purpose string `json:"purpose"`
	Action  string `json:"action"`
	Value   string `json:"value"`
	Note    string `json:"note"`
	Confirm bool   `json:"confirm"`
}

// Adjust changes one purpose wallet.
// POST /api/v1/admin/usage-monitor/adjustments
func (h *UsageMonitorHandler) Adjust(c *gin.Context) {
	if h == nil || h.svc == nil {
		response.ErrorFrom(c, infraerrors.ServiceUnavailable("USAGE_MONITOR_UNAVAILABLE", "用量监控尚未就绪"))
		return
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.ErrorFrom(c, infraerrors.Unauthorized("ADJUSTMENT_ACTOR_REQUIRED", "authentication required"))
		return
	}
	var body purposeAdjustBody
	if err := c.ShouldBindJSON(&body); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("INVALID_ADJUSTMENT", "invalid adjustment"))
		return
	}
	out, err := h.svc.Adjust(c.Request.Context(), service.PurposeAdjustInput{
		ActorUserID: subject.UserID,
		UserID:      body.UserID,
		Purpose:     body.Purpose,
		Action:      body.Action,
		Value:       body.Value,
		Note:        body.Note,
		Confirm:     body.Confirm,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, out)
}

// ListAdjustments returns audit rows. It does not create wallets.
// GET /api/v1/admin/usage-monitor/adjustments
func (h *UsageMonitorHandler) ListAdjustments(c *gin.Context) {
	if h == nil || h.svc == nil {
		response.ErrorFrom(c, infraerrors.ServiceUnavailable("USAGE_MONITOR_UNAVAILABLE", "用量监控尚未就绪"))
		return
	}
	query, err := purposeAdjustmentQuery(c)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	out, err := h.svc.ListAdjustments(c.Request.Context(), query)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, out)
}

func purposeAdjustmentQuery(c *gin.Context) (service.PurposeAdjustmentQuery, error) {
	var query service.PurposeAdjustmentQuery
	if raw := strings.TrimSpace(c.Query("user_id")); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			return service.PurposeAdjustmentQuery{}, infraerrors.BadRequest("INVALID_ADJUSTMENT_QUERY", "invalid user_id")
		}
		query.UserID = &id
	}
	if raw := strings.TrimSpace(c.Query("before_id")); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			return service.PurposeAdjustmentQuery{}, infraerrors.BadRequest("INVALID_ADJUSTMENT_QUERY", "invalid before_id")
		}
		query.BeforeID = &id
	}
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit <= 0 {
			return service.PurposeAdjustmentQuery{}, infraerrors.BadRequest("INVALID_ADJUSTMENT_QUERY", "invalid limit")
		}
		query.Limit = limit
	}
	return query, nil
}
