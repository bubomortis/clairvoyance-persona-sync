// Package importer applies persona (Tier 1/2) and workspace (Tier 3/4) packages (§9).
//
// Order: operator guard (S15) → verify signature (S8) → decrypt (§7) → safe-extract
// (S3) → verify manifest (S8) → dispatch by tier → collision/mode resolution (S6) →
// non-destructive component merge with backup (S7, §17). A --dry-run pass computes
// the same plan without writing. On success the finisher writes an import receipt
// (§19.2) for the restart-time verify-import reconciliation.
package importer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"aead.dev/minisign"

	"github.com/bubomortis/clairvoyance-persona-sync/internal/clv"
	"github.com/bubomortis/clairvoyance-persona-sync/internal/cryptobox"
	"github.com/bubomortis/clairvoyance-persona-sync/internal/manifest"
	"github.com/bubomortis/clairvoyance-persona-sync/internal/pkg"
)

// Options controls decryption, verification, merge mode, and workspace prep.
type Options struct {
	Passphrase        string
	Identity          string // age X25519 secret key
	Sig               []byte
	VerifyKey         *minisign.PublicKey
	Mode              clv.Mode // sync (default) | overwrite | skip (§17.3)
	Force             bool     // back-compat: equivalent to Mode=overwrite
	DryRun            bool     // compute the plan, write nothing (§17.3)
	AllowOperatorSync bool     // override the S15 self-sync guard (§19.3)
	WorkspacePath     string   // Tier 3: where to create the target workspace if absent
	ReceiptPath       string   // where the finisher writes import-receipt.json (§19.2)
	NoNameReserve     bool     // opt out of reserving the persona's name in staff-names.json
}

// mode resolves the effective merge mode (Force wins for back-compat).
func (o Options) mode() clv.Mode {
	if o.Force {
		return clv.ModeOverwrite
	}
	if o.Mode == "" {
		return clv.ModeSync
	}
	return o.Mode
}

// Report summarizes an import (or a dry-run plan).
type Report struct {
	PersonaID        string
	PersonaName      string // or workspace name for Tier 3
	WorkspaceName    string
	WorkspaceID      string
	Tier             int
	Mode             string
	DryRun           bool
	Placed           []string // components actually written (empty on dry-run)
	Plan             []string // human-readable planned actions (both modes)
	Warnings         []string
	SkippedScopes    []string
	BackedUp         []string
	SessionIDs       []string
	PortableUpdated  []string
	MachineLocalKept []string
	MemoryPlaced     bool // curated and/or agent memory was placed → surfaces only on next session start (Q1/§21.5)
	ReviewNote       string
	ReceiptPath      string
	// D17 Model 2c travel pairing: the sender's public key carried in the package
	// (public, not a secret). The CLI trust-on-first-use records it after a
	// successful import. Empty unless the sender used identity + travel pairing.
	SenderName       string
	SenderPublicKey  string

	files []string // absolute placed paths to hash into the receipt
}

func (r *Report) plan(format string, a ...any) { r.Plan = append(r.Plan, fmt.Sprintf(format, a...)) }
func (r *Report) warn(format string, a ...any) {
	r.Warnings = append(r.Warnings, fmt.Sprintf(format, a...))
}
func (r *Report) placed(what string, files ...string) {
	r.Placed = append(r.Placed, what)
	r.files = append(r.files, files...)
}

// Apply imports a package, dispatching on its tier.
func Apply(pkgPath string, in *clv.Instance, opts Options) (*Report, error) {
	stage, meta, err := openPackage(pkgPath, opts)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)

	if err := operatorGuard(meta, in, opts); err != nil {
		return nil, err
	}

	var rep *Report
	switch {
	case meta.Tier == 4:
		rep, err = applyWorkspaceHeavy(stage, meta, in, opts)
	case meta.Tier == 3:
		rep, err = applyWorkspace(stage, meta, in, opts)
	default:
		rep, err = applyPersona(stage, meta, in, opts)
	}
	if err != nil {
		return nil, err
	}
	rep.Mode = string(opts.mode())
	rep.DryRun = opts.DryRun
	rep.SenderName = meta.SenderName
	rep.SenderPublicKey = meta.SenderPublicKey
	if !opts.DryRun {
		if err := writeReceipt(rep, in, opts); err != nil {
			rep.warn("could not write import receipt: %v", err)
		}
	}
	return rep, nil
}

