// Package export builds a Tier-1 persona package (§8).
//
// Flow: gather definition + memory + history into a staging tree → secret-scan
// (block by default, S1) → SHA-256 manifest (S8) → tar → optional age encryption
// (§7) → optional minisign signature.
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

// Options controls encryption, signing, and the secret-scan gate.
type Options struct {
	Tier         int                  // 1 = portable; 2 = + Universal Resume artifacts
	Passphrase   string               // age passphrase mode
	Recipient    string               // age X25519 recipient ("age1...")
	SignKey      *minisign.PrivateKey // minisign detached signature
	AllowSecrets bool                 // override the secret-scan block (S1)
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
	return os.WriteFile(path, b, 0o644)
}

// Persona exports one persona to outPath (Tier 1, or Tier 2 with resume artifacts).
func Persona(in *clv.Instance, p *clv.Persona, outPath string, opts Options) (*Result, error) {
	stage, err := os.MkdirTemp("", "clvsync-export-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)

	// definition
	defDir := filepath.Join(stage, "definition")
	if err := os.MkdirAll(defDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(defDir, "staff-entry.json"), p.Entry, 0o644); err != nil {
		return nil, err
	}
	meta := pkg.Meta{
		SchemaVersion: 1, Tier: 1, PersonaID: p.ID, PersonaName: p.Name,
		Template: p.Template, CreatedAt: time.Now().UTC().Format(time.RFC3339), SourceOS: runtime.GOOS,
	}
	if tp, ok := in.TemplatePath(p.Template); ok {
		if err := pkg.CopyFile(tp, filepath.Join(defDir, "personas", p.Template)); err != nil {
			return nil, err
		}
	}

	// memory (keyed by scope name, not source path — this is the tokenization)
	for _, m := range p.Memory {
		dst := filepath.Join(stage, "memory", m.Scope, p.Lname)
		if err := os.MkdirAll(dst, 0o755); err != nil {
			return nil, err
		}
		if err := os.CopyFS(dst, os.DirFS(m.Dir)); err != nil {
			return nil, err
		}
		meta.Scopes = append(meta.Scopes, m.Scope)
	}

	// history
	if p.History != "" {
		if err := pkg.CopyFile(p.History, filepath.Join(stage, "history", "staff-"+p.ID+".json")); err != nil {
			return nil, err
		}
	}

	// Tier 2: Universal Resume artifacts (records tokenized by workspace scope;
	// provider/model kept — Universal Resume resumes across providers).
	if opts.Tier >= 2 {
		r := in.LoadResume(p)
		if len(r.Sessions) > 0 {
			rdir := filepath.Join(stage, "resume")
			if err := os.MkdirAll(rdir, 0o755); err != nil {
				return nil, err
			}
			var recs []map[string]any
			for _, s := range r.Sessions {
				s.Rec["workspacePath"] = clv.WSToken(s.Scope)
				s.Rec["workspaceId"] = clv.WSIDToken(s.Scope)
				recs = append(recs, s.Rec)
			}
			if err := writeJSON(filepath.Join(rdir, "records.json"), recs); err != nil {
				return nil, err
			}
			if err := writeJSON(filepath.Join(rdir, "session-summaries.json"), r.Summaries); err != nil {
				return nil, err
			}
			if err := writeJSON(filepath.Join(rdir, "resume-exclusions.json"), r.Exclusions); err != nil {
				return nil, err
			}
			meta.Tier = 2
		}
	}

	// meta (included in the manifest)
	mb, _ := json.MarshalIndent(meta, "", "  ")
	if err := os.WriteFile(filepath.Join(stage, "meta.json"), mb, 0o644); err != nil {
		return nil, err
	}

	// secret scan gate (S1) — before packaging
	sc, _ := scan.New(nil)
	var hits []scan.Match
	err = filepath.WalkDir(stage, func(pth string, d fs.DirEntry, err error) error {
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

	// manifest (S8) — written after Build so it is not self-referential
	mani, err := manifest.Build(stage)
	if err != nil {
		return nil, err
	}
	mjson, _ := mani.JSON()
	if err := os.WriteFile(filepath.Join(stage, "manifest.json"), mjson, 0o644); err != nil {
		return nil, err
	}

	// tar
	var tarbuf bytes.Buffer
	if err := pkg.WriteTar(stage, &tarbuf); err != nil {
		return nil, err
	}

	// write (encrypted or plain)
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

	// sign (S8/authenticity)
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
