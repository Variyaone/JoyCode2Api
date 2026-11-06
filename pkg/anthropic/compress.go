package anthropic

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/vibe-coding-labs/JoyCode2Api/pkg/joycode"
)

// Summary compression replaces hard truncation as the preferred way to fit an
// over-window request. The proxy is otherwise stateless — the client resends
// the full history every turn, so hard truncation lost different content on
// every request and the assistant appeared to repeatedly forget. Compression
// instead asks the upstream model to summarize the messages that would have
// been cut, caches the summary keyed by the exact conversation prefix, and
// reuses it for subsequent requests that share that prefix. Hard truncation
// stays as the fallback when summarization fails or the compressed request
// still overflows.

const (
	// compactPrompt is the summarization instruction, modelled on Claude
	// Code's own /compact behaviour: preserve task state, decisions, and
	// open work; drop verbatim dumps.
	compactPrompt = "Summarize the conversation so far in a dense, factual outline. " +
		"Preserve: the user's original task and goals, key decisions and their reasons, " +
		"file paths / APIs / commands / error messages mentioned, current state of work, " +
		"and any unresolved questions or next steps. Drop: verbatim file contents, " +
		"full tool outputs, and other bulk detail. Write in the language the " +
		"conversation mostly uses. Output only the summary."
	// summaryMaxTokens bounds the summarization call's own output.
	summaryMaxTokens = 4096
	// summaryCacheTTL bounds how long a cached summary stays fresh. The client
	// resends history every turn, so hits are common within a session.
	summaryCacheTTL = 12 * time.Hour
	// summaryCacheMaxEntries bounds the cache; conversations are the unit, so
	// a few hundred entries covers heavy multi-session use.
	summaryCacheMaxEntries = 512
)

// summaryCacheEntry is a cached summary of a fixed conversation prefix.
type summaryCacheEntry struct {
	summary   string
	createdAt time.Time
	lastUsed  time.Time
}

// summaryCache is a process-wide LRU keyed by the SHA-256 of the exact message
// slice that was summarized. Same prefix → same summary, so the compressed
// view of the conversation stays stable across turns instead of re-summarizing
// (and re-drifting) every request.
var summaryCache = struct {
	sync.Mutex
	entries map[string]*summaryCacheEntry
}{entries: make(map[string]*summaryCacheEntry)}

// prefixKey hashes the messages that a summary would replace.
func prefixKey(messages []MessageParam) string {
	h := sha256.New()
	for _, m := range messages {
		h.Write([]byte(m.Role))
		h.Write([]byte{0})
		h.Write(m.Content)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Summarizer issues the summarization call through the upstream.
type Summarizer interface {
	Summarize(messages []MessageParam) (string, error)
}

// NewChatSummarizer builds a Summarizer for the resolved model defaults.
func NewChatSummarizer(c *joycode.Client, systemDefault string) Summarizer {
	return &chatSummarizer{client: c, systemDefault: systemDefault}
}

// chatSummarizer summarizes via the OpenAI-compatible chat endpoint, the same
// path every non-native model request already takes.
type chatSummarizer struct {
	client        *joycode.Client
	systemDefault string
}

// Summarize calls the upstream chat endpoint with a flattened transcript and
// returns the model's summary. No tools ride along — a tool_use block in the
// summary output would be unusable. The fixed JoyAI model keeps the call on
// the OpenAI chat endpoint even when the conversation's model is a native
// Claude or Responses-API model served elsewhere.
func (s *chatSummarizer) Summarize(messages []MessageParam) (string, error) {
	transcript := renderSummaryInput(messages)
	userContent, err := json.Marshal(transcript)
	if err != nil {
		return "", fmt.Errorf("summarizer: encode transcript: %w", err)
	}
	systemContent, err := json.Marshal(compactPrompt)
	if err != nil {
		return "", fmt.Errorf("summarizer: encode prompt: %w", err)
	}
	req := &MessageRequest{
		Model:     joycode.DefaultModel,
		MaxTokens: summaryMaxTokens,
		System:    systemContent,
		Messages: []MessageParam{
			{Role: "user", Content: userContent},
		},
	}
	body := TranslateRequest(req, "", s.systemDefault)
	delete(body, "tools")
	delete(body, "tool_choice")
	jcResp, err := s.client.Post(chatEndpoint, body)
	if err != nil {
		return "", fmt.Errorf("summarizer upstream: %w", err)
	}
	choices, _ := jcResp["choices"].([]interface{})
	if len(choices) == 0 {
		return "", fmt.Errorf("summarizer: no choices in response")
	}
	choice, _ := choices[0].(map[string]interface{})
	message, _ := choice["message"].(map[string]interface{})
	if message == nil {
		return "", fmt.Errorf("summarizer: no message in response")
	}
	text, _ := message["content"].(string)
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("summarizer: empty summary text")
	}
	return text, nil
}

// renderSummaryInput flattens messages into a transcript the summarizer can
// read. Tool blocks are rendered as readable text so the summary reads like a
// session log rather than raw protocol JSON.
func renderSummaryInput(messages []MessageParam) string {
	var b strings.Builder
	for _, m := range messages {
		switch m.Role {
		case "user", "assistant", "system":
			fmt.Fprintf(&b, "%s: ", strings.ToUpper(m.Role[:1])+m.Role[1:])
		default:
			fmt.Fprintf(&b, "[%s]: ", m.Role)
		}
		b.WriteString(renderMessageText(m.Content))
		b.WriteString("\n\n")
	}
	return b.String()
}

// renderMessageText extracts readable text from message content, which may be
// a plain string, a block array, or unknown JSON.
func renderMessageText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []contentBlock
	if json.Unmarshal(raw, &blocks) != nil {
		return string(raw)
	}
	var parts []string
	for _, blk := range blocks {
		switch blk.Type {
		case "text":
			parts = append(parts, blk.Text)
		case "tool_use":
			parts = append(parts, fmt.Sprintf("(calls tool %s with input %s)", blk.Name, string(blk.Input)))
		case "tool_result":
			parts = append(parts, fmt.Sprintf("(tool result: %s)", renderMessageText(blk.Content)))
		case "thinking":
			parts = append(parts, "(reasoning omitted)")
		case "image":
			parts = append(parts, "(image attached)")
		default:
			if jb, err := json.Marshal(blk); err == nil {
				parts = append(parts, string(jb))
			}
		}
	}
	return strings.Join(parts, "\n")
}

