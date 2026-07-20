// Package datadir resolves the Clairvoyance user-data directory per OS (D10).
package datadir

import (
	"os"
	"path/filepath"
	"runtime"
)

// Resolve returns the Clairvoyance user-data directory for the current OS.
// The CLV_DATA_DIR environment variable overrides detection (tests / non-standard installs).
//
//	Windows: %APPDATA%\clairvoyance
//	macOS:   ~/Library/Application Support/clairvoyance
//	Linux:   $XDG_CONFIG_HOME/clairvoyance  (else ~/.config/clairvoyance)
func Resolve() (string, error) {
	if v := os.Getenv("CLV_DATA_DIR"); v != "" {
		return v, nil
	}
	switch runtime.GOOS {
	case "windows":
		if ad := os.Getenv("APPDATA"); ad != "" {
			return filepath.Join(ad, "clairvoyance"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "AppData", "Roaming", "clairvoyance"), nil
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", "clairvoyance"), nil
	default:
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			return filepath.Join(xdg, "clairvoyance"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".config", "clairvoyance"), nil
	}
}
