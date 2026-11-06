//go:build !testmode

package sessionguard

import (
	"errors"
	"time"
)

var errSelfIssueUnavailable = errors.New("self-issue unavailable: production session key active")

// SelfIssueDev always fails on production builds: only the public key is
// embedded, so no process can mint a valid token locally.
func SelfIssueDev(ttl time.Duration) (string, error) {
	return "", errSelfIssueUnavailable
}
