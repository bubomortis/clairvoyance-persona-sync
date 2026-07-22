package e2e

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bubomortis/clairvoyance-persona-sync/internal/clv"
	"github.com/bubomortis/clairvoyance-persona-sync/internal/export"
	"github.com/bubomortis/clairvoyance-persona-sync/internal/importer"
)

// D19 --include-agent-memory: the rich .claude/projects/<munge>/memory store travels only
// on opt-in, is secret-scanned, and is remapped to the TARGET machine's home + munge on
// import. This exercises both remaps: a distinct AgentHome per side, and a ghost source cwd
// that repoints to the target data dir (so the target munge differs from the source munge).
func TestRoundtrip_AgentMemory(t *testing.T) {
	srcDir := t.TempDir()
	srcHome := t.TempDir()
	ghostCwd := `Z:\ghost\ws` // exists on neither machine → repoints on import

	write(t, filepath.Join(srcDir, "clairvoyance-store.json"), storeJSON(t, srcDir, "TestWS", filepath.Join(srcDir, "ws")))
	write(t, filepath.Join(srcDir, "profiles", "prof1", "staff.json"),
		`[{"id":"staff-am-1","name":"Amy","shell":{"cwd":"Z:\\ghost\\ws"},"ai":{"model":"claude","permissionMode":"standard"}}]`)
	// Source agent-memory under the ghost cwd's munge.
	wantMem := "# Amy's working memory\nfavorite color: teal\n"
	srcMemDir, _ := clv.AgentMemoryDir(srcHome, ghostCwd)
	write(t, filepath.Join(srcMemDir, "MEMORY.md"), wantMem)
	write(t, filepath.Join(srcMemDir, "sub", "note.md"), "nested\n")

	src, _ := clv.Open(srcDir)
	src.AgentHome = srcHome
	p, err := src.FindPersona("Amy")
	if err != nil {
		t.Fatalf("find persona: %v", err)
	}

	pkgPath := filepath.Join(t.TempDir(), "amy.cvpkg.age")
	if _, err := export.Persona(src, p, pkgPath, export.Options{Passphrase: "strong-enough-pass", IncludeAgentMemory: true}); err != nil {
		t.Fatalf("export: %v", err)
	}

	// Target: different data dir, different AgentHome, empty roster.
	dstDir := t.TempDir()
	dstHome := t.TempDir()
	write(t, filepath.Join(dstDir, "clairvoyance-store.json"), storeJSON(t, dstDir, "TestWS", filepath.Join(dstDir, "ws")))
	write(t, filepath.Join(dstDir, "profiles", "prof2", "staff.json"), "[]")

	dst, _ := clv.Open(dstDir)
	dst.AgentHome = dstHome
	if _, err := importer.Apply(pkgPath, dst, importer.Options{Passphrase: "strong-enough-pass"}); err != nil {
		t.Fatalf("import: %v", err)
	}

	// The ghost cwd repointed to dstDir on import, so the memory lands under dstHome +
	// munge(dstDir), NOT the source munge.
	dstMemDir, _ := clv.AgentMemoryDir(dstHome, dstDir)
	assertFile(t, filepath.Join(dstMemDir, "MEMORY.md"), wantMem)
	assertFile(t, filepath.Join(dstMemDir, "sub", "note.md"), "nested\n")

	// And it must NOT have been written under the (untranslated) source munge on the target.
	srcMungeOnDst, _ := clv.AgentMemoryDir(dstHome, ghostCwd)
	if _, err := os.Stat(srcMungeOnDst); !os.IsNotExist(err) {
		t.Errorf("agent-memory leaked under the source munge on target (err=%v)", err)
	}
}

