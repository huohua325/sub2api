//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// A bare 403 must not be presented as proven entitlement or quota exhaustion.
// Dropping evidence at a Grok forwarding or ops queue boundary breaks these tests.
func TestGrokDiagnosticMediaPreservesSafeEvidenceThroughOps(t *testing.T) {
	cases := []struct{ name, body, wantReason, wantFormat, wantCode string }{
		{"entitlement", `{"error":{"code":"subscription_required","type":"permission_error","message":"subscription required Bearer sensitive-bearer-value"},"access_token":"access-secret","prompt":"private-conversation","request":{"input":"private-input"}}`, "entitlement", "json", "subscription_required"},
		{"empty", "", "unknown", "empty", ""},
		{"plain", "Access denied access_token=access-secret api_key=api-secret", "unknown", "text", ""},
		{"html", `<html><title>Just a moment...</title><body>private-conversation cf_chl</body></html>`, "unknown", "html", ""},
		{"oversized", strings.Repeat("private-conversation", 10000), "unknown", "oversized", ""},
		{"invalid_json", `{"error":{"message":"private-conversation"`, "unknown", "invalid_json", ""},
		{"object_code", `{"error":{"code":{"text":"private-conversation"}}}`, "unknown", "json", ""},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
			account := &Account{ID: 14, Platform: PlatformGrok, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "api-secret", "access_token": "access-secret"}}
			request := httptest.NewRequest(http.MethodPost, "https://api.x.ai/v1/images/generations?api_key=api-secret", nil)
			resp := &http.Response{StatusCode: 403, Request: request, Header: http.Header{
				"Xai-Request-Id": {"xai-evidence-123"}, "Retry-After": {"5"}, "Cf-Ray": {"ray-123"},
				"Www-Authenticate": {`Bearer error="insufficient_scope"`}, "Set-Cookie": {"session=private-cookie"}, "Authorization": {"Bearer sensitive-bearer-value"},
			}, Body: io.NopCloser(strings.NewReader(tt.body))}
			svc := &OpenAIGatewayService{}
			_, err := svc.handleGrokMediaErrorResponse(context.Background(), resp, c, account, "xai-evidence-123", "grok-imagine-image")
			require.Error(t, err)
			raw, ok := c.Get(OpsUpstreamErrorsKey)
			require.True(t, ok)
			events := raw.([]*OpsUpstreamErrorEvent)
			require.Len(t, events, 1)
			event := events[0]
			require.Equal(t, "xai-evidence-123", event.UpstreamRequestID)
			require.Equal(t, "https://api.x.ai/v1/images/generations", event.UpstreamURL)
			require.True(t, json.Valid([]byte(event.Detail)), event.Detail)
			require.Equal(t, tt.wantReason, gjson.Get(event.Detail, "reason").String())
			require.Equal(t, tt.wantFormat, gjson.Get(event.Detail, "body_format").String())
			require.Equal(t, tt.wantCode, gjson.Get(event.Detail, "provider_error_code").String())
			require.Equal(t, "5", gjson.Get(event.Detail, `response_headers.#(name=="retry-after").value`).String())
			require.Equal(t, "grok-imagine-image", gjson.Get(event.Detail, "model").String())
			require.LessOrEqual(t, len(event.Detail), 8192)
			var stored *OpsInsertErrorLogInput
			ops := NewOpsService(&opsRepoMock{InsertErrorLogFn: func(_ context.Context, input *OpsInsertErrorLogInput) (int64, error) { stored = input; return 1, nil }}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
			require.NoError(t, ops.RecordError(context.Background(), &OpsInsertErrorLogInput{ErrorPhase: "upstream", UpstreamErrors: events}))
			require.NotNil(t, stored)
			require.NotNil(t, stored.UpstreamErrorsJSON)
			require.Contains(t, *stored.UpstreamErrorsJSON, "xai-evidence-123")
			for _, secret := range []string{"sensitive-bearer-value", "access-secret", "api-secret", "private-cookie", "private-conversation", "private-input"} {
				require.NotContains(t, *stored.UpstreamErrorsJSON, secret)
			}
		})
	}
}

