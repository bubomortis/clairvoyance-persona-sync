package datadir

import "testing"

func TestResolve_Override(t *testing.T) {
	t.Setenv("CLV_DATA_DIR", "/tmp/clv-test")
	got, err := Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/clv-test" {
		t.Fatalf("override not honored: got %q", got)
	}
}

func TestResolve_DefaultNonEmpty(t *testing.T) {
	t.Setenv("CLV_DATA_DIR", "")
	got, err := Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if got == "" {
		t.Fatal("resolved data dir is empty")
	}
}
