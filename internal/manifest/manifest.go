// Package manifest builds and verifies per-file SHA-256 manifests (audit S8).
package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// Entry is one file's integrity record.
type Entry struct {
	Path   string `json:"path"` // package-relative, forward slashes
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

// Manifest is the full per-file integrity record for a package tree.
type Manifest struct {
	Version int     `json:"version"`
	Entries []Entry `json:"entries"`
}

// Build walks root and produces a sorted manifest of all regular files.
func Build(root string) (*Manifest, error) {
	m := &Manifest{Version: 1}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		sum, n, err := hashFile(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		m.Entries = append(m.Entries, Entry{Path: filepath.ToSlash(rel), SHA256: sum, Bytes: n})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(m.Entries, func(i, j int) bool { return m.Entries[i].Path < m.Entries[j].Path })
	return m, nil
}

// Verify checks every entry against a file under root; returns a *MismatchError on the first divergence.
func (m *Manifest) Verify(root string) error {
	for _, e := range m.Entries {
		p := filepath.Join(root, filepath.FromSlash(e.Path))
		sum, n, err := hashFile(p)
		if err != nil {
			return err
		}
		if sum != e.SHA256 || n != e.Bytes {
			return &MismatchError{Path: e.Path}
		}
	}
	return nil
}

// JSON serializes the manifest indented.
func (m *Manifest) JSON() ([]byte, error) { return json.MarshalIndent(m, "", "  ") }

// Parse deserializes a manifest.
func Parse(b []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// MismatchError reports an integrity failure for a specific path.
type MismatchError struct{ Path string }

func (e *MismatchError) Error() string { return "manifest mismatch: " + e.Path }

func hashFile(p string) (string, int64, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}
