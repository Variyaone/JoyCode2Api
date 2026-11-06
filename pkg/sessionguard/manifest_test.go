package sessionguard

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestManifestRoundTrip verifies issue → apply → verify extends a session.
func TestManifestRoundTrip(t *testing.T) {
	priv, _ := devKey(t)
	fs := newFakeStore()
	v := fs.verifier()

	// Import a short-lived token (1 day).
	tok := IssueToken(priv, MachineFP(), now().Add(24*time.Hour))
	if err := v.Import(tok); err != nil {
		t.Fatalf("import: %v", err)
	}

	// Apply a manifest valid for 90 days.
	until := now().Add(90 * 24 * time.Hour)
	if _, err := v.applyManifest(IssueManifest(priv, until)); err != nil {
		t.Fatalf("applyManifest: %v", err)
	}

	if err := v.Verify(); err != nil {
		t.Fatalf("verify with manifest: %v", err)
	}
	d, ok := v.Status()
	if !ok {
		t.Fatal("status not ok")
	}
	if d < 89*24*time.Hour {
		t.Errorf("status = %s, want ~90 days", d)
	}
}

// TestManifestRevocation: a manifest with a past validUntil kills the session
// even though the local token is still valid.
func TestManifestRevocation(t *testing.T) {
	priv, _ := devKey(t)
	fs := newFakeStore()
	v := fs.verifier()

	tok := IssueToken(priv, MachineFP(), now().Add(30*24*time.Hour))
	if err := v.Import(tok); err != nil {
		t.Fatalf("import: %v", err)
	}
	if err := v.Verify(); err != nil {
		t.Fatalf("verify before revocation: %v", err)
	}

	// Revocation manifest: validUntil yesterday.
	if _, err := v.applyManifest(IssueManifest(priv, now().Add(-24*time.Hour))); err != nil {
		t.Fatalf("applyManifest: %v", err)
	}
	if err := v.Verify(); err == nil {
		t.Fatal("verify after revocation should fail")
	}
}

// TestManifestStaleness: an applied manifest must be re-synced within the
// staleness window; a stamp older than the window fails verification even if
// validUntil is still in the future.
func TestManifestStaleness(t *testing.T) {
	priv, _ := devKey(t)
	fs := newFakeStore()
	v := fs.verifier()

	tok := IssueToken(priv, MachineFP(), now().Add(24*time.Hour))
	if err := v.Import(tok); err != nil {
		t.Fatalf("import: %v", err)
	}

	until := now().Add(90 * 24 * time.Hour)
	if _, err := v.applyManifest(IssueManifest(priv, until)); err != nil {
		t.Fatalf("applyManifest: %v", err)
	}
	if err := v.Verify(); err != nil {
		t.Fatalf("fresh manifest verify: %v", err)
	}

	// Backdate the sync stamp beyond the window (unix seconds).
	old := now().Add(-(manifestStaleAfter + time.Hour))
	fs.m[settingManifestStamp] = formatUnix(old)
	if err := v.Verify(); err == nil {
		t.Fatal("stale manifest should fail verify")
	}
}

func formatUnix(t time.Time) string {
	return time.Unix(t.Unix(), 0).Format("2006-01-02 15:04:05")[:0] + itoa(t.Unix())
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// TestManifestTamper: a manifest signed by a different key must be rejected.
func TestManifestTamper(t *testing.T) {
	_, _ = devKey(t) // installs matching verify key
	fs := newFakeStore()
	v := fs.verifier()

	_, otherPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := v.applyManifest(IssueManifest(otherPriv, now().Add(90*24*time.Hour))); err == nil {
		t.Fatal("manifest signed by wrong key should be rejected")
	}
}

// TestSyncManifestHTTP: the fetch path applies a manifest served over HTTP.
func TestSyncManifestHTTP(t *testing.T) {
	priv, _ := devKey(t)
	blob := IssueManifest(priv, now().Add(180*24*time.Hour))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(blob + "\n"))
	}))
	defer srv.Close()

	fs := newFakeStore()
	v := fs.verifier()
	t.Setenv("JOYCODE_SYNC_URL", srv.URL)

	if err := v.SyncManifest(context.Background()); err != nil {
		t.Fatalf("SyncManifest: %v", err)
	}
	until, ok := v.cachedManifest()
	if !ok {
		t.Fatal("manifest not cached after sync")
	}
	got := time.Until(until)
	if got < 179*24*time.Hour {
		t.Errorf("cached until = %s away, want ~180 days", got)
	}
}

// TestSyncManifestBadEndpoint: fetch errors are reported, cache untouched.
func TestSyncManifestBadEndpoint(t *testing.T) {
	priv, _ := devKey(t)
	fs := newFakeStore()
	v := fs.verifier()
	t.Setenv("JOYCODE_SYNC_URL", "http://127.0.0.1:1/none")

	if err := v.SyncManifest(context.Background()); err == nil {
		t.Fatal("sync to dead endpoint should error")
	}

	// Pre-seeded good manifest survives a failed sync.
	if _, err := v.applyManifest(IssueManifest(priv, now().Add(30*24*time.Hour))); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, ok := v.cachedManifest(); !ok {
		t.Fatal("cached manifest lost after failed sync")
	}
}
