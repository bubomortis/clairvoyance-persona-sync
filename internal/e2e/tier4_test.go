package e2e

import (
	"path/filepath"
	"testing"

	"github.com/bubomortis/clairvoyance-persona-sync/internal/clv"
	"github.com/bubomortis/clairvoyance-persona-sync/internal/export"
	"github.com/bubomortis/clairvoyance-persona-sync/internal/importer"
)

func TestRoundtrip_Tier4_HeavyAddon(t *testing.T) {
	srcDir, _ := buildWorkspaceSource(t)
	src, _ := clv.Open(srcDir)

	if export.HeavySize(src, "Proj") <= 0 {
		t.Fatal("expected non-zero heavy size (venv/big.dll)")
	}

	t3 := filepath.Join(t.TempDir(), "proj.cvpkg.age")
	t4 := filepath.Join(filepath.Dir(t3), "proj_heavy.cvpkg.age")
	if _, err := export.Workspace(src, "Proj", t3, export.Options{Passphrase: "pw"}); err != nil {
		t.Fatal(err)
	}
	if _, err := export.WorkspaceHeavy(src, "Proj", t4, export.Options{Passphrase: "pw"}); err != nil {
		t.Fatal(err)
	}

	dstDir := t.TempDir()
	write(t, filepath.Join(dstDir, "clairvoyance-store.json"), `{"workspaces":[{"id":"workspace-home","name":"Home","path":"`+jsonPath(dstDir)+`"}]}`)
	write(t, filepath.Join(dstDir, "profiles", "prof2", "staff.json"), "[]")
	dstWSPath := filepath.Join(dstDir, "Workspaces", "Proj")
	dst, _ := clv.Open(dstDir)

	// Tier 4 before Tier 3 must fail (paired add-on, never standalone)
	if _, err := importer.Apply(t4, dst, importer.Options{Passphrase: "pw", WorkspacePath: dstWSPath}); err == nil {
		t.Fatal("expected Tier 4 import to fail before the Tier 3 workspace exists")
	}

	// Tier 3, then Tier 4 overlay
	if _, err := importer.Apply(t3, dst, importer.Options{Passphrase: "pw", WorkspacePath: dstWSPath}); err != nil {
		t.Fatalf("tier3 import: %v", err)
	}
	dst2, _ := clv.Open(dstDir) // reload to see the newly-registered workspace
	rep, err := importer.Apply(t4, dst2, importer.Options{Passphrase: "pw"})
	if err != nil {
		t.Fatalf("tier4 import: %v", err)
	}
	if rep.Tier != 4 {
		t.Fatalf("expected tier 4, got %d", rep.Tier)
	}
	assertFile(t, filepath.Join(dstWSPath, "venv", "big.dll"), "PRETEND-HUGE-BINARY")
}
