---
title: "Security Cloud URLs were version-first, so the gateway executed mutations it never audited"
date: 2026-08-21
category: logic-errors
module: generator/parser
problem_type: logic-error
severity: high
applies_when:
  - "Ingesting a refreshed Jamf Security Cloud spec from jamfplatform-go-sdk"
  - "Adding a gateway namespace whose paths carry /tenant/{tenantId}"
  - "A generated platform command works but its effect is not recorded anywhere"
tags:
  - platform-gateway
  - security-cloud
  - auditing
  - code-generation
  - url-construction
---

## What happened

Every gateway-served Security Cloud command sent its tenant segment *after* the version:

```
/api/securitycloud/v1/tenant/{t}/dns/zones      ← what the CLI sent
/api/securitycloud/tenant/{t}/v1/dns/zones      ← what it sends now
```

Both orderings are routed. Jamf Security Cloud's Tyk definition is a catch-all proxy, and
the SDK wire-verified 200 for every family in both shapes on 2026-08-21 (including
mutating methods: tenant-first `PATCH /ztna/gateways/{id}` → 204, `POST /groups` → 201).
So nothing failed, no command was broken, and no test caught it.

What differs is **auditing**. The gateway's audit rules — which decide whether a mutating
request is recorded at all — are path globs of the form `/**/v{n}/{service}/…`. Those match
a stripped path only when the tenant precedes the version. Under version-first, most
Security Cloud mutations executed and left no record: a `security dns-zones delete` did
the deletion and wrote nothing an investigation could later find.

This is the worst shape a bug can take. The failure is invisible from the caller's side —
correct output, correct exit code, correct effect — and only shows up much later, as an
absence, to someone asking who changed something.

## Why the CLI had to change at all

The SDK fixed this on its side (`tenantFirstNamespaces` in its `internal/client`, feeding
`TenantPrefix`), but the CLI's generated commands **do not build their paths through
`TenantPrefix`**. They hold a full path string emitted at generation time and only
substitute `{tenantId}` from `Transport().TenantIDFor(service)`:

```go
path := "/api/securitycloud/v1/tenant/{tenantId}/dns/zones"
path = strings.Replace(path, "{tenantId}", url.PathEscape(...), 1)
```

So the SDK bump alone left every generated command on the old ordering. Only
`platform setup`'s Security Cloud tenant probe, which does go through `TenantPrefix`,
moved — which is what surfaced the divergence at all (its httptest mux stopped matching).

## The fix

`tenantBeforeVersion` in `generator/parser/platform.go`, applied in
`normalisePlatformPaths` as each path is rewritten to its full gateway form, gated on
`tenantFirstServices`.

It moves the tenant *to the version* rather than swapping the two, because Security Cloud's
five specs arrive in three different shapes and all three have to land tenant-first:

| Spec | Raw path | Version comes from |
|---|---|---|
| dns, ztna | `/tenant/{t}/dns/zones` | `x-jamf-tenant-path-version` (prefix, ahead of the tenant) |
| categories, device-groups | `/v1/tenant/{t}/groups` | the spec path itself, ahead of the tenant |
| uem-connect | `/tenant/{t}/uem-connect/v1/connectors` | the spec path, *after* the sub-namespace |

uem-connect is why swapping is wrong: its version is not the segment the tenant belongs in
front of, and it is already tenant-first, so the transform must leave it untouched.

## The duplication, and why it is acceptable

`tenantFirstServices` is a second copy of the SDK's `tenantFirstNamespaces` — the CLI
cannot import it (`internal/client`), and the SDK's own generator keeps a third copy for
the same reason.

The duplication is **self-detecting rather than silent**:
`TestValidatePlatformGatewayCredentials_SecurityCloudTenant` registers the probe path
*exactly*, not as a prefix, and the probe is built by the SDK while the constant is written
by hand from the generator's rule. Diverge and the client calls a path the mux never
registered.

Two generator tests pin the rest: `TestTenantBeforeVersion` covers the three shapes plus
"leave every other namespace alone", and
`TestLoadResources_SecurityCloudPathsAreTenantFirst` asserts the ordering on the paths
every committed spec actually produces — both directions, since a non-Security-Cloud
namespace turning tenant-first is equally a bug.

## Wire evidence (tenant `wisconsam`, 2026-08-21)

Confirmed from this repo, not taken on the SDK's word:

| Probe | Result |
|---|---|
| `GET .../tenant/{t}/v1/categories` | 200, 36 categories |
| `GET .../v1/tenant/{t}/categories` | 200, byte-identical |
| `PUT .../tenant/{t}/v1/groups/{id}` | 200 `{id, name}` |
| `PUT .../v1/tenant/{t}/groups/{id}` | 200, byte-identical |
| client id in the tenant slot, either ordering | 403 `OWNERSHIP_FORBIDDEN` |

Every gateway-served command was then run against the tenant in the new shape — all
five DNS/ZTNA families, categories, device groups and UEM connectors read clean, and
`device-groups` round-tripped create → get → update → delete. So the ordering change is
behaviour-neutral on the wire in both directions, which is exactly why nothing caught it.

## Lessons

- **A working request is not a complete one.** Routing, response shape and exit code all
  looked right; the missing thing was on the gateway's side of the call and unobservable
  from the CLI. When an upstream fix is described as being about auditing rather than
  behaviour, that is precisely the kind that no test of ours would have caught.
- **Check whether a shared SDK fix actually reaches this repo.** Generated platform
  commands carry literal paths, so anything the SDK fixes inside `TenantPrefix` bypasses
  them entirely. A dependency bump is not a fix here.
- **When you must duplicate a rule across modules, arrange for the copies to be compared
  by a test that exercises both.** One exact-path httptest registration is enough, and it
  is worth more than a comment asking the next person to keep two files in step.
