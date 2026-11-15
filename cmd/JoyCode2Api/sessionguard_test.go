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

// TestSessionGuardDormantByDefault runs a production-mode binary with no
// session material and verifies the guard is dormant: /v1/* works normally
// (200), identical to an ungated build. The gate only engages once grant
// material exists (token import or manifest sync), which is covered by the
// pkg/sessionguard unit tests — the production binary embeds only the public
// key, so no test process can mint a token or manifest for it — by design.
func TestSessionGuardDormantByDefault(t *testing.T) {
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
	// Point the manifest sync at a local dead endpoint so the production
	// binary never attempts an external fetch (which could hang behind
	// proxies and stall the request path).
	cmd.Env = append(testCommandEnv(t, home), "JOYCODE_SYNC_URL=http://127.0.0.1:1/none")
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

	// /v1/models must work: dormant guard does not block anything.
	// Retry: first upstream call can be slow on a cold build.
	var code int
	deadline = time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(base + "/v1/models")
		if err == nil {
			code = resp.StatusCode
			readBody(t, resp)
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if code == 0 {
		t.Fatalf("v1/models request kept failing")
	}
	if code != http.StatusOK {
		t.Errorf("v1/models status = %d, want 200 (dormant guard must not block)", code)
	}

	// Startup log must show the dormant state (not "armed"). log output is
	// rotated into the HOME-based log dir.
	stderrLog := readServeLog(t, filepath.Join(home, ".joycode-proxy", "logs", "stderr.log"))
	if !strings.Contains(stderrLog, "dormant") {
		t.Errorf("startup log should mention dormant guard, got:\n%s", stderrLog)
	}

	// A garbage token import must still be rejected by the hidden command
	// (it would arm the guard with nonsense otherwise).
	importCmd := exec.Command(bin, "session-data", "AAAAgarbageAAAA")
	importCmd.Env = append(testCommandEnv(t, home), "JOYCODE_SESSION_MGMT=1")
	if out, err := importCmd.CombinedOutput(); err == nil {
		t.Errorf("garbage token import should fail, got success: %s", out)
	}

	// And after the failed import the guard must still be dormant.
	resp2, err := client.Get(base + "/v1/models")
	if err != nil {
		t.Fatalf("v1/models after garbage import: %v", err)
	}
	readBody(t, resp2)
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("v1/models after garbage import status = %d, want 200", resp2.StatusCode)
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
