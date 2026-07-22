// Package dpapi provides the host-side Sealer implementations that protect the
// clvsync credential broker's local private identity at rest (spec §20.5, D17).
//
// This is deliberately host glue: it imports internal/cred and returns a
// cred.Sealer, so the extractable cred package stays OS-agnostic. On Windows the
// sealer is DPAPI (per-user, CRYPTPROTECT_UI_FORBIDDEN); elsewhere it is a
// documented no-op that relies on 0600 file permissions.
package dpapi

import "github.com/bubomortis/clairvoyance-persona-sync/internal/cred"

// Sealer returns the best available at-rest sealer for this platform.
func Sealer() cred.Sealer { return platformSealer() }
