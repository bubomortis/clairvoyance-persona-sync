package pkg

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestTar_Roundtrip(t *testing.T) {
	stage := t.TempDir()
	files := map[string]string{
		"a.txt":       "alpha",
		"sub/b.txt":   "bravo",
		"sub/c/d.txt": "delta",
	}
	for rel, body := range files {
		p := filepath.Join(stage, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var buf bytes.Buffer
	if err := WriteTar(stage, &buf); err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	if err := ExtractTar(&buf, dest); err != nil {
		t.Fatal(err)
	}
	for rel, body := range files {
		got, err := os.ReadFile(filepath.Join(dest, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		if string(got) != body {
			t.Errorf("%s: got %q want %q", rel, got, body)
		}
	}
}

// A malicious tar with a traversal entry must be rejected by ExtractTar (S3).
func TestExtractTar_RejectsTraversal(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	body := []byte("pwned")
	hdr := &tar.Header{Name: "../evil.txt", Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	tw.Close()

	dest := t.TempDir()
	if err := ExtractTar(&buf, dest); err == nil {
		t.Fatal("expected traversal entry to be rejected")
	}
	// Ensure nothing was written outside dest.
	if _, err := os.Stat(filepath.Join(filepath.Dir(dest), "evil.txt")); err == nil {
		t.Fatal("traversal file escaped destination")
	}
}
