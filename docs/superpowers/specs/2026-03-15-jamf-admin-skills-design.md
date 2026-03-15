# Jamf Pro Admin Skills — Design Spec

**Date:** 2026-03-15
**Status:** Draft
**Approach:** Layered — CLI Power Commands + Claude Code Skills

## Problem

Jamf Pro admins face recurring pain points that the web UI and raw API don't address well:

1. **No config backup/export** — zero native capability for disaster recovery or change tracking
2. **Bulk operations are manual** — the UI forces one-at-a-time workflows for policies, profiles, groups
3. **Compliance reporting requires spreadsheet gymnastics** — no cross-resource views, no management-ready output
4. **Patch management visibility is poor** — fleet-wide patch status requires manual assembly
5. **Multi-instance management has no tooling** — no diff, sync, or promotion workflows
6. **Smart group management is disorganized** — no categories, no unused detection, no membership export

Sources: Reddit (r/jamf, r/macsysadmin), Jamf Nation feature requests, Jamf Nation community discussions, TrustRadius/Slashdot reviews, community tool ecosystem analysis (Installomator, SUPER, python-jamf, jctl).

## Architecture

Three layers, each building on the one below:

```
┌─────────────────────────────────────────────────┐
│  Layer 3: Claude Code Skills (conversational)    │
│  /jamf-audit  /jamf-backup  /jamf-bulk          │
│  /jamf-report  /jamf-migrate  /jamf-investigate │
├─────────────────────────────────────────────────┤
│  Layer 2: CLI Power Commands (deterministic)     │
│  audit  backup  bulk  report  diff  groups      │
├─────────────────────────────────────────────────┤
│  Layer 1: Existing CLI (200+ API commands)       │
│  Generated from OpenAPI specs + Classic manifest │
└─────────────────────────────────────────────────┘
```

**Key constraint:** Claude skills never call the Jamf API directly. They always invoke the CLI binary, which is the single source of truth for auth, retry, rate limiting, and error handling.

## Layer 2: CLI Power Commands

Six new handwritten commands in `internal/commands/`. All follow existing patterns: same output formatters, same auth resolution, same flag conventions.

### 2.1 `backup`

Exports all configuration objects from a Jamf Pro instance to a local directory.

**Usage:**
```
jamfpro-cli backup --output ./backup/
jamfpro-cli backup --output ./backup/ --resources policies,profiles,scripts
jamfpro-cli backup --output ./backup/ --format yaml
```

**Exported resources:**
- Policies, configuration profiles (macOS + iOS), scripts, extension attributes
- Smart groups (computer + mobile), static groups
- Categories, buildings, departments, sites
- Packages (metadata only), printers, dock items
- Network segments, restricted software, disk encryption configs
- Patch policies and titles

**Output structure:**
```
backup/
  policies/
    deploy-chrome.yaml
  profiles/
    macos/
      wifi-corporate.yaml
    ios/
      restrictions-student.yaml
  scripts/
    install-rosetta.yaml
  smart-groups/
    computers/
      all-laptops.yaml
  extension-attributes/
    battery-health.yaml
  ...
```

Each file is a self-contained YAML/JSON document. Server-generated fields (id, timestamps) are stripped by default for clean diffing. File names are slugified from the object name.

**Flags:**
- `--output` (required) — destination directory
- `--format` — yaml (default) or json
- `--resources` — comma-separated filter (e.g., `policies,scripts`)
- `--include-ids` — retain server IDs (useful for targeted restore)

**Implementation notes:**
- Follows the parallel-fetch pattern from `overview.go` — fetches resource lists concurrently, then individual objects concurrently with bounded parallelism
- Uses existing `HTTPClient` and `OutputFormatter` interfaces
- Writes files via standard library, no new dependencies

### 2.2 `audit`

Cross-resource health check that goes deeper than `overview`.

**Usage:**
```
jamfpro-cli audit
jamfpro-cli audit --checks compliance
jamfpro-cli audit --checks security
jamfpro-cli audit -o json
```

**Check categories:**

**Security:**
- FileVault enforcement gaps (policy exists but devices show disabled)
- SIP status across fleet
- Gatekeeper status
- Password policy coverage
- Unencrypted devices

