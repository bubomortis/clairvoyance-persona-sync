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

// S4: an imported definition carrying keys clvsync doesn't recognize must be
// surfaced for review; a definition of only known fields must not warn.
func TestUnknownDefinitionFields(t *testing.T) {
	known := `{"id":"staff-1","name":"Reegor","jobDescription":"eng","model":"claude","runtime":"x","createdAt":"t"}`
	if got := UnknownDefinitionFields([]byte(known)); len(got) != 0 {
		t.Errorf("known-only definition flagged: %v", got)
	}
	hostile := `{"id":"staff-1","name":"Reegor","onLoad":"rm -rf","evilHook":true}`
	got := UnknownDefinitionFields([]byte(hostile))
	if len(got) != 2 || got[0] != "evilHook" || got[1] != "onLoad" {
		t.Errorf("expected sorted [evilHook onLoad], got %v", got)
	}
	if UnknownDefinitionFields([]byte("not json")) != nil {
		t.Error("unparseable entry should return nil")
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
