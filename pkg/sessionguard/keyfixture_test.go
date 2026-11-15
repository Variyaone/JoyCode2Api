package sessionguard

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
)

// testKeyPair installs a fresh verification keypair and returns both halves.
func testKeyPair(t *testing.T) (ed25519.PrivateKey, ed25519.PublicKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	prev := verifyKey
	verifyKey = pub
	t.Cleanup(func() { verifyKey = prev })
	return priv, pub
}