// operatorGuard enforces S15: don't sync the Sync Operator (§19.3). A self-overwrite
// (incoming id already an operator on this machine) is a HARD block regardless of the
// override; any other operator package needs the explicit --allow-operator-sync.
func operatorGuard(meta pkg.Meta, in *clv.Instance, opts Options) error {
	incomingOperator := clv.IsOperatorMarker(meta.Template, meta.PersonaName)
	var rosterOp *pkg.RosterEntry
	for i := range meta.Roster {
		if clv.IsOperatorMarker(meta.Roster[i].Template, meta.Roster[i].Name) {
			rosterOp = &meta.Roster[i]
			break
		}
	}
	if !incomingOperator && rosterOp == nil {
		return nil
	}
	ops := in.OperatorIDs()
	if incomingOperator && ops[meta.PersonaID] {
		return fmt.Errorf("refusing to import the Sync Operator over the operator already on this machine (id %s) — this is never allowed; run the import from a different persona if you truly mean to replace it", meta.PersonaID)
	}
	if rosterOp != nil && ops[rosterOp.ID] {
		return fmt.Errorf("workspace package carries a Sync Operator (%q) whose id matches this machine's operator — refusing to overwrite the live operator", rosterOp.Name)
	}
	if !opts.AllowOperatorSync {
		return fmt.Errorf("this package contains the Sync Operator persona, which is machine-local infrastructure, not portable content (§19.3) — re-run with AllowOperatorSync only if you deliberately intend to move it")
	}
	return nil
}

// Inspect verifies the signature, decrypts, and verifies the manifest, returning
// the package meta without applying anything (backs the `verify` command).
func Inspect(pkgPath string, opts Options) (*pkg.Meta, error) {
	stage, meta, err := openPackage(pkgPath, opts)
	if err != nil {
		return nil, err
	}
	os.RemoveAll(stage)
	return &meta, nil
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
	mode := opts.mode()
	rep := &Report{PersonaID: meta.PersonaID, PersonaName: meta.PersonaName, Tier: meta.Tier,
		ReviewNote: "imported persona definition + memory are externally sourced; review before relying on this Staff member"}
	lname := clv.Slug(meta.PersonaName)

	existing, _ := in.FindPersona(meta.PersonaID)
	if existing != nil && mode == clv.ModeSkip {
		rep.plan("persona %s exists — skip mode, nothing changed", meta.PersonaName)
		return rep, nil
	}

	entry, err := os.ReadFile(filepath.Join(stage, "definition", "staff-entry.json"))
	if err != nil {
		return nil, err
	}
	// S4: surface unrecognized definition fields for review. The definition is inert
	// data clvsync never executes, but it becomes agent-loaded config on the target.
	if unknown := clv.UnknownDefinitionFields(entry); len(unknown) > 0 {
		rep.warn("imported definition carries unrecognized field(s) %v — review before relying on this Staff member (they are inert to clvsync but loaded by Clairvoyance)", unknown)
	}
	prof, err := in.DefaultProfile()
	if err != nil {
		return nil, err
	}

	// v0.1.1: repoint machine-local paths (shell.cwd) that don't exist on THIS machine,
	// so a freshly created/overwritten persona starts without a manual cwd fix.
	entry, repoints := repointDeadPaths(entry, in.DataDir)

	action, changed, preserved, trustNote, err := in.MergeStaffEntry(prof, entry, meta.PersonaID, mode, opts.DryRun)
	if err != nil {
		return nil, err
	}
	rep.PortableUpdated, rep.MachineLocalKept = changed, preserved
	if trustNote != "" {
		rep.warn("trust: %s", trustNote) // D18: permissionMode is a local grant
	}
	switch action {
	case "created", "overwritten":
		if action == "created" {
			rep.plan("definition: create new persona %s", meta.PersonaName)
		} else {
			rep.plan("definition: overwrite whole entry")
		}
		// These actions take the incoming machine-local fields, so surface repoints + advisories.
		for _, r := range repoints {
			rep.plan("definition: %s", r)
		}
		for _, w := range machineLocalAdvisories(entry) {
			rep.warn("%s", w)
		}
	case "merged":
		// Sync preserves the destination's machine-local runtime, so incoming paths are moot.
		rep.plan("definition: merge — portable updated %v, machine-local preserved %v", orNone(changed), orNone(preserved))
	}
	if !opts.DryRun {
		rep.BackedUp = append(rep.BackedUp, "profiles/"+prof+"/staff.json")
		rep.placed("definition", filepath.Join(in.DataDir, "profiles", prof, "staff.json"))
	}
	reserveStaffName(in, meta.PersonaName, rep, opts)

	placeTemplate(stage, "", meta.Template, in, rep, opts)
	mergeMemory(stage, lname, in, rep, opts)
	mergeHistory(stage, "", meta.PersonaID, prof, in, rep, opts)
	mergeResume(stage, "", prof, in, rep, opts)
	mergeAgentMemory(stage, "", in, rep, opts, agentMemCwd(in, entry, meta.PersonaID, opts.DryRun))
	return rep, nil
}

