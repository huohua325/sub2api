package admin

import (
	"context"
	"strconv"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type codingReservationReader interface {
	ListCodingReservations(context.Context, service.CodingReservationQuery) (service.CodingReservationPage, error)
}

// ListCodingReservations is read-only and inherits the existing admin route guard.
func (h *UsageMonitorHandler) ListCodingReservations(c *gin.Context) {
	reader, ok := h.svc.(codingReservationReader)
	if !ok {
		response.ErrorFrom(c, infraerrors.ServiceUnavailable("CODING_QUOTA_UNAVAILABLE", "quota service unavailable"))
		return
	}
	q := service.CodingReservationQuery{Purpose: c.Query("purpose"), State: c.Query("state"), Before: c.Query("before"), Limit: 50}
	if raw := c.Query("user_id"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			response.ErrorFrom(c, infraerrors.BadRequest("INVALID_QUOTA_QUERY", "invalid user_id"))
			return
		}
		q.UserID = id
	}
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			response.ErrorFrom(c, infraerrors.BadRequest("INVALID_QUOTA_QUERY", "invalid limit"))
			return
		}
		q.Limit = n
		if q.Limit > 100 {
			q.Limit = 100
		}
	}
	c.Header("Cache-Control", "no-store")
	page, err := reader.ListCodingReservations(c.Request.Context(), q)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, page)
}
