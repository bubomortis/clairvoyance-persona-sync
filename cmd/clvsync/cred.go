package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bubomortis/clairvoyance-persona-sync/internal/cred"
	"github.com/bubomortis/clairvoyance-persona-sync/internal/datadir"
	"github.com/bubomortis/clairvoyance-persona-sync/internal/dpapi"
	"github.com/bubomortis/clairvoyance-persona-sync/internal/export"
	"github.com/bubomortis/clairvoyance-persona-sync/internal/importer"
)

const credDirName = "clvsync-cred"

const credUsage = `clvsync cred — encryption credential model (D17 §20)

  cred status                       Show the selected model, local identity, and paired peers
  cred model <name> [--pairing p]   Select the credential model:
                                      shared-passphrase | per-transfer | identity
                                      --pairing cloud-sync|travel (identity only)
  cred init                         Generate this machine's age identity (Model 2c), sealed at rest
  cred pubkey [--out <file>]        Print (or write) this machine's PUBLIC key / pairing doc
  cred pair --name <n> --key <age1…>   Trust a peer's public key (trust-on-first-use)
  cred pair --in <pairing-file>     Trust a peer from a pairing doc written by 'cred pubkey --out'
  cred peers                        List trusted peer public keys
  cred unpair --name <n>            Remove a trusted peer (then re-pair to accept a rotated key)

The private identity never leaves this machine (DPAPI-sealed on Windows); only the
public key is ever shared. Nothing here is entered in chat — keys are public, and the
shared-passphrase model reads CLVSYNC_PASSPHRASE from the environment.
`

// openCredManager roots the broker under the resolved (or overridden) data dir and
// wires the platform sealer (DPAPI on Windows, permissions-only elsewhere).
func openCredManager(dataDirOverride string) (*cred.Manager, error) {
	dir := dataDirOverride
	if dir == "" {
		d, err := datadir.Resolve()
		if err != nil {
			return nil, err
		}
		dir = d
	}
	return cred.NewManager(filepath.Join(dir, credDirName), dpapi.Sealer()), nil
}

func cmdCred(args []string) error {
	if len(args) == 0 {
		fmt.Print(credUsage)
		return nil
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "status":
		return credStatus(rest)
	case "model":
		return credSetModel(rest)
	case "init":
		return credInit(rest)
	case "pubkey":
		return credPubkey(rest)
	case "pair":
		return credPair(rest)
	case "peers":
		return credPeers(rest)
	case "unpair":
		return credUnpair(rest)
	case "-h", "--help", "help":
		fmt.Print(credUsage)
		return nil
	default:
		return fmt.Errorf("unknown 'cred' subcommand %q\n\n%s", sub, credUsage)
	}
}

func credStatus(args []string) error {
	fs := flag.NewFlagSet("cred status", flag.ExitOnError)
	dataDir := fs.String("data-dir", "", "override Clairvoyance data dir")
	fs.Parse(args)
	m, err := openCredManager(*dataDir)
	if err != nil {
		return err
	}
	cfg, err := m.Config()
	if err != nil {
		return err
	}

	fmt.Println("Credential model:")
	if cfg.Model == "" {
		fmt.Println("  model:   (none selected — run 'clvsync cred model <name>')")
	} else {
		fmt.Printf("  model:   %s\n", cfg.Model)
		if cfg.Model == cred.ModelIdentity {
			pr := cfg.Pairing
			if pr == "" {
				pr = "(not set)"
			}
			fmt.Printf("  pairing: %s\n", pr)
		}
	}

	if cfg.Model == cred.ModelIdentity || m.HasIdentity() {
		fmt.Println("\nLocal identity (Model 2c):")
		if m.HasIdentity() {
			pub, err := m.PublicKey()
			if err != nil {
				return err
			}
			fmt.Printf("  present: yes\n  sealing: %s\n  public:  %s\n", m.SealerName(), pub)
			if m.SealerName() == "none" {
				fmt.Println("  ⚠ private key is NOT sealed at rest on this platform (permissions-only).")
			}
		} else {
			fmt.Println("  present: no  — run 'clvsync cred init'")
		}

		peers, err := m.Peers()
		if err != nil {
			return err
		}
		fmt.Printf("\nTrusted peers: %d\n", len(peers))
		names := make([]string, 0, len(peers))
		for n := range peers {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			fmt.Printf("  %s  %s\n", n, peers[n])
		}
	}
	return nil
}

