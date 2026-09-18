# Design: `jamf-cli dashboard` — Cross-Product HTML Dashboard

## Problem

The CLI has 1,158 commands across Jamf Pro, Protect, and Platform — including
powerful compound commands like `overview` (37 parallel API calls), `audit`,
`backup`, `diff`, and `report`. But customers either don't know the CLI exists
(web UI admins) or underuse it (basic CLI users who stick to CRUD operations).

There's no single artifact that showcases the CLI's depth across products in a
way that's visually compelling, shareable, and immediately useful.

## Solution

A new top-level `jamf-cli dashboard` command that authenticates against one or
more config profiles, collects fleet data in parallel, and renders a
self-contained HTML report. The report covers fleet health, security posture,
audit findings, patch compliance, Protect coverage, and Platform status — all in
one file that can be emailed, bookmarked, or presented to leadership.

## Command Interface

```
jamf-cli dashboard --profile prod-pro --profile prod-protect [--out-file report.html]
```

### Flags

| Flag | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `--profile` | string (repeatable) | yes | — | Config profiles to pull data from |
| `--out-file` | string | no | stdout | Write HTML to file instead of stdout |
| `--title` | string | no | "Jamf Fleet Dashboard" | Custom report title |
| `--no-color` | bool | no | false | Strip color from terminal progress output |

### Auth Model

The command iterates each `--profile`, calls `ResolveAuthForProfile` (already
used by `diff`), builds the appropriate client for that profile's product type
(Pro, Protect, or Platform), and validates auth. If any profile fails
authentication, the command errors immediately before collecting any data.

The command is added to the `chainSkip` map in `root.go` since it handles its
own multi-profile auth orchestration, like `diff`.

### Alias

`db` maps to `dashboard` in root aliases.

## Data Collection

### Per-Profile Parallel Fetching

Within each profile, all API calls run in parallel using the existing
`sync.WaitGroup` + goroutine pattern from `pro_overview.go`. Profiles are
processed sequentially (fail-fast auth requires this).

### Jamf Pro Data

| Section | Data Source | What It Collects |
|---------|------------|-----------------|
| Fleet Summary | `fetchPaginatedCount` on computers, mobile-devices, users endpoints | Total managed devices, mobile devices, users |
| Security Posture | RSQL-filtered inventory queries (same as `pro report security`) | FileVault, Firewall, Gatekeeper, SIP compliance percentages |
| Audit Findings | `allAuditChecks()` from `pro_audit.go` | All check results with severity, category, affected count, recommendation |
| Patch Compliance | Patch report data-gathering pattern | Per-title compliance rates |
| Device Compliance | Device compliance report queries | Stale device count, failed MDM commands, missing profiles |
| OS Distribution | Inventory summary queries | OS version breakdown for fleet summary charts |

### Jamf Protect Data

| Section | Data Source | What It Collects |
|---------|------------|-----------------|
| Plan Status | `ListPlans` SDK call | Plan count, deployment breakdown |
| Analytics | `ListAnalytics` SDK call | Enabled/disabled analytics counts |
| Endpoint Coverage | `ListComputers` SDK call | Managed endpoint count |
| Configuration | `ListAnalyticSets`, `ListExceptionSets` SDK calls | Set counts for configuration coverage |

### Jamf Platform Data

Collected when a profile has `auth-method: platform`.

| Section | Data Source | What It Collects |
|---------|------------|-----------------|
| Blueprint Status | Platform SDK `ListBlueprints` | Blueprint list with deployment status |
| Compliance Benchmarks | Platform SDK compliance endpoints | Benchmark scores, failing rule counts |
| DDM Status | Platform SDK DDM endpoints | Declaration deployment status |

### Data Types

A `DashboardData` struct holds everything the HTML template needs. No raw API
responses leak into the template layer:

```go
type DashboardData struct {
    Title       string
    GeneratedAt time.Time
    CLIVersion  string
    Profiles    []ProfileMeta

    // Conditional sections — nil means "don't render"
    Fleet       *FleetSummary
    Security    *SecurityPosture
    Audit       *AuditSummary
    Patch       *PatchCompliance
    Devices     *DeviceCompliance
    Protect     *ProtectCoverage
    Blueprints  *BlueprintStatus
    Compliance  *ComplianceBenchmarks
}
```

## HTML Output

### Self-Contained File

A single HTML file with no external dependencies:

- **Chart.js** (~80KB minified) embedded as an inline `<script>` tag for
  doughnut charts (security posture), horizontal bar charts (patch compliance),
  and stacked bars (OS distribution)
- **CSS** inlined in a `<style>` block — CSS grid for card layout, responsive
  breakpoints, print-friendly styles
