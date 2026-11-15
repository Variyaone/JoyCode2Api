// Package common provides small shared helpers used across protocol, client,
// and CLI packages to avoid duplicate implementations.
package common

import "strings"

// IsTimeoutError reports whether err looks like an HTTP client / context
// timeout. It matches on the error message because the upstream JoyCode API
// and Go's net/http wrap timeouts in several different ways.
func IsTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "context deadline exceeded") ||
		strings.Contains(lower, "client.timeout exceeded") ||
		strings.Contains(lower, "deadline exceeded") ||
		strings.Contains(lower, "i/o timeout") ||
		strings.Contains(lower, "read: connection reset") ||
		strings.Contains(lower, "connection reset by peer")
}

// IsHtmlShellResponse reports whether an upstream response body is an HTML
// page rather than the expected JSON. The JoyCode gateway (JD's jfe-lb layer)
// answers oversized or otherwise rejected POSTs with its default "京东" HTML
// landing page instead of a JSON error; treating that as a transient network
// error leads to slow retries that can never succeed.
func IsHtmlShellResponse(body string) bool {
	trimmed := strings.TrimSpace(body)
	if len(trimmed) < 9 {
		return false
	}
	head := trimmed
	if len(head) > 16 {
		head = head[:16]
	}
	head = strings.ToLower(head)
	return strings.HasPrefix(head, "<!doctype") ||
		strings.HasPrefix(head, "<html")
}

// Truncate shortens s to at most maxLen bytes, appending "..." if it was
// truncated. Safe for logging previews of long strings.
func Truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