func credSetModel(args []string) error {
	// The model name is the first positional argument; take it before flag parsing
	// so 'cred model identity --pairing travel' works (Go's flag parser otherwise
	// stops at the leading positional and never sees --pairing).
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("usage: clvsync cred model <shared-passphrase|per-transfer|identity> [--pairing cloud-sync|travel]")
	}
	modelName := args[0]
	fs := flag.NewFlagSet("cred model", flag.ExitOnError)
	pairing := fs.String("pairing", "", "identity model only: cloud-sync | travel")
	dataDir := fs.String("data-dir", "", "override Clairvoyance data dir")
	fs.Parse(args[1:])
	cfg := cred.Config{Model: cred.Model(modelName), Pairing: cred.Pairing(*pairing)}
	if err := m0(dataDir).SetConfig(cfg); err != nil {
		return err
	}
	fmt.Printf("credential model set: %s", cfg.Model)
	if cfg.Model == cred.ModelIdentity && *pairing != "" {
		fmt.Printf(" (pairing: %s)", *pairing)
	}
	fmt.Println()
	if cfg.Model == cred.ModelIdentity {
		mgr := m0(dataDir)
		if !mgr.HasIdentity() {
			fmt.Println("next: run 'clvsync cred init' to create this machine's identity, then pair with your other machine.")
		}
	}
	return nil
}

func credInit(args []string) error {
	fs := flag.NewFlagSet("cred init", flag.ExitOnError)
	dataDir := fs.String("data-dir", "", "override Clairvoyance data dir")
	fs.Parse(args)
	m := m0(dataDir)
	doc, err := m.InitIdentity()
	if errors.Is(err, cred.ErrIdentityExists) {
		pub, _ := m.PublicKey()
		return fmt.Errorf("an identity already exists on this machine (public key %s). Delete the cred dir deliberately to rotate — this would break peers that trust the old key", pub)
	}
	if err != nil {
		return err
	}
	fmt.Printf("created local identity (sealing: %s)\n", m.SealerName())
	fmt.Printf("public key (share this with your other machine):\n  %s\n", doc.PublicKey)
	if m.SealerName() == "none" {
		fmt.Println("⚠ this platform has no at-rest sealing — the private key is protected by file permissions only.")
	}
	return nil
}

