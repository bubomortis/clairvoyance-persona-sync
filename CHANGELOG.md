# Changelog

All notable changes to `clvsync` are documented here. Format loosely follows
[Keep a Changelog](https://keepachangelog.com/); the project is pre-1.0.

## [Unreleased]

_Nothing yet._

## [0.2.3] - 2026-07-22

### Added
- **Import flags that placed memory surfaces only at the persona's next session start (D19 Q1 / §21.5).**
  Clairvoyance injects a Staff member's knowledge at session start (runtime init), not by polling — so
  curated `.clairvoyance/staff` or `--include-agent-memory` content that lands mid-session is invisible
  to a persona that is already running until it restarts. The import report now says so explicitly and
  tells you to fully restart the persona (start a new session); the app-closed import + relaunch does this
  automatically.
  (clvsync deliberately does **not** touch the app-owned `staff-names.json` registry — whether it gates
  memory discovery is unconfirmed, and it carries name-disambiguation semantics a wrong write could break.)
- **`clvsync export --include-agent-memory` (D19).** Opt-in to also bundle the persona's rich
  Claude Code working memory — the `~/.claude/projects/<munge>/memory` store, keyed by the
  workspace cwd — on top of the curated `.clairvoyance/staff` memory that always travels. It is
  secret-scanned like all staged content (the `.claude` store is exactly where a stray API key
  tends to live), and on import it is **remapped to the target machine's own home and cwd munge**
  (following the same `shell.cwd` repoint as the rest of the import), placed non-destructively with
  a `.clvsync-bak` on any overwrite. Without the flag, the store does not travel at all. The
  target path is contained under `<home>/.claude/projects` — a degenerate persona cwd is refused
  rather than allowed to place memory outside that sandbox.

## [0.2.2] - 2026-07-21

### Changed
- **A Tier-1 persona package no longer carries the raw conversation transcript (D19).** A lone
  `agent-history` file is a trap: on the destination it is clobbered by the first fresh chat and is
  unusable without a resumable session anyway — this is what lost a test persona's memory. Tier-1
  continuity now rides on the curated `.clairvoyance/staff` memory that already travels. The
  transcript travels at **Tier 2** (as part of Universal Resume, where it is actually replayable)
  and inside a whole-workspace **Tier 3** package (workspace scope is unchanged).

### Security
- **Self-update download hardening.** `clvsync update` now caps each download at a fixed size
  (100 MiB binary / 1 MiB `SHA256SUMS`) via `io.LimitReader`, refusing an over-cap body instead of
  reading it into memory (SU3), and refuses any asset URL that is not HTTPS on `github.com` or a
  `*.githubusercontent.com` host (SU4).
- **CI Actions are pinned to commit SHAs** (CI1) instead of mutable `@vN` tags, so a compromised or
  retagged action cannot silently enter the release pipeline that self-update trusts.
- **Tool-run permission (`permissionMode`) never travels with a persona (D18).** `permissionMode`
  is a machine-local *trust* grant. On import, a **newly created** persona is placed at `standard`
  (any `skip-permissions` in the package is stripped; re-grant it locally if wanted), and an
  **existing** persona keeps *this* machine's prior grant regardless of what the package carried —
  on sync-merge (machine-local fields are preserved anyway) and on wholesale overwrite alike. The
  import report surfaces a trust note whenever it resets or preserves the grant.

### Fixed
- **Import no longer panics on a crafted/degenerate staff entry.** A staff entry whose `ai` field —
  or the whole entry — was the JSON literal `null` unmarshalled to a nil Go map and crashed the
  import chokepoint (a reliable crash on a hostile package; fail-closed on trust, but a crash all
  the same). The definition merge/trust path now guards every such map and errors cleanly instead.

## [0.2.1] - 2026-07-21

### Security
- **`export` never silently ships a plaintext package (D8 / spec §20.2).** Previously, with no
  `CLVSYNC_PASSPHRASE` and no `--recipient`, `export` produced an *unencrypted* `.cvpkg` with no
  prompt or warning — so a persona's memory and history could leave the machine in the clear
  unnoticed. Now export **fails closed**: an interactive terminal is **prompted** for a passphrase
  (entered twice to confirm, no-echo), a non-interactive caller is **refused** with guidance, and the
  only way to get an unencrypted package is the explicit new **`--plaintext`** flag (which prints a
  warning). The interactive `import` passphrase prompt is also now no-echo.

### Added
- **`clvsync export --plaintext`** — explicit opt-in to write an unencrypted package.

### Added
- **In-place self-update.** New commands: `clvsync status` (version, data dir, Sync Operator
  present/absent/duplicate, and whether a newer GitHub release exists), `clvsync update` (downloads
  this platform's release asset, **checksum-verifies it against `SHA256SUMS`**, and swaps the running
  binary — Windows-safe: the current exe is renamed to `.old` rather than overwritten; `--yes` to
  skip the prompt), and `clvsync version`. Builds are version-stamped via ldflags in CI. `status` is
  the deterministic idempotency gate for the Staff install runbook (`AGENTS.md` §1 step 0).

### Security
- **S4 — unrecognized-definition-field review advisory.** On import, a staff definition carrying
  top-level keys outside the documented portable/machine-local set is now surfaced as a warning for
  review (`clv.UnknownDefinitionFields`). The definition is inert data clvsync never executes, so this
  warns rather than blocks; imports remain quarantined for review (S5).
- **`verify` no longer claims a signature it never checked (P2).** Running `verify` without
  `--verify-key`/`--sig` now prints `UNVERIFIED` (integrity-only) and exits non-zero, instead of
  reporting "signature verified" and exit 0. Because the manifest travels inside the package,
  integrity alone is not authenticity — the command now says so and fails closed.
- **Crafted/empty persona names can no longer wipe all Staff memory (P3).** `Slug` neutralizes path
  separators and leading/trailing dots, and the memory merge refuses an empty or path-bearing key
  (with a defense-in-depth guard before any `RemoveAll`), so an `--mode overwrite` import can never
  collapse to the staff-memory root and delete every persona's memory.
- **The secret scanner no longer silently skips files (P4).** Files larger than 5 MiB are now
  streamed and scanned instead of skipped, and binary (NUL-containing) files are reported as an
  explicit skip rather than passing as "clean" — a skipped file is surfaced on export, not mistaken
  for a scanned-clean one.

### Added
- **Export remembers its output location.** `export` now accepts `--out-dir <folder>` (auto-names
  the package) in addition to `--out <full-path>`, and **remembers the folder** in a small
  `clvsync-state.json` beside the data dir. With neither flag, the export defaults to that
  last-used folder; the new `clvsync last-export-dir` command prints it. The Sync Operator uses this
  to **ask where to save on the first export and offer the last location as the default thereafter**
  (`AGENTS.md` §3). First export with no remembered location and no flag errors with guidance.

### Docs
- **README "Installation" quick-start** — a user-facing **Option A: Ask Clairvoyance to install it**
  (verbatim, idempotency-first copy-paste prompt that points Staff at `AGENTS.md` §1 and gates on
  `clvsync status`) and **Option B: install it yourself** (download + checksum-verify + PATH), plus a
  self-update note. Mirrors the backup-system / AGY-Shim "ask Clairvoyance to install it" convention,
  closing the gap where `AGENTS.md` had the procedure but the README didn't tell users how to start it.
- **Staff-directed install** (`AGENTS.md` §1 rewritten): a Staff agent installs Persona Sync from the
  GitHub Release — fetch + checksum-verify the binary, place the Sync Operator template, create the
  operator (attended), arm-check the S15 guard — as the authoritative procedure, with integrity-checked /
  attended / idempotent / don't-modify-source / report-every-command hard rules, mirroring the
  backup-system "ask Clairvoyance to install it" convention.
- **Assisted-path test runbook** (`docs/SYNC-OPERATOR-TEST.md`): a fresh machine where a Staff agent
  installs from GitHub, then an import is driven by the operator — acceptance checks for the
  Staff-driven install, the app-closed finisher handoff, the S15 guard, and non-CLI usability.
  Complements the CLI `INTEGRATION-TEST.md`.
- **Credential-hygiene guidance** for developers who push to GitHub with Staff: keep tokens in
  Settings → Credentials (never in chat), push via git's credential helper / `gh`, let CI use its
  own scoped token, and treat a blocked export as a **real leak** (rotate + re-store + scrub, not a
  reflexive `--allow-secrets`). Added to the Operator Guide (new "Credential hygiene" section),
  README security posture, the Sync Operator persona, and `AGENTS.md`.

## [0.1.1] — 2026-07-21

### Added
- **On-import auto-repoint of dead machine-local paths.** When an imported persona's `shell.cwd`
  points at a path that doesn't exist on this machine (e.g. the source machine's data dir), it is
  repointed to the target data dir so the persona starts without a manual `cwd` fix — the
  "cwd does not exist" case seen in the first cross-machine import. Applies to created/overwritten
  personas and workspace roster members; a `cwd` that already exists is left untouched; sync-merges
  are unaffected (they keep the destination's shell). Replaces the previous cwd advisory. Validated
  by a second two-machine run.

## [0.1.0] — 2026-07-21

First tagged release. **Validated by the two-machine Universal Resume integration test**
(`docs/INTEGRATION-TEST.md`): a persona was exported from machine A, imported to machine B on a
different drive layout, and resumed its session with full prior context and live continuation.
Phases 0–7 complete: all four tiers, `age` encryption + `minisign` signing, per-OS data-dir
resolution, secret-scan / zip-slip / integrity / non-destructive controls, and CI cross-compile
for win/linux/mac.

### Fixed
- **Conversation transcript now travels.** Clairvoyance names the transcript `{staffId}.json`,
  and the staff id already carries the `staff-` prefix; the code prepended a second one and
  looked for `staff-staff-…json`, which never exists — so the transcript was silently omitted
  from every package and a resumed session opened blank. Fixed across export, import, and merge.
- **Data-dir resolution now honors `CLAIRVOYANCE_BASE_USER_DATA`.** When the user has
  relocated their Clairvoyance store off the OS default (e.g. onto another drive), the app
  reads from that path via `CLAIRVOYANCE_BASE_USER_DATA`; `clvsync` previously ignored it and
  imported into the OS-default dir the app wasn't looking at (a persona would land on disk but
  not appear in the app). Resolution precedence is now `CLV_DATA_DIR` → `CLAIRVOYANCE_BASE_USER_DATA`
  → OS default. `--data-dir` still overrides everything.

