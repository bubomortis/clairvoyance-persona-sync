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

## Step 4 — (Machine B) import with Clairvoyance CLOSED

```sh
# If the persona's workspace(s) don't exist on B, prep or pass --workspace-path.
export CLVSYNC_PASSPHRASE='the-passphrase'
./clvsync import --in persona.cvpkg.age
```
Expect `placed: [... resume/sessions resume/summaries ...]`. If you see `skipped scopes: session:<ws>`, Machine B lacks that workspace — create it (workspace-prep) and re-import.

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
