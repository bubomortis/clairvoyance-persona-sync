// Package importer applies a Tier-1 package to a target instance (§9).
//
// Order: verify signature (S8) → decrypt (§7) → safe-extract (S3) → verify
// manifest (S8) → collision check (S6) → non-destructive merge with backup (S7).
package importer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"aead.dev/minisign"

	"github.com/bubomortis/clairvoyance-persona-sync/internal/clv"
	"github.com/bubomortis/clairvoyance-persona-sync/internal/cryptobox"
	"github.com/bubomortis/clairvoyance-persona-sync/internal/manifest"
	"github.com/bubomortis/clairvoyance-persona-sync/internal/pkg"
)

// Options controls decryption, verification, and collision handling.
type Options struct {
	Passphrase string
	Identity   string // age X25519 secret key
	Sig        []byte
	VerifyKey  *minisign.PublicKey
	Force      bool // overwrite on staff-id collision / re-import
}

// Report summarizes what an import did.
type Report struct {
	PersonaID     string
	PersonaName   string
	Tier          int
	Placed        []string
	SkippedScopes []string
	BackedUp      []string
	ReviewNote    string
}

// Apply imports a package into the target instance.
func Apply(pkgPath string, in *clv.Instance, opts Options) (*Report, error) {
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return nil, err
	}

	// 1. authenticity (before touching contents)
	if opts.VerifyKey != nil {
		if opts.Sig == nil {
			return nil, fmt.Errorf("verify key provided but no signature")
		}
		if !cryptobox.Verify(*opts.VerifyKey, data, opts.Sig) {
			return nil, fmt.Errorf("signature verification failed")
		}
	}

	// 2. decrypt
	var src io.Reader = bytes.NewReader(data)
	switch {
	case opts.Passphrase != "":
		r, err := cryptobox.DecryptPassphrase(bytes.NewReader(data), opts.Passphrase)
		if err != nil {
			return nil, fmt.Errorf("decrypt: %w", err)
		}
		src = r
	case opts.Identity != "":
		r, err := cryptobox.DecryptIdentity(bytes.NewReader(data), opts.Identity)
		if err != nil {
			return nil, fmt.Errorf("decrypt: %w", err)
		}
		src = r
	}

	// 3. safe extract
	stage, err := os.MkdirTemp("", "clvsync-import-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	if err := pkg.ExtractTar(src, stage); err != nil {
		return nil, fmt.Errorf("extract: %w", err)
	}

	// 4. integrity
	mb, err := os.ReadFile(filepath.Join(stage, "manifest.json"))
	if err != nil {
		return nil, err
	}
	mani, err := manifest.Parse(mb)
	if err != nil {
		return nil, err
	}
	if err := mani.Verify(stage); err != nil {
		return nil, fmt.Errorf("integrity check failed: %w", err)
	}

	// 5. meta
	var meta pkg.Meta
	mtb, err := os.ReadFile(filepath.Join(stage, "meta.json"))
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(mtb, &meta); err != nil {
		return nil, err
	}
	rep := &Report{PersonaID: meta.PersonaID, PersonaName: meta.PersonaName, Tier: meta.Tier,
		ReviewNote: "imported persona definition + memory are externally sourced; review before relying on this Staff member"}
	lname := clv.Slug(meta.PersonaName)

	// 6. collision (S6)
	if existing, _ := in.FindPersona(meta.PersonaID); existing != nil && !opts.Force {
		return nil, fmt.Errorf("collision: persona %q (%s) already on target; use Force to overwrite", existing.Name, existing.ID)
	}

	// 7. definition (S7: SpliceStaffEntry backs up first)
	entry, err := os.ReadFile(filepath.Join(stage, "definition", "staff-entry.json"))
	if err != nil {
		return nil, err
	}
	prof, err := in.DefaultProfile()
	if err != nil {
		return nil, err
	}
	if err := in.SpliceStaffEntry(prof, entry, meta.PersonaID, opts.Force); err != nil {
		return nil, err
	}
	rep.BackedUp = append(rep.BackedUp, "profiles/"+prof+"/staff.json")
	rep.Placed = append(rep.Placed, "definition")

	// 8. custom template (never overwrite an existing one)
	if meta.Template != "" {
		srcT := filepath.Join(stage, "definition", "personas", meta.Template)
		if _, err := os.Stat(srcT); err == nil {
			dstT := filepath.Join(in.DataDir, "neurons", "personas", meta.Template)
			if _, err := os.Stat(dstT); err != nil {
				if err := pkg.CopyFile(srcT, dstT); err != nil {
					return nil, err
				}
				rep.Placed = append(rep.Placed, "template:"+meta.Template)
			}
		}
	}

	// 9. memory (scope "home" -> datadir; workspace name -> matching target workspace)
	memRoot := filepath.Join(stage, "memory")
	if ents, err := os.ReadDir(memRoot); err == nil {
		for _, e := range ents {
			if !e.IsDir() {
				continue
			}
			scope := e.Name()
			srcDir := filepath.Join(memRoot, scope, lname)
			if _, err := os.Stat(srcDir); err != nil {
				continue
			}
			var base string
			if scope == "home" {
				base = in.DataDir
			} else if ws, ok := in.WorkspaceByName(scope); ok {
				base = ws.Path
			} else {
				rep.SkippedScopes = append(rep.SkippedScopes, scope)
				continue
			}
			dstDir := filepath.Join(clv.StaffDir(base), lname)
			if opts.Force {
				_ = os.RemoveAll(dstDir)
			}
			if err := os.MkdirAll(filepath.Dir(dstDir), 0o755); err != nil {
				return nil, err
			}
			if err := os.CopyFS(dstDir, os.DirFS(srcDir)); err != nil {
				return nil, err
			}
			rep.Placed = append(rep.Placed, "memory/"+scope)
		}
	}

	// 10. history
	srcH := filepath.Join(stage, "history", "staff-"+meta.PersonaID+".json")
	if _, err := os.Stat(srcH); err == nil {
		dstH := filepath.Join(in.DataDir, "profiles", prof, "agent-history", "staff-"+meta.PersonaID+".json")
		if err := pkg.CopyFile(srcH, dstH); err != nil {
			return nil, err
		}
		rep.Placed = append(rep.Placed, "history")
	}

	return rep, nil
}
