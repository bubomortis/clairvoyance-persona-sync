package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// exportEncMode is how an export resolves encryption when neither CLVSYNC_PASSPHRASE
// nor --recipient was supplied.
type exportEncMode int

const (
	encFromInputs exportEncMode = iota // env passphrase or --recipient present → encrypt as given
	encPrompt                          // interactive terminal → prompt for a passphrase
	encPlaintext                       // explicit --plaintext → proceed UNENCRYPTED (warn)
	encRefuse                          // non-interactive, nothing supplied, no opt-in → fail closed
)

// resolveExportEncryption decides how export handles encryption. It never returns a
// "silently ship plaintext" outcome: with no passphrase/recipient, plaintext requires
// the explicit --plaintext opt-in, an interactive terminal is prompted, and a
// non-interactive caller is refused (D8 / spec §20.2). Pure + table-testable.
func resolveExportEncryption(envPass, recipient string, plaintext, isTTY bool) exportEncMode {
	if envPass != "" || recipient != "" {
		return encFromInputs
	}
	if plaintext {
		return encPlaintext
	}
	if isTTY {
		return encPrompt
	}
	return encRefuse
}

// stdinIsTerminal reports whether stdin is an interactive terminal.
func stdinIsTerminal() bool { return term.IsTerminal(int(os.Stdin.Fd())) }

// readPassphrase prompts (to stderr) and reads a line without echoing on a terminal;
// on a non-terminal (piped input) it reads a plain line. The prompt goes to stderr so
// stdout stays clean.
func readPassphrase(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		b, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		return strings.TrimSpace(string(b)), err
	}
	s, err := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.TrimSpace(s), err
}

// readNewPassphrase prompts for a passphrase to ENCRYPT a package: read twice and
// confirm (a typo'd export passphrase yields an unrecoverable package). A blank first
// entry cancels (returns ""). A short passphrase is warned but allowed.
func readNewPassphrase() (string, error) {
	p1, err := readPassphrase("Passphrase to encrypt the package (blank to cancel): ")
	if err != nil {
		return "", err
	}
	if p1 == "" {
		return "", nil
	}
	if len(p1) < 8 {
		fmt.Fprintln(os.Stderr, "⚠ short passphrase — consider a longer, stronger one.")
	}
	p2, err := readPassphrase("Confirm passphrase: ")
	if err != nil {
		return "", err
	}
	if p1 != p2 {
		return "", fmt.Errorf("passphrases did not match")
	}
	return p1, nil
}
