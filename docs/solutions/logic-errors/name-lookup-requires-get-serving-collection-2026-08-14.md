---
title: "--name and apply were generated for resources with no GET-serving collection; the lookup 405s"
date: 2026-08-14
category: logic-errors
module: generator/parser
problem_type: bug
severity: medium
applies_when:
  - "Auditing which generated commands get --name / --serial / --udid / apply"
  - "Changing collectionPath, opHasNameLookup, patchHasLookup or shouldGenerateApply"
  - "A user reports --name failing with HTTP 405 or a 404 on a collection URL"
  - "Adding a resource whose modern API exposes POST /collection but no GET /collection"
tags:
  - generator
  - name-resolution
  - collection-path
  - apply
  - upsert
  - dead-flag
  - dock-items
  - digicert
---

# Name lookup requires a GET-serving collection, not just a collection path

## Context

`--name` (and `--serial`/`--udid`, and the whole of `apply`) work one way: GET the
resource's collection, RSQL-filter it for the supplied identifier, extract the ID,
then act on `/{id}`. `resolveNameToID` / `resolveNameToIDForApply` in the generated
`registry.go` do the GET; the generator decides whether to emit the flag at all.

That decision went through `collectionPath(r.Operations) != ""`, checked in three
places: `opHasNameLookup` (the `--name` flag on get/update/delete/actions),
`patchHasLookup` (the same on `patch`), and `shouldGenerateApply` (`apply`).

## The bug

`collectionPath` answers a different question — *"what is this resource's collection
URL"* — and it has four fallbacks, in order:

1. a `list` GET with no path param,
2. a `create` **POST** with no path param,
3. the `get` `{id}` path minus its last `/{…}` segment,
4. the `update` `{id}` path minus its last `/{…}` segment.

Only fallback 1 implies the server will answer a GET. Fallbacks 2–4 do not: a
resource whose modern API is `POST /v1/dock-items` + `GET /v1/dock-items/{id}` has
a perfectly good collection path and no collection GET whatsoever. The generator
emitted `--name` anyway, and `resolveNameToID` GET a POST-only URL.

Wire-confirmed on Jamf Pro 11.31.0:

```
pro dock-items get --name Foo              → looking up "Foo": request failed (HTTP 405)
pro venafis get --name Foo                 → looking up "Foo": request failed (HTTP 405)
pro cloud-ldaps get --name Foo             → looking up "Foo": request failed (HTTP 405)
pro adcs-settings get --name Foo           → looking up "Foo": request failed (HTTP 405)
pro icons get --name Foo                   → looking up "Foo": request failed (HTTP 405)
pro certificate-authorities get --name Foo → resource not found (HTTP 404)
pro computer-groups get --name Foo         → resource not found (HTTP 404)
```

The 405s are fallback 2 (collection exists, POST only). The 404s are fallbacks 3–4
(the derived parent does not exist server-side at all).

`apply` was worse than a dead flag: its whole contract is name-based upsert, so on
these seven resources — adcs-settings, cloud-azures, cloud-ldaps, digi-cert-settings,
dock-items, team-viewer-remote-administrations, venafis — it could never do the
"update if it exists" half.

## Fix

Split the question in two. `collectionPath` keeps its meaning and its fallbacks
(`listPathFromOps` and `dedupeOperations` legitimately want the structural answer).
A new `nameResolutionPath(r)` answers *"which URL can we GET and RSQL-filter"*: take
`collectionPath`'s candidate, then require that some operation on the resource
actually performs a `GET` against that exact path. An explicit
`resourceNameLookupPathOverrides` entry is hand-picked, so it wins outright without
the check (that is how `mobile-devices` → `/v2/mobile-devices/detail` works).

All three gates and both template path helpers (`nameLookupPath`, `lookupFieldPath`)
now use `nameResolutionPath`.

Net effect: 40 dead `--name`/`--serial` lookup sites and 7 unusable `apply`
commands removed across 16 resources. Those resources keep full ID-based CRUD.
No registry changed — `backup`/`diff` and the smoke tests key off paths, not lookups.

## Why not make the lookup work instead

For some of these a sibling endpoint could plausibly serve the lookup, and that is
what `resourceNameLookupPathOverrides` is for — per-resource, deliberate, verified
against the wire. What the generator must not do is infer a lookup endpoint from a
POST route and ship a flag that cannot work. Removing the false affordance is the
correct default; adding overrides is follow-up work, one resource at a time.

## Guardrails

- `TestNameResolutionPath` pins the divergence: each case asserts both
  `collectionPath` *and* `nameResolutionPath`, so a future change that collapses
  the two back together fails here rather than silently resurrecting the dead flags.
- `TestOpHasNameLookup` gained POST-only-collection, derived-parent, and
  override-wins cases.
- `TestShouldGenerateApply` gained a POST-only-collection case.

Four pre-existing tests (`TestGenerate_ApplyWithDisplayName`, `TestNeedsFmt_WithApply`,
`TestNeedsURL_WithApply`, `TestShouldGenerateApply`) had fixtures with `create`+`update`
and no list GET. They were asserting apply generation off a resource shape that cannot
support apply; each fixture gained the list GET it was implicitly assuming.

## Watch for

A resource with a paginated collection GET whose operation is **not** named `list`
still resolves fine — the check is on method and path, not operation name. But a
resource whose collection GET lives at a *different* path than `collectionPath`
returns will not, and needs a `resourceNameLookupPathOverrides` entry rather than a
loosening of the check.
