// Package pkg builds and extracts the clvsync tar container.
//
// Extraction validates every entry path with safepath (audit S3) so a malicious
// package cannot write outside the destination (zip-slip). Only regular files and
// directories are materialized; symlinks/devices are skipped.
package pkg

import (
	"archive/tar"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/bubomortis/clairvoyance-persona-sync/internal/safepath"
)

// WriteTar writes every regular file under stageDir into w as a deterministic
// (sorted, forward-slash) tar stream.
func WriteTar(stageDir string, w io.Writer) error {
	tw := tar.NewWriter(w)
	var files []string
	err := filepath.WalkDir(stageDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(files)
	for _, p := range files {
		rel, err := filepath.Rel(stageDir, p)
		if err != nil {
			return err
		}
		info, err := os.Stat(p)
		if err != nil {
			return err
		}
		hdr := &tar.Header{
			Name:     filepath.ToSlash(rel),
			Mode:     0o644,
			Size:     info.Size(),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		if _, err := io.Copy(tw, f); err != nil {
			f.Close()
			return err
		}
		f.Close()
	}
	return tw.Close()
}

// ExtractTar extracts a tar stream into destDir, rejecting any entry whose path
// escapes destDir.
func ExtractTar(r io.Reader, destDir string) error {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		target, err := safepath.SafeJoin(destDir, hdr.Name)
		if err != nil {
			return err // zip-slip / traversal rejected
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()
		default:
			// skip symlinks, devices, etc.
		}
	}
	return nil
}
