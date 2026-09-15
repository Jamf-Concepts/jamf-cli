---
title: "Classic scope writes send only <scope>, and the element order in it is load-bearing"
date: 2026-09-12
category: conventions
module: internal/scope
problem_type: wire-contract
severity: high
supersedes: docs/solutions/conventions/scope-put-avoid-subset-2026-07-08.md
applies_when:
  - "Writing a Classic API mutation that edits one section of a resource"
  - "Assembling a Classic XML body from bytes the server returned"
  - "Adding or touching internal/scope"
tags:
  - classic-api
  - platform-gateway
  - scope
  - xml-binding
---

# Classic scope writes send only `<scope>`, and the element order in it is load-bearing

## Context

`internal/scope`'s `PutScope` used to GET the whole resource document, splice a
new `<scope>` block into its bytes, and PUT the entire document back. With the
name-keyed fetch in front of it and the post-write verification behind it, one
`scope add` cost **three GETs and a PUT**, and every write re-sent sections the
caller had not touched — a policy's packages and scripts, a configuration
profile's `<payloads>`.

The predecessor doc explains why the `/subset/Scope` shortcut was abandoned (the
platform gateway's Classic proxy forwards only top-level paths, so it answers
403 there). It concluded "GET the full document, patch the element, PUT the
whole document back", and the full-document half of that was never necessary.

## What the wire actually does

Probed on Jamf Pro 11.31.1 on 2026-09-12, against a direct instance and through
the GA platform gateway, across all eight scopeable Classic resources.

**A Classic PUT is a partial update at top-level-section granularity.** A body
of `<policy><scope>…</scope></policy>` applies the scope and leaves every other
section byte-identical — verified by diffing each resource's whole document with
the `<scope>` block elided, including a 19 KB configuration profile's
`<payloads>`.

**`<scope>` itself is replaced wholesale.** A body carrying only some scope
categories wipes the rest: sending just `<computer_groups>` cleared the
resource's buildings, limitations and exclusions. So the entire block has to be
sent, which is exactly what a read-modify-write already holds. An empty category
element (`<computer_groups></computer_groups>`) is what clears a category; a
body whose scope has no category elements at all is ignored.

**The element order inside `<scope>` decides whether the write does anything,
and the order a GET returns is not that order.** The Classic API's XML binding
is sequence-ordered: it reads scope children in schema order and silently
ignores whatever arrives out of it, answering **200 either way**. This is the
trap that cost this investigation an hour of wrong conclusions. Echoing a
resource's own bytes back — the obvious way to build a "safe" scope-only body —
is accepted and applied to nothing, on some resources and not others:

| resource | order its GET returns `<exclusions>` in |
|---|---|
| `policy`, `os_x_configuration_profile` | computers, buildings, departments, computer_groups, users, … |
| `macapplications` | **buildings, departments, mobile_device_groups**, users, user_groups, network_segments, computers, computer_groups, … |

A hand-assembled scope-only PUT applied cleanly to policies and profiles and
was a silent no-op on `macapplications` and `mobiledeviceapplications`, which
looked exactly like "these two resources need more than `<scope>` in the body".
They do not. Marshalling through `encoding/xml` from `ScopeXML`, whose field
order *is* the schema order, applies on all eight.

**`<limit_to_users>` need not be modelled at all.** The server denormalises
`<limitations><user_groups>` into `<limit_to_users><user_groups>` on every write
and back again on every read, so the two wire paths always carry identical
values. Verified in both directions: writing only `limitations.user_groups`
populates `limit_to_users`; writing only `limit_to_users` populates
`limitations.user_groups`; and an empty `limitations.user_groups` with
`limit_to_users` omitted clears both. (When both are sent and disagree,
`limit_to_users` wins — which is why the old policy-only special case was
correct, just unnecessary.) Dropping it removed a struct, a slice type and
three functions, plus a branch in each of five others.

**PI-827 is not avoided by sending less.** A configuration profile whose
`<payloads>` carries an escaped entity loses one escaping level on **any**
successful save, whether or not the body carries `<payloads>` — the server
re-serialises the stored payload on write. After that decay the stored payload
is no longer re-saveable and every further write answers 409. So a scope-only
body is smaller and touches fewer fields, but it buys **no** payload-integrity
guarantee, and this doc deliberately does not claim one.

## Guidance

`PutScope` marshals `<singularKey><scope>…</scope></singularKey>` and PUTs it to
`/JSSResource/{path}/id/{id}`. One request, no splicing, `replaceScopeInXML`
deleted.

**Do not re-order `ScopeXML`'s fields.** The field order is the schema order and
is what makes the write take effect. Re-ordering them to match any one
resource's GET breaks the others, with no error and a 200.
`TestMarshalScopeBody_FieldOrderIsSchemaOrder` is the guard.

**When adding a Classic mutation that edits one section:** send only that
section, marshalled from a typed struct whose field order follows the schema.
Never assemble a Classic body by string-splicing bytes the server returned — it
works until it reaches a resource whose GET order differs, and then it is a
silent no-op.

**Do not reach for `/subset/{Name}`.** Re-probed 2026-09-12: `PUT
/JSSResource/policies/id/{id}/subset/Scope` answers 201 on a direct instance
(with the `<policy>` wrapper; a bare `<scope>` body is a 400) and **403 through
the platform gateway**. One code path that works on both beats two that
disagree.

## Reading a Classic failure

The Classic API reports a refused write as an HTML status page, and the reason
in it is specific and actionable. `classicHTMLErrorReason`
(`internal/client/client.go`) extracts it, because passing the page through
verbatim buried the answer in ~400 bytes of markup and inline CSS — one
unreadable escaped line inside the JSON error envelope. Reasons seen in this
work, each of which was previously invisible:

- `Error: Unable to match computer group` — the identifier named no record.
- `Error: Mobile device groups cannot be assigned to an macOS profile` — a
  cross-family category (see the matrix doc below).
- `Error: Unable to match excluded computer group` — a **dangling** reference
  the GET still reports, to an object since deleted. Such a scope is not
  re-saveable by any means.
- `Error: Duplicate name` — two records share a name, so neither can be saved.
  This is why one probe object 409'd on every write regardless of strategy.
- `Unable to update the database` — also seen transiently on objects that saved
  fine on retry, so do not read a single 409 as a property of the request.

## Related

- `docs/solutions/conventions/scope-put-avoid-subset-2026-07-08.md` — the
  predecessor; its `/subset/` conclusion still holds, its full-document
  conclusion is superseded here.
- `docs/solutions/conventions/classic-scope-matrix-2026-09-12.md` — which
  categories each resource carries, and why the CLI refuses the rest.
- `docs/solutions/conventions/classic-profile-payload-ampersand-escaping-2026-07-30.md`
  — PI-827.
