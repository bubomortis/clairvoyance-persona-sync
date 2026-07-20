package clv

import "strings"

// OperatorTemplate is the reserved knowledgeTemplate marker that identifies the
// machine-local Sync Operator persona (S15, §19.3). It is authoritative — a renamed
// operator is still recognized. OperatorName is a secondary, belt-and-suspenders match.
const (
	OperatorTemplate = "Sync Operator"
	OperatorName     = "Sync Operator"
)

// IsOperatorMarker reports whether a template/name pair identifies the Sync Operator.
func IsOperatorMarker(template, name string) bool {
	return strings.EqualFold(strings.TrimSpace(template), OperatorTemplate) ||
		strings.EqualFold(strings.TrimSpace(name), OperatorName)
}

// IsOperator reports whether a persona is the Sync Operator.
func (p *Persona) IsOperator() bool { return IsOperatorMarker(p.Template, p.Name) }

// OperatorIDs returns the staff ids of every Sync Operator persona on this instance.
func (in *Instance) OperatorIDs() map[string]bool {
	ids := map[string]bool{}
	for _, p := range in.AllPersonas() {
		if p.IsOperator() {
			ids[p.ID] = true
		}
	}
	return ids
}
