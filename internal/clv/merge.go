package clv

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Mode selects how an import resolves an already-present persona (§17.3).
type Mode string

const (
	// ModeSync (default) creates if absent, else component-merges: portable
	// definition fields update, machine-local fields are preserved (§17.1).
	ModeSync Mode = "sync"
	// ModeOverwrite replaces the destination entry/files wholesale.
	ModeOverwrite Mode = "overwrite"
	// ModeSkip leaves an existing persona untouched.
	ModeSkip Mode = "skip"
)

// ParseMode validates a mode string, defaulting to ModeSync for "".
func ParseMode(s string) (Mode, error) {
	switch Mode(s) {
	case "", ModeSync:
		return ModeSync, nil
	case ModeOverwrite:
		return ModeOverwrite, nil
	case ModeSkip:
		return ModeSkip, nil
	}
	return "", fmt.Errorf("unknown mode %q (want sync|overwrite|skip)", s)
}

// PortableFields are copied from the incoming definition on a sync-merge. Everything
// else in the destination entry — including machine-local runtime — is preserved
// (§17.1, D12). Keeping the preserve set open-ended (preserve-unless-portable) means
// a field this build doesn't know about can never be clobbered by a round-trip.
var PortableFields = map[string]bool{
	"name":              true,
	"jobDescription":    true,
	"knowledgeTemplate": true,
	"interactionMode":   true,
	"type":              true,
	"wiggumMode":        true,
}

// MachineLocalFields is the explicit machine-local set (documentation + receipt
// reporting). These are never taken from an incoming package on a sync-merge.
var MachineLocalFields = []string{
	"ai", "model", "runtime", "shell", "status", "isDefault", "activity", "createdAt",
}

// MergeDefinition merges an incoming staff entry into an existing one under the
// portable-vs-machine-local rule: start from the destination entry, overwrite only
// the portable keys with incoming values. Returns the merged JSON plus which portable
// fields changed and which machine-local fields were preserved (differed but kept).
func MergeDefinition(existing, incoming json.RawMessage) (merged []byte, changed, preserved []string, err error) {
	var dst, src map[string]json.RawMessage
	if err = json.Unmarshal(existing, &dst); err != nil {
		return nil, nil, nil, fmt.Errorf("parse destination entry: %w", err)
	}
	if dst == nil { // null local entry → nil map; the dst[k] portable-copy below would panic (D-L1)
		return nil, nil, nil, fmt.Errorf("destination staff entry is null")
	}
	if err = json.Unmarshal(incoming, &src); err != nil {
		return nil, nil, nil, fmt.Errorf("parse incoming entry: %w", err)
	}
	for k, v := range src {
		if !PortableFields[k] {
			continue
		}
		if old, ok := dst[k]; !ok || string(old) != string(v) {
			changed = append(changed, k)
		}
		dst[k] = v
	}
	for _, k := range MachineLocalFields {
		dv, dok := dst[k]
		sv, sok := src[k]
		if dok && sok && string(dv) != string(sv) {
			preserved = append(preserved, k) // destination value kept over a differing incoming one
		}
	}
	sort.Strings(changed)
	sort.Strings(preserved)
	merged, err = json.Marshal(dst)
	return merged, changed, preserved, err
}

// permissionModeStandard is the trust level a newly created persona lands at.
// permissionMode is a MACHINE-LOCAL trust grant — the right to auto-run tools without
// prompting is never carried in by a package (D18). A brand-new persona is created at
// "standard" (skip-permissions stripped); an existing persona keeps THIS machine's
// prior grant no matter what the incoming package held. The user re-grants locally.
const permissionModeStandard = "standard"

// entryPermissionMode reads the effective (ai.permissionMode) trust value from a staff
// entry, returning "" if absent.
func entryPermissionMode(entry json.RawMessage) string {
	var m struct {
		AI struct {
			PermissionMode string `json:"permissionMode"`
		} `json:"ai"`
	}
	if json.Unmarshal(entry, &m) != nil {
		return ""
	}
	return m.AI.PermissionMode
}

// applyLocalTrust returns entry with its permissionMode forced to mode, preserving all
// other fields. It sets ai.permissionMode (the authoritative runtime value) and, if a
// top-level permissionMode is present as a non-null string, normalises it too so a
// crafted package can't slip trust in through the legacy field.
func applyLocalTrust(entry json.RawMessage, mode string) (json.RawMessage, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(entry, &top); err != nil {
		return nil, err
	}
	if top == nil { // entry == JSON null → nil map; the top["ai"] assignment below would panic (D-M2)
		return nil, fmt.Errorf("staff entry is null")
	}
	ai := map[string]json.RawMessage{}
	if raw, ok := top["ai"]; ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &ai); err != nil {
			return nil, err
		}
	}
	if ai == nil { // "ai": null unmarshals a map to nil (D-M1) — assigning to it would panic
		ai = map[string]json.RawMessage{}
	}
	pm, _ := json.Marshal(mode)
	ai["permissionMode"] = pm
	aiRaw, err := json.Marshal(ai)
	if err != nil {
		return nil, err
	}
	top["ai"] = aiRaw
	if raw, ok := top["permissionMode"]; ok && string(raw) != "null" {
		top["permissionMode"] = pm
	}
	return json.Marshal(top)
}

