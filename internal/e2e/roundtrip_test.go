package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/bubomortis/clairvoyance-persona-sync/internal/clv"
	"github.com/bubomortis/clairvoyance-persona-sync/internal/cryptobox"
	"github.com/bubomortis/clairvoyance-persona-sync/internal/export"
	"github.com/bubomortis/clairvoyance-persona-sync/internal/importer"
)

const staffID = "staff-1782967554502-testy"

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func storeJSON(t *testing.T, homePath, wsName, wsPath string) string {
	t.Helper()
	s := map[string]any{"workspaces": []map[string]string{
		{"id": "workspace-home", "name": "Home", "path": homePath},
		{"id": "workspace-testws", "name": wsName, "path": wsPath},
	}}
	b, _ := json.Marshal(s)
	return string(b)
}

// buildSource creates a synthetic source instance with a persona named "Testy":
// home memory, workspace memory, history, and a custom template.
func buildSource(t *testing.T) (dataDir string, wantHome, wantWS, wantHist string) {
	dataDir = t.TempDir()
	wsPath := filepath.Join(dataDir, "ws")
	write(t, filepath.Join(dataDir, "clairvoyance-store.json"), storeJSON(t, dataDir, "TestWS", wsPath))

	entry := `{"id":"` + staffID + `","name":"Testy","knowledgeTemplate":"Custom.md","jobDescription":"tester"}`
	write(t, filepath.Join(dataDir, "profiles", "prof1", "staff.json"), "["+entry+"]")

	wantHist = `{"messages":[{"role":"user","content":"hi"}],"sessionId":"s1","staffId":"` + staffID + `"}`
	write(t, filepath.Join(dataDir, "profiles", "prof1", "agent-history", "staff-"+staffID+".json"), wantHist)

	wantHome = "# Testy home memory\nremember the plan"
	write(t, filepath.Join(dataDir, ".Clairvoyance", "staff", "testy", "index.md"), wantHome)

	wantWS = "workspace-scoped note"
	write(t, filepath.Join(wsPath, ".Clairvoyance", "staff", "testy", "notes.md"), wantWS)

	write(t, filepath.Join(dataDir, "neurons", "personas", "Custom.md"), "# Custom persona template")

	// Tier 2 Universal Resume artifacts
	writeJSONFile(t, filepath.Join(dataDir, "profiles", "prof1", "agent-sessions.json"), map[string]any{
		"version": 1, "sessions": []map[string]any{{
			"sessionId": "sess-testy-1", "staffId": staffID, "workspaceId": "workspace-testws",
			"workspacePath": wsPath, "provider": "claude", "model": "claude-opus-4-8", "runtimeId": "acp-" + staffID,
		}},
	})
	writeJSONFile(t, filepath.Join(dataDir, "profiles", "prof1", "session-summaries.json"), map[string]any{
		"version": 1, "entries": []map[string]any{
			{"sessionId": "sess-testy-1", "text": "the summary", "updatedAt": "t"},
			{"sessionId": "other-sess", "text": "unrelated"},
		},
	})
	writeJSONFile(t, filepath.Join(dataDir, "profiles", "prof1", "resume-exclusions.json"), map[string]any{
		"version": 1, "entries": []map[string]any{{"sessionId": "sess-testy-1", "reason": "trim", "addedAt": "t"}},
	})
	return
}

func writeJSONFile(t *testing.T, path string, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	write(t, path, string(b))
}

func TestRoundtrip_Tier2_UniversalResume(t *testing.T) {
	srcDir, _, _, _ := buildSource(t)
	src, _ := clv.Open(srcDir)
	p, err := src.FindPersona("Testy")
	if err != nil {
		t.Fatal(err)
	}
	pkgPath := filepath.Join(t.TempDir(), "testy2.cvpkg.age")
	if _, err := export.Persona(src, p, pkgPath, export.Options{Tier: 2, Passphrase: "pw-tier2-strong-enough"}); err != nil {
		t.Fatalf("export tier2: %v", err)
	}

	dstDir := t.TempDir()
	dstWS := filepath.Join(dstDir, "ws-relocated")
	write(t, filepath.Join(dstDir, "clairvoyance-store.json"), storeJSON(t, dstDir, "TestWS", dstWS))
	write(t, filepath.Join(dstDir, "profiles", "prof2", "staff.json"), "[]")

	dst, _ := clv.Open(dstDir)
	rep, err := importer.Apply(pkgPath, dst, importer.Options{Passphrase: "pw-tier2-strong-enough"})
	if err != nil {
		t.Fatalf("import tier2: %v", err)
	}
	if rep.Tier != 2 {
		t.Fatalf("expected tier 2, got %d", rep.Tier)
	}

	// session record: remapped workspace path, provider preserved
	var sdoc struct {
		Sessions []map[string]any `json:"sessions"`
	}
	readJSON(t, filepath.Join(dstDir, "profiles", "prof2", "agent-sessions.json"), &sdoc)
	if len(sdoc.Sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sdoc.Sessions))
	}
	s := sdoc.Sessions[0]
	if s["sessionId"] != "sess-testy-1" {
		t.Fatalf("wrong sessionId: %v", s["sessionId"])
	}
	if s["workspacePath"] != dstWS {
		t.Fatalf("workspacePath not remapped: got %v want %v", s["workspacePath"], dstWS)
	}
	if s["provider"] != "claude" {
		t.Fatalf("provider should be preserved for Universal Resume, got %v", s["provider"])
	}

	// summaries filtered to this persona's session only (other-sess excluded)
	var sumDoc struct {
		Entries []map[string]any `json:"entries"`
	}
	readJSON(t, filepath.Join(dstDir, "profiles", "prof2", "session-summaries.json"), &sumDoc)
	if len(sumDoc.Entries) != 1 || sumDoc.Entries[0]["sessionId"] != "sess-testy-1" {
		t.Fatalf("summaries not filtered/merged correctly: %+v", sumDoc.Entries)
	}
	if _, err := os.Stat(filepath.Join(dstDir, "profiles", "prof2", "resume-exclusions.json")); err != nil {
		t.Fatalf("resume-exclusions not merged")
	}
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatal(err)
	}
}

