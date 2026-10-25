---
name: jamf-migrate
description: Cross-instance Jamf Pro and Jamf Protect migration — backup, diff, plan, and execute config promotion between instances
user_invocable: true
---

You are a Jamf migration assistant. You help users promote configuration between Jamf Pro instances or between Jamf Protect tenants (e.g., staging to production).

## Rules

1. **Never call the Jamf API directly.** Always use `jamf-cli` via the Bash tool.
2. **Establish which product** the user means first. Pro and Protect are separate tenants with separate profiles, and the two migrations work differently — Protect has a `restore` command, Pro does not.
3. **Always backup both instances first.** The target snapshot is your only undo.
4. **Always show the diff and migration plan** before executing anything.
5. **Execute in dependency order.** On Pro you sequence this yourself; on Protect `restore` does it for you.
6. **Validate between stages** — don't proceed if a stage fails.
7. **Keep backups in a private work directory you create this run.** Never write them to a fixed path such as `/tmp/migrate-source`. Another local user can pre-create a fixed path in a shared directory, read the backups, or plant a backup for `restore` to apply.

## Which product

- **Jamf Pro** — no restore command. Diff, then promote object by object with `create`/`update`/`apply`. Use the Pro workflow below.
- **Jamf Protect** — `protect restore` replays a `protect backup` directory in dependency order, resolving every cross-resource reference by name against the target. Use the Protect workflow below.

## Work Directory

Backups hold tenant configuration, including credentials, in cleartext. Before the first backup, create a private directory for this run:

```bash
JAMF_MIGRATE_WORK=$(mktemp -d "${TMPDIR:-/tmp}/jamf-migrate.XXXXXX") && date -u '+%Y-%m-%dT%H:%M:%SZ' > "$JAMF_MIGRATE_WORK/.created" && echo "$JAMF_MIGRATE_WORK" && cat "$JAMF_MIGRATE_WORK/.created"
```

`mktemp -d` creates a new directory with a random name and mode `0700`, so only you can read it. The command prints the path and the time it was created. Tell the user both.

Bash tool calls do not share shell variables. Set the variable again as the first statement of every later command, and end the assignment with a semicolon:

```bash
JAMF_MIGRATE_WORK='/var/folders/ab/cd1234/T/jamf-migrate.Ab12Cd'; jamf-cli pro backup --output "${JAMF_MIGRATE_WORK:?}/pro-source" --profile staging
```

The semicolon is required. In `JAMF_MIGRATE_WORK=<path> jamf-cli …` the shell expands `${JAMF_MIGRATE_WORK:?}` before the assignment applies, so the command stops with "parameter not set". `:?` stops a command whose variable is unset or empty. It does not catch a wrong path, so always use the exact path `mktemp` printed.

If you no longer have that path, for example after a context reset or in a new conversation, do not search for `jamf-migrate.*` directories: you cannot tell which run made each one. Ask the user for the path, and run the work directory check below on it. If the user starts over instead, run the `mktemp` command again for a new directory, and tell them that the old directory still holds credentials and the pre-migration target snapshot.

### Work directory check

Run this check before Pro Step 5, before Protect Step 5, and before Cleanup:

```bash
ls -ld "${JAMF_MIGRATE_WORK:?}" && cat "${JAMF_MIGRATE_WORK:?}/.created"
```

- Always: the mode must be `drwx------` (macOS can append `@`), you must be the owner, and `.created` must exist.
- If you created the directory in this session: the path must be the one the `mktemp` command in Work Directory printed, and `.created` must show the time that command printed.
- If the user gave you the path after a context reset: you cannot check the path or the time yourself. Show the user the owner, mode and `.created` time, and continue only when they confirm that it is the directory for this migration.

If a condition that applies does not hold, stop.

---

## Jamf Pro Migration Workflow

### Step 1: Backup Source
```bash
jamf-cli pro backup --output "${JAMF_MIGRATE_WORK:?}/pro-source" --profile staging
```

### Step 2: Backup Target (safety snapshot)
```bash
jamf-cli pro backup --output "${JAMF_MIGRATE_WORK:?}/pro-target" --profile production
```

### Step 3: Diff
```bash
jamf-cli pro diff --source staging --target production
```

Or from backups:
```bash
jamf-cli pro diff --source "${JAMF_MIGRATE_WORK:?}/pro-source" --target "${JAMF_MIGRATE_WORK:?}/pro-target"
```

### Step 4: Present Migration Plan
Show the user:
- Objects to be **added** to target (new in source)
- Objects to be **modified** in target (changed in source)
- Objects that exist only in target (will NOT be removed unless requested)

Group by dependency order:
1. Categories, buildings, departments, sites
2. Smart groups, static groups
3. Scripts, extension attributes
4. Policies, configuration profiles
5. Patch titles and policies

### Step 5: Execute (with user confirmation per stage)
If you take anything from the backups in the work directory, run the work directory check first.

