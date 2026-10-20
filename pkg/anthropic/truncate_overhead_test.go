package anthropic

import (
	"encoding/json"
	"strings"
	"testing"
)

// Tool schemas ride on every upstream call but are not messages, so the
// estimator must count them — otherwise a workflow session loading a browser
// MCP's multi-KB schemas looks small to the preemptive pass while the upstream
// is already over its window.
func TestEstimateTokens_IncludesToolSchemaBytes(t *testing.T) {
	msg := MessageParam{Role: "user", Content: json.RawMessage(`"hi"`)}
	bare := estimateTokens(&MessageRequest{Messages: []MessageParam{msg}})

	bigSchema := strings.Repeat(`{"type":"string","description":"param"}`, 200)
	withTools := estimateTokens(&MessageRequest{
		Messages: []MessageParam{msg},
		Tools: []Tool{{
			Name:        "browser_navigate",
			Description: strings.Repeat("navigate the browser. ", 100),
			InputSchema: json.RawMessage(bigSchema),
		}},
	})

	overheadBytes := int64(len("browser_navigate")) + int64(len(strings.Repeat("navigate the browser. ", 100))) + int64(len(bigSchema))
	wantDelta := int(float64(overheadBytes) / bytesPerToken)
	if got := withTools - bare; got < wantDelta-2 || got > wantDelta+2 {
		t.Errorf("tool schema not counted: delta=%d, want ~%d", got, wantDelta)
	}
}

// A request whose tool schemas alone exceed the threshold can never be fixed
// by dropping messages, yet that is exactly the workflow-subagent case the old
// code 400'd on. The preemptive pass must collapse to the floor and let the
// upstream (which sees the real token count) decide.
func TestPreemptiveTruncate_NeverRejects(t *testing.T) {
	msgs := []MessageParam{{Role: "user", Content: json.RawMessage(`"start"`)}}
	for i := 0; i < 40; i++ {
		msgs = append(msgs,
			MessageParam{Role: "assistant", Content: json.RawMessage(`"` + strings.Repeat("analysis ", 500) + `"`)},
			MessageParam{Role: "user", Content: json.RawMessage(`"` + strings.Repeat("next step ", 500) + `"`)},
		)
	}
	// Tool schema larger than the whole estimated context window: truncation
	// of messages can never bring the estimate below threshold.
	hugeSchema := json.RawMessage(`{"type":"object","properties":{"x":{"description":"` + strings.Repeat("s", int(float64(contextWindowSize)*bytesPerToken)) + `"}}}`)
	req := &MessageRequest{
		Messages: msgs,
		Tools:    []Tool{{Name: "mcp_browser", Description: "browser automation", InputSchema: hugeSchema}},
	}

	rounds := PreemptiveTruncate(req)
	if rounds < 0 {
		t.Fatal("PreemptiveTruncate must never reject; upstream arbitrates real limits")
	}
	if len(req.Messages) != 4 {
		t.Fatalf("expected floor collapse to 4 messages, got %d", len(req.Messages))
	}
	if req.Messages[0].Role != "user" {
		t.Errorf("floor collapse must keep a user message first, got role %q", req.Messages[0].Role)
	}
	if got := req.Messages[1].Role; got != "assistant" {
		t.Errorf("expected truncation notice in assistant slot, got %q", got)
	} else if !strings.Contains(string(req.Messages[1].Content), "auto-truncated") {
		t.Errorf("truncation notice missing: %s", req.Messages[1].Content)
	}
}

// The floor collapse must leave a user-role message first even when the
// conversation started with an assistant message (odd histories happen after
// mid-turn system injections).
func TestTruncateMessagesToFloor_NormalizesLeadingRole(t *testing.T) {
	req := &MessageRequest{Messages: []MessageParam{
		{Role: "assistant", Content: json.RawMessage(`"opening"`)},
		{Role: "user", Content: json.RawMessage(`"a"`)},
		{Role: "assistant", Content: json.RawMessage(`"b"`)},
		{Role: "user", Content: json.RawMessage(`"c"`)},
		{Role: "assistant", Content: json.RawMessage(`"d"`)},
		{Role: "user", Content: json.RawMessage(`"latest"`)},
	}}
	truncateMessagesToFloor(req)
	if len(req.Messages) != 4 {
		t.Fatalf("floor collapse kept %d messages, want 4", len(req.Messages))
	}
	if req.Messages[0].Role != "user" {
		t.Errorf("leading message role = %q, want user", req.Messages[0].Role)
	}
	if string(req.Messages[len(req.Messages)-1].Content) != `"latest"` {
		t.Errorf("floor collapse dropped the latest message: %v", req.Messages)
	}
}

// Conversations at or below the floor size must pass through untouched.
func TestTruncateMessagesToFloor_SmallConversationUntouched(t *testing.T) {
	orig := []MessageParam{
		{Role: "user", Content: json.RawMessage(`"hi"`)},
		{Role: "assistant", Content: json.RawMessage(`"hello"`)},
	}
	req := &MessageRequest{Messages: orig}
	truncateMessagesToFloor(req)
	if len(req.Messages) != 2 {
		t.Fatalf("small conversation changed: %d messages", len(req.Messages))
	}
}
