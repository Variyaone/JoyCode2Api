package openai

// Claude bridge: routes Claude-family models arriving on the OpenAI
// /v1/chat/completions endpoint through JoyCode's native Anthropic endpoint.
// The upstream rejects Claude on the legacy chat/completions path with
// error 6001 ("当前模型不可用或已下线"), so clients speaking the OpenAI
// protocol (Trae, Codex, …) get an empty response without this bridge.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/vibe-coding-labs/JoyCode2Api/pkg/anthropic"
	"github.com/vibe-coding-labs/JoyCode2Api/pkg/store"
)

func nowUnix() int64 { return time.Now().Unix() }

// IsClaudeModel reports whether the model label routes through the native
// Anthropic endpoint.
func IsClaudeModel(model string) bool {
	return anthropic.IsNativeAnthropicModel(model)
}

// chatToAnthropicRequest converts an OpenAI ChatRequest into an Anthropic
// MessageRequest. System messages become the system prompt; string and
// content-part messages pass through with role mapping (assistant stays
// assistant, everything else non-system becomes user).
func chatToAnthropicRequest(req *ChatRequest, maxTokens int) (*anthropic.MessageRequest, error) {
	out := &anthropic.MessageRequest{
		Model:     req.Model,
		MaxTokens: maxTokens,
	}
	if out.MaxTokens <= 0 {
		out.MaxTokens = 8192
	}
	if out.MaxTokens > 32768 {
		out.MaxTokens = 32768
	}

	var msgs []anthropic.MessageParam
	var systemParts []string

	var rawMsgs []struct {
		Role         string          `json:"role"`
		Content      json.RawMessage `json:"content"`
		ToolCalls    json.RawMessage `json:"tool_calls"`
		ToolCallID   string          `json:"tool_call_id"`
		Name         string          `json:"name"`
	}
	if len(req.Messages) > 0 {
		if err := json.Unmarshal(req.Messages, &rawMsgs); err != nil {
			return nil, fmt.Errorf("decode messages: %w", err)
		}
	}
	for _, m := range rawMsgs {
		content := normalizeOpenAIContent(m.Content)
		switch m.Role {
		case "system", "developer":
			if content != "" {
				systemParts = append(systemParts, content)
			}
		case "tool":
			// OpenAI tool result → Anthropic user message with tool_result block.
			block := map[string]interface{}{
				"type":        "tool_result",
				"tool_use_id": m.ToolCallID,
				"content":     content,
			}
			b, _ := json.Marshal([]interface{}{block})
			msgs = append(msgs, anthropic.MessageParam{Role: "user", Content: b})
		case "assistant":
			// Assistant text and tool_calls merge into one content array.
			var blocks []interface{}
			if content != "" {
				blocks = append(blocks, map[string]interface{}{"type": "text", "text": content})
			}
			if len(m.ToolCalls) > 0 {
				var calls []struct {
					ID      string `json:"id"`
					Type    string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				}
				if err := json.Unmarshal(m.ToolCalls, &calls); err == nil {
					for _, c := range calls {
						blocks = append(blocks, map[string]interface{}{
							"type":  "tool_use",
							"id":    c.ID,
							"name":  c.Function.Name,
							"input": json.RawMessage(c.Function.Arguments),
						})
					}
				}
			}
			if len(blocks) == 0 {
				blocks = []interface{}{map[string]interface{}{"type": "text", "text": ""}}
			}
			b, _ := json.Marshal(blocks)
			msgs = append(msgs, anthropic.MessageParam{Role: "assistant", Content: b})
		default: // user
			b, _ := json.Marshal([]interface{}{map[string]interface{}{"type": "text", "text": content}})
			msgs = append(msgs, anthropic.MessageParam{Role: "user", Content: b})
		}
	}
	out.Messages = msgs
	if len(systemParts) > 0 {
		b, _ := json.Marshal([]map[string]string{{"type": "text", "text": strings.Join(systemParts, "\n\n")}})
		out.System = b
	}

	// Tools: OpenAI function tools → Anthropic tools.
	if len(req.Tools) > 0 {
		var rawTools []struct {
			Type     string `json:"type"`
			Function struct {
				Name        string          `json:"name"`
				Description string          `json:"description"`
				Parameters  json.RawMessage `json:"parameters"`
			} `json:"function"`
		}
		if err := json.Unmarshal(req.Tools, &rawTools); err == nil {
			for _, t := range rawTools {
				schema := t.Function.Parameters
				if len(schema) == 0 {
					schema = json.RawMessage(`{"type":"object"}`)
				}
				out.Tools = append(out.Tools, anthropic.Tool{
					Name:        t.Function.Name,
					Description: t.Function.Description,
					InputSchema: schema,
				})
			}
		}
	}

	if req.Temperature != nil {
		out.Temperature = req.Temperature
	}
	if req.TopP != nil {
		out.TopP = req.TopP
	}
	if len(req.Stop) > 0 {
		var stops []string
		if json.Unmarshal(req.Stop, &stops) == nil && len(stops) > 0 {
			out.StopSequences = stops
		}
	}
	return out, nil
}

