package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/util/logredact"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const grokCredentialObservationKey = "grok_request_credential_observation"

type grokCredentialObservationContextKey struct{}

// Only facts about credential acquisition belong here, never credential values.
type grokCredentialObservation struct {
	AccountID     int64      `json:"account_id"`
	Result        string     `json:"result"`
	Source        string     `json:"source"`
	RefreshResult string     `json:"refresh_result"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
}

type grokUpstreamDiagnostic struct {
	Version           int                        `json:"version"`
	Reason            string                     `json:"reason"`
	ProviderErrorCode string                     `json:"provider_error_code,omitempty"`
	ProviderErrorType string                     `json:"provider_error_type,omitempty"`
	ProviderMessage   string                     `json:"provider_message,omitempty"`
	BodyFormat        string                     `json:"body_format"`
	BodyBytes         int                        `json:"body_bytes"`
	Truncated         bool                       `json:"truncated,omitempty"`
	Model             string                     `json:"model,omitempty"`
	InboundEndpoint   string                     `json:"inbound_endpoint,omitempty"`
	UpstreamEndpoint  string                     `json:"upstream_endpoint,omitempty"`
	ResponseHeaders   []grokDiagnosticHeader     `json:"response_headers,omitempty"`
	Authentication    *grokCredentialObservation `json:"authentication,omitempty"`
}

// Names are values so the existing credential-key scrubber does not remove
// legitimate usage headers such as x-ratelimit-remaining-tokens.
type grokDiagnosticHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

var grokDiagnosticBearer = regexp.MustCompile(`(?i)\bBearer\s+[a-z0-9._~+/=-]{12,}`)
var grokDiagnosticSecret = regexp.MustCompile(`\b(?:sk-[A-Za-z0-9_-]{24,}|xai-[A-Za-z0-9_-]{24,}|eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+)`)

func grokDiagnosticText(value string, limit int, account *Account) string {
	// Redact before truncation: a cut credential prefix is still sensitive.
	if len(value) > 32*1024 {
		return "[omitted: oversized error field]"
	}
	value = strings.TrimSpace(value)
	if account != nil {
		for _, key := range []string{"api_key", "access_token", "refresh_token", "id_token", "client_secret"} {
			if secret := account.GetCredential(key); secret != "" {
				value = strings.ReplaceAll(value, secret, "[REDACTED]")
			}
		}
	}
	value = grokDiagnosticBearer.ReplaceAllString(value, "Bearer [REDACTED]")
	value = grokDiagnosticSecret.ReplaceAllString(value, "[REDACTED]")
	return truncateString(logredact.RedactText(value, "authorization", "api_key", "apikey", "token", "cookie", "set-cookie", "prompt", "input", "messages"), limit)
}

func grokDiagnosticURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	u.ForceQuery = false
	return truncateString(u.String(), 512)
}

// Explicit allowlist: neither cookies nor authorization/header overrides enter storage.
func grokDiagnosticHeaders(headers http.Header, account *Account) []grokDiagnosticHeader {
	var result []grokDiagnosticHeader
	for _, name := range []string{"content-type", "retry-after", "www-authenticate", "x-request-id", "xai-request-id", "cf-ray", "x-ratelimit-limit-requests", "x-ratelimit-remaining-requests", "x-ratelimit-reset-requests", "x-ratelimit-limit-tokens", "x-ratelimit-remaining-tokens", "x-ratelimit-reset-tokens", "x-rate-limit-limit-requests", "x-rate-limit-remaining-requests", "x-rate-limit-reset-requests", "x-rate-limit-limit-tokens", "x-rate-limit-remaining-tokens", "x-rate-limit-reset-tokens", "x-subscription-tier", "xai-subscription-tier", "x-xai-user-tier", "x-entitlement-status", "xai-entitlement-status", "x-xai-entitlement-status"} {
		if value := headers.Get(name); value != "" {
			result = append(result, grokDiagnosticHeader{Name: name, Value: grokDiagnosticText(value, 256, account)})
		}
	}
	return result
}

// Capture a bounded error projection, never JSON input/output, HTML, or request bodies.
// Unknown 403s stay unknown: status is not proof of subscription loss or suspension.
func buildGrokUpstreamDiagnostic(c *gin.Context, account *Account, resp *http.Response, body []byte, model string) grokUpstreamDiagnostic {
	d := grokUpstreamDiagnostic{Version: 1, Reason: "unknown", BodyFormat: "empty", BodyBytes: len(body), Model: grokDiagnosticText(model, 128, account)}
	if c != nil {
		if c.Request != nil {
			d.InboundEndpoint = grokDiagnosticText(c.Request.URL.Path, 256, account)
		}
		if value, ok := c.Get(grokCredentialObservationKey); ok {
			if observation, ok := value.(*grokCredentialObservation); ok && observation.AccountID == account.ID {
				snapshot := *observation
				d.Authentication = &snapshot
			}
		}
	}
	if resp != nil {
		d.ResponseHeaders = grokDiagnosticHeaders(resp.Header, account)
		if resp.Request != nil && resp.Request.URL != nil {
			d.UpstreamEndpoint = grokDiagnosticURL(resp.Request.URL.String())
		}
	}
	raw := ""
	if len(body) <= 32*1024 {
		raw = strings.TrimSpace(string(body))
	}
	switch {
	case len(body) > 32*1024:
		d.BodyFormat = "oversized"
		d.Truncated = true
	case raw == "":
	case json.Valid(body):
		d.BodyFormat = "json"
		for _, prefix := range []string{"error.", "response.error.", ""} {
			if d.ProviderErrorCode == "" {
				value := gjson.GetBytes(body, prefix+"code")
				if value.Type == gjson.String || value.Type == gjson.Number {
					d.ProviderErrorCode = grokDiagnosticText(value.String(), 128, account)
				}
			}
			if d.ProviderErrorType == "" {
				value := gjson.GetBytes(body, prefix+"type")
				if value.Type == gjson.String {
					d.ProviderErrorType = grokDiagnosticText(value.String(), 128, account)
				}
			}
			if d.ProviderMessage == "" {
				value := gjson.GetBytes(body, prefix+"message")
				if value.Type == gjson.String {
					d.ProviderMessage = grokDiagnosticText(value.String(), 1024, account)
				}
			}
		}
		if d.ProviderMessage == "" {
			if value := gjson.GetBytes(body, "error"); value.Type == gjson.String {
				d.ProviderMessage = grokDiagnosticText(value.String(), 1024, account)
			}
		}
	case strings.HasPrefix(raw, "<"):
		d.BodyFormat = "html" // Do not retain HTML, which can include reflected input and session challenges.
	case strings.HasPrefix(raw, "{") || strings.HasPrefix(raw, "["):
		d.BodyFormat = "invalid_json" // Do not turn malformed/truncated envelopes into raw text capture.
	default:
		d.BodyFormat = "text"
		d.ProviderMessage = grokDiagnosticText(raw, 1024, account)
		d.Truncated = len(raw) > 1024
	}
	// Classify only the retained error fields, not echoed input/output in an envelope.
	projection, _ := json.Marshal(map[string]any{"error": map[string]string{"code": d.ProviderErrorCode, "type": d.ProviderErrorType, "message": d.ProviderMessage}})
	status := 0
	if resp != nil {
		status = resp.StatusCode
	}
	switch {
	case isGrokContentPolicyRejection(status, projection):
		d.Reason = "content_policy"
	case grokAccountAccessMessage(d.ProviderMessage) || isGrokAccountAccessCode(d.ProviderErrorCode):
		d.Reason = "entitlement"
	default:
		decision := classifyGrokUpstreamFailure(status, projection, model)
		if decision.Class != "" {
			d.Reason = string(decision.Class)
		}
	}
	return d
}

func appendGrokUpstreamError(c *gin.Context, account *Account, resp *http.Response, body []byte, model string, event OpsUpstreamErrorEvent) {
	if account == nil || !account.IsGrok() {
		appendOpsUpstreamError(c, event)
		return
	}
	diagnostic := buildGrokUpstreamDiagnostic(c, account, resp, body, model)
	detail, err := json.Marshal(diagnostic)
	// Enforce the encoded budget as well: JSON escaping can expand sixfold.
	if len(detail) > 8192 {
		diagnostic.Truncated = true
		diagnostic.ResponseHeaders = nil
		diagnostic.ProviderMessage = truncateString(diagnostic.ProviderMessage, 256)
		detail, err = json.Marshal(diagnostic)
	}
	if len(detail) > 8192 {
		diagnostic.ProviderMessage = ""
		diagnostic.InboundEndpoint = ""
		diagnostic.UpstreamEndpoint = ""
		detail, err = json.Marshal(diagnostic)
	}
	if err != nil {
		detail = []byte(`{"version":1,"reason":"unknown","capture_error":"encoding_failed"}`)
	}
	event.Detail = string(detail)
	event.UpstreamURL = diagnostic.UpstreamEndpoint
	if diagnostic.ProviderMessage != "" {
		event.Message = diagnostic.ProviderMessage
	} else {
		event.Message = fmt.Sprintf("xAI upstream returned status %d", event.UpstreamStatusCode)
	}
	headers := http.Header{}
	if resp != nil {
		headers = resp.Header
	}
	event.UpstreamRequestID = grokDiagnosticText(firstNonEmpty(event.UpstreamRequestID, headers.Get("x-request-id"), headers.Get("xai-request-id")), 128, account)
	event.UpstreamResponseBody = "" // Existing opt-in raw logging must not bypass this error-only projection.
	setOpsUpstreamError(c, event.UpstreamStatusCode, event.Message, event.Detail)
	appendOpsUpstreamError(c, event)
}

func grokCredentialObservationFromContext(ctx context.Context) *grokCredentialObservation {
	if ctx == nil {
		return nil
	}
	observation, _ := ctx.Value(grokCredentialObservationContextKey{}).(*grokCredentialObservation)
	return observation
}
