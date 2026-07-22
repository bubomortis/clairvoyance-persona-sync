package e2e

import (
	"path/filepath"
	"testing"

	"filippo.io/age"

	"github.com/bubomortis/clairvoyance-persona-sync/internal/clv"
	"github.com/bubomortis/clairvoyance-persona-sync/internal/export"
	"github.com/bubomortis/clairvoyance-persona-sync/internal/importer"
)

func genID(t *testing.T) *age.X25519Identity {
	t.Helper()
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// TestRoundtrip_IdentityModel exercises the D17 Model 2c plumbing end to end at the
// package level: export encrypts to the recipient's public key (age recipients),
// carries the SENDER's public key in meta for trust-on-first-use, and import
// decrypts with the recipient's private key and surfaces the sender key.
func TestRoundtrip_IdentityModel(t *testing.T) {
	srcDir, wantHome, _, _ := buildSource(t)
	src, _ := clv.Open(srcDir)
	p, err := src.FindPersona("Testy")
	if err != nil {
		t.Fatal(err)
	}

	sender := genID(t)     // machine A (the exporter)
	recipient := genID(t)  // machine B (the importer)

	pkgPath := filepath.Join(t.TempDir(), "testy-id.cvpkg.age")
	res, err := export.Persona(src, p, pkgPath, export.Options{
		Recipients:      []string{recipient.Recipient().String()},
		SenderName:      "machineA",
		SenderPublicKey: sender.Recipient().String(),
	})
	if err != nil {
		t.Fatalf("export identity: %v", err)
	}
	if !res.Encrypted {
		t.Fatal("identity export should be encrypted")
	}

	// A wrong identity must NOT decrypt.
	dstDir := t.TempDir()
	dstWS := filepath.Join(dstDir, "ws-relocated")
	write(t, filepath.Join(dstDir, "clairvoyance-store.json"), storeJSON(t, dstDir, "TestWS", dstWS))
	write(t, filepath.Join(dstDir, "profiles", "prof2", "staff.json"), "[]")
	dst, _ := clv.Open(dstDir)

	wrong := genID(t)
	if _, err := importer.Apply(pkgPath, dst, importer.Options{Identity: wrong.String()}); err == nil {
		t.Fatal("import with the wrong identity should fail to decrypt")
	}

	// The correct recipient identity decrypts, and the sender key surfaces for TOFU.
	rep, err := importer.Apply(pkgPath, dst, importer.Options{Identity: recipient.String()})
	if err != nil {
		t.Fatalf("import identity: %v", err)
	}
	if rep.SenderPublicKey != sender.Recipient().String() {
		t.Fatalf("sender public key did not travel: got %q", rep.SenderPublicKey)
	}
	if rep.SenderName != "machineA" {
		t.Fatalf("sender name did not travel: got %q", rep.SenderName)
	}
	assertFile(t, filepath.Join(dstDir, ".Clairvoyance", "staff", "testy", "index.md"), wantHome)
}

// TestRoundtrip_MultiRecipient proves a single package encrypted to two peers can
// be decrypted by EITHER peer's private key (D17 identity model to multiple peers).
func TestRoundtrip_MultiRecipient(t *testing.T) {
	srcDir, _, _, _ := buildSource(t)
	src, _ := clv.Open(srcDir)
	p, _ := src.FindPersona("Testy")

	peer1 := genID(t)
	peer2 := genID(t)

	pkgPath := filepath.Join(t.TempDir(), "multi.cvpkg.age")
	if _, err := export.Persona(src, p, pkgPath, export.Options{
		Recipients: []string{peer1.Recipient().String(), peer2.Recipient().String()},
	}); err != nil {
		t.Fatalf("export multi: %v", err)
	}

	for i, id := range []*age.X25519Identity{peer1, peer2} {
		dstDir := t.TempDir()
		dstWS := filepath.Join(dstDir, "ws-relocated")
		write(t, filepath.Join(dstDir, "clairvoyance-store.json"), storeJSON(t, dstDir, "TestWS", dstWS))
		write(t, filepath.Join(dstDir, "profiles", "prof2", "staff.json"), "[]")
		dst, _ := clv.Open(dstDir)
		if _, err := importer.Apply(pkgPath, dst, importer.Options{Identity: id.String()}); err != nil {
			t.Fatalf("peer%d could not decrypt the shared package: %v", i+1, err)
		}
	}
}

// TestExport_NoSenderKeyByDefault confirms a passphrase export carries no sender
// public key (the TOFU field is identity-travel only).
func TestExport_NoSenderKeyByDefault(t *testing.T) {
	srcDir, _, _, _ := buildSource(t)
	src, _ := clv.Open(srcDir)
	p, _ := src.FindPersona("Testy")

	pkgPath := filepath.Join(t.TempDir(), "pw.cvpkg.age")
	if _, err := export.Persona(src, p, pkgPath, export.Options{Passphrase: "correct-horse-battery"}); err != nil {
		t.Fatalf("export: %v", err)
	}
	dstDir := t.TempDir()
	dstWS := filepath.Join(dstDir, "ws-relocated")
	write(t, filepath.Join(dstDir, "clairvoyance-store.json"), storeJSON(t, dstDir, "TestWS", dstWS))
	write(t, filepath.Join(dstDir, "profiles", "prof2", "staff.json"), "[]")
	dst, _ := clv.Open(dstDir)
	rep, err := importer.Apply(pkgPath, dst, importer.Options{Passphrase: "correct-horse-battery"})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if rep.SenderPublicKey != "" || rep.SenderName != "" {
		t.Fatalf("passphrase export must not carry a sender key: %q/%q", rep.SenderName, rep.SenderPublicKey)
	}
}
