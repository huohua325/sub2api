package service

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
)

type experimentRequestContextKey struct{}

const experimentRequestHandleKey = "mile.experiment-accounting"

// ExperimentRequestHandle preserves an unknown-cost attempt across disconnects.
// Its execution slot and unresolved billing liability have separate lifetimes.
type ExperimentRequestHandle struct {
	mu       sync.Mutex
	input    ExperimentAdmissionInput
	store    ExperimentAdmissionStore
	gin      *gin.Context
	reserved bool
	sent     bool
}

// PrepareExperimentAccountingContext attaches lazy admission to authenticated
// experiment requests. Discovery and other suppliers do not reserve anything.
func (s *APIKeyService) PrepareExperimentAccountingContext(c *gin.Context, key *APIKey) {
	if s != nil && s.cfg != nil && s.cfg.RunMode == config.RunModeSimple {
		return // Simple mode intentionally has no personal experiment billing.
	}
	if c == nil || c.Request == nil || key == nil || key.Group == nil || PurposeOfGroup(key.Group.Platform, key.Group.UsageKind) != PurposeExperiment {
		return
	}
	h := &ExperimentRequestHandle{gin: c, input: ExperimentAdmissionInput{
		RequestID: resolveUsageBillingRequestID(c.Request.Context(), ""),
		UserID:    key.UserID, APIKeyID: key.ID, StartedAt: time.Now().UTC(),
	}}
	if s != nil && s.officialPoints != nil {
		h.store, _ = s.officialPoints.store.(ExperimentAdmissionStore)
	}
	c.Set(experimentRequestHandleKey, h)
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), experimentRequestContextKey{}, h))
}

func experimentRequestFromContext(ctx context.Context) *ExperimentRequestHandle {
	if ctx == nil {
		return nil
	}
	h, _ := ctx.Value(experimentRequestContextKey{}).(*ExperimentRequestHandle)
	return h
}

// HasExperimentAccounting marks billing work that must not enter a droppable queue.
func HasExperimentAccounting(ctx context.Context) bool {
	h := experimentRequestFromContext(ctx)
	if h == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.reserved || h.sent
}

// CarryExperimentAccounting preserves the durable logical request when billing
// detaches from a disconnected client or a handler copies request context values.
func CarryExperimentAccounting(parent, target context.Context) context.Context {
	if h := experimentRequestFromContext(parent); h != nil {
		return context.WithValue(target, experimentRequestContextKey{}, h)
	}
	return target
}

// BeginExperimentHTTPAttempt is called at the real HTTP send boundary. A sent
// attempt is never silently retried, even if its first result was unavailable.
func BeginExperimentHTTPAttempt(req *http.Request, accountID int64) error {
	if req == nil || req.URL == nil || req.Method != http.MethodPost ||
		strings.HasSuffix(req.URL.Path, ":countTokens") ||
		strings.HasSuffix(req.URL.Path, "/messages/count_tokens") {
		return nil
	}
	h := experimentRequestFromContext(req.Context())
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !strings.EqualFold(req.URL.Hostname(), globalAIHost) && !h.reserved && !h.sent {
		return nil
	}
	var err error
	if !supportedGlobalAIExperimentPath(req.URL.Path) {
		err = ErrExperimentProtocolUnsupported
	} else if h.store == nil {
		err = ErrExperimentAccountingUnavailable
	} else if h.sent {
		err = ErrExperimentAlreadySent
	} else if h.reserved && (h.input.AccountID != accountID || !strings.EqualFold(req.URL.Hostname(), globalAIHost)) {
		err = ErrExperimentAccountingUnavailable
	} else {
		h.input.AccountID = accountID
		if !h.reserved {
			err = h.store.ReserveExperimentRequest(req.Context(), h.input)
			if err == nil {
				h.reserved = true
			}
		}
		if err == nil {
			err = h.store.MarkExperimentRequestSent(req.Context(), h.input.RequestID, h.input.APIKeyID)
			if err == nil {
				h.sent = true
			}
		}
	}
	if err != nil {
		writeExperimentAccountingError(h.gin, err)
	}
	return err
}

