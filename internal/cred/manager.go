package cred

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"filippo.io/age"
)

// File names inside the broker's directory. The identity is stored sealed; the
// public key is kept beside it in the clear (it is public) so status/pairing can
// read it without unsealing.
const (
	fileConfig   = "cred-config.json"
	fileIdentity = "identity.sealed" // Sealer(private key bytes)
	filePublic   = "identity.pub"    // "age1..." public key, plaintext
	filePeers    = "peers.json"      // name -> "age1..." public key (TOFU)
)

// Manager is a file-backed credential broker rooted at a directory the host
// chooses. It is safe to construct cheaply per command; it holds no open state.
type Manager struct {
	dir    string
	sealer Sealer
}

// NewManager returns a broker rooted at dir, using sealer to protect the private
// identity at rest. If sealer is nil, NoopSealer is used (unsealed at rest — see
// its doc). The directory is created lazily on first write.
func NewManager(dir string, sealer Sealer) *Manager {
	if sealer == nil {
		sealer = NoopSealer{}
	}
	return &Manager{dir: dir, sealer: sealer}
}

// SealerName reports the at-rest protection mechanism (for status output).
func (m *Manager) SealerName() string { return m.sealer.Name() }

func (m *Manager) ensureDir() error { return os.MkdirAll(m.dir, 0o700) }

func (m *Manager) path(name string) string { return filepath.Join(m.dir, name) }

// ---- Config ---------------------------------------------------------------

// Config reads the persisted credential choice (zero Config if none set yet).
func (m *Manager) Config() (Config, error) {
	var c Config
	b, err := os.ReadFile(m.path(fileConfig))
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, nil
		}
		return Config{}, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return Config{}, fmt.Errorf("cred: corrupt %s: %w", fileConfig, err)
	}
	return c, nil
}

// SetConfig validates and persists the credential choice. Pairing is only allowed
// with ModelIdentity; a passphrase model clears any stale pairing value.
func (m *Manager) SetConfig(c Config) error {
	if !c.Model.Valid() {
		return fmt.Errorf("cred: %w: %q", ErrNoModel, c.Model)
	}
	if c.Model == ModelIdentity {
		if c.Pairing != "" && !c.Pairing.Valid() {
			return fmt.Errorf("cred: invalid pairing %q", c.Pairing)
		}
	} else {
		c.Pairing = "" // pairing is meaningless without an identity
	}
	if err := m.ensureDir(); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	return writeFilePriv(m.path(fileConfig), b, 0o600)
}

// ---- Identity (Model 2c) --------------------------------------------------

// HasIdentity reports whether a local identity exists on this machine.
func (m *Manager) HasIdentity() bool {
	_, err := os.Stat(m.path(fileIdentity))
	return err == nil
}

// InitIdentity generates a fresh age keypair, seals the private half through the
// Sealer, and writes the public half in the clear. It refuses to overwrite an
// existing identity (ErrIdentityExists) so a machine's private key is never
// silently rotated out from under peers that trust its public key.
func (m *Manager) InitIdentity() (PairingDoc, error) {
	if m.HasIdentity() {
		return PairingDoc{}, ErrIdentityExists
	}
	id, err := age.GenerateX25519Identity()
	if err != nil {
		return PairingDoc{}, err
	}
	if err := m.ensureDir(); err != nil {
		return PairingDoc{}, err
	}
	sealed, err := m.sealer.Seal([]byte(id.String()))
	if err != nil {
		return PairingDoc{}, fmt.Errorf("cred: seal identity: %w", err)
	}
	// Write the public key first: if the sealed write fails we have a stray .pub
	// but no usable identity (HasIdentity stays false) — harmless and overwritten
	// on retry. The reverse (sealed present, pub missing) would look like a valid
	// identity with no readable public key.
	if err := writeFilePriv(m.path(filePublic), []byte(id.Recipient().String()), 0o600); err != nil {
		return PairingDoc{}, err
	}
	if err := writeFilePriv(m.path(fileIdentity), sealed, 0o600); err != nil {
		return PairingDoc{}, err
	}
	return PairingDoc{PublicKey: id.Recipient().String()}, nil
}

// PublicKey returns this machine's age public key ("age1..."), or ErrNoIdentity.
func (m *Manager) PublicKey() (string, error) {
	b, err := os.ReadFile(m.path(filePublic))
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrNoIdentity
		}
		return "", err
	}
	pub := strings.TrimSpace(string(b))
	if pub == "" {
		return "", ErrNoIdentity
	}
	return pub, nil
}

// unsealPrivate materializes the private key for the duration of a decrypt. The
// caller must not persist the returned string.
func (m *Manager) unsealPrivate() (string, error) {
	sealed, err := os.ReadFile(m.path(fileIdentity))
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrNoIdentity
		}
		return "", err
	}
	plain, err := m.sealer.Unseal(sealed)
	if err != nil {
		return "", fmt.Errorf("cred: unseal identity: %w", err)
	}
	key := strings.TrimSpace(string(plain))
	// Sanity-check that what we unsealed is actually an age identity, so a wrong
	// sealer (e.g. DPAPI blob from another user) fails loudly here rather than
	// producing a cryptic age error later.
	if _, err := age.ParseX25519Identity(key); err != nil {
		return "", fmt.Errorf("cred: unsealed identity is not a valid age key: %w", err)
	}
	return key, nil
}

// ---- Peers (trust-on-first-use) -------------------------------------------

