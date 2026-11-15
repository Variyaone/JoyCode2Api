package anthropic

import (
	"encoding/json"
	"strings"
	"testing"
)

// The exact HTML shell page the JoyCode gateway returns when it rejects an
// oversized POST (JD's jfe-lb default "京东" landing page), abbreviated.
const htmlShellFixture = `<!DOCTYPE html><html><head><title>京东</title><link rel="shortcut icon" href="https://jfe-lb.jd.com/favicon.ico"><meta charset="utf-8"><meta name="apple-mobile-web-app-capable" content="yes"><meta name="viewport" content="width=device-width,initial-scale=1,user-scalable=no,maximum-scale=1,viewport-fit=cover"><style>*{margin:0;padding:0;box-sizing:border-box}</style></head><body></body></html>`

func TestIsHtmlShellError(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{"raw html doctype", htmlShellFixture, true},
		{"raw html html-tag", "<html><body>err</body></html>", true},
		{"client-wrapped upstream prefix", "upstream html shell response (parse error: invalid character '<' looking for beginning of value): " + htmlShellFixture, true},
		{"client-wrapped invalid-json prefix", "invalid JSON response (parse error: invalid character '<' looking for beginning of value): " + htmlShellFixture, true},
		{"normal json error", `{"error":{"message":"API error 500: something","type":"api_error"}}`, false},
		{"empty", "", false},
		{"whitespace only", "   \n  ", false},
		{"plain text error", "dial tcp: lookup api-ai.jd.com: no such host", false},
		{"json embedded in prose (not leading)", `parse error then <!DOCTYPE html> later in the string`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isHtmlShellError(tt.body); got != tt.want {
				t.Fatalf("isHtmlShellError(%q) = %v, want %v", tt.body[:min(len(tt.body), 60)], got, tt.want)
			}
		})
	}
}

func TestDropToolSchemas(t *testing.T) {
	req := &MessageRequest{
		Model: "GLM-5.3",
		Tools: []Tool{
			{Name: "bash", Description: "run bash", InputSchema: jsonRaw(`{"type":"object"}`)},
			{Name: "read", Description: "read file", InputSchema: jsonRaw(`{"type":"object"}`)},
		},
	}
	if !dropToolSchemas(req) {
		t.Fatal("dropToolSchemas should report true when tools were dropped")
	}
	if len(req.Tools) != 0 {
		t.Fatalf("tools not cleared, len = %d", len(req.Tools))
	}
	if dropToolSchemas(req) {
		t.Fatal("dropToolSchemas should report false when there is nothing to drop")
	}
}

func jsonRaw(s string) json.RawMessage { return json.RawMessage(s) }

func TestParseErrorPrefixStripping(t *testing.T) {
	// The classifier must see through the client's prefix; a long inner
	// body that starts with HTML after the "): " separator still counts.
	wrapped := "upstream html shell response (parse error: invalid character '<' looking for beginning of value): <!DOCTYPE html><html>..."
	if !strings.Contains(wrapped, "<!DOCTYPE") {
		t.Fatal("fixture sanity check failed")
	}
	if !isHtmlShellError(wrapped) {
		t.Fatal("wrapped HTML shell not detected")
	}
}
