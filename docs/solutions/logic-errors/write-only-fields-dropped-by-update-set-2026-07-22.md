---
title: "update --set silently drops write-only fields; warn on every one, not just required top-level"
date: 2026-07-22
category: logic-errors
module: generator/parser
problem_type: bug
severity: high
applies_when:
  - "Adding or auditing the fetch-merge-PUT 'update --set' code path in the modern API generator"
  - "A PUT request schema contains writeOnly fields (passwords, secrets, keystores, tokens)"
  - "A user reports a credential field blanked after an unrelated update --set change"
tags:
  - generator
  - update-set
  - write-only
  - fetch-merge-put
  - credentials
  - data-loss
  - computer-prestages
---

# update --set silently drops write-only fields

## Context

`update --set KEY=VALUE` on a modern API command is **not** a partial update. It
is fetch → merge → PUT-the-whole-record (`fetchForMerge` + `deepMergeJSON` in the
generated `registry.go`; template in `generator/parser/generator.go`
`resourceTemplate`). The current resource is GET'd, read-only fields are stripped
via the `fieldFilter` allowlist, the caller's `--set` changes are merged, and the
full body is PUT back.

Write-only fields (`writeOnly: true` in the spec — passwords, secrets) are
**never returned by the GET**. So they are absent from the fetched body, absent
from `--set` unless the caller names them explicitly, and therefore sent back
**blank** on the PUT. The server treats the missing value as "clear it."

Reported via issue #302: `jamf-cli pro computer-prestages update 12 --set
department="Config Management"` silently blanked the managed local admin password
(`accountSettings.adminPassword`). The Jamf UI then showed an empty password field
and often the server returned a 500 while still persisting the mutation (the
resulting managed-admin state — enabled, no password — is invalid).

The pre-existing guard, `writeOnlyRequiredFields`, only warned for fields that
were **both required AND top-level** in the PUT schema:

```go
for name, prop := range s.Properties {
    if prop.WriteOnly && required[name] {   // top-level + required only
        out = append(out, name)
    }
}
```

`accountSettings.adminPassword` is nested one level down and is not in the
schema's `required` list, so the warning never fired. Same for
`recoveryLockPassword`, distribution-point passwords, SMTP/GSX/SSO/LDAP secrets,
and user-account passwords — every one of them slipped through silently.

## Guidance

**Any write-only field on a PUT-update endpoint is undetectably dropped by
`update --set` unless the caller re-supplies it. Warn on every one — at every
nesting level, regardless of `required`.**

The warning is the only safety net possible here: the CLI physically cannot read
the value back to preserve it, so it cannot fix the data loss — only tell the user
it is about to happen and how to prevent it.

### The detector

Walk the PUT request schema recursively, depth-capped and `$ref`-cycle-guarded
(mirror `schemaFilterLiteral`'s traversal), returning **dot-notation paths** of
every write-only field. Do not gate on `required`:

```go
func writeOnlyFields(op *Operation, schemas map[string]*Schema) []string {
    if op == nil || op.RequestBody == nil || op.RequestBody.Schema == nil {
        return nil
    }
    var out []string
    collectWriteOnlyFields(op.RequestBody.Schema, schemas, "", 0, map[string]bool{}, &out)
    sort.Strings(out)
    return out
}
```

### The runtime check

`--set` pairs build a nested map (`buildMergePatchFromSet`), so presence of a
nested field must be checked by walking the dot-path — a top-level
`setMap["accountSettings.adminPassword"]` lookup would always miss:

```go
func hasNestedKey(m map[string]any, path string) bool { /* walk keys split on "." */ }
```

### The warning

Name the field, state that the update will blank it, and give the exact
incantation to preserve it:

```
warning: computer-prestage field "accountSettings.adminPassword" is write-only:
the server never returns it, so this update will blank any existing value.
Pass --set accountSettings.adminPassword=<value> to preserve it.
```

## Why this beats the alternative

**Vs. warning only on required top-level fields (the old behavior):** secrets are
almost never top-level and almost never marked `required` in Jamf's PUT schemas,
so the narrow guard caught essentially nothing. A false-positive warning (nagging
about a field that happened to be empty anyway) is cheap noise; a false negative
here is silent credential loss on production prestages. The asymmetry is decisive
— warn broadly.

**Vs. auto-preserving the value:** impossible. Write-only means the GET never
returns it; there is nothing to preserve. `patch` (JSON Merge Patch, changed
fields only) would sidestep the full-replace — but many of these resources
(ComputerPrestagesV3 included) are PUT-only, so `patch` is not available. The
warning is the ceiling of what the client can do.

## Related

- Issue #302, and the `writeOnlyFields` / `collectWriteOnlyFields` / `hasNestedKey`
  helpers in `generator/parser/generator.go`
- `writableFilterLiteral` / `schemaFilterLiteral` — the sibling recursion whose
  depth-cap and cycle-guard shape `collectWriteOnlyFields` copies
- Tests: `TestWriteOnlyFields` (parser), `TestHasNestedKey` +
  `TestUpdateSetWriteOnlyWarning` (`internal/commands/pro/generated/update_set_test.go`)
