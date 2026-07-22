//go:build !windows

package dpapi

import "github.com/bubomortis/clairvoyance-persona-sync/internal/cred"

// platformSealer on non-Windows falls back to cred.NoopSealer: the private
// identity is stored under 0600 permissions but is NOT sealed against a reader
// with filesystem access. clvsync's primary target is Windows (DPAPI); a native
// macOS Keychain / Linux keyring sealer can be dropped in here later without
// touching the extractable cred package. `cred status` reports "none" so the user
// is told the at-rest protection is permissions-only.
func platformSealer() cred.Sealer { return cred.NoopSealer{} }
