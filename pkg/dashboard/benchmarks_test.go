package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/vibe-coding-labs/JoyCode2Api/pkg/auth"
	"github.com/vibe-coding-labs/JoyCode2Api/pkg/joycode"
)

func TestBenchmarkSnapshot(t *testing.T) {
	var data struct {
		SchemaVersion int                                 `json:"schema_version"`
		CollectedAt   string                              `json:"collected_at"`
		Sources       []struct{ ID, URL, Version string } `json:"sources"`
		Models        []struct {
			ID, Mapping, Note string
			Results           []struct {
				SourceID     string `json:"source_id"`
				PublicModel  string `json:"public_model"`
				Variant, URL string
				Score        *float64
			} `json:"results"`
		} `json:"models"`
	}
	if err := json.Unmarshal(benchmarkSnapshot, &data); err != nil {
		t.Fatal(err)
	}
	if data.SchemaVersion != 1 {
		t.Fatal("unexpected schema version")
	}
	if _, err := time.Parse("2006-01-02", data.CollectedAt); err != nil {
		t.Fatal(err)
	}
	sources := map[string]bool{}
	checkURL := func(raw string) {
		t.Helper()
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			t.Fatalf("invalid source URL: %s", raw)
		}
	}
	for _, source := range data.Sources {
		if sources[source.ID] || source.Version == "" {
			t.Fatal("invalid/duplicate source")
		}
		sources[source.ID] = true
		checkURL(source.URL)
	}
	seen := map[string]bool{}
	for _, model := range data.Models {
		if seen[model.ID] || model.Note == "" {
			t.Fatalf("invalid/duplicate model: %s", model.ID)
		}
		seen[model.ID] = true
		variants := map[string]bool{}
		for _, result := range model.Results {
			if !sources[result.SourceID] || result.Variant == "" || result.PublicModel == "" {
				t.Fatalf("missing provenance for %s", model.ID)
			}
			checkURL(result.URL)
			key := result.SourceID + result.PublicModel + result.Variant
			if variants[key] {
				t.Fatalf("duplicate variant for %s", model.ID)
			}
			variants[key] = true
			if result.Score != nil && (*result.Score <= 0 || *result.Score > 100) {
				t.Fatal("invalid AA snapshot score")
			}
		}
		if model.ID == "DeepSeek-V4-Pro" && (model.Mapping != "version_ambiguous" || len(model.Results) != 2) {
			t.Fatal("DeepSeek versions must stay separate")
		}
		if (model.ID == "Kimi-K3-jcloud" || model.ID == "GLM-5.2-jcloud") && model.Mapping != "deployment_reference" {
			t.Fatal("jcloud must not claim identical deployment")
		}
		if model.Mapping == "unverified" && len(model.Results) != 0 {
			t.Fatal("unverified scores must remain absent")
		}
	}
	for _, model := range joycode.Models {
		if !seen[model] {
			t.Errorf("model missing from snapshot: %s", model)
		}
	}
}

func TestModelBenchmarksEndpoint(t *testing.T) {
	h, s := setupTestHandler(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	for _, tc := range []struct {
		method string
		code   int
	}{{"GET", 200}, {"OPTIONS", 204}, {"POST", 405}} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(tc.method, "/api/model-benchmarks", nil))
		if w.Code != tc.code {
			t.Fatalf("%s: got %d", tc.method, w.Code)
		}
		if tc.method == "GET" && !json.Valid(w.Body.Bytes()) {
			t.Fatal("invalid response JSON")
		}
	}
	if err := s.SetSetting("auth_password_hash", "configured-test-password"); err != nil {
		t.Fatal(err)
	}
	protected := auth.JWTMiddleware(s, mux)
	w := httptest.NewRecorder()
	protected.ServeHTTP(w, httptest.NewRequest("GET", "/api/model-benchmarks", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatal("benchmark API bypasses dashboard authentication")
	}
}
