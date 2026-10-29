package service

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// readOpenAINonStreamingBody buffers SSE through its terminal frame rather than
// waiting for EOF. OAuth upstreams always stream, even for non-stream clients;
// an idle or broken connection after completion must not discard response usage.
func (s *OpenAIGatewayService) readOpenAINonStreamingBody(resp *http.Response, c *gin.Context, account *Account) ([]byte, error) {
	if !isEventStreamResponse(resp.Header) && (account == nil || account.Type != AccountTypeOAuth) {
		return ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	}
	if resp.Body == nil {
		return nil, errors.New("response body is nil")
	}
	limit := resolveUpstreamResponseReadLimit(s.cfg)
	tooLarge := func() ([]byte, error) {
		setOpsUpstreamError(c, http.StatusBadGateway, "upstream response too large", "")
		openAITooLargeError(c)
		return nil, fmt.Errorf("%w: limit=%d", ErrUpstreamResponseBodyTooLarge, limit)
	}
	scanner := bufio.NewScanner(io.LimitReader(resp.Body, limit+1))
	scanner.Split(scanSSELinesPreservingEndings)
	scanner.Buffer(make([]byte, 4096), int(limit+1))
	var body bytes.Buffer
	var parser openAICompatSSEFrameParser
	terminal := func(frame openAICompatSSEFrame) bool {
		data := strings.TrimSpace(frame.Data)
		if data == "[DONE]" {
			return true
		}
		if !gjson.Valid(data) {
			return false
		}
		eventType := effectiveOpenAISSEEventType([]byte(data), frame.EventType)
		// A bare error may be followed by a recovered completed response or a
		// more specific response.failed event. Only response terminals are final.
		return eventType != "error" && openAIStreamEventIsTerminalWithType(data, eventType)
	}
	for scanner.Scan() {
		line := scanner.Bytes()
		if int64(body.Len())+int64(len(line)) > limit {
			return tooLarge()
		}
		_, _ = body.Write(line) // bytes.Buffer.Write never returns an error.
		if frame, ok := parser.AddLine(string(trimSSELineEnding(line))); ok && terminal(frame) {
			return body.Bytes(), nil
		}
	}
	// Some upstreams omit the final blank line. A complete terminal payload is
	// still authoritative, including when the transport reports a trailing error.
	if frame, ok := parser.Finish(); ok && terminal(frame) {
		return body.Bytes(), nil
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return tooLarge()
		}
		return nil, err
	}
	return body.Bytes(), nil
}