// normalizeOpenAIContent flattens an OpenAI content field (string or parts
// array) into plain text. Image parts become "[image]" placeholders — the
// native endpoint accepts Anthropic-format images, but OpenAI image_url
// parts reference URLs we do not fetch.
func normalizeOpenAIContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		ImageURL *struct {
			URL string `json:"url"`
		} `json:"image_url"`
	}
	if json.Unmarshal(raw, &parts) == nil {
		var texts []string
		for _, p := range parts {
			switch p.Type {
			case "text":
				texts = append(texts, p.Text)
			case "image_url":
				texts = append(texts, "[image]")
			}
		}
		return strings.Join(texts, "\n")
	}
	return ""
}

const claudeBridgeEndpoint = "/api/saas/anthropic/v1/messages"

// handleClaudeChat bridges an OpenAI chat request for a Claude model through
// the native Anthropic endpoint and re-emits it in OpenAI chunk format.
func (s *Server) handleClaudeChat(w http.ResponseWriter, r *http.Request, req *ChatRequest, model string) {
	areq, err := chatToAnthropicRequest(req, req.MaxTokens)
	if err != nil {
		writeError(w, 400, "claude bridge: "+err.Error())
		return
	}
	areq.Model = model

	systemDefault := ""
	if s.store != nil {
		systemDefault = s.store.GetSetting("default_model")
	}
	body := anthropic.TranslateAnthropicRequest(areq, store.GetAccountDefaultModel(r), systemDefault)
	client := s.getClient(r)

	resp, err := client.PostAnthropicStream(claudeBridgeEndpoint, body)
	if err != nil {
		writeError(w, 502, "claude bridge upstream: "+err.Error())
		return
	}
	defer resp.Body.Close()

	if req.Stream {
		s.streamClaudeAsOpenAI(w, r, resp.Body, model)
		return
	}
	s.aggregateClaudeAsOpenAI(w, r, resp.Body, model)
}

// claudeChunk emits one OpenAI chat.completion.chunk SSE line.
func claudeChunk(w http.ResponseWriter, flusher http.Flusher, id, model, content string, toolIndex int, toolCall *claudeToolDelta, finish string) {
	delta := map[string]interface{}{}
	if content != "" {
		delta["content"] = content
	}
	if toolCall != nil {
		delta["tool_calls"] = []interface{}{toolCall.toOpenAI(toolIndex)}
	}
	chunk := map[string]interface{}{
		"id":      id,
		"object":  "chat.completion.chunk",
		"created": nowUnix(),
		"model":   model,
		"choices": []interface{}{map[string]interface{}{
			"index":         0,
			"delta":         delta,
			"finish_reason": nil,
		}},
	}
	if finish != "" {
		chunk["choices"].([]interface{})[0].(map[string]interface{})["finish_reason"] = finish
	}
	b, _ := json.Marshal(chunk)
	fmt.Fprintf(w, "data: %s\n\n", b)
	flusher.Flush()
}

type claudeToolDelta struct {
	ID        string
	Name      string
	Arguments string
}

func (t *claudeToolDelta) toOpenAI(index int) map[string]interface{} {
	return map[string]interface{}{
		"index":    index,
		"id":       t.ID,
		"type":     "function",
		"function": map[string]interface{}{"name": t.Name, "arguments": t.Arguments},
	}
}

// streamClaudeAsOpenAI reads native Anthropic SSE and re-emits OpenAI chunks.
func (s *Server) streamClaudeAsOpenAI(w http.ResponseWriter, r *http.Request, body interface{ Read([]byte) (int, error) }, model string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, 500, "streaming not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "close")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(200)

	id := "chatcmpl-claude-" + fmt.Sprint(nowUnix())
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	toolIndex := -1
	var toolID, toolName, toolArgs strings.Builder
	inTk, outTk := 0, 0
	finish := ""

	for scanner.Scan() {
		payload := unwrapSSEData(scanner.Text())
		if payload == "" || !strings.HasPrefix(payload, "{") {
			continue
		}
		var ev struct {
			Type string `json:"type"`
			Delta struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				PartialJSON string `json:"partial_json"`
				StopReason  string `json:"stop_reason"`
			} `json:"delta"`
			ContentBlock struct {
				Type  string `json:"type"`
				ID    string `json:"id"`
				Name  string `json:"name"`
			} `json:"content_block"`
			Message struct {
				Usage struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			} `json:"message"`
			Usage struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal([]byte(payload), &ev) != nil {
			continue
		}
		switch ev.Type {
		case "message_start":
			inTk = ev.Message.Usage.InputTokens
		case "content_block_start":
			if ev.ContentBlock.Type == "tool_use" {
				toolIndex++
				toolID.Reset()
				toolName.Reset()
				toolArgs.Reset()
				toolID.WriteString(ev.ContentBlock.ID)
				toolName.WriteString(ev.ContentBlock.Name)
				claudeChunk(w, flusher, id, model, "", toolIndex, &claudeToolDelta{ID: ev.ContentBlock.ID, Name: ev.ContentBlock.Name}, "")
			}
		case "content_block_delta":
			switch ev.Delta.Type {
			case "text_delta":
				claudeChunk(w, flusher, id, model, ev.Delta.Text, -1, nil, "")
			case "input_json_delta":
				toolArgs.WriteString(ev.Delta.PartialJSON)
				claudeChunk(w, flusher, id, model, "", toolIndex, &claudeToolDelta{Arguments: ev.Delta.PartialJSON}, "")
			}
		case "message_delta":
			if ev.Delta.StopReason != "" {
				switch ev.Delta.StopReason {
				case "tool_use":
					finish = "tool_calls"
				case "max_tokens":
					finish = "length"
				default:
					finish = "stop"
				}
			}
			if ev.Usage.OutputTokens > 0 {
				outTk = ev.Usage.OutputTokens
			}
		}
	}
	if finish == "" {
		finish = "stop"
	}
	// Final chunk with finish_reason and usage.
	final := map[string]interface{}{
		"id":      id,
		"object":  "chat.completion.chunk",
		"created": nowUnix(),
		"model":   model,
		"choices": []interface{}{map[string]interface{}{
			"index":         0,
			"delta":         map[string]interface{}{},
			"finish_reason": finish,
		}},
		"usage": map[string]interface{}{"prompt_tokens": inTk, "completion_tokens": outTk},
	}
	b, _ := json.Marshal(final)
	fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", b)
	flusher.Flush()
	store.SetTokenUsage(r, inTk, outTk)
}

