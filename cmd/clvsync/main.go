// Command clvsync is the Clairvoyance Persona & Workspace Sync CLI.
//
// Secrets (encryption passphrase, signing-key password) are read from environment
// variables, never flags, so they don't appear in the process list.
package main

import (
	"crypto/rand"
	"flag"
	"fmt"
	"os"

	"aead.dev/minisign"

	"github.com/bubomortis/clairvoyance-persona-sync/internal/clv"
	"github.com/bubomortis/clairvoyance-persona-sync/internal/cryptobox"
	"github.com/bubomortis/clairvoyance-persona-sync/internal/datadir"
	"github.com/bubomortis/clairvoyance-persona-sync/internal/export"
	"github.com/bubomortis/clairvoyance-persona-sync/internal/importer"
)

const usage = `clvsync — Clairvoyance Persona & Workspace Sync

Commands:
  export   Export a persona to a package (Tier 1)
  import   Import a package into this instance
  keygen   Generate a minisign signing keypair
  datadir  Print the resolved Clairvoyance data directory

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
	case "keygen":
		err = cmdKeygen(os.Args[2:])
	case "export":
		err = cmdExport(os.Args[2:])
	case "import":
		err = cmdImport(os.Args[2:])
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
	persona := fs.String("persona", "", "persona name or staff id (required)")
	out := fs.String("out", "", "output package path (required)")
	tier := fs.Int("tier", 1, "1 = portable persona; 2 = + Universal Resume (cross-model session resume)")
	recipient := fs.String("recipient", "", "age X25519 recipient public key (encrypt to key)")
	signKey := fs.String("sign-key", "", "minisign private key file to sign with")
	allowSecrets := fs.Bool("allow-secrets", false, "override the secret-scan block")
	dataDir := fs.String("data-dir", "", "override Clairvoyance data dir")
	fs.Parse(args)
	if *persona == "" || *out == "" {
		return fmt.Errorf("--persona and --out are required")
	}
	in, err := resolveDataDir(*dataDir)
	if err != nil {
		return err
	}
	p, err := in.FindPersona(*persona)
	if err != nil {
		return err
	}
	opts := export.Options{Tier: *tier, Recipient: *recipient, AllowSecrets: *allowSecrets, Passphrase: os.Getenv("CLVSYNC_PASSPHRASE")}
	if *signKey != "" {
		priv, err := loadPrivateKey(*signKey)
		if err != nil {
			return err
		}
		opts.SignKey = &priv
	}
	res, err := export.Persona(in, p, *out, opts)
	if err != nil {
		if len(res.SecretHits) > 0 {
			fmt.Fprintf(os.Stderr, "secret-scan found %d match(es):\n", len(res.SecretHits))
			for _, h := range res.SecretHits {
				fmt.Fprintf(os.Stderr, "  %s:%d  %s\n", h.Path, h.Line, h.Pattern)
			}
		}
		return err
	}
	fmt.Printf("exported %s -> %s (encrypted=%v)\n", p.Name, res.PackagePath, res.Encrypted)
	if res.SigPath != "" {
		fmt.Printf("signature: %s\n", res.SigPath)
	}
	return nil
}

func cmdImport(args []string) error {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	in := fs.String("in", "", "package path (required)")
	identity := fs.String("identity", "", "age X25519 identity file (decrypt with key)")
	verifyKey := fs.String("verify-key", "", "minisign public key file to verify the signature")
	sigFile := fs.String("sig", "", "detached signature file (.minisig)")
	force := fs.Bool("force", false, "overwrite on staff-id collision")
	dataDir := fs.String("data-dir", "", "override Clairvoyance data dir")
	fs.Parse(args)
	if *in == "" {
		return fmt.Errorf("--in is required")
	}
	inst, err := resolveDataDir(*dataDir)
	if err != nil {
		return err
	}
	opts := importer.Options{Force: *force, Passphrase: os.Getenv("CLVSYNC_PASSPHRASE")}
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
	fmt.Printf("imported %s (%s), tier %d\n", rep.PersonaName, rep.PersonaID, rep.Tier)
	fmt.Printf("  placed: %v\n", rep.Placed)
	if len(rep.SkippedScopes) > 0 {
		fmt.Printf("  skipped scopes (no matching workspace on target): %v\n", rep.SkippedScopes)
	}
	fmt.Printf("  note: %s\n", rep.ReviewNote)
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
