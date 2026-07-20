# clairvoyance-persona-sync (`clvsync`)

Transport a Clairvoyance **Staff member** — or a whole **workspace** — from one machine to another: identity + accumulated memory + conversation history, optionally with a resumable session, packaged as a small, verifiable, optionally-encrypted artifact.

A Staff member isn't one file — it's a definition (`profiles/{id}/staff.json` entry), a custom persona template, per-workspace memory (`.Clairvoyance/staff/{name}/`), and history (`agent-history/staff-{id}.json`). `clvsync` gathers those, scrubs anything that shouldn't leave the machine, and re-homes them on the target.

> ⚠️ **Status: early build (Phase 0 complete).** The security-critical core is implemented and unit-tested; the export/import commands are being layered on. Not yet ready for general use. See [Roadmap](#roadmap).

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

## Build

Requires Go ≥ 1.26.

```sh
go build ./...
go test ./...
./clvsync datadir      # prints the resolved Clairvoyance data dir for this OS
```

Cross-platform: resolves the Clairvoyance data directory per OS (Windows `%APPDATA%`, macOS `~/Library/Application Support`, Linux `~/.config`).

## Roadmap

- [x] **Phase 0** — core: per-OS data-dir resolver, secret scanner, safe-path guard, SHA-256 manifest, CLI skeleton *(done, unit-tested)*
- [ ] **Phase 1** — Tier 1 export/import (`age` + `minisign`), round-trip self-test
- [ ] **Phase 2** — Tier 2 Universal Resume
- [ ] **Phase 3** — Tier 3 workspace + `workspace-prep`
- [ ] **Phase 4** — Tier 4 heavy add-on + space-aware fail-down
- [ ] **Phase 5** — harden, docs, signed cross-platform release

## License

MIT — see [LICENSE](LICENSE). Provided as-is, without warranty.
