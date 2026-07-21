// Package selfupdate downloads the release binary matching this OS/arch, verifies
// its SHA-256 against the release SHA256SUMS, and replaces the running executable
// in place. The replacement is Windows-safe: a running .exe cannot be overwritten
// or deleted, but it CAN be renamed, so the current binary is moved aside to
// "<exe>.old" and the new one is put in its place.
package selfupdate

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/bubomortis/clairvoyance-persona-sync/internal/release"
)

// AssetName is the release asset name for the current OS/arch, e.g.
// "clvsync-windows-amd64.exe" or "clvsync-linux-amd64".
func AssetName() string {
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	return fmt.Sprintf("clvsync-%s-%s%s", runtime.GOOS, runtime.GOARCH, ext)
}

// Apply downloads this platform's asset from rel, verifies it against the release
// SHA256SUMS, and replaces the running executable. It returns the replaced path.
func Apply(ctx context.Context, rel *release.Release) (string, error) {
	name := AssetName()
	binURL, ok := rel.AssetURL(name)
	if !ok {
		return "", fmt.Errorf("release %s has no asset %q for this platform — build from source instead", rel.Tag, name)
	}
	sumsURL, ok := rel.AssetURL("SHA256SUMS")
	if !ok {
		return "", fmt.Errorf("release %s has no SHA256SUMS to verify against; refusing to install unverified binary", rel.Tag)
	}

	bin, err := download(ctx, binURL)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", name, err)
	}
	sums, err := download(ctx, sumsURL)
	if err != nil {
		return "", fmt.Errorf("download SHA256SUMS: %w", err)
	}
	want, err := checksumFor(string(sums), name)
	if err != nil {
		return "", err
	}
	got := sha256.Sum256(bin)
	if !strings.EqualFold(hex.EncodeToString(got[:]), want) {
		return "", fmt.Errorf("checksum mismatch for %s — refusing to install (expected %s, got %x)", name, want, got)
	}
	return replaceSelf(bin)
}

// download fetches a URL fully into memory (release assets are small, ~10 MB).
func download(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "clvsync-selfupdate")
	resp, err := (&http.Client{Timeout: 5 * time.Minute}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s", resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// checksumFor finds the hex digest for name in a SHA256SUMS body. Lines are
// "<hexdigest>  <filename>" (a '*' before the name marks binary mode).
func checksumFor(sums, name string) (string, error) {
	sc := bufio.NewScanner(strings.NewReader(sums))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) >= 2 && strings.TrimPrefix(f[len(f)-1], "*") == name {
			return f[0], nil
		}
	}
	return "", fmt.Errorf("SHA256SUMS has no entry for %s", name)
}

// replaceSelf writes bin over the current executable via a same-directory temp
// file and rename, moving the running binary aside to "<exe>.old" first.
func replaceSelf(bin []byte) (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}
	dir := filepath.Dir(self)

	tmp, err := os.CreateTemp(dir, ".clvsync-new-*")
	if err != nil {
		return "", fmt.Errorf("stage new binary: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(bin); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return "", err
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		os.Remove(tmpName)
		return "", err
	}

	old := self + ".old"
	_ = os.Remove(old) // clear a stale .old from a prior update
	if err := os.Rename(self, old); err != nil {
		os.Remove(tmpName)
		return "", fmt.Errorf("move current executable aside: %w", err)
	}
	if err := os.Rename(tmpName, self); err != nil {
		_ = os.Rename(old, self) // roll back
		os.Remove(tmpName)
		return "", fmt.Errorf("install new executable: %w", err)
	}
	// Best-effort: on Windows the .old may stay locked until this process exits.
	_ = os.Remove(old)
	return self, nil
}