// reserveStaffName reserves the imported persona's display name in the app's
// staff-names.json — a confirmed-optional nicety so the in-app Create Staff modal knows the
// name is taken (and won't suggest it for a brand-new staff member). It is append-only and
// idempotent; the registry is NOT load-bearing for discovery, so any failure is non-fatal
// and never blocks the import. Skipped on dry-run and when the caller opts out.
func reserveStaffName(in *clv.Instance, name string, rep *Report, opts Options) {
	if opts.DryRun {
		rep.plan("staff-names: reserve %q so the Create Staff modal shows it as taken (optional)", name)
		return
	}
	if opts.NoNameReserve {
		return
	}
	reserved, err := clv.ReserveStaffName(in.DataDir, name, time.Now().UnixMilli())
	if err != nil {
		rep.warn("staff-names: could not reserve %q: %v (non-fatal; the name just won't show as taken in the Create Staff modal)", name, err)
		return
	}
	if reserved {
		rep.Placed = append(rep.Placed, "name-reserved:"+name)
	}
}

// agentMemCwd resolves the cwd whose munge the rich agent-memory should land under on THIS
// machine. After a real import the authoritative value is the persona's on-disk shell.cwd
// (covers sync-preserve, create-repoint, and overwrite); for a dry run we preview against
// the repointed incoming cwd.
func agentMemCwd(in *clv.Instance, entry json.RawMessage, id string, dryRun bool) string {
	cwd := clv.EntryShellCwd(entry)
	if !dryRun {
		if fp, err := in.FindPersona(id); err == nil {
			if c := clv.EntryShellCwd(fp.Entry); c != "" {
				cwd = c
			}
		}
	}
	return cwd
}

// mergeAgentMemory places a package's bundled rich agent-memory (.claude/projects/<munge>/
// memory) under THIS machine's munge for the resolved cwd (D19). Non-destructive: a
// differing existing file is backed up (.clvsync-bak) before the incoming copy replaces it;
// identical files are skipped. Absent bundle → no-op.
func mergeAgentMemory(stage, subdir string, in *clv.Instance, rep *Report, opts Options, cwd string) {
	src := filepath.Join(stage, subdir, "agent-memory")
	if info, err := os.Stat(src); err != nil || !info.IsDir() {
		return
	}
	if cwd == "" {
		cwd = in.DataDir
	}
	dstDir, ok := clv.AgentMemoryDir(in.AgentHome, cwd)
	if !ok {
		// AM-1: a degenerate cwd ("."/"..") would collapse the target out of
		// .claude/projects — refuse to place rather than write outside the sandbox.
		rep.warn("agent-memory: refusing to place — persona cwd %q resolves outside the agent-memory sandbox", cwd)
		return
	}
	rep.plan("agent-memory: place rich working memory → %s", dstDir)
	rep.MemoryPlaced = true // Q1: surfaces on the persona's next session start
	if opts.DryRun {
		return
	}
	backedUp := false
	if err := filepath.WalkDir(src, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil || d.IsDir() {
			return werr
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dstDir, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if old, e := os.ReadFile(target); e == nil {
			nb, _ := os.ReadFile(p)
			if string(old) == string(nb) {
				return nil // identical: skip
			}
			_ = os.WriteFile(target+".clvsync-bak", old, 0o644) // S7
			backedUp = true
		}
		return pkg.CopyFile(p, target)
	}); err != nil {
		// AM-2: don't silently report a partial placement as done.
		rep.warn("agent-memory: partial placement into %s — %v", dstDir, err)
		return
	}
	rep.placed("agent-memory", dstDir)
	if backedUp {
		rep.BackedUp = append(rep.BackedUp, "agent-memory (.clvsync-bak on overwrite)")
	}
}

