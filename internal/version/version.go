// Package version exposes the clvsync build version and the repo coordinates used
// for the self-update check.
//
// Version is stamped at build time via:
//
//	-ldflags "-X github.com/bubomortis/clairvoyance-persona-sync/internal/version.Version=<tag>"
//
// An un-stamped local build reports "dev".
package version

// Version is the clvsync build version ("dev" when not stamped by the release build).
var Version = "dev"

// Repo coordinates for the GitHub Releases self-update check.
const (
	Owner = "bubomortis"
	Repo  = "clairvoyance-persona-sync"
)
