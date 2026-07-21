package clv

import (
	"encoding/json"
	"sort"
)

// KnownDefinitionFields is the set of staff.json keys clvsync recognizes: the
// portable fields (D12), the machine-local fields, and the identity. It backs the
// S4 review advisory — a key outside this set in an imported definition is surfaced
// for a human glance before the persona is relied on.
//
// clvsync never EXECUTES a definition (it is inert data), but an imported entry
// becomes agent-loaded configuration, so an unrecognized field is worth review.
// The set is intentionally the documented core, so genuinely novel keys stand out;
// the check only warns, it never blocks (imports are already quarantined, S5).
func KnownDefinitionFields() map[string]bool {
	known := map[string]bool{"id": true}
	for k := range PortableFields {
		known[k] = true
	}
	for _, k := range MachineLocalFields {
		known[k] = true
	}
	return known
}

// UnknownDefinitionFields returns the sorted top-level keys of an imported staff
// entry that clvsync does not recognize (audit S4). Returns nil if the entry is
// unparseable or carries only known fields.
func UnknownDefinitionFields(entry json.RawMessage) []string {
	var m map[string]json.RawMessage
	if json.Unmarshal(entry, &m) != nil {
		return nil
	}
	known := KnownDefinitionFields()
	var out []string
	for k := range m {
		if !known[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
