# AGENTS.md — have Clairvoyance Staff install and run clvsync for you

Instructions for an AI agent (Clairvoyance Staff or similar) asked to **install, set up, or run**
Persona Sync on a user's machine. The user shouldn't have to touch a terminal — they paste one of
the prompts below to a Staff member, and the agent does the work by following the steps here.

**Repo:** `github.com/bubomortis/clairvoyance-persona-sync` · **Releases:**
`github.com/bubomortis/clairvoyance-persona-sync/releases`

---

## 1. Install Persona Sync on a machine (Staff-directed, from GitHub)

> **Prompt to Staff:** "Install Clairvoyance Persona Sync on this machine from GitHub."

This section is the **authoritative install procedure** — read it fully and follow it in order.

**Prerequisites to confirm (report and stop if any is missing):** Clairvoyance ≥ 0.77.0 (Universal
Resume); network access to `github.com`; a shell the agent can run `clvsync` from.

**Idempotency first (deterministic gate):** before changing anything, run **`clvsync status`** if a
`clvsync` is already on `PATH`. It reports the version, data dir, whether a **Sync Operator** exists
(and flags DUPLICATES), and whether a newer release is available. Decide from its output:
- `clvsync` runs **and** "Sync Operator: present" → the install is complete. If it also says
  "UPDATE AVAILABLE", offer to run **`clvsync update`** (downloads + checksum-verifies the latest
  release and replaces the binary; nothing else changes). Otherwise report the existing install and
  **stop** — do not recreate anything.
- `clvsync` runs but "Sync Operator: NOT present" → skip step 1 (binary is installed); go to step 2
  to place the template and create the operator.
- "Sync Operator: DUPLICATE" → stop and tell the user to remove the extra operator(s) first.
- `clvsync` is not found → do the full install below.

Steps:

0. **Check for an existing install** with `clvsync status` (see the gate above) and branch accordingly.
1. **Fetch the binary from the Release (verify its integrity).** From the latest release, download the
   build for this OS/arch (`clvsync-<os>-<arch>[.exe]`) **and** `SHA256SUMS`. **Verify the checksum**
   (`Get-FileHash <file> -Algorithm SHA256` vs the matching `SHA256SUMS` line) and refuse to use a
   binary that doesn't match. *(Release `minisign` signatures are a pending enhancement; verify them
   too once published.)* If no prebuilt binary exists for this platform, fall back to
   `go build ./cmd/clvsync` from a clone. Place the binary on `PATH`.
   - ✅ Confirm `clvsync datadir` prints this machine's Clairvoyance data dir (call it `<DATA>`).
2. **Place the Sync Operator template.** Copy `personas/Sync Operator.md` (from a clone or the raw
   repo file at the release tag) into `<DATA>/neurons/personas/` so it becomes a selectable
   **Knowledge Base**.
3. **Create the Sync Operator persona (attended — get the user's OK first).** Add a Staff member
   named **Sync Operator**, Knowledge Base **`Sync Operator`**, with **shell access** so it can run
   `clvsync`. The `knowledgeTemplate: "Sync Operator"` marker is load-bearing — it arms the S15
   self-sync guard.
4. **Arm-check the guard.** Run `clvsync export --persona "Sync Operator" --out op.cvpkg`; it MUST
   be **refused** (S15). If it succeeds, the template marker didn't take — fix the operator's
   Knowledge Base before proceeding. Delete `op.cvpkg` if one was written.
5. **Report done.** Tell the user they can now say "import the package on my Desktop" or "export my
   `<persona>` to send to my other machine."

**Install hard rules:**
- **Integrity-checked:** verify the binary's checksum against `SHA256SUMS` before using it (this proves
  integrity, not authorship — trust rests on GitHub + the publisher until release signatures land), and
  place only the repo's own template.
- **Attended:** get the user's explicit approval before creating the Sync Operator Staff member and
  granting it shell access.
- **Idempotent:** detect an existing valid install and stop rather than clobber it.
- **Do not modify this source repo** — no commits or pushes back to origin.
- **Report every command and its result.**

## 2. Assisted import

> **Prompt to Staff:** "Import the Clairvoyance package at `<path>`." *(path can be anywhere —
> Downloads, Desktop, a USB drive)*

