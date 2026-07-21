# Sync Operator & Install Test — the assisted (non-CLI) path, installed from GitHub

`INTEGRATION-TEST.md` proves the **CLI** path. This runbook proves the **assisted** path: a
fresh machine that installs Persona Sync **from GitHub**, stands up the **Sync Operator** Staff
member, and does an import driven by the operator in plain language — no terminal knowledge
assumed of the end user. Run it before relying on the assisted path for real adopters.

**Prerequisites**
- A **new/fresh** computer with Clairvoyance **≥ 0.77.0** (Windows for this runbook; adapt paths
  for macOS/Linux).
- A **test package** to import — e.g. a Tier-2 `reegor.cvpkg.age` — plus its passphrase.
- GitHub access to `bubomortis/clairvoyance-persona-sync`.

> The point is to touch nothing you built by hand tonight: fetch everything the way a real
> adopter would, from the GitHub Release and repo.

---

## Part 1 — Install Persona Sync from GitHub

Do **1A** (manual) to establish ground truth. Optionally also do **1B** to test the
Staff-driven runbook.

### 1A — Manual install from the Release
1. Open the Releases page: **https://github.com/bubomortis/clairvoyance-persona-sync/releases**
2. From the latest release, download the two files for this OS/arch:
   - `clvsync-windows-amd64.exe` (or `-arm64`)
   - `SHA256SUMS`
3. **Verify the checksum** in PowerShell:
   `Get-FileHash .\clvsync-windows-amd64.exe -Algorithm SHA256`
   Compare the hash to the matching line in `SHA256SUMS`.
   > Note: releases are currently **checksum-verified only** — `minisign` signing of release
   > assets is a pending enhancement. Treat a checksum match as the integrity bar for now.
4. Rename to `clvsync.exe` and put it on `PATH` (or a known folder you invoke it from).
   - ☐ **Acceptance:** `.\clvsync.exe datadir` prints this machine's Clairvoyance data dir.
     Note that path — call it `<DATA>` below.
5. Get the **Sync Operator template** from the repo (either clone, or download the raw file):
   - Clone: `git clone https://github.com/bubomortis/clairvoyance-persona-sync.git`
   - or download raw: `personas/Sync Operator.md` (and `AGENTS.md` for reference)
6. **Place the template so it's selectable:** copy `Sync Operator.md` into `<DATA>\neurons\personas\`.
   - ☐ **Acceptance:** in New/Edit Staff, **"Sync Operator"** now appears as a **Knowledge Base** option.

### 1B — Staff-driven install (tests `AGENTS.md` §1) — optional
- Paste to an existing Staff member (e.g. the default assistant): **"Set up Clairvoyance Persona
  Sync on this machine."**
- The agent should follow `AGENTS.md`: download + checksum-verify the binary, place it, create the
  operator, and confirm the guard.
- ☐ **Acceptance:** it completes the steps and reports done. **Record any step it could not do**
  (no shell access, couldn't place the template, couldn't reach GitHub) — those are runbook gaps.

---

## Part 2 — Create the Sync Operator Staff member
1. **New Staff** →
   - **Name:** `Sync Operator`
   - **Knowledge Base:** `Sync Operator` (the template placed in Part 1)
   - **Provider:** Claude (or whatever this machine runs)
   - **Permission / tools:** **enable shell access** — the operator must be able to run
     `clvsync.exe`. A locked-down persona cannot do its job.
   - **Interaction Mode:** Direct (ACP)
   - Save.
   > The `knowledgeTemplate: "Sync Operator"` marker is load-bearing — it arms the S15 self-sync guard.
2. ☐ **Acceptance:** the operator appears in the Staff panel, and when you ask it to run
   `clvsync datadir` it returns this machine's data dir (proves shell access works).

---

## Part 3 — Guard check (S15)
Ask the operator (or run directly): export the Sync Operator persona —
`.\clvsync.exe export --persona "Sync Operator" --out op.cvpkg`
- ☐ **Acceptance:** **REFUSED** with the S15 message (telling you to pass `--allow-operator-sync`
  only if deliberate). If it *succeeds*, the template marker is missing — fix the operator's
  Knowledge Base before continuing.
- ☐ Ask the operator to explain **why** it won't sync itself. It should say it's machine-local
  infrastructure, not portable content.

---

## Part 4 — Assisted import through the operator (the main event)
1. Put the test package (e.g. `reegor.cvpkg.age`) somewhere ordinary — **Downloads, Desktop, or a
   USB drive** (no fixed folder required).
2. Open the **Sync Operator** and paste, in plain language:
   > "Import the Clairvoyance package at `<path-to-.cvpkg>`. I'll give you the passphrase when you ask."
3. **Intake (app open).** The operator should:
   - run `clvsync verify` (signature/integrity),
   - run `clvsync import --dry-run` and **narrate the plan** — which persona/workspace, portable
     fields updating, machine-local preserved, memory/history/sessions merging, and (on a fresh
     create) any `shell.cwd` **auto-repoint**,
   - **ask for explicit confirmation.**
   - ☐ **Acceptance #1:** the narration is accurate and it **waits for your "yes"** — no auto-apply.
4. **Finish (app closed).** The operator guides you to **close Clairvoyance**, then run the
   finisher it prepared (or a one-line command it gives you):
   `clvsync import --in <pkg> --data-dir "<DATA>" --receipt import-receipt.json`
   - ☐ **Acceptance #2:** import succeeds app-closed; `import-receipt.json` is written.
   - **This is the riskiest step** — note whether the close/hand-off/reopen dance felt smooth or
     clunky, and whether the operator's instructions were followable without terminal knowledge.
5. **Verify (app open, on restart).** Reopen Clairvoyance, open the operator, and ask it to verify.
   It runs `clvsync verify-import --receipt import-receipt.json` and narrates the pass/fail table.
   - ☐ **Acceptance #3:** all `[PASS]`; the imported persona is present and (Tier 2) resumable.
6. ☐ **Acceptance #4 (usability):** could a person who won't open a terminal have followed this
   start to finish? Note every point where the operator assumed CLI knowledge or its guidance was
   unclear.

---

## Part 5 — Blocked-export handling (credential hygiene) — optional
Plant a fake token (e.g. a `github_pat_` string) in a throwaway persona's memory, then ask the
operator to export it.
- ☐ **Acceptance:** the operator reports a **credential leaked into the conversation** and walks you
  through **rotate → store in Settings → Credentials → scrub (app closed) → re-export** — it does
  **not** reach for `--allow-secrets`.

---

## What to report

| Check | Result |
| ----- | ------ |
| Binary installed from Release + checksum verified | ☐ |
| `Sync Operator` selectable as a Knowledge Base | ☐ |
| Operator created with working shell access | ☐ |
| Guard refuses to export the operator (S15) | ☐ |
| Operator gives an accurate dry-run narration + waits for confirm | ☐ |
| App-closed finisher succeeds; receipt written | ☐ |
| `verify-import` all PASS on restart | ☐ |
| A non-CLI user could have followed it | ☐ |
| Blocked-export → rotate guidance (not `--allow-secrets`) | ☐ |
| Staff-driven install (1B) completed | ☐ |

Capture any rough edges in: **template placement**, **operator shell access**, the **app-closed
handoff smoothness**, and **guidance clarity**. Those are the acceptance bar for the assisted path
— fixes fold into `v0.1.2`.
