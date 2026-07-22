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
// a given user home: <home>/.claude/projects/<munge>/memory.
func AgentMemoryDir(home, cwd string) string {
	return filepath.Join(home, ".claude", "projects", AgentMemoryMunge(cwd), "memory")
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