**Compliance:**
- Devices not checking in (configurable `--days` threshold, default 14)
- Failed MDM commands
- Devices missing required profiles
- Patch compliance percentage per title

**Hygiene:**
- Empty smart groups
- Policies with no scope
- Unused scripts (not referenced by any policy)
- Duplicate profiles
- Categories with no objects
- Extension attributes that never report data

**Enrollment:**
- Expired or expiring DEP tokens
- Prestage assignment gaps
- Devices not ADE-enrolled

**Output:** Table with columns: severity (CRITICAL/WARNING/INFO), check name, affected count, recommendation. JSON mode for CI gating.

**Flags:**
- `--checks` — filter to category (security, compliance, hygiene, enrollment)
- `--days` — check-in threshold for compliance (default 14)
- Standard output flags (`-o`, `--out-file`)

### 2.3 `bulk`

Batch mutations with mandatory dry-run preview.

**Usage:**
```
jamfpro-cli bulk disable-policies --scope-group "Lab Machines" --dry-run
jamfpro-cli bulk disable-policies --scope-group "Lab Machines" --yes
jamfpro-cli bulk add-to-group --group "Needs Update" --from-file device-list.txt
jamfpro-cli bulk send-command --command RestartDevice --group "Lab Machines"
```

**Subcommands:**
- `enable-policies` / `disable-policies` — by scope group, category, or `--name-pattern`
- `add-to-group` / `remove-from-group` — from `--from-file`, stdin, or `--filter`
- `send-command` — MDM commands (`RestartDevice`, `EraseDevice`, `EnableRemoteDesktop`, etc.) to a group
- `update-ea` — set extension attribute values from file
- `assign-profile` / `remove-profile` — bulk profile scoping

**Safety model:**
- Every subcommand requires either `--dry-run` or `--yes`. No default to execute.
- `--dry-run` outputs a table of affected objects with no side effects.
- Destructive commands (`send-command EraseDevice`) require both `--yes` and `--confirm-destructive`.
- All mutations are logged to stderr with timestamps for audit trail.

### 2.4 `report`

Pre-built reports that admins currently assemble manually.

**Usage:**
```
jamfpro-cli report patch-status -o csv --out-file patch-report.csv
jamfpro-cli report device-compliance --days-since-checkin 14
jamfpro-cli report inventory-summary --group "All Laptops"
jamfpro-cli report software-installs --title "Google Chrome"
jamfpro-cli report ea-results --name "Battery Health"
```

**Reports:**
- `patch-status` — per-title patch compliance across the fleet, with device counts per version
- `device-compliance` — devices missing profiles, stale check-in, failed commands
- `inventory-summary` — hardware model / OS version / architecture breakdown, filterable by group
- `software-installs` — installed software with version distribution across fleet
- `ea-results` — extension attribute results across all devices, with empty/error detection

**Flags:** Standard output flags plus report-specific filters (`--group`, `--title`, `--name`, `--days-since-checkin`).

### 2.5 `diff`

Compare configurations between two sources.

**Usage:**
```
jamfpro-cli diff --source prod --target staging
jamfpro-cli diff --source ./backup/ --target prod
jamfpro-cli diff --source prod --target prod --snapshot
```

**Comparison modes:**
- **Instance vs. instance** — two config profile names, compares objects by name
- **Backup vs. instance** — local directory against live instance
- **Instance vs. snapshot** — current state against a previous backup (change detection)

**Output:** List of added/removed/modified objects per resource type. Modified objects show field-level diffs. Supports `--resources` filter.

**Flags:**
- `--source` — profile name or directory path
- `--target` — profile name or directory path
- `--snapshot` — compare against last backup in `--source` output dir
- `--resources` — filter to specific resource types
- Standard output flags

### 2.6 `groups`

Smart group management tooling.

**Usage:**
```
jamfpro-cli groups list --type smart --empty
jamfpro-cli groups members "All Laptops" -o plain
jamfpro-cli groups analyze --unused
jamfpro-cli groups export --format yaml
```

**Subcommands:**
- `list` — list groups with filters (`--type smart|static`, `--empty`, `--name-pattern`)
- `members` — dump membership of a specific group
- `analyze` — find unused groups (not referenced by any policy, profile, or other group)
- `export` — export all group definitions to YAML/JSON

