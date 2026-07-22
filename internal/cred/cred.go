// Package cred is a self-contained credential broker for age-encrypted file
// transfer. It resolves *how* a package should be encrypted or decrypted from a
// user-selected credential model, and it manages the local age identity (Model
// 2c) and the trust-on-first-use store of paired peer public keys.
//
// # Modularity (deliberate design)
//
// This package has NO dependency on the rest of clvsync (no clv/pkg/export/…) —
// it imports only filippo.io/age and the standard library. Every host-specific
// capability is injected:
//
//   - Sealer      — how the local private identity is protected at rest
//     (Windows DPAPI in clvsync; a caller-supplied no-op elsewhere).
//   - the storage directory — the host says WHERE the broker keeps its files.
//
// so the whole package can later be lifted out into its own module and reused by
// any tool that needs "pick a credential model, manage an age identity, pair with
// peers, and be told how to encrypt/decrypt." The clvsync integration is just the
// glue in cmd/clvsync + internal/dpapi.
//
// The broker never invents time or randomness beyond age key generation, and it
// never marshals a private key in the clear — the private half is only ever
// written through the injected Sealer and read back through it.
package cred

import "errors"

// Model is the credential model a user selects (spec §20.3, D17).
type Model string

const (
	// ModelShared (2a) — the same passphrase is stored on each machine (in the
	// host's credential store) and used automatically. One secret guards all
	// transfers. From the broker's crypto view this is identical to 2b: a
	// passphrase drives age scrypt encryption. The distinction is purely where
	// the host sources the passphrase (stored vs freshly prompted), which the
	// broker leaves to the host.
	ModelShared Model = "shared-passphrase"
	// ModelPerTransfer (2b) — a fresh passphrase chosen per export and entered
	// per import; nothing is stored. Strongest, most manual.
	ModelPerTransfer Model = "per-transfer"
	// ModelIdentity (2c) — a per-machine age keypair. Encrypt to the peer's
	// public key; decrypt with the local private key (sealed at rest, never
	// travels). Most transparent after a one-time pairing.
	ModelIdentity Model = "identity"
)

// Valid reports whether m is a recognized model.
func (m Model) Valid() bool {
	switch m {
	case ModelShared, ModelPerTransfer, ModelIdentity:
		return true
	}
	return false
}

// NeedsPassphrase reports whether the model drives encryption from a passphrase
// the host must supply (2a/2b) rather than from an age identity (2c).
func (m Model) NeedsPassphrase() bool {
	return m == ModelShared || m == ModelPerTransfer
}

// Pairing is how Model 2c exchanges public keys (spec §20.5). It is meaningful
// only when Model == ModelIdentity.
type Pairing string

const (
	// PairingCloudSync publishes this machine's public key to a Cloud-Synced
	// note and reads the peer's from it (Plus+; eventual, so the host polls).
	// The broker does not perform Cloud Sync itself — the host operator does —
	// but records the choice so status/UX is consistent.
	PairingCloudSync Pairing = "cloud-sync"
	// PairingTravel carries the sender's public key inside every package
	// (trust-on-first-use); the first exchange bootstraps with a public-key-only
	// pairing artifact.
	PairingTravel Pairing = "travel"
)

// Valid reports whether p is a recognized pairing mode.
func (p Pairing) Valid() bool {
	return p == PairingCloudSync || p == PairingTravel
}

// Config is the persisted credential choice. It is deliberately small and
// host-agnostic so it can be serialized wherever the host keeps broker state.
type Config struct {
	Model   Model   `json:"model,omitempty"`
	Pairing Pairing `json:"pairing,omitempty"` // only meaningful for ModelIdentity
}

// PairingDoc is the public-key-only artifact exchanged during pairing. It carries
// NO secret — only a name and the sender's age public key — so it is safe to send
// over any channel (spec §20.5 travel-with-export bootstrap). The host stamps any
// timestamp/transport around it; the broker keeps it minimal on purpose.
type PairingDoc struct {
	Name      string `json:"name"`
	PublicKey string `json:"publicKey"` // "age1..."
}

// Sealer protects the local private identity at rest. The private key is written
// only through Seal and read only through Unseal, so a plaintext key never
// touches disk when the host provides real sealing (DPAPI). Name identifies the
// mechanism for status output (e.g. "dpapi", "none").
//
// Sealer is the primary extraction seam: OS-bound sealing lives in the host, not
// in this package.
type Sealer interface {
	Seal(plaintext []byte) ([]byte, error)
	Unseal(sealed []byte) ([]byte, error)
	Name() string
}

// NoopSealer stores the private key unsealed (relying on file permissions only).
// It exists so the broker is usable out of the box on platforms without a
// host-provided sealer, but it is NOT confidential-at-rest — Name reports "none"
// so callers can warn. Prefer a real Sealer (DPAPI/Keychain) in production.
type NoopSealer struct{}

func (NoopSealer) Seal(plaintext []byte) ([]byte, error) { return plaintext, nil }
func (NoopSealer) Unseal(sealed []byte) ([]byte, error)  { return sealed, nil }
func (NoopSealer) Name() string                          { return "none" }

// Typed errors let the host (and the operator persona) react precisely instead of
// string-matching. Each says exactly what the host must gather next.
var (
	// ErrNoModel — no credential model has been selected yet.
	ErrNoModel = errors.New("cred: no credential model selected")
	// ErrNeedPassphrase — a passphrase model (2a/2b) was resolved but the host
	// supplied no passphrase.
	ErrNeedPassphrase = errors.New("cred: model needs a passphrase but none was supplied")
	// ErrNoIdentity — Model 2c but this machine has no local identity yet.
	ErrNoIdentity = errors.New("cred: no local identity — initialize one first")
	// ErrNoPeers — Model 2c encrypt but no peer public keys are paired yet.
	ErrNoPeers = errors.New("cred: no paired peer public keys — pair with a peer first")
	// ErrPeerConflict — a pairing offered a public key that differs from the one
	// already trusted for that name (possible impersonation; verify out of band).
	ErrPeerConflict = errors.New("cred: peer public key changed — refusing to overwrite; verify out of band")
	// ErrIdentityExists — refusing to overwrite an existing local identity.
	ErrIdentityExists = errors.New("cred: a local identity already exists")
)

// EncryptPlan tells the host how to encrypt a package. Exactly one of Passphrase
// or Recipients is set. EmbedPublicKey, when non-empty, is this machine's public
// key that the host should carry inside the package for trust-on-first-use
// (Model 2c travel pairing).
type EncryptPlan struct {
	Passphrase     string   // set for ModelShared / ModelPerTransfer
	Recipients     []string // set for ModelIdentity — peer "age1..." public keys
	EmbedPublicKey string   // Model 2c travel: sender public key to bundle (TOFU)
}

// DecryptPlan tells the host how to decrypt a package. Exactly one of Passphrase
// or Identity is set. Identity is the unsealed private key ("AGE-SECRET-KEY-…"),
// materialized only for the duration of the decrypt.
type DecryptPlan struct {
	Passphrase string // set for ModelShared / ModelPerTransfer
	Identity   string // set for ModelIdentity — unsealed private key
}
