package dashboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHandleExit_RequiresPost(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodGet, "/api/exit", nil)
	req.RemoteAddr = "127.0.0.1:55555"
	w := httptest.NewRecorder()
	h.handleExit(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /api/exit: got %d, want 405", w.Code)
	}
}

func TestHandleExit_RejectsRemotePeer(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodPost, "/api/exit", bytes.NewReader(nil))
	req.RemoteAddr = "10.20.30.40:55555"
	w := httptest.NewRecorder()
	h.handleExit(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("remote POST /api/exit: got %d, want 403", w.Code)
	}
}

func TestHandleExit_LocalPostTriggersExit(t *testing.T) {
	exited := make(chan struct{}, 1)
	orig := ExitFunc
	ExitFunc = func() { exited <- struct{}{} }
	defer func() { ExitFunc = orig }()

	h := &Handler{}
	req := httptest.NewRequest(http.MethodPost, "/api/exit", bytes.NewReader(nil))
	req.RemoteAddr = "127.0.0.1:55555"
	w := httptest.NewRecorder()
	h.handleExit(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("local POST /api/exit: got %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response not JSON: %v", err)
	}
	if resp["ok"] != true {
		t.Fatalf("ok field not true: %v", resp)
	}

	select {
	case <-exited:
	case <-timeAfter():
		t.Fatal("ExitFunc was not called after local POST /api/exit")
	}
}

// timeAfter waits past the 500ms response-flush delay in handleExit.
func timeAfter() <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		time.Sleep(2 * time.Second)
		close(ch)
	}()
	return ch
}
