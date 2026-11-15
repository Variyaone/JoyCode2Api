//go:build testmode

package sessionguard

import (
	"crypto/ed25519"
	"crypto/sha256"
	"time"
)

// SelfIssueDev mints a short-lived token signed with the deterministic dev
// key. Only available in testmode builds; production builds compile this out
// entirely, so self-issue cannot bypass production gating.
func SelfIssueDev(ttl time.Duration) (string, error) {
	h := sha256.Sum256([]byte("joycode-dev-session-key"))
	priv := ed25519.NewKeyFromSeed(h[:])
	return IssueToken(priv, MachineFP(), now().Add(ttl)), nil
}
