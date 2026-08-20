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
  - "a resource accepts two document shapes and apply decodes only one"
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

Anything that is not byte-identical is either a bug or a field you can name and
justify. In the Protect clone that leaves exactly two:

- `commsConfig.fqdn` — the region-assigned IoT endpoint, where the target
  correctly keeps its own (`us-east-1` vs `eu-central-1` between the two tenants
  used here).
- an exception's `analyticuuid` — which *must* differ for a custom analytic,
  because the restore rebound it from the `analytic:` name to the target's own
  uuid. A byte-identical `analyticuuid` here would mean the rebinding did **not**
  happen, so this is the one field where equality is the failure signal.

Everything else in the tree is byte-identical, with four categories legitimately
absent rather than differing: api-clients (a new secret is issued on create),
data forwarding, identity provider connections, and tenant defaults. And the
target keeps anything it holds that the backup does not mention — restore never
deletes — so the target's own analytic overrides survive alongside the replayed
ones, and a per-file comparison is the right check rather than a whole-tree diff.

## Why this beats the alternative

Unit tests over the converters pass happily while the round-trip is broken,
because both directions are wrong in the same way, or the dropped field simply
is not in the fixture. `analytics export | analytics apply` was documented in
CLAUDE.md as a supported pipe and had been broken for every analytic in every
tenant; no test noticed, because no test ever fed one command's real output into
the other.

The diff-after-clone check is cheap, needs no fixtures, and is the only thing
that would have caught all six.

## A seventh, found by pointing one command at the other

Reviewing the change that produced this document, one probe — pipe each `export`
into its own `apply` against a live tenant — turned up the same bug once more, in
the resource sitting next to the one already fixed:

```
$ jamf-cli protect unified-logging-filters export "Some filter" -o yaml |     jamf-cli protect unified-logging-filters apply --yes
CreateUnifiedLoggingFilter: input → filter: '' should be non-empty
```

`ulfToYAML` writes the community schema, whose predicate key is `predicate`; the
SDK input calls the same field `Filter`. `apply` decoded the SDK shape directly,
so the predicate never bound and the server refused every filter in every tenant
— exactly the analytics failure, in a resource whose backup/restore path was
already correct because it went through the YAML converters.

Fixed the same way: `ulfInputFromDocument` sniffs which schema a document is in,
mirroring `analyticInputFromDocument`.

The lesson is narrower than the one above and worth stating on its own: **when a
resource has two document shapes, the pipe between its own commands is the test.**
Unit tests over each converter passed. What found it was running
`export | apply` once, which is a single line of shell and belongs in the review
of any export/apply pair.
