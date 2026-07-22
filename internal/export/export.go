// Package export builds persona (Tier 1/2) and workspace (Tier 3) packages (§8).
//
// Common tail (finalize): secret-scan block (S1) → SHA-256 manifest (S8) → tar →
// optional age encryption (§7) → optional minisign signature.
package export

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"aead.dev/minisign"

	"github.com/bubomortis/clairvoyance-persona-sync/internal/clv"
	"github.com/bubomortis/clairvoyance-persona-sync/internal/cryptobox"
	"github.com/bubomortis/clairvoyance-persona-sync/internal/manifest"
	"github.com/bubomortis/clairvoyance-persona-sync/internal/pkg"
	"github.com/bubomortis/clairvoyance-persona-sync/internal/scan"
)

// Options controls tier, encryption, signing, and the secret-scan gate.
type Options struct {
	Tier               int
	Passphrase         string
	Recipient          string
	SignKey            *minisign.PrivateKey
	AllowSecrets       bool
	AllowOperatorSync  bool // override the S15 export guard (§19.3)
	IncludeAgentMemory bool // D19: also bundle the rich .claude/projects/<munge>/memory store
}

// Result reports the produced artifacts and any secret findings.
type Result struct {
	PackagePath string
	SigPath     string
	SecretHits  []scan.Match
	SecretSkips []scan.Skip
	Encrypted   bool
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if string(b) == "null" {
		b = []byte("[]")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func newMeta(tier int) pkg.Meta {
	return pkg.Meta{SchemaVersion: 1, Tier: tier, CreatedAt: time.Now().UTC().Format(time.RFC3339), SourceOS: runtime.GOOS}
}

// stagePersona lays a persona's Tier-1(+2) contents into stageRoot under subdir
// ("" for a standalone persona, or "roster/<lname>" inside a workspace package).
//
// includeHistory carries the raw agent-history transcript. It is DELIBERATELY OFF for
// a standalone Tier-1 persona (D19): a lone transcript file is the trap that lost the
// test persona's memory — it is clobbered by the destination's first fresh chat and is
// unusable without a resumable session anyway. The transcript travels only at Tier 2
// (as part of Universal Resume) and inside a whole-workspace package (Tier 3, scope
// unchanged). Tier-1 continuity rides on the curated .clairvoyance/staff memory instead.
func stagePersona(in *clv.Instance, p *clv.Persona, stageRoot, subdir string, includeMemory, includeHistory, tier2 bool) error {
	base := filepath.Join(stageRoot, subdir)
	if err := os.WriteFile(mustDir(filepath.Join(base, "definition", "staff-entry.json")), p.Entry, 0o644); err != nil {
		return err
	}
	if tp, ok := in.TemplatePath(p.Template); ok {
		if err := pkg.CopyFile(tp, filepath.Join(base, "definition", "personas", p.Template)); err != nil {
			return err
		}
	}
	if includeMemory {
		for _, m := range p.Memory {
			dst := filepath.Join(base, "memory", m.Scope, p.Lname)
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return err
			}
			if err := os.CopyFS(dst, os.DirFS(m.Dir)); err != nil {
				return err
			}
		}
	}
	if includeHistory && p.History != "" {
		if err := pkg.CopyFile(p.History, filepath.Join(base, "history", p.ID+".json")); err != nil {
			return err
		}
	}
	if tier2 {
		r := in.LoadResume(p)
		if len(r.Sessions) > 0 {
			var recs []map[string]any
			for _, s := range r.Sessions {
				s.Rec["workspacePath"] = clv.WSToken(s.Scope)
				s.Rec["workspaceId"] = clv.WSIDToken(s.Scope)
				recs = append(recs, s.Rec)
			}
			if err := writeJSON(filepath.Join(base, "resume", "records.json"), recs); err != nil {
				return err
			}
			if err := writeJSON(filepath.Join(base, "resume", "session-summaries.json"), r.Summaries); err != nil {
				return err
			}
			if err := writeJSON(filepath.Join(base, "resume", "resume-exclusions.json"), r.Exclusions); err != nil {
				return err
			}
		}
	}
	return nil
}

func mustDir(file string) string {
	_ = os.MkdirAll(filepath.Dir(file), 0o755)
	return file
}

// stageAgentMemory copies the persona's rich .claude/projects/<munge>/memory store into
// <stageRoot>/<subdir>/agent-memory. The munge is derived from the persona's shell.cwd
// (falling back to the home data dir). Returns false with no error when the store does not
// exist on this machine (nothing to travel — not a failure).
func stageAgentMemory(in *clv.Instance, p *clv.Persona, stageRoot, subdir string) (bool, error) {
	cwd := clv.EntryShellCwd(p.Entry)
	if cwd == "" {
		cwd = in.DataDir
	}
	dir, ok := clv.AgentMemoryDir(in.AgentHome, cwd)
	if !ok {
		return false, nil // degenerate cwd → can't locate a store (AM-1 guard)
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return false, nil
	}
	dst := filepath.Join(stageRoot, subdir, "agent-memory")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return false, err
	}
	return true, os.CopyFS(dst, os.DirFS(dir))
}

