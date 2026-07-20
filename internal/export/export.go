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
	Tier         int
	Passphrase   string
	Recipient    string
	SignKey      *minisign.PrivateKey
	AllowSecrets bool
}

// Result reports the produced artifacts and any secret findings.
type Result struct {
	PackagePath string
	SigPath     string
	SecretHits  []scan.Match
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
func stagePersona(in *clv.Instance, p *clv.Persona, stageRoot, subdir string, includeMemory, tier2 bool) error {
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
	if p.History != "" {
		if err := pkg.CopyFile(p.History, filepath.Join(base, "history", "staff-"+p.ID+".json")); err != nil {
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

// Persona exports one persona (Tier 1, or Tier 2 with resume artifacts).
func Persona(in *clv.Instance, p *clv.Persona, outPath string, opts Options) (*Result, error) {
	stage, err := os.MkdirTemp("", "clvsync-export-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)

	if err := stagePersona(in, p, stage, "", true, opts.Tier >= 2); err != nil {
		return nil, err
	}
	meta := newMeta(1)
	meta.PersonaID, meta.PersonaName, meta.Template = p.ID, p.Name, p.Template
	for _, m := range p.Memory {
		meta.Scopes = append(meta.Scopes, m.Scope)
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

	// secret scan gate (S1)
	sc, _ := scan.New(nil)
	var hits []scan.Match
	err := filepath.WalkDir(stage, func(pth string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		m, e := sc.File(pth)
		hits = append(hits, m...)
		return e
	})
	if err != nil {
		return nil, err
	}
	if len(hits) > 0 && !opts.AllowSecrets {
		return &Result{SecretHits: hits}, fmt.Errorf("secret-scan blocked export: %d match(es); use AllowSecrets to override", len(hits))
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
	res := &Result{PackagePath: outPath, SecretHits: hits}
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
