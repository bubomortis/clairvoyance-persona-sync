# Integration Test — Tier 2 Universal Resume across two machines

The one thing the automated suite can't cover: whether an imported session is actually **offered for resume** by the target's Universal Resume, **under a different model/provider**. This runbook exercises exactly that. Run it before cutting a release.

**Prerequisites**
- Two machines, both Clairvoyance **≥ 0.77.0** (Universal Resume) with `clvsync` built.
- **Machine A** (source): has a persona with at least one **real, resumable session** (talk to it once so a session exists).
- **Machine B** (target): ideally configured to run a **different model/provider** than A (e.g. A = Claude Opus, B = Sonnet / a local model / Codex) — that's the whole point of the test.

---

## Step 1 — (Machine A) confirm a session exists

```sh
# The persona should have an agent-sessions.json record + agent-history transcript.
./clvsync export --persona "<Name>" --tier 2 --out probe.cvpkg
tar -xOf probe.cvpkg meta.json | grep '"tier"'          # expect "tier": 2
tar -tf probe.cvpkg | grep resume/                      # expect resume/records.json etc.
rm probe.cvpkg
```
If `tier` is 1 (no `resume/`), the persona has no live session yet — chat with it once and retry.

## Step 2 — (Machine A) export Tier 2, encrypted

```sh
export CLVSYNC_PASSPHRASE='pick-a-strong-one'
./clvsync export --persona "<Name>" --tier 2 --out persona.cvpkg.age
```
Note the exported `sessionId`(s) for later:
```sh
# (unencrypted peek only if you must; otherwise skip)
```

## Step 3 — transport
Copy `persona.cvpkg.age` to Machine B. **Send the passphrase separately** (not with the file).

## Step 4 — (Machine B) preview, then import with Clairvoyance CLOSED

Preview first (writes nothing), then apply and emit a receipt:

```sh
export CLVSYNC_PASSPHRASE='the-passphrase'
./clvsync import --in persona.cvpkg.age --dry-run          # preview the plan
# If the persona's workspace(s) don't exist on B, prep or pass --workspace-path.
./clvsync import --in persona.cvpkg.age --receipt receipt.json
```
Expect `placed: [... resume/sessions resume/summaries ...]`. If you see `skipped scopes: session:<ws>`, Machine B lacks that workspace — create it (workspace-prep) and re-import.

## Step 4b — (Machine B) reconcile the receipt after restart

After starting Clairvoyance on B, confirm the on-disk import is correct:

```sh
./clvsync verify-import --receipt receipt.json
```
Expect all `[PASS]` rows (persona present, files hash-match, session registered). This
proves registration; the *resume offer* itself is the UI check in Step 5.

## Step 5 — (Machine B) start Clairvoyance and RESUME

1. Start Clairvoyance on B.
2. Open the imported persona.
3. Look for the **resumable session** in its history / resume UI and **resume it**.
4. Confirm:
   - the **prior conversation context is present** (it continues, doesn't start blank), and
   - it now runs under **B's model/provider**, not A's.

---

## What to report back

| Check | Result |
| ----- | ------ |
| Import reported `resume/sessions` placed | ☐ yes ☐ no |
| Session appears on B and is offered for resume | ☐ yes ☐ no |
| Resuming continues with full prior context | ☐ yes ☐ no |
| It runs under B's (different) model | ☐ yes ☐ no |
| Any errors / surprises (paste them) | |

**If resume works:** cut the `v0.1.0` release.
**If the session lands but isn't offered / doesn't resume:** capture B's `agent-sessions.json` record for that `sessionId` (path/id remapped correctly?) and any app log — that tells us whether it's a packaging gap or an app-side registration nuance to adapt to.

Either way it's non-destructive: Tier 1 (identity + full history) is already imported, so worst case the persona is present with its history readable, just not live-resumed.

---

## Step 6 (optional) — round-trip sync-back (exercises Phase 6)

Confirms the create-or-merge sync preserves each machine's local runtime. On **B**, note
the persona's model/runtime (they should be B's, not A's — machine-local is preserved).
Then chat with it once on B, export **B → A**, and sync back into A:

```sh
# On B:
./clvsync export --persona "<Name>" --tier 2 --out back.cvpkg.age   # CLVSYNC_PASSPHRASE set
# Transport back to A, then on A (app closed):
./clvsync import --in back.cvpkg.age --dry-run        # preview: expect "merge — machine-local preserved [...]"
./clvsync import --in back.cvpkg.age --receipt rt.json
./clvsync verify-import --receipt rt.json             # after restart
```

| Check | Result |
| ----- | ------ |
| B's persona kept **B's** model/runtime after the A→B import (machine-local preserved) | ☐ yes ☐ no |
| Sync-back dry-run reported a **merge** (not a blind overwrite) with machine-local preserved | ☐ yes ☐ no |
| After sync-back, A still has **A's** model/runtime (not B's) | ☐ yes ☐ no |
| verify-import all `[PASS]` on both machines | ☐ yes ☐ no |

## Step 7 (optional) — the Sync Operator guard sanity check

Confirm you cannot accidentally sync the operator:

```sh
./clvsync export --persona "Sync Operator" --out op.cvpkg   # expect: refused (S15)
```
Expect an error telling you to pass `--allow-operator-sync`. That's the guard working.
