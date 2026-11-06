//go:build testmode

package sessionguard

// guardEnabled reports whether session integrity gating is compiled in.
// Production builds leave this true; test builds pass -tags testmode.
const guardEnabled = false