- **Vanilla JS** (~200-300 lines) inlined for interactivity: collapsible
  sections, summary/detail toggle, severity filtering on audit findings
- **No external requests** — works offline, in email clients, on file shares

Expected file size: ~150KB.

### Layout

1. **Header** — report title, generation timestamp, CLI version, profile names
2. **Fleet Summary** — big number cards (computers, mobile devices, users) with
   per-profile breakdown if multiple Pro profiles provided
3. **Security Posture** — four doughnut charts (FileVault, Firewall, Gatekeeper,
   SIP) with color coding: green >90%, yellow 70-90%, red <70%
4. **Audit Findings** — grouped by severity (critical/warning/info), collapsible
   detail per finding showing category, affected count, recommendation. Critical
   findings visually emphasized.
5. **Patch Compliance** — horizontal bar chart of software titles with compliance
   percentages
6. **Device Compliance** — stale devices, failed MDM commands, missing profiles
   with counts
7. **Protect Coverage** — plan count, analytics status, endpoint count,
   configuration set counts (only rendered if Protect profile provided)
8. **Platform Status** — blueprint deployment table, compliance benchmark scores,
   DDM status (only rendered if Platform profile provided)

### Styling

- Light theme, clean and professional
- Borrows color palette and typography sensibility from the existing `docs/site/`
- Print-friendly (respects `@media print`)
- CSS grid for responsive card layout
- "Executive summary" aesthetic — suitable for showing a CISO

### Interactivity

- **Collapsible sections** — click section headers to expand/collapse detail
- **Summary/detail toggle** — top-level switch between executive overview
  (numbers + charts only) and full drill-down (tables, per-device findings)
- **Severity filter** — audit section can filter by critical/warning/info

### Conditional Rendering

Sections only render if data was collected for them. A Pro-only run shows Fleet,
Security, Audit, Patch, Device Compliance. Adding a Protect profile adds the
Protect section. Adding a Platform profile adds Blueprint and Compliance
sections. No empty sections, no "N/A" placeholders.

## Template Implementation

The HTML template uses Go `html/template`. The template source (HTML + embedded
CSS + embedded JS + embedded Chart.js) lives as a `const` string or `embed.FS`
in `dashboard_html.go`, matching the pattern used by generator templates
elsewhere in the codebase.

The render function takes `DashboardData`, executes the template, and writes the
result to the configured output (stdout or `--out-file`).

## File Structure

### New Files

| File | Purpose | ~LOC |
|------|---------|------|
| `internal/commands/dashboard.go` | Command definition, auth orchestration, data collection coordination | 150 |
| `internal/commands/dashboard_pro.go` | Pro data collection (fleet, security, audit, patch, device compliance) | 300 |
| `internal/commands/dashboard_protect.go` | Protect data collection (plans, analytics, endpoints, sets) | 150 |
| `internal/commands/dashboard_platform.go` | Platform data collection (blueprints, benchmarks, DDM) | 200 |
| `internal/commands/dashboard_html.go` | DashboardData struct, HTML/CSS/JS template, render function | 500 |

### Modified Files

| File | Change |
|------|--------|
| `internal/commands/root.go` | Add `cmd.AddCommand(newDashboardCmd())`, add `"dashboard"` to `chainSkip` |
| `internal/commands/groups.go` | Add dashboard to root group map |
| `internal/commands/aliases.go` | Add `db` → `dashboard` root alias |

## Scope Boundaries (v1 Exclusions)

- **No auto-refresh / live mode** — point-in-time snapshot, not monitoring
- **No dark mode toggle** — light theme only, simpler template, print-friendly
- **No historical comparison** — each report is standalone
- **No PDF export** — HTML is the format; users can browser print-to-PDF
- **No custom section selection** — all available sections render based on
  profiles provided
- **No terminal output mode** — HTML only. The existing `pro overview` and
  `protect overview` already serve the terminal use case.

## Error Handling

- **Auth failure on any profile** → command exits immediately with error message
  naming the failing profile. No partial data collection.
- **Individual API call failure within a profile** → the section that depends on
  that data renders with an error indicator (e.g., "Security data unavailable:
  HTTP 403"). Other sections still render. This matches `pro overview` behavior
  where individual fetch failures don't block the whole dashboard.
- **No profiles provided** → usage error listing available profiles from config
- **Unknown profile name** → error with suggestion to run `jamf-cli config list`

## Success Criteria

1. `jamf-cli dashboard --profile my-pro > report.html && open report.html`
   produces a visually compelling, self-contained HTML report
2. Adding `--profile my-protect` adds the Protect section without any other
   flags
3. The report is useful enough that an admin would run it weekly
4. The report is impressive enough that someone forwards it to their team saying
   "look what our CLI can do"
