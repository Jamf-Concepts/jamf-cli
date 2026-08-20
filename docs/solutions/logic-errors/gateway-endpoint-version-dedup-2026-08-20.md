---
title: "A new gateway endpoint version shipped as a second command, leaving the deprecated one holding the plain name"
date: 2026-08-20
category: logic-errors
module: generator/parser
problem_type: logic-error
severity: high
applies_when:
  - "Ingesting a refreshed Platform Gateway or Security Cloud spec"
  - "A spec starts publishing a second version of an endpoint it already had"
  - "Deciding which command name a new endpoint version should take"
  - "Adding table columns for a platform list operation"
tags:
  - platform-gateway
  - security-cloud
  - api-versioning
  - code-generation
  - deduplication
  - table-columns
---

## What happened

Jamf Security Cloud's GitOps build v1353 published `GET /v2/tenant/{t}/groups` alongside
the device-groups `GET /v1/…/groups` it already had, and marked v1 deprecated
(`x-deprecation-date: 2026-08-12`, `x-sunset-date: 2027-08-12`).

The CLI shipped **both** as commands: `security device-groups list` on the deprecated v1,
and `security device-groups list-v2` on its successor. The v2 command was also broken —
it printed the raw `{"groups": […]}` envelope instead of the list, so `-o csv` emitted a
single column named `groups` containing a JSON blob.

This was not a missing feature. `deduplicateVersionedOps` — the function whose whole job
is "two versions of one endpoint become one command on the highest version" — was already
running on platform specs. It just could not see the version.

## Why the dedup silently didn't fire

Two independent bugs, both from assuming the Jamf Pro path shape:

1. **`stripVersionPrefix` only strips a *leading* version segment.** That is correct for
   Jamf Pro (`/v1/computers-inventory`), but the gateway puts the version after the
   service namespace (`/api/securitycloud/v1/groups`) and sometimes after the tenant
   (`/api/securitycloud/uem-connect/v1/connectors`). The dedup key therefore retained
   `/v1/` and `/v2/`, the two ops never collided, and both survived.

2. **`compareAPIVersions` ranked by the leading segment too.** Every gateway path scored
   0, so even once the keys did collide the "prefer the higher version" branch was a tie —
   resolved by map iteration order. Latent, but it would have picked the winner at random.

Both now read the version wherever it sits, via `stripVersionSegments` and
`apiVersionRank`. Verified before switching: stripping *every* version segment introduces
exactly one new collision across all 834 Jamf Pro operations and every published platform
spec — the v1/v2 device-groups list it is meant to catch.

## The naming decision, and why it is not the SDK's

`jamfplatform-go-sdk` keeps both versions and adds v2 alongside v1, by an explicit
additive rule. Copying that into the CLI is the wrong call, because the two products are
protecting different things:

- The SDK's constraint is **Go API compatibility**. Renaming or removing
  `ListGroupsV1` breaks downstream compilation, so it keeps both until Jamf removes the
  path from the spec.
- The CLI's constraint is **which name a human types**. If `list` stays on v1, sunset day
  breaks the *primary* command name and forces every user onto a differently-named
  command. If `list` is v2 now, sunset is a no-op — the v1 entry just stops being
  generated.

The wire made the switch free: v1 and v2 return the same seven groups, field for field,
differing only in the envelope (`{groups: […]}` vs a bare array) — which the CLI unwraps
anyway. So `list` moved to v2 with no observable change in output.

## FallbackPaths is populated and deliberately unused

Dedup records the displaced v1 path on the winner's `FallbackPaths`, and
`generator/platform/template.go` ignores it. That is intentional:

- Jamf Pro retries a displaced older path on 404 because **customers run older Jamf Pro
  versions**. The gateway is a single deployment — there is no tenant that has v1 but not
  v2 — so the retry has nothing to protect against.
- An unrouted gateway path answers **403 `BAD_PERMISSIONS`**, not 404, and that is
  indistinguishable from a genuine privilege failure. Falling back on it would convert a
  permission problem into a silent downgrade to a deprecated endpoint — the same failure
  class as the `baselineId` rename that answered 0 rules for every baseline with no error.

If v2 is ever un-routed, the right outcome is a loud failure, not a quiet older answer.

## The second bug this surfaced

Driving the switched command through the CLI exposed an unrelated, pre-existing defect:
`platformTableColumns` was keyed on the bare resource name, and **two** specs produce a
`device-groups` — the Jamf Pro device group inventory and Security Cloud's device groups.
The rename to `platform-device-groups` runs before the column lookup, so the columns
landed on exactly the wrong resource: Security Cloud groups (which carry only `id` and
`name`) printed four permanently empty columns, while the Pro inventory the columns
actually describe rendered with none.

Now keyed `{service}/{name}`, matching `platformResourceNameOverrides`.

## Rules

- **Read the version from wherever it sits in the path.** Anything keyed on "the first
  segment" is a Jamf Pro assumption and is wrong for every gateway path.
- **The unversioned command name belongs to the newest version**, and the older version
  stops being a command. Do not ship `list` + `list-v2`; a version suffix on the successor
  while the deprecated endpoint holds the clean name is backwards.
- **Do not copy the SDK's additive-versioning rule into the CLI.** It exists to protect Go
  compilation, and the CLI has no equivalent constraint.
- **Key any per-resource generator table by `{service}/{name}`.** A bare platform resource
  name is not unique, and a collision here fails silently — nothing errors, the data just
  attaches to the wrong command.
- **When a spec is bumped, diff its paths and schemas**, not its extensions or a release
  preview. Build v1353 moved the version into the categories paths and dropped
  `x-jamf-tenant-path-version` in the same change; a spec that gained the prefix while
  keeping the extension would have produced `/v1/v1/…`.

## Verification

Wire-probed against tenant `wisconsam` (`928260f5-…`) on 2026-08-20: v1 and v2 group
payloads diffed identical after envelope unwrapping; the full lifecycle
(`create` → `update --name` → `get --name` → `delete --name`) driven through the v2-backed
resolver; `-o csv` confirmed clean. Regression tests in
`generator/parser/parser_test.go` (`TestStripVersionSegments`, `TestAPIVersionRank`,
`TestDeduplicateVersionedOps_GatewayPathShape`,
`TestDeduplicateVersionedOps_DistinctGatewayServicesSurvive`) and
`generator/platform/generator_test.go` (`TestPlatformTableColumns_KeyedByService`).

`TestSecurityCloudSpecParity` was counting raw spec operations, so the collapse read as a
regression (47 declared, 46 exposed). It now counts distinct *endpoints* — method plus
version-stripped path — which encodes the dedup rule rather than working around it.
