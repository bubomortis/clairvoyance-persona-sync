// Package state persists small clvsync preferences beside the Clairvoyance data dir —
// currently the last directory an export was written to, so future exports can default
// to it (prompt once, remember thereafter).
package state

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const fileName = "clvsync-state.json"

// State is the persisted preference set.
type State struct {
	LastExportDir string `json:"lastExportDir,omitempty"`
}

func path(dataDir string) string { return filepath.Join(dataDir, fileName) }

// Load reads the state for a data dir (zero value if absent or unreadable).
func Load(dataDir string) State {
	var s State
	if b, err := os.ReadFile(path(dataDir)); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

// Save writes the state for a data dir.
func Save(dataDir string, s State) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(path(dataDir), b, 0o644)
}

// RememberExportDir stores dir as the last export location (no-op on empty dir).
func RememberExportDir(dataDir, dir string) error {
	if dir == "" {
		return nil
	}
	s := Load(dataDir)
	s.LastExportDir = dir
	return Save(dataDir, s)
}
