# Preparing for the Platform API GA

The Jamf Platform API is leaving public beta. This page is what a jamf-cli user has to
change, and what the CLI now does for you.

> **Provisional, and subject to change without notice.** The Platform API is still moving
> ahead of GA. Version numbers, the refused-command list and the permission names quoted
> here all track a specific SDK ingest — currently `jamfplatform-go-sdk` `589cbe3`, whose
> published surface is Jamf Pro API 11.31.0 and Classic API 11.28.0. Re-read this against
> the release you are actually upgrading to.

**Nothing about a Jamf Pro instance profile changes.** If you authenticate with
`auth-method: oauth2` or `token` against `https://your.jamfcloud.com`, none of this applies
to you. Everything below is about `auth-method: platform` — the gateway.

Jamf's own console-side instructions (registering an integration, the permission picker) are
published at [Getting started with the Platform
API](https://developer.jamf.com/platform-api/reference/getting-started-with-platform-api).
The Terraform provider's [Preparing for the Platform API
GA](https://registry.terraform.io/providers/Jamf-Concepts/jamfplatform/latest/docs/guides/platform-api-ga)
guide covers the same GA in more depth on the Jamf Account side, including the permission
model and the scope levels; it is worth reading alongside this if you use both.

## Action needed

Per platform profile. The first group applies as soon as you take a build carrying this
change, including before GA. The second only at GA.

**Now:**

1. **Point `url` at the GA gateway.** `https://{region}.api.jamfcloud.com`, replacing
   `https://{region}.apigw.jamf.com`. See [Base URL](#base-url).

**At GA:**

2. **Register a replacement API integration in Jamf Account and update the profile's
   credentials.** Beta credentials are revoked at GA and a beta client cannot be migrated.
   Record what each beta integration could reach first, so you can pick the equivalent
   permissions. See [Credentials and permissions](#credentials-and-permissions).
3. **Replace `tenant-id` with `environment-id`**, unless single-product access is
   deliberately what you want. See [Scope](#scope).

There is no state to migrate and no config-file schema change beyond those keys. Re-running
`jamf-cli platform setup` does all three interactively and writes a fresh profile.

## Base URL

The GA gateway is `https://{region}.api.jamfcloud.com` — `us`, `eu` or `apac`. Supply the
host only. Every namespace is served at the root and the beta's `/api` path segment is gone:
a request carrying it gets the gateway's plain-text `404 page not found` rather than a JSON
error.

```yaml
profiles:
  platform-prod:
    url: https://eu.api.jamfcloud.com   # was https://eu.apigw.jamf.com
    auth-method: platform
    client-id: keychain:jamf-cli/platform-prod-client-id
    client-secret: keychain:jamf-cli/platform-prod-client-secret
```

The old host is refused **by name**, before any request is sent:

```
Error: https://eu.apigw.jamf.com is the retired Jamf Platform gateway and does not serve
the GA API paths.
Set url: https://eu.api.jamfcloud.com in this profile (jamf-cli config path prints the
file), or re-run `jamf-cli platform setup`.
```

That refusal exists because the wire symptom is useless: the failure lands in the token
exchange, before your command is sent, as an edge-level 403 with an HTML body that names
neither the host nor the reason. The CLI does not rewrite the URL for you — the profile on
disk stays wrong for every other tool reading it, and a URL you did not type is a bad thing
to send a credential to.

The GA host is already live and accepts a public-beta integration, so you can make and
verify this change before GA, independently of the credential swap.

## Scope

An API integration is created at one of three levels in Jamf Account, and its credential
only works with that level. This is a choice between integrations, not between ways of
naming the same access:

| Level | Profile key | What it reaches |
|---|---|---|
| Organization | *neither key* | Jamf Account (`platform account-licenses`, `sso-*`, `distributor-*`) and AI Governance |
| Platform environment | `environment-id` | a group of tenants across product types — **prefer this**; also the only level `platform audit` accepts |
| Tenant | `tenant-id` | one Jamf Pro / School / Protect / Security Cloud tenant — the legacy level |

Public-beta integrations were tenant-scoped, so most existing profiles carry `tenant-id`. A
GA replacement will usually be environment-scoped:

```yaml
    # tenant-id: 0f9c…          # remove
    environment-id: 4b21…
```

Both are also settable per-invocation (`--environment-id`, `--tenant-id`) and by environment
variable (`JAMF_ENVIRONMENT_ID`, `JAMF_TENANT_ID`), which is the CI/CD route. An explicitly
supplied level **replaces** the profile's rather than joining it, so
`JAMF_ENVIRONMENT_ID=… jamf-cli -p tenant-profile …` means "use this environment".

Three things to know:

- **Supplying both levels at once is refused** with a usage error naming both values, in the
  environment as well as in a profile. Precedence would hide the mistake, and the wire
  symptom is a 403 from whichever half the credential does not match.
- **Sending the wrong level for the credential is a `403 OWNERSHIP_FORBIDDEN`**, even when
  both identifiers belong to you. Sending neither, on a credential that expects one, is
  `400 REQUEST_CONTEXT_NOT_PROVIDED`.
- **Organization scope has no ID**, so the gateway host itself is what selects platform auth
  for it. `JAMF_URL=https://us.api.jamfcloud.com` plus a client ID and secret, with no scope
  ID at all, is a valid organization-scoped setup.

A customer with several tenants and no platform environment keeps a profile per tenant, as
before.

## Credentials and permissions

Credentials are registered in Jamf Account's Platform API integrations UI. One OAuth 2.0
model now covers the platform, replacing one credential set per product. `jamf-cli platform
setup` prompts for region, client ID and secret, and the scope, validates them against the
gateway, and stores the secrets in the keychain. It also reports whether the credential
reaches Jamf Security Cloud rather than passing or failing on it — a Jamf Pro tenant
legitimately cannot.

`jamf-cli security setup` still owns the three Jamf Security Cloud Radar credential pairs
(Risk, Device Lifecycle, SSE), which are unrelated to the gateway and unaffected by GA.

**Permissions are organised by capability and action** — `device-groups:read`,
`compliance-benchmarks:create` — not by Jamf Pro privilege name. The two vocabularies are
independent: the GA consolidation folded several Jamf Pro privileges into one capability,
the computer and mobile pairs collapsed into single device-level permissions, and Jamf
Account no longer offers the old names. Jamf's [Jamf Pro permissions
map](https://developer.jamf.com/platform-api/reference/jamf-pro-permissions-map) is the
reference for the full mapping.

Two consequences worth knowing before you grant anything: an action covers only itself, so a
command that reads a record before writing it needs both; and `erase`, `unmanage` and
`platform-devices delete` sit under `destructive-device-actions:execute`, split out of
`device-actions:execute` — so those three can 403 on a credential that executes every other
device action.

### What the CLI tells you

**A 403 now names the permission in the vocabulary of the API that answered.** On a gateway
profile:

```
permission denied (HTTP 403): {"httpStatus":403,"errors":[…]}
hint: grant the Jamf Platform API integration these permissions in Jamf Account — Global
settings > Self Service configuration: Read (self-service:read); Organizational context >
Categories: Read (categories:read). Names are as the permission picker shows them:
https://developer.jamf.com/platform-api/reference/jamf-pro-permissions-map
```

The section and permission name are what Jamf Account's picker prints — it is searched by
name, and the names differ substantially from the slugs
(`computer-inventory-collection-settings` is "Device inventory collection settings"). The
slug is printed beside each one because that is what the gateway's own errors and the
command catalog use.

The same command against a Jamf Pro instance profile still reports Jamf Pro privilege names
(`Required privilege(s): Read Categories, Read Self Service`), because that is what you would
grant there. Neither vocabulary is printed for the other credential.

**Platform command 403s now exit 5, not 1.** `permission denied` has a documented exit code;
platform commands previously returned the SDK's error untouched and exited 1. Scripts
keying on exit codes should expect 5 from a platform permission failure.

### Sizing an integration without provoking 403s

`jamf-cli commands -o json` carries the requirement per command, in both forms:

| Field | What it holds |
|---|---|
| `privileges` | Jamf Pro API-role privilege names (`Read Categories`) for a Pro or Classic command; for a Platform command this is already the capability list |
| `gatewayPrivileges` | the capability permissions the **gateway** requires for that Pro or Classic endpoint (`categories:read`) |
| `gatewayPermissions` | the same requirement in Jamf Account's own words — `Organizational context > Categories: Read (categories:read)` |
| `api` | which API serves it: `pro`, `pro-classic`, `platform-gateway` or `radar` |

`gatewayPermissions` is the field to work from when creating the integration, because the
Platform API integrations UI is the only place an integration can be created and its picker
lists named permissions with a checkbox per action — the capability slug appears nowhere in
it, and the two differ enough that guessing fails
(`computer-inventory-collection-settings` is "Device inventory collection settings").

So the permissions for a set of commands are a query, not a support ticket:

```bash
jamf-cli commands -o json | jq -r '
  .[] | select(.command | test("^pro blueprints ")) | .gatewayPermissions[]?
' | sort -u
```
```
Deployment > Blueprints: Delete (blueprints:delete)
Deployment > Blueprints: Deploy (blueprints:deploy)
Deployment > Blueprints: Read (blueprints:read)
Deployment > Blueprints: Update (blueprints:update)
```

Each row is per command, so widening the filter can list one permission twice with
different actions ticked — `Categories: Create` from `create`, `Categories: Create, Read,
Update` from `apply`. Tick the union.

Three things the fields deliberately do not claim:

- **Absent is not "needs none".** A command outside the published API declares no capability,
  as do the 44 unauthenticated Jamf Pro endpoints (`pro health-checks`,
  `pro jamf-pro-versions`) and the hand-written commands, which send no single endpoint.
- **A `--name`, `--serial` or `--udid` lookup resolves the identifier through the resource's
  collection first**, so those invocations also need its read permission. Only the
  permissions a command *always* uses are listed, so that `delete <id>` does not ask you to
  grant a read it never makes. `apply` is the exception that really does always read, and its
  row says so.
- **One permission, one row.** `Categories: Create, Read, Update` is one row of the picker
  with three boxes ticked, not three permissions.

## Commands refused on a gateway profile

Some Jamf Pro and Classic endpoints are not part of the gateway's published API. Those
commands are **refused before a request is sent** on a gateway profile, with exit code 2:

| Command group | Subcommands | Why |
|---|---|---|
| `pro app-installer-deployments` | 13 | wire-confirmed unrouted (EU and US, 2026-08-28, re-confirmed 2026-08-31) |
| `pro app-installer-titles` | 2 | as above |
| `pro app-installer-global-settings` | 2 | as above |
| `pro api-integrations` | 6 | outside the published API — withdrawn to close a privilege-escalation path |
| `pro api-roles` | 5 | as above |
| `pro api-roles-privileges` | 2 | as above |
| `pro authentications` | 6 | outside the published API |
| `pro oauth-token-sessions` | 1 | outside the published API |
| `pro environment-type` | 1 | outside the published API |
| `pro database-connections` | 1 | outside the published API |
| `pro systems` | 2 | `initialize` / `platform-initialize`, withdrawn upstream |
| `pro mac-os-managed-software-updates` | 1 | `list` (the deprecated available-updates endpoint) |
| `pro mdm-commands commands` | 1 | the gateway declares GET on that path, not POST |
| `pro classic-computer-configs` | 7 | outside the published Classic API 11.28.0 |

46 operations in total. **Nothing else changes for the ~1,700 other commands** — Pro and
Classic still route through the gateway as before.

The refusal explains itself, and the wording differs by evidence. For the app-installer
family the gateway demonstrably does not route it. For the rest, the endpoint **may still
answer today** — that is transitional, and the refusal says so:

```
$ jamf-cli -p platform-prod pro api-roles list
jamf-cli pro api-roles list is not part of the Jamf Platform gateway's published API

Not declared by the gateway's Jamf Pro API 11.31.0.

The gateway still routes some endpoints its published API omits, so this one may answer
today — that is transitional, and a workflow built on it will break without notice. It is
refused here rather than later, when the gateway's 403 BAD_PERMISSIONS would be
indistinguishable from a missing API-role privilege.

Run it against a Jamf Pro instance directly — a profile whose url is your instance and whose
auth-method is oauth2 or token.

Published surface: Jamf Pro API 11.31.0, Classic API 11.28.0.
hint: auth-method platform against the gateway, from profile "platform-prod"
```

Refusing something that works is deliberate: every day a workflow keeps running against a
route that is going away, the eventual failure gets more expensive, and it arrives as a bare
`403 BAD_PERMISSIONS` with nothing saying a withdrawal caused it.

**The remedy is a second profile.** These endpoints exist on your Jamf Pro instance; keep an
`oauth2` profile alongside the platform one and select it with `-p` for the affected
commands. App installers in particular are instance-only and will stay that way.

The reverse direction is refused too: a Platform-only command (`pro blueprints`,
`platform ai-policies`, `security ztna-apps`, …) on an instance profile names the profile,
its resolved auth method, and `jamf-cli platform setup`, rather than reading as a credential
problem.

`jamf-cli commands -o json` reports the verdict per command as `gateway`, `gatewayBasis` and
`gatewayDetail`, so a script can check the whole surface without running anything.

## New at GA

Additive — no action needed.

- **Jamf AI Governance:** `platform ai-policies`, `platform ai-tools`. Environment or
  organization scope.
- **Jamf Account:** `platform account-licenses`, `deal-registrations`,
  `distributor-configuration`, `distributor-purchase-orders`, `distributor-quotes`,
  `sso-connections`, `sso-domains`. Organization scope, and **US-only** — a non-US profile
  is refused before sending, permanently, because these APIs are served from one region.
- **Platform audit:** `platform audit`. Environment scope only; an organization-scoped
  profile gets an error explaining that. Not to be confused with `pro audit`, which runs
  health checks on a Jamf Pro instance.
- **Jamf Security Cloud through the gateway:** `security dns-*`, `ztna-*`,
  `content-categories`, `device-groups`, `uem-*`. Every `security` command says which API
  serves it — `(Security Cloud · platform gateway)` or `(Security Cloud · Radar API)` — in
  its `Short`, so `security <TAB>` shows it.
- `-n, --dry-run` is honoured on platform and Security Cloud writes, printing the method,
  resolved path and body to stderr.
- `-v` labels retried requests with the attempt number and the wait, so a slow call is
  distinguishable from a retry sequence.

## Troubleshooting

| Symptom | Cause and resolution |
|---|---|
| `… is the retired Jamf Platform gateway` (exit 2) | Set `url:` to `https://{region}.api.jamfcloud.com`. |
| `404 page not found`, no JSON body | A path reached the gateway that it does not route — usually a `url` carrying a path segment. Supply the host only. |
| `403 OWNERSHIP_FORBIDDEN` | `environment-id` supplied for a tenant-scoped integration, or the reverse. |
| `400 REQUEST_CONTEXT_NOT_PROVIDED` | No scope sent, on a credential that expects one. Add `environment-id` or `tenant-id`. |
| `400 INVALID_REQUEST_CONTEXT_TYPE` | The level sent is not the level that endpoint accepts; the message names both. `platform audit` accepts environment only. |
| `permission denied (HTTP 403)` with a permission hint | The integration lacks that permission. Grant it in Jamf Account by the section and name the hint prints. |
| `403 BAD_PERMISSIONS` with no permission named | The endpoint has no recorded capability, or the namespace is not entitled for this tenant. Check the integration's permissions and the tenant's entitlements. |
| `… is not part of the Jamf Platform gateway's published API` (exit 2) | Expected. Run it against a Jamf Pro instance profile — see [Commands refused on a gateway profile](#commands-refused-on-a-gateway-profile). |
| `… is served by the Jamf Platform API, which the active credentials do not reach` (exit 2) | A platform command on an instance profile. Use `-p <platform profile>`. |
| `--environment-id and --tenant-id are mutually exclusive` | Both levels supplied. Unset whichever the credential was not created for, including in the environment. |
| Authentication fails outright after GA | Beta credentials. Register a replacement integration in Jamf Account and re-run `jamf-cli platform setup`. |
