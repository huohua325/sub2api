package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func fallbackRequestPolicy() *APIFallbackPolicy {
	return &APIFallbackPolicy{Model: "gpt-6.1-sol", CapabilitiesVerified: true, MaxInputTokens: 100000, MaxOutputTokens: 4096}
}

func TestAPIFallbackBodyPreservesCodexTextAndLocalToolHistory(t *testing.T) {
	raw := []byte(`{"model":"gpt-6.1-sol","instructions":"Complete the project task.","stream":true,"store":true,
 "reasoning":{"effort":"high","summary":"auto"},"include":["reasoning.encrypted_content"],"text":{"verbosity":"medium"},
 "input":[{"role":"user","content":[{"type":"input_text","text":"Read and patch the file."}]},
 {"type":"function_call","call_id":"read-1","name":"read_file","namespace":"functions","arguments":"{\"path\":\"app.go\"}"},
 {"type":"function_call_output","call_id":"read-1","output":"package main"},
 {"type":"custom_tool_call","call_id":"patch-1","name":"apply_patch","input":"*** Begin Patch\n*** End Patch"},
 {"type":"custom_tool_call_output","call_id":"patch-1","output":[{"type":"input_text","text":"Done"}]}],
 "tools":[{"type":"namespace","name":"functions","tools":[{"type":"function","name":"read_file","parameters":{"type":"object","properties":{"encrypted_content":{"type":"string"},"media_type":{"enum":["input_image"]}}}}]},
 {"type":"custom","name":"apply_patch","format":{"type":"grammar","syntax":"lark","definition":"start: /.+/"}}]}`)
	body, input, output, err := prepareAPIFallbackBody(fallbackRequestPolicy(), raw)
	require.NoError(t, err)
	require.Greater(t, input, int64(len(body)))
	require.Equal(t, int64(4096), output)
	require.Equal(t, int64(4096), gjson.GetBytes(body, "max_output_tokens").Int())
	require.Equal(t, "default", gjson.GetBytes(body, "service_tier").String())
	require.False(t, gjson.GetBytes(body, "store").Bool())
	var before, after any
	require.NoError(t, json.Unmarshal([]byte(gjson.GetBytes(raw, "input").Raw), &before))
	require.NoError(t, json.Unmarshal([]byte(gjson.GetBytes(body, "input").Raw), &after))
	require.Equal(t, before, after, "complete tool history must survive without dropping an item")
	require.True(t, gjson.GetBytes(body, "tools.0.tools.0.parameters.properties.encrypted_content").Exists(),
		"schema property names must not be mistaken for actual encrypted conversation data")
}

func TestAPIFallbackBodyEncryptedHistoryRequiresVerifiedLineage(t *testing.T) {
	raw := []byte(`{"model":"gpt-6.1-sol","input":[{"type":"reasoning","id":"rs_own","summary":[],"encrypted_content":"private-ciphertext"},{"role":"user","content":"Continue"}]}`)
	_, _, _, err := prepareAPIFallbackBody(fallbackRequestPolicy(), raw)
	require.ErrorContains(t, err, "encrypted reasoning")
	body, _, _, err := prepareAPIFallbackBodyWithLineage(fallbackRequestPolicy(), raw, true)
	require.NoError(t, err)
	require.Equal(t, "private-ciphertext", gjson.GetBytes(body, "input.0.encrypted_content").String())
	_, _, _, err = prepareAPIFallbackBodyWithLineage(fallbackRequestPolicy(), []byte(`{"model":"gpt-6.1-sol","input":[{"type":"reasoning","summary":[],"encrypted_content":{"cipher":"not-a-token"}}]}`), true)
	require.ErrorContains(t, err, "invalid standby encrypted reasoning")
}

