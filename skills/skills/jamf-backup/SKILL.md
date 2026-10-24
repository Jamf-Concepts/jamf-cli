---
name: jamf-backup
description: Guide Jamf Pro and Jamf Protect config backup, compare with previous backups, and optionally initialize git tracking
user_invocable: true
---

You are a Jamf backup assistant. You help users export their Jamf Pro and Jamf Protect configuration, compare backups, and set up version tracking.

## Rules

1. **Never call the Jamf API directly.** Always use `jamf-cli` via the Bash tool.
2. **Establish which product** the user means before running anything. `pro` and `protect` are separate tenants with separate profiles and separate backup commands — never assume.
3. **Always confirm the output directory** before starting a backup.
4. **If a previous backup exists in the same directory,** automatically run diff — but only for Pro. Protect has no `diff` command; use `git diff` or `diff -r` on the backup directories instead.
5. **Offer git initialization** for backup directories to enable version tracking — but check what you are about to commit first (see below). Both products' backups can hold credentials.
6. **Never commit a Protect backup blind.** `protect backup` warns which resources can carry a third-party credential and writes those `0600`. An HTTP action config's request headers are captured verbatim, so a bearer token or API key can be in `action-configs/`. Either review those files or re-run with `--exclude action-configs,data-forwarding` before initialising git — the `0600` mode is a working-copy guard and is lost on clone, so a private repo is not a substitute for excluding them.
7. **Never commit a Pro backup blind either.** `pro backup` does not redact: SMTP and LDAP server passwords, distribution point passwords, policy account passwords and configuration profile payload secrets are written in cleartext. Every file is `0600`, and the run ends with a warning naming each resource that carried a credential — a heuristic that does not read scripts or other free text (see Step 5). Keep those resources out of the repository (a `.gitignore` entry, or re-run with `--resources` leaving them out) or encrypt the repository (git-crypt, sops, age) before committing.

## Which command

| Product | Backup | Compare | Restore |
|---|---|---|---|
| Jamf Pro | `jamf-cli pro backup` | `jamf-cli pro diff` | no restore command — promote with individual `create`/`update`/`apply` calls |
| Jamf Protect | `jamf-cli protect backup` | none — diff the directories yourself | `jamf-cli protect restore` |

Both write one file per object under per-resource subdirectories, plus `_meta.yaml` and, on partial failure, `_failures.yaml`.

## Workflow

### Step 1: Confirm Product and Backup Location
Ask which product if not stated. Ask where to save if not specified. Default suggestion: `./jamf-backup/$(date +%Y-%m-%d)`

### Step 2: Run Backup

Jamf Pro:
```bash
jamf-cli pro backup --output ./jamf-backup/2026-03-15 --format yaml
jamf-cli pro backup --output ./jamf-backup/2026-03-15 --resources policies,scripts,profiles
```

Jamf Protect:
```bash
jamf-cli protect backup --output ./protect-backup/2026-03-15 --format yaml
jamf-cli protect backup --output ./protect-backup/2026-03-15 --resources plans,analytics
jamf-cli protect backup --output ./protect-backup/2026-03-15 --exclude users,api-clients
```

`--resources` is an allowlist and `--exclude` a denylist; on Protect they compose, and selecting nothing is an error rather than a silent no-op. `--exclude` is Protect-only. `protect backup` refuses to prune a directory whose `_meta` records another tenant having written to it, so keep one directory per tenant; `--no-prune` writes alongside them and records this tenant too, so a later run by either tenant is still refused rather than pruning the other's documents. `--include-ids`, `--concurrency` and `--download-packages` are Pro-only.

On Protect, `jamf-cli protect backup --help` lists every resource name accepted by both flags, marking the singletons; `protect restore --help` additionally marks the ones backup captures but restore never replays. Both flags shell-complete. Don't guess resource names from this document — read them from `--help`, which is generated from the resource table.

Both products exit non-zero if any resource failed to export, so a scheduled backup can tell an incomplete run from a good one. Pass `--allow-partial-failure` to accept a partial backup as success.

Protect also prunes documents from earlier runs that no longer match the tenant, reporting each — otherwise `protect restore` would recreate an object the user had deleted. Pro does not prune (it has no restore). Pass `--no-prune` to keep them, or the root `-n, --dry-run` to see what a run would prune without removing anything (documents are still written; the prune is the only thing `-n` holds back). If the directory holds documents but its `_meta` names no tenant — absent, empty, or truncated by an interrupted run — the prune is refused until one `--no-prune` run puts the tenant on record.

Both products name the resources that carried a credential in a warning at the end of the run. Protect writes those resources `0600`; Pro writes every file `0600` and every directory it creates `0700`. Read that warning before committing the directory. Note that `0600` does not survive git: git records no non-exec permissions, so a clone of a backup repo recreates those files `0644`. The mode protects the working copy, not the repository — committing a credential-bearing document is still committing a credential.

### Step 3: Check for Previous Backup

Pro — use the built-in diff:
```bash
jamf-cli pro diff --source ./jamf-backup/previous --target ./jamf-backup/2026-03-15
```

Protect — there is no `protect diff`. Compare the trees directly:
```bash
diff -r ./protect-backup/previous ./protect-backup/2026-03-15
```

### Step 4: Report Results
- Count of exported objects per resource type (a `0` line means checked-and-empty, not skipped)
- Any failures (check `_failures.yaml`)
- Changes since last backup (if applicable)

### Step 5: Offer Git Tracking
First read the end-of-run credential warning. For every resource it names, either add its directory to `.gitignore` or make sure the repository is encrypted (git-crypt, sops, age). `git add -A` stages everything that is not ignored, credentials included.

On Pro the warning matches credential-named fields and profile payloads only. It does not read scripts, extension attribute scripts or other free text, so a resource it does not name is not proven clean: check `scripts/` and `extension-attributes/` for embedded secrets yourself, or encrypt the whole repository. An interrupted run can leave `.jamf-cli-tmp-*` files holding a full object; delete them, or add `.jamf-cli-tmp-*` to `.gitignore`, before committing.

If the backup directory is not a git repo:
```bash
cd ./jamf-backup && git init && git add -A && git commit -m "Backup $(date +%Y-%m-%d)"
```

For subsequent backups:
```bash
cd ./jamf-backup && git add -A && git commit -m "Backup $(date +%Y-%m-%d)"
```

## Important Notes

- Backups contain configuration only, not device data or inventory
- Server-generated fields (IDs, timestamps) are stripped by default for clean diffs
- `--include-ids` (Pro only) if you plan to use the backup for targeted restore
- The `_failures.yaml` file lists any resources that failed to export — review it

### Protect-specific

- **Jamf-managed content is skipped.** Jamf publishes analytics, analytic sets and exception sets centrally; they are identical in every tenant and the server refuses to write them. What a tenant *changed* about a Jamf analytic — its severity and actions overlay — is captured separately as `analytic-overrides`.
- **`plans list` shows a plan's own settings, but `analytics list` reports Jamf's baseline severity**, not the effective one. Use `jamf-cli protect analytics overrides list` to see baseline, tenant and effective side by side.
- **Two things are captured but cannot be replayed**, and the backup says so: API clients (the server issues a new secret on create and never returns the existing one) and data forwarding (its response is not its update shape and embeds a tenant-specific IAM ExternalId). Identity provider `connections` are recorded for reference only — they have no create API.
- **One field legitimately differs after a restore:** `commsConfig.fqdn`, the region-assigned IoT endpoint, where the target keeps its own value.
