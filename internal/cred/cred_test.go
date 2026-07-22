package cred

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"filippo.io/age"
)

// xorSealer is a trivial reversible "sealer" for tests — it is NOT secure, it
// only proves the private key round-trips through Seal/Unseal (and that a wrong
// sealer is detected). Real hosts inject DPAPI.
type xorSealer struct{ k byte }

func (s xorSealer) Seal(p []byte) ([]byte, error) {
	out := make([]byte, len(p))
	for i, b := range p {
		out[i] = b ^ s.k
	}
	return out, nil
}
func (s xorSealer) Unseal(p []byte) ([]byte, error) { return s.Seal(p) }
func (s xorSealer) Name() string                    { return "xor-test" }

func newMgr(t *testing.T, sealer Sealer) *Manager {
	t.Helper()
	return NewManager(filepath.Join(t.TempDir(), "cred"), sealer)
}

func genPub(t *testing.T) string {
	t.Helper()
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	return id.Recipient().String()
}

func TestModelValidity(t *testing.T) {
	for _, m := range []Model{ModelShared, ModelPerTransfer, ModelIdentity} {
		if !m.Valid() {
			t.Errorf("%q should be valid", m)
		}
	}
	if Model("nonsense").Valid() {
		t.Error("garbage model reported valid")
	}
	if !ModelShared.NeedsPassphrase() || !ModelPerTransfer.NeedsPassphrase() {
		t.Error("2a/2b must need a passphrase")
	}
	if ModelIdentity.NeedsPassphrase() {
		t.Error("2c must not need a passphrase")
	}
}

func TestConfigRoundTripAndPairingScope(t *testing.T) {
	m := newMgr(t, NoopSealer{})

	// Zero config when nothing persisted.
	if c, err := m.Config(); err != nil || c.Model != "" {
		t.Fatalf("fresh config = %+v, %v", c, err)
	}
	// Invalid model refused.
	if err := m.SetConfig(Config{Model: "bogus"}); err == nil {
		t.Fatal("expected invalid-model error")
	}
	// Pairing dropped for a passphrase model.
	if err := m.SetConfig(Config{Model: ModelShared, Pairing: PairingTravel}); err != nil {
		t.Fatal(err)
	}
	if c, _ := m.Config(); c.Pairing != "" {
		t.Errorf("pairing should be cleared for passphrase model, got %q", c.Pairing)
	}
	// Pairing kept for identity model.
	if err := m.SetConfig(Config{Model: ModelIdentity, Pairing: PairingCloudSync}); err != nil {
		t.Fatal(err)
	}
	if c, _ := m.Config(); c.Model != ModelIdentity || c.Pairing != PairingCloudSync {
		t.Errorf("identity config not persisted: %+v", c)
	}
	// Invalid pairing on identity refused.
	if err := m.SetConfig(Config{Model: ModelIdentity, Pairing: "bogus"}); err == nil {
		t.Fatal("expected invalid-pairing error")
	}
}

func TestIdentityLifecycleAndSealRoundTrip(t *testing.T) {
	sealer := xorSealer{k: 0x5a}
	m := newMgr(t, sealer)

	if m.HasIdentity() {
		t.Fatal("fresh manager should have no identity")
	}
	if _, err := m.PublicKey(); !errors.Is(err, ErrNoIdentity) {
		t.Fatalf("PublicKey before init = %v, want ErrNoIdentity", err)
	}

	doc, err := m.InitIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if doc.PublicKey == "" {
		t.Fatal("InitIdentity returned empty public key")
	}
	if !m.HasIdentity() {
		t.Fatal("HasIdentity false after init")
	}
	pub, err := m.PublicKey()
	if err != nil || pub != doc.PublicKey {
		t.Fatalf("PublicKey = %q,%v want %q", pub, err, doc.PublicKey)
	}

	// The private key must be sealed at rest, not stored in the clear.
	sealed, err := os.ReadFile(m.path(fileIdentity))
	if err != nil {
		t.Fatal(err)
	}
	if _, perr := age.ParseX25519Identity(string(sealed)); perr == nil {
		t.Fatal("private key is readable in the clear on disk — not sealed")
	}

	// Unseal must recover a valid identity whose recipient matches the public key.
	priv, err := m.unsealPrivate()
	if err != nil {
		t.Fatal(err)
	}
	id, err := age.ParseX25519Identity(priv)
	if err != nil {
		t.Fatal(err)
	}
	if id.Recipient().String() != pub {
		t.Fatal("unsealed private key does not match stored public key")
	}

	// Re-init refused.
	if _, err := m.InitIdentity(); !errors.Is(err, ErrIdentityExists) {
		t.Fatalf("re-init = %v, want ErrIdentityExists", err)
	}
}

func TestUnsealWrongSealerDetected(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cred")
	// Seal with one key...
	if _, err := NewManager(dir, xorSealer{k: 0x11}).InitIdentity(); err != nil {
		t.Fatal(err)
	}
	// ...unseal with another → garbage, must be rejected, not returned.
	_, err := NewManager(dir, xorSealer{k: 0x22}).unsealPrivate()
	if err == nil {
		t.Fatal("wrong sealer should fail to produce a valid identity")
	}
}

