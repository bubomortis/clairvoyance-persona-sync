# clairvoyance-persona-sync (`clvsync`)

Transport a Clairvoyance **Staff member** — or a whole **workspace** — from one machine to another: identity + accumulated memory + conversation history, optionally with a resumable session, packaged as a small, verifiable, optionally-encrypted artifact.

A Staff member isn't one file — it's a definition (`profiles/{id}/staff.json` entry), a custom persona template, per-workspace memory (`.Clairvoyance/staff/{name}/`), and history (`agent-history/staff-{id}.json`). `clvsync` gathers those, scrubs anything that shouldn't leave the machine, and re-homes them on the target.

> **Status: validated on hardware.** All four tiers, round-trip create-or-merge sync, the Sync Operator assisted-import flow, the self-sync guard, and in-place self-update are implemented and tested. The two-machine Universal Resume integration test ([docs/INTEGRATION-TEST.md](docs/INTEGRATION-TEST.md)) passed end to end: a persona exported from machine A imported to machine B on a different drive layout and resumed with full context and live continuation. `clvsync status` reports the install and whether an update is available; `clvsync update` upgrades in place.

## Layered tiers

| Tier | Contents | Size |
| ---- | -------- | ---- |
| **1 — Portable Persona** | definition + custom template + memory + history (provider-agnostic, always works) | ~8–30 KB |
| **2 — Full-Sync Persona** | + **Universal Resume** artifacts → resume the thread under *any* model/provider on the target (Clairvoyance ≥ 0.77.0) | + a few KB |
| **3 — Workspace (lightweight)** | all personas in a workspace + non-ballooning content + shared memory | 100s KB–low MB |
| **4 — Workspace Heavy Add-on** | the regenerable/ballooning dirs (venv, models, node_modules, media), a **separate, last, space-gated** package | GB-scale |

Tier 4 is written **after** Tier 3 is complete and verified, and is **skipped (not truncated)** if the target (e.g. a USB drive) lacks room — so a limited destination degrades gracefully to the largest tier that fits.

## Security posture

Because a package **leaves the machine**, the design treats it as sensitive in transit and untrusted on arrival:

- **Secret scrub (S1):** exports are scanned for API keys / tokens / private keys and **blocked by default**; credential stores are never included.
- **Encryption (`age`):** optional ChaCha20-Poly1305, passphrase or recipient-public-key mode. *(Not AES — chosen for clean key handling; see the spec.)*
- **Authenticity (`minisign`):** detached signature over the package + manifest, verified before anything is unpacked.
- **Safe import (S3):** every path is validated against allowed roots — zip-slip / traversal / UNC / drive-letter escapes rejected.
- **Integrity (S8):** per-file SHA-256 manifest, verified on import.
- **Non-destructive (S7):** imports back up before merge, splice rather than replace, and quarantine imported persona/memory (which becomes agent-loaded instructions) for review before activation.

A full pre-build security audit backs these controls.

