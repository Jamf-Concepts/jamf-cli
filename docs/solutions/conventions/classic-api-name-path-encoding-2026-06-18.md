---
title: "Encode classic API name path segments with registry.EscapeClassicPathSegment, not url.PathEscape"
date: 2026-06-18
category: conventions
module: internal/registry
problem_type: convention
severity: medium
applies_when:
  - "Building a /JSSResource/<resource>/name/{name} (or other name/value) classic API path"
  - "Adding a classic command or resolver that looks up by name"
tags:
  - classic-api
  - url-encoding
  - name-lookup
  - platform-gateway
  - rsql
---

# Encode classic API name path segments with `registry.EscapeClassicPathSegment`

## Context

`jamf-cli pro classic-advanced-computer-searches get --name "Aged 5+ Years"`
returned 404 (issue #244). The CLI built the path with `url.PathEscape`, which
left the `+` literal (`/name/Aged%205+%20Years`). The classic API **form-decodes
`+` to a space** inside the path, so the server looked up `Aged 5  Years` and
found nothing.

`url.PathEscape` is wrong for this because per RFC 3986 `+` is a valid path
character, so it is intentionally left literal — there is no stdlib path
escaper that encodes it. Verified live against the classic API: only `+` is
mishandled; `& = : @ $ ( ) , ; ' % ? #`, spaces and non-ASCII all round-trip
fine under `PathEscape`.

## Guidance

For any classic API path segment built from a user-supplied name/value, use:

```go
registry.EscapeClassicPathSegment(name)
```

It percent-encodes via `url.QueryEscape` (which escapes `+` as `%2B` along with
every other reserved character) then rewrites the space-as-`+` it emits back to
`%20` — a fully percent-encoded segment that round-trips regardless of whether
the server decodes it as a path or a form value. ID-path segments (numeric, no
`+`) can stay on `url.PathEscape`.

All generated classic name/value lookup sites (get/update by name, the registry
name resolvers) and the hand-written `internal/scope` and `internal/resolve`
group-name lookups route through this helper.

## Known limits (not client-fixable)

- **`/` in a name** encodes to `%2F`, which the classic API rejects (HTTP 400).
  Such objects cannot be looked up by name through `/name/{name}` at all — fall
  back to id.
- **Platform Gateway + `+`:** through `auth-method: platform`
  (`/api/proclassic/tenant/{id}/...`) the gateway's proclassic proxy drops `+`
  from the name path under *every* client encoding (`%2B`, `%252B`, literal `+`
  all 404), while id lookups and plain names proxy fine. This is a server-side
  gateway defect, escalated separately; no CLI encoding works around it. Use id
  lookup, or a direct (non-gateway) connection.

## Related: RSQL `--filter` is not an encoding problem

A sibling report (issue #246) — `--filter "version==7.0.5 (81138)"` returning
HTTP 500 — is **not** a CLI bug. `url.QueryEscape` already encodes the parens
(`%28`/`%29`); the server decodes them back and the RSQL parser treats bare
parens as grouping operators. The fix is RSQL quoting on the caller's side:
`--filter 'version=="7.0.5 (81138)"'`. Don't try to "fix" parens in the CLI.
