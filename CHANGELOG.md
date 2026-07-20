# Changelog

All notable changes to `clvsync` are documented here. Format loosely follows
[Keep a Changelog](https://keepachangelog.com/); the project is pre-1.0.

## [Unreleased]

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

## [0.1.0] — pending
First tagged release. Held for the two-machine Universal Resume integration test
(`docs/INTEGRATION-TEST.md`). Phases 0–7 complete: all four tiers, `age` encryption +
`minisign` signing, per-OS data-dir resolution, secret-scan / zip-slip / integrity /
non-destructive controls, CI cross-compile for win/linux/mac.
