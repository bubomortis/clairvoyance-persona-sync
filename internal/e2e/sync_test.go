package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/bubomortis/clairvoyance-persona-sync/internal/clv"
	"github.com/bubomortis/clairvoyance-persona-sync/internal/export"
	"github.com/bubomortis/clairvoyance-persona-sync/internal/importer"
)

const syncID = "staff-1783000000001-syncy"

// miniInstance builds a minimal data dir (Home workspace only) with one persona entry.
func miniInstance(t *testing.T, entry string) string {
	t.Helper()
	dir := t.TempDir()
	s := map[string]any{"workspaces": []map[string]string{{"id": "workspace-home", "name": "Home", "path": dir}}}
	b, _ := json.Marshal(s)
	write(t, filepath.Join(dir, "clairvoyance-store.json"), string(b))
	write(t, filepath.Join(dir, "profiles", "prof1", "staff.json"), "["+entry+"]")
	return dir
}

func entryOf(t *testing.T, dataDir, id string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dataDir, "profiles", "prof1", "staff.json"))
	if err != nil {
		t.Fatal(err)
	}
	var arr []map[string]any
	if err := json.Unmarshal(b, &arr); err != nil {
		t.Fatal(err)
	}
	for _, e := range arr {
		if e["id"] == id {
			return e
		}
	}
	t.Fatalf("entry %s not found", id)
	return nil
}

// TestSync_PreservesMachineLocal is the core round-trip guarantee (§17.1, D12):
// a sync-merge updates portable fields from the incoming package but keeps the
// destination's machine-local runtime.
func TestSync_PreservesMachineLocal(t *testing.T) {
	srcEntry := `{"id":"` + syncID + `","name":"Syncy","knowledgeTemplate":"","jobDescription":"NEW role",` +
		`"model":"claude-opus-4-8","runtime":"acp-source","shell":"pwsh"}`
	srcDir := miniInstance(t, srcEntry)
	src, _ := clv.Open(srcDir)
	p, err := src.FindPersona(syncID)
	if err != nil {
		t.Fatal(err)
	}
	pkgPath := filepath.Join(t.TempDir(), "syncy.cvpkg")
	if _, err := export.Persona(src, p, pkgPath, export.Options{}); err != nil {
		t.Fatalf("export: %v", err)
	}

	// Destination already has the persona with DIFFERENT machine-local values + old role.
	dstEntry := `{"id":"` + syncID + `","name":"Syncy","knowledgeTemplate":"","jobDescription":"OLD role",` +
		`"model":"local-sonnet","runtime":"acp-dest","shell":"bash","isDefault":true}`
	dstDir := miniInstance(t, dstEntry)
	dst, _ := clv.Open(dstDir)

	rep, err := importer.Apply(pkgPath, dst, importer.Options{Mode: clv.ModeSync})
	if err != nil {
		t.Fatalf("import sync: %v", err)
	}
	if rep.Mode != "sync" {
		t.Fatalf("expected sync mode, got %s", rep.Mode)
	}

	e := entryOf(t, dstDir, syncID)
	if e["jobDescription"] != "NEW role" {
		t.Fatalf("portable jobDescription not updated: %v", e["jobDescription"])
	}
	if e["model"] != "local-sonnet" {
		t.Fatalf("machine-local model was clobbered: got %v want local-sonnet", e["model"])
	}
	if e["runtime"] != "acp-dest" {
		t.Fatalf("machine-local runtime was clobbered: got %v", e["runtime"])
	}
	if e["shell"] != "bash" {
		t.Fatalf("machine-local shell was clobbered: got %v", e["shell"])
	}
	if e["isDefault"] != true {
		t.Fatalf("machine-local isDefault was clobbered: got %v", e["isDefault"])
	}
}

// TestOverwriteMode replaces the whole entry (machine-local included).
func TestOverwriteMode(t *testing.T) {
	srcEntry := `{"id":"` + syncID + `","name":"Syncy","jobDescription":"NEW","model":"opus","runtime":"acp-source"}`
	srcDir := miniInstance(t, srcEntry)
	src, _ := clv.Open(srcDir)
	p, _ := src.FindPersona(syncID)
	pkgPath := filepath.Join(t.TempDir(), "syncy.cvpkg")
	if _, err := export.Persona(src, p, pkgPath, export.Options{}); err != nil {
		t.Fatal(err)
	}
	dstEntry := `{"id":"` + syncID + `","name":"Syncy","jobDescription":"OLD","model":"local","runtime":"acp-dest"}`
	dstDir := miniInstance(t, dstEntry)
	dst, _ := clv.Open(dstDir)
	if _, err := importer.Apply(pkgPath, dst, importer.Options{Mode: clv.ModeOverwrite}); err != nil {
		t.Fatal(err)
	}
	e := entryOf(t, dstDir, syncID)
	if e["model"] != "opus" || e["runtime"] != "acp-source" {
		t.Fatalf("overwrite should replace machine-local: %v", e)
	}
}