func applyWorkspace(stage string, meta pkg.Meta, in *clv.Instance, opts Options) (*Report, error) {
	mode := opts.mode()
	rep := &Report{Tier: meta.Tier, PersonaName: meta.WorkspaceName, WorkspaceName: meta.WorkspaceName,
		ReviewNote: "imported workspace + roster are externally sourced; review before relying on them"}

	ws, created, err := in.EnsureWorkspaceMaybe(meta.WorkspaceName, opts.WorkspacePath, opts.DryRun)
	if err != nil {
		return nil, err
	}
	rep.WorkspaceID = ws.ID
	if created {
		rep.plan("workspace: register %q at %s", ws.Name, ws.Path)
		if !opts.DryRun {
			rep.placed("workspace-registered:" + ws.Name)
		}
	} else {
		rep.plan("workspace: %q already registered", ws.Name)
	}

	if wsSrc := filepath.Join(stage, "workspace"); dirExists(wsSrc) {
		rep.plan("workspace: copy content tree")
		rep.MemoryPlaced = true // the tree carries the roster's curated .Clairvoyance/staff memory (Q1)
		if !opts.DryRun {
			if err := pkg.CopyTree(wsSrc, ws.Path, nil); err != nil {
				return nil, err
			}
			rep.placed("workspace-content")
		}
	}

	prof, err := in.DefaultProfile()
	if err != nil {
		return nil, err
	}
	ops := in.OperatorIDs()
	for _, r := range meta.Roster {
		if clv.IsOperatorMarker(r.Template, r.Name) && !opts.AllowOperatorSync {
			rep.SkippedScopes = append(rep.SkippedScopes, "operator-guarded:"+r.Name)
			rep.plan("roster: SKIP Sync Operator %q (S15 guard)", r.Name)
			continue
		}
		if ops[r.ID] {
			rep.SkippedScopes = append(rep.SkippedScopes, "operator-live:"+r.Name)
			continue
		}
		rdir := filepath.Join(stage, "roster", r.Lname)
		entry, err := os.ReadFile(filepath.Join(rdir, "definition", "staff-entry.json"))
		if err != nil {
			continue
		}
		if existing, _ := in.FindPersona(r.ID); existing != nil && mode == clv.ModeSkip {
			rep.SkippedScopes = append(rep.SkippedScopes, "persona-exists:"+r.Name)
			continue
		}
		// S4 (SU5): surface unrecognized definition fields on roster members too, not just
		// the Tier-1/2 lone persona — a workspace import loads every roster definition.
		if unknown := clv.UnknownDefinitionFields(entry); len(unknown) > 0 {
			rep.warn("roster %s: imported definition carries unrecognized field(s) %v — review before relying on this Staff member (inert to clvsync, loaded by Clairvoyance)", r.Name, unknown)
		}
		entry, repoints := repointDeadPaths(entry, in.DataDir)
		action, _, _, trustNote, err := in.MergeStaffEntry(prof, entry, r.ID, mode, opts.DryRun)
		if err != nil {
			return nil, err
		}
		if trustNote != "" {
			rep.warn("trust: roster %s: %s", r.Name, trustNote) // D18
		}
		rep.plan("roster: %s persona %s", action, r.Name)
		if action == "created" || action == "overwritten" {
			for _, rp := range repoints {
				rep.plan("roster %s: %s", r.Name, rp)
			}
		}
		placeTemplate(rdir, "", r.Template, in, rep, opts)
		mergeHistory(rdir, "", r.ID, prof, in, rep, opts)
		reserveStaffName(in, r.Name, rep, opts)
		if !opts.DryRun {
			rep.placed("persona:" + r.Name)
		}
	}
	return rep, nil
}

// applyWorkspaceHeavy overlays the Tier-4 heavy dirs onto an ALREADY-imported
// Tier-3 workspace (§8a: the add-on is paired, never standalone).
func applyWorkspaceHeavy(stage string, meta pkg.Meta, in *clv.Instance, opts Options) (*Report, error) {
	ws, ok := in.WorkspaceByName(meta.WorkspaceName)
	if !ok {
		return nil, fmt.Errorf("Tier 4 heavy add-on requires workspace %q to already exist — import its Tier 3 package first", meta.WorkspaceName)
	}
	rep := &Report{Tier: 4, PersonaName: meta.WorkspaceName, WorkspaceName: meta.WorkspaceName, WorkspaceID: ws.ID,
		ReviewNote: "heavy add-on overlaid onto the existing workspace"}
	if src := filepath.Join(stage, "workspace-heavy"); dirExists(src) {
		rep.plan("overlay heavy/regenerable content onto %q", ws.Name)
		if !opts.DryRun {
			if err := pkg.CopyTree(src, ws.Path, nil); err != nil {
				return nil, err
			}
			rep.placed("workspace-heavy")
		}
	}
	return rep, nil
}

