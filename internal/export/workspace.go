package export

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/bubomortis/clairvoyance-persona-sync/internal/clv"
	"github.com/bubomortis/clairvoyance-persona-sync/internal/pkg"
)

// Workspace exports a whole workspace (Tier 3): every persona bound to it (definition
// + history + custom template) plus the workspace's non-ballooning content (heavy
// dirs excluded → Tier 4). Persona memory travels inside the workspace tree.
func Workspace(in *clv.Instance, wsName, outPath string, opts Options) (*Result, error) {
	ws, ok := in.WorkspaceByName(wsName)
	if !ok {
		return nil, fmt.Errorf("workspace %q not found", wsName)
	}
	stage, err := os.MkdirTemp("", "clvsync-ws-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)

	meta := newMeta(3)
	meta.WorkspaceName = wsName

	for _, p := range in.PersonasInWorkspace(wsName) {
		if p.IsOperator() && !opts.AllowOperatorSync {
			// S15: never carry the Sync Operator in a workspace package by default.
			continue
		}
		// memory=false: it travels inside workspace/.Clairvoyance/staff
		if err := stagePersona(in, p, stage, filepath.Join("roster", p.Lname), false, false); err != nil {
			return nil, err
		}
		meta.Roster = append(meta.Roster, pkg.RosterEntry{ID: p.ID, Name: p.Name, Lname: p.Lname, Template: p.Template})
	}

	if err := pkg.CopyTree(ws.Path, filepath.Join(stage, "workspace"), pkg.HeavyDirs); err != nil {
		return nil, err
	}

	return finalize(stage, outPath, meta, opts)
}
