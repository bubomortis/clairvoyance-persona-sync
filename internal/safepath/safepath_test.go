package safepath

import (
	"path/filepath"
	"testing"
)

func TestSafeJoin_Valid(t *testing.T) {
	root := t.TempDir()
	got, err := SafeJoin(root, "memory/reegor/index.md")
	if err != nil {
		t.Fatalf("valid path rejected: %v", err)
	}
	want := filepath.Join(root, "memory", "reegor", "index.md")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestSafeJoin_Rejects(t *testing.T) {
	root := t.TempDir()
	bad := []string{
		"../../etc/passwd",
		`..\..\Windows\System32`,
		"/etc/shadow",
		`\\server\share\x`,
		`C:\Windows\notepad.exe`,
		"a/../../b",
		"",
	}
	for _, r := range bad {
		if _, err := SafeJoin(root, r); err == nil {
			t.Errorf("expected rejection for %q, got none", r)
		}
	}
}

func TestSafeJoin_DotSelf(t *testing.T) {
	root := t.TempDir()
	if _, err := SafeJoin(root, "./a/b"); err != nil {
		t.Fatalf("relative ./a/b should be allowed: %v", err)
	}
}