func credPubkey(args []string) error {
	fs := flag.NewFlagSet("cred pubkey", flag.ExitOnError)
	out := fs.String("out", "", "write a pairing doc (JSON) to this file instead of printing the key")
	name := fs.String("name", "", "name to advertise in the pairing doc (defaults to hostname)")
	dataDir := fs.String("data-dir", "", "override Clairvoyance data dir")
	fs.Parse(args)
	m := m0(dataDir)
	pub, err := m.PublicKey()
	if errors.Is(err, cred.ErrNoIdentity) {
		return fmt.Errorf("no local identity yet — run 'clvsync cred init' first")
	}
	if err != nil {
		return err
	}
	if *out == "" {
		fmt.Println(pub)
		return nil
	}
	advertised := strings.TrimSpace(*name)
	if advertised == "" {
		if h, err := os.Hostname(); err == nil {
			advertised = h
		} else {
			advertised = "this-machine"
		}
	}
	b, _ := json.MarshalIndent(cred.PairingDoc{Name: advertised, PublicKey: pub}, "", "  ")
	if err := os.WriteFile(*out, b, 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote pairing doc for %q -> %s (public key only; safe to send)\n", advertised, *out)
	return nil
}

func credPair(args []string) error {
	fs := flag.NewFlagSet("cred pair", flag.ExitOnError)
	name := fs.String("name", "", "peer name")
	key := fs.String("key", "", "peer age public key (age1…)")
	in := fs.String("in", "", "pairing doc file written by 'cred pubkey --out'")
	dataDir := fs.String("data-dir", "", "override Clairvoyance data dir")
	fs.Parse(args)

	peerName, peerKey := *name, *key
	if *in != "" {
		b, err := os.ReadFile(*in)
		if err != nil {
			return err
		}
		var doc cred.PairingDoc
		if err := json.Unmarshal(b, &doc); err != nil {
			return fmt.Errorf("not a valid pairing doc: %w", err)
		}
		if peerName == "" {
			peerName = doc.Name
		}
		peerKey = doc.PublicKey
	}
	if peerName == "" || peerKey == "" {
		return fmt.Errorf("usage: clvsync cred pair --name <n> --key <age1…>   OR   --in <pairing-file>")
	}

	m := m0(dataDir)
	added, err := m.AddPeer(peerName, peerKey)
	if errors.Is(err, cred.ErrPeerConflict) {
		return fmt.Errorf("peer %q is already trusted with a DIFFERENT public key — refusing to overwrite. If the peer deliberately rotated, run 'clvsync cred unpair --name %s' then pair again (verify the new key out of band first)", peerName, peerName)
	}
	if err != nil {
		return err
	}
	if added {
		fmt.Printf("trusted peer %q -> %s\n", peerName, peerKey)
	} else {
		fmt.Printf("peer %q already trusted with this key (no change)\n", peerName)
	}
	return nil
}

func credPeers(args []string) error {
	fs := flag.NewFlagSet("cred peers", flag.ExitOnError)
	dataDir := fs.String("data-dir", "", "override Clairvoyance data dir")
	fs.Parse(args)
	peers, err := m0(dataDir).Peers()
	if err != nil {
		return err
	}
	if len(peers) == 0 {
		fmt.Println("no trusted peers")
		return nil
	}
	names := make([]string, 0, len(peers))
	for n := range peers {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Printf("%s\t%s\n", n, peers[n])
	}
	return nil
}

func credUnpair(args []string) error {
	fs := flag.NewFlagSet("cred unpair", flag.ExitOnError)
	name := fs.String("name", "", "peer name to remove")
	dataDir := fs.String("data-dir", "", "override Clairvoyance data dir")
	fs.Parse(args)
	if *name == "" {
		return fmt.Errorf("usage: clvsync cred unpair --name <n>")
	}
	removed, err := m0(dataDir).RemovePeer(*name)
	if err != nil {
		return err
	}
	if removed {
		fmt.Printf("removed peer %q\n", *name)
	} else {
		fmt.Printf("no peer named %q\n", *name)
	}
	return nil
}

// ---- Export / import integration (D17 §20) --------------------------------

// credAssistExport consults the configured credential model to fill an export's
// encryption when the user gave NO explicit directive (explicit == true short-
// circuits: --recipient / --plaintext / CLVSYNC_PASSPHRASE always win). It
// returns credResolved==true when the model fully determined encryption, so the
// caller skips the interactive plaintext-prompt fallback. A hard error carries
// actionable guidance (e.g. identity model but no peers paired yet).
func credAssistExport(dataDir string, opts *export.Options, explicit bool) (credResolved bool, err error) {
	if explicit {
		return false, nil
	}
	m, oerr := openCredManager(dataDir)
	if oerr != nil {
		return false, nil // no broker available → fall through to the fallback
	}
	plan, perr := m.ResolveEncrypt(opts.Passphrase)
	switch {
	case perr == nil:
		opts.Passphrase = plan.Passphrase
		opts.Recipients = plan.Recipients
		if plan.EmbedPublicKey != "" {
			opts.SenderPublicKey = plan.EmbedPublicKey
			opts.SenderName = hostnameOr("this-machine")
		}
		return true, nil
	case errors.Is(perr, cred.ErrNoModel), errors.Is(perr, cred.ErrNeedPassphrase):
		// No model selected, or a passphrase model with nothing supplied yet →
		// fall through to the interactive prompt / refuse fallback.
		return false, nil
	default:
		// ErrNoPeers / ErrNoIdentity — can't prompt our way out; guide the user.
		return false, fmt.Errorf("credential model: %w — set it up with 'clvsync cred ...', or pass --recipient/--plaintext/CLVSYNC_PASSPHRASE", perr)
	}
}

// credAssistDecrypt fills an import's decryption (passphrase or identity) from the
// configured credential model when the caller supplied neither explicitly. It is
// best-effort: on any model error it leaves opts untouched so Apply can still try
// plaintext or fail with an actionable decrypt error. Returns the manager (or nil)
// for a follow-up trust-on-first-use record.
func credAssistDecrypt(dataDir string, opts *importer.Options) *cred.Manager {
	if opts.Identity != "" || opts.Passphrase != "" {
		return nil
	}
	m, err := openCredManager(dataDir)
	if err != nil {
		return nil
	}
	if plan, perr := m.ResolveDecrypt(opts.Passphrase); perr == nil {
		opts.Passphrase = plan.Passphrase
		opts.Identity = plan.Identity
	}
	return m
}

// recordSenderTOFU trust-on-first-use records a traveling sender public key after
// a successful identity-model import, so the recipient can later encrypt back. It
// only acts when THIS machine uses the identity model, and never silently
// overwrites a changed key (that surfaces a warning for out-of-band verification).
func recordSenderTOFU(m *cred.Manager, rep *importer.Report) {
	if m == nil || rep == nil || rep.DryRun || rep.SenderPublicKey == "" {
		return
	}
	cfg, err := m.Config()
	if err != nil || cfg.Model != cred.ModelIdentity {
		return
	}
	name := strings.TrimSpace(rep.SenderName)
	if name == "" {
		name = "sender"
	}
	added, err := m.AddPeer(name, rep.SenderPublicKey)
	switch {
	case errors.Is(err, cred.ErrPeerConflict):
		fmt.Printf("  ⚠ sender %q offered a public key that DIFFERS from the one already trusted — NOT auto-updated. If they rotated deliberately: 'clvsync cred unpair --name %s' then re-pair after verifying out of band.\n", name, name)
	case err != nil:
		fmt.Printf("  ⚠ could not record sender public key: %v\n", err)
	case added:
		fmt.Printf("  ⓘ trusted sender %q public key (trust-on-first-use) — you can now encrypt back to them.\n", name)
	}
}

// hostnameOr returns this machine's hostname, or def if it cannot be determined.
func hostnameOr(def string) string {
	if h, err := os.Hostname(); err == nil && strings.TrimSpace(h) != "" {
		return h
	}
	return def
}

// m0 opens the credential manager, exiting on a data-dir resolution error (these
// subcommands are interactive; a resolution failure is fatal and rare).
func m0(dataDir *string) *cred.Manager {
	m, err := openCredManager(*dataDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	return m
}
