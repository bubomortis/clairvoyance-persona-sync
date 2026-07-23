package clv

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func loadStaffNames(t *testing.T, dataDir string) StaffNames {
	t.Helper()
	b, err := os.ReadFile(staffNamesPath(dataDir))
	if err != nil {
		t.Fatalf("read staff-names.json: %v", err)
	}
	var sn StaffNames
	if err := json.Unmarshal(b, &sn); err != nil {
		t.Fatalf("parse staff-names.json: %v", err)
	}
	return sn
}

func TestReserveStaffName_FreshAndIdempotent(t *testing.T) {
	dir := t.TempDir()

	// Fresh: no file yet → creates it and reserves the name.
	got, err := ReserveStaffName(dir, "Reegor", 1000)
	if err != nil || !got {
		t.Fatalf("first reserve: got=%v err=%v, want true/nil", got, err)
	}
	sn := loadStaffNames(t, dir)
	if sn.Version != 1 || len(sn.Names) != 1 {
		t.Fatalf("want version 1 + 1 name, got version %d + %d names", sn.Version, len(sn.Names))
	}
	e := sn.Names[0]
	if e.Name != "Reegor" || !e.Active || e.AssignedAt != 1000 || e.LastUsedAt != 1000 {
		t.Fatalf("unexpected entry: %+v", e)
	}

	// Idempotent: same name → no new entry.
	if got, _ := ReserveStaffName(dir, "Reegor", 2000); got {
		t.Fatal("reserving the same name again should not add a duplicate")
	}
	// Case-insensitive: a different case is still the same name.
	if got, _ := ReserveStaffName(dir, "reegor", 3000); got {
		t.Fatal("case-insensitive match should not add a duplicate")
	}
	if sn := loadStaffNames(t, dir); len(sn.Names) != 1 {
		t.Fatalf("want still 1 name after dedup, got %d", len(sn.Names))
	}

	// A genuinely new name appends.
	if got, _ := ReserveStaffName(dir, "Archivist", 4000); !got {
		t.Fatal("a new name should be reserved")
	}
	if sn := loadStaffNames(t, dir); len(sn.Names) != 2 {
		t.Fatalf("want 2 names, got %d", len(sn.Names))
	}
}

func TestReserveStaffName_PreservesRetiredEntries(t *testing.T) {
	dir := t.TempDir()
	p := staffNamesPath(dir)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	// Pre-seed a retired (active:false) name the app deliberately keeps for bring-back.
	seed := `{"version":1,"names":[{"name":"Quinn","assignedAt":10,"active":false,"lastUsedAt":20}]}`
	if err := os.WriteFile(p, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	// Reserving the retired name must NOT re-activate or duplicate it (don't corrupt the
	// app's bring-back semantics).
	if got, _ := ReserveStaffName(dir, "Quinn", 5000); got {
		t.Fatal("must not re-add or reactivate a retired name")
	}
	sn := loadStaffNames(t, dir)
	if len(sn.Names) != 1 || sn.Names[0].Active {
		t.Fatalf("retired entry must be left untouched, got %+v", sn.Names)
	}

	// A new name still appends alongside the retired one.
	if got, _ := ReserveStaffName(dir, "Newbie", 6000); !got {
		t.Fatal("a new name should be reserved")
	}
	if sn := loadStaffNames(t, dir); len(sn.Names) != 2 || sn.Names[0].Active {
		t.Fatalf("retired entry must remain retired after append, got %+v", sn.Names)
	}
}

func TestReserveStaffName_DoesNotClobberUnreadable(t *testing.T) {
	dir := t.TempDir()
	p := staffNamesPath(dir)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	garbage := []byte("{ this is not valid json ")
	if err := os.WriteFile(p, garbage, 0o644); err != nil {
		t.Fatal(err)
	}
	// Present but unparseable → skip silently, do not overwrite.
	if got, err := ReserveStaffName(dir, "Reegor", 7000); got || err != nil {
		t.Fatalf("unreadable registry: got=%v err=%v, want false/nil", got, err)
	}
	b, _ := os.ReadFile(p)
	if string(b) != string(garbage) {
		t.Fatal("must not clobber an unparseable app-owned registry")
	}
}

func TestReserveStaffName_SkipsUnknownSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	p := staffNamesPath(dir)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	// A newer schema than we model (version 2) — must be left completely alone rather than
	// rewritten with a shape we don't understand.
	seed := `{"version":2,"names":[{"name":"Quinn"}],"newField":"keepme"}`
	if err := os.WriteFile(p, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := ReserveStaffName(dir, "Reegor", 9000); got || err != nil {
		t.Fatalf("unknown schema version: got=%v err=%v, want false/nil", got, err)
	}
	b, _ := os.ReadFile(p)
	if string(b) != seed {
		t.Fatalf("a newer-schema registry must be left byte-identical, got: %s", b)
	}
}

func TestReserveStaffName_EmptyNameNoop(t *testing.T) {
	dir := t.TempDir()
	if got, err := ReserveStaffName(dir, "   ", 8000); got || err != nil {
		t.Fatalf("blank name: got=%v err=%v, want false/nil", got, err)
	}
	if _, err := os.Stat(staffNamesPath(dir)); !os.IsNotExist(err) {
		t.Fatal("blank name must not create the registry file")
	}
}
