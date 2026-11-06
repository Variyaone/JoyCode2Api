package sessionguard

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

// manifestLayout: version(1) | validUntil(8, unix seconds) | sig(64).
// The manifest is the operator's global kill switch: a small signed blob
// published at a fixed URL. Clients fetch it periodically; a valid manifest
// extends the local session to its validUntil date. Revoking access means
// publishing a manifest with a past date (or removing the manifest), and all
// copies stop working within one fetch cycle plus the offline grace window.
const (
	manifestVersion = 1
	manifestHeader  = 1 + 8
	manifestTotal   = manifestHeader + ed25519.SignatureSize

	// manifestGrace is how long the last good manifest keeps working when
	// the fetch fails (offline machine, transient network). Prevents a
	// network blip from locking everyone out, while bounding how long a
	// revoked copy can survive without reaching the manifest URL.
	manifestGrace = 14 * 24 * time.Hour

	// manifestStaleAfter forces at least one successful fetch inside this
	// window or the session is treated as expired. Mirrors manifestGrace.
	manifestStaleAfter = 14 * 24 * time.Hour

	// manifestFetchPeriod is the background refresh interval.
	manifestFetchPeriod = 24 * time.Hour
)

// Setting keys for the manifest cache. Deliberately mundane names.
const (
	settingManifest      = "auth_sync_cache"
	settingManifestStamp = "auth_sync_stamp"
)

// DefaultManifestURL is where the signed manifest is published. The operator
// controls this; it can point at any static file host (gist raw URL, CDN).
// Overridable via the JOYCODE_SYNC_URL environment variable.
var DefaultManifestURL = "https://gist.githubusercontent.com/REPLACE_ME/manifest"

// ManifestURL returns the effective manifest endpoint.
func ManifestURL() string {
	if u := strings.TrimSpace(os.Getenv("JOYCODE_SYNC_URL")); u != "" {
		return u
	}
	return DefaultManifestURL
}

// DecodeManifest parses and verifies a base64 manifest, returning its
// validUntil date.
func DecodeManifest(s string) (time.Time, error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return time.Time{}, err
	}
	if len(raw) != manifestTotal {
		return time.Time{}, errors.New("bad manifest length")
	}
	if raw[0] != manifestVersion {
		return time.Time{}, errors.New("unsupported manifest version")
	}
	if !ed25519.Verify(verifyKey, raw[:manifestHeader], raw[manifestHeader:]) {
		return time.Time{}, errors.New("manifest signature verification failed")
	}
	return time.Unix(int64(binary.BigEndian.Uint64(raw[1:9])), 0), nil
}

// IssueManifest signs a manifest with the given private key (licgen and
// tests). Returns the base64 blob to publish.
func IssueManifest(priv ed25519.PrivateKey, validUntil time.Time) string {
	var buf [manifestHeader]byte
	buf[0] = manifestVersion
	binary.BigEndian.PutUint64(buf[1:9], uint64(validUntil.Unix()))
	sig := ed25519.Sign(priv, buf[:])
	out := make([]byte, 0, manifestTotal)
	out = append(out, buf[:]...)
	out = append(out, sig...)
	return base64.RawURLEncoding.EncodeToString(out)
}

// applyManifest validates a fetched manifest and stores it. A manifest whose
// validUntil is in the past is still stored (the operator's revocation), but
// Verify then treats the session as expired. Returns the effective date.
func (v *Verifier) applyManifest(blob string) (time.Time, error) {
	until, err := DecodeManifest(blob)
	if err != nil {
		return time.Time{}, err
	}
	if err := v.set(settingManifest, strings.TrimSpace(blob)); err != nil {
		return time.Time{}, fmt.Errorf("store manifest: %w", err)
	}
	_ = v.set(settingManifestStamp, fmt.Sprintf("%d", now().Unix()))
	return until, nil
}

// cachedManifest returns the stored manifest's validUntil, when present and
// verifiable.
func (v *Verifier) cachedManifest() (time.Time, bool) {
	raw := v.get(settingManifest)
	if raw == "" {
		return time.Time{}, false
	}
	until, err := DecodeManifest(raw)
	if err != nil {
		return time.Time{}, false
	}
	return until, true
}

// manifestFresh reports whether a successful fetch happened inside the
// staleness window.
func (v *Verifier) manifestFresh() bool {
	raw := v.get(settingManifestStamp)
	if raw == "" {
		return false
	}
	var stamp int64
	if _, err := fmt.Sscanf(raw, "%d", &stamp); err != nil {
		return false
	}
	return now().Sub(time.Unix(stamp, 0)) < manifestStaleAfter
}

// fetchManifest downloads and verifies the manifest from the sync URL.
// Returns the blob on success. Network errors are returned; the caller
// decides whether to keep serving on the cached copy.
func (v *Verifier) fetchManifest(ctx context.Context) (string, error) {
	url := ManifestURL()
	if url == "" || strings.Contains(url, "REPLACE_ME") {
		return "", errors.New("manifest sync URL not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("manifest endpoint status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	if err != nil {
		return "", err
	}
	blob := strings.TrimSpace(string(body))
	if _, err := DecodeManifest(blob); err != nil {
		return "", err
	}
	return blob, nil
}

// SyncManifest runs one fetch-and-apply cycle. Called from the background
// refresher and directly at startup.
func (v *Verifier) SyncManifest(ctx context.Context) error {
	blob, err := v.fetchManifest(ctx)
	if err != nil {
		return err
	}
	_, err = v.applyManifest(blob)
	return err
}

// StartManifestSync launches the background refresher: one immediate sync,
// then every manifestFetchPeriod until ctx is cancelled. Errors are logged
// at warn level and never fatal — the cached manifest keeps serving.
func (v *Verifier) StartManifestSync(ctx context.Context) {
	go func() {
		v.SyncManifest(ctx)
		t := time.NewTicker(manifestFetchPeriod)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := v.SyncManifest(ctx); err != nil {
					slog.Warn("sessionguard: manifest sync failed", "error", err)
				}
			}
		}
	}()
}
