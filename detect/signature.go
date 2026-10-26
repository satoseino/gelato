package detect

import (
	"encoding/hex"

	"golang.org/x/crypto/blake2s"
)

// Signature returns the identifier of a finding of match in the file at path:
// the lowercase hex BLAKE2s-256 digest of "{match}$${path}". It is
// byte-for-byte tartufo's signature, so existing `exclude-signatures` entries
// keep working.
func Signature(match, path string) string {
	sum := blake2s.Sum256([]byte(match + "$$" + path))

	return hex.EncodeToString(sum[:])
}

// signatureExcluded reports whether a finding of match with signature sig is
// suppressed by excluded. The raw match is checked too because a signature
// written into a config file is itself a high-entropy string.
func signatureExcluded(excluded map[string]struct{}, match, sig string) bool {
	if _, ok := excluded[match]; ok {
		return true
	}

	_, ok := excluded[sig]

	return ok
}
