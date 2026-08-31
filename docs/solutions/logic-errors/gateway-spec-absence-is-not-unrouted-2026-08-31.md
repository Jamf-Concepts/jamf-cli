---
title: "The gateway's published spec is the contract; the wire is transitional"
date: 2026-08-31
category: logic-errors
module: generator/gateway
problem_type: logic-error
severity: high
applies_when:
  - "Deciding whether the platform gateway serves a Jamf Pro or Classic endpoint"
  - "Adding an entry to probedUnserved or forceServed"
  - "Ingesting a newer jamf/public-apis-oas GitOps bundle"
  - "A gateway request answers 403 BAD_PERMISSIONS and the privilege looks correct"
  - "Tempted to allow an endpoint because it demonstrably works today"
tags:
  - platform-gateway
  - code-generation
  - wire-facts
  - error-messages
---

## The problem being solved

The gateway does not expose every Jamf Pro endpoint, and it does not say so. An
unrouted path answers **403 `BAD_PERMISSIONS`** — byte-identical to a missing
API-role privilege — so `pro app-installer-titles list` on a platform profile
sends an operator hunting for a grant that cannot help. That was already known
for app installers and handled by one hardcoded prefix in
`gatewayUnservedNote`. The task was to generalise it: derive the whole unserved
set, refuse those commands before sending, and do the reverse too (a
Platform-only command on an instance profile).

## What was wrong, and it was the design

The obvious oracle is the gateway's own published Pro API. At the time, two
artefacts in the jamf/public-apis-oas GitOps release bundle described it, and for
Jamf Pro they were genuinely distinct:

| artefact | paths |
|---|---|
| `external/jpapi/openapi.yaml` — the Pro API as published **on the gateway** | 528 |
| `external/_permissions/routes.yaml`, `domain: pro` — route-to-scope map | 489 |

*(Both are now superseded as a source — see "Where the manifest comes from" at
the end. The reasoning below is unaffected: the SDK publishes the same content.)*

Diffed against this repo's `specs/*.yaml` (both 11.31.0, so no version skew),
**43 of 757 operations** are absent from both. They cluster coherently —
app-installers, `api-integrations`/`api-roles`/`api-role-privileges`, the
`auth`/`oauth` families, `environment-type`, `system/initialize-database-connection`
— and two independent artefacts agreeing reads like strong evidence. The design
refused all 43 before sending.

**Fifteen of the 43 answer on the wire.** Probed 2026-08-31, EU, tenant-scoped
credential:

| endpoint | answer |
|---|---|
| `GET /pro/v1/api-roles` | **200**, 15 roles |
| `GET /pro/v1/api-integrations` | **200**, 81 integrations |
| `GET /pro/v1/api-role-privileges` | **200**, the full privilege list |
| `GET /pro/v1/api-role-privileges/search` | **400** — *Jamf's* envelope, for a missing parameter |
| `GET /pro/v2/mdm/commands` | **400** — Jamf's envelope, likewise |
| `GET /pro/v1/macos-managed-software-updates/available-updates` | **503** — Jamf's, naming the Managed Software Update toggle |

A Jamf-issued status is the proof: the request reached Jamf, so the gateway
routed it. Shipping the strict rule would have broken four working command
families on every platform profile, with no override — and the operator would
have had no way to tell that from a real routing gap.

So: **the deployed Tyk route set is broader than either published artefact.**
Spec absence is a hint about documentation, not a fact about routing.

## Why the wire cannot settle the rest either

The remaining 403s (`/pro/v1/auth`, `/pro/v2/environment-type`,
`/pro/v1/oauth2/session-tokens`) look unrouted but cannot be shown to be. A
fabricated path answers identically:

```
GET /pro/v1/definitely-not-a-real-endpoint  → 403 {"httpStatus":403,"traceId":…,
                                                  "errors":[{"code":"BAD_PERMISSIONS",…}]}
GET /pro/v1/auth                            → 403  … same body, same headers
```

Same status, same code, same description, same `x-tyk-trace-id` header set —
only the trace id differs. A bare Tyk `404 page not found` distinguishes an
unknown **namespace** (`/nosuchnamespace/v1/x`) and nothing finer. So "I got a
403 and I think I have the privilege" is not a probe.

## Correction: the wire is the transitional state, not the truth

The paragraphs above are the discovery, and they were half the answer. Absence
from the published artefacts is indeed not evidence that an endpoint is
**currently** unrouted — that part stands. But it does not follow that absence
means nothing:

> **The gateway's route set is being narrowed onto its published surface.** The
> endpoints answering today while absent from `jpapi/openapi.yaml` and
> `routes.yaml` are routed transitionally, and are expected to stop.

So the published artefacts are a **leading indicator**, not a stale document.
That inverts the treatment. "It returns 200 today" is a reason to refuse it
*sooner*, not a reason to allow it: every day a workflow keeps working against an
endpoint outside the supported surface is a day the eventual failure gets more
expensive, and that failure arrives as an unexplained `403 BAD_PERMISSIONS` with
no signal that a withdrawal caused it.

The first design refused on the artefacts and was wrong about why. The second
allowed on the wire and was wrong about what. Both mistakes came from treating
one artefact as the whole truth.

## The fix

**Every operation outside the gateway's published surface is refused on a gateway
profile**, before a request is sent, by `checkAPIMatch`
(`internal/commands/gateway_coverage.go`) with `exitcode.Usage`. One level,
`unserved`. What varies is the *evidence*, carried as `Basis`, which selects the
wording and nothing else:

