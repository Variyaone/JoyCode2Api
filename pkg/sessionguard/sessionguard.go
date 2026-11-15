// Package sessionguard validates local session integrity for API access.
//
// It verifies Ed25519-signed tokens that bind a machine fingerprint to an
// expiry time. Only the verification public key ships in the binary; tokens
// are issued offline by the operator. Expired or mismatched sessions surface
// as ordinary upstream credential errors, not license errors.
package sessionguard

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Setting keys persisted in the store's settings table. Names are deliberately
// mundane; nothing here should read as a licensing scheme.
const (
	settingToken     = "auth_session_cache"
	settingHighWater = "auth_session_last_seen"
)

// Verification public key (Ed25519, 32 raw bytes). Tokens are issued against
// this key; the issuing private key never ships. Tests may swap it via
// swapVerifyKey (test helpers only, never compiled into release binaries).
var verifyKey = mustDecodeKey("ba9520df1c4b7050e77b33062f098e476ede22d95fe51fc96d2a2f9cf772f321")

// GuardEnabled reports whether session integrity gating is compiled into
// this build. Production builds compile it in; builds with -tags testmode
// compile it out.
func GuardEnabled() bool { return guardEnabled }

// tokenVersion distinguishes token format generations.
const tokenVersion = 1

// maxClockBackstep is how far the wall clock may run backwards relative to
// the high-water mark before the session is treated as invalid.
const maxClockBackstep = 48 * time.Hour

// tokenLayout: version(1) | expiry(8, unix seconds) | machineFP(8) | sig(64)
const (
	tokenHeaderLen = 1 + 8 + 8
	tokenSigLen    = ed25519.SignatureSize
	tokenTotalLen  = tokenHeaderLen + tokenSigLen
)

// ErrSessionExpired is the sentinel meaning "session no longer valid".
// Callers translate this into an upstream-style credential error.
var ErrSessionExpired = errors.New("session expired")

// ErrSessionMismatch means the token was issued for a different machine.
var ErrSessionMismatch = errors.New("session bound to another machine")

// Token is a decoded, unverified-or-verified token payload.
type Token struct {
	Expiry    time.Time
	MachineFP uint64
}

// Clock can be overridden in tests.
var now = time.Now

// Verifier checks tokens against the embedded public key.
type Verifier struct {
	mu        sync.Mutex
	fp        uint64
	token     Token
	haveToken bool
	loaded    bool
	get       func(string) string
	set       func(string, string) error
}

// NewVerifier builds a verifier for this machine using the store-backed
// settings accessor pair (same interface as store Get/SetSetting).
func NewVerifier(get func(string) string, set func(key, value string) error) *Verifier {
	return &Verifier{get: get, set: set}
}

// MachineFP returns a stable fingerprint for this machine: a hash of the
// hostname and the first non-loopback MAC address. Stable across restarts,
// different on every device.
func MachineFP() uint64 {
	h := sha256.New()
	h.Write([]byte(hostname()))
	for _, mac := range macs() {
		h.Write(mac)
	}
	return binary.BigEndian.Uint64(h.Sum(nil)[:8])
}

func hostname() string {
	hn, err := os.Hostname()
	if err != nil {
		return "unknown-host"
	}
	return hn
}

func macs() [][]byte {
	var out [][]byte
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagLoopback != 0 || len(ifc.HardwareAddr) == 0 {
			continue
		}
		out = append(out, ifc.HardwareAddr)
	}
	// Deterministic order regardless of interface enumeration order.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && bytesCompare(out[j-1], out[j]) > 0; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

func bytesCompare(a, b []byte) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	}
	return 0
}