## Layer 3: Claude Code Skills

Delivered as a Claude Code plugin. Six skills that compose CLI commands.

### 3.1 `/jamf-audit` — Instance Health Investigation

1. Runs `jamfpro-cli audit` and `jamfpro-cli overview`
2. Prioritizes findings by severity and blast radius
3. Explains each finding: why it matters and what to do
4. Offers to execute remediations with confirmation
5. Can generate management-ready summary (non-technical language)

### 3.2 `/jamf-backup` — Guided Backup & Restore

1. Runs `jamfpro-cli backup`, confirms export
2. If previous backup exists, auto-runs `jamfpro-cli diff` to show changes
3. Can initialize git repo in backup directory for version tracking
4. For restore: shows dry-run, confirms before executing, handles dependency ordering

### 3.3 `/jamf-bulk` — Safe Batch Operations

1. Accepts natural language targeting ("all lab computers", "devices missing Chrome update")
2. Translates to CLI filters/groups
3. Always shows dry-run with affected count
4. Requires explicit confirmation
5. Streams progress, reports failures

### 3.4 `/jamf-report` — Management-Ready Reporting

1. Runs appropriate `jamfpro-cli report` command
2. Generates narrative summary at requested level (executive, detailed, raw)
3. Highlights trends if previous reports exist
4. Exports to requested format

### 3.5 `/jamf-migrate` — Cross-Instance Migration

1. Backs up source instance
2. Diffs against target
3. Presents migration plan with dependency ordering
4. Executes in stages with validation between each
5. Groups and categories first, then policies/profiles that reference them

### 3.6 `/jamf-investigate` — Ad-Hoc Exploration

1. Takes natural language question about the Jamf instance
2. Determines which CLI commands to run
3. Chains commands, interprets results, answers the question
4. Examples: "why aren't building 3 devices getting the wifi profile?", "which groups reference Legacy Macs?"

## Priority Order

Based on community pain intensity and implementation dependency:

1. **backup** — foundational; diff and migrate depend on it
2. **audit** — highest standalone value; extends existing `overview`
3. **bulk** — most requested workflow improvement
4. **report** — addresses top feature request (management-friendly output)
5. **diff** — depends on backup format being stable
6. **groups** — smallest scope, can ship independently

Skills ship alongside or after their corresponding CLI commands. `/jamf-investigate` can ship early since it only needs the existing 200+ commands.

## Implementation Approach

### CLI Commands (Layer 2)

All new commands go in `internal/commands/` as handwritten Go files. They follow existing patterns:

- Use `CLIContext` for auth/client/output access
- Register in `root.go` via `rootCmd.AddCommand()`
- Add to appropriate group in `groups.go`
- Use `client.Do()` for API calls (handles auth, retry, rate limiting)
- Use `OutputFormatter` for all output (table/JSON/CSV/YAML/plain)
- Support `--dry-run` where applicable

Parallel API calls follow the bounded-concurrency pattern from `overview.go`.

### Claude Code Plugin (Layer 3)

Plugin structure:
```
jamfpro-skills/
  plugin.json
  skills/
    jamf-audit.md
    jamf-backup.md
    jamf-bulk.md
    jamf-report.md
    jamf-migrate.md
    jamf-investigate.md
```

Each skill is a markdown file with YAML frontmatter defining triggers, and body content providing Claude with the orchestration instructions. Skills invoke the CLI via `Bash` tool calls.

## Open Questions

1. **Restore command:** Should `backup` have a `--restore` flag, or should restore be a separate `jamfpro-cli restore` command? Separate command is safer (harder to accidentally restore).
2. **Backup format versioning:** If the backup YAML schema changes between CLI versions, how do we handle migration? Embedding a schema version in each file is the simplest approach.
3. **Rate limiting for bulk:** Should `bulk` have a `--concurrency` flag to control parallel mutations, or should it always be sequential? Sequential is safest; concurrency is a future optimization.
4. **Report scheduling:** Should the CLI support cron-like scheduling for reports, or leave that to external schedulers (cron, launchd, CI)? External schedulers — keep the CLI stateless.