// CompressMessages attempts summary compression of an over-window request:
// summarize the older portion, replace it with the summary, keep the tail.
// Returns true if req.Messages was rewritten with a compressed history.
//
// The cut point mirrors truncateMessages' tail-keeping: keep the first
// message (original task) + last 60%, summarize the middle 40%. The summary
// call is skipped when the prefix is already cached.
func CompressMessages(req *MessageRequest, s Summarizer) bool {
	if s == nil {
		return false
	}
	n := len(req.Messages)
	if n <= 4 {
		return false
	}
	keepLast := int(float64(n) * 0.6)
	if keepLast < 4 {
		keepLast = 4
	}
	cutEnd := n - keepLast
	if cutEnd <= 1 {
		return false
	}
	cutEnd = findToolPairBoundary(req.Messages, cutEnd)
	if cutEnd <= 1 || cutEnd >= n {
		return false
	}

	prefix := req.Messages[:cutEnd]
	key := prefixKey(prefix)

	summaryCache.Lock()
	entry, ok := summaryCache.entries[key]
	if ok && time.Since(entry.createdAt) < summaryCacheTTL {
		entry.lastUsed = time.Now()
		summary := entry.summary
		summaryCache.Unlock()
		return applySummary(req, summary, cutEnd)
	}
	summaryCache.Unlock()

	summary, err := s.Summarize(prefix)
	if err != nil {
		slog.Warn("context compression: summarize failed, will fall back to truncation", "error", err)
		return false
	}

	summaryCache.Lock()
	if len(summaryCache.entries) >= summaryCacheMaxEntries {
		evictOldestSummaryLocked()
	}
	summaryCache.entries[key] = &summaryCacheEntry{
		summary:   summary,
		createdAt: time.Now(),
		lastUsed:  time.Now(),
	}
	summaryCache.Unlock()

	return applySummary(req, summary, cutEnd)
}

// evictOldestSummaryLocked evicts the least-recently-used cache entry.
// Caller holds the cache lock.
func evictOldestSummaryLocked() {
	var oldestKey string
	var oldestTime time.Time
	first := true
	for k, e := range summaryCache.entries {
		if first || e.lastUsed.Before(oldestTime) {
			oldestKey, oldestTime = k, e.lastUsed
			first = false
		}
	}
	if oldestKey != "" {
		delete(summaryCache.entries, oldestKey)
	}
}

// applySummary rewrites req.Messages: first message + summary carrier + tail.
// The summary rides in as an assistant text block so the conversation stays a
// legal alternation for the upstream.
func applySummary(req *MessageRequest, summary string, cutEnd int) bool {
	n := len(req.Messages)
	blocks, err := json.Marshal([]contentBlock{{
		Type: "text",
		Text: "[System: Earlier conversation messages were auto-compressed to fit the context window. Summary of the conversation so far:\n" + summary + "]",
	}})
	if err != nil {
		return false
	}
	compressed := make([]MessageParam, 0, 2+n-cutEnd)
	compressed = append(compressed, req.Messages[0])
	compressed = append(compressed, MessageParam{Role: "assistant", Content: blocks})
	compressed = append(compressed, req.Messages[cutEnd:]...)
	slog.Info("context compression applied",
		"original_count", n,
		"compressed_count", len(compressed),
		"summarized_messages", cutEnd-1,
		"kept_tail", n-cutEnd,
	)
	req.Messages = compressed
	return true
}