// --- shared helpers ---

func orNone[T any](s []T) any {
	if len(s) == 0 {
		return "none"
	}
	return s
}

func placeTemplate(root, subdir, template string, in *clv.Instance, rep *Report, opts Options) {
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
	rep.plan("template: place %s", template)
	if opts.DryRun {
		return
	}
	if pkg.CopyFile(srcT, dstT) == nil {
		rep.placed("template:"+template, dstT)
	}
}

// mergeMemory unions the packaged memory into the target. In sync mode it is a
// per-file union (identical files skipped, differing files backed up then updated);
// in overwrite mode the scope dir is replaced wholesale (§17.2).
func mergeMemory(stage, lname string, in *clv.Instance, rep *Report, opts Options) {
	memRoot := filepath.Join(stage, "memory")
	ents, err := os.ReadDir(memRoot)
	if err != nil {
		return
	}
	overwrite := opts.mode() == clv.ModeOverwrite
	// P3: refuse an empty/path-bearing persona key. In overwrite mode dstDir would
	// collapse to the staff-memory root and RemoveAll would wipe ALL personas.
	if !clv.ValidMemKey(lname) {
		rep.warn("memory SKIPPED: refusing unsafe persona key %q (would target the staff-memory root)", lname)
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
		added, updated := mergeDir(srcDir, dstDir, overwrite, opts.DryRun, rep)
		rep.plan("memory/%s: +%d new, ~%d updated", scope, added, updated)
		rep.MemoryPlaced = true // Q1: surfaces on the persona's next session start
		if !opts.DryRun {
			rep.placed("memory/" + scope)
		}
	}
}

// mergeDir copies srcDir into dstDir file-by-file. Returns counts of added and updated
// files. Differing existing files are backed up (S7) unless overwrite replaced the tree.
func mergeDir(srcDir, dstDir string, overwrite, dryRun bool, rep *Report) (added, updated int) {
	if overwrite && !dryRun {
		// P3 defense-in-depth: never RemoveAll a path whose final segment is not a
		// concrete persona folder (guards against an empty/'.'/'..' key slipping through).
		if seg := filepath.Base(dstDir); clv.ValidMemKey(seg) {
			_ = os.RemoveAll(dstDir)
		}
	}
	filepath.Walk(srcDir, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(srcDir, p)
		dst := filepath.Join(dstDir, rel)
		sb, _ := os.ReadFile(p)
		if db, derr := os.ReadFile(dst); derr == nil {
			if string(db) == string(sb) {
				return nil // identical: nothing to do
			}
			updated++
			if !dryRun {
				_ = os.WriteFile(dst+".clvsync-bak", db, 0o644)
			}
		} else {
			added++
		}
		if !dryRun {
			_ = os.MkdirAll(filepath.Dir(dst), 0o755)
			if os.WriteFile(dst, sb, 0o644) == nil {
				rep.files = append(rep.files, dst) // hash into the receipt
			}
		}
		return nil
	})
	return added, updated
}

// mergeHistory applies the packaged transcript under the D13 newest-wins policy:
// take the incoming transcript only if it is newer (savedAt) or larger; back up any
// replaced transcript and warn if the two have diverged.
func mergeHistory(root, subdir, id, prof string, in *clv.Instance, rep *Report, opts Options) {
	srcH := filepath.Join(root, subdir, "history", id+".json")
	sb, err := os.ReadFile(srcH)
	if err != nil {
		return
	}
	dst := filepath.Join(in.DataDir, "profiles", prof, "agent-history", id+".json")
	db, _ := os.ReadFile(dst)
	take, diverged := clv.HistoryDecision(db, sb)
	if opts.mode() == clv.ModeOverwrite {
		take = true
	}
	if diverged {
		rep.warn("history for %s diverged between machines; kept the %s copy (a .clvsync-bak preserves the other)", id, pick(take, "incoming", "local"))
	}
	if !take {
		rep.plan("history: keep local (newer or identical)")
		return
	}
	rep.plan("history: take incoming transcript")
	if opts.DryRun {
		return
	}
	if len(db) > 0 {
		_ = os.WriteFile(dst+".clvsync-bak", db, 0o644)
	}
	if err := pkg.CopyFile(srcH, dst); err == nil {
		rep.placed("history", dst)
	}
}

func pick(b bool, yes, no string) string {
	if b {
		return yes
	}
	return no
}