// Verify checks the current session state. It returns nil when access is
// granted, ErrSessionExpired when a present grant has expired or been
// revoked, and ErrSessionMismatch when a present token was issued for
// another machine.
//
// Dormant until armed: when NO grant material exists at all (no synced
// manifest, no imported token) access is allowed — the guard is inert and
// deployment behaves identically to an ungated build. The moment any grant
// material appears, it governs: the manifest (once synced) fully takes over
// (renewal extends, revocation caps) and must stay fresh; without a manifest
// a locally imported machine-bound token applies. Revocation propagates
// within one sync cycle.
func (v *Verifier) Verify() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.ensureLoaded()

	now := now()
	// Clock-backstep protection: if wall time ran backwards past the stored
	// high-water mark beyond tolerance, treat as tampering.
	if hw, ok := v.highWater(); ok {
		if hw.Sub(now) > maxClockBackstep {
			return ErrSessionExpired
		}
	}

	// Manifest grant: once a manifest has ever been synced it fully governs
	// (renewal extends, revocation caps) and must stay fresh — a revocation
	// in the manifest propagates within one sync cycle.
	if until, ok := v.cachedManifest(); ok {
		if !v.manifestFresh() {
			return ErrSessionExpired
		}
		if now.After(until) {
			return ErrSessionExpired
		}
		v.bumpHighWater(now)
		return nil
	}

	// Local token grant: applies only when a token has been imported.
	if !v.haveToken {
		// Re-read in case a token was imported since the last check.
		v.reloadToken()
		if !v.haveToken {
			// Dormant: no grant material at all — allow.
			return nil
		}
	}
	if v.token.MachineFP != v.fp {
		return ErrSessionMismatch
	}
	if now.After(v.token.Expiry) {
		return ErrSessionExpired
	}
	v.bumpHighWater(now)
	return nil
}

// reloadToken re-reads the stored token, ignoring the loaded cache. Caller
// must hold v.mu.
func (v *Verifier) reloadToken() {
	raw := v.get(settingToken)
	v.haveToken = false
	if raw == "" {
		return
	}
	tok, err := decodeToken(raw)
	if err != nil {
		slog.Warn("sessionguard: cached session unreadable", "error", err)
		return
	}
	v.token = tok
	v.haveToken = true
}

func (v *Verifier) ensureLoaded() {
	if v.loaded {
		return
	}
	v.loaded = true
	v.fp = MachineFP()
	raw := v.get(settingToken)
	if raw == "" {
		return
	}
	tok, err := decodeToken(raw)
	if err != nil {
		slog.Warn("sessionguard: cached session unreadable", "error", err)
		return
	}
	v.token = tok
	v.haveToken = true
}

// Reload drops the cached state so the next Verify re-reads from the store.
func (v *Verifier) Reload() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.loaded = false
	v.haveToken = false
}

// Import validates and stores a token issued for this machine. Returns an
// error when the token is malformed, signed by the wrong key, or bound to a
// different machine.
func (v *Verifier) Import(token string) error {
	tok, err := decodeToken(strings.TrimSpace(token))
	if err != nil {
		return fmt.Errorf("invalid session data: %w", err)
	}
	fp := MachineFP()
	if tok.MachineFP != fp {
		return errors.New("session data not valid for this machine")
	}
	if err := v.set(settingToken, strings.TrimSpace(token)); err != nil {
		return fmt.Errorf("store session: %w", err)
	}
	v.mu.Lock()
	v.fp = fp
	v.token = tok
	v.haveToken = true
	v.loaded = true
	v.mu.Unlock()
	// Reset the high-water mark so a fresh import isn't tripped by it.
	_ = v.set(settingHighWater, encodeUint64(uint64(now().Unix())))
	return nil
}

// DiagCode returns a short human-friendly code identifying this machine for
// token issuance. It is the machine FP in a compact alphabet.
func DiagCode() string {
	return encodeBase32(MachineFP())
}

// Status returns remaining validity for logging/diagnostics (not exposed in
// any API response). Reflects the governing grant (manifest first, then local
// token); ok is false when no grant material exists (dormant guard — access
// is allowed) or the governing grant has expired.
func (v *Verifier) Status() (time.Duration, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.ensureLoaded()

	if until, ok := v.cachedManifest(); ok {
		d := time.Until(until)
		if d < 0 {
			return 0, false
		}
		return d, true
	}
	if v.haveToken && v.token.MachineFP == v.fp {
		d := time.Until(v.token.Expiry)
		if d < 0 {
			return 0, false
		}
		return d, true
	}
	return 0, false
}

func (v *Verifier) highWater() (time.Time, bool) {
	raw := v.get(settingHighWater)
	if raw == "" {
		return time.Time{}, false
	}
	var u uint64
	if _, err := fmt.Sscanf(raw, "%d", &u); err != nil {
		return time.Time{}, false
	}
	return time.Unix(int64(u), 0), true
}

