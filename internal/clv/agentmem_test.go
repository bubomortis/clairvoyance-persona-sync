package clv

import (
	"path/filepath"
	"testing"
)

func TestAgentMemoryMunge(t *testing.T) {
	cases := map[string]string{
		`C:\Users\allen\AppData\Roaming\clairvoyance`:            "C--Users-allen-AppData-Roaming-clairvoyance",
		`E:\Clairvoyance\Workspaces\Clairvoyance-Control-Center`: "E--Clairvoyance-Workspaces-Clairvoyance-Control-Center",
		`C:\ws\`:     "C--ws",      // trailing separator trimmed
		`C:\ws/`:     "C--ws",      // mixed trailing separator trimmed
		`/home/a/ws`: "-home-a-ws", // posix
		"":           "",
		"..":         "..", // AM-1: special segments have no separators to neutralize...
		".":          ".",
		`a/../b`:     "a-..-b", // ...but an embedded ".." is a harmless literal segment
	}
	for in, want := range cases {
		if got := AgentMemoryMunge(in); got != want {
			t.Errorf("AgentMemoryMunge(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAgentMemoryDir(t *testing.T) {
	got, ok := AgentMemoryDir(`C:\home`, `E:\ws`)
	want := filepath.Join(`C:\home`, ".claude", "projects", "E--ws", "memory")
	if !ok || got != want {
		t.Errorf("AgentMemoryDir = %q (ok=%v), want %q", got, ok, want)
	}
	// AM-1: a degenerate cwd whose munge is "."/".."/"" must be rejected, not allowed to
	// collapse the path out of .claude/projects (e.g. ".." → <home>/.claude/memory).
	for _, bad := range []string{"..", ".", "", `..\`, `.`} {
		if dir, ok := AgentMemoryDir(`C:\home`, bad); ok {
			t.Errorf("AgentMemoryDir(home, %q) accepted → %q, want rejected", bad, dir)
		}
	}
}

func TestEntryShellCwd(t *testing.T) {
	if c := EntryShellCwd([]byte(`{"shell":{"cwd":"E:\\ws"}}`)); c != `E:\ws` {
		t.Errorf("cwd = %q, want E:\\ws", c)
	}
	if c := EntryShellCwd([]byte(`{"name":"x"}`)); c != "" {
		t.Errorf("missing shell should give empty, got %q", c)
	}
	if c := EntryShellCwd([]byte(`not json`)); c != "" {
		t.Errorf("bad json should give empty, got %q", c)
	}
}
