package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// buildProdTestBinary builds the server WITHOUT the testmode tag, so the
// session integrity gate is compiled in exactly as in release builds.
func buildProdTestBinary(t *testing.T) string {
	t.Helper()
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	binPath := filepath.Join(t.TempDir(), "joycode-proxy-prodtest"+suffix)
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"+suffix), "build", "-o", binPath, ".")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("prod build failed: %s: %v", string(output), err)
	}
	return binPath
}

// TestSessionGuardBlocksV1 runs a production-mode binary with no session
// token and verifies the gate: /v1/* must return the disguised 401
// (upstream-credential-style error), while dashboard and non-/v1 routes stay
// reachable.
//
// The companion "allow" path (valid token passes the gate) is covered by the
// unit tests in pkg/sessionguard, which inject test keys; the production
// binary itself only embeds the public key, so no test process can mint a
// token for it — by design.
func TestSessionGuardBlocksV1(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	bin := buildProdTestBinary(t)
	port := freePort(t)
	home := t.TempDir()
	logPath := filepath.Join(home, "server.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("create log: %v", err)
	}
	defer logFile.Close()

	cmd := exec.Command(bin, "serve", "--host", "127.0.0.1", "--port", fmt.Sprintf("%d", port),
		"--skip-validation", "--ptkey", "test", "--userid", "test", "--tls=false")
	cmd.Env = testCommandEnv(t, home)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	defer func() {
		select {
		case <-done:
		default:
			cmd.Process.Kill()
			<-done
		}
	}()

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(10 * time.Second)
	ready := false
	for time.Now().Before(deadline) && !ready {
		resp, err := client.Get(base + "/api/health")
		if err == nil {
			resp.Body.Close()
			ready = resp.StatusCode == http.StatusOK
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ready {
		t.Fatalf("server never became ready; log:\n%s", readServeLog(t, logPath))
	}

	// /v1/models must be blocked with the disguised credential error.
	resp, err := client.Get(base + "/v1/models")
	if err != nil {
		t.Fatalf("v1 models request: %v", err)
	}
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("v1/models status = %d, want 401", resp.StatusCode)
	}
	if !strings.Contains(body, "登录") {
		t.Errorf("v1/models body should look like a login error, got: %s", body)
	}

	// /v1/messages must also be blocked.
	resp, err = client.Post(base+"/v1/messages", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("v1 messages request: %v", err)
	}
	readBody(t, resp)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("v1/messages status = %d, want 401", resp.StatusCode)
	}

	// Dashboard and health routes must stay reachable.
	for _, path := range []string{"/health", "/api/health"} {
		resp, err := client.Get(base + path)
		if err != nil {
			t.Fatalf("get %s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s status = %d, want 200 (guard must not block non-/v1)", path, resp.StatusCode)
		}
	}

	// A garbage token imported via the hidden command must be rejected.
	importCmd := exec.Command(bin, "session-data", "AAAAgarbageAAAA")
	importCmd.Env = append(testCommandEnv(t, home), "JOYCODE_SESSION_MGMT=1")
	if out, err := importCmd.CombinedOutput(); err == nil {
		t.Errorf("garbage token import should fail, got success: %s", out)
	}
	resp, _ = client.Get(base + "/v1/models")
	readBody(t, resp)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("v1/models after garbage import status = %d, want 401", resp.StatusCode)
	}
}

// TestSessionGuardDevIssueUnavailable verifies the dev self-issue path is
// inert on a production build (production public key embedded).
func TestSessionGuardDevIssueUnavailable(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}
	bin := buildProdTestBinary(t)
	home := t.TempDir()
	cmd := exec.Command(bin, "session-diag", "--self-issue")
	cmd.Env = testCommandEnv(t, home)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("self-issue unexpectedly succeeded on production build: %s", out)
	}
	if !strings.Contains(string(out), "unavailable") {
		t.Errorf("expected 'unavailable' error, got: %s", out)
	}
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return sb.String()
}

func readServeLog(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return "(unreadable)"
	}
	return string(b)
}
