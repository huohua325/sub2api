package routes

import (
	"errors"
	"io"
	"os"
	"strings"

	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/pkg/developmentworkspace"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func registerDevelopmentWorkspaceRoutes(admin *gin.RouterGroup) {
	var cfg developmentworkspace.Config
	var configErr error
	enabled := strings.EqualFold(strings.TrimSpace(os.Getenv("DEV_WORKSPACE_ENABLED")), "true")
	if enabled {
		cfg = developmentworkspace.Config{
			URL:           os.Getenv("DEV_WORKSPACE_T3_URL"),
			Token:         os.Getenv("DEV_WORKSPACE_T3_TOKEN"),
			ProjectID:     os.Getenv("DEV_WORKSPACE_T3_PROJECT_ID"),
			DefaultBranch: os.Getenv("DEV_WORKSPACE_T3_DEFAULT_BRANCH"),
			Provider:      os.Getenv("DEV_WORKSPACE_T3_PROVIDER"),
			Model:         os.Getenv("DEV_WORKSPACE_T3_MODEL"),
		}
		if tokenFile := os.Getenv("DEV_WORKSPACE_T3_TOKEN_FILE"); tokenFile != "" {
			cfg.Token, configErr = readDevelopmentWorkspaceToken(tokenFile)
		}
	}
	client, err := developmentworkspace.New(cfg)
	if enabled && strings.TrimSpace(cfg.URL) == "" {
		client, err = nil, errors.New("missing development workspace endpoint")
	}
	if configErr != nil {
		client, err = nil, configErr
	}
	if err != nil {
		logger.LegacyPrintf("admin.development_workspace", "[DevelopmentWorkspace] invalid T3 configuration")
	}
	mountDevelopmentWorkspaceRoutes(admin, adminhandler.NewDevelopmentWorkspaceHandler(client, err != nil))
}

func readDevelopmentWorkspaceToken(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	const maxTokenBytes = 16 << 10
	raw, err := io.ReadAll(io.LimitReader(file, maxTokenBytes+1))
	if err != nil {
		return "", err
	}
	if len(raw) > maxTokenBytes || strings.TrimSpace(string(raw)) == "" {
		return "", errors.New("invalid development workspace token file")
	}
	return strings.TrimSpace(string(raw)), nil
}

func mountDevelopmentWorkspaceRoutes(admin *gin.RouterGroup, h *adminhandler.DevelopmentWorkspaceHandler) {
	workspace := admin.Group("/development-workspace", requireDevelopmentWorkspaceSession)
	workspace.GET("/status", h.Status)
	workspace.GET("/tasks", h.List)
	workspace.POST("/tasks", h.Create)
	workspace.GET("/tasks/:id", h.Get)
	workspace.POST("/tasks/:id/messages", h.Send)
	workspace.POST("/tasks/:id/stop", h.Stop)
}

func requireDevelopmentWorkspaceSession(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "Authorization required")
		c.Abort()
		return
	}
	role, _ := middleware.GetUserRoleFromContext(c)
	if role != service.RoleAdmin || c.GetString("auth_method") != service.AuditAuthMethodJWT {
		response.Forbidden(c, "An administrator browser session is required")
		c.Abort()
		return
	}
	c.Next()
}
