package pkg

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Meta describes a package's tier and contents for import (embedded as meta.json).
type Meta struct {
	SchemaVersion int      `json:"schemaVersion"`
	Tier          int      `json:"tier"`
	PersonaID     string   `json:"personaId,omitempty"`
	PersonaName   string   `json:"personaName,omitempty"`
	Template      string   `json:"template,omitempty"`
	Scopes        []string `json:"scopes,omitempty"` // memory scopes present ("home" / workspace names)
	AgentMemory   bool     `json:"agentMemory,omitempty"` // rich .claude/projects memory bundled (D19 --include-agent-memory)
	// Tier 3 (workspace):
	WorkspaceName string         `json:"workspaceName,omitempty"`
	Roster        []RosterEntry  `json:"roster,omitempty"`
	CreatedAt     string         `json:"createdAt"`
	SourceOS      string         `json:"sourceOS"`
}

// RosterEntry identifies a persona bundled inside a Tier-3 workspace package.
type RosterEntry struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Lname    string `json:"lname"`
	Template string `json:"template,omitempty"`
}

// HeavyDirs are the ballooning/regenerable directory names excluded from Tier 3
// (deferred to the Tier 4 heavy add-on).
var HeavyDirs = map[string]bool{
	"venv": true, ".venv": true, "site-packages": true, "node_modules": true,
	"models": true, "downloads": true, "__pycache__": true, ".git": true, "whisper": true,
}

// CopyTree copies src into dst, skipping any directory whose base name is in
// excludeDirs (case-insensitive). Existing files are overwritten.
func CopyTree(src, dst string, excludeDirs map[string]bool) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		if d.IsDir() {
			if rel != "." && excludeDirs[strings.ToLower(d.Name())] {
				return filepath.SkipDir
			}
			return nil
		}
		return CopyFile(p, filepath.Join(dst, rel))
	})
}

// CopyFile copies src to dst, creating parent directories.
func CopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
