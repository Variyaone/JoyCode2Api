package openai

import (
	"encoding/json"
	"time"
)

// Responses API stream/response conversion. The chat<->Responses request
// translation lives in pkg/joycode/responses_translate.go (shared with the
// anthropic handler).

// ResponsesStreamState accumulates a Responses API SSE stream and converts it
// to OpenAI chat completion chunks.
type ResponsesStreamState struct {
	Model       string
	inTk        int
	outTk       int
	toolCalls   map[string]*responsesToolCall
	textStarted bool
}

type responsesToolCall struct {
	ID        string
	CallID    string
	Name      string
	Arguments string
	Emitted   bool
	Index     int
}

// Feed processes one upstream SSE data line (already unwrapped from "data:").
// It returns chat.completion.chunk JSON payloads to emit to the client.
func (st *ResponsesStreamState) Feed(payload string) []string {
	var ev struct {
		Type   string `json:"type"`
		Delta  string `json:"delta"`
		Output string `json:"output"`
		Item   *struct {
			ID        string `json:"id"`
			Type      string `json:"type"`
			CallID    string `json:"call_id"`
			Name      string `json:"name"`
			Status    string `json:"status"`
			Arguments string `json:"arguments"`
			Content   []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"item"`
		Response *struct {
			Usage *struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		} `json:"response"`
		Usage *struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal([]byte(payload), &ev); err != nil {
		return nil
	}

	var chunks []string
	switch ev.Type {
	case "response.output_text.delta":
		if ev.Delta != "" {
			chunks = append(chunks, st.textChunk(ev.Delta))
		}
	case "response.output_item.added":
		if ev.Item != nil && ev.Item.Type == "function_call" {
			st.ensureToolCall(ev.Item.CallID, ev.Item.ID, ev.Item.Name)
		}
	case "response.output_item.done":
		if ev.Item != nil && ev.Item.Type == "function_call" {
			tc := st.ensureToolCall(ev.Item.CallID, ev.Item.ID, ev.Item.Name)
			args := ev.Item.Arguments
			if args == "" || !json.Valid([]byte(args)) {
				args = "{}"
			}
			tc.Arguments = args
			chunks = append(chunks, st.toolChunk(tc))
		}
	case "response.completed":
		if ev.Response != nil && ev.Response.Usage != nil {
			st.inTk = ev.Response.Usage.InputTokens
			st.outTk = ev.Response.Usage.OutputTokens
		}
		if ev.Usage != nil {
			if ev.Usage.InputTokens > 0 {
				st.inTk = ev.Usage.InputTokens
			}
			if ev.Usage.OutputTokens > 0 {
				st.outTk = ev.Usage.OutputTokens
			}
		}
	}
	return chunks
}

func (st *ResponsesStreamState) ensureToolCall(callID, itemID, name string) *responsesToolCall {
	key := callID
	if key == "" {
		key = itemID
	}
	if tc, ok := st.toolCalls[key]; ok {
		return tc
	}
	tc := &responsesToolCall{
		ID:     "call_" + newShortID(),
		CallID: callID,
		Name:   name,
		Index:  len(st.toolCalls),
	}
	st.toolCalls[key] = tc
	return tc
}

// Finalize returns closing chunks: pending tool calls plus the final
// finish_reason/usage chunk.
func (st *ResponsesStreamState) Finalize() []string {
	var chunks []string
	for _, tc := range st.sortedToolCalls() {
		if !tc.Emitted {
			chunks = append(chunks, st.toolChunk(tc))
		}
	}
	finish := "stop"
	if len(st.toolCalls) > 0 {
		finish = "tool_calls"
	}
	final := map[string]interface{}{
		"id":      "chatcmpl-" + newShortID(),
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   st.Model,
		"choices": []interface{}{map[string]interface{}{
			"index":         0,
			"delta":         map[string]interface{}{},
			"finish_reason": finish,
		}},
	}
	if st.inTk > 0 || st.outTk > 0 {
		final["usage"] = map[string]interface{}{
			"prompt_tokens":     st.inTk,
			"completion_tokens": st.outTk,
		}
	}
	b, _ := json.Marshal(final)
	return append(chunks, string(b))
}

// Usage reports accumulated token usage.
func (st *ResponsesStreamState) Usage() (int, int) {
	return st.inTk, st.outTk
}

func (st *ResponsesStreamState) sortedToolCalls() []*responsesToolCall {
	calls := make([]*responsesToolCall, 0, len(st.toolCalls))
	for _, tc := range st.toolCalls {
		calls = append(calls, tc)
	}
	for i := 0; i < len(calls); i++ {
		for j := i + 1; j < len(calls); j++ {
			if calls[j].Index < calls[i].Index {
				calls[i], calls[j] = calls[j], calls[i]
			}
		}
	}
	return calls
}

func (st *ResponsesStreamState) textChunk(text string) string {
	st.textStarted = true
	chunk := map[string]interface{}{
		"id":      "chatcmpl-" + newShortID(),
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   st.Model,
		"choices": []interface{}{map[string]interface{}{
			"index":         0,
			"delta":         map[string]interface{}{"content": text},
			"finish_reason": nil,
		}},
	}
	b, _ := json.Marshal(chunk)
	return string(b)
}

func (st *ResponsesStreamState) toolChunk(tc *responsesToolCall) string {
	tc.Emitted = true
	chunk := map[string]interface{}{
		"id":      "chatcmpl-" + newShortID(),
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   st.Model,
		"choices": []interface{}{map[string]interface{}{
			"index": 0,
			"delta": map[string]interface{}{
				"tool_calls": []interface{}{map[string]interface{}{
					"index": tc.Index,
					"id":    tc.ID,
					"type":  "function",
					"function": map[string]interface{}{
						"name":      tc.Name,
						"arguments": tc.Arguments,
					},
				}},
			},
			"finish_reason": nil,
		}},
	}
	b, _ := json.Marshal(chunk)
	return string(b)
}

// ResponsesFinalResponse converts a completed (non-stream) Responses object
// into chat.completion format.
func ResponsesFinalResponse(resp map[string]interface{}, model string) map[string]interface{} {
	content := ""
	var toolCalls []interface{}
	if output, ok := resp["output"].([]interface{}); ok {
		for _, item := range output {
			im, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			switch t, _ := im["type"].(string); t {
			case "message":
				if cs, ok := im["content"].([]interface{}); ok {
					for _, c := range cs {
						cm, ok := c.(map[string]interface{})
						if !ok {
							continue
						}
						if txt, _ := cm["text"].(string); txt != "" {
							content += txt
						}
					}
				}
			case "function_call":
				args, _ := im["arguments"].(string)
				if args == "" || !json.Valid([]byte(args)) {
					args = "{}"
				}
				callID, _ := im["call_id"].(string)
				name, _ := im["name"].(string)
				toolCalls = append(toolCalls, map[string]interface{}{
					"id":   callID,
					"type": "function",
					"function": map[string]interface{}{
						"name":      name,
						"arguments": args,
					},
				})
			}
		}
	}
	message := map[string]interface{}{"role": "assistant", "content": content}
	finish := "stop"
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
		finish = "tool_calls"
	}
	usage := map[string]interface{}{}
	if u, ok := resp["usage"].(map[string]interface{}); ok {
		usage = u
	}
	return map[string]interface{}{
		"id":      "chatcmpl-" + newShortID(),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []interface{}{map[string]interface{}{
			"index":         0,
			"message":       message,
			"finish_reason": finish,
		}},
		"usage": usage,
	}
}
