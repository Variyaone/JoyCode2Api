package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"
)

func buildTestBinary(t *testing.T) string {
	t.Helper()
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	binPath := filepath.Join(t.TempDir(), "joycode-proxy-test"+suffix)
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"+suffix), "build", "-o", binPath, ".")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %s: %v", string(output), err)
	}
	return binPath
}

// testCommandEnv isolates local state and keeps upstream requests on loopback.
func testCommandEnv(t *testing.T, home string) []string {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api" || r.URL.Query().Get("functionId") != "joycode_modelList" {
			t.Errorf("unexpected upstream request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected upstream request", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"code":0,"data":[{"label":"test-model","modelId":"test-model","features":[]}]}`))
	}))
	t.Cleanup(upstream.Close)
	return append(os.Environ(),
		"HOME="+home,
		"USERPROFILE="+home,
		"APPDATA="+filepath.Join(home, "AppData", "Roaming"),
		"JOYCODE_STATE_DB="+filepath.Join(home, "missing-state.vscdb"),
		"JOYCODE_BASE_URL="+upstream.URL,
		"JOYCODE_SAAS_BASE_URL="+upstream.URL,
		"JOYCODE_COLOR_BASE_URL="+upstream.URL,
	)
}

func testDaemonCleanup(t *testing.T, bin, home string, env []string) func() {
	t.Helper()
	cleanup := func() {
		// Keep the test supervisor PID: Windows stop may remove its PID file
		// even if the detached process requires forced termination.
		var pid daemonPID
		data, err := os.ReadFile(filepath.Join(home, pidFileName))
		if os.IsNotExist(err) {
			return
		}
		if err != nil {
			t.Errorf("read test daemon PID: %v", err)
			return
		}
		if err := json.Unmarshal(data, &pid); err != nil || pid.PID <= 0 {
			t.Errorf("invalid test daemon PID file: %s", data)
			return
		}
		proc, err := os.FindProcess(pid.PID)
		if err != nil {
			t.Errorf("find test daemon: %v", err)
			return
		}
		defer proc.Release()

		cmd := exec.Command(bin, "daemon", "stop")
		cmd.Env = env
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("daemon stop failed: %s: %v", output, err)
		}
		if isProcessAlive(proc) {
			if err := killProcess(proc); err != nil {
				t.Errorf("kill test daemon: %v", err)
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Cleanup(cleanup)
	return cleanup
}

func TestDaemonStartStop(t *testing.T) {
	bin := buildTestBinary(t)
	home := t.TempDir()
	env := testCommandEnv(t, home)
	port := strconv.Itoa(freePort(t))
	stop := testDaemonCleanup(t, bin, home, env)

	// Start daemon
	cmd := exec.Command(bin, "daemon", "start", "--port", port, "--skip-validation")
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("daemon start failed: %s: %v", string(output), err)
	}
	if !containsStr(string(output), "Daemon") {
		t.Errorf("expected 'Daemon' in start output, got: %s", string(output))
	}

	// Wait for daemon to start
	time.Sleep(2 * time.Second)

	// Check status
	cmd = exec.Command(bin, "daemon", "status")
	cmd.Env = env
	output, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("daemon status failed: %s: %v", string(output), err)
	}
	if !containsStr(string(output), "running") {
		t.Errorf("expected 'running' in status, got: %s", string(output))
	}

	// Stop daemon
	stop()
}

func TestServiceHelpOutput(t *testing.T) {
	bin := buildTestBinary(t)

	tests := []struct {
		args   []string
		expect string
	}{
		{[]string{"service", "--help"}, "install"},
		{[]string{"service", "--help"}, "uninstall"},
		{[]string{"service", "--help"}, "status"},
		{[]string{"daemon", "--help"}, "start"},
		{[]string{"daemon", "--help"}, "stop"},
		{[]string{"daemon", "--help"}, "restart"},
		{[]string{"daemon", "--help"}, "status"},
		{[]string{"daemon", "--help"}, "logs"},
	}

	for _, tt := range tests {
		t.Run(tt.args[0]+"_"+tt.expect, func(t *testing.T) {
			cmd := exec.Command(bin, tt.args...)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("--help failed: %v", err)
			}
			if !containsStr(string(output), tt.expect) {
				t.Errorf("expected '%s' in help output, got: %s", tt.expect, string(output))
			}
		})
	}
}

func TestDaemonDoubleStart(t *testing.T) {
	bin := buildTestBinary(t)
	home := t.TempDir()
	env := testCommandEnv(t, home)
	port := strconv.Itoa(freePort(t))
	testDaemonCleanup(t, bin, home, env)

	// Start daemon first time
	cmd := exec.Command(bin, "daemon", "start", "--port", port, "--skip-validation")
	cmd.Env = env
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("first start failed: %s: %v", string(output), err)
	}
	time.Sleep(2 * time.Second)

	// Try starting again — should fail (SilenceErrors suppresses output)
	cmd = exec.Command(bin, "daemon", "start", "--port", port, "--skip-validation")
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Errorf("expected second start to fail, but succeeded. output: %s", string(output))
	}
}
