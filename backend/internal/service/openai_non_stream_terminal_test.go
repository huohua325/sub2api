package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type nonStreamTerminalTailReader struct {
	reader    *strings.Reader
	tailReads int
}

func (r *nonStreamTerminalTailReader) Read(p []byte) (int, error) {
	if r.reader.Len() == 0 {
		r.tailReads++
		return 0, io.ErrUnexpectedEOF
	}
	return r.reader.Read(p)
}

func (r *nonStreamTerminalTailReader) Close() error { return nil }

func forwardNonStreamTerminalBody(t *testing.T, passthrough bool, contentType string, body io.ReadCloser, maxBytes int64) (*httptest.ResponseRecorder, *OpenAIForwardResult, error) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(nil))
	c.Request.Header.Set("Accept", "application/json")
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{contentType}}, Body: body}}
	cfg := &config.Config{}
	cfg.Gateway.UpstreamResponseReadMaxBytes = maxBytes
	svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream, responseHeaderFilter: compileResponseHeaderFilter(cfg)}
	account := &Account{ID: 7978, Name: "terminal-regression", Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "test-oauth-token", "chatgpt_account_id": "test-account"},
		Extra:       map[string]any{"openai_passthrough": passthrough}, Status: StatusActive, Schedulable: true, RateMultiplier: f64p(1)}
	result, err := svc.Forward(context.Background(), c, account, []byte(`{"model":"gpt-5.4","instructions":"x","input":"hi","stream":false}`))
	return rec, result, err
}

func TestOpenAIOAuthNonStreamStopsAtTerminalAndPreservesUsage(t *testing.T) {
	for _, passthrough := range []bool{true, false} {
		mode := "legacy"
		if passthrough {
			mode = "passthrough"
		}
		for _, contentType := range []string{"text/event-stream", ""} {
			for _, status := range []string{"completed", "incomplete", "cancelled"} {
				t.Run(mode+"/"+contentType+"/"+status, func(t *testing.T) {
					payload := strings.ReplaceAll(openAINonStreamRegressionSSE, "completed", status)
					reader := &nonStreamTerminalTailReader{reader: strings.NewReader(payload)}
					rec, result, err := forwardNonStreamTerminalBody(t, passthrough, contentType, reader, 0)
					require.NoError(t, err)
					require.NotNil(t, result)
					require.False(t, result.Stream)
					require.Equal(t, status, gjson.GetBytes(rec.Body.Bytes(), "status").String())
					require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
					require.Equal(t, 1, result.Usage.InputTokens)
					require.Equal(t, 1, result.Usage.OutputTokens)
					require.Zero(t, reader.tailReads, "a terminal response must not wait for EOF or read a broken/idle tail")
				})
			}
		}
	}
}

func TestOpenAIOAuthNonStreamKeepsReadErrorsBeforeTerminal(t *testing.T) {
	for _, passthrough := range []bool{true, false} {
		body := &nonStreamTerminalTailReader{reader: strings.NewReader("event: response.created\ndata: {\"type\":\"response.created\"}\n\n")}
		_, result, err := forwardNonStreamTerminalBody(t, passthrough, "text/event-stream", body, 0)
		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
		require.Nil(t, result)
	}
}

func TestOpenAIOAuthNonStreamBareErrorWaitsForAuthoritativeTerminal(t *testing.T) {
	const bareError = "event: error\ndata: {\"type\":\"error\",\"error\":{\"message\":\"transient\"}}\n\n"
	for _, status := range []string{"completed", "failed"} {
		t.Run(status, func(t *testing.T) {
			payload := bareError + strings.ReplaceAll(openAINonStreamRegressionSSE, "completed", status)
			reader := &nonStreamTerminalTailReader{reader: strings.NewReader(payload)}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			svc := &OpenAIGatewayService{cfg: &config.Config{}}
			body, err := svc.readOpenAINonStreamingBody(&http.Response{Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: reader}, c, &Account{Type: AccountTypeOAuth})
			require.NoError(t, err)
			require.Equal(t, payload, string(body), "a bare error must not hide a later authoritative response and usage")
			require.Zero(t, reader.tailReads)
		})
	}
	for _, passthrough := range []bool{true, false} {
		rec, result, err := forwardNonStreamTerminalBody(t, passthrough, "text/event-stream", io.NopCloser(strings.NewReader(bareError+openAINonStreamRegressionSSE)), 0)
		require.NoError(t, err)
		require.NotNil(t, result)
		requireSingleJSONResponsesObject(t, rec)
		require.Equal(t, 1, result.Usage.InputTokens)
		require.Equal(t, 1, result.Usage.OutputTokens)
	}
}

func TestOpenAIOAuthNonStreamStillEnforcesBodyLimit(t *testing.T) {
	for _, passthrough := range []bool{true, false} {
		rec, result, err := forwardNonStreamTerminalBody(t, passthrough, "text/event-stream", io.NopCloser(strings.NewReader(openAINonStreamRegressionSSE)), 48)
		require.ErrorIs(t, err, ErrUpstreamResponseBodyTooLarge)
		require.Nil(t, result)
		require.Equal(t, http.StatusBadGateway, rec.Code)
	}
}