// entryField extracts a top-level string field from a staff entry (best-effort).
func entryField(entry json.RawMessage, key string) string {
	var m map[string]json.RawMessage
	if json.Unmarshal(entry, &m) != nil {
		return ""
	}
	raw, ok := m[key]
	if !ok {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return ""
}

// MergeStaffEntry applies an incoming entry to a profile's staff.json under mode,
// backing up the file first (S7). Returns a human-readable action plus the merge
// detail (for sync). When dryRun is set nothing is written.
//
//	action ∈ {"created","merged","overwritten","skipped"}
func (in *Instance) MergeStaffEntry(profile string, incoming json.RawMessage, id string, mode Mode, dryRun bool) (action string, changed, preserved []string, trustNote string, err error) {
	p := filepath.Join(in.DataDir, "profiles", profile, "staff.json")
	b, err := os.ReadFile(p)
	if err != nil {
		return "", nil, nil, "", err
	}
	arr, err := parseStaffArray(b)
	if err != nil {
		return "", nil, nil, "", err
	}

	idx := -1
	for i, raw := range arr {
		if entryField(raw, "id") == id {
			idx = i
			break
		}
	}

	switch {
	case idx < 0:
		// D18 create: trust never crosses machines — force to standard.
		action = "created"
		hadPM := entryPermissionMode(incoming)
		if incoming, err = applyLocalTrust(incoming, permissionModeStandard); err != nil {
			return "", nil, nil, "", err
		}
		if hadPM != "" && hadPM != permissionModeStandard {
			trustNote = fmt.Sprintf("permissionMode reset to %q on create (package carried %q; local re-grant required)", permissionModeStandard, hadPM)
		}
		arr = append(arr, incoming)
	case mode == ModeSkip:
		return "skipped", nil, nil, "", nil
	case mode == ModeOverwrite:
		// D18 overwrite: wholesale replace, but keep THIS machine's trust grant.
		action = "overwritten"
		destPM := entryPermissionMode(arr[idx])
		if destPM == "" {
			destPM = permissionModeStandard
		}
		if incoming, err = applyLocalTrust(incoming, destPM); err != nil {
			return "", nil, nil, "", err
		}
		trustNote = fmt.Sprintf("permissionMode preserved as %q on overwrite (incoming trust ignored)", destPM)
		arr[idx] = incoming
	default: // ModeSync — MergeDefinition preserves the destination's ai (incl. permissionMode).
		action = "merged"
		m, ch, pre, mErr := MergeDefinition(arr[idx], incoming)
		if mErr != nil {
			return "", nil, nil, "", mErr
		}
		arr[idx], changed, preserved = json.RawMessage(m), ch, pre
	}

	if dryRun {
		return action, changed, preserved, trustNote, nil
	}
	if err := os.WriteFile(p+".clvsync-bak", b, 0o644); err != nil { // S7
		return "", nil, nil, "", err
	}
	nb, err := json.MarshalIndent(arr, "", "  ")
	if err != nil {
		return "", nil, nil, "", err
	}
	return action, changed, preserved, trustNote, os.WriteFile(p, nb, 0o644)
}

// historyStamp pulls a best-effort "last updated" marker from a history document.
// Clairvoyance transcripts vary; we probe common fields and fall back to message count.
func historyStamp(b []byte) (stamp string, msgCount int) {
	var doc struct {
		SavedAt   string            `json:"savedAt"`
		UpdatedAt string            `json:"updatedAt"`
		Messages  []json.RawMessage `json:"messages"`
	}
	if json.Unmarshal(b, &doc) != nil {
		return "", 0
	}
	stamp = doc.SavedAt
	if stamp == "" {
		stamp = doc.UpdatedAt
	}
	return stamp, len(doc.Messages)
}

// HistoryDecision decides whether to take an incoming transcript over the existing
// one (D13: newest-wins by savedAt, else more-messages-wins), and whether the two
// have diverged (both non-empty with different content) so the caller can warn/back up.
func HistoryDecision(existing, incoming []byte) (takeIncoming, diverged bool) {
	if len(existing) == 0 {
		return true, false
	}
	if string(existing) == string(incoming) {
		return false, false // identical: keep existing, no divergence
	}
	diverged = true
	es, ec := historyStamp(existing)
	is, ic := historyStamp(incoming)
	switch {
	case is != "" && es != "" && is != es:
		return is > es, diverged // ISO-8601 sorts lexicographically
	case ic != ec:
		return ic > ec, diverged // more messages wins
	default:
		return true, diverged // tie-break: prefer incoming (explicit sync intent)
	}
}