func TestAPIFallbackBodyRejectsUnverifiedCapabilitiesBeforeSpending(t *testing.T) {
	for _, tc := range []struct{ name, extra string }{
		{"model mismatch", `"model":"other-model","input":"hello"`},
		{"duplicate model", `"model":"gpt-6.1-sol","model":"other-model","input":"hello"`},
		{"duplicate encrypted lineage", `"input":[{"type":"reasoning","encrypted_content":"first","encrypted_content":"other"}]`},
		{"remote prompt template", `"input":"hello","prompt":{"id":"server-template"}`},
		{"new unknown field", `"input":"hello","future_hosted_tool":true`},
		{"image input", `"input":[{"role":"user","content":[{"type":"input_image","image_url":"https://example.com/a.png"}]}]`},
		{"video input", `"input":[{"role":"user","content":[{"type":"input_video","video_url":"https://example.com/a.mp4"}]}]`},
		{"remote input item", `"input":[{"type":"item_reference","id":"stored-item"}]`},
		{"compaction input", `"input":[{"type":"compaction","encrypted_content":"foreign"}]`},
		{"native remote shell", `"input":[{"type":"shell_call","call_id":"remote","action":{"commands":["ls"]}}]`},
		{"hosted search", `"input":"hello","tools":[{"type":"web_search","name":"search"}]`},
		{"unknown function transport", `"input":"hello","tools":[{"type":"function","name":"search","url":"https://example.com"}]`},
		{"unknown nested namespace tool", `"input":"hello","tools":[{"type":"namespace","name":"tools","tools":[{"type":"mcp","name":"remote"}]}]`},
		{"server previous response", `"input":"hello","previous_response_id":"resp_other"`},
		{"server conversation", `"input":"hello","conversation":{"id":"conv_other"}`},
		{"server background", `"input":"hello","background":true`},
		{"automatic truncation", `"input":"hello","truncation":"auto"`},
		{"premium retention", `"input":"hello","prompt_cache_retention":"24h"`},
		{"premium tier", `"input":"hello","service_tier":"priority"`},
		{"orphan tool output", `"input":[{"type":"function_call_output","call_id":"missing","output":"done"}]`},
		{"missing tool output", `"input":[{"type":"function_call","call_id":"a","name":"read","arguments":"{}"}]`},
		{"wrong tool output kind", `"input":[{"type":"function_call","call_id":"a","name":"read","arguments":"{}"},{"type":"custom_tool_call_output","call_id":"a","output":"done"}]`},
		{"unknown output media", `"input":[{"type":"function_call","call_id":"a","name":"read","arguments":"{}"},{"type":"function_call_output","call_id":"a","output":[{"type":"input_image","image_url":"data:image/png;base64,AA=="}]}]`},
		{"negative output cap", `"input":"hello","max_output_tokens":-1`},
		{"fractional output cap", `"input":"hello","max_output_tokens":1.5`},
		{"unknown response include", `"input":"hello","include":["web_search_call.action.sources"]`},
		{"unknown reasoning mode", `"input":"hello","reasoning":{"mode":"pro"}`},
		{"unknown stream option", `"input":"hello","stream_options":{"remote_task":true}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := `{"model":"gpt-6.1-sol",` + tc.extra + `}`
			if tc.name == "model mismatch" || tc.name == "duplicate model" {
				raw = `{` + tc.extra + `}`
			}
			_, _, _, err := prepareAPIFallbackBody(fallbackRequestPolicy(), []byte(raw))
			require.Error(t, err)
		})
	}
}

func TestAPIFallbackBodyUsesServerBoundAndLowerClientLimit(t *testing.T) {
	p := fallbackRequestPolicy()
	body, _, output, err := prepareAPIFallbackBody(p, []byte(`{"model":"gpt-6.1-sol","input":"hello","max_output_tokens":128,"service_tier":null}`))
	require.NoError(t, err)
	require.Equal(t, int64(128), output)
	require.Equal(t, int64(128), gjson.GetBytes(body, "max_output_tokens").Int())
	_, _, output, err = prepareAPIFallbackBody(p, []byte(`{"model":"gpt-6.1-sol","input":"hello","max_output_tokens":99999}`))
	require.NoError(t, err)
	require.Equal(t, p.MaxOutputTokens, output)
	p.MaxInputTokens = 10000
	_, _, _, err = prepareAPIFallbackBody(p, []byte(`{"model":"gpt-6.1-sol","input":"`+strings.Repeat("界", 1000)+`"}`))
	require.ErrorContains(t, err, "input bound")
	p.CapabilitiesVerified = false
	_, _, _, err = prepareAPIFallbackBody(p, []byte(`{"model":"gpt-6.1-sol","input":"hello"}`))
	require.ErrorContains(t, err, "not verified")
}