func mergeResume(stage, subdir, prof string, in *clv.Instance, rep *Report, opts Options) {
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
		for _, rec := range applied {
			rep.SessionIDs = append(rep.SessionIDs, fmt.Sprint(rec["sessionId"]))
		}
		if len(applied) > 0 {
			rep.plan("resume: merge %d session record(s)", len(applied))
			if !opts.DryRun && in.MergeSessions(prof, applied) == nil {
				rep.placed("resume/sessions")
			}
		}
	}
	if es := readEntries(filepath.Join(stage, subdir, "resume", "session-summaries.json")); len(es) > 0 && !opts.DryRun {
		if in.MergeResumeEntries(prof, "session-summaries.json", es) == nil {
			rep.placed("resume/summaries")
		}
	}
	if es := readEntries(filepath.Join(stage, subdir, "resume", "resume-exclusions.json")); len(es) > 0 && !opts.DryRun {
		if in.MergeResumeEntries(prof, "resume-exclusions.json", es) == nil {
			rep.placed("resume/exclusions")
		}
	}
}

// writeReceipt records what the import placed for the restart-time reconciliation.
func writeReceipt(rep *Report, in *clv.Instance, opts Options) error {
	path := opts.ReceiptPath
	if path == "" {
		path = filepath.Join(in.DataDir, "import-receipt.json")
	}
	r := newReceipt(in, opts.mode(), rep)
	for _, f := range rep.files {
		r.addFile(f)
	}
	if err := r.Write(path); err != nil {
		return err
	}
	rep.ReceiptPath = path
	return nil
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

// machineLocalAdvisories inspects a freshly-created persona definition for machine-local
// fields that carried over from the SOURCE machine and likely need review on this one
// (§17.1: on a create there is no destination value to preserve). Non-fatal; advisory.
func machineLocalAdvisories(entry []byte) []string {
	var d struct {
		Shell struct {
			Cwd     string `json:"cwd"`
			Command string `json:"command"`
			Type    string `json:"type"`
		} `json:"shell"`
		AI struct {
			Provider string `json:"provider"`
			Model    string `json:"model"`
		} `json:"ai"`
	}
	if json.Unmarshal(entry, &d) != nil {
		return nil
	}
	var out []string
	// shell.cwd is auto-repointed by repointDeadPaths, so it is no longer advised here.
	if cmd := d.Shell.Command; cmd != "" && looksWindowsShell(cmd) {
		out = append(out, fmt.Sprintf("definition's shell is %q — if this machine is not Windows, change the shell in the persona settings", cmd))
	}
	if p := d.AI.Provider; p != "" {
		out = append(out, fmt.Sprintf("definition runs under provider=%q — if this machine uses a different provider/model, update it in the persona settings (Universal Resume still resumes the session cross-provider)", p))
	}
	return out
}

// repointDeadPaths rewrites machine-local path fields in an incoming definition that
// don't exist on THIS machine (chiefly the source machine's shell.cwd), pointing them at
// the target data dir so a freshly created/overwritten persona starts cleanly (v0.1.1).
// A path that already exists here is left untouched. Returns the (possibly rewritten)
// entry and a human list of what changed.
func repointDeadPaths(entry json.RawMessage, dataDir string) (json.RawMessage, []string) {
	var m map[string]json.RawMessage
	if json.Unmarshal(entry, &m) != nil {
		return entry, nil
	}
	shellRaw, ok := m["shell"]
	if !ok {
		return entry, nil
	}
	var shell map[string]json.RawMessage
	if json.Unmarshal(shellRaw, &shell) != nil {
		return entry, nil
	}
	var cwd string
	if cwdRaw, ok := shell["cwd"]; !ok || json.Unmarshal(cwdRaw, &cwd) != nil {
		return entry, nil
	}
	if cwd == "" || dirExists(cwd) {
		return entry, nil // absent field or a path that exists here → nothing to do
	}
	nb, _ := json.Marshal(dataDir)
	shell["cwd"] = nb
	nsh, err := json.Marshal(shell)
	if err != nil {
		return entry, nil
	}
	m["shell"] = nsh
	out, err := json.Marshal(m)
	if err != nil {
		return entry, nil
	}
	return out, []string{fmt.Sprintf("repointed shell.cwd %q → %q (source path absent on this machine)", cwd, dataDir)}
}

func looksWindowsShell(cmd string) bool {
	c := filepath.Base(cmd)
	return c == "powershell.exe" || c == "pwsh.exe" || c == "cmd.exe"
}
