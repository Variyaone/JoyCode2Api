package dashboard

import (
	"log"
	"log/slog"
	"net/http"
	"time"
)

// Self-exit mechanism for no-admin upgrades (2026-11-12).
//
// Background: the production proxy runs as SYSTEM (Session 0, spawned by the
// cc-connect-proxy-boot watchdog task). A normal user session cannot kill it
// (taskkill/Stop-Process -> access denied), which previously meant a binary
// swap only took effect at the next natural restart. But a process can
// always exit itself, and the SYSTEM watchdog re-launches the (already
// swapped) exe within minutes. POST /api/exit closes that loop from any
// shell without admin rights.
//
// Security: JWT-protected like every other dashboard endpoint (middleware
// whitelist does NOT include /api/exit). If no password is configured, the
// proxy is single-user localhost anyway and the request proceeds; a request
// without a valid token against a password-protected instance 401s before
// reaching this handler. Localhost-only source addresses are enforced on top.

// ExitFunc is called to terminate the serve process. It is set by the
// server (see cmd/JoyCode2Api/serve.go) and left nil in tests.
var ExitFunc = func() {}

// handleExit POSTs a graceful self-exit: flush the response first, then
// terminate the serve process so the SYSTEM watchdog restarts it from the
// (new) binary. The supervisor's own restart loop, if present, achieves the
// same effect.
func (h *Handler) handleExit(w http.ResponseWriter, r *http.Request) {
	setCors(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !isLocalRequest(r) {
		writeError(w, http.StatusForbidden, "exit is only allowed from localhost")
		return
	}

	slog.Info("api: exit requested — shutting down serve for upgrade")
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":      true,
		"message": "exiting; watchdog/supervisor will restart with the swapped binary",
	})

	// Give the response time to flush before the process dies.
	go func() {
		time.Sleep(500 * time.Millisecond)
		log.Println("[api-exit] terminating serve process")
		ExitFunc()
	}()
}

// isLocalRequest reports whether the peer is loopback.
func isLocalRequest(r *http.Request) bool {
	host := r.RemoteAddr
	for i := len(host) - 1; i >= 0; i-- {
		if host[i] == ':' {
			host = host[:i]
			break
		}
	}
	return host == "127.0.0.1" || host == "::1" || host == "[::1]" || host == "localhost"
}
