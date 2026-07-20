package scan

import "testing"

func TestScan_PlantedSecrets(t *testing.T) {
	s, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"anthropic": "here is my key sk-ant-abcdef0123456789ABCDEF more",
		"github-pat": `token: github_pat_11ABCDEFGHIJ0123456789_abcdefghij`,
		"aws":       "AKIAIOSFODNN7EXAMPLE",
		"json-secret": `{"client_secret":"abcdef012345678"}`,
		"pem":       "-----BEGIN RSA PRIVATE KEY-----",
	}
	for name, body := range cases {
		if m := s.Bytes(name, []byte(body)); len(m) == 0 {
			t.Errorf("%s: expected a secret match, got none", name)
		}
	}
}

func TestScan_CleanPasses(t *testing.T) {
	s, _ := New(nil)
	clean := "This is an ordinary note about the backup system. No secrets here."
	if m := s.Bytes("note", []byte(clean)); len(m) != 0 {
		t.Errorf("clean text flagged: %+v", m)
	}
}

func TestScan_BinarySkipped(t *testing.T) {
	s, _ := New(nil)
	// Contains a NUL byte -> treated as binary, not scanned.
	bin := []byte("sk-ant-abcdef0123456789ABCDEF\x00\x01\x02")
	if m := s.Bytes("blob", bin); len(m) != 0 {
		t.Errorf("binary content should be skipped, got %+v", m)
	}
}
