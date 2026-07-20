package clv

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Scope tokens used in packaged session records (machine-local values are replaced
// with these and resolved against the target on import).
const (
	scopeHome = "home"
)

// WSToken / WSIDToken build the package-side tokens for a scope.
func WSToken(scope string) string   { return "<WS:" + scope + ">" }
func WSIDToken(scope string) string { return "<WSID:" + scope + ">" }

// ParseScopeToken returns the scope from a <WS:...> or <WSID:...> token.
func ParseScopeToken(v string) (string, bool) {
	if strings.HasPrefix(v, "<WS:") && strings.HasSuffix(v, ">") {
		return v[4 : len(v)-1], true
	}
	if strings.HasPrefix(v, "<WSID:") && strings.HasSuffix(v, ">") {
		return v[6 : len(v)-1], true
	}
	return "", false
}

// SessionRec is a persona's session record plus its resolved scope.
type SessionRec struct {
	SessionID string
	Scope     string
	Rec       map[string]any
}

// Resume bundles a persona's Universal Resume artifacts.
type Resume struct {
	Sessions   []SessionRec
	Summaries  []json.RawMessage // filtered session-summaries entries
	Exclusions []json.RawMessage // filtered resume-exclusions entries
}

// LoadResume gathers the persona's sessions + filtered summaries/exclusions (Tier 2).
func (in *Instance) LoadResume(p *Persona) *Resume {
	r := &Resume{}
	b, err := os.ReadFile(filepath.Join(in.DataDir, "profiles", p.Profile, "agent-sessions.json"))
	if err != nil {
		return r
	}
	var doc struct {
		Sessions []map[string]any `json:"sessions"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return r
	}
	ids := map[string]bool{}
	for _, s := range doc.Sessions {
		if fmt.Sprint(s["staffId"]) != p.ID {
			continue
		}
		sid := fmt.Sprint(s["sessionId"])
		r.Sessions = append(r.Sessions, SessionRec{SessionID: sid, Scope: in.scopeForSession(s), Rec: s})
		ids[sid] = true
	}
	r.Summaries = filterEntries(filepath.Join(in.DataDir, "profiles", p.Profile, "session-summaries.json"), ids)
	r.Exclusions = filterEntries(filepath.Join(in.DataDir, "profiles", p.Profile, "resume-exclusions.json"), ids)
	return r
}

func filterEntries(path string, ids map[string]bool) []json.RawMessage {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var doc struct {
		Entries []json.RawMessage `json:"entries"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil
	}
	var out []json.RawMessage
	for _, e := range doc.Entries {
		var probe struct {
			SessionID string `json:"sessionId"`
		}
		if json.Unmarshal(e, &probe) == nil && ids[probe.SessionID] {
			out = append(out, e)
		}
	}
	return out
}

func (in *Instance) scopeForSession(s map[string]any) string {
	wid := fmt.Sprint(s["workspaceId"])
	for _, w := range in.Workspaces {
		if w.ID == wid {
			if strings.EqualFold(w.Name, "Home") {
				return scopeHome
			}
			return w.Name
		}
	}
	wp := fmt.Sprint(s["workspacePath"])
	for _, w := range in.Workspaces {
		if strings.EqualFold(w.Path, wp) {
			if strings.EqualFold(w.Name, "Home") {
				return scopeHome
			}
			return w.Name
		}
	}
	return scopeHome
}

// HomeWorkspaceID returns the id of the workspace named "Home" (or "").
func (in *Instance) HomeWorkspaceID() string {
	for _, w := range in.Workspaces {
		if strings.EqualFold(w.Name, "Home") {
			return w.ID
		}
	}
	return ""
}

// MergeSessions merges packaged session records into the target agent-sessions.json,
// replacing any with the same sessionId (backs up first, S7).
func (in *Instance) MergeSessions(profile string, recs []map[string]any) error {
	p := filepath.Join(in.DataDir, "profiles", profile, "agent-sessions.json")
	doc := struct {
		Version  int              `json:"version"`
		Sessions []map[string]any `json:"sessions"`
	}{Version: 1}
	if b, err := os.ReadFile(p); err == nil {
		_ = json.Unmarshal(b, &doc)
		_ = os.WriteFile(p+".clvsync-bak", b, 0o644)
	}
	incoming := map[string]bool{}
	for _, r := range recs {
		incoming[fmt.Sprint(r["sessionId"])] = true
	}
	kept := doc.Sessions[:0]
	for _, s := range doc.Sessions {
		if !incoming[fmt.Sprint(s["sessionId"])] {
			kept = append(kept, s)
		}
	}
	doc.Sessions = append(kept, recs...)
	nb, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, nb, 0o644)
}

// MergeResumeEntries merges entries into a {version,entries[]} file (session-summaries
// or resume-exclusions), replacing any with the same sessionId (backs up first, S7).
func (in *Instance) MergeResumeEntries(profile, filename string, entries []json.RawMessage) error {
	if len(entries) == 0 {
		return nil
	}
	p := filepath.Join(in.DataDir, "profiles", profile, filename)
	doc := struct {
		Version int               `json:"version"`
		Entries []json.RawMessage `json:"entries"`
	}{Version: 1}
	if b, err := os.ReadFile(p); err == nil {
		_ = json.Unmarshal(b, &doc)
		_ = os.WriteFile(p+".clvsync-bak", b, 0o644)
	}
	sidOf := func(e json.RawMessage) string {
		var probe struct {
			SessionID string `json:"sessionId"`
		}
		_ = json.Unmarshal(e, &probe)
		return probe.SessionID
	}
	incoming := map[string]bool{}
	for _, e := range entries {
		incoming[sidOf(e)] = true
	}
	kept := doc.Entries[:0]
	for _, e := range doc.Entries {
		if !incoming[sidOf(e)] {
			kept = append(kept, e)
		}
	}
	doc.Entries = append(kept, entries...)
	nb, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, nb, 0o644)
}
