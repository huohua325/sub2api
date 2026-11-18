package service

import (
	"context"
	"errors"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/gin-gonic/gin"
)

// ErrExperimentProtocolUnsupported rejects protocols without a durable GlobalAI
// send/receipt/settlement boundary. Other providers retain their current behavior.
var ErrExperimentProtocolUnsupported = infraerrors.New(http.StatusBadRequest, "EXPERIMENT_PROTOCOL_UNSUPPORTED", "GlobalAI API experiments currently require ordinary HTTP generation; WebSocket, batch and asynchronous generation are not supported")

type experimentDeferredProtocolKey struct{}

// WithExperimentDeferredProtocol records an asynchronous operation before its
// eventual account selection, without guessing which supplier will be chosen.
func WithExperimentDeferredProtocol(ctx context.Context, protocol string) context.Context {
	return context.WithValue(ctx, experimentDeferredProtocolKey{}, protocol)
}

// ValidateGlobalAIExperimentProtocol checks the actual selected account and key
// group. An empty protocol only checks an explicitly marked deferred operation.
func ValidateGlobalAIExperimentProtocol(ctx context.Context, c *gin.Context, account *Account, group *Group, protocol string) error {
	if protocol == "" && ctx != nil {
		protocol, _ = ctx.Value(experimentDeferredProtocolKey{}).(string)
	}
	if protocol == "" || !globalAIExperimentBill(account, group) {
		return nil
	}
	if c != nil && !c.Writer.Written() {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{
			"type": "EXPERIMENT_PROTOCOL_UNSUPPORTED", "code": "EXPERIMENT_PROTOCOL_UNSUPPORTED",
			"message": ErrExperimentProtocolUnsupported.Error(),
		}})
	}
	return ErrExperimentProtocolUnsupported
}

// IsExperimentAccountingError distinguishes local accounting admission failures
// from supplier/network faults so scheduling does not retry or evict an account.
func IsExperimentAccountingError(err error) bool {
	return errors.Is(err, ErrExperimentAlreadySent) || errors.Is(err, ErrExperimentRequestInFlight) ||
		errors.Is(err, ErrExperimentQuotaExhausted) || errors.Is(err, ErrExperimentAccountingUnavailable) ||
		errors.Is(err, ErrExperimentProtocolUnsupported)
}

func experimentAPIKeyGroup(c *gin.Context) *Group {
	if c == nil {
		return nil
	}
	value, _ := c.Get("api_key")
	key, _ := value.(*APIKey)
	return apiKeyGroup(key)
}

func experimentContextGroup(ctx context.Context) *Group {
	if ctx == nil {
		return nil
	}
	group, _ := ctx.Value(ctxkey.Group).(*Group)
	return group
}
