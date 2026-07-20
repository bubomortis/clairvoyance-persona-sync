package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "memory", "reegor"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"definition.json":          `{"id":"staff-1","name":"Reegor"}`,
		"memory/reegor/index.md":   "# Reegor\nnotes",
		"history/staff-1.json":     `{"messages":[]}`,
	}
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestManifest_BuildVerifyRoundtrip(t *testing.T) {
	root := writeTree(t)
	m, err := Build(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(m.Entries))
	}
	// serialize -> parse -> verify
	b, err := m.JSON()
	if err != nil {
		t.Fatal(err)
	}
	m2, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	if err := m2.Verify(root); err != nil {
		t.Fatalf("verify of untouched tree failed: %v", err)
	}
}

func TestManifest_DetectsTamper(t *testing.T) {
	root := writeTree(t)
	m, _ := Build(root)
	// Tamper with a file after manifest is built.
	if err := os.WriteFile(filepath.Join(root, "definition.json"), []byte("TAMPERED"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := m.Verify(root)
	if err == nil {
		t.Fatal("expected mismatch error, got nil")
	}
	if _, ok := err.(*MismatchError); !ok {
		t.Fatalf("expected *MismatchError, got %T: %v", err, err)
	}
}
