package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vibe-coding-labs/JoyCode2Api/pkg/store"
)

// Verify the /v1/v1 strip happens before mux routing (requestLogMiddleware order).
func TestDoubleV1Stripped(t *testing.T) {
	s, err := store.Open(t.TempDir() + string(rune('/')) + "test.db")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	var seen string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.URL.Path
	})
	mw := requestLogMiddleware(inner, s)
	req := httptest.NewRequest("POST", "/v1/v1/messages", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	mw.ServeHTTP(httptest.NewRecorder(), req)
	if seen != "/v1/messages" {
		t.Fatalf("expected /v1/messages, got %s", seen)
	}
	// non-doubled path untouched
	req2 := httptest.NewRequest("POST", "/v1/messages", strings.NewReader("{}"))
	req2.Header.Set("Content-Type", "application/json")
	mw.ServeHTTP(httptest.NewRecorder(), req2)
	if seen != "/v1/messages" {
		t.Fatalf("expected /v1/messages unchanged, got %s", seen)
	}
}
