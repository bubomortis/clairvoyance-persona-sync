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

**Idempotency first:** before changing anything, detect an existing install — if `clvsync datadir`
already works *and* a **Sync Operator** Staff member exists, report that and **stop**. Do not
reinstall or recreate.

Steps:

1. **Fetch the binary from the Release (trustless).** From the latest release, download the build
   for this OS/arch (`clvsync-<os>-<arch>[.exe]`) **and** `SHA256SUMS`. **Verify the checksum**
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
- **Trustless:** verify the binary's checksum before using it, and place only the repo's own template.
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
5. Tell the user to **reopen Clairvoyance**, then run
   `clvsync verify-import --receipt import-receipt.json` and read back the pass/fail table.
6. Remind the user that whether the session is *offered for resume* is their check in the UI.

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
3. Hand the user the resulting `.cvpkg.age` file and remind them to send the **passphrase out of
   band** (not with the file).

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