func supportedGlobalAIExperimentPath(path string) bool {
	switch strings.TrimSuffix(path, "/") {
	case "/v1/chat/completions", "/v1/responses", "/v1/messages", "/v1/embeddings", "/v1/images/generations", "/v1/images/edits":
		return true
	}
	return (strings.HasPrefix(path, "/v1beta/models/") || strings.HasPrefix(path, "/v1/models/")) &&
		(strings.HasSuffix(path, ":generateContent") || strings.HasSuffix(path, ":streamGenerateContent"))
}

// RecordExperimentHTTPResponse keeps the supplier request ID even when a
// streamed response disconnects before normal usage billing can run.
func RecordExperimentHTTPResponse(req *http.Request, resp *http.Response) {
	if req == nil || resp == nil {
		return
	}
	h := experimentRequestFromContext(req.Context())
	if h == nil {
		return
	}
	h.mu.Lock()
	sent, requestID, apiKeyID := h.sent, h.input.RequestID, h.input.APIKeyID
	h.mu.Unlock()
	if !sent {
		return
	}
	store, ok := h.store.(interface {
		RecordExperimentResponse(context.Context, string, int64, string, int) error
	})
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(req.Context()), 5*time.Second)
	defer cancel()
	if err := store.RecordExperimentResponse(ctx, requestID, apiKeyID, globalAIRequestID(resp.Header), resp.StatusCode); err != nil {
		slog.Error("experiment_response_record_failed", "request_id", requestID, "error", err)
	}
}

func finishExperimentRequest(c *gin.Context) {
	if c == nil {
		return
	}
	v, _ := c.Get(experimentRequestHandleKey)
	h, _ := v.(*ExperimentRequestHandle)
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.reserved || h.store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if h.sent {
		if store, ok := h.store.(ExperimentCompletionStore); ok {
			if err := store.CompleteExperimentRequest(ctx, h.input.RequestID, h.input.APIKeyID); err != nil {
				slog.Error("experiment_execution_completion_failed", "request_id", h.input.RequestID, "error", err)
			}
		}
		return
	}
	if err := h.store.ReleaseExperimentRequest(ctx, h.input.RequestID, h.input.APIKeyID, "request ended before send"); err != nil {
		slog.Error("experiment_unsent_release_failed", "request_id", h.input.RequestID, "error", err)
	} else {
		h.reserved = false
	}
}

func writeExperimentAccountingError(c *gin.Context, err error) {
	if c == nil || c.Writer.Written() {
		return
	}
	status, code, message := http.StatusServiceUnavailable, "EXPERIMENT_ACCOUNTING_UNAVAILABLE", "API experiment accounting is temporarily unavailable"
	switch {
	case errors.Is(err, ErrExperimentProtocolUnsupported):
		status, code, message = http.StatusBadRequest, "EXPERIMENT_PROTOCOL_UNSUPPORTED", "This GlobalAI experiment protocol cannot be safely reconciled; use standard HTTP generation"
	case errors.Is(err, ErrExperimentQuotaExhausted):
		status, code, message = http.StatusForbidden, "PURPOSE_QUOTA_EXHAUSTED", "Your API experiment quota is exhausted"
	case errors.Is(err, ErrExperimentAlreadySent):
		status, code, message = http.StatusConflict, "EXPERIMENT_ALREADY_SENT", "This experiment request was already sent and will not be replayed"
	case errors.Is(err, ErrExperimentRequestInFlight):
		status, code, message = http.StatusTooManyRequests, "EXPERIMENT_REQUEST_IN_FLIGHT", "API experiment execution capacity is busy; retry after a running request completes"
		c.Header("Retry-After", "2")
	}
	c.AbortWithStatusJSON(status, gin.H{"error": gin.H{"type": code, "code": code, "message": message}})
}
