package admin

import (
	"fmt"
	"net/http"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// WeeklyUsageExportHandler 提供管理员周用量汇总下载。
type WeeklyUsageExportHandler struct {
	svc *service.WeeklyUsageExportService
}

// NewWeeklyUsageExportHandler 创建周报导出处理器。
func NewWeeklyUsageExportHandler(svc *service.WeeklyUsageExportService) *WeeklyUsageExportHandler {
	return &WeeklyUsageExportHandler{svc: svc}
}

// Weeks 返回可选的网关周。
// GET /api/v1/admin/usage/weekly-export/weeks
func (h *WeeklyUsageExportHandler) Weeks(c *gin.Context) {
	if h == nil || h.svc == nil {
		response.ErrorFrom(c, errWeeklyExportUnavailable())
		return
	}
	out, err := h.svc.Weeks(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, out)
}

// Export 下载一个网关周的 zip。
// GET /api/v1/admin/usage/weekly-export
func (h *WeeklyUsageExportHandler) Export(c *gin.Context) {
	if h == nil || h.svc == nil {
		response.ErrorFrom(c, errWeeklyExportUnavailable())
		return
	}
	var userID int64
	if subject, ok := middleware.GetAuthSubjectFromContext(c); ok {
		userID = subject.UserID
	}
	body, filename, err := h.svc.Export(c.Request.Context(), c.Query("week_start"), userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "application/zip", body)
}

// Filtered 按日期、分组和账号下载用量 CSV。不写周报快照。
// GET /api/v1/admin/usage/weekly-export/filtered
func (h *WeeklyUsageExportHandler) Filtered(c *gin.Context) {
	if h == nil || h.svc == nil {
		response.ErrorFrom(c, errWeeklyExportUnavailable())
		return
	}
	body, filename, err := h.svc.ExportFiltered(
		c.Request.Context(),
		c.Query("start_date"),
		c.Query("end_date"),
		c.Query("group_id"),
		c.Query("account_id"),
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "text/csv; charset=utf-8", body)
}

func errWeeklyExportUnavailable() error {
	return infraerrors.ServiceUnavailable("WEEKLY_EXPORT_UNAVAILABLE", "周报导出尚未就绪")
}
