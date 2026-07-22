// Package clv reads and mutates a Clairvoyance instance's on-disk data:
// personas (staff.json entries), workspaces, per-workspace memory, and history.
package clv

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Workspace is one registered workspace (from clairvoyance-store.json).
type Workspace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

type store struct {
	Workspaces []Workspace `json:"workspaces"`
}

// Instance is an opened Clairvoyance data directory.
type Instance struct {
	DataDir    string
	Workspaces []Workspace
	// AgentHome is the user home under which the rich .claude/projects agent-memory
	// store lives (defaults to the OS user home; overridable for tests).
	AgentHome string
}

// Open loads the workspace registry for a data dir.
func Open(dataDir string) (*Instance, error) {
	in := &Instance{DataDir: dataDir}
	in.AgentHome, _ = os.UserHomeDir()
	if b, err := os.ReadFile(filepath.Join(dataDir, "clairvoyance-store.json")); err == nil {
		var s store
		if err := json.Unmarshal(b, &s); err == nil {
			in.Workspaces = s.Workspaces
		}
	}
	return in, nil
}

type staffMeta struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	KnowledgeTemplate string `json:"knowledgeTemplate"`
}

// MemLoc is a memory directory for a persona in a given scope ("home" or a workspace name).
type MemLoc struct {
	Scope string
	Dir   string
}

// Persona is a discovered Staff member and its file locations.
type Persona struct {
	ID       string
	Name     string
	Lname    string // memory-folder key (lowercase, spaces->hyphens)
	Profile  string // profileId (dir under profiles/)
	Entry    json.RawMessage
	Template string
	Memory   []MemLoc
	History  string
}

// Slug converts a display name to its memory-folder key. Path separators are
// neutralized so a crafted persona name can never escape a single folder segment.
func Slug(name string) string {
	s := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(name)), " ", "-")
	s = strings.NewReplacer("/", "-", "\\", "-").Replace(s)
	return strings.Trim(s, ".") // never leave a leading/trailing dot ("." / "..")
}

// ValidMemKey reports whether lname is a safe single-segment memory-folder key.
// Empty or path-bearing keys are rejected because they would let a merge target
// the staff-memory ROOT (wiping every persona's memory) instead of one persona.
func ValidMemKey(lname string) bool {
	if lname == "" || lname == "." || lname == ".." {
		return false
	}
	return !strings.ContainsAny(lname, `/\`)
}

func parseStaffArray(b []byte) ([]json.RawMessage, error) {
	var arr []json.RawMessage
	if err := json.Unmarshal(b, &arr); err == nil {
		return arr, nil
	}
	var obj struct {
		Staff []json.RawMessage `json:"staff"`
	}
	if err := json.Unmarshal(b, &obj); err == nil && obj.Staff != nil {
		return obj.Staff, nil
	}
	return nil, fmt.Errorf("unrecognized staff.json shape")
}

// FindPersona locates a persona by display name (case-insensitive) or staff id.
func (in *Instance) FindPersona(nameOrID string) (*Persona, error) {
	profilesDir := filepath.Join(in.DataDir, "profiles")
	ents, err := os.ReadDir(profilesDir)
	if err != nil {
		return nil, err
	}
	needle := strings.ToLower(nameOrID)
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(profilesDir, e.Name(), "staff.json"))
		if err != nil {
			continue
		}
		arr, err := parseStaffArray(b)
		if err != nil {
			continue
		}
		for _, raw := range arr {
			var m staffMeta
			if err := json.Unmarshal(raw, &m); err != nil {
				continue
			}
			if strings.ToLower(m.ID) == needle || strings.ToLower(m.Name) == needle {
				p := &Persona{ID: m.ID, Name: m.Name, Lname: Slug(m.Name), Profile: e.Name(), Entry: raw, Template: m.KnowledgeTemplate}
				in.populate(p)
				return p, nil
			}
		}
	}
	return nil, fmt.Errorf("persona %q not found", nameOrID)
}

func (in *Instance) populate(p *Persona) {
	if h := filepath.Join(in.DataDir, "profiles", p.Profile, "agent-history", p.ID+".json"); fileExists(h) {
		p.History = h
	}
	if d := staffMemDir(in.DataDir, p.Lname); d != "" {
		p.Memory = append(p.Memory, MemLoc{Scope: "home", Dir: d})
	}
	for _, ws := range in.Workspaces {
		if strings.EqualFold(ws.Name, "Home") {
			continue // Home == datadir, already covered
		}
		if d := staffMemDir(ws.Path, p.Lname); d != "" {
			p.Memory = append(p.Memory, MemLoc{Scope: ws.Name, Dir: d})
		}
	}
}

func staffMemDir(base, lname string) string {
	for _, dot := range []string{".Clairvoyance", ".clairvoyance"} {
		d := filepath.Join(base, dot, "staff", lname)
		if fi, err := os.Stat(d); err == nil && fi.IsDir() {
			return d
		}
	}
	return ""
}

// TemplatePath returns the custom persona template file if present.
func (in *Instance) TemplatePath(name string) (string, bool) {
	if name == "" {
		return "", false
	}
	p := filepath.Join(in.DataDir, "neurons", "personas", name)
	if fileExists(p) {
		return p, true
	}
	return "", false
}

// WorkspaceByName returns the workspace with the given name.
func (in *Instance) WorkspaceByName(name string) (Workspace, bool) {
	for _, w := range in.Workspaces {
		if strings.EqualFold(w.Name, name) {
			return w, true
		}
	}
	return Workspace{}, false
}

// DefaultProfile returns a profile dir holding a staff.json, creating one if none exists.
func (in *Instance) DefaultProfile() (string, error) {
	dir := filepath.Join(in.DataDir, "profiles")
	if ents, err := os.ReadDir(dir); err == nil {
		for _, e := range ents {
			if e.IsDir() && fileExists(filepath.Join(dir, e.Name(), "staff.json")) {
				return e.Name(), nil
			}
		}
	}
	id := "profile-clvsync"
	if err := os.MkdirAll(filepath.Join(dir, id), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, id, "staff.json"), []byte("[]\n"), 0o644); err != nil {
		return "", err
	}
	return id, nil
}

// StaffDir returns the base staff-memory dir under a location (default ".Clairvoyance").
func StaffDir(base string) string {
	for _, dot := range []string{".Clairvoyance", ".clairvoyance"} {
		if fi, err := os.Stat(filepath.Join(base, dot)); err == nil && fi.IsDir() {
			return filepath.Join(base, dot, "staff")
		}
	}
	return filepath.Join(base, ".Clairvoyance", "staff") // default for creation
}

// SpliceStaffEntry inserts entry into a profile's staff.json (backing up first).
// If an entry with the same id exists: replaced when force, else an error.
func (in *Instance) SpliceStaffEntry(profile string, entry json.RawMessage, id string, force bool) error {
	p := filepath.Join(in.DataDir, "profiles", profile, "staff.json")
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	arr, err := parseStaffArray(b)
	if err != nil {
		return err
	}
	_ = os.WriteFile(p+".clvsync-bak", b, 0o644) // S7: back up before mutate
	out := arr[:0]
	for _, raw := range arr {
		var m staffMeta
		_ = json.Unmarshal(raw, &m)
		if m.ID == id {
			if !force {
				return fmt.Errorf("staff id %q already present", id)
			}
			continue
		}
		out = append(out, raw)
	}
	out = append(out, entry)
	nb, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, nb, 0o644)
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
