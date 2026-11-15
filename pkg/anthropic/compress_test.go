package anthropic

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// stubSummarizer returns a canned summary, recording what it was asked to summarize.
type stubSummarizer struct {
	calls    int
	summary  string
	fail     bool
	lastSeen []MessageParam
}

func (s *stubSummarizer) Summarize(messages []MessageParam) (string, error) {
	s.calls++
	s.lastSeen = messages
	if s.fail {
		return "", fmt.Errorf("summarizer unavailable")
	}
	return s.summary, nil
}

func makeOverWindowRequest(n int) *MessageRequest {
	msgs := make([]MessageParam, 0, n)
	msgs = append(msgs, MessageParam{Role: "user", Content: json.RawMessage(`"original task"`)})
	for i := 1; i < n; i++ {
		if i%2 == 1 {
			msgs = append(msgs, MessageParam{Role: "assistant", Content: json.RawMessage(fmt.Sprintf(`"assistant turn %d with a fairly long filler body to give it some weight %s"`, i, strings.Repeat("x", 200)))})
		} else {
			msgs = append(msgs, MessageParam{Role: "user", Content: json.RawMessage(fmt.Sprintf(`"user turn %d %s"`, i, strings.Repeat("y", 200)))})
		}
	}
	return &MessageRequest{Model: "GLM-4.6", MaxTokens: 8192, Messages: msgs}
}

func TestCompressMessagesReplacesPrefixWithSummary(t *testing.T) {
	clearSummaryCache(t)
	req := makeOverWindowRequest(20)
	s := &stubSummarizer{summary: "SUMMARY-OF-PREFIX"}

	if !CompressMessages(req, s) {
		t.Fatal("compression returned false")
	}
	if s.calls != 1 {
		t.Fatalf("summarizer calls = %d, want 1", s.calls)
	}
	if got := len(req.Messages); got != 2+12 {
		t.Fatalf("compressed message count = %d, want 14 (first + summary + 12 tail)", got)
	}
	if req.Messages[0].Role != "user" || !strings.Contains(string(req.Messages[0].Content), "original task") {
		t.Fatalf("first message not preserved: %s", req.Messages[0].Content)
	}
	if req.Messages[1].Role != "assistant" || !strings.Contains(string(req.Messages[1].Content), "SUMMARY-OF-PREFIX") {
		t.Fatalf("summary carrier missing: %s", req.Messages[1].Content)
	}
}

func TestCompressMessagesUsesCacheOnRepeat(t *testing.T) {
	clearSummaryCache(t)
	req := makeOverWindowRequest(20)
	s := &stubSummarizer{summary: "CACHED-SUMMARY"}

	if !CompressMessages(req, s) {
		t.Fatal("first compression failed")
	}
	// Second request with the same prefix: the summarize call must hit cache.
	req2 := makeOverWindowRequest(20)
	s.calls = 0
	if !CompressMessages(req2, s) {
		t.Fatal("second compression failed")
	}
	if s.calls != 0 {
		t.Fatalf("summarizer called %d times on cache hit, want 0", s.calls)
	}
	if !strings.Contains(string(req2.Messages[1].Content), "CACHED-SUMMARY") {
		t.Fatalf("cached summary not applied: %s", req2.Messages[1].Content)
	}
}

func TestCompressMessagesFallsBackWhenSummarizerFails(t *testing.T) {
	clearSummaryCache(t)
	req := makeOverWindowRequest(20)
	s := &stubSummarizer{fail: true}

	if CompressMessages(req, s) {
		t.Fatal("compression should fail when summarizer fails")
	}
	// Messages untouched; caller falls back to truncateMessages.
	if len(req.Messages) != 20 {
		t.Fatalf("messages mutated on failure: %d", len(req.Messages))
	}
}

func TestCompressMessagesTooFewMessages(t *testing.T) {
	clearSummaryCache(t)
	req := makeOverWindowRequest(4)
	s := &stubSummarizer{summary: "x"}
	if CompressMessages(req, s) {
		t.Fatal("compression should refuse tiny conversations")
	}
}