**Developing with Staff and pushing to GitHub?** `clvsync` transports conversation history, so a
token pasted into a chat can travel with a persona. Keep credentials in **Settings → Credentials**
(never in chat), push via git's credential helper or `gh` (never a tokenized URL), and let CI
releases use Actions' own token. If a secret does leak into a transcript, the scan blocks the export
— rotate it and re-store it rather than overriding. See [Credential hygiene](docs/OPERATOR-GUIDE.md#credential-hygiene-read-this-if-you-push-to-github--or-anywhere--with-staff).

## Installation

`clvsync` is a single self-contained binary. Choose **one** method.

### Option A: Ask Clairvoyance to install it

Use this if you have a trusted Clairvoyance Staff agent that can run commands on your machine. It installs the binary **and** sets up a **Sync Operator** Staff member that drives your imports and exports in plain language (no terminal). Paste this prompt to that agent verbatim:

```text
Install Clairvoyance Persona Sync (clvsync) on this machine from
https://github.com/bubomortis/clairvoyance-persona-sync

Treat AGENTS.md in that repository as the AUTHORITATIVE, step-by-step procedure:
read section 1 in full and follow it exactly. Observe these rules:

1. IDEMPOTENCY FIRST. If clvsync is already on PATH, run `clvsync status` before
   changing anything. If it reports a working install with a Sync Operator present,
   do NOT reinstall -- report the existing install and stop. If it reports "UPDATE
   AVAILABLE", offer to run `clvsync update` and nothing else. If the Sync Operator
   is missing, only create it (skip the binary install). If it reports a DUPLICATE
   operator, stop and ask me to remove the extra.
2. Confirm prerequisites: Clairvoyance 0.77.0 or later, network access to github.com,
   and a shell you can run clvsync from. Report any missing prerequisite and stop.
3. VERIFY THE BINARY'S INTEGRITY. Download the release build for this OS/arch AND
   SHA256SUMS from the latest GitHub release, verify the checksum against SHA256SUMS,
   and refuse any binary that does not match. (This proves integrity, not authorship:
   the trust anchor is GitHub + the publisher's account, not a signature yet.) If
   there is no prebuilt binary for this platform, build from source with
   `go build ./cmd/clvsync`. Put clvsync on PATH; confirm `clvsync datadir` works.
4. Place only this repo's own personas/"Sync Operator.md" template into the data dir.
5. STOP AND GET MY EXPLICIT APPROVAL before creating the Sync Operator Staff member
   and granting it shell access -- a prompt is not consent.
6. Arm-check the guard: `clvsync export --persona "Sync Operator" --out op.cvpkg`
   MUST be refused (S15). If it succeeds, fix the operator's Knowledge Base marker.
7. Do NOT modify, commit to, or push to the source repository. Report every command
   and its result.
```

The agent must still stop and ask for your approval before creating Staff or granting shell access. A copy-paste prompt is a convenience, **not consent**.

### Option B: Install it yourself

1. Download the build for your OS/arch (`clvsync-<os>-<arch>[.exe]`) and `SHA256SUMS` from the [latest release](https://github.com/bubomortis/clairvoyance-persona-sync/releases/latest).
2. **Verify the checksum** and refuse a mismatch:
   - Windows (PowerShell): `Get-FileHash clvsync-windows-amd64.exe -Algorithm SHA256`
   - macOS / Linux: `shasum -a 256 clvsync-<os>-<arch>`

   Compare the digest to the matching line in `SHA256SUMS`.
3. Put the binary on your `PATH`, then confirm: `clvsync status`.

No prebuilt binary for your platform? Build from source (Go ≥ 1.26): `go build -o clvsync ./cmd/clvsync`.

Once installed, **`clvsync status`** shows the version, data dir, Sync Operator state, and whether an update is available; **`clvsync update`** upgrades the binary in place (downloads the latest release, checksum-verifies it, and swaps it in — Windows-safe).

## Build (from source)

Requires Go ≥ 1.26.

```sh
go build ./...
go test ./...
./clvsync datadir      # prints the resolved Clairvoyance data dir for this OS
```

Cross-platform: resolves the Clairvoyance data directory per OS (Windows `%APPDATA%`, macOS `~/Library/Application Support`, Linux `~/.config`).

## Roadmap

- [x] **Phase 0** — core: per-OS data-dir resolver, secret scanner, safe-path guard, SHA-256 manifest, CLI skeleton *(done, unit-tested)*
- [x] **Phase 1** — Tier 1 export/import with `age` encryption + `minisign` signing; `export`/`import`/`keygen` CLI *(done; round-trip self-test + validated against live instance data)*
- [x] **Phase 2** — Tier 2 Universal Resume (session records + summaries + exclusions; workspace binding remapped, provider/model preserved) *(done; round-trip + live-data validated)*
- [x] **Phase 3** — Tier 3 whole-workspace (roster + content, heavy dirs excluded) + `workspace-prep` offline registry mint *(done; round-trip + live-data validated)*
- [x] **Phase 4** — Tier 4 heavy add-on (`--include-heavy`) + §8a space-aware fail-down (skip-not-truncate) *(done; round-trip + live-validated)*
- [x] **Phase 5** — docs (Operator Guide, Security Audit re-verified), CI cross-compile (win/linux/mac) *(done)*
- [x] **Phase 6** — round-trip **create-or-merge sync**: `--mode sync|overwrite|skip`, portable-vs-machine-local definition split (machine-local runtime preserved on a round-trip), memory union, history newest-wins, `--dry-run` preview *(done; unit-tested + live CLI-validated)*
- [x] **Phase 7** — **Sync Operator** assisted-import persona + `AGENTS.md` runbook, `import-receipt.json` + `verify-import` restart reconciliation, guided interactive `import`, and the **S15 self-sync guard** *(done; unit-tested + live CLI-validated)*
- [x] **`v0.1.0`** — signed release (binaries + `SHA256SUMS` on GitHub Releases), validated by the two-machine Universal Resume integration test ([docs/INTEGRATION-TEST.md](docs/INTEGRATION-TEST.md))
- [x] **`v0.1.1`** — on-import auto-repoint of machine-local paths (`shell.cwd`/runtime) that don't exist on the target
- [x] **`v0.2.0`** — in-place self-update (`status` / `update` / `version`, checksum-verified, Windows-safe binary swap), S4 unrecognized-definition-field review advisory, and the Staff-driven "install from GitHub" quick-start

## Round-trip sync & the Sync Operator

`clvsync import` defaults to **`sync`** (create-or-merge): re-syncing the same persona
updates its portable fields but **preserves each machine's local runtime** (`model`,
`runtime`, `shell`, …), so you can move a persona back and forth without clobbering
either box's settings. `--dry-run` previews the exact plan first.

For non-CLI users, each machine can run a **Sync Operator** Staff member that handles
verify → preview → app-closed finish → restart verification in plain language. See
[docs/SYNC-OPERATOR.md](docs/SYNC-OPERATOR.md) and [AGENTS.md](AGENTS.md). The operator is
machine-local: `clvsync` refuses to sync it by default (guard **S15**).

## Documentation

- [AGENTS.md](AGENTS.md) — have Clairvoyance Staff set up and run `clvsync` for you (no terminal)
- [docs/OPERATOR-GUIDE.md](docs/OPERATOR-GUIDE.md) — export → transport → prep → import → verify, with troubleshooting
- [docs/SYNC-OPERATOR.md](docs/SYNC-OPERATOR.md) — round-trip merge semantics, the Sync Operator persona, receipt/verify-import, the S15 guard
- [docs/INTEGRATION-TEST.md](docs/INTEGRATION-TEST.md) — two-machine Tier-2 Universal Resume test, CLI path (run before release)
- [docs/SYNC-OPERATOR-TEST.md](docs/SYNC-OPERATOR-TEST.md) — assisted-path test: install from GitHub → create the Sync Operator → import driven by the operator
- [docs/SECURITY-AUDIT.md](docs/SECURITY-AUDIT.md) — the findings re-verified against the built code

## License

MIT — see [LICENSE](LICENSE). Provided as-is, without warranty.