func TestGrokDiagnosticResponsesCapturedWithoutRawLoggingFlag(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	account := &Account{ID: 14, Platform: PlatformGrok, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "private-api-key"}}
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 403, Header: http.Header{"Xai-Request-Id": {"response-attempt"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"entitlement_required","message":"entitlement required"}}`))}}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	_, err := svc.forwardGrokResponses(context.Background(), c, account, []byte(`{"model":"grok-4.7","input":"private-conversation","stream":true}`), "grok-4.7", true, time.Now())
	require.Error(t, err)
	events, ok := c.Get(OpsUpstreamErrorsKey)
	require.True(t, ok)
	attempts := events.([]*OpsUpstreamErrorEvent)
	require.Len(t, attempts, 1)
	require.Equal(t, "entitlement_required", gjson.Get(attempts[0].Detail, "provider_error_code").String())
	require.Equal(t, "grok-4.7", gjson.Get(attempts[0].Detail, "model").String())
	require.NotContains(t, attempts[0].Detail, "private-conversation")
}

func TestGrokDiagnosticStreamFailureKeepsSemanticError(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	svc := &OpenAIGatewayService{}
	account := &Account{ID: 14, Platform: PlatformGrok, Type: AccountTypeOAuth}
	svc.recordOpenAIStreamUpstreamError(c, account, false, "stream-request", "stream_failed", []byte(`{"type":"response.failed","response":{"error":{"code":"subscription_required","message":"subscription required"},"output":[{"text":"private-output"}]}}`), "subscription required")
	raw, _ := c.Get(OpsUpstreamErrorsKey)
	events := raw.([]*OpsUpstreamErrorEvent)
	require.Len(t, events, 1)
	require.Equal(t, "subscription_required", gjson.Get(events[0].Detail, "provider_error_code").String())
	require.Equal(t, "entitlement", gjson.Get(events[0].Detail, "reason").String())
	require.NotContains(t, events[0].Detail, "private-output")
}

func TestGrokDiagnosticCredentialRefreshObservation(t *testing.T) {
	for _, refresh := range []bool{false, true} {
		t.Run(map[bool]string{false: "valid_cached", true: "refreshed"}[refresh], func(t *testing.T) {
			account := healthyGrokOAuthGatewayTestAccount(94014, "old-access-secret")
			if refresh {
				account.Credentials["expires_at"] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
			}
			repo := &tokenRefreshAccountRepo{accountsByID: map[int64]*Account{account.ID: account}}
			cache := &grokTokenCacheForProviderTest{token: "old-access-secret", lockResult: true}
			provider := NewGrokTokenProvider(repo, cache)
			provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, cache), &tokenRefresherStub{credentials: map[string]any{"access_token": "new-access-secret", "refresh_token": "refresh-secret", "expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}})
			svc := &OpenAIGatewayService{grokTokenProvider: provider}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			_, _, err := svc.getRequestCredential(context.Background(), c, account)
			require.NoError(t, err)
			observation, ok := c.Get("grok_request_credential_observation")
			require.True(t, ok)
			encoded, err := json.Marshal(observation)
			require.NoError(t, err)
			require.Equal(t, "success", gjson.GetBytes(encoded, "result").String())
			want := "not_needed"
			if refresh {
				want = "refreshed"
			}
			require.Equal(t, want, gjson.GetBytes(encoded, "refresh_result").String())
			require.NotContains(t, string(encoded), "access-secret")
			require.NotContains(t, string(encoded), "refresh-secret")
		})
	}
}

func TestGrokDiagnosticBoundsEncodedPayloadAndRedactsBeforeTruncating(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	account := &Account{ID: 14, Platform: PlatformGrok, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": strings.Repeat("private-key-value", 100)}}
	secret := account.GetCredential("api_key")
	resp := &http.Response{StatusCode: 403, Header: http.Header{}}
	for _, header := range []string{"Content-Type", "Retry-After", "Www-Authenticate", "X-Request-Id", "Xai-Request-Id", "Cf-Ray", "X-Ratelimit-Limit-Requests", "X-Ratelimit-Remaining-Requests", "X-Ratelimit-Reset-Requests"} {
		resp.Header.Set(header, strings.Repeat("<", 1000))
	}
	body, _ := json.Marshal(map[string]any{"error": map[string]string{"message": secret + strings.Repeat("<", 1500)}})
	appendGrokUpstreamError(c, account, resp, body, "grok-4.7", OpsUpstreamErrorEvent{Platform: PlatformGrok, AccountID: 14, UpstreamStatusCode: 403, Message: secret})
	raw, _ := c.Get(OpsUpstreamErrorsKey)
	event := raw.([]*OpsUpstreamErrorEvent)[0]
	require.LessOrEqual(t, len(event.Detail), 8192)
	require.True(t, json.Valid([]byte(event.Detail)))
	require.NotContains(t, event.Message, "private-key-value")
	require.NotContains(t, event.Detail, "private-key-value")
}

func TestGrokDiagnosticAuthAndUsageHeadersSurviveQueueSanitization(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	account := &Account{ID: 14, Platform: PlatformGrok, Type: AccountTypeOAuth}
	c.Set("grok_request_credential_observation", &grokCredentialObservation{AccountID: 14, Result: "success", RefreshResult: "refreshed"})
	resp := &http.Response{StatusCode: 403, Header: http.Header{"X-Ratelimit-Remaining-Tokens": {"150019"}}}
	appendGrokUpstreamError(c, account, resp, []byte(`{"error":{"message":"forbidden"}}`), "grok-4.7", OpsUpstreamErrorEvent{Platform: PlatformGrok, AccountID: 14, UpstreamStatusCode: 403})
	raw, _ := c.Get(OpsUpstreamErrorsKey)
	entry := &OpsInsertErrorLogInput{UpstreamErrors: raw.([]*OpsUpstreamErrorEvent)}
	require.NoError(t, SanitizeOpsUpstreamErrorsForQueue(entry))
	var stored []OpsUpstreamErrorEvent
	require.NoError(t, json.Unmarshal([]byte(*entry.UpstreamErrorsJSON), &stored))
	require.Contains(t, stored[0].Detail, "150019")
	require.Contains(t, stored[0].Detail, "refreshed")
}

func TestGrokDiagnosticRefreshFailureStoredWithoutCredential(t *testing.T) {
	account := expiredGrokOAuthAccountForCredentialTest(94015)
	provider := NewGrokTokenProvider(nil, nil)
	svc := &OpenAIGatewayService{grokTokenProvider: provider}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	_, _, err := svc.getRequestCredential(context.Background(), c, account)
	require.Error(t, err)
	raw, ok := c.Get(OpsUpstreamErrorsKey)
	require.True(t, ok)
	events := raw.([]*OpsUpstreamErrorEvent)
	require.Len(t, events, 1)
	require.Equal(t, "failed", gjson.Get(events[0].Detail, "authentication.result").String())
	require.Equal(t, "unavailable", gjson.Get(events[0].Detail, "authentication.refresh_result").String())
	require.NotContains(t, events[0].Detail, account.GetGrokAccessToken())
}