- **`probe`** — a recorded, corroborated wire probe found it unrouted. The
  message states the fact. One entry: `/pro/v1/app-installers`, 17 operations.
- **`unpublished`** — absent from the published artefacts. The message says the
  endpoint may still answer today, that this is transitional, and that it is
  refused now rather than later. 27 operations: `api-roles`,
  `api-integrations`, `api-role-privileges`, the `auth`/`oauth` families,
  `environment-type`, `oauth2/session-tokens`,
  `system/initialize-database-connection`, `POST /v2/mdm/commands`, and
  `classic-computer-configs`.

The `unpublished` wording carries the load. Several of those endpoints work right
now, so a bare "not served by the gateway" reads as a CLI defect to anyone whose
command demonstrably returns data — the message has to explain that it is being
stopped *because* it works and won't. `TestCheckAPIMatchRefusesAnUnpublishedEndpointAndExplainsWhy`
pins that, asserting the explanation and not just the refusal.

`forceServed` survives as an escape hatch and is **empty**. Its bar is
deliberately high, and specifically **not** "the wire says this still works" —
that is the transitional state, not a counter-example. An entry asserts the
published surface is *wrong*. Where it genuinely matters is Classic, whose spec
trails the Pro one (11.28.0 against 11.31.0): a Classic resource added since
11.28 is indistinguishable here from one withdrawn, and would be refused on a
stale spec. One resource is affected today (`computerconfigurations`), and the
instance 404s it too, so it is dead rather than new.

## Where the manifest comes from

`specs/gateway/coverage.json` is derived from **jamfplatform-go-sdk's published
`api/pro_api.json` and `api/classic_api_resource_documentation.json`**, the same
source as `specs/platform/`. Two things had to become true first, and both did in
SDK `adb8d7b`:

- **Completeness.** The SDK's build used to strip 24 paths the gateway declares —
  inventory-preload v1, the whole team-viewer preview family, `cache-settings`,
  `settings/obj/policyProperties`, `cloud-azure/defaults/mappings`,
  `dss-declarations/{id}`, `jamf-pro-server-url/history`,
  `macos-managed-software-updates/send-updates`. Deriving coverage from it then
  would have refused two dozen commands the published surface carries. `adb8d7b`
  whitelisted the remaining 38 jpapi paths; the published spec is now
  method-for-method identical to the gateway's own drop.
- **Scopes.** A verdict wants the gateway scope per operation, and only the
  bundle's `_permissions/routes.yaml` used to carry it. The SDK's
  `x-required-privileges` now reproduces it exactly — 1352 operations across both
  specs, zero disagreements, nothing on either side alone.

The bundle route was **deleted rather than kept as a fallback**. A second source
nobody exercises is how the earlier gateway URL-shape bug survived weeks, and the
two would drift the moment one moved.

Two shapes the manifest still has to respect. A path item's keys are **not all
operations** — OpenAPI allows a path-level `parameters` array beside the methods —
so typing every key as an operation fails outright on the first path that
declares shared parameters (`/v1/log-flushing/task/{id}`, in this spec). And an
operation with **no scope is not an absent operation**: 44 Jamf Pro endpoints are
unauthenticated (`/v1/health-check`, `/v1/jamf-pro-version`, `/v1/locales`) and
declare none.

**Classic trails Pro, and that is the live hazard.** Its spec is 11.28.0 against
the Pro API's 11.31.0, so a Classic resource added since 11.28 is
indistinguishable here from one withdrawn, and would be refused on a stale spec.
One resource is affected today (`computerconfigurations`), and the instance 404s
it too, so it is dead rather than new. Classic is also judged per **subtree**, not
per path: five resources have no bare collection endpoint at all
(`computerhistory`, `computerapplications`, `mobiledevicehistory`,
`patchavailabletitles`, `patchreports` are reachable only as
`/computerhistory/id/{}`), so the exact-path form reported five served resources
as absent.

## The reverse direction

A Platform-only command on an instance profile was already gated by
`platform.RequirePlatformClient`, but its message explains how to *set up* a
platform profile without saying that the profile in hand is an instance one — so
an operator with perfectly good oauth2 credentials reads it as a credential
problem. `checkAPIMatch` now refuses it by annotation (`jamf:api:
platform-gateway` against a non-platform provider) and names the profile, its
auth method, and `platform setup`. Not extended to the `security` namespace,
whose own resolver returns before this check and already hints about the base URL.

## Lessons

1. **Two artefacts from one build are not two sources.** They shared an upstream;
   agreeing tells you the documentation is consistent, not that it is complete.
2. **Probe before shipping a rule derived from a spec** — and then ask what the
   probe result *means*. Eight curl calls disproved "the spec says it is gone, so
   it is gone". They did not establish "so it should be allowed": that needed the
   direction of travel, which no artefact and no probe carries. Wire evidence
   tells you the present state, never the intent.
3. **A published API surface is a forward contract, not a description.** When the
   wire is wider than the spec, ask which one is moving. Here the wire is
   converging onto the spec, so the spec is the thing to build against and the
   extra routes are a grace period.
4. **When behaviour is right but the reason is wrong, the code is not safe.** The
   first design refused these endpoints on evidence that did not support it. It
   would have "worked" — and the first bundle that legitimately declared one of
   them would have kept refusing it, with a comment explaining a rationale nobody
   could check. Getting the reason right is what makes `forceServed`'s bar
   describable.
5. **An override table that immediately needs five entries is telling you
   something.** Here it was telling me the oracle was being read with the wrong
   question, not that the data needed patching. The five entries are gone.
