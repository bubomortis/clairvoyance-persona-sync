// Package safepath guards against path traversal / zip-slip on import (audit S3).
//
// Every package-relative path is resolved against an allowed root and the result
// is proven to stay inside that root; absolute, drive-letter, UNC, device, and
// ".." escaping paths are rejected. This is the Go reimplementation of the backup
// engine's Test-SafeTarget guard.
package safepath

import (
	"fmt"
	"path/filepath"
	"strings"
)

// SafeJoin joins root and a package-relative path (rel), guaranteeing the result
// stays within root. It returns the cleaned absolute path or an error.
func SafeJoin(root, rel string) (string, error) {
	if strings.TrimSpace(rel) == "" {
		return "", fmt.Errorf("empty path")
	}
	r := strings.ReplaceAll(rel, "\\", "/")

	// Reject absolute POSIX paths and UNC.
	if strings.HasPrefix(r, "/") {
		return "", fmt.Errorf("absolute/UNC path rejected: %q", rel)
	}
	// Reject Windows drive-letter (e.g. C:) and device paths.
	if len(r) >= 2 && r[1] == ':' {
		return "", fmt.Errorf("drive-letter path rejected: %q", rel)
	}
	if strings.Contains(r, "\x00") {
		return "", fmt.Errorf("NUL in path rejected: %q", rel)
	}

	cleanRoot, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return "", err
	}
	joined := filepath.Clean(filepath.Join(cleanRoot, filepath.FromSlash(r)))

	rel2, err := filepath.Rel(cleanRoot, joined)
	if err != nil {
		return "", fmt.Errorf("cannot relativize %q: %w", rel, err)
	}
	if rel2 == ".." || strings.HasPrefix(rel2, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes root: %q", rel)
	}
	return joined, nil
}
