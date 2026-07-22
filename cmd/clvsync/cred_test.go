package main

import (
	"path/filepath"
	"testing"

	"filippo.io/age"

	"github.com/bubomortis/clairvoyance-persona-sync/internal/cred"
	"github.com/bubomortis/clairvoyance-persona-sync/internal/dpapi"
	"github.com/bubomortis/clairvoyance-persona-sync/internal/export"
	"github.com/bubomortis/clairvoyance-persona-sync/internal/importer"
)

// mgrFor builds a broker rooted where openCredManager(dir) would look, so the CLI
// assist helpers (which resolve their own manager from dir) see the same state.
func mgrFor(t *testing.T, dir string) *cred.Manager {
	t.Helper()
	return cred.NewManager(filepath.Join(dir, credDirName), dpapi.Sealer())
}

func pub(t *testing.T) string {
	t.Helper()
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	return id.Recipient().String()
}

func TestCredAssistExport(t *testing.T) {
	// explicit=true always short-circuits, regardless of model.
	dir := t.TempDir()
	m := mgrFor(t, dir)
	if err := m.SetConfig(cred.Config{Model: cred.ModelIdentity, Pairing: cred.PairingTravel}); err != nil {
		t.Fatal(err)
	}
	var opts export.Options
	if resolved, err := credAssistExport(dir, &opts, true); resolved || err != nil {
		t.Fatalf("explicit should not resolve: %v,%v", resolved, err)
	}

	// No model → fall through (not resolved, no error).
	dir2 := t.TempDir()
	if resolved, err := credAssistExport(dir2, &export.Options{}, false); resolved || err != nil {
		t.Fatalf("no model = %v,%v want false,nil", resolved, err)
	}

	// Shared model, no passphrase → fall through so the caller can prompt.
	dir3 := t.TempDir()
	if err := mgrFor(t, dir3).SetConfig(cred.Config{Model: cred.ModelShared}); err != nil {
		t.Fatal(err)
	}
	if resolved, err := credAssistExport(dir3, &export.Options{}, false); resolved || err != nil {
		t.Fatalf("shared/no-pass = %v,%v want false,nil (fall through to prompt)", resolved, err)
	}

	// Identity model, no peers → hard error with guidance.
	dir4 := t.TempDir()
	if err := mgrFor(t, dir4).SetConfig(cred.Config{Model: cred.ModelIdentity, Pairing: cred.PairingTravel}); err != nil {
		t.Fatal(err)
	}
	if _, err := credAssistExport(dir4, &export.Options{}, false); err == nil {
		t.Fatal("identity model with no peers should hard-error")
	}

	// Identity model + peer + local identity (travel) → resolved, recipients + embedded pubkey.
	dir5 := t.TempDir()
	m5 := mgrFor(t, dir5)
	if err := m5.SetConfig(cred.Config{Model: cred.ModelIdentity, Pairing: cred.PairingTravel}); err != nil {
		t.Fatal(err)
	}
	peer := pub(t)
	if _, err := m5.AddPeer("bubo", peer); err != nil {
		t.Fatal(err)
	}
	if _, err := m5.InitIdentity(); err != nil {
		t.Fatal(err)
	}
	var opts5 export.Options
	resolved, err := credAssistExport(dir5, &opts5, false)
	if err != nil || !resolved {
		t.Fatalf("identity resolve = %v,%v", resolved, err)
	}
	if len(opts5.Recipients) != 1 || opts5.Recipients[0] != peer {
		t.Fatalf("recipients = %v", opts5.Recipients)
	}
	if opts5.SenderPublicKey == "" || opts5.SenderName == "" {
		t.Fatalf("travel pairing should embed sender name+key, got %q/%q", opts5.SenderName, opts5.SenderPublicKey)
	}
}

func TestCredAssistDecrypt(t *testing.T) {
	// Explicit passphrase already set → helper leaves opts alone and returns nil.
	dir := t.TempDir()
	if err := mgrFor(t, dir).SetConfig(cred.Config{Model: cred.ModelIdentity}); err != nil {
		t.Fatal(err)
	}
	opts := importer.Options{Passphrase: "already"}
	if m := credAssistDecrypt(dir, &opts); m != nil {
		t.Fatal("explicit passphrase should short-circuit (nil manager)")
	}
	if opts.Identity != "" {
		t.Fatal("must not overwrite an explicit passphrase with an identity")
	}

	// Identity model with a local identity → fills opts.Identity.
	dir2 := t.TempDir()
	m2 := mgrFor(t, dir2)
	if err := m2.SetConfig(cred.Config{Model: cred.ModelIdentity}); err != nil {
		t.Fatal(err)
	}
	if _, err := m2.InitIdentity(); err != nil {
		t.Fatal(err)
	}
	var opts2 importer.Options
	if m := credAssistDecrypt(dir2, &opts2); m == nil {
		t.Fatal("expected a manager back")
	}
	if _, err := age.ParseX25519Identity(opts2.Identity); err != nil {
		t.Fatalf("assist did not fill a valid identity: %v (%q)", err, opts2.Identity)
	}
}

func TestRecordSenderTOFU_OnlyIdentityModel(t *testing.T) {
	// Passphrase model: a traveling sender key must NOT be recorded.
	dir := t.TempDir()
	m := mgrFor(t, dir)
	if err := m.SetConfig(cred.Config{Model: cred.ModelShared}); err != nil {
		t.Fatal(err)
	}
	recordSenderTOFU(m, &importer.Report{SenderName: "a", SenderPublicKey: pub(t)})
	if peers, _ := m.Peers(); len(peers) != 0 {
		t.Fatalf("shared model should not auto-record peers, got %v", peers)
	}

	// Identity model: it IS recorded.
	dir2 := t.TempDir()
	m2 := mgrFor(t, dir2)
	if err := m2.SetConfig(cred.Config{Model: cred.ModelIdentity}); err != nil {
		t.Fatal(err)
	}
	key := pub(t)
	recordSenderTOFU(m2, &importer.Report{SenderName: "machineA", SenderPublicKey: key})
	peers, _ := m2.Peers()
	if peers["machineA"] != key {
		t.Fatalf("identity model should record the sender, got %v", peers)
	}

	// Dry-run must not record.
	dir3 := t.TempDir()
	m3 := mgrFor(t, dir3)
	if err := m3.SetConfig(cred.Config{Model: cred.ModelIdentity}); err != nil {
		t.Fatal(err)
	}
	recordSenderTOFU(m3, &importer.Report{SenderName: "x", SenderPublicKey: pub(t), DryRun: true})
	if peers, _ := m3.Peers(); len(peers) != 0 {
		t.Fatalf("dry-run should not record peers, got %v", peers)
	}
}
