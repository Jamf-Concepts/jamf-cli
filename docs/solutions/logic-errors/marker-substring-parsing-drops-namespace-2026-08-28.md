---
title: "Cutting a URL on \"/api/\" returned empty when the segment went away, silently dropping the namespace from every generated path"
date: 2026-08-28
category: logic-errors
module: generator/parser
problem_type: logic-error
severity: high
applies_when:
  - "Ingesting a refreshed platform spec whose servers[0].url shape has changed"
  - "A generated platform command 404s and nothing in the build reported a problem"
  - "Extracting a structural part of a URL (host, namespace, version) from a spec"
tags:
  - platform-gateway
  - code-generation
  - url-construction
  - silent-failure
---

## What happened

At the Jamf Platform API GA the gateway moved from `{region}.apigw.jamf.com`, which
required an `/api` segment, to `{region}.api.jamfcloud.com`, which mounts every
namespace at the root. GitOps build v1807 followed: `servers[0].url` went from
`https://{region}.apigw.jamf.com/api/blueprints` to
`https://{region}.api.jamfcloud.com/blueprints`.

`serviceSegment` read the namespace by cutting the URL on the literal `"/api/"`:

```go
const marker = "/api/"
_, after, ok := strings.Cut(url, marker)
if !ok {
    return ""
}
return after
```

The new URL has no `/api/` substring. The host's `api` is **dot**-delimited
(`.api.jamfcloud.com`), not slash-delimited, so `Cut` failed and the function
returned `""` — its documented "URL does not match the expected gateway shape"
answer. An empty service makes `normalisePlatformPaths` build no prefix at all,
so every path came out as `/v1/blueprints` instead of `/blueprints/v1/blueprints`.

No error. No warning. `make generate` succeeded, `make test` passed, and every
platform command would have 404'd at runtime with the gateway's bare
`404 page not found` — indistinguishable from a wrong path.

## Why it was invisible

Three things lined up:

1. **The failure is a valid return value.** `""` is what the function returns for
   a URL it does not recognise, and there is no caller that can tell "no namespace
   in this spec" from "I failed to read the namespace".
2. **The assertion tested for the bug's own shape.** The generator tests asserted
   `strings.HasPrefix(op.Path, "/api/")`. Under the new specs those fail, which is
   lucky — but only because the prefix was checked *literally*. An assertion of
   "the path starts with a namespace" would have caught the class; an assertion of
   "the path starts with `/api/`" caught this instance and would have had to be
   deleted to make the new specs build.
3. **A near-miss substring.** The marker still appears in the URL, just not with
   the delimiters that make it a path segment. Any grep for `/api` in the new spec
   finds nothing, but a human reading `https://{region}.api.jamfcloud.com/blueprints`
   and asking "does this contain /api/?" can talk themselves into yes.

## The fix

Parse the structure rather than matching a marker: take the URL's **path**, then
drop a leading `api/` segment if present.

```go
if _, after, ok := strings.Cut(rawURL, "://"); ok {
    rawURL = after
}
_, path, ok := strings.Cut(rawURL, "/")
if !ok {
    return ""
}
path = strings.Trim(path, "/")
if rest, ok := strings.CutPrefix(path, "api/"); ok {
    return strings.Trim(rest, "/")
}
return path
```

Dropping rather than requiring `api/` is not tidiness — one spec drop legitimately
mixes both shapes. The six Platform specs are v1807 and carry no `/api`; the five
Security Cloud specs are generated from a different upstream tree that still
declares it. Both have to yield the same namespace from the same drop.

`TestServiceSegment` pins both shapes plus the two that must return `""` (a
host-only URL, and a bare `/api`). The generator assertions were rewritten as
`assertNamespacePrefixed`, which checks the path is **not** under `/api` **and**
that its first segment is not version-shaped — the second half being the direct
test for "no namespace was read".

## Rules

- When you need a structural part of a URL — host, namespace, version — parse the
  structure. A substring marker survives only until the surrounding shape changes,
  and it fails by returning the caller's "not applicable" value.
- A function whose failure and whose legitimate empty answer are the same value
  needs an assertion on the *consequence*, not on the return. Here the consequence
  is a path with no namespace, which is what the test now checks.
- Assert the property, not the literal. `HasPrefix(path, "/api/")` was a test for
  one build's URL shape; `serviceFromPath(path) != ""` is a test for the invariant
  that actually has to hold.
