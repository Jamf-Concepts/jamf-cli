---
title: "Legacy Casper/Jamf Remote ad-hoc policies are in the Classic policy list and skewed every policy report"
date: 2026-10-19
category: logic-errors
module: internal/commands
problem_type: skewed-results
severity: medium
applies_when:
  - "A command lists /JSSResource/policies and analyses, counts or reports on every row"
  - "A report shows hundreds of 'No targets in scope' / 'No triggers' / 'Uncategorised' warnings on policies nobody recognises"
  - "A policy count in the CLI is higher than the Jamf Pro GUI's"
  - "Adding a new command that walks the Classic policy list"
tags:
  - classic-api
  - policies
  - casper-remote
  - jamf-remote
  - reports
  - jamf-pro-api
---

## Symptom

`pro report policy-status` on one customer tenant returned 68 findings, every
one on a policy named like `2021-07-19 at 6:59 PM | jane.doe$@example.com | 1 Computer`:
23 "Uncategorised", 23 "No triggers, not Self Service", 21 "No targets in
scope", 2 "1 computer(s) scoped directly". None was a real policy.

## Cause

Casper Remote and Jamf Remote write a policy row for every task they run. Jamf
Pro hides these in the GUI (they open only by typing `policies.html?id=<n>`),
but Classic `/JSSResource/policies` returns them (PI109008). A one-off row
has no scope, trigger, payload or category by construction, so every
missing-X check fires on it. Tenants hold from dozens to ~19,000 of them, and
each one also cost a detail GET.

They can be told apart by `created_by != "jss"` in the database, but Classic
does not expose that field. The name is the only discriminator a list response
carries.

## Fix

`isRemoteAdHocPolicy` (`internal/commands/pro_remote_policies.go`) matches the
name with an anchored pattern: `<date> at <time> AM|PM | <user> | N Computer(s)`.
`FetchAnalyticPolicyList` lists the policies, drops matches **before any detail
fetch**, and prints one stderr line (once per process, silenced by `--quiet` and
`--no-hints`) naming the count and the `classic-policies list` / `delete <id>`
route to see and remove them.

Wired into every analytic or count path: `pro report policy-status`, `pro audit`
(`checkPoliciesNoScope`), `pro dashboard` (`fetchPolicyDetails` and the policy
count) and `pro overview`.

## The rule

**A command that analyses, counts or reports on policies lists through
`FetchAnalyticPolicyList`. A command that manages them — `classic-policies
list/get/delete/apply` — reads the raw list.** The second group is how an admin
finds and removes these rows, so they must stay visible there.

Not filtered, deliberately:

- `pro backup` / `pro diff` — a backup is a restore source, and silently dropping
  rows from one is a different decision from tidying a report.
- `pro group-tools analyze-unused` — the rows reference no groups, so the result
  is unaffected; it only fetches them needlessly.

## Not done

- No `--include-remote-policies` escape hatch on the reports.
- No `classic-policies prune-remote`. Deletion is irreversible; if added it needs
  a `--dry-run` preview and `--yes`. Deleting these rows through the API does not
  disturb policy ID numbering.
