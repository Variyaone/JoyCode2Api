package dashboard

import (
	_ "embed"
	"net/http"
)

// benchmarkSnapshot contains public scores only; serving the dashboard never
// contacts benchmark sites or sends JoyCode credentials to them.
//
//go:embed data/benchmarks.json
var benchmarkSnapshot []byte

func (h *Handler) handleModelBenchmarks(w http.ResponseWriter, r *http.Request) {
	setCors(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(benchmarkSnapshot)
}
