package scan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScan_PlantedSecrets(t *testing.T) {
	s, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"anthropic":   "here is my key sk-ant-abcdef0123456789ABCDEF more",
		"github-pat":  `token: github_pat_11ABCDEFGHIJ0123456789_abcdefghij`,
		"aws":         "AKIAIOSFODNN7EXAMPLE",
		"json-secret": `{"client_secret":"abcdef012345678"}`,
		"pem":         "-----BEGIN RSA PRIVATE KEY-----",
	}
	for name, body := range cases {
		if m := s.Bytes(name, []byte(body)); len(m) == 0 {
			t.Errorf("%s: expected a secret match, got none", name)
		}
	}
}

func TestScan_CleanPasses(t *testing.T) {
	s, _ := New(nil)
	clean := "This is an ordinary note about the backup system. No secrets here."
	if m := s.Bytes("note", []byte(clean)); len(m) != 0 {
		t.Errorf("clean text flagged: %+v", m)
	}
}

func TestScan_BinarySkipped(t *testing.T) {
	s, _ := New(nil)
	// Contains a NUL byte -> treated as binary, not scanned.
	bin := []byte("sk-ant-abcdef0123456789ABCDEF\x00\x01\x02")
	if m := s.Bytes("blob", bin); len(m) != 0 {
		t.Errorf("binary content should be skipped, got %+v", m)
	}
}

// P4: a large file (past the old 5 MiB cap) must still be scanned, not silently
// passed as clean. Previously File() returned nil,nil for anything over MaxBytes.
func TestScan_File_LargeFileStreamed(t *testing.T) {
	s, _ := New(nil)
	dir := t.TempDir()
	p := filepath.Join(dir, "big.log")
	var b strings.Builder
	b.Grow(6 << 20)
	for b.Len() < 6<<20 { // exceed the old 5 MiB skip threshold
		b.WriteString("ordinary line of harmless log text padding padding padding\n")
	}
	b.WriteString("trailing sk-ant-abcdef0123456789ABCDEF secret at the end\n")
	if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	m, sk, err := s.File(p)
	if err != nil {
		t.Fatal(err)
	}
	if sk != nil {
		t.Fatalf("large text file wrongly skipped: %+v", sk)
	}
	if len(m) == 0 {
		t.Fatal("secret in a >5MiB file was not detected (silent skip regression)")
	}
}

// P4: a binary file on disk must be reported as a Skip, not silently clean.
func TestScan_File_BinaryReportsSkip(t *testing.T) {
	s, _ := New(nil)
	dir := t.TempDir()
	p := filepath.Join(dir, "blob.bin")
	if err := os.WriteFile(p, []byte("sk-ant-abcdef0123456789ABCDEF\x00\x01\x02"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, sk, err := s.File(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 0 {
		t.Errorf("binary file should not text-match, got %+v", m)
	}
	if sk == nil {
		t.Fatal("binary file must produce a Skip, not a silent clean pass")
	}
}
