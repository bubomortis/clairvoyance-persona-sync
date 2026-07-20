package cryptobox

import (
	"bytes"
	"io"
	"testing"
)

func TestAge_PassphraseRoundtrip(t *testing.T) {
	plain := []byte("persona bundle contents — secret to the wire")
	var enc bytes.Buffer
	if err := EncryptPassphrase(&enc, bytes.NewReader(plain), "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(enc.Bytes(), plain) {
		t.Fatal("ciphertext contains plaintext")
	}
	r, err := DecryptPassphrase(&enc, "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("roundtrip mismatch: got %q", got)
	}
}

func TestAge_WrongPassphraseFails(t *testing.T) {
	var enc bytes.Buffer
	if err := EncryptPassphrase(&enc, bytes.NewReader([]byte("x")), "right"); err != nil {
		t.Fatal(err)
	}
	if _, err := DecryptPassphrase(&enc, "wrong"); err == nil {
		t.Fatal("expected decryption failure with wrong passphrase")
	}
}

func TestMinisign_SignVerify(t *testing.T) {
	pub, priv, err := GenerateSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("the package bytes + manifest")
	sig := Sign(priv, data)
	if !Verify(pub, data, sig) {
		t.Fatal("valid signature failed to verify")
	}
	// Tamper with the data -> must not verify.
	if Verify(pub, append(data, '!'), sig) {
		t.Fatal("tampered data verified against original signature")
	}
}
