package clv

import "testing"

// P3: crafted/empty persona names must never yield a key that escapes a single
// memory folder (which would let a merge target the staff-memory root).
func TestSlug_NeutralizesPathChars(t *testing.T) {
	cases := map[string]string{
		"Reegor":    "reegor",
		"My Agent":  "my-agent",
		"../../etc": "-..-etc", // '/' -> '-', leading/trailing dots trimmed; interior is a harmless literal segment
		`a\b/c`:     "a-b-c",
		"..":        "", // pure traversal collapses to empty (then rejected)
		".":         "",
		"   ":       "",
	}
	for in, want := range cases {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidMemKey(t *testing.T) {
	good := []string{"reegor", "my-agent", "a-b-c"}
	for _, g := range good {
		if !ValidMemKey(g) {
			t.Errorf("ValidMemKey(%q) = false, want true", g)
		}
	}
	bad := []string{"", ".", "..", "a/b", `a\b`, "/"}
	for _, b := range bad {
		if ValidMemKey(b) {
			t.Errorf("ValidMemKey(%q) = true, want false", b)
		}
	}
}