func TestPeerTOFU(t *testing.T) {
	m := newMgr(t, NoopSealer{})
	pubA := genPub(t)
	pubB := genPub(t)

	// First add.
	added, err := m.AddPeer("bubo", pubA)
	if err != nil || !added {
		t.Fatalf("first add = %v,%v", added, err)
	}
	// Idempotent re-add of the same key.
	added, err = m.AddPeer("bubo", pubA)
	if err != nil || added {
		t.Fatalf("idempotent add = %v,%v", added, err)
	}
	// Changed key for the same name is refused (TOFU).
	if _, err := m.AddPeer("bubo", pubB); !errors.Is(err, ErrPeerConflict) {
		t.Fatalf("changed key = %v, want ErrPeerConflict", err)
	}
	// Deliberate remove + re-add accepts the rotated key.
	if ok, err := m.RemovePeer("bubo"); err != nil || !ok {
		t.Fatalf("remove = %v,%v", ok, err)
	}
	if added, err := m.AddPeer("bubo", pubB); err != nil || !added {
		t.Fatalf("re-add after remove = %v,%v", added, err)
	}
	// Invalid key rejected.
	if _, err := m.AddPeer("evil", "not-an-age-key"); err == nil {
		t.Fatal("invalid peer key accepted")
	}
	// Empty name rejected.
	if _, err := m.AddPeer("", pubA); err == nil {
		t.Fatal("empty peer name accepted")
	}

	recips, err := m.Recipients()
	if err != nil || len(recips) != 1 || recips[0] != pubB {
		t.Fatalf("recipients = %v,%v", recips, err)
	}
}

func TestRecipientsDedup(t *testing.T) {
	m := newMgr(t, NoopSealer{})
	shared := genPub(t)
	other := genPub(t)
	// Same key trusted under two names (manual pairing + travel auto-record).
	if _, err := m.AddPeer("machineA", shared); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AddPeer("hostA", shared); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AddPeer("machineB", other); err != nil {
		t.Fatal(err)
	}
	recips, err := m.Recipients()
	if err != nil {
		t.Fatal(err)
	}
	if len(recips) != 2 {
		t.Fatalf("expected 2 unique recipients (dup key collapsed), got %d: %v", len(recips), recips)
	}
}

func TestResolveEncrypt(t *testing.T) {
	// No model → ErrNoModel.
	m := newMgr(t, NoopSealer{})
	if _, err := m.ResolveEncrypt("x"); !errors.Is(err, ErrNoModel) {
		t.Fatalf("no model = %v, want ErrNoModel", err)
	}

	// Passphrase model with/without passphrase.
	must(t, m.SetConfig(Config{Model: ModelShared}))
	if _, err := m.ResolveEncrypt(""); !errors.Is(err, ErrNeedPassphrase) {
		t.Fatalf("empty passphrase = %v, want ErrNeedPassphrase", err)
	}
	plan, err := m.ResolveEncrypt("s3cret")
	if err != nil || plan.Passphrase != "s3cret" || plan.Recipients != nil {
		t.Fatalf("passphrase plan = %+v,%v", plan, err)
	}

	// Identity model, no peers → ErrNoPeers.
	must(t, m.SetConfig(Config{Model: ModelIdentity, Pairing: PairingTravel}))
	if _, err := m.ResolveEncrypt(""); !errors.Is(err, ErrNoPeers) {
		t.Fatalf("identity no peers = %v, want ErrNoPeers", err)
	}

	// Pair a peer, init identity → recipients + embedded pubkey on travel.
	pub := genPub(t)
	if _, err := m.AddPeer("bubo", pub); err != nil {
		t.Fatal(err)
	}
	doc, err := m.InitIdentity()
	if err != nil {
		t.Fatal(err)
	}
	plan, err = m.ResolveEncrypt("")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Recipients) != 1 || plan.Recipients[0] != pub {
		t.Fatalf("identity recipients = %v", plan.Recipients)
	}
	if plan.EmbedPublicKey != doc.PublicKey {
		t.Fatalf("travel pairing should embed own pubkey; got %q want %q", plan.EmbedPublicKey, doc.PublicKey)
	}
	if plan.Passphrase != "" {
		t.Fatal("identity plan must not carry a passphrase")
	}

	// Cloud-sync pairing does NOT embed the public key.
	must(t, m.SetConfig(Config{Model: ModelIdentity, Pairing: PairingCloudSync}))
	plan, err = m.ResolveEncrypt("")
	if err != nil {
		t.Fatal(err)
	}
	if plan.EmbedPublicKey != "" {
		t.Fatal("cloud-sync pairing should not embed a public key in the package")
	}
}

func TestResolveDecrypt(t *testing.T) {
	m := newMgr(t, xorSealer{k: 0x7e})

	must(t, m.SetConfig(Config{Model: ModelPerTransfer}))
	if _, err := m.ResolveDecrypt(""); !errors.Is(err, ErrNeedPassphrase) {
		t.Fatalf("empty = %v, want ErrNeedPassphrase", err)
	}
	if plan, err := m.ResolveDecrypt("pw"); err != nil || plan.Identity != "" || plan.Passphrase != "pw" {
		t.Fatalf("passphrase decrypt = %+v,%v", plan, err)
	}

	// Identity model without a local identity → ErrNoIdentity.
	must(t, m.SetConfig(Config{Model: ModelIdentity}))
	if _, err := m.ResolveDecrypt(""); !errors.Is(err, ErrNoIdentity) {
		t.Fatalf("no identity decrypt = %v, want ErrNoIdentity", err)
	}
	// With an identity → returns a working private key.
	if _, err := m.InitIdentity(); err != nil {
		t.Fatal(err)
	}
	plan, err := m.ResolveDecrypt("")
	if err != nil {
		t.Fatal(err)
	}
	if _, perr := age.ParseX25519Identity(plan.Identity); perr != nil {
		t.Fatalf("decrypt identity not a valid key: %v", perr)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
