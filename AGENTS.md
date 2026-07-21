# AGENTS.md — have Clairvoyance Staff set up and run clvsync for you

You don't have to touch a terminal. Paste one of the prompts below to a Clairvoyance
Staff member and it will do the work. These are the same instructions a Staff agent
should follow when asked to "set up persona sync" or "import this package."

---

## 1. One-time setup on a machine (create the Sync Operator)

> **Prompt to Staff:** "Set up Clairvoyance Persona Sync on this machine."

Staff steps:

1. **Fetch the binary.** Download the `clvsync` build for this OS from the repo's GitHub
   **Releases** (`clvsync-<os>-<arch>[.exe]`) plus its `.minisig` and `SHA256SUMS`.
   Verify the checksum and (if a release signing key is published) the signature, then
   place the binary somewhere on PATH. If no prebuilt binary exists for this platform,
   fall back to `go build ./cmd/clvsync` from a clone.
2. **Create the Sync Operator persona.** Add a Staff member named **Sync Operator** whose
   knowledge template is **`Sync Operator`** (see `personas/Sync Operator.md`). That
   `knowledgeTemplate` marker is load-bearing: it is how `clvsync` recognizes the operator
   and refuses to sync it (guard S15).
3. Tell the user setup is done and that they can now say "import the package on my Desktop"
   or "export my <persona> to send to my other machine."

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

1. `clvsync export --persona "<name>" --tier 2 --out <name>.cvpkg.age`
   (set `CLVSYNC_PASSPHRASE` first to encrypt; Tier 2 includes the resumable session).
2. Hand the user the `.cvpkg.age` file and remind them to send the **passphrase out of band**
   (not with the file).

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
