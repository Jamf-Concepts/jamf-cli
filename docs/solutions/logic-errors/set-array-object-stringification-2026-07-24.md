---
title: "--set silently stringified array/object field values; parse by schema kind and reject mismatches"
date: 2026-07-24
category: logic-errors
module: generator/parser
problem_type: bug
severity: high
applies_when:
  - "Adding or auditing the 'update --set' / 'patch --set' KEY=VALUE code path in the modern API generator"
  - "A PUT/PATCH request schema contains array or object fields (customPackageIds, criteria, privileges, extensionAttributes, skipSetupItems, ...)"
  - "A user reports --set sending a wrong-typed payload (a string where an array/object/number is expected)"
tags:
  - generator
  - update-set
  - patch-set
  - type-coercion
  - json
  - data-corruption
  - computer-prestages
---

# --set silently stringified array/object field values

## Context

Both `update --set KEY=VALUE` (fetch-merge-PUT) and `patch --set KEY=VALUE`
(JSON Merge Patch) funnel their pairs through `buildMergePatchFromSet` in the
generated `registry.go` (template in `generator/parser/generator.go`,
`registryTemplate`). The original value inference was scalar-only:

```go
// parsePatchValue: "true"/"false" → bool, "null" → nil, integers → int64, else string.
```

Every non-scalar value therefore fell through to the `else string` branch. For a
field whose target type is an **array** or **object**, the CLI shipped a
wrong-typed payload with no error and no warning — silent data corruption.

Reported via issue #304 for `customPackageIds` (an array of strings) on a
computer prestage. Verified with `--dry-run -vvv` against the outgoing body:

| `--set` input                   | Serialized in request body            | JSON type      |
|---------------------------------|---------------------------------------|----------------|
| `customPackageIds=["295"]`      | `"customPackageIds":"[\"295\"]"`      | string, not array |
| `customPackageIds.0=295`        | `"customPackageIds":{"0":295}`        | object, not array |
| `customPackageIds=295`          | `"customPackageIds":295`              | number, not array |

The stdin path (`get -o json | jq '.customPackageIds=["295"]' | update`) always
worked because jq produced a real array; only the `--set` convenience path was
broken.

## Guidance

**The generator already knows every field's type from the request schema. Emit a
per-op `map[string]string` of dot-path → JSON kind, and parse each `--set` value
against its target kind: JSON-decode arrays/objects, coerce scalars to their
declared type, and reject a mismatch with a hint. Never coerce a value into a
type that doesn't match the target field.**

### The generated type map

`setFieldTypeMap` walks the request schema — top-level properties plus one level
of object nesting (the same depth as `flattenSchemaToScalarFields`) — recording
each writable field's kind (`string`/`integer`/`number`/`boolean`/`array`/`object`).
`setFieldTypesLiteral` emits it as a Go map literal into each `--set` call site:

```go
setDoc, serr := buildMergePatchFromSet(flagSet, map[string]string{
    "customPackageIds": "array", "skipSetupItems": "object",
    "enrollmentSiteId": "string", "versionLock": "integer", /* ... */ })
```

### The runtime parser

`parseSetValue(key, val, kind)` dispatches on the declared kind:

- `array` / `object` → JSON-decode (`parseJSONSetValue`, rejects trailing garbage)
  and verify the decoded value is the right JSON kind; error with a stdin/JSON hint
  otherwise.
- `integer` / `number` / `boolean` → `strconv` coerce; error if it doesn't parse.
- `string` → keep the literal text (never infer a number/bool — this also fixed
  numeric-looking string IDs like `enrollmentSiteId=-1` being sent as a number).
- `""` (field absent from the map — unmodelled schema, or a path deeper than the
  two-level walk) → best-effort: JSON-parse `[`/`{`-prefixed values, else fall back
  to the legacy `parsePatchValue` inference. Nothing that used to work regresses.

`null` always yields JSON null, regardless of kind (escape hatch, and back-compat).

`checkSetParentKind` additionally rejects a dotted key whose parent path is an
array or scalar (`customPackageIds.0=...`) — dot notation can only descend into
objects, so that input was always a bug that silently built a bogus nested object.

### deepMergeJSON already does the right thing for arrays

`deepMergeJSON` merges objects key-by-key but **replaces** arrays wholesale, and
JSON Merge Patch (RFC 7386) replaces arrays too. So a correctly-parsed array in
the set map replaces the fetched value — parsing (not just erroring) is a genuine
feature win, not just a safety fix.

## Why this beats the alternatives

**Vs. a pure "looks like JSON → parse" heuristic (what `internal/platform/body.go`
and `internal/security/body.go` already do, lacking schema access):** the heuristic
fixes `customPackageIds=["295"]` but not `customPackageIds=295` (still a silent
number) nor `customPackageIds.0=295` (still a silent object), and it can misparse a
string field whose value happens to start with `[`. The schema-driven path handles
all three issue cases correctly and keeps string fields as strings. The heuristic
is kept only as the unmodelled-field fallback.

**Vs. erroring on all array/object `--set` (the issue's option 2):** rejecting is
strictly less useful than parsing. With parsing, `--set` finally works for arrays
of scalars and even arrays of objects (`criteria=[{...},{...}]`), verified end-to-end.

## Verification

Real create→set→re-fetch→delete cycles against a live tenant confirmed the value
persists with the correct JSON type: `api-roles.privileges` (array of strings),
`advanced-mobile-device-searches.criteria` (array of objects),
`computer-prestages.customPackageIds` (array; note this endpoint returns HTTP 500
while still persisting the mutation — a server bug, unrelated), and
`buildings.zipPostalCode` (numeric-looking string kept as a string).

## Related

- Issue #304; `buildMergePatchFromSet` / `parseSetValue` / `parseJSONSetValue` /
  `checkSetParentKind` in `generator/parser/generator.go` (`registryTemplate`)
- `setFieldTypeMap` / `setFieldTypesLiteral` / `jsonKindOfProperty` — the generator
  side that produces the type map
- `write-only-fields-dropped-by-update-set-2026-07-22.md` — the sibling `update --set`
  gotcha on the same fetch-merge-PUT path
- Tests: `TestBuildMergePatchFromSet_Typed`
  (`internal/commands/pro/generated/patch_helpers_test.go`)
