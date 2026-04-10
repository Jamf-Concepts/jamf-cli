# Platform API Integration Features

New capabilities that connect Jamf Platform API data (blueprints, compliance benchmarks, device groups) with existing CLI power commands.

## Features

### 1. `pro overview` — Platform Dashboard Section

Adds a "Platform" section to the instance overview dashboard. Fetches in parallel:

- **Blueprints** — total count, deployed count, count with deployment failures
- **Compliance Benchmarks** — total count, updates available, average compliance %
- **Device Groups** — total count

Color-coded: failures in red, updates available in yellow, low compliance (<80%) in red, moderate (<95%) in yellow. Section only appears when platform gateway auth is active.

### 2. `pro device` — Platform Compliance Card

Adds two new sections to the device deep-dive when platform auth is active:

**Platform Blueprints** — Lists blueprints scoped to the device's groups with deployment status:
```
Platform Blueprints
  Security Baseline              DEPLOYED (142 succeeded)
  Corporate Passcode             DEPLOYED (3 failed)          *
```

**Platform Compliance** — Lists benchmarks scoped to the device's groups with enforcement mode and compliance %:
```
Platform Compliance
  CIS macOS 15 Level 1           ENFORCE (97.2%)
  Custom Internal Baseline       MONITOR (84.5%)              *
```

Works by resolving the device's serial number to a platform device ID, fetching group memberships, then cross-referencing all blueprints and benchmarks.

### 3. `pro audit --checks platform` — Platform Health Checks

Six new audit checks under the `platform` category:

| Check | Severity | What it catches |
|-------|----------|-----------------|
| Undeployed blueprints | WARNING | Created but never deployed |
| Blueprint deployment failures | CRITICAL | Deployed blueprints with failed devices |
| Stale blueprints | WARNING | Updated after last deployment (config drift) |
| Benchmarks with updates available | WARNING | New mSCP baseline version not applied |
| Benchmarks in MONITOR mode | INFO | Eligible to switch to ENFORCE |
| Empty platform scope | WARNING | Blueprints/benchmarks with no device groups |

Runs automatically when platform auth is active (alongside other categories), or filter with `--checks platform`.

### 4. `pro report blueprint-status` — Deployment Dashboard

```bash
jamf-cli pro report blueprint-status
```

Shows all blueprints with deployment state, scope/step counts, and device counts:

| Column | Description |
|--------|-------------|
| name | Blueprint name |
| state | DEPLOYED or NOT_DEPLOYED |
| scope | Number of device groups targeted |
| steps | Number of configuration steps |
| succeeded | Devices successfully deployed (deployed only) |
| failed | Devices with failures (deployed only) |
| pending | Devices pending deployment (deployed only) |

Requires platform gateway auth.

### 5. `pro report compliance-rules <benchmark-title>` — Rule Failure Heat Map

```bash
jamf-cli pro report compliance-rules "CIS macOS 15 Level 1"
jamf-cli pro report compliance-rules "CIS macOS 15 Level 1" --state failed
jamf-cli pro report compliance-rules "CIS macOS 15 Level 1" --sort "failed:desc"
```

Per-rule compliance stats sorted by failure rate (default: worst first).

| Flag | Description |
|------|-------------|
| `--sort` | Sort field (e.g. `passPercentage:asc`, `failed:desc`) |
| `--search` | Filter rules by title |
| `--state` | Filter by result: `passed`, `failed`, `unknown` |

### 6. `pro report compliance-devices <benchmark-title>` — Non-Compliant Devices

```bash
jamf-cli pro report compliance-devices "CIS macOS 15 Level 1"
```

Aggregates per-device compliance across all rules. For each failing rule, fetches the device list and counts how many rules each device fails. Shows devices sorted by failure count.

| Flag | Description |
|------|-------------|
| `--state` | Filter by result state (default: `FAILED`) |

### 7. `pro blueprints clone <source> <new-name>` — Blueprint Cloning

```bash
jamf-cli pro blueprints clone "Security Baseline" "Security Baseline - Staging"
jamf-cli pro blueprints clone "Security Baseline" "Security Baseline v2" --scope group-id-1,group-id-2
```

Creates a copy of an existing blueprint with a new name. Copies all steps, components, and scope. Use `--scope` to override the device group targets (useful for staging-to-production promotion).

### 8. `pro backup` — Platform Resources

```bash
jamf-cli pro backup --output ./my-backup
jamf-cli pro backup --output ./my-backup --resources blueprints,compliance-benchmarks
```

When platform auth is active, backs up:
- **Blueprints** to `blueprints/` subdirectory (server fields stripped)
- **Compliance Benchmarks** to `compliance-benchmarks/` subdirectory

Works with `--resources` filter. Respects `--format yaml|json`.

### 9. `pro diff` — Platform Resource Comparison

```bash
# Directory-to-directory (works with backups from #8)
jamf-cli pro diff --source ./backup-staging --target ./backup-prod --resources blueprints

# Live instance comparison (when profiles have platform auth)
jamf-cli pro diff --source staging --target production --resources blueprints,compliance-benchmarks
```

For directory sources, reads from `blueprints/` and `compliance-benchmarks/` subdirs automatically. For live instance sources, constructs a Platform SDK client when the profile uses platform gateway auth.

### 10. `pro group-tools analyze --unused-platform` — Orphaned Platform Groups

```bash
jamf-cli pro group-tools analyze --unused-platform
```

Finds platform device groups not referenced by any blueprint or compliance benchmark. Lists orphaned groups with their type, device type, and member count.

## Auth Requirement

All features require platform gateway auth (`auth-method: platform`). Features degrade gracefully:
- `pro overview` and `pro device` simply omit platform sections when not available
- `pro audit` skips platform checks when not available
- Report, backup, diff, and group-tools commands return a clear error message with setup instructions