// Persona exports one persona (Tier 1, or Tier 2 with resume artifacts).
func Persona(in *clv.Instance, p *clv.Persona, outPath string, opts Options) (*Result, error) {
	if p.IsOperator() && !opts.AllowOperatorSync {
		return nil, fmt.Errorf("%q is the Sync Operator — machine-local infrastructure, not a portable persona (§19.3); re-run with AllowOperatorSync only if you deliberately intend to move it", p.Name)
	}
	stage, err := os.MkdirTemp("", "clvsync-export-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)

	// Tier 1: definition + template + curated memory only (history dropped, D19).
	// Tier 2: also the transcript + resume records (Universal Resume).
	if err := stagePersona(in, p, stage, "", true, opts.Tier >= 2, opts.Tier >= 2); err != nil {
		return nil, err
	}
	meta := newMeta(1)
	meta.PersonaID, meta.PersonaName, meta.Template = p.ID, p.Name, p.Template
	for _, m := range p.Memory {
		meta.Scopes = append(meta.Scopes, m.Scope)
	}
	// D19 --include-agent-memory: bundle the rich .claude/projects working store (orthogonal
	// to tier). Staged under the stage root, so the finalize secret scan (S1) covers it.
	if opts.IncludeAgentMemory {
		staged, err := stageAgentMemory(in, p, stage, "")
		if err != nil {
			return nil, err
		}
		meta.AgentMemory = staged
	}
	if opts.Tier >= 2 {
		if _, err := os.Stat(filepath.Join(stage, "resume")); err == nil {
			meta.Tier = 2
		}
	}
	return finalize(stage, outPath, meta, opts)
}

// finalize runs the shared packaging tail for any staged tree.
func finalize(stage, outPath string, meta pkg.Meta, opts Options) (*Result, error) {
	mb, _ := json.MarshalIndent(meta, "", "  ")
	if err := os.WriteFile(filepath.Join(stage, "meta.json"), mb, 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(stage, "IMPORT.md"), []byte(importDoc(meta)), 0o644); err != nil {
		return nil, err
	}

	// secret scan gate (S1)
	sc, _ := scan.New(nil)
	var hits []scan.Match
	var skips []scan.Skip
	err := filepath.WalkDir(stage, func(pth string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		m, sk, e := sc.File(pth)
		hits = append(hits, m...)
		if sk != nil {
			skips = append(skips, *sk)
		}
		return e
	})
	if err != nil {
		return nil, err
	}
	if len(hits) > 0 && !opts.AllowSecrets {
		return &Result{SecretHits: hits, SecretSkips: skips}, fmt.Errorf("secret-scan blocked export: %d match(es); use AllowSecrets to override", len(hits))
	}

	// manifest (S8) — after Build so it is not self-referential
	mani, err := manifest.Build(stage)
	if err != nil {
		return nil, err
	}
	mjson, _ := mani.JSON()
	if err := os.WriteFile(filepath.Join(stage, "manifest.json"), mjson, 0o644); err != nil {
		return nil, err
	}

	var tarbuf bytes.Buffer
	if err := pkg.WriteTar(stage, &tarbuf); err != nil {
		return nil, err
	}

	out, err := os.Create(outPath)
	if err != nil {
		return nil, err
	}
	res := &Result{PackagePath: outPath, SecretHits: hits, SecretSkips: skips}
	switch {
	case opts.Passphrase != "":
		err = cryptobox.EncryptPassphrase(out, bytes.NewReader(tarbuf.Bytes()), opts.Passphrase)
		res.Encrypted = true
	case opts.Recipient != "":
		err = cryptobox.EncryptRecipient(out, bytes.NewReader(tarbuf.Bytes()), opts.Recipient)
		res.Encrypted = true
	default:
		_, err = out.Write(tarbuf.Bytes())
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return nil, err
	}

	if opts.SignKey != nil {
		data, err := os.ReadFile(outPath)
		if err != nil {
			return nil, err
		}
		sig := cryptobox.Sign(*opts.SignKey, data)
		res.SigPath = outPath + ".minisig"
		if err := os.WriteFile(res.SigPath, sig, 0o644); err != nil {
			return nil, err
		}
	}
	return res, nil
}