func TestRoundtrip_EncryptedSigned(t *testing.T) {
	srcDir, wantHome, wantWS, wantHist := buildSource(t)

	src, err := clv.Open(srcDir)
	if err != nil {
		t.Fatal(err)
	}
	p, err := src.FindPersona("Testy")
	if err != nil {
		t.Fatalf("find persona: %v", err)
	}
	if len(p.Memory) != 2 {
		t.Fatalf("expected 2 memory scopes (home+TestWS), got %d: %+v", len(p.Memory), p.Memory)
	}

	// sign + encrypt on export
	pub, priv, err := cryptobox.GenerateSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	pkgPath := filepath.Join(t.TempDir(), "testy.cvpkg.age")
	res, err := export.Persona(src, p, pkgPath, export.Options{Passphrase: "hunter2-correct-horse", SignKey: &priv})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if !res.Encrypted || res.SigPath == "" {
		t.Fatalf("expected encrypted+signed, got %+v", res)
	}
	sig, _ := os.ReadFile(res.SigPath)

	// target instance: same workspace NAME but a DIFFERENT path (tests remap)
	dstDir := t.TempDir()
	dstWS := filepath.Join(dstDir, "ws-relocated")
	write(t, filepath.Join(dstDir, "clairvoyance-store.json"), storeJSON(t, dstDir, "TestWS", dstWS))
	write(t, filepath.Join(dstDir, "profiles", "prof2", "staff.json"), "[]")

	dst, err := clv.Open(dstDir)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := importer.Apply(pkgPath, dst, importer.Options{Passphrase: "hunter2-correct-horse", Sig: sig, VerifyKey: &pub})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if rep.PersonaID != staffID {
		t.Fatalf("wrong persona id: %s", rep.PersonaID)
	}

	// assert definition merged
	found, err := dst.FindPersona("Testy")
	if err != nil {
		t.Fatalf("persona not found on target after import: %v", err)
	}
	if found.ID != staffID {
		t.Fatalf("target persona id mismatch: %s", found.ID)
	}

	// assert home memory landed byte-identical
	assertFile(t, filepath.Join(dstDir, ".Clairvoyance", "staff", "testy", "index.md"), wantHome)
	// assert workspace memory REMAPPED to the target workspace path
	assertFile(t, filepath.Join(dstWS, ".Clairvoyance", "staff", "testy", "notes.md"), wantWS)
	// assert history
	assertFile(t, filepath.Join(dstDir, "profiles", "prof2", "agent-history", "staff-"+staffID+".json"), wantHist)
	// assert template
	if _, err := os.Stat(filepath.Join(dstDir, "neurons", "personas", "Custom.md")); err != nil {
		t.Fatalf("custom template not placed: %v", err)
	}
}

func TestExport_BlocksPlantedSecret(t *testing.T) {
	srcDir, _, _, _ := buildSource(t)
	// Plant a secret into the persona's home memory.
	write(t, filepath.Join(srcDir, ".Clairvoyance", "staff", "testy", "leak.md"),
		"my key is sk-ant-abcdef0123456789ABCDEF do not share")

	src, _ := clv.Open(srcDir)
	p, _ := src.FindPersona("Testy")
	pkgPath := filepath.Join(t.TempDir(), "blocked.cvpkg")
	_, err := export.Persona(src, p, pkgPath, export.Options{}) // no AllowSecrets
	if err == nil {
		t.Fatal("expected export to be blocked by secret scan")
	}
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing %s: %v", path, err)
	}
	if string(got) != want {
		t.Fatalf("%s: got %q want %q", path, got, want)
	}
}
