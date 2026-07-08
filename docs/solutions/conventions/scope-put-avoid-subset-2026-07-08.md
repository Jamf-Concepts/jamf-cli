---
title: "Classic API scope add/remove must PUT the top-level endpoint, not /subset/Scope"
date: 2026-07-08
category: conventions
module: internal/scope
problem_type: platform-gateway-limitation
severity: high
applies_when:
  - "Writing a Classic API mutation that targets a /subset/{Name} sub-resource"
  - "Adding or touching internal/scope PutScope-style read-modify-write flows"
tags:
  - classic-api
  - platform-gateway
  - scope
  - subset-endpoint
---

# Classic API scope add/remove must PUT the top-level endpoint, not `/subset/Scope`

## Context

`jamf-cli pro classic-macos-config-profiles scope add ... --computer-group ...`
returned HTTP 403 `BAD_PERMISSIONS` (issue #267) even for a token with full
config-profile privileges. `PutScope` built its request against
`/JSSResource/{path}/id/{id}/subset/Scope` — a Classic API shortcut endpoint
that PUTs (and previously PUT-only) just the `<scope>` element instead of the
whole document.

The 403 only reproduced under `auth-method: platform` (Jamf Platform Gateway).
The gateway's Classic API proxy (`/api/proclassic/tenant/{id}/...`) does a pure
path-prefix rewrite (`internal/client/rewritePathForGateway`) — it doesn't
block or mangle `/subset/...` paths syntactically. The 403 is server-side: the
gateway's proxy only forwards top-level Classic resource paths, not
`/subset/{Name}` sub-resources, regardless of the caller's privileges.

A handful of resources (`restrictedsoftware`, `vppassignments`,
`vppinvitations`) already special-cased this with a per-resource
`NoSubsetPut` flag (`no_subset_put: true` in `specs/classic/resources.yaml`)
because the Classic API itself 404s `/subset/Scope` for them even without a
gateway in the picture — but every other scopeable resource (policies, config
profiles, mac/mobile apps, ebooks) still used the subset shortcut, so they all
broke identically under platform gateway auth.

## Guidance

`PutScope` (`internal/scope/scope.go`) now unconditionally does a
fetch-splice-PUT against the **top-level** endpoint
(`/JSSResource/{path}/id/{id}`) for every scopeable Classic resource — GET the
full document, replace the `<scope>` element via `replaceScopeInXML`, PUT the
whole document back. There is no subset-vs-full branch anymore; the
`Resource.NoSubsetPut` field (and the manifest's `no_subset_put` key) were
removed as dead weight once the shortcut path was dropped entirely.

This works identically against the direct Jamf Pro API and through the
platform gateway, since top-level Classic paths are proxied. The cost is one
extra GET+larger PUT body versus the old subset shortcut, which is negligible
for scope mutations.

**When adding a new Classic mutation:** never build a request against
`/subset/{Name}` if there's any chance it needs to work through the platform
gateway. Fetch top-level, patch the specific element client-side, PUT the
whole document back — the same pattern as `PutScope`/`replaceScopeInXML`.

## Related

See `docs/solutions/conventions/classic-api-name-path-encoding-2026-06-18.md`
for another platform-gateway proxy limitation (the `+`-in-name defect) that
required a similar "avoid the shortcut, use what the gateway actually
proxies" workaround.
