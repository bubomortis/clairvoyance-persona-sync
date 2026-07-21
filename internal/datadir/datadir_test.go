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

func TestResolve_BaseUserData(t *testing.T) {
	t.Setenv("CLV_DATA_DIR", "")
	t.Setenv("CLAIRVOYANCE_BASE_USER_DATA", `D:\Clairvoyance\UserData`)
	got, err := Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if got != `D:\Clairvoyance\UserData` {
		t.Fatalf("CLAIRVOYANCE_BASE_USER_DATA not honored: got %q", got)
	}
}

func TestResolve_CLVDataDirWinsOverBaseUserData(t *testing.T) {
	t.Setenv("CLV_DATA_DIR", "/tmp/clv-explicit")
	t.Setenv("CLAIRVOYANCE_BASE_USER_DATA", `D:\Clairvoyance\UserData`)
	got, err := Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/clv-explicit" {
		t.Fatalf("CLV_DATA_DIR should win: got %q", got)
	}
}

func TestResolve_DefaultNonEmpty(t *testing.T) {
	t.Setenv("CLAIRVOYANCE_BASE_USER_DATA", "")
	t.Setenv("CLV_DATA_DIR", "")
	got, err := Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if got == "" {
		t.Fatal("resolved data dir is empty")
	}
}
