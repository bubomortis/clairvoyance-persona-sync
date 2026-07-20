// Package importer applies persona (Tier 1/2) and workspace (Tier 3) packages (§9).
//
// Order: verify signature (S8) → decrypt (§7) → safe-extract (S3) → verify manifest
// (S8) → dispatch by tier → collision check (S6) → non-destructive merge with backup (S7).
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

// Options controls decryption, verification, collision handling, and workspace prep.
type Options struct {
	Passphrase    string
	Identity      string // age X25519 secret key
	Sig           []byte
	VerifyKey     *minisign.PublicKey
	Force         bool
	WorkspacePath string // Tier 3: where to create the target workspace if absent
}

// Report summarizes an import.
type Report struct {
	PersonaID     string
	PersonaName   string // or workspace name for Tier 3
	Tier          int
	Placed        []string
	SkippedScopes []string
	BackedUp      []string
	ReviewNote    string
}

// Apply imports a package, dispatching on its tier.
func Apply(pkgPath string, in *clv.Instance, opts Options) (*Report, error) {
	stage, meta, err := openPackage(pkgPath, opts)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	if meta.Tier >= 3 {
		return applyWorkspace(stage, meta, in, opts)
	}
	return applyPersona(stage, meta, in, opts)
}

// openPackage verifies, decrypts, safely extracts, and verifies the manifest,
// returning the staging dir and parsed meta.
func openPackage(pkgPath string, opts Options) (string, pkg.Meta, error) {
	var meta pkg.Meta
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return "", meta, err
	}
	if opts.VerifyKey != nil {
		if opts.Sig == nil {
			return "", meta, fmt.Errorf("verify key provided but no signature")
		}
		if !cryptobox.Verify(*opts.VerifyKey, data, opts.Sig) {
			return "", meta, fmt.Errorf("signature verification failed")
		}
	}
	var src io.Reader = bytes.NewReader(data)
	switch {
	case opts.Passphrase != "":
		r, err := cryptobox.DecryptPassphrase(bytes.NewReader(data), opts.Passphrase)
		if err != nil {
			return "", meta, fmt.Errorf("decrypt: %w", err)
		}
		src = r
	case opts.Identity != "":
		r, err := cryptobox.DecryptIdentity(bytes.NewReader(data), opts.Identity)
		if err != nil {
			return "", meta, fmt.Errorf("decrypt: %w", err)
		}
		src = r
	}
	stage, err := os.MkdirTemp("", "clvsync-import-*")
	if err != nil {
		return "", meta, err
	}
	if err := pkg.ExtractTar(src, stage); err != nil {
		os.RemoveAll(stage)
		return "", meta, fmt.Errorf("extract: %w", err)
	}
	mb, err := os.ReadFile(filepath.Join(stage, "manifest.json"))
	if err != nil {
		os.RemoveAll(stage)
		return "", meta, err
	}
	mani, err := manifest.Parse(mb)
	if err != nil {
		os.RemoveAll(stage)
		return "", meta, err
	}
	if err := mani.Verify(stage); err != nil {
		os.RemoveAll(stage)
		return "", meta, fmt.Errorf("integrity check failed: %w", err)
	}
	mtb, err := os.ReadFile(filepath.Join(stage, "meta.json"))
	if err != nil {
		os.RemoveAll(stage)
		return "", meta, err
	}
	if err := json.Unmarshal(mtb, &meta); err != nil {
		os.RemoveAll(stage)
		return "", meta, err
	}
	return stage, meta, nil
}

func applyPersona(stage string, meta pkg.Meta, in *clv.Instance, opts Options) (*Report, error) {
	rep := &Report{PersonaID: meta.PersonaID, PersonaName: meta.PersonaName, Tier: meta.Tier,
		ReviewNote: "imported persona definition + memory are externally sourced; review before relying on this Staff member"}
	lname := clv.Slug(meta.PersonaName)

	if existing, _ := in.FindPersona(meta.PersonaID); existing != nil && !opts.Force {
		return nil, fmt.Errorf("collision: persona %q (%s) already on target; use Force to overwrite", existing.Name, existing.ID)
	}

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

	placeTemplate(stage, "", meta.Template, in, rep)
	placeMemory(stage, lname, in, rep, opts.Force)

	srcH := filepath.Join(stage, "history", "staff-"+meta.PersonaID+".json")
	if _, err := os.Stat(srcH); err == nil {
		if err := pkg.CopyFile(srcH, filepath.Join(in.DataDir, "profiles", prof, "agent-history", "staff-"+meta.PersonaID+".json")); err != nil {
			return nil, err
		}
		rep.Placed = append(rep.Placed, "history")
	}

	mergeResume(stage, "", prof, in, rep)
	return rep, nil
}

