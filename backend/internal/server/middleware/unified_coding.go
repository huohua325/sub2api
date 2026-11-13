package middleware

import (
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// UnifiedCodingProtocolAllowed deliberately limits this first release to
// metered HTTP Responses. In particular, GET /responses must not open a
// WebSocket before the request's actual wallet is known.
func UnifiedCodingProtocolAllowed(method, path string) bool {
	if method == http.MethodGet {
		return path == "/v1/models" || path == "/models" || path == "/backend-api/codex/models" ||
			path == "/v1/usage" || path == "/v1/sub2api/billing"
	}
	if method != http.MethodPost {
		return false
	}
	for _, prefix := range []string{"/v1/responses", "/responses", "/backend-api/codex/responses"} {
		if path == prefix || path == prefix+"/compact" || path == prefix+"/input_tokens" {
			return true
		}
	}
	return false
}

func abortIfUnifiedCodingProtocolUnsupported(c *gin.Context, key *service.APIKey) bool {
	if key == nil || !service.IsUnifiedCodingGroup(key.Group) {
		return false
	}
	if UnifiedCodingProtocolAllowed(c.Request.Method, strings.TrimRight(c.Request.URL.Path, "/")) {
		return false
	}
	AbortWithError(c, http.StatusBadRequest, "UNIFIED_CODING_PROTOCOL_UNSUPPORTED", "Unified coding supports HTTP Responses only; disable WebSockets in your Codex provider configuration")
	return true
}
