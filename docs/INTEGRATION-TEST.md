# Integration Test — Tier 2 Universal Resume across two machines

The one thing the automated suite can't cover: whether an imported session is actually **offered for resume** by the target's Universal Resume, **under a different model/provider**. This runbook exercises exactly that. Run it before cutting a release.

**Prerequisites**
- Two machines, both Clairvoyance **≥ 0.77.0** (Universal Resume).
- **Machine A** (source): has a persona with at least one **real, resumable session** (talk to it once so a session exists).
- **Machine B** (target): ideally configured to run a **different model/provider** than A (e.g. A = Claude Opus, B = Sonnet / a local model / Codex) — that's the whole point of the test.

---

## Step 0 — (BOTH machines) install & prep the sync system

This exercises the setup runbook itself ([AGENTS.md](../AGENTS.md)), so the install path
is validated, not assumed. Do this on **A and B**.

**0a. Obtain `clvsync`.** Either download the build for this OS from GitHub **Releases** and
verify it, or build from a clone:

```sh
# Option 1 — prebuilt (once a release exists):
#   download clvsync-<os>-<arch>[.exe] + SHA256SUMS, then:
sha256sum -c SHA256SUMS 2>/dev/null | grep "$(uname -s | tr A-Z a-z)" || echo "check your line"
# Option 2 — from source:
go build -o clvsync ./cmd/clvsync
./clvsync datadir        # smoke: prints this machine's Clairvoyance data dir
```
> Pre-release note: no signed Release binaries exist until `v0.1.0` is tagged, so tonight
> you'll use **Option 2 (build from source)** on both machines. The Release/verify path is
> what the tag's CI job produces.

**0b. Create the Sync Operator staff.** In Clairvoyance on each machine, add a Staff member
named **Sync Operator** whose **knowledge template is `Sync Operator`** (content:
[personas/Sync Operator.md](../personas/Sync%20Operator.md)). The `knowledgeTemplate` marker
is what arms the S15 guard.

**0c. Confirm the guard recognizes it** (proves the marker took):

```sh
./clvsync export --persona "Sync Operator" --out op.cvpkg
# expect: REFUSED with an S15 message telling you to pass --allow-operator-sync
```
If that export is refused, the operator is correctly recognized as machine-local. If it
*succeeds*, the persona's `knowledgeTemplate` marker is missing — fix it before continuing.

| Check | A | B |
| ----- | - | - |
| `clvsync` obtained + `datadir` smoke works | ☐ | ☐ |
| Sync Operator staff created with the `Sync Operator` template | ☐ | ☐ |
| Operator export refused by the S15 guard | ☐ | ☐ |

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

Confirms the create-or-merge sync preserves each machine's local runtime **and** that new
work done on B travels home. On **B**, note the persona's model/runtime (they should be B's,
not A's — machine-local is preserved).

### 6a — do some real work on B, then have it record a result in staff memory

Give the resumed persona a task on B (e.g. installing the Clairvoyance Versioning Backup
System). Then — **before exporting** — have it jot a short outcome into its **Clairvoyance
staff memory**, so the result is a durable artifact that syncs home rather than living only
in the conversation transcript.

> Why this matters: `clvsync` captures the Clairvoyance staff-memory dir
> (`<data-dir>/.Clairvoyance/staff/<persona-slug>/`) plus history and resume — but **not**
> the Claude Code `~/.claude` project memory. A note written into staff memory is the
> reliable way to carry a finding back; it also gives the sync-back a concrete file whose
> arrival on A you can verify.

Ask the persona on B, in chat, something like:

> "Append a short dated note to your Clairvoyance staff memory recording the result of the
> backup-system install — what worked, what didn't, and any fresh-machine gotchas."

That writes to, e.g. (persona "Reegor" → slug `reegor`):

```
<B-data-dir>/.Clairvoyance/staff/reegor/index.md      # or a new file in that folder
```

Confirm the file exists on B before you export (it's what you'll check for on A):

```sh
ls "<B-data-dir>/.Clairvoyance/staff/reegor/"
```

### 6b — export B → A and sync back

```sh
# On B:
./clvsync export --persona "<Name>" --tier 2 --out back.cvpkg.age   # CLVSYNC_PASSPHRASE set
tar -tf back.cvpkg.age 2>/dev/null | grep memory/   # (unencrypted only) sanity: the note is inside
# Transport back to A, then on A (app closed):
./clvsync import --in back.cvpkg.age --dry-run        # preview: expect "merge — machine-local preserved [...]"
#                                                     #  and "memory/home: +N new / ~N updated"
./clvsync import --in back.cvpkg.age --receipt rt.json
./clvsync verify-import --receipt rt.json             # after restart
```

Because Reegor already exists on A this is a **merge**, so expect a **history divergence
warning** (A's session and B's session are different branches — A's transcript is preserved
as `*.clvsync-bak`, and both sessions stay resumable). That warning is the feature working.

### 6c — confirm the B-side note arrived on A

```sh
# On A, the staff-memory note written on B should now be present (its prior version, if any,
# is preserved as .clvsync-bak):
ls "<A-data-dir>/.Clairvoyance/staff/reegor/"
```

| Check | Result |
| ----- | ------ |
| B's persona kept **B's** model/runtime after the A→B import (machine-local preserved) | ☐ yes ☐ no |
| B wrote the outcome note into its staff memory before export | ☐ yes ☐ no |
| Sync-back dry-run reported a **merge** (not a blind overwrite) with machine-local preserved | ☐ yes ☐ no |
| Sync-back showed the expected **history divergence** warning (A's transcript kept as `.clvsync-bak`) | ☐ yes ☐ no |
| After sync-back, A still has **A's** model/runtime (not B's) | ☐ yes ☐ no |
| **The B-side staff-memory note is present on A** after the sync-back | ☐ yes ☐ no |
| verify-import all `[PASS]` on both machines | ☐ yes ☐ no |

*(The Sync Operator guard was already validated in Step 0c.)*
