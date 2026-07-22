package clv

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

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

// D18: applyLocalTrust forces permissionMode while preserving every other field,
// including the rest of the ai object, and normalises a non-null legacy top-level
// permissionMode so trust can't slip in through it.
func TestApplyLocalTrust(t *testing.T) {
	in := `{"id":"staff-1","name":"X","ai":{"provider":"claude","model":"opus","permissionMode":"skip-permissions"},"permissionMode":"skip-permissions"}`
	out, err := applyLocalTrust([]byte(in), permissionModeStandard)
	if err != nil {
		t.Fatal(err)
	}
	if pm := entryPermissionMode(out); pm != permissionModeStandard {
		t.Errorf("ai.permissionMode = %q, want standard", pm)
	}
	var m map[string]json.RawMessage
	_ = json.Unmarshal(out, &m)
	if string(m["permissionMode"]) != `"standard"` {
		t.Errorf("top-level permissionMode = %s, want \"standard\"", m["permissionMode"])
	}
	var ai map[string]string
	_ = json.Unmarshal(m["ai"], &ai)
	if ai["provider"] != "claude" || ai["model"] != "opus" {
		t.Errorf("other ai fields not preserved: %v", ai)
	}
	// A null legacy top-level permissionMode is left as null (not resurrected).
	out2, _ := applyLocalTrust([]byte(`{"id":"s","ai":{"permissionMode":"skip-permissions"},"permissionMode":null}`), permissionModeStandard)
	_ = json.Unmarshal(out2, &m)
	if string(m["permissionMode"]) != "null" {
		t.Errorf("null top-level permissionMode changed to %s", m["permissionMode"])
	}
	// Entry with no ai object at all gets one.
	out3, _ := applyLocalTrust([]byte(`{"id":"s","name":"Y"}`), permissionModeStandard)
	if entryPermissionMode(out3) != permissionModeStandard {
		t.Error("missing ai object should be created with standard trust")
	}
}

// D18: create strips incoming trust to standard; overwrite keeps THIS machine's grant;
// sync-merge preserves the destination ai (and thus its permissionMode).
func TestMergeStaffEntry_TrustPolicy(t *testing.T) {
	writeStaff := func(t *testing.T, entries string) *Instance {
		dir := t.TempDir()
		prof := filepath.Join(dir, "profiles", "p")
		if err := os.MkdirAll(prof, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(prof, "staff.json"), []byte(entries), 0o644); err != nil {
			t.Fatal(err)
		}
		return &Instance{DataDir: dir}
	}
	readPM := func(t *testing.T, in *Instance, id string) string {
		b, _ := os.ReadFile(filepath.Join(in.DataDir, "profiles", "p", "staff.json"))
		arr, _ := parseStaffArray(b)
		for _, e := range arr {
			if entryField(e, "id") == id {
				return entryPermissionMode(e)
			}
		}
		return "<absent>"
	}

	incoming := json.RawMessage(`{"id":"staff-new","name":"Imp","ai":{"model":"opus","permissionMode":"skip-permissions"}}`)

	// CREATE: not present → stripped to standard, note surfaced.
	in := writeStaff(t, `[]`)
	act, _, _, note, err := in.MergeStaffEntry("p", incoming, "staff-new", ModeSync, false)
	if err != nil || act != "created" {
		t.Fatalf("create: act=%q err=%v", act, err)
	}
	if pm := readPM(t, in, "staff-new"); pm != permissionModeStandard {
		t.Errorf("create left permissionMode=%q, want standard", pm)
	}
	if note == "" {
		t.Error("create should surface a trust note when stripping skip-permissions")
	}

	// OVERWRITE: destination has skip-permissions → local grant preserved despite incoming.
	in = writeStaff(t, `[{"id":"staff-new","name":"Old","ai":{"model":"sonnet","permissionMode":"skip-permissions"}}]`)
	act, _, _, note, _ = in.MergeStaffEntry("p", incoming, "staff-new", ModeOverwrite, false)
	if act != "overwritten" {
		t.Fatalf("overwrite: act=%q", act)
	}
	if pm := readPM(t, in, "staff-new"); pm != "skip-permissions" {
		t.Errorf("overwrite changed local trust to %q, want skip-permissions preserved", pm)
	}
	if note == "" {
		t.Error("overwrite should surface a preserved-trust note")
	}

	// SYNC-MERGE: destination standard, incoming skip-permissions → destination ai preserved.
	in = writeStaff(t, `[{"id":"staff-new","name":"Old","ai":{"model":"sonnet","permissionMode":"standard"}}]`)
	act, _, _, _, _ = in.MergeStaffEntry("p", incoming, "staff-new", ModeSync, false)
	if act != "merged" {
		t.Fatalf("sync: act=%q", act)
	}
	if pm := readPM(t, in, "staff-new"); pm != permissionModeStandard {
		t.Errorf("sync-merge changed permissionMode to %q, want standard preserved", pm)
	}
}
