package clv

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

// Package agentmem: locating the rich Claude Code "agent memory" working store.
//
// This is the machine-local working memory at <home>/.claude/projects/<munge>/memory,
// keyed by the WORKSPACE cwd (shared across the personas/sessions running in that cwd) —
// distinct from the curated .clairvoyance/staff memory that always travels. It moves only
// on an explicit --include-agent-memory opt-in (D19), remapped to the target's own munge.

// AgentMemoryMunge maps an absolute workspace cwd to the Claude Code project-dir key that
// names its .claude/projects subdirectory: each ':' '\\' '/' becomes '-'. For example
// C:\Users\a\clv -> C--Users-a-clv. Case is preserved (the on-disk keys are case-sensitive)
// and any trailing separators are trimmed first so "C:\ws" and "C:\ws\" map identically.
func AgentMemoryMunge(cwd string) string {
	cwd = strings.TrimRight(cwd, `\/`)
	return strings.NewReplacer(":", "-", "\\", "-", "/", "-").Replace(cwd)
}

// AgentMemoryDir returns the rich agent working-memory directory for a workspace cwd under
// a given user home: <home>/.claude/projects/<munge>/memory, and ok=false when the cwd is
// degenerate. A munge of "." or ".." has no separators to neutralize and would collapse the
// path out of projects/<munge> (e.g. cwd ".." → <home>/.claude/memory, AM-1); such a cwd is
// rejected, and as belt-and-suspenders the resolved dir is asserted to stay under
// <home>/.claude/projects so no future munge quirk can escape.
func AgentMemoryDir(home, cwd string) (string, bool) {
	m := AgentMemoryMunge(cwd)
	if m == "" || m == "." || m == ".." {
		return "", false
	}
	base := filepath.Join(home, ".claude", "projects")
	dir := filepath.Join(base, m, "memory")
	if dir != base && !strings.HasPrefix(dir, base+string(filepath.Separator)) {
		return "", false
	}
	return dir, true
}

// EntryShellCwd extracts shell.cwd from a staff entry, or "" if absent/unparseable.
func EntryShellCwd(entry json.RawMessage) string {
	var m struct {
		Shell struct {
			Cwd string `json:"cwd"`
		} `json:"shell"`
	}
	if json.Unmarshal(entry, &m) != nil {
		return ""
	}
	return m.Shell.Cwd
}
