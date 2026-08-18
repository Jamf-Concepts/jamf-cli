---
title: "An export is only portable if it round-trips through the input type, proven on the wire"
date: 2026-08-18
category: logic-errors
module: internal/commands
problem_type: bug
severity: high
applies_when:
  - "writing an export/apply pair for a resource"
  - "adding a resource to protect backup/restore"
  - "hand-writing an export struct instead of reusing the SDK input type"
  - "a round-trip silently loses fields or is refused by the server"
tags: [protect, export, apply, backup, restore, fidelity, graphql, round-trip, portability]
---

## Context

Building `protect backup`/`restore` surfaced six separate bugs in export/apply
pairs that had shipped and looked fine. Every one had the same root cause: the
code assumed **the shape you read is the shape you can write**. It usually is
not, and nothing in the type system or the tests caught the difference — the
exports were well-formed, the unit tests passed, and the data was wrong or
rejected only when actually replayed into a tenant.

The six, all found by cloning between two live tenants:

| Symptom | Cause |
|---|---|
| `analytics export \| analytics apply` → "input is not valid JSON or YAML" | export emitted the community schema (`actions` = objects), apply decoded the SDK input (`Actions` = strings) |
| `longDescription`, `remediation` silently dropped | emitted by the export converter, never read by the import converter |
| `startup`, `label`, `matchReason` silently dropped | present on the response and the input, absent from the hand-written export struct. Every Jamf analytic has a `label`, so **every** export lost one |
| `threatPreventionStrategy`, `customEngineConfig` silently dropped | same: hand-written `planExport` drifted from `PlanInput` |
| action config unrestorable | `params` reads back as an object but the input declares `AWSJSON!`, a JSON-encoded *string*, and it cannot be omitted either |
| data retention unrestorable | response is nested (`database.log.numberOfDays`), input is flat (`DatabaseLogDays`); replay sent zeros |
| groups-only user unrestorable | nil slice marshalled as `null`; API wants `[]` |

Fixed across 2d81c9d, 7c0072f, 76efdd3, 300f709.

## Guidance

**Prefer the SDK input type as the export shape.** Where a resource exports
`jamfprotect.XInput` directly (telemetry, prevent lists, roles, exception sets,
RSCS), field coverage is complete *by construction* and cannot drift. Eight of
the fifteen Protect resources do this and none of them had a bug. Both resources
with a hand-written export struct — plans and analytics — had silent field loss.

**If you must hand-write one** (because references need to travel as names, or an
external schema is being matched), add a test that asserts field coverage against
the input type, and re-check it whenever the SDK is bumped. A hand-written export
struct is a standing invitation to drift.

**References travel as names, never as server IDs.** IDs are per-tenant, and
Protect's are small sequential integers, so a foreign ID does not error — it
binds to whatever object happens to hold that integer. Resolve names against the
target at apply time and let an unknown name fail loudly. See `groupExport` /
`groupExportToInput` in `protect_rbac_refs.go` for the shape, and keep accepting
the old ID-shaped document so existing files still apply.

**Empty collections must marshal as `[]`, not `null`.** Initialise reference
slices to `[]string{}` in the converter.

**Prove it on the wire, in two directions and two tenants.** The only check that
actually catches this class:

```
backup tenant A  →  restore into tenant B  →  backup tenant B  →  diff
```

Anything that is not byte-identical is either a bug or a server-owned field you
can name and justify. In the Protect clone that left exactly one: `commsConfig.fqdn`,
the region-assigned IoT endpoint, where the target correctly keeps its own.

## Why this beats the alternative

Unit tests over the converters pass happily while the round-trip is broken,
because both directions are wrong in the same way, or the dropped field simply
is not in the fixture. `analytics export | analytics apply` was documented in
CLAUDE.md as a supported pipe and had been broken for every analytic in every
tenant; no test noticed, because no test ever fed one command's real output into
the other.

The diff-after-clone check is cheap, needs no fixtures, and is the only thing
that would have caught all six.
