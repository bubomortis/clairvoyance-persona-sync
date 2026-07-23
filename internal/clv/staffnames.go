package clv

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// staff-names.json is the app-owned display-name registry the in-app Create Staff modal
// reads. The Clairvoyance devs confirmed (forum, 2026-07-22) that it is NOT load-bearing
// for persona/memory discovery — nothing rebuilds it from profiles on launch, and an
// imported persona that is absent from it still loads fine. Writing it is a purely optional
// nicety: it reserves a display name so the modal knows the name is taken (and won't suggest
// it for a brand-new staff member). Confirmed shape:
//
//	{ "version": 1, "names": [ { "name", "assignedAt", "active", "lastUsedAt" } ] }
const staffNamesFile = "staff-names.json"

// StaffNameEntry is one reserved display name. assignedAt/lastUsedAt are millisecond epochs.
type StaffNameEntry struct {
	Name       string `json:"name"`
	AssignedAt int64  `json:"assignedAt"`
	Active     bool   `json:"active"`
	LastUsedAt int64  `json:"lastUsedAt"`
}

// StaffNames is the staff-names.json document.
type StaffNames struct {
	Version int              `json:"version"`
	Names   []StaffNameEntry `json:"names"`
}

// staffNamesPath resolves <dataDir>/<.Clairvoyance>/staff-names.json, reusing StaffDir's
// case resolution (prefer an existing .Clairvoyance/.clairvoyance, default to capital) so
// the registry lands in the SAME dir the app uses — critical on case-sensitive filesystems.
// staff-names.json is a sibling of the staff/ memory dir.
func staffNamesPath(dataDir string) string {
	return filepath.Join(filepath.Dir(StaffDir(dataDir)), staffNamesFile)
}

// ReserveStaffName reserves display name `name` in the app's staff-names.json, appending an
// active entry only when no entry with that name (case-insensitive) already exists. It is
// append-only and idempotent: an existing entry — active or retired — is left untouched, so
// the app's own name-recycling / bring-back semantics are never corrupted. Returns whether a
// new entry was written.
//
// It is deliberately conservative about the app-owned file: a file that exists but does not
// parse is left alone (the reservation is skipped, not forced over unreadable content); only
// an absent or cleanly-parsed file is written. nowMs is injected for testability.
func ReserveStaffName(dataDir, name string, nowMs int64) (bool, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return false, nil
	}
	p := staffNamesPath(dataDir)

	var reg StaffNames
	if b, err := os.ReadFile(p); err == nil {
		if json.Unmarshal(b, &reg) != nil {
			// Present but unreadable — do not clobber an app-owned registry we can't parse.
			return false, nil
		}
	} else if !os.IsNotExist(err) {
		return false, err
	}
	if reg.Version == 0 {
		reg.Version = 1
	}
	for _, e := range reg.Names {
		if strings.EqualFold(strings.TrimSpace(e.Name), name) {
			return false, nil // already reserved — never double-add
		}
	}
	reg.Names = append(reg.Names, StaffNameEntry{Name: name, AssignedAt: nowMs, Active: true, LastUsedAt: nowMs})

	b, err := json.MarshalIndent(&reg, "", "  ")
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		return false, err
	}
	return true, nil
}