// aggregateClaudeAsOpenAI reads native Anthropic SSE and returns one
// chat.completion object.
func (s *Server) aggregateClaudeAsOpenAI(w http.ResponseWriter, r *http.Request, body interface{ Read([]byte) (int, error) }, model string) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var text strings.Builder
	type toolCall struct {
		ID   string
		Name string
		Args strings.Builder
	}
	var tools []toolCall
	var current *toolCall
	inTk, outTk := 0, 0
	stopReason := "end_turn"

	for scanner.Scan() {
		payload := unwrapSSEData(scanner.Text())
		if payload == "" || !strings.HasPrefix(payload, "{") {
			continue
		}
		var ev struct {
			Type string `json:"type"`
			Delta struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				PartialJSON string `json:"partial_json"`
				StopReason  string `json:"stop_reason"`
			} `json:"delta"`
			ContentBlock struct {
				Type  string `json:"type"`
				ID    string `json:"id"`
				Name  string `json:"name"`
			} `json:"content_block"`
			Message struct {
				Usage struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			} `json:"message"`
			Usage struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal([]byte(payload), &ev) != nil {
			continue
		}
		switch ev.Type {
		case "message_start":
			inTk = ev.Message.Usage.InputTokens
		case "content_block_start":
			if ev.ContentBlock.Type == "tool_use" {
				tools = append(tools, toolCall{ID: ev.ContentBlock.ID, Name: ev.ContentBlock.Name})
				current = &tools[len(tools)-1]
			}
		case "content_block_delta":
			switch ev.Delta.Type {
			case "text_delta":
				text.WriteString(ev.Delta.Text)
			case "input_json_delta":
				if current != nil {
					current.Args.WriteString(ev.Delta.PartialJSON)
				}
			}
		case "message_delta":
			if ev.Delta.StopReason != "" {
				stopReason = ev.Delta.StopReason
			}
			if ev.Usage.OutputTokens > 0 {
				outTk = ev.Usage.OutputTokens
			}
		}
	}

	finish := "stop"
	switch stopReason {
	case "tool_use":
		finish = "tool_calls"
	case "max_tokens":
		finish = "length"
	}
	message := map[string]interface{}{"role": "assistant", "content": text.String()}
	if len(tools) > 0 {
		var calls []interface{}
		for _, t := range tools {
			calls = append(calls, map[string]interface{}{
				"id":   t.ID,
				"type": "function",
				"function": map[string]interface{}{
					"name":      t.Name,
					"arguments": t.Args.String(),
				},
			})
		}
		message["tool_calls"] = calls
	}
	resp := map[string]interface{}{
		"id":      "chatcmpl-claude-" + fmt.Sprint(nowUnix()),
		"object":  "chat.completion",
		"created": nowUnix(),
		"model":   model,
		"choices": []interface{}{map[string]interface{}{
			"index":         0,
			"message":       message,
			"finish_reason": finish,
		}},
		"usage": map[string]interface{}{"prompt_tokens": inTk, "completion_tokens": outTk},
	}
	b, _ := json.Marshal(resp)
	w.Header().Set("Content-Type", "application/json")
	w.Write(b)
	store.SetTokenUsage(r, inTk, outTk)
}

// unwrapSSEData strips SSE framing (leading "data:" prefixes) from a line.
func unwrapSSEData(line string) string {
	trimmed := strings.TrimSpace(line)
	for {
		next := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
		if next == trimmed {
			return trimmed
		}
		trimmed = next
	}
}