// Without --include-agent-memory, the store must not travel at all.
func TestRoundtrip_AgentMemory_OptInOnly(t *testing.T) {
	srcDir := t.TempDir()
	srcHome := t.TempDir()
	write(t, filepath.Join(srcDir, "clairvoyance-store.json"), storeJSON(t, srcDir, "TestWS", filepath.Join(srcDir, "ws")))
	write(t, filepath.Join(srcDir, "profiles", "prof1", "staff.json"),
		`[{"id":"staff-am-2","name":"Bea","shell":{"cwd":"Z:\\ghost\\ws"},"ai":{"model":"claude","permissionMode":"standard"}}]`)
	beaMem, _ := clv.AgentMemoryDir(srcHome, `Z:\ghost\ws`)
	write(t, filepath.Join(beaMem, "MEMORY.md"), "secret plan\n")

	src, _ := clv.Open(srcDir)
	src.AgentHome = srcHome
	p, _ := src.FindPersona("Bea")
	pkgPath := filepath.Join(t.TempDir(), "bea.cvpkg.age")
	if _, err := export.Persona(src, p, pkgPath, export.Options{Passphrase: "strong-enough-pass"}); err != nil { // no IncludeAgentMemory
		t.Fatalf("export: %v", err)
	}

	dstDir := t.TempDir()
	dstHome := t.TempDir()
	write(t, filepath.Join(dstDir, "clairvoyance-store.json"), storeJSON(t, dstDir, "TestWS", filepath.Join(dstDir, "ws")))
	write(t, filepath.Join(dstDir, "profiles", "prof2", "staff.json"), "[]")
	dst, _ := clv.Open(dstDir)
	dst.AgentHome = dstHome
	if _, err := importer.Apply(pkgPath, dst, importer.Options{Passphrase: "strong-enough-pass"}); err != nil {
		t.Fatalf("import: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dstHome, ".claude")); !os.IsNotExist(err) {
		t.Errorf("agent-memory traveled without --include-agent-memory (.claude exists on target)")
	}
}

// A secret in the rich agent-memory must block export like any other staged content (S1) —
// the .claude working store is exactly where a stray API key tends to live.
func TestExport_AgentMemory_BlocksPlantedSecret(t *testing.T) {
	srcDir := t.TempDir()
	srcHome := t.TempDir()
	write(t, filepath.Join(srcDir, "clairvoyance-store.json"), storeJSON(t, srcDir, "TestWS", filepath.Join(srcDir, "ws")))
	write(t, filepath.Join(srcDir, "profiles", "prof1", "staff.json"),
		`[{"id":"staff-am-3","name":"Cy","shell":{"cwd":"Z:\\ghost\\ws"},"ai":{"model":"claude","permissionMode":"standard"}}]`)
	cyMem, _ := clv.AgentMemoryDir(srcHome, `Z:\ghost\ws`)
	write(t, filepath.Join(cyMem, "leak.md"),
		"my key is sk-ant-abcdef0123456789ABCDEF do not share")

	src, _ := clv.Open(srcDir)
	src.AgentHome = srcHome
	p, _ := src.FindPersona("Cy")
	pkgPath := filepath.Join(t.TempDir(), "cy.cvpkg.age")
	if _, err := export.Persona(src, p, pkgPath, export.Options{Passphrase: "strong-enough-pass", IncludeAgentMemory: true}); err == nil {
		t.Fatal("expected secret in agent-memory to block export")
	}
}

// AM-1: even with a real agent-memory bundle present, if the persona's cwd on THIS machine
// resolves to ".." the import must refuse to place it rather than escape into
// <home>/.claude/memory. The bundle is genuine (exported from a valid source cwd); the
// TARGET persona is pre-seeded with cwd ".." so the sync-merge preserves that machine-local
// cwd, driving mergeAgentMemory to the degenerate munge.
func TestImport_AgentMemory_RefusesDegenerateCwd(t *testing.T) {
	srcDir := t.TempDir()
	srcHome := t.TempDir()
	write(t, filepath.Join(srcDir, "clairvoyance-store.json"), storeJSON(t, srcDir, "TestWS", filepath.Join(srcDir, "ws")))
	write(t, filepath.Join(srcDir, "profiles", "prof1", "staff.json"),
		`[{"id":"staff-am-4","name":"Dot","shell":{"cwd":"Z:\\real\\ws"},"ai":{"model":"claude","permissionMode":"standard"}}]`)
	srcMem, _ := clv.AgentMemoryDir(srcHome, `Z:\real\ws`)
	write(t, filepath.Join(srcMem, "MEMORY.md"), "should not escape\n")
	src, _ := clv.Open(srcDir)
	src.AgentHome = srcHome
	p, _ := src.FindPersona("Dot")
	pkgPath := filepath.Join(t.TempDir(), "dot.cvpkg.age")
	if _, err := export.Persona(src, p, pkgPath, export.Options{Passphrase: "strong-enough-pass", IncludeAgentMemory: true}); err != nil {
		t.Fatalf("export: %v", err)
	}

	// Target already has the persona with a ".." cwd; sync-merge preserves it.
	dstDir := t.TempDir()
	dstHome := t.TempDir()
	write(t, filepath.Join(dstDir, "clairvoyance-store.json"), storeJSON(t, dstDir, "TestWS", filepath.Join(dstDir, "ws")))
	write(t, filepath.Join(dstDir, "profiles", "prof2", "staff.json"),
		`[{"id":"staff-am-4","name":"Dot","shell":{"cwd":".."},"ai":{"model":"claude","permissionMode":"standard"}}]`)
	dst, _ := clv.Open(dstDir)
	dst.AgentHome = dstHome
	if _, err := importer.Apply(pkgPath, dst, importer.Options{Passphrase: "strong-enough-pass"}); err != nil {
		t.Fatalf("import: %v", err)
	}
	// The one-level-up escape target must never be created.
	if _, err := os.Stat(filepath.Join(dstHome, ".claude", "memory")); !os.IsNotExist(err) {
		t.Errorf("AM-1: agent-memory escaped to <home>/.claude/memory (err=%v)", err)
	}
}
