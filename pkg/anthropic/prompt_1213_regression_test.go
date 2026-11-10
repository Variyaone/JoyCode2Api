package anthropic

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

// Regression for the UK-org-chart session failure (2026-11-10): a conversation
// of 5888 messages + 173 tools hit the floor collapse, whose last two
// messages were an assistant tool_use and a user tool_result whose tool_use
// author was cut. Translation then produced {"role":"user","content":""} and
// the upstream rejected every retry with code 1213
// "未正常接收到prompt参数".

// floorTailOrphanToolResult builds a conversation whose tail is
// assistant(tool_use) + user(tool_result) with the tool_use far earlier in
// the trajectory such that pre-floor truncation rounds cut the pair apart.
func floorTailOrphanToolResult() *MessageRequest {
	msgs := []MessageParam{
		{Role: "user", Content: json.RawMessage(`"investigate the org chart"`)}}
	// Filler mid-conversation traffic.
	for i := 0; i < 10; i++ {
		msgs = append(msgs,
			MessageParam{Role: "assistant", Content: json.RawMessage(`"step ` + strconv.Itoa(i) + `"`)},
			MessageParam{Role: "user", Content: json.RawMessage(`"go on ` + strconv.Itoa(i) + `"`)},
		)
	}
	// Far tail: assistant tool_use (kept last-2 slot in the old code)…
	msgs = append(msgs, MessageParam{Role: "assistant", Content: json.RawMessage(
		`[{"type":"tool_use","id":"toolu_1","name":"search","input":{"q":"uk org"}}]`)})
	// …and its tool_result, which after earlier truncation rounds is
	// orphaned (the old floor kept both verbatim).
	msgs = append(msgs, MessageParam{Role: "user", Content: json.RawMessage(
		`[{"type":"tool_result","tool_use_id":"toolu_1","content":"some results"}]`)})
	return &MessageRequest{Messages: msgs}
}

func iDigits(i int) string { return strconv.Itoa(i) }

// The floor collapse must not end on a user message that translates to an
// empty payload: orphaned tool_result-only user messages are skipped in
// favor of the nearest content-bearing messages.
func TestTruncateMessagesToFloor_SkipsOrphanedToolResultTail(t *testing.T) {
	req := floorTailOrphanToolResult()
	truncateMessagesToFloor(req)

	if len(req.Messages) < 3 {
		t.Fatalf("floor collapse kept %d messages, want at least 3", len(req.Messages))
	}
	tail := req.Messages[len(req.Messages)-1]
	// Whatever the tail is, translating it must never yield an empty user
	// message. The most direct check: the tail user message carries text.
	if tail.Role == "user" {
		var blocks []contentBlock
		if json.Unmarshal(tail.Content, &blocks) == nil {
			for _, b := range blocks {
				if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
					return // found real content
				}
			}
			// No text block: is the tool_result paired?
			for _, b := range blocks {
				if b.Type == "tool_result" && b.ToolUseID != "" {
					paired := false
					for _, m := range req.Messages[:len(req.Messages)-1] {
						if m.Role != "assistant" {
							continue
						}
						var pb []contentBlock
						if json.Unmarshal(m.Content, &pb) != nil {
							continue
						}
						for _, x := range pb {
							if x.Type == "tool_use" && x.ID == b.ToolUseID {
								paired = true
							}
						}
					}
					if !paired {
						t.Fatalf("tail user message is orphaned tool_result: %s", tail.Content)
					}
					break
				}
			}
		}
	}
}

// The translated floor output must contain no empty-content user message —
// the exact upstream 1213 trigger.
func TestTruncateMessagesToFloor_TranslatedOutputNeverEmpty(t *testing.T) {
	req := floorTailOrphanToolResult()
	truncateMessagesToFloor(req)
	msgs := buildMessages(req)
	if len(msgs) == 0 {
		t.Fatal("translated floor output is empty")
	}
	for _, m := range msgs {
		content, _ := m["content"].(string)
		if m["role"] == "user" && strings.TrimSpace(content) == "" {
			t.Fatalf("empty user message in translated output: %+v", m)
		}
	}
}

