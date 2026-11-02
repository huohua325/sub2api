package handler

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type userModelCatalogLister interface {
	List(context.Context, int64) (*service.UserModelCatalog, error)
}

type UserModelCatalogHandler struct{ catalog userModelCatalogLister }

func NewUserModelCatalogHandler(catalog *service.UserModelCatalogService) *UserModelCatalogHandler {
	return &UserModelCatalogHandler{catalog: catalog}
}

// List returns the authenticated user's account model union, never another
// user's catalog selected by a query parameter or a shared HTTP cache.
func (h *UserModelCatalogHandler) List(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	catalog, err := h.catalog.List(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, catalog)
}
