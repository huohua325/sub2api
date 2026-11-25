package developmentworkspace

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const (
	// T3 v0.1.11 provides a stateless streamable HTTP binding for this version.
	mcpProtocol     = "2026-07-28"
	maxResponseSize = 2 * 1024 * 1024
)

type values map[string]any

type toolResult struct {
	IsError    bool            `json:"isError"`
	Structured json.RawMessage `json:"structuredContent"`
	Content    []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

func (c *Client) call(ctx context.Context, name string, input values, output any) error {
	if c.http == nil {
		return ErrDisabled
	}
	payload, err := json.Marshal(values{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": values{
			"_meta": values{
				"io.modelcontextprotocol/protocolVersion":    mcpProtocol,
				"io.modelcontextprotocol/clientCapabilities": values{},
				"io.modelcontextprotocol/clientInfo":         values{"name": "mile-development-workspace", "version": "1.0.0"},
			},
			"name": name, "arguments": input,
		},
	})
	if err != nil {
		return fmt.Errorf("%w: encode %s", ErrInvalid, name)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.URL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("%w: invalid T3 request", ErrInvalid)
	}
	req.Header.Set("Authorization", "Bearer "+c.config.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Method", "tools/call")
	req.Header.Set("MCP-Name", name)
	req.Header.Set("MCP-Protocol-Version", mcpProtocol)
	resp, err := c.http.Do(req)
	if err != nil {
		// The configured endpoint and transport diagnostics may contain private data.
		return fmt.Errorf("%w: T3 transport for %s", ErrUnavailable, name)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: T3 returned HTTP %d for %s", ErrUnavailable, resp.StatusCode, name)
	}
	response, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize+1))
	if err != nil || len(response) > maxResponseSize {
		return fmt.Errorf("%w: invalid T3 response for %s", ErrUnavailable, name)
	}
	var rpc struct {
		Version string          `json:"jsonrpc"`
		ID      int             `json:"id"`
		Result  *toolResult     `json:"result"`
		Error   json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(response, &rpc); err != nil || rpc.Version != "2.0" || rpc.ID != 1 || rpc.Result == nil || rpc.Error != nil {
		return fmt.Errorf("%w: invalid T3 RPC result for %s", ErrUnavailable, name)
	}
	data := rpc.Result.Structured
	if len(data) == 0 && len(rpc.Result.Content) == 1 && rpc.Result.Content[0].Type == "text" {
		data = json.RawMessage(rpc.Result.Content[0].Text)
	}
	var failure struct {
		Tag   string `json:"_tag"`
		Code  string `json:"code"`
		Error *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &failure); err != nil || len(data) == 0 || data[0] != '{' {
		return fmt.Errorf("%w: malformed T3 tool response for %s", ErrUnavailable, name)
	}
	if rpc.Result.IsError || failure.Tag != "" || failure.Code != "" || failure.Error != nil {
		code := failure.Code
		if failure.Error != nil {
			code = failure.Error.Code
		}
		return mapToolError(name, code)
	}
	if output != nil {
		if err := json.Unmarshal(data, output); err != nil {
			return fmt.Errorf("%w: invalid T3 data for %s", ErrUnavailable, name)
		}
	}
	return nil
}

func mapToolError(name, code string) error {
	switch code {
	case "thread_not_found", "run_not_found":
		return ErrNotFound
	case "thread_not_sendable", "thread_not_interruptible":
		return ErrConflict
	case "invalid_request", "workspace_preparation_failed":
		return ErrInvalid
	default:
		// Do not reflect T3 error details, which can contain host paths or agent output.
		return fmt.Errorf("%w: T3 rejected %s", ErrUnavailable, name)
	}
}
