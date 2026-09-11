package dashboard

import (
	"github.com/vibe-coding-labs/JoyCode2Api/pkg/auth"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCostsEndpoint(t *testing.T) {
	h, s := setupTestHandler(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	for _, tc := range []struct {
		method string
		status int
	}{{"GET", 200}, {"OPTIONS", 204}, {"POST", 405}} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(tc.method, "/api/costs", nil))
		if w.Code != tc.status {
			t.Fatal(w.Code)
		}
		if tc.method == "GET" {
			m := decodeJSON(t, w)
			if m["currency"] != "USD" || m["notice"] == "" {
				t.Fatal(m)
			}
		}
	}
	s.SetSetting("auth_password_hash", "configured")
	w := httptest.NewRecorder()
	auth.JWTMiddleware(s, mux).ServeHTTP(w, httptest.NewRequest("GET", "/api/costs", nil))
	if w.Code != 401 {
		t.Fatal("cost API unprotected")
	}
}
