# Sync Operator & Install Test — the assisted (non-CLI) path, installed from GitHub

`INTEGRATION-TEST.md` proves the **CLI** path. This runbook proves the **assisted** path: a fresh
machine where a **Clairvoyance Staff agent installs Persona Sync from GitHub** (the user never opens
a terminal), stands up the **Sync Operator**, and runs an import driven by the operator in plain
language. Run it before relying on the assisted path for real adopters.

**Prerequisites**
- A **new/fresh** computer with Clairvoyance **≥ 0.77.0** (Windows for this runbook; adapt paths for
  macOS/Linux).
- At least one existing Staff member on that machine that can run a shell (e.g. the default
  assistant) to perform the install.
- A **test package** to import — e.g. a Tier-2 `reegor.cvpkg.age` — plus its passphrase.

> The whole point is that a Staff agent does the fetch-and-install the way a real adopter would —
> the same "ask Clairvoyance to install it" convention as the backup system. You only approve.

---

## Part 1 — Staff-directed install from GitHub (the path under test)

### 1A — Have a Staff agent install it
On the fresh machine, paste to a Staff member that can run a shell:

> **"Install Clairvoyance Persona Sync on this machine from GitHub."**

The agent should follow the repo's [`AGENTS.md`](../AGENTS.md) §1 — the authoritative install
procedure — and:
1. confirm prerequisites,
2. **fetch the binary from the latest Release and verify its `SHA256SUMS` checksum** before using it,
3. place it on `PATH` (confirm `clvsync datadir` prints the data dir — call it `<DATA>`),
4. place `personas/Sync Operator.md` into `<DATA>\neurons\personas\`,
5. **create the Sync Operator** Staff member (Knowledge Base `Sync Operator`, with shell access) —
   pausing for **your approval**,
6. **arm-check the guard** (`clvsync export --persona "Sync Operator"` must be refused, S15),
7. report done.

Watch that the agent behaves per the install hard rules — **integrity-checked** (verifies the checksum
before using the binary), **attended** (asks before creating the operator / granting shell access),
**idempotent** (detects an existing install and stops), reports each command + result, and does
**not** try to modify the source repo.

- ☐ **Acceptance:** the agent completes the install and reports done.
- ☐ **Record every step it could not do** — no shell access, couldn't reach GitHub, couldn't place
  the template, couldn't create Staff, checksum step skipped. Each is a gap in `AGENTS.md` to fix.

### 1B — Manual fallback / spot-check (reference only)
If the agent gets stuck, or to verify its work by hand:
1. From the Releases page (`github.com/bubomortis/clairvoyance-persona-sync/releases`), download
   `clvsync-windows-amd64.exe` + `SHA256SUMS`; verify with
   `Get-FileHash .\clvsync-windows-amd64.exe -Algorithm SHA256`; rename to `clvsync.exe` on `PATH`.
2. Copy `personas/Sync Operator.md` (clone or raw file) into `<DATA>\neurons\personas\`.
3. New Staff → Name `Sync Operator`, Knowledge Base `Sync Operator`, **shell access enabled**,
   Interaction Direct (ACP).

### Part 1 ground-truth checks (however it was installed)
- ☐ `clvsync datadir` prints this machine's data dir.
- ☐ **"Sync Operator"** appears as a **Knowledge Base** option in New/Edit Staff.
- ☐ The operator, asked to run `clvsync datadir`, returns the data dir (proves **shell access**).
- ☐ `clvsync export --persona "Sync Operator" --out op.cvpkg` is **refused** (S15). If it succeeds,
  the template marker didn't take — fix the operator's Knowledge Base before continuing.
- ☐ Ask the operator **why** it won't sync itself — it should say it's machine-local infrastructure.

---

## Part 2 — Assisted import through the operator (the main event)
1. Put the test package (e.g. `reegor.cvpkg.age`) somewhere ordinary — **Downloads, Desktop, or a
   USB drive** (no fixed folder required).
2. Open the **Sync Operator** and paste, in plain language:
   > "Import the Clairvoyance package at `<path-to-.cvpkg>`. I'll give you the passphrase when you ask."
3. **Intake (app open).** The operator should run `clvsync verify`, then `clvsync import --dry-run`,
   and **narrate the plan** — which persona/workspace, portable fields updating, machine-local
   preserved, memory/history/sessions merging, and (on a fresh create) any `shell.cwd` **auto-repoint**
   — then **ask for explicit confirmation.**
   - ☐ **Acceptance #1:** the narration is accurate and it **waits for your "yes"** — no auto-apply.
4. **Finish (app closed).** The operator guides you to **close Clairvoyance**, then run the finisher
   it prepared: `clvsync import --in <pkg> --data-dir "<DATA>" --receipt import-receipt.json`.
   - ☐ **Acceptance #2:** import succeeds app-closed; `import-receipt.json` is written.
   - **Riskiest step** — note whether the close/hand-off/reopen dance felt smooth and whether the
     operator's instructions were followable without terminal knowledge.
5. **Verify (app open, on restart).** Reopen Clairvoyance, open the operator, ask it to verify. It
   runs `clvsync verify-import --receipt import-receipt.json` and narrates the pass/fail table.
   - ☐ **Acceptance #3:** all `[PASS]`; the imported persona is present and (Tier 2) resumable.
6. ☐ **Acceptance #4 (usability):** could a person who won't open a terminal have followed this start
   to finish? Note every point where the operator assumed CLI knowledge or was unclear.

---

## Part 3 — Blocked-export handling (credential hygiene) — optional
Plant a fake token (e.g. a `github_pat_` string) in a throwaway persona's memory, then ask the
operator to export it.
- ☐ **Acceptance:** the operator reports a **credential leaked into the conversation** and walks you
  through **rotate → store in Settings → Credentials → scrub (app closed) → re-export** — it does
  **not** reach for `--allow-secrets`.

---

## What to report

| Check | Result |
| ----- | ------ |
| Staff agent installed it from GitHub, following `AGENTS.md` | ☐ |
| Binary checksum verified before use (integrity, not authorship) | ☐ |
| Agent asked before creating the operator / granting shell access (attended) | ☐ |
| `Sync Operator` selectable as a Knowledge Base | ☐ |
| Operator has working shell access | ☐ |
| Guard refuses to export the operator (S15) | ☐ |
| Operator gives an accurate dry-run narration + waits for confirm | ☐ |
| App-closed finisher succeeds; receipt written | ☐ |
| `verify-import` all PASS on restart | ☐ |
| A non-CLI user could have followed it | ☐ |
| Blocked-export → rotate guidance (not `--allow-secrets`) | ☐ |

Capture any rough edges in: the **Staff-driven install** (`AGENTS.md` gaps), **template placement**,
**operator shell access**, the **app-closed handoff smoothness**, and **guidance clarity**. Those are
the acceptance bar for the assisted path — fixes fold into `v0.1.2`.