func TestCompressMessagesPreservesToolPairs(t *testing.T) {
	clearSummaryCache(t)
	// tool_use at index 15, tool_result at 16 — the cut must not orphan them.
	msgs := make([]MessageParam, 0, 20)
	msgs = append(msgs, MessageParam{Role: "user", Content: json.RawMessage(`"task"`)})
	for i := 1; i < 20; i++ {
		switch i {
		case 15:
			msgs = append(msgs, MessageParam{Role: "assistant", Content: json.RawMessage(`[{"type":"tool_use","id":"tool_1","name":"Read","input":{"path":"a.go"}}]`)})
		case 16:
			msgs = append(msgs, MessageParam{Role: "user", Content: json.RawMessage(`[{"type":"tool_result","tool_use_id":"tool_1","content":"file contents"}]`)})
		default:
			role := "assistant"
			if i%2 == 0 {
				role = "user"
			}
			msgs = append(msgs, MessageParam{Role: role, Content: json.RawMessage(fmt.Sprintf(`"turn %d"`, i))})
		}
	}
	req := &MessageRequest{Model: "GLM-4.6", MaxTokens: 8192, Messages: msgs}
	s := &stubSummarizer{summary: "SUMMARY"}

	if !CompressMessages(req, s) {
		t.Fatal("compression failed")
	}
	// Walk kept messages: every tool_result must have its tool_use still present.
	uses := map[string]bool{}
	for _, m := range req.Messages {
		var blocks []contentBlock
		if json.Unmarshal(m.Content, &blocks) != nil {
			continue
		}
		for _, b := range blocks {
			if b.Type == "tool_use" && b.ID != "" {
				uses[b.ID] = true
			}
		}
	}
	for _, m := range req.Messages {
		var blocks []contentBlock
		if json.Unmarshal(m.Content, &blocks) != nil {
			continue
		}
		for _, b := range blocks {
			if b.Type == "tool_result" && !uses[b.ToolUseID] {
				t.Fatalf("orphaned tool_result %s after compression", b.ToolUseID)
			}
		}
	}
}

func TestPreemptiveShrinkPrefersCompression(t *testing.T) {
	clearSummaryCache(t)
	// Build a request over the threshold: many large messages (~700 bytes each,
	// 3.5 bytes/token → need >167K tokens ≈ >585KB ≈ >800 messages).
	n := 3000
	req := makeOverWindowRequest(n)
	s := &stubSummarizer{summary: "BIG-SUMMARY"}

	rounds := PreemptiveTruncate(req, s)
	if rounds == 0 {
		t.Fatal("no shrink rounds for over-threshold request")
	}
	if s.calls == 0 {
		t.Fatal("compression was not attempted before truncation")
	}
	// The result must contain the summary, not just a truncation notice.
	found := false
	for _, m := range req.Messages {
		if strings.Contains(string(m.Content), "BIG-SUMMARY") {
			found = true
		}
	}
	if !found {
		t.Fatal("compressed request does not carry the summary")
	}
}

func TestPreemptiveShrinkFallsBackToTruncation(t *testing.T) {
	clearSummaryCache(t)
	n := 3000
	req := makeOverWindowRequest(n)
	s := &stubSummarizer{fail: true}

	rounds := PreemptiveTruncate(req, s)
	if rounds == 0 {
		t.Fatal("no shrink rounds for over-threshold request")
	}
	if s.calls == 0 {
		t.Fatal("summarizer was never tried")
	}
	// Fallback truncation notice should be present.
	found := false
	for _, m := range req.Messages {
		if strings.Contains(string(m.Content), "auto-truncated") {
			found = true
		}
	}
	if !found {
		t.Fatal("truncation fallback did not run")
	}
}

func clearSummaryCache(t *testing.T) {
	t.Helper()
	summaryCache.Lock()
	summaryCache.entries = make(map[string]*summaryCacheEntry)
	summaryCache.Unlock()
}