// A conversation whose entire tail is orphaned tool traffic must still
// produce a non-empty request — a synthesized user turn.
func TestTruncateMessagesToFloor_AllToolTrafficStillNonEmpty(t *testing.T) {
	msgs := []MessageParam{
		{Role: "user", Content: json.RawMessage(`"start"`)},
	}
	for i := 0; i < 5; i++ {
		msgs = append(msgs,
			MessageParam{Role: "user", Content: json.RawMessage(
				`[{"type":"tool_result","tool_use_id":"toolu_x`+string(rune('0'+i))+`","content":"r"}]`)},
			MessageParam{Role: "user", Content: json.RawMessage(
				`[{"type":"tool_result","tool_use_id":"toolu_y`+string(rune('0'+i))+`","content":"r"}]`)},
		)
	}
	req := &MessageRequest{Messages: msgs}
	truncateMessagesToFloor(req)
	out := buildMessages(req)
	if len(out) == 0 {
		t.Fatal("translated output empty for all-tool-traffic conversation")
	}
	hasContent := false
	for _, m := range out {
		if c, _ := m["content"].(string); strings.TrimSpace(c) != "" {
			hasContent = true
		}
	}
	if !hasContent {
		t.Fatalf("no message carries any content: %+v", out)
	}
}

// convertUserBlocks must never emit an empty user message when every block
// was an orphaned tool_result.
func TestConvertUserBlocks_DropsEmptyUserMessage(t *testing.T) {
	blocksJSON := json.RawMessage(
		`[{"type":"tool_result","tool_use_id":"toolu_gone","content":"r"}]`)
	var blocks []contentBlock
	if err := json.Unmarshal(blocksJSON, &blocks); err != nil {
		t.Fatal(err)
	}
	out := convertUserBlocks(blocks, map[string]bool{}) // no tool_use survives
	if len(out) != 0 {
		t.Fatalf("expected empty conversion for orphaned tool_result, got %+v", out)
	}
}

// stripEmptyUserMessages must remove orphaned tool_result-only user messages
// and keep everything else.
func TestStripEmptyUserMessages(t *testing.T) {
	req := &MessageRequest{Messages: []MessageParam{
		{Role: "user", Content: json.RawMessage(`"start"`)},
		{Role: "assistant", Content: json.RawMessage(
			`[{"type":"tool_use","id":"toolu_1","name":"search","input":{}}]`)},
		{Role: "user", Content: json.RawMessage(
			`[{"type":"tool_result","tool_use_id":"toolu_1","content":"ok"}]`)},
		// Orphaned: no matching tool_use.
		{Role: "user", Content: json.RawMessage(
			`[{"type":"tool_result","tool_use_id":"toolu_missing","content":"?"}]`)},
		{Role: "assistant", Content: json.RawMessage(`"done"`)},
		// Empty text only.
		{Role: "user", Content: json.RawMessage(`"   "`)},
	}}
	removed := stripEmptyUserMessages(req)
	if removed != 2 {
		t.Fatalf("removed %d messages, want 2 (orphaned tool_result + whitespace-only)", removed)
	}
	if len(req.Messages) != 4 {
		t.Fatalf("kept %d messages, want 4", len(req.Messages))
	}
}

func TestIsUpstreamPromptError(t *testing.T) {
	cases := []struct {
		body string
		want bool
	}{
		{`{"error":{"cause":"{\"error\":{\"code\":\"1213\",\"message\":\"未正常接收到prompt参数。\"}}","code":400,"message":"模型服务调用失败","status":"FAILED_RESPONSE"}}`, true},
		{`upstream Chat response error: code 1213`, true},
		{`{"error":{"code":"6001","message":"model not supported"}}`, false},
		{`ordinary timeout`, false},
	}
	for _, c := range cases {
		if got := isUpstreamPromptError(c.body); got != c.want {
			t.Errorf("isUpstreamPromptError(%q) = %v, want %v", c.body, got, c.want)
		}
	}
}
