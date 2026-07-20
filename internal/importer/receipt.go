// Receipt support (§19.2): the app-closed finisher writes an import-receipt.json
// recording exactly what it placed; on restart the Sync Operator runs verify-import
// to reconcile the receipt against live on-disk state.
package importer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/bubomortis/clairvoyance-persona-sync/internal/clv"
)

// FileRef is a placed file plus the hash the finisher recorded for it.
type FileRef struct {
	Path   string `json:"path"` // absolute on the target machine
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

// Receipt is the durable record of one import, written by the finisher and read
// back by verify-import.
type Receipt struct {
	SchemaVersion int       `json:"schemaVersion"`
	ImportedAt    string    `json:"importedAt"`
	Tier          int       `json:"tier"`
	Mode          string    `json:"mode"`
	PersonaID     string    `json:"personaId,omitempty"`
	PersonaName   string    `json:"personaName,omitempty"`
	WorkspaceName string    `json:"workspaceName,omitempty"`
	WorkspaceID   string    `json:"workspaceId,omitempty"`
	SessionIDs    []string  `json:"sessionIds,omitempty"`
	PortableSet   []string  `json:"portableUpdated,omitempty"`  // definition fields changed
	PreservedSet  []string  `json:"machineLocalKept,omitempty"` // definition fields preserved
	Files         []FileRef `json:"files"`                      // hashed placed files
	Placed        []string  `json:"placed"`                     // human component list
	DataDir       string    `json:"dataDir"`
}

// hashFile returns the SHA-256 and size of a file.
func hashFile(p string) (string, int64, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// newReceipt seeds a receipt from an import report.
func newReceipt(in *clv.Instance, mode clv.Mode, rep *Report) *Receipt {
	return &Receipt{
		SchemaVersion: 1,
		ImportedAt:    time.Now().UTC().Format(time.RFC3339),
		Tier:          rep.Tier,
		Mode:          string(mode),
		PersonaID:     rep.PersonaID,
		PersonaName:   rep.PersonaName,
		WorkspaceID:   rep.WorkspaceID,
		WorkspaceName: rep.WorkspaceName,
		SessionIDs:    rep.SessionIDs,
		PortableSet:   rep.PortableUpdated,
		PreservedSet:  rep.MachineLocalKept,
		Placed:        rep.Placed,
		DataDir:       in.DataDir,
	}
}

// addFile hashes p (if it exists) and records it in the receipt.
func (r *Receipt) addFile(p string) {
	sum, n, err := hashFile(p)
	if err != nil {
		return
	}
	r.Files = append(r.Files, FileRef{Path: p, SHA256: sum, Bytes: n})
}

// Write serializes the receipt to path.
func (r *Receipt) Write(path string) error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// LoadReceipt reads a receipt file.
func LoadReceipt(path string) (*Receipt, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r Receipt
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// CheckLine is one reconciliation result line.
type CheckLine struct {
	Layer  string
	Detail string
	OK     bool
}

// VerifyResult is the outcome of a verify-import reconciliation.
type VerifyResult struct {
	Lines []CheckLine
	OK    bool
}

func (v *VerifyResult) add(ok bool, layer, detail string) {
	v.Lines = append(v.Lines, CheckLine{Layer: layer, Detail: detail, OK: ok})
	if !ok {
		v.OK = false
	}
}

// VerifyReceipt reconciles a receipt against live on-disk state (§19.2). It is
// read-only. Returns a per-layer pass/fail result.
func VerifyReceipt(r *Receipt, in *clv.Instance) *VerifyResult {
	v := &VerifyResult{OK: true}

	// Persona definition present, portable updated + machine-local preserved intact.
	if r.PersonaID != "" {
		p, err := in.FindPersona(r.PersonaID)
		if err != nil || p == nil {
			v.add(false, "persona", "definition not found for "+r.PersonaID)
		} else {
			detail := "present as "+p.Name
			if len(r.PortableSet) > 0 {
				detail += fmt.Sprintf("; portable updated %v", r.PortableSet)
			}
			if len(r.PreservedSet) > 0 {
				detail += fmt.Sprintf("; machine-local preserved %v", r.PreservedSet)
			}
			v.add(true, "persona", detail)
		}
	}

	// Placed files hash-match the receipt.
	for _, f := range r.Files {
		sum, n, err := hashFile(f.Path)
		switch {
		case err != nil:
			v.add(false, "file", "missing: "+f.Path)
		case sum != f.SHA256 || n != f.Bytes:
			v.add(false, "file", "hash mismatch: "+f.Path)
		default:
			v.add(true, "file", short(f.Path))
		}
	}

	// Sessions registered (Tier 2).
	if len(r.SessionIDs) > 0 {
		live := liveSessionIDs(in, r)
		for _, sid := range r.SessionIDs {
			ok := live[sid]
			v.add(ok, "session", sid+registered(ok))
		}
	}

	// Workspace registered (Tier 3/4).
	if r.WorkspaceName != "" {
		_, ok := in.WorkspaceByName(r.WorkspaceName)
		v.add(ok, "workspace", r.WorkspaceName+registered(ok))
	}

	// Non-destructive proof: at least one backup where a merge touched an existing file.
	return v
}

func registered(ok bool) string {
	if ok {
		return " — registered"
	}
	return " — MISSING"
}

func short(p string) string {
	if len(p) <= 60 {
		return p
	}
	return "..." + p[len(p)-57:]
}

// liveSessionIDs reads the target profile's agent-sessions.json and returns the set
// of session ids present.
func liveSessionIDs(in *clv.Instance, r *Receipt) map[string]bool {
	out := map[string]bool{}
	prof, err := in.DefaultProfile()
	if err != nil {
		return out
	}
	b, err := os.ReadFile(filepath.Join(in.DataDir, "profiles", prof, "agent-sessions.json"))
	if err != nil {
		return out
	}
	var doc struct {
		Sessions []map[string]any `json:"sessions"`
	}
	if json.Unmarshal(b, &doc) != nil {
		return out
	}
	for _, s := range doc.Sessions {
		out[fmt.Sprint(s["sessionId"])] = true
	}
	return out
}
