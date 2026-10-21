package admin

import (
	"context"
	"log/slog"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

type usageMonitorReader interface {
	Read(ctx context.Context) (*service.UsageMonitorView, error)
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
