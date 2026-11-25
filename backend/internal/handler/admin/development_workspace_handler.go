package admin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/developmentworkspace"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const workspaceWriteTimeout = 35 * time.Second

// DevelopmentWorkspaceHandler exposes the configured T3 project to the administrator panel.
type DevelopmentWorkspaceHandler struct {
	client        *developmentworkspace.Client
	misconfigured bool
}

// NewDevelopmentWorkspaceHandler creates the administrator's task handler.
func NewDevelopmentWorkspaceHandler(client *developmentworkspace.Client, misconfigured bool) *DevelopmentWorkspaceHandler {
	return &DevelopmentWorkspaceHandler{client: client, misconfigured: misconfigured}
}

// Status reports the configured project and model.
func (h *DevelopmentWorkspaceHandler) Status(c *gin.Context) {
	if h.misconfigured {
		response.ErrorWithDetails(c, http.StatusServiceUnavailable, "Development workspace configuration is incomplete", "DEV_WORKSPACE_MISCONFIGURED", nil)
		return
	}
	status, err := h.client.Status(c.Request.Context())
	workspaceReply(c, status, err)
}

// List returns the recent tasks in the configured project.
func (h *DevelopmentWorkspaceHandler) List(c *gin.Context) {
	if h.available(c) {
		tasks, err := h.client.List(c.Request.Context())
		workspaceReply(c, tasks, err)
	}
}

// Get returns the conversation and activity of one task.
func (h *DevelopmentWorkspaceHandler) Get(c *gin.Context) {
	if h.available(c) {
		task, err := h.client.Get(c.Request.Context(), c.Param("id"))
		workspaceReply(c, task, err)
	}
}

// Create starts a Codex task in a worktree.
func (h *DevelopmentWorkspaceHandler) Create(c *gin.Context) {
	var input developmentworkspace.CreateInput
	if !h.available(c) || !decodeWorkspaceInput(c, &input) || !workspaceRequestID(c, input.RequestID) {
		return
	}
	input.RequestID = workspaceRemoteID(c, "create")
	executeAdminIdempotentJSONWithTimeout(c, "admin.development_workspace.create", input, service.DefaultWriteIdempotencyTTL(), workspaceWriteTimeout, func(ctx context.Context) (any, error) {
		task, err := h.client.Create(ctx, input)
		if err == nil {
			recordWorkspaceTask(c, task.ID)
		}
		return task, workspaceError(err)
	})
}

// Send adds a follow-up to an existing task.
func (h *DevelopmentWorkspaceHandler) Send(c *gin.Context) {
	var input developmentworkspace.SendInput
	if !h.available(c) || !decodeWorkspaceInput(c, &input) || !workspaceRequestID(c, input.RequestID) {
		return
	}
	id := c.Param("id")
	input.RequestID = workspaceRemoteID(c, "send:"+id)
	payload := struct {
		TaskID string `json:"task_id"`
		developmentworkspace.SendInput
	}{TaskID: id, SendInput: input}
	executeAdminIdempotentJSONWithTimeout(c, "admin.development_workspace.send", payload, service.DefaultWriteIdempotencyTTL(), workspaceWriteTimeout, func(ctx context.Context) (any, error) {
		err := h.client.Send(ctx, id, input)
		if err == nil {
			recordWorkspaceTask(c, id)
		}
		return nil, workspaceError(err)
	})
}

// Stop interrupts the running task.
func (h *DevelopmentWorkspaceHandler) Stop(c *gin.Context) {
	var input struct {
		RequestID string `json:"request_id"`
	}
	if !h.available(c) || !decodeWorkspaceInput(c, &input) || !workspaceRequestID(c, input.RequestID) {
		return
	}
	id := c.Param("id")
	requestID := workspaceRemoteID(c, "stop:"+id)
	executeAdminIdempotentJSONWithTimeout(c, "admin.development_workspace.stop", id, service.DefaultWriteIdempotencyTTL(), workspaceWriteTimeout, func(ctx context.Context) (any, error) {
		err := h.client.StopWithRequestID(ctx, id, requestID)
		if err == nil {
			recordWorkspaceTask(c, id)
		}
		return nil, workspaceError(err)
	})
}

func (h *DevelopmentWorkspaceHandler) available(c *gin.Context) bool {
	if h.client != nil {
		return true
	}
	response.ErrorWithDetails(c, http.StatusServiceUnavailable, "Development workspace configuration is incomplete", "DEV_WORKSPACE_MISCONFIGURED", nil)
	return false
}

func decodeWorkspaceInput(c *gin.Context, input any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(input); err == nil && decoder.Decode(new(any)) == io.EOF {
		return true
	}
	response.BadRequest(c, "Invalid development workspace request")
	return false
}

func workspaceRequestID(c *gin.Context, key string) bool {
	id, err := uuid.Parse(key)
	if err != nil || id == uuid.Nil || len(key) != 36 || !strings.EqualFold(id.String(), key) {
		response.BadRequest(c, "A valid request_id is required")
		return false
	}
	c.Request.Header.Set("Idempotency-Key", id.String())
	return true
}

func workspaceRemoteID(c *gin.Context, action string) string {
	actor, _ := middleware.GetAuthSubjectFromContext(c)
	identity := "mile-development-workspace/" + strconv.FormatInt(actor.UserID, 10) + "/" + action + "/" + c.GetHeader("Idempotency-Key")
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(identity)).String()
}

func recordWorkspaceTask(c *gin.Context, id string) {
	middleware.SetAuditExtra(c, map[string]any{"development_task_id": id})
}

func workspaceReply(c *gin.Context, data any, err error) {
	if err != nil {
		response.ErrorFrom(c, workspaceError(err))
		return
	}
	response.Success(c, data)
}

func workspaceError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, developmentworkspace.ErrDisabled):
		return infraerrors.ServiceUnavailable("DEV_WORKSPACE_DISABLED", "Development workspace is disabled")
	case errors.Is(err, developmentworkspace.ErrNotFound):
		return infraerrors.NotFound("DEV_WORKSPACE_TASK_NOT_FOUND", "Development task not found")
	case errors.Is(err, developmentworkspace.ErrInvalid):
		return infraerrors.BadRequest("DEV_WORKSPACE_INVALID_REQUEST", "Invalid task message, branch or ID")
	case errors.Is(err, developmentworkspace.ErrConflict):
		return infraerrors.Conflict("DEV_WORKSPACE_CONFLICT", "Development task state has changed; refresh and try again")
	default:
		return infraerrors.ServiceUnavailable("DEV_WORKSPACE_UNAVAILABLE", "Unable to reach the development workspace")
	}
}