Staff steps (this is the Sync Operator's job if present):

1. `clvsync verify --in <path>` — confirm signature + integrity.
2. `clvsync import --in <path> --dry-run` — **preview**. Read the plan to the user: persona/
   workspace, portable fields updating, machine-local fields preserved, memory/history/
   session merges. **Get explicit confirmation.**
3. Tell the user to **close Clairvoyance** (the import writes app-owned files).
4. Run the finisher: `clvsync import --in <path> --receipt import-receipt.json`
   (add `--mode overwrite|skip` only if the user asked; default is `sync` = create-or-merge).
   - **Robust option (recommended when *you* are the operator running inside the app).**
     Because closing the app ends your turn, hand the Finish phase to the hardened runner
     `scripts/clvsync-import-runner.ps1` staged as a detached **one-shot Scheduled Task** (it
     closes the app, waits for the single-instance lock to settle, imports, then relaunches
     best-effort — decoupled from import success — and writes `import-done.json`). Use the
     **two-turn gate**: on this turn, stage the script and register the task and **show the
     user what it will do without triggering it**; only on the user's *next, explicit*
     approval do you trigger the task. Never stage-and-trigger in one turn. See
     `docs/SYNC-OPERATOR.md` § "Hardened app-closed finisher."
5. Tell the user to **reopen Clairvoyance**, then run
   `clvsync verify-import --receipt import-receipt.json` and read back the pass/fail table.
   - Expect `NOTE` rows for the app-owned aggregates (`staff.json` / `agent-history`): the
     app rewrites those on reopen, so their hash mismatch is **expected, not corruption**
     (§23.4). The persona's own definition and curated memory are verified strictly; a real
     mismatch there — or a genuinely missing file — is a `FAIL`.
6. If the import placed **memory** (the report shows the "next session start" notice), tell the user
   the imported persona picks up its memory **only when its runtime (re)starts** — Clairvoyance injects
   Staff knowledge at session start, not continuously. Reopening the app (step 5) is that restart, so a
   persona created by this import loads its memory on first launch; a persona that was **already running**
   during an app-open import must be restarted before it will see the new memory.
7. Remind the user that whether the session is *offered for resume* is their check in the UI.

**Name reservation (automatic).** The import also reserves the persona's display name in
Clairvoyance's `staff-names.json` so the in-app Create Staff modal shows it as taken. It is
append-only, idempotent, and non-load-bearing (the persona loads fine regardless), so nothing
extra is needed. Pass `--no-name-reserve` if the user explicitly does not want the name reserved.

**Non-CLI fallback:** `clvsync import` with **no** `--in` runs a guided interactive prompt
(file → passphrase → preview → confirm → apply). A double-clickable `import.cmd` /
`import.command` can wrap that.

## 3. Assisted export

> **Prompt to Staff:** "Export my `<persona>` so I can move it to my other machine."

Staff steps:

1. **Ask where to save it.** On the **first** export, ask the user which folder to write the package
   to. On **later** exports, offer the last-used folder as the default — check it with
   `clvsync last-export-dir` and say e.g. *"I'll save it to `<that folder>` again — okay, or
   somewhere else?"* Never silently pick a location.
2. Set `CLVSYNC_PASSPHRASE` (Tier 2 includes the resumable session), then run:
   `clvsync export --persona "<name>" --tier 2 --out-dir "<folder>"`
   — `clvsync` auto-names the file and **remembers the folder** for next time. (Omit `--out-dir` to
   reuse the remembered folder; use `--out <full-path>` for a one-off exact filename.)
   - **Export never silently produces a plaintext package.** With no `CLVSYNC_PASSPHRASE` and no
     `--recipient`, a non-interactive run (which is you, the operator) is **refused** — set the
     passphrase (or a `--recipient` key), or pass `--plaintext` only if the user deliberately wants an
     unencrypted package. Do **not** collect the passphrase in chat; source it from Settings →
     Credentials or a `--recipient` public key.
3. Hand the user the resulting `.cvpkg.age` file and remind them to send the **passphrase out of
   band** (not with the file).

## 4. Choose the encryption credential model (D17 §20)

> **Prompt to Staff:** "Set up encryption so I can sync between my machines."

Offer the user a choice and store it with `clvsync cred model <name>` on **each** machine.
Recommend a tier-aware default; the user still decides:

- **`identity` (recommended for syncing your own machines).** A per-machine `age`
  keypair; the private half is DPAPI-sealed and never leaves the machine — nothing to
  type on export or import once paired. Set it up **on each machine**:
  1. `clvsync cred model identity --pairing travel` (or `--pairing cloud-sync` on Plus+).
  2. `clvsync cred init` — creates + seals this machine's identity; prints its **public** key.
  3. Exchange **public** keys (never the private key). Print this machine's key with
     `clvsync cred pubkey` (or write a name+key doc with `cred pubkey --out mine.pair`),
     then choose a pairing channel:
     - **`--pairing travel` (any tier):** hand the other machine your `age1…` key any way you
       like and run `clvsync cred pair --name <other-machine> --key <age1…>` (or
       `--in <pairing-doc>`) on each side. After the first exchange the sender's public key
       also rides inside each package and is trusted automatically.
     - **`--pairing cloud-sync` (Plus+):** publish the public key as a **Cloud-Synced note**
       and let it replicate to the other machine. Concretely, the operator:
       1. Publishes with **`create_note`** — `folder: "clvsync-pairing"`,
          `title: "pubkey-<this-hostname>"`, body = the `age1…` line **only**. (Use
          `create_note`, the local Notes store that Cloud Sync replicates — **not**
          `domain_notes`, which is org/domain-scoped and does not cross your own machines.)
          Writing the note is what starts propagation; Cloud Sync is a background
          watcher + periodic reconcile, so it is **eventual**, not instant.
       2. On the peer machine, **polls** for the synced note at
          `<data-dir>/notes/clvsync-pairing/pubkey-<peer-hostname>.md` (it also shows in the
          Notes sidebar) with a friendly "waiting for your other machine to sync" message and
          a timeout — never one-shot-fail. Reads the `age1…` line and runs
          `clvsync cred pair --name <peer-hostname> --key <age1…>`.
       Cloud Sync only replicates notes/docs/exhibits — it **never** touches the sealed
       private identity (kept outside that scope), so a pubkey note is safe by construction.
       Transparent pairing needs Plus+; already-paired machines keep working after a
       downgrade (keys are local), only **new** pairings lose the channel.
  After that, `clvsync export`/`import` need no passphrase — the model handles it.
- **`shared-passphrase`** — one passphrase both machines share. On each machine, verify
  `CLVSYNC_PASSPHRASE` is populated in **Settings → Credentials** (a fresh `credentials` read,
  not the session-start snapshot); if missing, ask the user to add it there — **the same value
  as the other machine**, entered in Credentials, **not in chat**.
- **`per-transfer`** — no stored secret; a fresh passphrase per transfer, sent out of band.

**Hard rules:** the **private** identity never leaves its machine (not to a note, not to
cloud, not to chat) — only the **public** key is ever shared. Never enter any passphrase in
chat. A peer whose public key **changed** is refused by `cred pair` — treat that as possible
impersonation and verify out of band before `cred unpair` + re-pair.

## Guard rails the agent must respect

- **Never** export or import the **Sync Operator** persona without the explicit
  `--allow-operator-sync` flag, and never when it would overwrite the operator running the
  import — `clvsync` will refuse, and so should you.
- **Never** send the passphrase together with the package.
- **Always** dry-run and get confirmation before a real import.
- Imported personas/memory are untrusted until reviewed — do not auto-activate them.
- **A blocked export = a real secret in the history.** If the secret scan stops an export, don't
  reflexively pass `--allow-secrets` — tell the user a credential leaked into the conversation and
  have them **rotate** it, **store** it in Settings → Credentials (not chat), and **scrub** the
  transcript (app closed) before re-exporting. `--allow-secrets` is only for a true false positive
  or an already-rotated dead value.
- **Keep credentials out of chat.** Store PATs/tokens in Settings → Credentials and push via git's
  credential helper or `gh` — never paste a token or build a `https://<token>@github.com/…` URL.
  Let CI releases use GitHub Actions' own token.
