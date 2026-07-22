package selfupdate

import (
	"strings"
	"testing"
)

func TestAssertGitHubHTTPS(t *testing.T) {
	cases := []struct {
		url string
		ok  bool
	}{
		{"https://github.com/o/r/releases/download/v1/clvsync-linux-amd64", true},
		{"https://api.github.com/repos/o/r/releases/latest", true},
		{"https://objects.githubusercontent.com/github-production-release-asset/x", true},
		{"https://release-assets.githubusercontent.com/x", true},
		{"http://github.com/o/r/releases/download/v1/x", false},  // not HTTPS
		{"https://evil.com/clvsync-linux-amd64", false},           // wrong host
		{"https://github.com.evil.com/x", false},                  // suffix trick
		{"https://notgithubusercontent.com/x", false},             // suffix trick
		{"ftp://github.com/x", false},                             // wrong scheme
		{"://bad", false},                                         // unparseable
		{"https://github.com@evil.com/x", false},                  // N1: userinfo trick → host evil.com
		{"https://evil.com@github.com/x", true},                   // N1: userinfo noise, real host github.com
		{"https://GitHub.com/o/r/releases/download/v1/x", true},   // N1: case-fold
		{"https:///x", false},                                     // N1: empty host
	}
	for _, c := range cases {
		err := assertGitHubHTTPS(c.url)
		if c.ok && err != nil {
			t.Errorf("assertGitHubHTTPS(%q) = %v, want nil", c.url, err)
		}
		if !c.ok && err == nil {
			t.Errorf("assertGitHubHTTPS(%q) = nil, want error", c.url)
		}
	}
}

func TestReadCapped(t *testing.T) {
	// Exactly at the cap: allowed.
	if b, err := readCapped(strings.NewReader(strings.Repeat("A", 50)), 50); err != nil || len(b) != 50 {
		t.Fatalf("at-cap read = %d bytes, %v; want 50, nil", len(b), err)
	}
	// One byte over: refused, not truncated.
	if _, err := readCapped(strings.NewReader(strings.Repeat("A", 51)), 50); err == nil {
		t.Fatalf("over-cap read should refuse")
	}
	// Under the cap: fine.
	if b, err := readCapped(strings.NewReader("hello"), 1024); err != nil || string(b) != "hello" {
		t.Fatalf("under-cap read = %q, %v", b, err)
	}
	if maxSumsBytes >= maxBinaryBytes {
		t.Fatalf("sums cap should be far below binary cap")
	}
}

func TestChecksumFor(t *testing.T) {
	sums := "aa11  clvsync-linux-amd64\nbb22 *clvsync-windows-amd64.exe\n"
	got, err := checksumFor(sums, "clvsync-windows-amd64.exe")
	if err != nil || got != "bb22" {
		t.Fatalf("checksumFor windows = %q, %v; want bb22", got, err)
	}
	if _, err := checksumFor(sums, "clvsync-darwin-arm64"); err == nil {
		t.Fatalf("expected error for missing entry")
	}
}
