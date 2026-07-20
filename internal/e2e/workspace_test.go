package e2e

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bubomortis/clairvoyance-persona-sync/internal/clv"
	"github.com/bubomortis/clairvoyance-persona-sync/internal/export"
	"github.com/bubomortis/clairvoyance-persona-sync/internal/importer"
)

// Build a source instance with a non-Home workspace "Proj" holding two personas
// (with memory + a heavy dir that must be excluded) and a shared-memory file.
func buildWorkspaceSource(t *testing.T) (dataDir, wsPath string) {
	dataDir = t.TempDir()
	wsPath = filepath.Join(dataDir, "proj")
	write(t, filepath.Join(dataDir, "clairvoyance-store.json"), storeJSON(t, dataDir, "Proj", wsPath))

	// two personas in one profile
	e1 := `{"id":"staff-alpha","name":"Alpha","jobDescription":"a"}`
	e2 := `{"id":"staff-beta","name":"Beta","jobDescription":"b"}`
	write(t, filepath.Join(dataDir, "profiles", "prof1", "staff.json"), "["+e1+","+e2+"]")
	write(t, filepath.Join(dataDir, "profiles", "prof1", "agent-history", "staff-alpha.json"), `{"messages":[]}`)

	// workspace memory for each persona + shared memory
	write(t, filepath.Join(wsPath, ".Clairvoyance", "staff", "alpha", "index.md"), "alpha memory")
	write(t, filepath.Join(wsPath, ".Clairvoyance", "staff", "beta", "index.md"), "beta memory")
	write(t, filepath.Join(wsPath, ".Clairvoyance", "memory", "shared.md"), "shared workspace memory")
	// ordinary content + a HEAVY dir that must NOT travel in Tier 3
	write(t, filepath.Join(wsPath, "notes", "plan.md"), "the plan")
	write(t, filepath.Join(wsPath, "venv", "big.dll"), "PRETEND-HUGE-BINARY")
	return
}

func TestRoundtrip_Tier3_Workspace(t *testing.T) {
	srcDir, _ := buildWorkspaceSource(t)
	src, _ := clv.Open(srcDir)

	roster := src.PersonasInWorkspace("Proj")
	if len(roster) != 2 {
		t.Fatalf("expected 2 personas in workspace, got %d", len(roster))
	}

	pkgPath := filepath.Join(t.TempDir(), "proj.cvpkg.age")
	if _, err := export.Workspace(src, "Proj", pkgPath, export.Options{Passphrase: "proj-pass-strong"}); err != nil {
		t.Fatalf("export workspace: %v", err)
	}

	// target: workspace does NOT exist yet -> workspace-prep via --workspace-path
	dstDir := t.TempDir()
	write(t, filepath.Join(dstDir, "clairvoyance-store.json"), `{"workspaces":[{"id":"workspace-home","name":"Home","path":"`+jsonPath(dstDir)+`"}]}`)
	write(t, filepath.Join(dstDir, "profiles", "prof2", "staff.json"), "[]")
	dstWSPath := filepath.Join(dstDir, "Workspaces", "Proj")

	dst, _ := clv.Open(dstDir)
	rep, err := importer.Apply(pkgPath, dst, importer.Options{Passphrase: "proj-pass-strong", WorkspacePath: dstWSPath})
	if err != nil {
		t.Fatalf("import workspace: %v", err)
	}
	if rep.Tier != 3 {
		t.Fatalf("expected tier 3, got %d", rep.Tier)
	}

	// workspace registered on target
	dst2, _ := clv.Open(dstDir)
	if _, ok := dst2.WorkspaceByName("Proj"); !ok {
		t.Fatal("Proj workspace not registered on target")
	}
	// personas merged
	if _, err := dst2.FindPersona("Alpha"); err != nil {
		t.Fatalf("Alpha not imported: %v", err)
	}
	if _, err := dst2.FindPersona("Beta"); err != nil {
		t.Fatalf("Beta not imported: %v", err)
	}
	// memory + content landed in the target workspace path
	assertFile(t, filepath.Join(dstWSPath, ".Clairvoyance", "staff", "alpha", "index.md"), "alpha memory")
	assertFile(t, filepath.Join(dstWSPath, ".Clairvoyance", "memory", "shared.md"), "shared workspace memory")
	assertFile(t, filepath.Join(dstWSPath, "notes", "plan.md"), "the plan")
	// HEAVY dir must have been excluded from Tier 3
	if _, err := os.Stat(filepath.Join(dstWSPath, "venv", "big.dll")); err == nil {
		t.Fatal("heavy dir 'venv' leaked into Tier 3 workspace package")
	}
}

func jsonPath(p string) string {
	// escape backslashes for embedding in a JSON string literal
	out := make([]rune, 0, len(p)+8)
	for _, r := range p {
		if r == '\\' {
			out = append(out, '\\', '\\')
		} else {
			out = append(out, r)
		}
	}
	return string(out)
}
