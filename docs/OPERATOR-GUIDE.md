# clvsync — Operator Guide

End-to-end guide for moving a Clairvoyance persona or workspace between machines.

> ⚠️ Treat every package as **sensitive in transit** (conversation history + memory) and **untrusted on arrival**. Encrypt anything leaving a trusted machine, and review imported content before relying on it.

## 0. Build / install

Requires Go ≥ 1.26 on both machines (or a prebuilt binary from Releases).

```sh
go build -o clvsync ./cmd/clvsync
./clvsync datadir     # sanity check: prints this machine's Clairvoyance data dir
```

Secrets are read from **environment variables**, never flags (so they don't show in the process list):

| Variable | Used for |
| -------- | -------- |
| `CLVSYNC_PASSPHRASE` | age encryption/decryption passphrase |
| `CLVSYNC_SIGN_PASS`  | password protecting a minisign private key |

## 1. Choose a tier

| Goal | Command shape |
| ---- | ------------- |
| Move a persona's identity + memory + history | `export --persona <name> --tier 1` |
| …plus resume its live thread on the target (any model) | `export --persona <name> --tier 2` |
| Clone a whole workspace + all its personas | `export --workspace <name>` |
| …plus the heavy/regenerable dirs (venv, models, …) | `export --workspace <name> --include-heavy` |

## 2. Export (source machine)

Optionally generate a signing key once (`keygen`), then:

```sh
export CLVSYNC_PASSPHRASE='choose-a-strong-one'
# optional signing:  export CLVSYNC_SIGN_PASS='key-pass'; ./clvsync keygen --out mykey
./clvsync export --persona "Reegor" --tier 2 \
    --out reegor.cvpkg.age \
    [--sign-key mykey.key]     # produces reegor.cvpkg.age(.minisig)
```

- The export **secret-scans** everything first and **blocks** if it finds keys/tokens (`--allow-secrets` to override, `--redact` planned). Fix the finding rather than overriding when you can.
- Encryption modes: passphrase (`CLVSYNC_PASSPHRASE`) or recipient public key (`--recipient age1…`, no shared secret needed).
- Every package embeds an `IMPORT.md` with tailored instructions.

## 3. Transport

Move the `.cvpkg.age` (and `.minisig` if signed) by any means. **Send the passphrase out-of-band** — never alongside the package. Even encrypted, treat the file as sensitive.

## 4. Prep the target (workspaces only)

If a Tier-3 workspace doesn't exist yet on the target, register it **with Clairvoyance closed**:

```sh
./clvsync workspace-prep --name "MyProject" --path "D:/Clairvoyance/Workspaces/MyProject"
```

Then start and re-close the app, or pass `--workspace-path` to `import` and it will register it for you. (Persona imports need no prep.)

## 5. Verify, then import (target machine)

```sh
./clvsync verify --in reegor.cvpkg.age --verify-key mykey.pub --sig reegor.cvpkg.age.minisig
export CLVSYNC_PASSPHRASE='the-passphrase'
./clvsync import --in reegor.cvpkg.age \
    [--verify-key mykey.pub --sig reegor.cvpkg.age.minisig] \
    [--workspace-path D:/Clairvoyance/Workspaces/MyProject]   # Tier 3 only
```

Import order (all automatic): **verify signature → decrypt → safe-extract → verify manifest → collision check → non-destructive merge**. Existing files are backed up (`*.clvsync-bak`); a staff-id collision stops unless you pass `--force`.

## 6. After import

1. **Restart Clairvoyance** so it re-reads staff/workspace state.
2. Open the persona/workspace and confirm memory + history are present.
3. **Review the imported persona** before trusting it (its persona/memory files become instructions the target's agents load).
4. Tier 2: the thread should be resumable under the target's own model (Universal Resume, ≥ 0.77.0).

## Troubleshooting

| Symptom | Cause / fix |
| ------- | ----------- |
| `secret-scan blocked export` | A key/token is in the persona's data. Remove it, or `--allow-secrets` if it's a false positive. |
| `collision: persona … already on target` | Same staff id exists. `--force` to overwrite (backs up first). |
| `Tier 4 … requires workspace … to already exist` | Import the Tier-3 package first; the heavy add-on is paired. |
| `Tier 4 SKIPPED (space-aware fail-down)` | Target lacks room; the workspace synced without regenerable content — recreate it from manifests, or free space and re-run. |
| `signature verification failed` | Wrong public key or tampered package. Do not import. |
| Workspace not showing after import | Run the import/prep with the app **closed**, then start it. |

## Security cheat-sheet

- **Never** send the passphrase with the package.
- **Always** review the export's secret-scan output.
- Prefer **signing** for anything shared beyond your own machines.
- Treat imported personas as **untrusted** until reviewed.
- Provided **as-is, without warranty.**
