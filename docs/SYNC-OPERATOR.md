# The Sync Operator & round-trip sync (Phase 6–7)

This is the friendly front-end to `clvsync`: a dedicated Staff member — the **Sync
Operator** — created on each machine, plus the create-or-merge semantics that make it safe
to sync the *same* persona back and forth without clobbering each machine's local runtime.

## Round-trip sync semantics (Phase 6, §17)

The common workflow is transporting one persona between two machines repeatedly. A staff
definition (`staff.json` entry) mixes two kinds of fields:

| Class | Fields | On a sync-merge |
| ----- | ------ | --------------- |
| **Portable** | `name`, `jobDescription`, `knowledgeTemplate`, `interactionMode`, `type`, `wiggumMode` | **updated** from the incoming package |
| **Machine-local** | `ai`, `model`, `runtime`, `shell`, `status`, `isDefault`, `activity`, `createdAt` | **preserved** — the destination's values are kept |

The rule is *preserve-unless-portable*: the merge starts from the destination entry and only
overwrites the known-portable keys. Any field this build doesn't recognize is preserved, so a
future machine-local field can never be silently clobbered by a round-trip.

**Modes** (`--mode`, default `sync`):

- **`sync`** — create if absent, else component-merge: definition per the table above; memory
  is a per-file **union** (identical files skipped, changed files backed up then updated);
  history is **newest-wins** by `savedAt` (else more-messages-wins) with a `.clvsync-bak` of
  the replaced transcript and a **divergence warning**; sessions merge by `sessionId`.
- **`overwrite`** — replace the entry and files wholesale (machine-local included).
- **`skip`** — leave an existing persona untouched.

**`--dry-run`** computes the exact plan and writes nothing — always preview before applying.

## The Sync Operator persona (§19.1)

A Staff member named **Sync Operator** with knowledge template **`Sync Operator`**, created on
each machine by the setup runbook (`AGENTS.md`). It is the plain-language front-end: "import
the package on my Desktop" instead of a shell command.

### The app-closed paradox

The operator lives *inside* the running app, but the destructive part of import must run with
the app **closed** (`staff.json` and `clairvoyance-store.json` are app-owned at runtime).
So the operator's role is **prepare + preview + hand off a one-click finisher**, in three phases:

| Phase | App state | Actions |
| ----- | --------- | ------- |
| **Intake** | open | locate the `.cvpkg` anywhere (`--in <path>`), `verify`, `import --dry-run`, narrate preview, confirm |
| **Finish** | **closed** | run `import --receipt import-receipt.json`; it writes the receipt |
| **Verify** | open (restart) | `verify-import --receipt import-receipt.json`, narrate the pass/fail table |

## Post-import verification (§19.2)

The finisher writes `import-receipt.json` — tier, mode, persona id, the portable fields it
updated, the machine-local fields it preserved, session ids, target workspace id, and a
SHA-256 for every placed file. On restart, `clvsync verify-import --receipt <file>` does a
read-only **expected-vs-actual reconciliation**:

- persona definition present, portable updated + machine-local preserved;
- every placed file hash-matches the receipt;
- each session id is registered in `agent-sessions.json`;
- the workspace is registered (Tier 3/4).

**App-owned aggregates are advisory, not failures (§23.4).** Once the app reopens it
rewrites its own aggregate files — the profile `staff.json` roster and the
`agent-history/<id>.json` transcripts it re-flushes from memory — so their import-time
hashes no longer match. `verify-import` classifies those as **`NOTE`** rows (reconciliation
still passes) rather than `FAIL`, because a post-restart rewrite is expected, not
corruption. Your persona's own definition and its static curated `.clairvoyance/staff`
memory are still verified **strictly**; a real mismatch there — or a *missing* aggregate —
is still a `FAIL`.

**Boundary:** this proves correct **on-disk registration**. Whether the session is actually
*offered for resume* is a human eyeball check in the UI (that's what the two-machine
integration test covers).

## Hardened app-closed finisher (§23)

The plain "operator runs `import` with the app closed" step has a chicken-and-egg: the
operator runs *inside* Clairvoyance, so the moment it closes the app its own turn ends — it
cannot then run the import. `scripts/clvsync-import-runner.ps1` resolves this by moving the
Finish phase into a detached, OS-owned **one-shot Scheduled Task** that outlives both the
app and the operator's turn.

The runner, in order:

1. Logs to a stable work dir and **snapshots** the app-owned aggregates (`staff.json` per
   profile + `clairvoyance-store.json`) as belt-and-suspenders over clvsync's own
   `*.clvsync-bak`.
2. Gracefully closes Clairvoyance (`CloseMainWindow` → force-kill survivors) and then
   **waits for the single-instance lock to settle** — a process-stability window — before
   importing. Relaunching before the lock clears makes the new instance immediately quit,
   which is exactly the failure the first test run hit.
3. Runs `clvsync import --in <pkg> --receipt <file> --mode sync`, capturing the exit code
   and output.
4. Makes a **best-effort relaunch with retries that is decoupled from import success** —
   the import is the safe critical operation; a relaunch that fails degrades to a logged
   "start Clairvoyance yourself" note and **never** risks the imported data.
5. Writes `import-done.json` (`importOk`, `relaunchOk`, timestamps, log/receipt paths) for
   the operator to read on the next session, and self-deletes its one-shot task (keeping the
   log, receipt, and marker).

**Two-turn approval gate (hard rule).** The operator **stages** the runner and **registers**
the task on one turn, shows the user exactly what it will do, and stops — triggering nothing.
Only on a *separate* explicit approval does it trigger the task (at which point the app
closes and the operator's turn may end, as expected). Never stage-and-trigger in one turn.

**Secrets.** The runner never takes or logs a passphrase. The identity credential model
needs none; for a passphrase model, `CLVSYNC_PASSPHRASE` must be present in the task's own
session environment (never as a task argument, never in the script or log).

## Don't sync the operator (S15, §19.3)

The operator is machine-local infrastructure. Syncing it can clobber the destination's
operator or corrupt an in-flight import. `clvsync` recognizes the operator by its
`knowledgeTemplate: "Sync Operator"` marker (authoritative; display name is a secondary
match) and guards both ends:

- **Export** warns and refuses unless `--allow-operator-sync`.
- **Import** blocks by default, and **hard-blocks (no override honored)** when the incoming
  id matches the operator currently running the import (self-overwrite). A *different*
  operator id may proceed only with `--allow-operator-sync`.

A deliberate operator migration (seeding a brand-new machine) is still possible with the
flag — you just can't trip into it by accident.
