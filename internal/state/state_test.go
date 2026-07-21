package state

import "testing"

func TestRememberAndLoad(t *testing.T) {
	dir := t.TempDir()
	if Load(dir).LastExportDir != "" {
		t.Fatal("expected empty initial state")
	}
	if err := RememberExportDir(dir, `D:\Clairvoyance\exports`); err != nil {
		t.Fatal(err)
	}
	if got := Load(dir).LastExportDir; got != `D:\Clairvoyance\exports` {
		t.Fatalf("remembered dir not loaded: %q", got)
	}
	// An empty dir is a no-op — the last real location stays.
	if err := RememberExportDir(dir, ""); err != nil {
		t.Fatal(err)
	}
	if got := Load(dir).LastExportDir; got != `D:\Clairvoyance\exports` {
		t.Fatalf("empty RememberExportDir clobbered state: %q", got)
	}
	// A new location overwrites.
	_ = RememberExportDir(dir, `E:\out`)
	if got := Load(dir).LastExportDir; got != `E:\out` {
		t.Fatalf("dir not updated: %q", got)
	}
}
