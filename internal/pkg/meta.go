package pkg

import (
	"io"
	"os"
	"path/filepath"
)

// Meta describes a package's tier and persona for import (embedded as meta.json).
type Meta struct {
	SchemaVersion int      `json:"schemaVersion"`
	Tier          int      `json:"tier"`
	PersonaID     string   `json:"personaId"`
	PersonaName   string   `json:"personaName"`
	Template      string   `json:"template,omitempty"`
	Scopes        []string `json:"scopes"` // memory scopes present ("home" / workspace names)
	CreatedAt     string   `json:"createdAt"`
	SourceOS      string   `json:"sourceOS"`
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
