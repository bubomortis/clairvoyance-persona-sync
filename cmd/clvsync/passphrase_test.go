package main

import "testing"

// D8 / §20.2: export must never silently ship plaintext.
func TestResolveExportEncryption(t *testing.T) {
	cases := []struct {
		name      string
		envPass   string
		recipient string
		plaintext bool
		isTTY     bool
		want      exportEncMode
	}{
		{"env passphrase encrypts", "s3cret", "", false, false, encFromInputs},
		{"recipient encrypts", "", "age1xyz", false, false, encFromInputs},
		{"env wins even with --plaintext", "s3cret", "", true, false, encFromInputs},
		{"explicit plaintext opt-in", "", "", true, false, encPlaintext},
		{"interactive prompts", "", "", false, true, encPrompt},
		{"non-interactive, nothing given, refused", "", "", false, false, encRefuse},
	}
	for _, c := range cases {
		if got := resolveExportEncryption(c.envPass, c.recipient, c.plaintext, c.isTTY); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}