func (m *Manager) loadPeers() (map[string]string, error) {
	peers := map[string]string{}
	b, err := os.ReadFile(m.path(filePeers))
	if err != nil {
		if os.IsNotExist(err) {
			return peers, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(b, &peers); err != nil {
		return nil, fmt.Errorf("cred: corrupt %s: %w", filePeers, err)
	}
	return peers, nil
}

// Peers returns a copy of the paired peer public keys, keyed by name.
func (m *Manager) Peers() (map[string]string, error) { return m.loadPeers() }

// AddPeer records a peer's public key under a name (trust-on-first-use).
//
//   - First time for a name: stored; added=true.
//   - Same name, identical key: idempotent no-op; added=false, err=nil.
//   - Same name, DIFFERENT key: refused with ErrPeerConflict — a changed key is
//     a possible impersonation and must be resolved out of band (remove the peer
//     deliberately, then re-add). This is the TOFU security property.
//
// The public key is validated as a real age recipient before it is trusted.
func (m *Manager) AddPeer(name, publicKey string) (added bool, err error) {
	name = strings.TrimSpace(name)
	publicKey = strings.TrimSpace(publicKey)
	if name == "" {
		return false, fmt.Errorf("cred: empty peer name")
	}
	if _, err := age.ParseX25519Recipient(publicKey); err != nil {
		return false, fmt.Errorf("cred: invalid peer public key: %w", err)
	}
	peers, err := m.loadPeers()
	if err != nil {
		return false, err
	}
	if existing, ok := peers[name]; ok {
		if existing == publicKey {
			return false, nil // idempotent
		}
		return false, ErrPeerConflict
	}
	peers[name] = publicKey
	return true, m.savePeers(peers)
}

// RemovePeer deletes a paired peer by name (reports whether it existed). Removing
// then re-adding is the deliberate path to accept a rotated peer key.
func (m *Manager) RemovePeer(name string) (bool, error) {
	name = strings.TrimSpace(name)
	peers, err := m.loadPeers()
	if err != nil {
		return false, err
	}
	if _, ok := peers[name]; !ok {
		return false, nil
	}
	delete(peers, name)
	return true, m.savePeers(peers)
}

func (m *Manager) savePeers(peers map[string]string) error {
	if err := m.ensureDir(); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(peers, "", "  ")
	return writeFilePriv(m.path(filePeers), b, 0o600)
}

// Recipients returns the paired peer public keys, deduplicated by value and
// sorted for deterministic output. These are the encrypt targets for Model 2c.
// Dedup matters because the same key can be trusted under two names (e.g. a
// manual pairing plus a travel-package auto-record), which would otherwise emit
// a redundant age stanza.
func (m *Manager) Recipients() ([]string, error) {
	peers, err := m.loadPeers()
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(peers))
	out := make([]string, 0, len(peers))
	for _, k := range peers {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out, nil
}

// ---- Resolution -----------------------------------------------------------

// ResolveEncrypt decides how to encrypt given the configured model. For a
// passphrase model (2a/2b) the host supplies the passphrase it already gathered
// (env or prompt); an empty one yields ErrNeedPassphrase. For Model 2c it targets
// every paired peer, and — when pairing is travel — asks the host to embed this
// machine's public key for TOFU. Typed errors tell the host exactly what to fix.
func (m *Manager) ResolveEncrypt(passphrase string) (EncryptPlan, error) {
	cfg, err := m.Config()
	if err != nil {
		return EncryptPlan{}, err
	}
	switch cfg.Model {
	case ModelShared, ModelPerTransfer:
		if passphrase == "" {
			return EncryptPlan{}, ErrNeedPassphrase
		}
		return EncryptPlan{Passphrase: passphrase}, nil
	case ModelIdentity:
		recips, err := m.Recipients()
		if err != nil {
			return EncryptPlan{}, err
		}
		if len(recips) == 0 {
			return EncryptPlan{}, ErrNoPeers
		}
		plan := EncryptPlan{Recipients: recips}
		if cfg.Pairing == PairingTravel {
			// Best-effort: if this machine has no identity yet we simply can't
			// embed a key; encryption to peers still works, so don't fail.
			if pub, err := m.PublicKey(); err == nil {
				plan.EmbedPublicKey = pub
			}
		}
		return plan, nil
	default:
		return EncryptPlan{}, ErrNoModel
	}
}

// ResolveDecrypt decides how to decrypt given the configured model. For a
// passphrase model the host supplies the passphrase; for Model 2c the local
// private key is unsealed for the decrypt. Typed errors tell the host what to fix.
func (m *Manager) ResolveDecrypt(passphrase string) (DecryptPlan, error) {
	cfg, err := m.Config()
	if err != nil {
		return DecryptPlan{}, err
	}
	switch cfg.Model {
	case ModelShared, ModelPerTransfer:
		if passphrase == "" {
			return DecryptPlan{}, ErrNeedPassphrase
		}
		return DecryptPlan{Passphrase: passphrase}, nil
	case ModelIdentity:
		key, err := m.unsealPrivate()
		if err != nil {
			return DecryptPlan{}, err
		}
		return DecryptPlan{Identity: key}, nil
	default:
		return DecryptPlan{}, ErrNoModel
	}
}

// writeFilePriv writes data with restrictive permissions, tightening an existing
// file's mode too (WriteFile alone won't narrow perms on an existing file).
func writeFilePriv(path string, data []byte, perm os.FileMode) error {
	if err := os.WriteFile(path, data, perm); err != nil {
		return err
	}
	return os.Chmod(path, perm)
}