// TestSkipMode leaves an existing persona untouched.
func TestSkipMode(t *testing.T) {
	srcEntry := `{"id":"` + syncID + `","name":"Syncy","jobDescription":"NEW"}`
	srcDir := miniInstance(t, srcEntry)
	src, _ := clv.Open(srcDir)
	p, _ := src.FindPersona(syncID)
	pkgPath := filepath.Join(t.TempDir(), "syncy.cvpkg")
	export.Persona(src, p, pkgPath, export.Options{})

	dstEntry := `{"id":"` + syncID + `","name":"Syncy","jobDescription":"OLD"}`
	dstDir := miniInstance(t, dstEntry)
	dst, _ := clv.Open(dstDir)
	if _, err := importer.Apply(pkgPath, dst, importer.Options{Mode: clv.ModeSkip}); err != nil {
		t.Fatal(err)
	}
	if e := entryOf(t, dstDir, syncID); e["jobDescription"] != "OLD" {
		t.Fatalf("skip mode should not change the entry: %v", e["jobDescription"])
	}
}

// TestDryRun writes nothing.
func TestDryRun(t *testing.T) {
	srcEntry := `{"id":"` + syncID + `","name":"Syncy","jobDescription":"NEW"}`
	srcDir := miniInstance(t, srcEntry)
	// give it home memory so there's more to (not) place
	write(t, filepath.Join(srcDir, ".Clairvoyance", "staff", "syncy", "note.md"), "hello")
	src, _ := clv.Open(srcDir)
	p, _ := src.FindPersona(syncID)
	pkgPath := filepath.Join(t.TempDir(), "syncy.cvpkg")
	export.Persona(src, p, pkgPath, export.Options{})

	dstDir := miniInstance(t, `{"id":"other","name":"Other"}`)
	dst, _ := clv.Open(dstDir)
	rep, err := importer.Apply(pkgPath, dst, importer.Options{Mode: clv.ModeSync, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.DryRun || len(rep.Placed) != 0 {
		t.Fatalf("dry-run should place nothing, got %v", rep.Placed)
	}
	if len(rep.Plan) == 0 {
		t.Fatal("dry-run should still produce a plan")
	}
	// persona must NOT have been added
	if _, err := dst.FindPersona(syncID); err == nil {
		t.Fatal("dry-run must not write the persona")
	}
	if _, err := os.Stat(filepath.Join(dstDir, ".Clairvoyance", "staff", "syncy", "note.md")); err == nil {
		t.Fatal("dry-run must not write memory")
	}
}

// TestMemoryUnion: a sync-merge adds new memory files and backs up changed ones.
func TestMemoryUnion(t *testing.T) {
	srcDir := miniInstance(t, `{"id":"`+syncID+`","name":"Syncy"}`)
	write(t, filepath.Join(srcDir, ".Clairvoyance", "staff", "syncy", "a.md"), "from-source")
	write(t, filepath.Join(srcDir, ".Clairvoyance", "staff", "syncy", "new.md"), "brand-new")
	src, _ := clv.Open(srcDir)
	p, _ := src.FindPersona(syncID)
	pkgPath := filepath.Join(t.TempDir(), "syncy.cvpkg")
	export.Persona(src, p, pkgPath, export.Options{})

	dstDir := miniInstance(t, `{"id":"`+syncID+`","name":"Syncy"}`)
	write(t, filepath.Join(dstDir, ".Clairvoyance", "staff", "syncy", "a.md"), "local-version")
	write(t, filepath.Join(dstDir, ".Clairvoyance", "staff", "syncy", "keep.md"), "local-only")
	dst, _ := clv.Open(dstDir)
	if _, err := importer.Apply(pkgPath, dst, importer.Options{Mode: clv.ModeSync}); err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(dstDir, ".Clairvoyance", "staff", "syncy")
	assertFile(t, filepath.Join(base, "a.md"), "from-source")        // updated
	assertFile(t, filepath.Join(base, "a.md.clvsync-bak"), "local-version") // backed up
	assertFile(t, filepath.Join(base, "new.md"), "brand-new")        // added
	assertFile(t, filepath.Join(base, "keep.md"), "local-only")      // untouched union
}

// TestOperatorGuard_Export refuses to export the Sync Operator without the override.
func TestOperatorGuard_Export(t *testing.T) {
	opEntry := `{"id":"op-1","name":"Sync Operator","knowledgeTemplate":"Sync Operator","jobDescription":"imports"}`
	srcDir := miniInstance(t, opEntry)
	src, _ := clv.Open(srcDir)
	p, _ := src.FindPersona("op-1")
	if !p.IsOperator() {
		t.Fatal("operator not recognized by marker")
	}
	pkgPath := filepath.Join(t.TempDir(), "op.cvpkg")
	if _, err := export.Persona(src, p, pkgPath, export.Options{}); err == nil {
		t.Fatal("expected export of the Sync Operator to be blocked")
	}
	if _, err := export.Persona(src, p, pkgPath, export.Options{AllowOperatorSync: true}); err != nil {
		t.Fatalf("override should allow operator export: %v", err)
	}
}

// TestOperatorGuard_Import blocks by default and HARD-blocks self-overwrite (§19.3).
func TestOperatorGuard_Import(t *testing.T) {
	opEntry := `{"id":"op-1","name":"Sync Operator","knowledgeTemplate":"Sync Operator"}`
	srcDir := miniInstance(t, opEntry)
	src, _ := clv.Open(srcDir)
	p, _ := src.FindPersona("op-1")
	pkgPath := filepath.Join(t.TempDir(), "op.cvpkg")
	if _, err := export.Persona(src, p, pkgPath, export.Options{AllowOperatorSync: true}); err != nil {
		t.Fatal(err)
	}

	// Target with NO operator: blocked without flag, allowed with flag.
	dstDir := miniInstance(t, `{"id":"other","name":"Other"}`)
	dst, _ := clv.Open(dstDir)
	if _, err := importer.Apply(pkgPath, dst, importer.Options{}); err == nil {
		t.Fatal("import of operator should be blocked by default")
	}
	if _, err := importer.Apply(pkgPath, dst, importer.Options{AllowOperatorSync: true}); err != nil {
		t.Fatalf("override should allow importing a new operator: %v", err)
	}

	// Target that ALREADY has that operator id: hard block even with the flag.
	dst2Dir := miniInstance(t, `{"id":"op-1","name":"Sync Operator","knowledgeTemplate":"Sync Operator"}`)
	dst2, _ := clv.Open(dst2Dir)
	if _, err := importer.Apply(pkgPath, dst2, importer.Options{AllowOperatorSync: true}); err == nil {
		t.Fatal("self-overwrite of the live operator must be hard-blocked even with the override")
	}
}

// TestReceipt_VerifyImport: a real import writes a receipt that reconciles clean.
func TestReceipt_VerifyImport(t *testing.T) {
	srcDir := miniInstance(t, `{"id":"`+syncID+`","name":"Syncy","jobDescription":"role"}`)
	write(t, filepath.Join(srcDir, ".Clairvoyance", "staff", "syncy", "m.md"), "memory")
	src, _ := clv.Open(srcDir)
	p, _ := src.FindPersona(syncID)
	pkgPath := filepath.Join(t.TempDir(), "syncy.cvpkg")
	export.Persona(src, p, pkgPath, export.Options{})

	dstDir := miniInstance(t, `{"id":"other","name":"Other"}`)
	dst, _ := clv.Open(dstDir)
	receiptPath := filepath.Join(t.TempDir(), "import-receipt.json")
	rep, err := importer.Apply(pkgPath, dst, importer.Options{Mode: clv.ModeSync, ReceiptPath: receiptPath})
	if err != nil {
		t.Fatal(err)
	}
	if rep.ReceiptPath != receiptPath {
		t.Fatalf("receipt path not reported: %q", rep.ReceiptPath)
	}
	rec, err := importer.LoadReceipt(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Files) == 0 {
		t.Fatal("receipt recorded no hashed files")
	}
	dst2, _ := clv.Open(dstDir) // reopen to see the merged persona
	res := importer.VerifyReceipt(rec, dst2)
	if !res.OK {
		for _, l := range res.Lines {
			if !l.OK {
				t.Errorf("FAIL %s: %s", l.Layer, l.Detail)
			}
		}
		t.Fatal("verify-import reconciliation should pass on a clean import")
	}

	// Tamper with a placed file → reconciliation must fail.
	os.WriteFile(rec.Files[0].Path, []byte("tampered"), 0o644)
	if importer.VerifyReceipt(rec, dst2).OK {
		t.Fatal("verify-import should detect a tampered placed file")
	}
}
