package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/tidwall/gjson"
)

func prepareAPIFallbackRequest(p *APIFallbackPolicy, raw []byte, allowEncrypted bool) ([]byte, int64, int64, error) {
	if p == nil || !p.CapabilitiesVerified || p.MaxInputTokens <= 0 || p.MaxInputTokens > 196608 || p.MaxOutputTokens <= 0 {
		return nil, 0, 0, errors.New("standby capabilities or bounds are not verified")
	}
	if err := rejectFallbackDuplicateKeys(raw); err != nil {
		return nil, 0, 0, err
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil || body == nil {
		return nil, 0, 0, errors.New("invalid Responses request")
	}
	allowed := strings.Fields("model instructions input tools tool_choice parallel_tool_calls reasoning max_output_tokens stream store include metadata text service_tier user safety_identifier prompt_cache_key prompt_cache_retention temperature top_p top_logprobs stream_options previous_response_id conversation context_management background truncation")
	for key := range body {
		if !fallbackContainsString(allowed, key) {
			return nil, 0, 0, fmt.Errorf("standby request field %s has no verified capability", key)
		}
	}
	if model := gjson.GetBytes(raw, "model"); model.Type != gjson.String || model.String() != p.Model {
		return nil, 0, 0, errors.New("standby request model does not match its approved price card")
	}
	for _, key := range []string{"previous_response_id", "conversation", "context_management", "background", "truncation"} {
		v := bytes.TrimSpace(body[key])
		if len(v) > 0 && !bytes.Equal(v, []byte("null")) && !bytes.Equal(v, []byte("false")) && !bytes.Equal(v, []byte(`"disabled"`)) {
			return nil, 0, 0, fmt.Errorf("standby cannot migrate %s; start a new conversation", key)
		}
	}
	if tier := gjson.GetBytes(raw, "service_tier"); tier.Exists() && tier.Type != gjson.Null && (tier.Type != gjson.String || tier.String() != "default") {
		return nil, 0, 0, errors.New("standby supports the approved standard service tier only")
	}
	if retention := gjson.GetBytes(raw, "prompt_cache_retention"); retention.Exists() && (retention.Type != gjson.String || retention.String() != "in_memory") {
		return nil, 0, 0, errors.New("standby cache retention has no approved price")
	}
	for _, key := range []string{"stream", "store", "parallel_tool_calls"} {
		if value := gjson.GetBytes(raw, key); value.Exists() && value.Type != gjson.True && value.Type != gjson.False {
			return nil, 0, 0, fmt.Errorf("invalid standby %s", key)
		}
	}
	for _, key := range []string{"instructions", "user", "safety_identifier", "prompt_cache_key"} {
		if value := gjson.GetBytes(raw, key); value.Exists() && value.Type != gjson.Null && value.Type != gjson.String {
			return nil, 0, 0, fmt.Errorf("invalid standby %s", key)
		}
	}
	if text := body["text"]; len(text) > 0 && string(text) != "null" {
		var values map[string]any
		if err := json.Unmarshal(text, &values); err != nil || values == nil {
			return nil, 0, 0, errors.New("invalid standby text options")
		}
		if err := fallbackObjectKeys(values, "format", "verbosity"); err != nil {
			return nil, 0, 0, err
		}
		if verbosity, exists := values["verbosity"]; exists && verbosity != "low" && verbosity != "medium" && verbosity != "high" {
			return nil, 0, 0, errors.New("invalid standby text verbosity")
		}
		if format, exists := values["format"]; exists {
			config, ok := format.(map[string]any)
			if !ok {
				return nil, 0, 0, errors.New("invalid standby text format")
			}
			if err := fallbackObjectKeys(config, "type", "name", "schema", "strict", "description"); err != nil {
				return nil, 0, 0, err
			}
			if config["type"] != "text" && config["type"] != "json_object" && config["type"] != "json_schema" {
				return nil, 0, 0, errors.New("standby text format has no verified capability")
			}
		}
	}
	if options := gjson.GetBytes(raw, "stream_options"); options.Exists() {
		var values map[string]any
		if err := json.Unmarshal([]byte(options.Raw), &values); err != nil || values == nil {
			return nil, 0, 0, errors.New("invalid standby stream options")
		}
		if err := fallbackObjectKeys(values, "include_obfuscation", "include_usage"); err != nil {
			return nil, 0, 0, err
		}
		for _, value := range values {
			if _, ok := value.(bool); !ok {
				return nil, 0, 0, errors.New("invalid standby stream option")
			}
		}
	}
	if included := gjson.GetBytes(raw, "include"); included.Exists() {
		var values []string
		if err := json.Unmarshal([]byte(included.Raw), &values); err != nil {
			return nil, 0, 0, errors.New("invalid standby response includes")
		}
		for _, value := range values {
			if value != "reasoning.encrypted_content" && value != "message.output_text.logprobs" {
				return nil, 0, 0, errors.New("standby response include has no verified capability")
			}
		}
	}
	if reasoning := body["reasoning"]; len(reasoning) > 0 && string(reasoning) != "null" {
		var values map[string]any
		if err := json.Unmarshal(reasoning, &values); err != nil || values == nil {
			return nil, 0, 0, errors.New("invalid standby reasoning options")
		}
		if err := fallbackObjectKeys(values, "effort", "summary"); err != nil {
			return nil, 0, 0, err
		}
		for key, value := range values {
			if _, ok := value.(string); !ok {
				return nil, 0, 0, fmt.Errorf("invalid standby reasoning %s", key)
			}
		}
	}
	var input any
	if err := json.Unmarshal(body["input"], &input); err != nil {
		return nil, 0, 0, errors.New("standby requires complete text input")
	}
	if err := validateFallbackInput(input, allowEncrypted); err != nil {
		return nil, 0, 0, err
	}
	if value := body["tools"]; len(value) > 0 && string(value) != "null" {
		var tools []map[string]any
		if err := json.Unmarshal(value, &tools); err != nil {
			return nil, 0, 0, errors.New("invalid standby tools")
		}
		if err := validateFallbackTools(tools, 0); err != nil {
			return nil, 0, 0, err
		}
	}
	if choice := body["tool_choice"]; len(choice) > 0 && string(choice) != "null" {
		var value any
		if err := json.Unmarshal(choice, &value); err != nil {
			return nil, 0, 0, errors.New("invalid standby tool choice")
		}
		switch selected := value.(type) {
		case string:
			if selected != "auto" && selected != "none" && selected != "required" {
				return nil, 0, 0, errors.New("standby tool choice has no verified capability")
			}
		case map[string]any:
			if err := fallbackObjectKeys(selected, "type", "name", "namespace"); err != nil {
				return nil, 0, 0, err
			}
			if selected["type"] != "function" && selected["type"] != "custom" {
				return nil, 0, 0, errors.New("standby tool choice must name a local tool")
			}
		default:
			return nil, 0, 0, errors.New("invalid standby tool choice")
		}
	}
	maxOutput := p.MaxOutputTokens
	if client := gjson.GetBytes(raw, "max_output_tokens"); client.Exists() && client.Type != gjson.Null {
		if client.Type != gjson.Number || client.Int() <= 0 || client.Float() != float64(client.Int()) {
			return nil, 0, 0, errors.New("invalid max_output_tokens")
		}
		if client.Int() < maxOutput {
			maxOutput = client.Int()
		}
	}
	body["max_output_tokens"] = json.RawMessage(fmt.Sprint(maxOutput))
	body["service_tier"] = json.RawMessage(`"default"`)
	body["store"] = json.RawMessage("false")
	prepared, err := json.Marshal(body)
	if err != nil {
		return nil, 0, 0, err
	}
	var tree any
	if err := json.Unmarshal(prepared, &tree); err != nil {
		return nil, 0, 0, err
	}
	upperBound := int64(len(prepared))*2 + countFallbackObjects(tree)*128 + 8192
	if upperBound > p.MaxInputTokens {
		return nil, 0, 0, errors.New("request exceeds the approved standby input bound")
	}
	return prepared, upperBound, maxOutput, nil
}

func fallbackContainsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func fallbackObjectKeys(value map[string]any, allowed ...string) error {
	for key := range value {
		if !fallbackContainsString(allowed, key) {
			return fmt.Errorf("standby field %s has no verified capability", key)
		}
	}
	return nil
}

func validateFallbackInput(value any, allowEncrypted bool) error {
	if _, ok := value.(string); ok {
		return nil
	}
	items, ok := value.([]any)
	if !ok {
		return errors.New("standby requires complete text input")
	}
	calls := make(map[string]string)
	outputs := make(map[string]bool)
	for _, item := range items {
		node, ok := item.(map[string]any)
		if !ok {
			return errors.New("invalid standby input item")
		}
		typ, _ := node["type"].(string)
		if value, exists := node["type"]; exists && value != typ {
			return errors.New("invalid standby input item type")
		}
		switch typ {
		case "", "message":
			if err := fallbackObjectKeys(node, "type", "id", "role", "content", "status", "phase"); err != nil {
				return err
			}
			role, _ := node["role"].(string)
			if role != "user" && role != "assistant" && role != "system" && role != "developer" {
				return errors.New("invalid standby message role")
			}
			if err := validateFallbackText(node["content"], "input_text", "output_text", "refusal"); err != nil {
				return err
			}
		case "function_call", "custom_tool_call":
			if err := fallbackObjectKeys(node, "type", "id", "call_id", "name", "arguments", "input", "status", "namespace"); err != nil {
				return err
			}
			id, _ := node["call_id"].(string)
			name, _ := node["name"].(string)
			field := "arguments"
			if typ == "custom_tool_call" {
				field = "input"
			}
			if _, valid := node[field].(string); id == "" || name == "" || !valid || calls[id] != "" {
				return errors.New("standby requires a complete unique local tool call")
			}
			calls[id] = typ
		case "function_call_output", "custom_tool_call_output":
			if err := fallbackObjectKeys(node, "type", "id", "call_id", "output", "status"); err != nil {
				return err
			}
			id, _ := node["call_id"].(string)
			if id == "" || calls[id]+"_output" != typ || outputs[id] {
				return errors.New("standby tool output lacks complete call history; start a new conversation")
			}
			if err := validateFallbackText(node["output"], "input_text", "output_text"); err != nil {
				return err
			}
			outputs[id] = true
		case "reasoning":
			if err := fallbackObjectKeys(node, "type", "id", "summary", "content", "status", "encrypted_content"); err != nil {
				return err
			}
			if encrypted := node["encrypted_content"]; encrypted != nil {
				value, ok := encrypted.(string)
				if !ok {
					return errors.New("invalid standby encrypted reasoning")
				}
				if value != "" && !allowEncrypted {
					return errors.New("standby cannot migrate encrypted reasoning; start a new conversation")
				}
			}
			for _, field := range []string{"summary", "content"} {
				if value, exists := node[field]; exists && value != nil {
					if err := validateFallbackText(value, "summary_text", "reasoning_text"); err != nil {
						return err
					}
				}
			}
		default:
			return errors.New("standby input item has no verified text or local tool capability")
		}
	}
	for id := range calls {
		if !outputs[id] {
			return errors.New("standby tool call is missing its output; start a new conversation")
		}
	}
	return nil
}

func validateFallbackText(value any, allowed ...string) error {
	if _, ok := value.(string); ok {
		return nil
	}
	parts, ok := value.([]any)
	if !ok {
		return errors.New("standby supports self-contained text content only")
	}
	for _, part := range parts {
		node, ok := part.(map[string]any)
		if !ok {
			return errors.New("invalid standby text content")
		}
		typ, _ := node["type"].(string)
		if !fallbackContainsString(allowed, typ) {
			return errors.New("standby content type has no verified text capability")
		}
		if err := fallbackObjectKeys(node, "type", "text", "refusal", "annotations", "logprobs"); err != nil {
			return err
		}
		field := "text"
		if typ == "refusal" {
			field = "refusal"
		}
		if _, ok := node[field].(string); !ok {
			return errors.New("invalid standby text value")
		}
	}
	return nil
}

func validateFallbackTools(tools []map[string]any, depth int) error {
	if depth > 4 {
		return errors.New("standby tool namespaces are too deeply nested")
	}
	for _, tool := range tools {
		name, _ := tool["name"].(string)
		if name == "" {
			return errors.New("standby tools require explicit names")
		}
		switch tool["type"] {
		case "function":
			if err := fallbackObjectKeys(tool, "type", "name", "description", "parameters", "strict", "defer_loading"); err != nil {
				return err
			}
			if value, exists := tool["parameters"]; exists && value != nil {
				if _, ok := value.(map[string]any); !ok {
					return errors.New("invalid standby function schema")
				}
			}
		case "custom":
			if err := fallbackObjectKeys(tool, "type", "name", "description", "format", "defer_loading"); err != nil {
				return err
			}
			if value, exists := tool["format"]; exists && value != nil {
				format, ok := value.(map[string]any)
				if !ok {
					return errors.New("invalid standby custom tool format")
				}
				if err := fallbackObjectKeys(format, "type", "syntax", "definition"); err != nil {
					return err
				}
				if format["type"] != "text" && format["type"] != "grammar" {
					return errors.New("standby custom tool format has no verified capability")
				}
				if format["type"] == "grammar" {
					definition, ok := format["definition"].(string)
					if !ok || definition == "" || (format["syntax"] != "lark" && format["syntax"] != "regex") {
						return errors.New("invalid standby custom tool grammar")
					}
				}
			}
		case "namespace":
			if err := fallbackObjectKeys(tool, "type", "name", "description", "tools"); err != nil {
				return err
			}
			encoded, err := json.Marshal(tool["tools"])
			if err != nil {
				return err
			}
			var children []map[string]any
			if err := json.Unmarshal(encoded, &children); err != nil {
				return err
			}
			if err := validateFallbackTools(children, depth+1); err != nil {
				return err
			}
		default:
			return errors.New("standby tool has no approved price or capability")
		}
	}
	return nil
}

func countFallbackObjects(value any) int64 {
	count := int64(0)
	switch node := value.(type) {
	case map[string]any:
		count++
		for _, child := range node {
			count += countFallbackObjects(child)
		}
	case []any:
		for _, child := range node {
			count += countFallbackObjects(child)
		}
	}
	return count
}

// JSON parsers differ on first/last duplicate keys. Reject ambiguity before
// model, lineage and budget checks can disagree with the actual upstream body.
func rejectFallbackDuplicateKeys(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 128 {
			return errors.New("standby request is too deeply nested")
		}
		token, err := decoder.Token()
		if err != nil {
			return errors.New("invalid standby JSON")
		}
		if delim, ok := token.(json.Delim); ok {
			switch delim {
			case '{':
				seen := make(map[string]bool)
				for decoder.More() {
					key, err := decoder.Token()
					if err != nil {
						return errors.New("invalid standby JSON object")
					}
					name, ok := key.(string)
					if !ok || seen[name] {
						return errors.New("standby JSON contains duplicate fields")
					}
					seen[name] = true
					if err := walk(depth + 1); err != nil {
						return err
					}
				}
			case '[':
				for decoder.More() {
					if err := walk(depth + 1); err != nil {
						return err
					}
				}
			default:
				return errors.New("invalid standby JSON delimiter")
			}
			if _, err := decoder.Token(); err != nil {
				return errors.New("invalid standby JSON end")
			}
		}
		return nil
	}
	if err := walk(0); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("standby JSON must contain one request")
	}
	return nil
}
