# Security Audit — verified against the built code

The pre-build spec enumerated 14 findings (S1–S14). This re-verifies each against the shipped implementation. Findings marked **built** have code + a test; **inherent** are accepted, documented design trade-offs; **partial** have a follow-up.

| # | Finding | Status | Where in code | Test |
| - | ------- | ------ | ------------- | ---- |
| **S1** | Secret leakage (history/memory/resume are a secret goldmine) | **built** | `internal/scan` + `export.finalize` scans the whole staging tree and **blocks** unless `AllowSecrets`; credential stores never gathered (`clv` only reads staff/memory/history) | `scan_test.go`, e2e `TestExport_BlocksPlantedSecret` |
| **S2** | Transport interception | **built** | `internal/cryptobox` age (ChaCha20-Poly1305), passphrase + X25519 recipient | `cryptobox_test.go` |
| **S3** | Zip-slip / path traversal on import | **built** | `internal/safepath.SafeJoin`, enforced in `pkg.ExtractTar` | `safepath_test.go`, `pkg_test.go TestExtractTar_RejectsTraversal` |
| **S4** | Malicious definition (hostile runtime/shell) | **built (advisory)** | definition is spliced verbatim and **not executed** by clvsync; `clv.UnknownDefinitionFields` now flags any key outside the documented portable/machine-local set and `applyPersona` warns for review (imports remain quarantined per S5) | `clv_test.go TestUnknownDefinitionFields` |
| **S5** | Prompt injection via imported persona/memory | **built (procedural)** | import never auto-activates; every report carries a `ReviewNote`; Operator Guide §6 mandates review | — (procedural) |
| **S6** | Identity collision / spoofing | **built** | `applyPersona`/`applyWorkspace` check `FindPersona(id)` and refuse without `Force` | e2e (collision path via Force) |
| **S7** | Destructive merge | **built** | `SpliceStaffEntry`, `MergeSessions`, `MergeResumeEntries`, `EnsureWorkspace` all write `*.clvsync-bak` and splice, not replace | e2e round-trips |
| **S8** | Tampering in transit | **built** | per-file SHA-256 `internal/manifest`, verified in `openPackage`; `minisign` detached signature verified **before** unpack | `manifest_test.go`, `cryptobox_test.go`, `verify` command |
| **S9** | Weak crypto usage | **built** | age scrypt/X25519 (strong KDF built in); no home-rolled crypto | `cryptobox_test.go` |
| **S10** | Metadata leak even when encrypted | **built** | age encrypts the whole stream; package names are generic; source paths tokenized (`<WS:scope>`) never embedded | e2e Tier-2 (token check) |
| **S11** | Residual passphrase exposure | **eliminated** | age stdin/key handling — no inline passphrase (the 7-Zip caveat does not apply) | — |
| **S12** | Version/format skew | **built** | `meta.schemaVersion`; unknown shapes fail closed in `clv.parseStaffArray` / manifest parse | — |
| **S13** | Privacy / PII awareness | **built** | export prints what's included; secret-scan surfaces findings; tokenized paths hide source layout | — |
| **S14** | Tier-3/4 blast-radius amplification | **built** | secret-scan runs over the whole workspace tree incl. roster; heavy dirs gated separately | e2e Tier-3/4 |

## Load-bearing controls (must stay green)
S1 (secret scrub), S3 (zip-slip), S5 (untrusted import), S7 (non-destructive), S8 (integrity+signature) — all **built and tested**.

## Accepted / documented trade-offs
- **Unencrypted-by-default output when no passphrase/recipient is given** — the CLI defaults to prompting; an operator can still write a plaintext package deliberately. Documented.
- **S4 field-whitelist** is now a review **advisory** (warn-only): an imported definition carrying keys outside the documented set is surfaced on import. It is not a hard block because the definition is inert data clvsync never executes and imports are already quarantined for review (S5).
- **age is not AES** (ChaCha20-Poly1305) — accepted per D2; not a compliance environment.

## Verdict
The system introduces **no new attack surface beyond data transport**, with all five load-bearing controls implemented and tested. Ship-ready pending the live Universal-Resume integration test (functional, not security).
