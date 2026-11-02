package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type userModelCatalogListerStub struct{ userID int64 }

func (s *userModelCatalogListerStub) List(_ context.Context, id int64) (*service.UserModelCatalog, error) {
	s.userID = id
	if id != 7 {
		return nil, errors.New("wrong authenticated user")
	}
	return &service.UserModelCatalog{Models: []service.UserCatalogModel{{ID: "future-model", Platform: "openai", Groups: []service.UserCatalogGroup{}}}}, nil
}

func TestUserModelCatalogUsesAuthenticatedUserAndDisablesSharedCaching(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &userModelCatalogListerStub{}
	h := &UserModelCatalogHandler{catalog: s}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/user/models?user_id=99", nil)
	h.List(c)
	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.Zero(t, s.userID)
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/user/models?user_id=99", nil)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
	h.List(c)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, int64(7), s.userID)
	require.Equal(t, "private, no-store", w.Header().Get("Cache-Control"))
	require.Contains(t, w.Body.String(), "future-model")
}