### Added — Phase 6: round-trip create-or-merge sync
- `import --mode sync|overwrite|skip` (default **`sync`**), replacing the old
  create-or-`--force` behavior. `--force` remains as a back-compat alias for `overwrite`.
- **Portable-vs-machine-local definition merge:** a sync-merge updates portable fields
  (`name`, `jobDescription`, `knowledgeTemplate`, `interactionMode`, `type`, `wiggumMode`)
  and **preserves** the destination's machine-local runtime (`ai`, `model`, `runtime`,
  `shell`, `status`, `isDefault`, `activity`, `createdAt`) — so a persona can round-trip
  between machines without clobbering either box's model/runtime.
- **Component merges:** memory is a per-file union (identical skipped, changed backed up
  then updated); history is newest-wins by `savedAt` (else more-messages-wins) with a
  `.clvsync-bak` and a divergence warning; sessions merge by `sessionId`.
- `import --dry-run` computes and prints the full plan without writing anything.

### Added — Phase 7: assisted import, verification, and distribution
- **Sync Operator** persona (`personas/Sync Operator.md`) + `AGENTS.md` runbook: a Staff
  member on each machine that runs verify → dry-run preview → app-closed finish → restart
  verification in plain language.
- **Import receipt:** the finisher writes `import-receipt.json` (tier, mode, persona id,
  portable-updated / machine-local-preserved field sets, session ids, workspace id, and a
  SHA-256 per placed file).
- **`verify-import --receipt`:** read-only expected-vs-actual reconciliation on restart,
  printing a per-layer pass/fail table.
- **Guided interactive import:** `clvsync import` with no `--in` prompts for file →
  passphrase → mode → preview → confirm.

### Security
- **S15 — self-sync guard:** `clvsync` recognizes the Sync Operator by its
  `knowledgeTemplate: "Sync Operator"` marker and refuses to sync it. Export warns and
  requires `--allow-operator-sync`; import blocks by default and **hard-blocks** a
  self-overwrite of the operator running the import, regardless of the override.

