package joycode

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ChatToResponses converts an OpenAI chat-completions style body (model,
// messages, tools, ...) into the Responses API format that GPT-family models
// on the JoyCode platform require. Rules mirror the IDE's
// normalizeMessagesForResponsesAPI:
//   - system/developer messages are dropped; the first system text becomes "instructions"
//   - user text -> {role:user, content:[{type:input_text,text}]}
//   - assistant text -> {role:assistant, content:[{type:output_text,text}]}
//   - assistant tool_calls -> {type:function_call, call_id, name, arguments}
//   - tool role -> {type:function_call_output, call_id, output}
//   - tools flattened to Responses function schema
func ChatToResponses(chatBody map[string]interface{}) map[string]interface{} {
	body := map[string]interface{}{
		"model":  chatBody["model"],
		"stream": chatBody["stream"],
	}
	// Reasoning models burn tokens on reasoning before emitting any text; a
	// small max_tokens (e.g. 50) yields an empty response with
	// incomplete_details.reason=max_output_tokens. The IDE omits the param
	// entirely; do the same unless the caller asks for a comfortable budget.
	if mt, ok := chatBody["max_tokens"]; ok {
		switch v := mt.(type) {
		case int:
			if v >= 4096 {
				body["max_output_tokens"] = v
			}
		case float64:
			if v >= 4096 {
				body["max_output_tokens"] = int(v)
			}
		}
	}

	// Normalize messages to []interface{} regardless of the concrete Go type
	// the caller used ([]map[string]interface{} from buildMessages, or
	// []interface{} from raw JSON unmarshaling).
	var msgs []interface{}
	switch m := chatBody["messages"].(type) {
	case []interface{}:
		msgs = m
	case []map[string]interface{}:
		msgs = make([]interface{}, len(m))
		for i, v := range m {
			msgs[i] = v
		}
	default:
		if raw, ok := chatBody["messages"]; ok && raw != nil {
			if b, err := json.Marshal(raw); err == nil {
				json.Unmarshal(b, &msgs)
			}
		}
	}
	input := make([]interface{}, 0, len(msgs))
	instructions := ""
	for _, mi := range msgs {
		m, ok := mi.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := m["role"].(string)
		switch role {
		case "system", "developer":
			if instructions == "" {
				instructions = extractStringContent(m["content"])
			}
			continue
		case "tool":
			callID, _ := m["tool_call_id"].(string)
			output := extractStringContent(m["content"])
			if output == "" {
				output = " "
			}
			input = append(input, map[string]interface{}{
				"type":    "function_call_output",
				"call_id": TrimCallID(callID),
				"output":  output,
			})
			continue
		}

		text := extractStringContent(m["content"])
		switch role {
		case "user":
			input = append(input, map[string]interface{}{
				"role":    "user",
				"content": chatContentToParts(m["content"], "input_text"),
			})
		case "assistant":
			if strings.TrimSpace(text) != "" {
				input = append(input, map[string]interface{}{
					"role":    "assistant",
					"content": []map[string]interface{}{{"type": "output_text", "text": text}},
				})
			}
			if tcs, ok := m["tool_calls"].([]interface{}); ok {
				for _, tci := range tcs {
					tc, ok := tci.(map[string]interface{})
					if !ok {
						continue
					}
					fn, _ := tc["function"].(map[string]interface{})
					if fn == nil {
						continue
					}
					name, _ := fn["name"].(string)
					args, _ := fn["arguments"].(string)
					if args == "" || !json.Valid([]byte(args)) {
						args = "{}"
					}
					id, _ := tc["id"].(string)
					input = append(input, map[string]interface{}{
						"type":      "function_call",
						"call_id":   TrimCallID(id),
						"name":      name,
						"arguments": args,
					})
				}
			}
		default:
			input = append(input, map[string]interface{}{
				"role":    role,
				"content": chatContentToParts(m["content"], "input_text"),
			})
		}
	}
	body["input"] = input
	if instructions != "" {
		body["instructions"] = instructions
	}

	if tools, ok := chatBody["tools"].([]interface{}); ok {
		if rt := ConvertChatTools(tools); len(rt) > 0 {
			body["tools"] = rt
		}
	}
	if tc, ok := chatBody["tool_choice"]; ok {
		switch v := tc.(type) {
		case string:
			body["tool_choice"] = v
		case map[string]interface{}:
			if fn, ok := v["function"].(map[string]interface{}); ok {
				if name, ok := fn["name"].(string); ok {
					body["tool_choice"] = map[string]interface{}{"type": "function", "name": name}
				}
			}
		}
	}
	return body
}

// TrimCallID caps call IDs at 64 chars (upstream rejects longer IDs; the IDE
// keeps the tail).
func TrimCallID(id string) string {
	if len(id) <= 64 {
		return id
	}
	return id[len(id)-64:]
}

// ConvertChatTools converts OpenAI chat tools to Responses function schema.
func ConvertChatTools(tools []interface{}) []interface{} {
	out := make([]interface{}, 0, len(tools))
	for _, t := range tools {
		tm, ok := t.(map[string]interface{})
		if !ok {
			continue
		}
		fn, ok := tm["function"].(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := fn["name"].(string)
		if name == "" {
			continue
		}
		rt := map[string]interface{}{"type": "function", "name": name}
		if d, ok := fn["description"].(string); ok && d != "" {
			rt["description"] = d
		}
		// parameters may arrive as a map, or as json.RawMessage when passed
		// through from typed structs (e.g. anthropic Tool.InputSchema).
		params, ok := fn["parameters"].(map[string]interface{})
		if !ok {
			if raw, ok := fn["parameters"].(json.RawMessage); ok && len(raw) > 0 {
				var p map[string]interface{}
				if err := json.Unmarshal(raw, &p); err == nil {
					params = p
				}
			} else if s, ok := fn["parameters"].(string); ok && json.Valid([]byte(s)) {
				var p map[string]interface{}
				if err := json.Unmarshal([]byte(s), &p); err == nil {
					params = p
				}
			}
		}
		if params == nil {
			params = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false}
		}
		rt["parameters"] = params
		if strict, ok := fn["strict"].(bool); ok {
			rt["strict"] = strict
		}
		out = append(out, rt)
	}
	return out
}

func extractStringContent(raw interface{}) string {
	switch c := raw.(type) {
	case nil:
		return ""
	case string:
		return c
	case []interface{}:
		var sb strings.Builder
		for _, p := range c {
			if pm, ok := p.(map[string]interface{}); ok {
				if t, _ := pm["type"].(string); t == "text" {
					if txt, _ := pm["text"].(string); txt != "" {
						if sb.Len() > 0 {
							sb.WriteString("\n")
						}
						sb.WriteString(txt)
					}
				}
			}
		}
		return sb.String()
	default:
		return fmt.Sprintf("%v", c)
	}
}

func chatContentToParts(raw interface{}, textType string) interface{} {
	switch c := raw.(type) {
	case string:
		return []map[string]interface{}{{"type": textType, "text": c}}
	case []interface{}:
		parts := make([]map[string]interface{}, 0, len(c))
		for _, p := range c {
			pm, ok := p.(map[string]interface{})
			if !ok {
				continue
			}
			switch t, _ := pm["type"].(string); t {
			case "text":
				txt, _ := pm["text"].(string)
				parts = append(parts, map[string]interface{}{"type": textType, "text": txt})
			case "image_url":
				url := ""
				if u, ok := pm["image_url"].(string); ok {
					url = u
				} else if um, ok := pm["image_url"].(map[string]interface{}); ok {
					url, _ = um["url"].(string)
				}
				if url != "" {
					parts = append(parts, map[string]interface{}{"type": "input_image", "image_url": url})
				}
			}
		}
		if len(parts) > 0 {
			return parts
		}
		return []map[string]interface{}{{"type": textType, "text": ""}}
	default:
		return []map[string]interface{}{{"type": textType, "text": extractStringContent(raw)}}
	}
}
