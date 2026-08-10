---
title: "plans apply cannot clear a reference collection — omitted and empty both mean 'leave unchanged'"
date: 2026-08-07
category: conventions
module: internal/commands
problem_type: convention
severity: medium
applies_when:
  - "Adding a cross-resource reference (list-of-names) field to plans apply/export"
  - "Expecting a plans export → edit → apply round-trip to behave as a declarative replace"
  - "Writing a cleanup path that detaches members from a plan"
tags:
  - protect
  - apply
  - upsert
  - collections
  - graphql
  - round-trip
---

# `plans apply` cannot clear a reference collection

## Context

Added `unifiedLoggingFilterSets` to `plans apply` alongside the existing
`exceptionSets` and `analyticSets` (jamfprotect-go-sdk v0.8.0). While cleaning
up a live probe on the sandbox tenant, re-applying the *original* exported plan
— the one with no `unifiedLoggingFilterSets` key at all — did **not** detach the
set. The plan kept the membership, and the set then refused to delete
("Can't delete a unified logging filter set that is used on a plan").

The cause is a two-layer omission, and neither layer is wrong on its own:

1. `planExportToInput` builds each cross-resource reference field with
   `if len(e.Field) > 0 { input.Field = ... }`.
2. The SDK only sends a GraphQL variable when the corresponding struct field is
   non-nil.

So an absent key and an explicit `[]` are indistinguishable by the time the
mutation is built, and both are dropped from the request. The server keeps
whatever it had. This is not specific to filter sets — `exceptionSets` and
`analyticSets` on `PlanInput` have always behaved this way.

Detaching by hand required reconstructing the whole plan and posting
`unifiedLoggingFilterSets: []` directly, because `PlanInput` has no partial
update: `name`, `description`, `actionConfigs` and `autoUpdate` are all required
even for a one-field change.

**This is scoped to `plans apply`'s cross-resource reference fields only.** A
set's own membership list is a different case: `ulfSetExportToInput` sends
`Filters` unconditionally (no `len() > 0` guard), and the SDK's
`buildUnifiedLoggingFilterSetVariables` has no nil guard either — `$filters:
[ID!]!` is a required non-null argument. So `ulfs apply` (and `analytic-sets
apply`, built the same way) **does** clear the membership when the field is
omitted or empty. That is correct: a resource's own membership is what `apply`
is meant to replace; only a *reference* to another resource on `PlanInput`
carries the limitation below.

## Guidance

**Don't describe or treat `plans apply`'s reference fields as a declarative
replace.** They can add to and reorder a collection, never empty it.

- To detach members from a plan, use the granular read-modify-write
  subcommands — `remove-analytic`, `remove-exception`, `remove-rule`,
  `remove-filter`. Those send the full remaining list, so they *can* reach
  empty.
- When adding a new **cross-resource reference** field to `plans`
  apply/export, follow the existing `if len(...) > 0` shape so the field
  behaves like its `PlanInput` siblings. Do not make one reference field
  clear-capable in isolation — inconsistency here is worse than the
  limitation.
- When adding a field for a resource's **own** membership list (e.g. a new
  set type's `apply`), send it unconditionally instead — that field should
  behave like `ulfs apply`'s `filters`, not like a `PlanInput` reference.
- If the `PlanInput` limitation is ever fixed, fix it across **all**
  reference fields at once. It needs a way to distinguish absent from empty —
  a `*[]string`, or checking key presence against the raw unmarshalled map —
  plus a matching SDK change so an explicitly-empty slice still marshals into
  the request.

Shape to be aware of when hand-probing the fix:

```go
// current — absent and empty are the same request
if len(e.ULFSets) > 0 {
    input.UnifiedLoggingFilterSets = uuids
}

// clear-capable — needs the SDK to send non-nil-but-empty
if e.ULFSets != nil {
    input.UnifiedLoggingFilterSets = uuids // len may be 0
}
```

## Why this beats the alternative

Making only the newest field clear-capable would mean two collections on the
same `plans apply` input behave differently, with nothing in the help text
explaining which is which. The limitation is at least uniform and documented,
and the `remove-*` subcommands already cover the real use case.

## Probing note: two GraphQL endpoints, two schemas

Verifying any of this by hand hits a trap. Jamf Protect serves both
`POST /app` and `POST /graphql`, and they do **not** expose the same schema —
`updatePlan` is `FieldUndefined` on `/graphql` but present on `/app`. SDK v0.8.0
moved unified-logging-filter operations to `/app` for this reason. If a mutation
comes back `Validation error of type FieldUndefined`, retry against `/app`
before concluding the tenant lacks the feature.

Token: `POST /token` with `{"client_id": ..., "password": ...}`, then pass it as
a bare `Authorization: <token>` header — no `Bearer` prefix.
