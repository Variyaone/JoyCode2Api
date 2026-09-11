package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// freePort asks the kernel for a free open port that is ready for use.
func freePort(t *testing.T) int {
	t.Helper()
	for {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("get free port: %v", err)
		}
		port := ln.Addr().(*net.TCPAddr).Port
		ln.Close()
		if port != 34891 {
			return port
		}
	}
}

func startTestServer(t *testing.T) string {
	t.Helper()
	bin := buildTestBinary(t)
	port := freePort(t)
	home := t.TempDir()
	logPath := filepath.Join(home, "server.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("create server log: %v", err)
	}
	t.Cleanup(func() {
		logFile.Close()
		if t.Failed() {
			output, _ := os.ReadFile(logPath)
			t.Logf("server output:\n%s", output)
		}
	})

	cmd := exec.Command(bin, "serve", "--host", "127.0.0.1", "--port", fmt.Sprintf("%d", port), "--skip-validation", "--ptkey", "test", "--userid", "test", "--tls=false")
	cmd.Env = testCommandEnv(t, home)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	// Register process cleanup only after Start succeeds. Wait runs exactly once.
	done := make(chan struct{})
	var waitErr error
	go func() {
		waitErr = cmd.Wait()
		close(done)
	}()
	t.Cleanup(func() {
		select {
		case <-done:
		default:
			if err := cmd.Process.Kill(); err != nil && err != os.ErrProcessDone {
				t.Errorf("kill test server: %v", err)
			}
			<-done
		}
	})

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	client := &http.Client{Timeout: 500 * time.Millisecond}
	defer client.CloseIdleConnections()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-done:
			t.Fatalf("server exited before becoming ready: %v", waitErr)
		default:
		}
		resp, err := client.Get(base + "/api/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return base
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("test server did not become ready within 10s")
	return ""
}

func TestDashboardEndpoints(t *testing.T) {
	base := startTestServer(t)

	// Test health endpoint
	t.Run("health", func(t *testing.T) {
		resp, err := http.Get(base + "/api/health")
		if err != nil {
			t.Fatalf("health request: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}
		var m map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&m)
		if m["status"] != "ok" {
			t.Errorf("status = %v, want ok", m["status"])
		}
	})

	// Test add account
	t.Run("add_account", func(t *testing.T) {
		body := `{"api_key":"test-integration","pt_key":"test-pt","user_id":"test-user","is_default":true}`
		resp, err := http.Post(base+"/api/accounts", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("add account: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}
	})

	// Test list accounts
	t.Run("list_accounts", func(t *testing.T) {
		resp, err := http.Get(base + "/api/accounts")
		if err != nil {
			t.Fatalf("list accounts: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		var m map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&m)
		accounts, ok := m["accounts"].([]interface{})
		if !ok {
			t.Fatalf("response missing 'accounts' array: %v", m)
		}
		if len(accounts) < 1 {
			t.Errorf("accounts len = %d, want >= 1", len(accounts))
		}
	})

	// Test models
	t.Run("models", func(t *testing.T) {
		resp, err := http.Get(base + "/api/models")
		if err != nil {
			t.Fatalf("models: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		var m map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&m)
		models, ok := m["models"].([]interface{})
		if !ok {
			t.Fatalf("response missing 'models' array: %v", m)
		}
		if len(models) == 0 {
			t.Error("expected at least 1 model")
		}
	})

	// Test stats
	t.Run("stats", func(t *testing.T) {
		resp, err := http.Get(base + "/api/stats")
		if err != nil {
			t.Fatalf("stats: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}
	})

	// Test settings
	t.Run("settings", func(t *testing.T) {
		resp, err := http.Get(base + "/api/settings")
		if err != nil {
			t.Fatalf("settings: %v", err)
		}
		defer resp.Body.Close()
		var m map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&m)
		if _, ok := m["settings"]; !ok {
			t.Error("missing settings field")
		}
	})

	// Test delete account
	t.Run("delete_account", func(t *testing.T) {
		req, _ := http.NewRequest("DELETE", base+"/api/accounts/test-integration", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("delete: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}
	})
}

func TestStaticFileServing(t *testing.T) {
	base := startTestServer(t)

	// Test index.html
	t.Run("index_html", func(t *testing.T) {
		resp, err := http.Get(base + "/")
		if err != nil {
			t.Fatalf("get index: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}
	})

	// Test SPA fallback
	t.Run("spa_fallback", func(t *testing.T) {
		resp, err := http.Get(base + "/accounts")
		if err != nil {
			t.Fatalf("get accounts page: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}
	})

	// Test favicon
	t.Run("favicon", func(t *testing.T) {
		resp, err := http.Get(base + "/favicon.svg")
		if err != nil {
			t.Fatalf("get favicon: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}
	})

	// Test /health (OpenAI handler)
	t.Run("health_endpoint", func(t *testing.T) {
		resp, err := http.Get(base + "/health")
		if err != nil {
			t.Fatalf("get health: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}
	})
}

func TestOpenAPIEndpoints(t *testing.T) {
	base := startTestServer(t)

	// Test /v1/models (OpenAI endpoint)
	t.Run("v1_models", func(t *testing.T) {
		resp, err := http.Get(base + "/v1/models")
		if err != nil {
			t.Fatalf("v1 models: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		var result struct {
			Object string `json:"object"`
			Data   []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decode models: %v", err)
		}
		if result.Object != "list" || len(result.Data) != 1 || result.Data[0].ID != "test-model" {
			t.Errorf("unexpected models response: %+v", result)
		}
	})

	// Test /health (OpenAI handler)
	t.Run("health", func(t *testing.T) {
		resp, err := http.Get(base + "/health")
		if err != nil {
			t.Fatalf("health: %v", err)
		}
		defer resp.Body.Close()
		var m map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&m)
		if m["status"] != "ok" {
			t.Errorf("status = %v, want ok", m["status"])
		}
	})
}
