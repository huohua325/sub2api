package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func TestUnifiedCodingProtocolGuard(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		method, path string
		allowed      bool
	}{
		{"GET", "/v1/models", true}, {"GET", "/models", true}, {"GET", "/v1/usage", true}, {"GET", "/v1/sub2api/billing", true},
		{"POST", "/v1/responses", true}, {"POST", "/responses/compact", true}, {"POST", "/backend-api/codex/responses/input_tokens", true},
		{"GET", "/v1/responses", false}, {"POST", "/v1/responses/arbitrary", false}, {"POST", "/v1/live", false},
		{"POST", "/v1/messages", false}, {"POST", "/v1/chat/completions", false}, {"POST", "/v1/images/generations", false},
		{"POST", "/v1beta/models/gemini:generateContent", false}, {"POST", "/antigravity/v1/responses", false},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(tc.method, tc.path, nil)
			key := &service.APIKey{Group: &service.Group{Platform: service.PlatformComposite, UsageKind: service.UsageKindCoding}}
			blocked := abortIfUnifiedCodingProtocolUnsupported(c, key)
			if blocked == tc.allowed || (blocked && rec.Code != http.StatusBadRequest) {
				t.Fatalf("allowed=%v blocked=%v status=%d", tc.allowed, blocked, rec.Code)
			}
			key.Group.UsageKind = service.UsageKindExperiment
			if abortIfUnifiedCodingProtocolUnsupported(c, key) {
				t.Fatal("existing experiment groups must retain protocols")
			}
		})
	}
}
