package export

import (
	"fmt"
	"strings"

	"github.com/bubomortis/clairvoyance-persona-sync/internal/pkg"
)

// importDoc builds the human-readable IMPORT.md embedded in every package.
func importDoc(meta pkg.Meta) string {
	var b strings.Builder
	b.WriteString("# clvsync package — how to import\n\n")
	switch meta.Tier {
	case 4:
		fmt.Fprintf(&b, "This is a **Tier 4 heavy add-on** for workspace **%s**.\n", meta.WorkspaceName)
		b.WriteString("Import the workspace's Tier 3 package first, then:\n\n")
		b.WriteString("```\nclvsync import --in <this-file>\n```\n")
	case 3:
		fmt.Fprintf(&b, "This is a **Tier 3 workspace** package for **%s** (%d persona(s)).\n\n", meta.WorkspaceName, len(meta.Roster))
		b.WriteString("On the target machine (Clairvoyance **closed**):\n\n")
		b.WriteString("```\nclvsync import --in <this-file> --workspace-path <local-dir-for-the-workspace>\n```\n\n")
		b.WriteString("Regenerable dirs (venv, models, node_modules, downloads, …) are **not** included; ")
		b.WriteString("recreate them from `requirements.txt` / package manifests / re-download, or import the paired `_heavy` package.\n")
	default:
		fmt.Fprintf(&b, "This is a **Tier %d persona** package for **%s**.\n\n", meta.Tier, meta.PersonaName)
		b.WriteString("```\nclvsync import --in <this-file>\n```\n")
		if meta.Tier >= 2 {
			b.WriteString("\nIncludes **Universal Resume** artifacts — the session resumes under whatever model/provider the target runs (Clairvoyance ≥ 0.77.0).\n")
		}
	}
	b.WriteString("\n## If the package is encrypted (`.age`) or signed (`.minisig`)\n\n")
	b.WriteString("- Encrypted: set the passphrase out-of-band, then `CLVSYNC_PASSPHRASE=… clvsync import --in <file>`\n")
	b.WriteString("- Signed: `clvsync import --in <file> --verify-key <pub> --sig <file>.minisig` (verified before anything is unpacked)\n")
	b.WriteString("\n## Safety\n\n")
	b.WriteString("Import is non-destructive (existing data is backed up, entries spliced not replaced) and refuses path-traversal. ")
	b.WriteString("Imported persona/memory are externally sourced — **review before relying on them**. Restart Clairvoyance after import.\n")
	return b.String()
}
