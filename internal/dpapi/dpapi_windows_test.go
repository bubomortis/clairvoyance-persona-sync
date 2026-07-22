//go:build windows

package dpapi

import (
	"bytes"
	"testing"
)

func TestDPAPIRoundTrip(t *testing.T) {
	s := Sealer()
	if s.Name() != "dpapi" {
		t.Fatalf("sealer name = %q, want dpapi", s.Name())
	}
	secret := []byte("AGE-SECRET-KEY-1EXAMPLEEXAMPLEEXAMPLEEXAMPLEEXAMPLEEXAMPLE0000")
	sealed, err := s.Seal(secret)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, secret) {
		t.Fatal("sealed blob still contains the plaintext key")
	}
	got, err := s.Unseal(sealed)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, secret) {
		t.Fatalf("round-trip mismatch: got %q", got)
	}
}