func (v *Verifier) bumpHighWater(now time.Time) {
	// Only write when it advanced by a meaningful margin, to avoid hammering
	// the store on every request.
	if hw, ok := v.highWater(); !ok || now.Sub(hw) >= time.Hour {
		_ = v.set(settingHighWater, encodeUint64(uint64(now.Unix())))
	}
}

func encodeUint64(u uint64) string {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], u)
	return fmt.Sprintf("%d", u)
}

// decodeToken parses and cryptographically verifies a base64 token.
func decodeToken(s string) (Token, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return Token{}, err
	}
	if len(raw) != tokenTotalLen {
		return Token{}, errors.New("bad token length")
	}
	if raw[0] != tokenVersion {
		return Token{}, errors.New("unsupported token version")
	}
	expiry := binary.BigEndian.Uint64(raw[1:9])
	fp := binary.BigEndian.Uint64(raw[9:17])
	if !ed25519.Verify(verifyKey, raw[:tokenHeaderLen], raw[tokenHeaderLen:]) {
		return Token{}, errors.New("signature verification failed")
	}
	return Token{Expiry: time.Unix(int64(expiry), 0), MachineFP: fp}, nil
}

func mustDecodeKey(hexStr string) ed25519.PublicKey {
	if strings.HasPrefix(hexStr, "REPLACE_WITH") {
		// Dev/self-build fallback: deterministic key from a fixed seed so
		// local development and CI builds work end-to-end. Production builds
		// embed a real public key above.
		h := sha256.Sum256([]byte("joycode-dev-session-key"))
		priv := ed25519.NewKeyFromSeed(h[:])
		return priv.Public().(ed25519.PublicKey)
	}
	b, err := hex.DecodeString(hexStr)
	if err != nil || len(b) != ed25519.PublicKeySize {
		panic("sessionguard: bad embedded public key")
	}
	return ed25519.PublicKey(b)
}

// encodeBase32 renders the fp as 13 chars in Crockford-ish alphabet,
// collision-safe for humans reading it over IM.
func encodeBase32(fp uint64) string {
	const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUV" // base32 standard
	var sb strings.Builder
	for i := 0; i < 13; i++ {
		sb.WriteByte(alphabet[fp&31])
		fp >>= 5
	}
	return sb.String()
}

// DecodeDiagCode parses a diag code back into a machine FP.
func DecodeDiagCode(s string) (uint64, error) {
	const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUV" // base32 standard
	s = strings.ToUpper(strings.TrimSpace(s))
	if len(s) != 13 {
		return 0, errors.New("diag code must be 13 chars")
	}
	var fp uint64
	for i := len(s) - 1; i >= 0; i-- {
		idx := strings.IndexByte(alphabet, s[i])
		if idx < 0 {
			return 0, fmt.Errorf("bad char %q in diag code", s[i])
		}
		fp = fp<<5 | uint64(idx)
	}
	return fp, nil
}

// Mux wraps the mux so /v1/* API requests are gated on a valid session.
// When the session is invalid the response mimics an upstream credential
// failure: same status code and error shape the JoyCode upstream returns for
// an expired login, so clients see a familiar re-login prompt rather than a
// licensing error.
func (v *Verifier) Mux(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v1/") {
			next.ServeHTTP(w, r)
			return
		}
		if err := v.Verify(); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprintf(w, `{"error":{"message":"JoyCode 登录态已失效，请重新登录 JoyCode 后重试","type":"authentication_error"}}`)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// IssueToken signs a token with the given private key (used by the offline
// issuer, and by tests).
func IssueToken(priv ed25519.PrivateKey, machineFP uint64, expiry time.Time) string {
	var buf [tokenHeaderLen]byte
	buf[0] = tokenVersion
	binary.BigEndian.PutUint64(buf[1:9], uint64(expiry.Unix()))
	binary.BigEndian.PutUint64(buf[9:17], machineFP)
	sig := ed25519.Sign(priv, buf[:])
	out := make([]byte, 0, tokenTotalLen)
	out = append(out, buf[:]...)
	out = append(out, sig...)
	return base64.RawURLEncoding.EncodeToString(out)
}
