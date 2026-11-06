package sessionguard

import (
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

// devKey installs a fresh test keypair and returns its private half.
func devKey(t *testing.T) (ed25519.PrivateKey, ed25519.PublicKey) {
	t.Helper()
	return testKeyPair(t)
}

type fakeStore struct {
	m map[string]string
}

func newFakeStore() *fakeStore { return &fakeStore{m: map[string]string{}} }

func (f *fakeStore) get(k string) string   { return f.m[k] }
func (f *fakeStore) set(k, v string) error { f.m[k] = v; return nil }

func (f *fakeStore) verifier() *Verifier {
	return NewVerifier(f.get, f.set)
}

func TestTokenRoundTrip(t *testing.T) {
	priv, _ := devKey(t)
	fp := uint64(0x1234567890abcdef)
	tok := IssueToken(priv, fp, time.Now().Add(24*time.Hour))

	decoded, err := decodeToken(tok)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.MachineFP != fp {
		t.Errorf("fp = %#x, want %#x", decoded.MachineFP, fp)
	}
	if time.Until(decoded.Expiry) < 23*time.Hour {
		t.Errorf("expiry too soon: %v", decoded.Expiry)
	}
}

func TestVerifyExpired(t *testing.T) {
	priv, _ := devKey(t)
	fp := MachineFP()
	fs := newFakeStore()
	tok := IssueToken(priv, fp, time.Now().Add(-time.Hour))
	if err := fs.set(settingToken, tok); err != nil {
		t.Fatal(err)
	}
	v := fs.verifier()
	if err := v.Verify(); err != ErrSessionExpired {
		t.Errorf("Verify = %v, want ErrSessionExpired", err)
	}
}

func TestVerifyWrongMachine(t *testing.T) {
	priv, _ := devKey(t)
	fs := newFakeStore()
	tok := IssueToken(priv, uint64(0xdeadbeef), time.Now().Add(24*time.Hour))
	if err := fs.set(settingToken, tok); err != nil {
		t.Fatal(err)
	}
	v := fs.verifier()
	if err := v.Verify(); err != ErrSessionMismatch {
		t.Errorf("Verify = %v, want ErrSessionMismatch", err)
	}
}

func TestVerifyValid(t *testing.T) {
	priv, _ := devKey(t)
	fs := newFakeStore()
	tok := IssueToken(priv, MachineFP(), time.Now().Add(24*time.Hour))
	if err := fs.set(settingToken, tok); err != nil {
		t.Fatal(err)
	}
	v := fs.verifier()
	if err := v.Verify(); err != nil {
		t.Errorf("Verify = %v, want nil", err)
	}
}

func TestImportRejectsForeignMachine(t *testing.T) {
	priv, _ := devKey(t)
	fs := newFakeStore()
	v := fs.verifier()
	tok := IssueToken(priv, uint64(0xdeadbeef), time.Now().Add(24*time.Hour))
	if err := v.Import(tok); err == nil {
		t.Fatal("Import accepted a token for another machine")
	}
	if fs.get(settingToken) != "" {
		t.Error("foreign token was persisted")
	}
}

func TestImportRoundTrip(t *testing.T) {
	priv, _ := devKey(t)
	fs := newFakeStore()
	v := fs.verifier()
	tok := IssueToken(priv, MachineFP(), time.Now().Add(24*time.Hour))
	if err := v.Import(tok); err != nil {
		t.Fatalf("Import: %v", err)
	}
	// Fresh verifier reads from the same store
	v2 := fs.verifier()
	if err := v2.Verify(); err != nil {
		t.Errorf("Verify after import: %v", err)
	}
}

func TestClockBackstep(t *testing.T) {
	priv, _ := devKey(t)
	fs := newFakeStore()
	tok := IssueToken(priv, MachineFP(), time.Now().Add(24*time.Hour))
	if err := fs.set(settingToken, tok); err != nil {
		t.Fatal(err)
	}
	// Simulate a previous high-water mark 10 days in the future (clock rolled back)
	if err := fs.set(settingHighWater, fmtSprint(time.Now().Add(240*time.Hour).Unix())); err != nil {
		t.Fatal(err)
	}
	v := fs.verifier()
	if err := v.Verify(); err != ErrSessionExpired {
		t.Errorf("Verify = %v, want ErrSessionExpired for big backstep", err)
	}
}

func TestGarbageTokenRejected(t *testing.T) {
	fs := newFakeStore()
	if err := fs.set(settingToken, "not-a-token"); err != nil {
		t.Fatal(err)
	}
	v := fs.verifier()
	if err := v.Verify(); err != ErrSessionExpired {
		t.Errorf("Verify = %v, want ErrSessionExpired for garbage", err)
	}
}

func TestMuxBlocksWhenExpired(t *testing.T) {
	fs := newFakeStore()
	v := fs.verifier()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	})
	h := v.Mux(inner)

	// No token -> blocked with disguised 401
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/chat/completions", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, want 401", rec.Code)
	}
	if body := rec.Body.String(); body == "ok" {
		t.Error("request reached handler despite invalid session")
	}

	// Non-/v1 path passes through (dashboard unaffected)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/health", nil))
	if rec.Code != 200 {
		t.Errorf("non-v1 path blocked: code = %d", rec.Code)
	}
}

func TestMuxPassesWhenValid(t *testing.T) {
	priv, _ := devKey(t)
	fs := newFakeStore()
	v := fs.verifier()
	if err := v.Import(IssueToken(priv, MachineFP(), time.Now().Add(time.Hour))); err != nil {
		t.Fatalf("import: %v", err)
	}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	})
	h := v.Mux(inner)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/messages", nil))
	if rec.Code != 200 || rec.Body.String() != "ok" {
		t.Errorf("valid session blocked: code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestDiagCodeRoundTrip(t *testing.T) {
	code := DiagCode()
	if len(code) != 13 {
		t.Fatalf("diag code len = %d", len(code))
	}
	fp, err := DecodeDiagCode(code)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if fp != MachineFP() {
		t.Errorf("round-trip fp = %#x, want %#x", fp, MachineFP())
	}
}

func TestSignatureFromWrongKeyRejected(t *testing.T) {
	// Token signed by a random key must fail verification.
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	tok := IssueToken(other, MachineFP(), time.Now().Add(24*time.Hour))
	if _, err := decodeToken(tok); err == nil {
		t.Error("token signed by wrong key verified")
	}
}

func fmtSprint(v int64) string {
	return strconv.FormatInt(v, 10)
}
