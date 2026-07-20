// Command clvsync is the Clairvoyance Persona & Workspace Sync CLI.
//
// Phase 0 scaffold: subcommands are registered but export/import/workspace-prep
// are implemented in later phases.
package main

import (
	"fmt"
	"os"

	"github.com/bubomortis/clairvoyance-persona-sync/internal/datadir"
)

const usage = `clvsync — Clairvoyance Persona & Workspace Sync

Usage:
  clvsync export         Export a persona or workspace to a package   (Phase 1+)
  clvsync import         Import a package into this instance           (Phase 1+)
  clvsync workspace-prep Register/scaffold a workspace for import      (Phase 3)
  clvsync verify         Verify a package's signature + manifest       (Phase 1)
  clvsync datadir        Print the resolved Clairvoyance data directory

Flags are documented per subcommand (e.g. clvsync export -h).
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "datadir":
		d, err := datadir.Resolve()
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		fmt.Println(d)
	case "export", "import", "workspace-prep", "verify":
		fmt.Fprintf(os.Stderr, "clvsync: %q not yet implemented (Phase 0 scaffold)\n", os.Args[1])
		os.Exit(1)
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
}
