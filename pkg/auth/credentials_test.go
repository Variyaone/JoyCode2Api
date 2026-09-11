package auth

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// isolateCredentialEnv keeps both explicit and platform-default lookups in a
// temporary home. Tests that exercise discovery clear stateDBEnv separately.
func isolateCredentialEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("APPDATA", filepath.Join(home, "isolated-appdata"))
	t.Setenv(stateDBEnv, filepath.Join(home, "state.vscdb"))
	return home
}

func useSystemCredentialPath(t *testing.T) string {
	t.Helper()
	home := isolateCredentialEnv(t)
	// The fixed container path takes precedence over platform discovery and
	// cannot be redirected by HOME. Never read or modify an existing database.
	if _, err := os.Stat(containerStateDB); err == nil {
		t.Skip("container state database takes precedence over platform discovery")
	}
	t.Setenv(stateDBEnv, "")
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "JoyCode", "User", "globalStorage", "state.vscdb")
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), "JoyCode", "User", "globalStorage", "state.vscdb")
	case "linux":
		return filepath.Join(home, ".config", "JoyCode", "User", "globalStorage", "state.vscdb")
	default:
		t.Skip("platform credential discovery is unsupported on " + runtime.GOOS)
		return ""
	}
}

func TestLoadFromSystem_DatabaseNotFound(t *testing.T) {
	dbPath := useSystemCredentialPath(t)
	_, err := LoadFromSystem()
	if err == nil {
		t.Fatal("expected error for missing database")
	}
	if !strings.Contains(err.Error(), dbPath) {
		t.Errorf("error = %q, want missing database path %q", err, dbPath)
	}
}

func TestCredentials_Fields(t *testing.T) {
	creds := &Credentials{PtKey: "test-key", UserID: "test-user"}
	if creds.PtKey != "test-key" {
		t.Errorf("PtKey = %q, want test-key", creds.PtKey)
	}
	if creds.UserID != "test-user" {
		t.Errorf("UserID = %q, want test-user", creds.UserID)
	}
}

func TestLoadFromSystem_Integration(t *testing.T) {
	// Exercise platform discovery using synthetic credentials, not a real login.
	dbPath := useSystemCredentialPath(t)
	createTestDB(t, dbPath, `{"joyCoderUser":{"ptKey":"test-key","userId":"test-user"}}`)

	creds, err := LoadFromSystem()
	if err != nil {
		t.Fatalf("LoadFromSystem: %v", err)
	}
	if creds.PtKey != "test-key" || creds.UserID != "test-user" {
		t.Errorf("credentials = %+v, want synthetic test credentials", creds)
	}
}

func TestLoadFromSystem_WindowsAppDataFallback(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific APPDATA fallback")
	}
	useSystemCredentialPath(t)
	dbPath := filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Roaming", "JoyCode", "User", "globalStorage", "state.vscdb")
	t.Setenv("APPDATA", "")
	createTestDB(t, dbPath, `{"joyCoderUser":{"ptKey":"fallback-key","userId":"fallback-user"}}`)

	creds, err := LoadFromSystem()
	if err != nil {
		t.Fatalf("LoadFromSystem without APPDATA: %v", err)
	}
	if creds.PtKey != "fallback-key" || creds.UserID != "fallback-user" {
		t.Errorf("credentials = %+v, want fallback test credentials", creds)
	}
}
