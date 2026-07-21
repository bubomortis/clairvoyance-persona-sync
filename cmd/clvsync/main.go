// Command clvsync is the Clairvoyance Persona & Workspace Sync CLI.
//
// Secrets (encryption passphrase, signing-key password) are read from environment
// variables, never flags, so they don't appear in the process list.
package main

import (
	"bufio"
	"crypto/rand"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"aead.dev/minisign"

	"github.com/bubomortis/clairvoyance-persona-sync/internal/clv"
	"github.com/bubomortis/clairvoyance-persona-sync/internal/cryptobox"
	"github.com/bubomortis/clairvoyance-persona-sync/internal/datadir"
	"github.com/bubomortis/clairvoyance-persona-sync/internal/diskfree"
	"github.com/bubomortis/clairvoyance-persona-sync/internal/export"
	"github.com/bubomortis/clairvoyance-persona-sync/internal/importer"
	"github.com/bubomortis/clairvoyance-persona-sync/internal/state"
)

const usage = `clvsync — Clairvoyance Persona & Workspace Sync

Commands:
  export          Export a persona (--persona, Tier 1/2) or workspace (--workspace, Tier 3)
  import          Import a package into this instance (create-or-merge; --dry-run to preview)
  verify          Verify a package's signature + integrity (no import)
  verify-import   Reconcile a post-import receipt against live state (§19.2)
  workspace-prep  Register + scaffold a workspace to receive an import (run app-closed)
  keygen          Generate a minisign signing keypair
  datadir         Print the resolved Clairvoyance data directory
  last-export-dir Print the directory the last export was written to (blank if none yet)

Run 'clvsync import' with no --in for a guided (interactive) import.

Secrets come from env vars (not flags):
  CLVSYNC_PASSPHRASE     age encryption/decryption passphrase
  CLVSYNC_SIGN_PASS      password protecting a minisign private key

Run 'clvsync <command> -h' for command flags.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "datadir":
		var d string
		if d, err = datadir.Resolve(); err == nil {
			fmt.Println(d)
		}
	case "last-export-dir":
		var d string
		if d, err = datadir.Resolve(); err == nil {
			fmt.Println(state.Load(d).LastExportDir)
		}
	case "keygen":
		err = cmdKeygen(os.Args[2:])
	case "export":
		err = cmdExport(os.Args[2:])
	case "import":
		err = cmdImport(os.Args[2:])
	case "workspace-prep":
		err = cmdWorkspacePrep(os.Args[2:])
	case "verify":
		err = cmdVerify(os.Args[2:])
	case "verify-import":
		err = cmdVerifyImport(os.Args[2:])
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func resolveDataDir(override string) (*clv.Instance, error) {
	dir := override
	if dir == "" {
		d, err := datadir.Resolve()
		if err != nil {
			return nil, err
		}
		dir = d
	}
	return clv.Open(dir)
}

func cmdKeygen(args []string) error {
	fs := flag.NewFlagSet("keygen", flag.ExitOnError)
	out := fs.String("out", "clvsync-signing", "output key prefix (writes <prefix>.pub and <prefix>.key)")
	fs.Parse(args)
	pass := os.Getenv("CLVSYNC_SIGN_PASS")
	if pass == "" {
		return fmt.Errorf("set CLVSYNC_SIGN_PASS to protect the private key")
	}
	pub, priv, err := minisign.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	pubText, err := pub.MarshalText()
	if err != nil {
		return err
	}
	if err := os.WriteFile(*out+".pub", pubText, 0o644); err != nil {
		return err
	}
	encKey, err := minisign.EncryptKey(pass, priv)
	if err != nil {
		return err
	}
	if err := os.WriteFile(*out+".key", encKey, 0o600); err != nil {
		return err
	}
	fmt.Printf("wrote %s.pub and %s.key\n", *out, *out)
	return nil
}

func cmdExport(args []string) error {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	persona := fs.String("persona", "", "persona name or staff id (Tier 1/2)")
	workspace := fs.String("workspace", "", "workspace name to export whole (Tier 3)")
	out := fs.String("out", "", "output package path (full path). If omitted, uses --out-dir or the last export location")
	outDir := fs.String("out-dir", "", "directory to write the package into (filename auto-generated); defaults to the last export location")
	tier := fs.Int("tier", 1, "1 = portable persona; 2 = + Universal Resume (cross-model session resume)")
	recipient := fs.String("recipient", "", "age X25519 recipient public key (encrypt to key)")
	signKey := fs.String("sign-key", "", "minisign private key file to sign with")
	allowSecrets := fs.Bool("allow-secrets", false, "override the secret-scan block")
	allowOperator := fs.Bool("allow-operator-sync", false, "override the S15 guard against exporting the Sync Operator")
	includeHeavy := fs.Bool("include-heavy", false, "workspace: also emit the Tier 4 heavy add-on (space-gated)")
	dataDir := fs.String("data-dir", "", "override Clairvoyance data dir")
	fs.Parse(args)
	if *persona == "" && *workspace == "" {
		return fmt.Errorf("one of --persona or --workspace is required")
	}
	in, err := resolveDataDir(*dataDir)
	if err != nil {
		return err
	}
	opts := export.Options{Tier: *tier, Recipient: *recipient, AllowSecrets: *allowSecrets, AllowOperatorSync: *allowOperator, Passphrase: os.Getenv("CLVSYNC_PASSPHRASE")}
	if *signKey != "" {
		priv, err := loadPrivateKey(*signKey)
		if err != nil {
			return err
		}
		opts.SignKey = &priv
	}

	// Resolve the output path: --out (full) > --out-dir/<name> > last export location/<name>.
	// After the first export the directory is remembered so future exports default to it.
	outPath := *out
	if outPath == "" {
		dir := *outDir
		usedLast := false
		if dir == "" {
			dir = state.Load(in.DataDir).LastExportDir
			usedLast = dir != ""
		}
		if dir == "" {
			return fmt.Errorf("no output location — pass --out <file> or --out-dir <dir> (after your first export, clvsync remembers the directory and defaults to it)")
		}
		base := *persona
		if *workspace != "" {
			base = *workspace + "-workspace"
		}
		base = strings.ReplaceAll(strings.TrimSpace(base), " ", "-")
		ext := ".cvpkg"
		if opts.Passphrase != "" || opts.Recipient != "" {
			ext += ".age"
		}
		outPath = filepath.Join(dir, base+ext)
		if usedLast {
			fmt.Printf("using last export location: %s\n", outPath)
		} else {
			fmt.Printf("output: %s\n", outPath)
		}
	}

	var res *export.Result
	if *workspace != "" {
		res, err = export.Workspace(in, *workspace, outPath, opts)
	} else {
		p, ferr := in.FindPersona(*persona)
		if ferr != nil {
			return ferr
		}
		res, err = export.Persona(in, p, outPath, opts)
	}
	if err != nil {
		if res != nil && len(res.SecretHits) > 0 {
			fmt.Fprintf(os.Stderr, "secret-scan found %d match(es):\n", len(res.SecretHits))
			for _, h := range res.SecretHits {
				fmt.Fprintf(os.Stderr, "  %s:%d  %s\n", h.Path, h.Line, h.Pattern)
			}
		}
		return err
	}
	// Remember the directory so the next export can default to it.
	_ = state.RememberExportDir(in.DataDir, filepath.Dir(res.PackagePath))

	label := *persona
	if *workspace != "" {
		label = "workspace:" + *workspace
	}
	fmt.Printf("exported %s -> %s (encrypted=%v)\n", label, res.PackagePath, res.Encrypted)
	if res.SigPath != "" {
		fmt.Printf("signature: %s\n", res.SigPath)
	}
	// P4: a file the scanner couldn't fully read is not the same as a clean file.
	if len(res.SecretSkips) > 0 {
		fmt.Fprintf(os.Stderr, "⚠ secret scan skipped %d file(s) (not text-scanned for secrets):\n", len(res.SecretSkips))
		for _, sk := range res.SecretSkips {
			fmt.Fprintf(os.Stderr, "    %s — %s\n", sk.Path, sk.Reason)
		}
	}
	if *workspace != "" && *includeHeavy {
		return exportHeavy(in, *workspace, outPath, opts)
	}
	return nil
}

// exportHeavy emits the Tier-4 add-on AFTER the Tier-3 package, gated on free space
// at the target (§8a): if the heavy content won't fit, it is skipped, not truncated.
func exportHeavy(in *clv.Instance, wsName, baseOut string, opts export.Options) error {
	sz := export.HeavySize(in, wsName)
	if sz == 0 {
		fmt.Println("Tier 4: no heavy/regenerable content; skipped")
		return nil
	}
	heavyOut := heavyName(baseOut)
	dir := filepath.Dir(heavyOut)
	if dir == "" {
		dir = "."
	}
	if free, err := diskfree.Available(dir); err == nil && uint64(sz) > free {
		fmt.Printf("Tier 4 SKIPPED (space-aware fail-down): heavy content ~%s, only %s free at target — workspace synced without regenerable content\n", human(sz), human(int64(free)))
		return nil
	}
	res, err := export.WorkspaceHeavy(in, wsName, heavyOut, opts)
	if err != nil {
		return err
	}
	fmt.Printf("Tier 4 heavy add-on -> %s (encrypted=%v)\n", res.PackagePath, res.Encrypted)
	return nil
}

func heavyName(out string) string {
	for _, ext := range []string{".cvpkg.age", ".cvpkg"} {
		if strings.HasSuffix(out, ext) {
			return out[:len(out)-len(ext)] + "_heavy" + ext
		}
	}
	return out + "_heavy"
}

func human(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func cmdVerify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	in := fs.String("in", "", "package path (required)")
	verifyKey := fs.String("verify-key", "", "minisign public key file")
	sigFile := fs.String("sig", "", "detached signature file")
	fs.Parse(args)
	if *in == "" {
		return fmt.Errorf("--in is required")
	}
	opts := importer.Options{Passphrase: os.Getenv("CLVSYNC_PASSPHRASE")}
	if *verifyKey != "" {
		pub, err := loadPublicKey(*verifyKey)
		if err != nil {
			return err
		}
		opts.VerifyKey = &pub
		if *sigFile == "" {
			return fmt.Errorf("--verify-key requires --sig")
		}
		if opts.Sig, err = os.ReadFile(*sigFile); err != nil {
			return err
		}
	}
	meta, err := importer.Inspect(*in, opts)
	if err != nil {
		return err
	}
	who := meta.PersonaName
	if meta.Tier >= 3 {
		who = "workspace:" + meta.WorkspaceName
	}
	// P2: never report a signature as verified when none was checked. The
	// manifest ships inside the package, so integrity alone is not authenticity.
	if opts.VerifyKey == nil {
		fmt.Printf("UNVERIFIED  tier=%d  %s  (created %s on %s)\n", meta.Tier, who, meta.CreatedAt, meta.SourceOS)
		fmt.Println("  manifest integrity OK, but AUTHENTICITY was NOT verified: no --verify-key/--sig supplied.")
		fmt.Println("  re-run with --verify-key <pub> --sig <file.minisig> to verify the publisher's signature.")
		return fmt.Errorf("unverified: package signature was not checked (no --verify-key)")
	}
	fmt.Printf("OK  tier=%d  %s  (created %s on %s)\n", meta.Tier, who, meta.CreatedAt, meta.SourceOS)
	fmt.Println("  signature verified + manifest integrity OK")
	return nil
}

func cmdWorkspacePrep(args []string) error {
	fs := flag.NewFlagSet("workspace-prep", flag.ExitOnError)
	name := fs.String("name", "", "workspace name to register (required)")
	path := fs.String("path", "", "local directory for the workspace (required)")
	dataDir := fs.String("data-dir", "", "override Clairvoyance data dir")
	fs.Parse(args)
	if *name == "" || *path == "" {
		return fmt.Errorf("--name and --path are required")
	}
	in, err := resolveDataDir(*dataDir)
	if err != nil {
		return err
	}
	ws, created, err := in.EnsureWorkspace(*name, *path)
	if err != nil {
		return err
	}
	if created {
		fmt.Printf("registered workspace %q (%s) at %s\n", ws.Name, ws.ID, ws.Path)
		fmt.Println("note: run this with Clairvoyance CLOSED, then start the app to pick up the new workspace.")
	} else {
		fmt.Printf("workspace %q already registered (%s) at %s\n", ws.Name, ws.ID, ws.Path)
	}
	return nil
}

func cmdImport(args []string) error {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	in := fs.String("in", "", "package path (with no --in, runs a guided interactive import)")
	identity := fs.String("identity", "", "age X25519 identity file (decrypt with key)")
	verifyKey := fs.String("verify-key", "", "minisign public key file to verify the signature")
	sigFile := fs.String("sig", "", "detached signature file (.minisig)")
	mode := fs.String("mode", "sync", "collision handling: sync (create-or-merge) | overwrite | skip")
	force := fs.Bool("force", false, "back-compat alias for --mode overwrite")
	dryRun := fs.Bool("dry-run", false, "preview the plan without writing anything")
	allowOperator := fs.Bool("allow-operator-sync", false, "override the S15 guard against importing the Sync Operator")
	wsPath := fs.String("workspace-path", "", "Tier 3: local path to create the target workspace if absent")
	receipt := fs.String("receipt", "", "where to write import-receipt.json (default: <data-dir>/import-receipt.json)")
	dataDir := fs.String("data-dir", "", "override Clairvoyance data dir")
	fs.Parse(args)

	// Guided interactive import when no package is named (§19 non-CLI path).
	if *in == "" {
		return interactiveImport(*dataDir)
	}

	m, err := clv.ParseMode(*mode)
	if err != nil {
		return err
	}
	inst, err := resolveDataDir(*dataDir)
	if err != nil {
		return err
	}
	opts := importer.Options{
		Mode: m, Force: *force, DryRun: *dryRun, AllowOperatorSync: *allowOperator,
		WorkspacePath: *wsPath, ReceiptPath: *receipt, Passphrase: os.Getenv("CLVSYNC_PASSPHRASE"),
	}
	if *identity != "" {
		b, err := os.ReadFile(*identity)
		if err != nil {
			return err
		}
		opts.Identity = string(b)
	}
	if *verifyKey != "" {
		pub, err := loadPublicKey(*verifyKey)
		if err != nil {
			return err
		}
		opts.VerifyKey = &pub
		if *sigFile == "" {
			return fmt.Errorf("--verify-key requires --sig")
		}
		if opts.Sig, err = os.ReadFile(*sigFile); err != nil {
			return err
		}
	}
	rep, err := importer.Apply(*in, inst, opts)
	if err != nil {
		return err
	}
	printReport(rep)
	return nil
}

// printReport renders an import (or dry-run) report.
func printReport(rep *importer.Report) {
	verb := "imported"
	if rep.DryRun {
		verb = "DRY-RUN (no changes written)"
	}
	fmt.Printf("%s %s (%s), tier %d, mode %s\n", verb, rep.PersonaName, rep.PersonaID, rep.Tier, rep.Mode)
	for _, p := range rep.Plan {
		fmt.Printf("  plan: %s\n", p)
	}
	if !rep.DryRun && len(rep.Placed) > 0 {
		fmt.Printf("  placed: %v\n", rep.Placed)
	}
	for _, w := range rep.Warnings {
		fmt.Printf("  ⚠ %s\n", w)
	}
	if len(rep.SkippedScopes) > 0 {
		fmt.Printf("  skipped: %v\n", rep.SkippedScopes)
	}
	if rep.ReceiptPath != "" {
		fmt.Printf("  receipt: %s  (run 'clvsync verify-import --receipt %s' after restart)\n", rep.ReceiptPath, rep.ReceiptPath)
	}
	fmt.Printf("  note: %s\n", rep.ReviewNote)
}

// interactiveImport walks a non-CLI user through file → passphrase → dry-run preview
// → confirm → apply (§19 guided wrapper).
func interactiveImport(dataDir string) error {
	r := bufio.NewReader(os.Stdin)
	ask := func(prompt string) string {
		fmt.Print(prompt)
		s, _ := r.ReadString('\n')
		return strings.TrimSpace(s)
	}
	pkgPath := ask("Package file (.cvpkg / .cvpkg.age): ")
	if pkgPath == "" {
		return fmt.Errorf("no package given")
	}
	pass := os.Getenv("CLVSYNC_PASSPHRASE")
	if pass == "" {
		pass = ask("Passphrase (blank if the package is not encrypted): ")
	}
	mode := ask("Mode [sync]/overwrite/skip: ")
	m, err := clv.ParseMode(mode)
	if err != nil {
		return err
	}
	inst, err := resolveDataDir(dataDir)
	if err != nil {
		return err
	}
	base := importer.Options{Mode: m, Passphrase: pass}

	// Always preview first.
	preview := base
	preview.DryRun = true
	rep, err := importer.Apply(pkgPath, inst, preview)
	if err != nil {
		return err
	}
	fmt.Println("\n--- preview ---")
	printReport(rep)
	if ask("\nApply these changes? (yes/no): ") != "yes" {
		fmt.Println("aborted; nothing changed.")
		return nil
	}
	fmt.Println("Make sure Clairvoyance is CLOSED before applying, so its file writes don't collide.")
	if ask("Is Clairvoyance closed? (yes/no): ") != "yes" {
		fmt.Println("aborted; close the app and re-run.")
		return nil
	}
	rep, err = importer.Apply(pkgPath, inst, base)
	if err != nil {
		return err
	}
	printReport(rep)
	return nil
}

func cmdVerifyImport(args []string) error {
	fs := flag.NewFlagSet("verify-import", flag.ExitOnError)
	receipt := fs.String("receipt", "", "import-receipt.json path (required)")
	dataDir := fs.String("data-dir", "", "override Clairvoyance data dir")
	fs.Parse(args)
	if *receipt == "" {
		return fmt.Errorf("--receipt is required")
	}
	rec, err := importer.LoadReceipt(*receipt)
	if err != nil {
		return err
	}
	dd := *dataDir
	if dd == "" {
		dd = rec.DataDir
	}
	inst, err := resolveDataDir(dd)
	if err != nil {
		return err
	}
	res := importer.VerifyReceipt(rec, inst)
	fmt.Printf("verify-import: %s (tier %d, mode %s, imported %s)\n", rec.PersonaName, rec.Tier, rec.Mode, rec.ImportedAt)
	for _, l := range res.Lines {
		mark := "PASS"
		if !l.OK {
			mark = "FAIL"
		}
		fmt.Printf("  [%s] %-9s %s\n", mark, l.Layer, l.Detail)
	}
	if !res.OK {
		return fmt.Errorf("reconciliation found mismatches — see FAIL rows above")
	}
	fmt.Println("  all checks passed; note: whether the session is offered for RESUME is still a human check in the UI.")
	return nil
}

func loadPrivateKey(path string) (minisign.PrivateKey, error) {
	pass := os.Getenv("CLVSYNC_SIGN_PASS")
	if pass == "" {
		return minisign.PrivateKey{}, fmt.Errorf("set CLVSYNC_SIGN_PASS to decrypt the signing key")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return minisign.PrivateKey{}, err
	}
	return minisign.DecryptKey(pass, b)
}

func loadPublicKey(path string) (minisign.PublicKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return minisign.PublicKey{}, err
	}
	var pub minisign.PublicKey
	if err := pub.UnmarshalText(b); err != nil {
		return minisign.PublicKey{}, err
	}
	return pub, nil
}

var _ = cryptobox.GenerateSigningKey // keep cryptobox linked for library parity
