// Package cryptobox wraps age encryption (§7) and minisign signing (§7/S8).
//
// Encryption is age (ChaCha20-Poly1305): passphrase (scrypt) or recipient
// public-key (X25519) mode. Signing is minisign detached signatures.
package cryptobox

import (
	"crypto/rand"
	"io"

	"aead.dev/minisign"
	"filippo.io/age"
)

// EncryptPassphrase encrypts src to dst under a scrypt passphrase recipient.
func EncryptPassphrase(dst io.Writer, src io.Reader, passphrase string) error {
	r, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return err
	}
	w, err := age.Encrypt(dst, r)
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, src); err != nil {
		return err
	}
	return w.Close()
}

// DecryptPassphrase decrypts an age scrypt stream.
func DecryptPassphrase(src io.Reader, passphrase string) (io.Reader, error) {
	id, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return nil, err
	}
	return age.Decrypt(src, id)
}

// EncryptRecipient encrypts src to dst for an age X25519 public key ("age1...").
func EncryptRecipient(dst io.Writer, src io.Reader, recipient string) error {
	r, err := age.ParseX25519Recipient(recipient)
	if err != nil {
		return err
	}
	w, err := age.Encrypt(dst, r)
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, src); err != nil {
		return err
	}
	return w.Close()
}

// DecryptIdentity decrypts an age stream with an X25519 identity ("AGE-SECRET-KEY-...").
func DecryptIdentity(src io.Reader, secretKey string) (io.Reader, error) {
	id, err := age.ParseX25519Identity(secretKey)
	if err != nil {
		return nil, err
	}
	return age.Decrypt(src, id)
}

// GenerateSigningKey returns a fresh minisign keypair (first-run / tests).
func GenerateSigningKey() (minisign.PublicKey, minisign.PrivateKey, error) {
	return minisign.GenerateKey(rand.Reader)
}

// Sign returns a detached minisign signature over data.
func Sign(priv minisign.PrivateKey, data []byte) []byte {
	return minisign.Sign(priv, data)
}

// Verify checks a detached minisign signature.
func Verify(pub minisign.PublicKey, data, sig []byte) bool {
	return minisign.Verify(pub, data, sig)
}
