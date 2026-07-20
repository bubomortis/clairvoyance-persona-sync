package clv

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// AllPersonas returns every persona across all profiles (populated with file locations).
func (in *Instance) AllPersonas() []*Persona {
	var out []*Persona
	dir := filepath.Join(in.DataDir, "profiles")
	ents, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name(), "staff.json"))
		if err != nil {
			continue
		}
		arr, err := parseStaffArray(b)
		if err != nil {
			continue
		}
		for _, raw := range arr {
			var m staffMeta
			if json.Unmarshal(raw, &m) != nil {
				continue
			}
			p := &Persona{ID: m.ID, Name: m.Name, Lname: Slug(m.Name), Profile: e.Name(), Entry: raw, Template: m.KnowledgeTemplate}
			in.populate(p)
			out = append(out, p)
		}
	}
	return out
}

// PersonasInWorkspace returns personas whose memory lives in the named workspace.
func (in *Instance) PersonasInWorkspace(wsName string) []*Persona {
	ws, ok := in.WorkspaceByName(wsName)
	if !ok {
		return nil
	}
	var staffBase string
	for _, dot := range []string{".Clairvoyance", ".clairvoyance"} {
		d := filepath.Join(ws.Path, dot, "staff")
		if fi, err := os.Stat(d); err == nil && fi.IsDir() {
			staffBase = d
			break
		}
	}
	if staffBase == "" {
		return nil
	}
	lnames := map[string]bool{}
	if ents, err := os.ReadDir(staffBase); err == nil {
		for _, e := range ents {
			if e.IsDir() {
				lnames[e.Name()] = true
			}
		}
	}
	var out []*Persona
	for _, p := range in.AllPersonas() {
		if lnames[p.Lname] {
			out = append(out, p)
		}
	}
	return out
}

// MintWorkspaceID generates a fresh workspace id: workspace-<unixMillis>-<7 base36>.
func MintWorkspaceID() string {
	const chars = "0123456789abcdefghijklmnopqrstuvwxyz"
	b := make([]byte, 7)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = chars[int(b[i])%len(chars)]
	}
	return fmt.Sprintf("workspace-%d-%s", time.Now().UnixMilli(), string(b))
}

// EnsureWorkspaceMaybe is EnsureWorkspace, but in dryRun it reports what WOULD be
// created without touching disk (returns a preview workspace with a minted id).
func (in *Instance) EnsureWorkspaceMaybe(name, path string, dryRun bool) (Workspace, bool, error) {
	if ws, ok := in.WorkspaceByName(name); ok {
		return ws, false, nil
	}
	if !dryRun {
		return in.EnsureWorkspace(name, path)
	}
	if path == "" {
		return Workspace{}, false, fmt.Errorf("workspace %q not found on target and no workspace path given", name)
	}
	return Workspace{ID: MintWorkspaceID(), Name: name, Path: path}, true, nil
}

// EnsureWorkspace returns the named workspace, creating and registering it (offline,
// app-closed) at path if absent. Returns (workspace, created, error).
func (in *Instance) EnsureWorkspace(name, path string) (Workspace, bool, error) {
	if ws, ok := in.WorkspaceByName(name); ok {
		return ws, false, nil
	}
	if path == "" {
		return Workspace{}, false, fmt.Errorf("workspace %q not found on target and no --workspace-path given", name)
	}
	ws := Workspace{ID: MintWorkspaceID(), Name: name, Path: path}
	if err := os.MkdirAll(filepath.Join(path, ".Clairvoyance", "staff"), 0o755); err != nil {
		return Workspace{}, false, err
	}
	if err := os.MkdirAll(filepath.Join(path, ".Clairvoyance", "memory"), 0o755); err != nil {
		return Workspace{}, false, err
	}

	// Append to clairvoyance-store.json, preserving all other keys.
	storePath := filepath.Join(in.DataDir, "clairvoyance-store.json")
	m := map[string]any{}
	if b, err := os.ReadFile(storePath); err == nil {
		_ = json.Unmarshal(b, &m)
		_ = os.WriteFile(storePath+".clvsync-bak", b, 0o644)
	}
	wsList, _ := m["workspaces"].([]any)
	wsList = append(wsList, map[string]any{
		"id": ws.ID, "name": ws.Name, "path": ws.Path,
		"createdAt": time.Now().UnixMilli(), "basesRoot": "Bases",
	})
	m["workspaces"] = wsList
	nb, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return Workspace{}, false, err
	}
	if err := os.MkdirAll(in.DataDir, 0o755); err != nil {
		return Workspace{}, false, err
	}
	if err := os.WriteFile(storePath, nb, 0o644); err != nil {
		return Workspace{}, false, err
	}
	in.Workspaces = append(in.Workspaces, ws)
	return ws, true, nil
}