func applyWorkspace(stage string, meta pkg.Meta, in *clv.Instance, opts Options) (*Report, error) {
	rep := &Report{Tier: meta.Tier, PersonaName: meta.WorkspaceName,
		ReviewNote: "imported workspace + roster are externally sourced; review before relying on them"}

	ws, created, err := in.EnsureWorkspace(meta.WorkspaceName, opts.WorkspacePath)
	if err != nil {
		return nil, err
	}
	if created {
		rep.Placed = append(rep.Placed, "workspace-registered:"+ws.Name)
	}

	if wsSrc := filepath.Join(stage, "workspace"); dirExists(wsSrc) {
		if err := pkg.CopyTree(wsSrc, ws.Path, nil); err != nil {
			return nil, err
		}
		rep.Placed = append(rep.Placed, "workspace-content")
	}

	prof, err := in.DefaultProfile()
	if err != nil {
		return nil, err
	}
	for _, r := range meta.Roster {
		rdir := filepath.Join(stage, "roster", r.Lname)
		entry, err := os.ReadFile(filepath.Join(rdir, "definition", "staff-entry.json"))
		if err != nil {
			continue
		}
		if existing, _ := in.FindPersona(r.ID); existing != nil && !opts.Force {
			rep.SkippedScopes = append(rep.SkippedScopes, "persona-exists:"+r.Name)
			continue
		}
		if err := in.SpliceStaffEntry(prof, entry, r.ID, opts.Force); err != nil {
			return nil, err
		}
		placeTemplate(rdir, "", r.Template, in, rep)
		srcH := filepath.Join(rdir, "history", "staff-"+r.ID+".json")
		if _, err := os.Stat(srcH); err == nil {
			_ = pkg.CopyFile(srcH, filepath.Join(in.DataDir, "profiles", prof, "agent-history", "staff-"+r.ID+".json"))
		}
		rep.Placed = append(rep.Placed, "persona:"+r.Name)
	}
	return rep, nil
}

// --- shared helpers ---

func placeTemplate(root, subdir, template string, in *clv.Instance, rep *Report) {
	if template == "" {
		return
	}
	srcT := filepath.Join(root, subdir, "definition", "personas", template)
	if _, err := os.Stat(srcT); err != nil {
		return
	}
	dstT := filepath.Join(in.DataDir, "neurons", "personas", template)
	if _, err := os.Stat(dstT); err == nil {
		return // never overwrite an existing template
	}
	if pkg.CopyFile(srcT, dstT) == nil {
		rep.Placed = append(rep.Placed, "template:"+template)
	}
}

func placeMemory(stage, lname string, in *clv.Instance, rep *Report, force bool) {
	memRoot := filepath.Join(stage, "memory")
	ents, err := os.ReadDir(memRoot)
	if err != nil {
		return
	}
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
		if force {
			_ = os.RemoveAll(dstDir)
		}
		_ = os.MkdirAll(filepath.Dir(dstDir), 0o755)
		if os.CopyFS(dstDir, os.DirFS(srcDir)) == nil {
			rep.Placed = append(rep.Placed, "memory/"+scope)
		}
	}
}

func mergeResume(stage, subdir, prof string, in *clv.Instance, rep *Report) {
	b, err := os.ReadFile(filepath.Join(stage, subdir, "resume", "records.json"))
	if err != nil {
		return
	}
	var recs []map[string]any
	if json.Unmarshal(b, &recs) == nil {
		var applied []map[string]any
		for _, rec := range recs {
			scope, ok := clv.ParseScopeToken(fmt.Sprint(rec["workspacePath"]))
			if !ok {
				scope = "home"
			}
			if scope == "home" {
				rec["workspacePath"] = in.DataDir
				rec["workspaceId"] = in.HomeWorkspaceID()
				applied = append(applied, rec)
			} else if ws, ok := in.WorkspaceByName(scope); ok {
				rec["workspacePath"] = ws.Path
				rec["workspaceId"] = ws.ID
				applied = append(applied, rec)
			} else {
				rep.SkippedScopes = append(rep.SkippedScopes, "session:"+scope)
			}
		}
		if len(applied) > 0 {
			if in.MergeSessions(prof, applied) == nil {
				rep.Placed = append(rep.Placed, "resume/sessions")
			}
		}
	}
	if es := readEntries(filepath.Join(stage, subdir, "resume", "session-summaries.json")); len(es) > 0 {
		if in.MergeResumeEntries(prof, "session-summaries.json", es) == nil {
			rep.Placed = append(rep.Placed, "resume/summaries")
		}
	}
	if es := readEntries(filepath.Join(stage, subdir, "resume", "resume-exclusions.json")); len(es) > 0 {
		if in.MergeResumeEntries(prof, "resume-exclusions.json", es) == nil {
			rep.Placed = append(rep.Placed, "resume/exclusions")
		}
	}
}

func readEntries(path string) []json.RawMessage {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var arr []json.RawMessage
	if json.Unmarshal(b, &arr) != nil {
		return nil
	}
	return arr
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}
