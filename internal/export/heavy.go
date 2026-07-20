package export

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/bubomortis/clairvoyance-persona-sync/internal/clv"
	"github.com/bubomortis/clairvoyance-persona-sync/internal/pkg"
)

// HeavySize sums the bytes of the workspace's ballooning/regenerable dirs (the
// content deferred to Tier 4). Used by the §8a space preflight.
func HeavySize(in *clv.Instance, wsName string) int64 {
	ws, ok := in.WorkspaceByName(wsName)
	if !ok {
		return 0
	}
	var total int64
	_ = filepath.WalkDir(ws.Path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			rel, _ := filepath.Rel(ws.Path, p)
			if rel != "." && pkg.HeavyDirs[strings.ToLower(d.Name())] {
				_ = filepath.WalkDir(p, func(q string, dd fs.DirEntry, e error) error {
					if e == nil && !dd.IsDir() {
						if fi, er := dd.Info(); er == nil {
							total += fi.Size()
						}
					}
					return nil
				})
				return filepath.SkipDir
			}
		}
		return nil
	})
	return total
}

// WorkspaceHeavy packages ONLY the workspace's heavy/regenerable dirs (Tier 4),
// as a separate add-on to the Tier-3 package.
func WorkspaceHeavy(in *clv.Instance, wsName, outPath string, opts Options) (*Result, error) {
	ws, ok := in.WorkspaceByName(wsName)
	if !ok {
		return nil, fmt.Errorf("workspace %q not found", wsName)
	}
	stage, err := os.MkdirTemp("", "clvsync-heavy-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)

	dst := filepath.Join(stage, "workspace-heavy")
	err = filepath.WalkDir(ws.Path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			rel, _ := filepath.Rel(ws.Path, p)
			if rel != "." && pkg.HeavyDirs[strings.ToLower(d.Name())] {
				if err := pkg.CopyTree(p, filepath.Join(dst, rel), nil); err != nil {
					return err
				}
				return filepath.SkipDir
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	meta := newMeta(4)
	meta.WorkspaceName = wsName
	return finalize(stage, outPath, meta, opts)
}