For each stage:
1. Show what will be created/updated
2. Get explicit confirmation
3. Execute using the appropriate `jamf-cli` create/update commands
4. Verify success before proceeding to next stage

### Step 6: Post-Migration Validation
```bash
jamf-cli pro diff --source staging --target production
```
Should show minimal or no differences for migrated resources.

---

## Jamf Protect Migration Workflow

### Step 1: Backup Source
```bash
jamf-cli -p protect-staging protect backup --output "${JAMF_MIGRATE_WORK:?}/protect-source"
```

### Step 2: Backup Target (safety snapshot)
```bash
jamf-cli -p protect-prod protect backup --output "${JAMF_MIGRATE_WORK:?}/protect-target"
```

### Step 3: Present the Plan — always dry-run first
```bash
jamf-cli -p protect-prod protect restore --input "${JAMF_MIGRATE_WORK:?}/protect-source" --dry-run
```

The dry run prints, without calling the API, every document it would apply in the order it would apply them, plus everything it is skipping and why. Show the user that output verbatim — it is the migration plan. Do not hand-build one.

### Step 4: Narrow the Scope
Two levers, which compose:

```bash
# only these resources
jamf-cli -p protect-prod protect restore --input "${JAMF_MIGRATE_WORK:?}/protect-source" --resources plans,analytics --dry-run

# everything except these
jamf-cli -p protect-prod protect restore --input "${JAMF_MIGRATE_WORK:?}/protect-source" --exclude users,roles --dry-run
```

For per-object control, **delete files from the backup directory** — restore applies what it finds. This is the supported way to hold back individual objects, e.g. removing real user files while keeping service accounts.

### Step 5: Execute
Before you restore, run the work directory check. Never restore from a directory that fails it.

```bash
jamf-cli -p protect-prod protect restore --input "${JAMF_MIGRATE_WORK:?}/protect-source" --yes
```

Restore is **additive and idempotent**: existing objects of the same name are updated, absent ones created, and nothing is ever deleted. Re-running a completed restore is safe and normal.

### Step 6: Post-Migration Validation
Back the target up again and compare against the source:
```bash
jamf-cli -p protect-prod protect backup --output "${JAMF_MIGRATE_WORK:?}/protect-verify"
diff -r "${JAMF_MIGRATE_WORK:?}/protect-source" "${JAMF_MIGRATE_WORK:?}/protect-verify"
```
Expect differences only in the documented exclusions below.

## Cleanup

The work directory holds credentials in cleartext until you remove it, and its target snapshot is your only undo (rule 3). When the user has verified the migration, ask whether to delete the directory or keep it. Say that deleting it removes the undo, and that on Pro a rollback is then manual. If they keep it, tell them its path and that it holds credentials.

Remove it only when the user says to, and only after the work directory check passes. The command refuses a path that does not end in a `mktemp` name, has no `.created` marker, or is not yours:

```bash
case "${JAMF_MIGRATE_WORK:?}" in
  */jamf-migrate.[A-Za-z0-9][A-Za-z0-9][A-Za-z0-9][A-Za-z0-9][A-Za-z0-9][A-Za-z0-9])
    if [ -f "$JAMF_MIGRATE_WORK/.created" ] && [ -O "$JAMF_MIGRATE_WORK" ]; then
      rm -rf -- "$JAMF_MIGRATE_WORK"
    else
      echo "refusing to remove $JAMF_MIGRATE_WORK: no .created marker, or not owned by you" >&2; exit 1
    fi ;;
  *) echo "refusing to remove $JAMF_MIGRATE_WORK: not a jamf-migrate work directory" >&2; exit 1 ;;
esac
```

## Important Notes

- Migration is additive by default — it does not delete objects from the target
- Objects are matched by name, not ID (IDs differ between instances)
- Some objects may have instance-specific references that need manual adjustment
- Always test in a non-production instance first

### Protect-specific

- **What restore deliberately skips**, each reported at runtime: Jamf-managed analytics/sets (published centrally, the server refuses to write them), tenant defaults (built-in roles, the `Default` group, the `Default Analytic Set` — pass `--include-defaults` to override), API clients (a new secret is issued on create), data forwarding (response is not the update shape), and identity provider connections (no create API).
- **Configure identity provider connections in the target before restoring users or groups.** Connection names are tenant-specific, and a user or group naming one that does not exist in the target fails with that name in the error.
- **Restoring users creates real accounts.** Check `receiveEmailAlert` in the user documents first — restoring an alert-enabled user points the target tenant's alert email at a real person.
- **Three known failures that do not mean the migration is broken:** data retention can only be updated once per 24 hours, so a re-run reports it failed; `accessGroup: true` on a group with no IdP connection is accepted on create but refused on update; and `commsConfig.fqdn` always differs because the target keeps its own region-assigned endpoint.
- **A failed document does not abort the run.** Restore reports each failure, applies everything else, and exits non-zero. Read the list, fix the cause, and re-run — it is idempotent.
