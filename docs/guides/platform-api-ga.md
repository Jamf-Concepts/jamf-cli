# The Platform API at GA

The Jamf Platform API reached general availability on **2026-09-03**, and the public beta is
over. This page is what a jamf-cli user has to change coming from the beta, and what the CLI
now does for you.

> **The API is GA; the numbers on this page are a snapshot.** Version numbers, the
> refused-command list and the permission names quoted here track a specific SDK ingest —
> currently `jamfplatform-go-sdk` `v0.22.0`, whose published surface is Jamf Pro API 11.31.0
> and Classic API 11.28.0. The published surface moves in both directions at GA: the whole
> Classic patch-management family was withdrawn in one build and restored in a later one, and
> `/v3/computers-inventory` came back the same way. So read the refused-command list against
> the build you are actually running: `jamf-cli commands -o json` always reports the current
> answer.

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

Three changes per platform profile, all of them now due — GA has happened, so nothing here
is waiting on a date. A profile still carrying beta values does not work.

1. **Point `url` at the GA gateway.** `https://{region}.api.jamfcloud.com`, replacing
   `https://{region}.apigw.jamf.com`. See [Base URL](#base-url).
2. **Register a replacement API integration in Jamf Account and update the profile's
   credentials.** Beta credentials were revoked at GA and a beta client cannot be migrated.
   If you still have a beta integration's configuration to hand, record what it could reach
   before replacing it, so you can pick the equivalent permissions. See [Credentials and
   permissions](#credentials-and-permissions).
3. **Replace `tenant-id` with `environment-id`**, unless single-product access is
   deliberately what you want. See [Scope](#scope).

There is no state to migrate and no config-file schema change beyond those keys. Re-running
`jamf-cli platform setup` does all three interactively and writes a fresh profile — which is
the shortest path if you are starting from a beta profile.

If you are setting up a platform profile for the first time, none of the above is a
migration: run `jamf-cli platform setup` and read [Scope](#scope) to pick the level.

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

The GA host is the only host now — `apigw.jamf.com` is retired, not deprecated — so this
change and the credential swap have to land together.

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

### Which level a command's API declares

Each Platform API spec declares the levels its endpoints accept, and the CLI stamps that on
every generated platform command. `jamf-cli commands -o json` reports it as `scopes`:

```bash
jamf-cli commands -o json | jq -r '
  .[] | select(.scopes) | "\(.command)\t\(.scopes | join(","))"
'
```

As of this ingest:

- **Environment only** — blueprints, compliance benchmarks (with `baselines`, `rules` and
  `benchmark-reports`), DDM reports, `platform-devices`, `platform-device-groups`, the
  platform device actions, AI Governance and audit.
- **Tenant or environment** — the Jamf Pro API, the Classic API, and every Jamf Security
  Cloud family (`dns-*`, `ztna-*`, `content-categories`, `device-groups`, `uem-*`,
  `enrollment-activation-profiles`).
- **Nothing declared** — the three Jamf Account APIs, which resolve the organization from
  the credential itself.

**Nothing is refused on this.** The specs are currently stricter than the gateway: a
tenant-scoped credential still reaches `pro platform-devices list` and
`pro platform-device-groups list` today, both declared environment-only (probed 2026-09-05).
So the declaration explains a failure the gateway has already returned rather than
pre-empting one — it is appended to a `400 REQUEST_CONTEXT_NOT_PROVIDED` and to a
`403 OWNERSHIP_FORBIDDEN`, where the gateway's own message names a capability permission and
says nothing about the level — and it is what `platform setup`'s closing summary is
assembled from. A refusal keyed on it would refuse working commands.

### A profile's scope level is not used with other credentials

An API integration is created at one level in Jamf Account and its credential carries that
choice, so a profile's `environment-id` or `tenant-id` describes the profile's own
integration. If the client ID comes from `--client-id` or `JAMF_CLIENT_ID`, both of the
profile's IDs are ignored — an organization-scoped credential must send no scope header at
all, and a level belonging to another integration is redundant at best and unusable at
worst.

So this reaches Jamf Account correctly even with a tenant-scoped default profile configured:

```bash
export JAMF_URL=https://us.api.jamfcloud.com
export JAMF_CLIENT_ID=...        # an organization-scoped integration
export JAMF_CLIENT_SECRET=...
jamf-cli platform account-licenses list
```

and this is how you name a level for those credentials:

```bash
export JAMF_ENVIRONMENT_ID=...   # or JAMF_TENANT_ID, or --environment-id / --tenant-id
```

If a command needs a level and none was supplied, the error names the profile whose level
was passed over and both ways to set one. A profile holding `client-id` with only
`JAMF_CLIENT_SECRET` injected keeps its level: the client ID is what names the integration,
so that is still the profile's own.

## Credentials and permissions

Credentials are registered in Jamf Account's Platform API integrations UI. One OAuth 2.0
model now covers the platform, replacing one credential set per product. `jamf-cli platform
setup` prompts for region, client ID and secret, and the scope, validates them against the
gateway, and stores the secrets in the keychain. It also reports whether the credential
reaches Jamf Security Cloud rather than passing or failing on it — a Jamf Pro tenant
legitimately cannot.

It closes by saying what the profile actually reaches, assembled from the scope levels the
specs declare rather than from which prompt you filled in: a tenant-scoped profile is told
which Platform resources are out of reach at that level, and an organization-scoped one is
told it serves the Jamf Account commands and nothing else. Both sentences were hand-written
before and both were wrong — a tenant profile was told it served "the Platform API commands"
when six Platform specs declare environment only, and an organization profile was told AI
Governance was served, which answers `400 REQUEST_CONTEXT_NOT_PROVIDED` with no scope
header.

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
| `scopes` | for a Platform command, the scope levels its API declares — `environment`, or `environment,tenant`. Not a permission; see [Which level a command's API declares](#which-level-a-commands-api-declares) |

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
commands are **refused before a request is sent** on a gateway profile, with **exit code 8**:

| Command group | Refused | Why |
|---|---|---|
| `pro mobile-devices` | 16 of 24 | the MDM device actions — the gateway declares GET on `/v2/mdm/commands`, not POST |
| `pro computers-inventory` | 8 of 27 | the MDM device actions, as above |
| `pro api-integrations` | 7 | outside the published API — withdrawn to close a privilege-escalation path |
| `pro classic-computer-configs` | 7 | outside the published Classic API 11.28.0 |
| `pro api-roles` | 6 | as `pro api-integrations` |
| `pro authentications` | 6 | outside the published API |
| `pro static-computer-groups` | 6 | the deprecated v2 endpoint — use `pro computer-groups-static-groups` |
| `pro api-roles-privileges` | 2 | as `pro api-integrations` |
| `pro systems` | 2 | `initialize` / `platform-initialize`, withdrawn upstream |
| `pro policy-properties` | 2 | `GET`/`PUT /settings/obj/policyProperties`, withdrawn from the published API |
| `pro oauth-token-sessions` | 1 | outside the published API |
| `pro environment-type` | 1 | outside the published API |
| `pro database-connections` | 1 | outside the published API |
| `pro mac-os-managed-software-updates` | 1 | `list` (the deprecated available-updates endpoint) |
| `pro mdm-commands commands` | 1 | the gateway declares GET on that path, not POST |

67 commands in total. `pro classic-computer-configs` accounts for 7 of them with 6
subcommands, because the whole resource is refused and its group node is refused in its own
right. **Nothing else changes for the ~1,700 other commands** — Pro and Classic still route
through the gateway as before.

The top two rows are the shape to expect from here on: a withdrawal can take **part of a
command group**. `pro mobile-devices list`, `get` and the rest are served while 16 of its
subcommands are refused, because those 16 send `POST /v2/mdm/commands` and the gateway
publishes only GET on that path. Check `--help` on the individual subcommand rather than the
group; a refused one says so in its first paragraph.

Classic is judged at three granularities for the same reason — the resource, then each
method across it, then the collection GET exactly — and **the binary has no live example of
the two narrower ones today**. The Classic patch-management family supplied it until this
ingest: `pro classic-patch-titles create` was served while `list`, `get`, `update`, `delete`
and `apply` were refused, because the gateway published `POST /patchsoftwaretitles/id/{id}`
and nothing else on that resource. That family is restored in full, so all three
granularities now agree on it. The mechanism stays, and the tests that exercise it are
synthetic rather than fixtures of a shipped command, because the last Classic withdrawal
landed *inside* surviving subtrees rather than on whole resources — which is what the next
one is expected to do too.

To see the current list for the binary you have, without a profile:

```
jamf-cli commands -o json | jq -r '.[] | select(.gateway=="unserved") | .command'
```

`pro static-computer-groups` is the one entry with a working replacement already in the
CLI, and it is worth understanding because more will follow it. The gateway's published
11.31.0 surface withdrew 122 superseded Jamf Pro endpoints — every one of them a version
with a published higher-version successor. In almost every case the CLI simply moved onto
the successor and you will notice nothing: `pro computers-inventory` now sends `/v4`
instead of `/v3`, and gained `erase` and `remove-mdm-profile` subcommands in the process.
(13 of the 122 have since been restored — the `/v3/computers-inventory` family, on the
grounds that it was deprecated too close to the removal for callers to reach v4. The CLI
sends v4 either way.)
Static computer groups are the exception, because the CLI ships the two versions under two
different command names:

```
pro static-computer-groups            # v2 — refused on a gateway profile
pro computer-groups-static-groups     # v3 — use this
```

Both still work against a Jamf Pro instance profile, and **the refusal names the
replacement**: where a working successor ships in the same binary, it is offered first, ahead
of the instance-profile remedy. `pro static-computer-groups --help` says the same thing on
the group itself, so you do not have to run a subcommand to find out.

The refusal explains itself. Every endpoint on the list today **may still answer** — that is
transitional, and the refusal says so rather than claiming the endpoint is gone:

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
$ echo $?
8
```

`pro api-roles` has no successor in the CLI, so the only remedy offered is the instance
profile. A command that does have one names it first:

```
$ jamf-cli -p platform-prod pro static-computer-groups list
jamf-cli pro static-computer-groups list is not part of the Jamf Platform gateway's published API

Not declared by the gateway's Jamf Pro API 11.31.0.

...

Use `jamf-cli pro computer-groups-static-groups` instead — the same resource on the v3
endpoint the gateway publishes, where this command is the withdrawn v2. It ships in this
binary and is served by the gateway.

Failing that, run it against a Jamf Pro instance directly — a profile whose url is your
instance and whose auth-method is oauth2 or token.
```

**Exit code 8, not 2.** A refusal is a policy answer about the credentials in hand, not a
malformed invocation, and exit 2 is also what a bad flag, a missing required flag, a flag
group violation, the wrong number of arguments, an unknown subcommand, a missing URL, a
missing credential, the retired gateway host and a scope conflict all return. A wrapper
that wants to skip refused commands and fail on everything else keys on 8:

```bash
jamf-cli -p platform-prod pro api-roles list
case $? in
  0) ;;
  8) echo "refused on this credential — skipping" >&2 ;;
  *) exit 1 ;;
esac
```

Refusing something that works is deliberate: every day a workflow keeps running against a
route that is going away, the eventual failure gets more expensive, and it arrives as a bare
`403 BAD_PERMISSIONS` with nothing saying a withdrawal caused it.

**The remedy is a second profile.** These endpoints exist on your Jamf Pro instance; keep an
`oauth2` profile alongside the platform one and select it with `-p` for the affected
commands.

The list moves in both directions. **App Installers used to head it — 17 commands,
instance-only — and no longer appears at all.** The endpoints were absent from every
published spec because they sit under `hiddenapi/` in Jamf Pro's source, so nothing routed
them and the CLI refused them on a recorded wire probe. Upstream published all 23 on
2026-09-03 and the gateway opened them the same day, so `pro app-installer-titles`,
`app-installer-deployments`, `app-installer-global-settings` and the new `pro app-installers`
now work on a gateway profile like any other Pro command. `pro policy-properties` moved the
other way in the same spec drop, and still answers on the wire — which is the case the
transitional wording exists for.

The Classic patch-management family moved the same way and faster. One build withdrew
`/patches`, `/patchreports`, `/patchsoftwaretitles` and two `/patchpolicies` reads from the
published Classic API, which refused eight commands here; the next SDK ingest restored every
one of them, on the stated reasoning that patch management is where Classic API callers are
most concentrated. `pro classic-patch-reports`, `pro classic-patch-titles` and
`pro classic-patch-policies list` work on a gateway profile again, and
`pro classic-patch-titles` also picked up `--scaffold` and `--set` in the process, its body
schema having come back with the endpoints.

The reverse direction is refused too: a Platform-only command (`pro blueprints`,
`platform ai-policies`, `security ztna-apps`, …) on an instance profile names the profile,
its resolved auth method, and `jamf-cli platform setup`, rather than reading as a credential
problem.

`jamf-cli commands -o json` reports the verdict per command as `gateway`, `gatewayBasis` and
`gatewayDetail`, so a script can check the whole surface without running anything.

### The stopgap: `JAMF_CLI_ALLOW_UNPUBLISHED`

Some of these endpoints demonstrably answer today. `GET /pro/settings/obj/policyProperties`
returns real data, and the build that withdrew it from the published spec did not stop
routing it — so an upgrade can refuse a command that was working an hour earlier, with no
route back until a release moves it.

Setting `JAMF_CLI_ALLOW_UNPUBLISHED=1` downgrades that refusal to a warning and sends the
request:

```
$ JAMF_CLI_ALLOW_UNPUBLISHED=1 jamf-cli -p platform-prod pro policy-properties policy-properties
warning: jamf-cli pro policy-properties policy-properties is not part of the Jamf Platform
gateway's published API, and JAMF_CLI_ALLOW_UNPUBLISHED is set — sending it anyway.
Not declared by the gateway's Jamf Pro API 11.31.0.
The gateway routes it today; that is transitional and it will stop answering without notice,
at which point the failure arrives as a bare 403 BAD_PERMISSIONS. This is a stopgap, not a
supported mode — move the workflow onto a Jamf Pro instance profile.
```

Read the terms before using it:

- **It is a stopgap, not a supported mode.** Nothing has committed to keeping these routes.
  When one is withdrawn, the command starts failing with a bare `403 BAD_PERMISSIONS` and the
  variable will not help.
- **The warning cannot be silenced.** It goes to stderr on every affected invocation, and
  neither `--quiet` nor `--no-hints` suppresses it. Being told is what the variable trades
  the refusal for.
- **It covers only endpoints that are merely unpublished.** An endpoint a wire probe found
  unrouted is refused regardless, because letting it through buys a 403 and nothing else.
- **It is value-parsed**, like `JAMF_CLI_NO_HINTS`: `JAMF_CLI_ALLOW_UNPUBLISHED=0` leaves the
  refusal in place, so a runner that exports it unconditionally can turn it off without
  unsetting it.
- **It changes nothing about the endpoint.** Prefer a second `oauth2` profile for anything
  scheduled; use the variable for a one-off job you are already planning to migrate.

## What GA added

Additive — no action needed, and all of it is live now.

- **Jamf AI Governance:** `platform ai-policies`, `platform ai-tools`. Environment or
  organization scope.
- **Jamf Account:** `platform account-licenses`, `deal-registrations`,
  `distributor-configuration`, `distributor-purchase-orders`, `distributor-quotes`,
  `sso-connections`, `sso-domains`. Organization scope, and **US-only** — a non-US profile
  is refused before sending, permanently, because these APIs are served from one region.
- **Platform audit:** `platform audit`. Environment scope only; an organization-scoped
  profile gets an error explaining that. Not to be confused with `pro audit`, which runs
  health checks on a Jamf Pro instance.
- **App Installers on the gateway:** `pro app-installer-titles`,
  `app-installer-deployments`, `app-installer-global-settings` and a new `pro app-installers`
  (whether the feature is available and which features the Cloud Services Connection
  enables). Previously instance-only and refused on a gateway profile; the endpoints are now
  published, and the commands are generated from that published spec rather than from a
  reverse-engineered one, which also added `titles versions`,
  `global-settings deployment-controls`, and `global-settings history` /
  `add-history-note`.
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
| `… is not part of the Jamf Platform gateway's published API` (exit 8) | Expected. Use the successor the message names, if it names one, or run it against a Jamf Pro instance profile — see [Commands refused on a gateway profile](#commands-refused-on-a-gateway-profile). `JAMF_CLI_ALLOW_UNPUBLISHED=1` is the stopgap. |
| `… is served by the Jamf Platform API, which the active credentials do not reach` (exit 8) | A platform command on an instance profile. Use `-p <platform profile>`. |
| `--environment-id and --tenant-id are mutually exclusive` | Both levels supplied. Unset whichever the credential was not created for, including in the environment. |
| Authentication fails outright with credentials that used to work | Beta credentials, revoked at GA. Register a replacement integration in Jamf Account and re-run `jamf-cli platform setup`. |

## Other CLI changes in this release

The gateway migration is not the only user-visible change shipping here. Everything in this
section except [A CDN refusal is named as one](#a-cdn-refusal-is-named-as-one) applies to a
Jamf Pro instance profile as much as to a gateway one.

### `pro ddm-reports declaration get` and `device get` are gone

Both endpoints were deprecated upstream in favour of a sibling on the same resource, and the
CLI already shipped both siblings, so the capability survives under a different subcommand
name:

| Removed | Replacement |
|---|---|
| `pro ddm-reports declaration get <declaration-id>` | `pro ddm-reports declaration devices <declaration-id> --filter …` |
| `pro ddm-reports device get <device-id>` | `pro ddm-reports device declarations <device-id> --filter …` |

The two successors declare `filter` **required** where the deprecated pair did not, so there
is no unfiltered read left on either resource. The CLI enforces the flag before sending, so
omitting it is a usage error rather than a 400. Where you want everything,
`active=in=(true,false)` is the tautology that stands in for an absent filter:

```bash
jamf-cli pro ddm-reports device declarations <device-id> --filter 'active=in=(true,false)'
```

Both successors also gained `--page`. They page on `page` + `size` and only `size` was being
sent, so until now just the first page was reachable.

Hand-written commands that read DDM state — `pro ddm-reports errors`, and the DDM sections
of `pro device`, `pro report` and `pro audit` — already used the filtered endpoints and are
unaffected.

### `pro computers-inventory` sends v4

Every `pro computers-inventory` (`pro comp`, `pro computers`) subcommand now sends `/v4`
instead of `/v3`, and `get` reads the v4 detail endpoint. Nothing renamed and no flag moved.
Generated subcommands retry the `/v1` path on a 404 and print a one-line warning to stderr,
so an instance that does not serve v4 still answers.

Two commands are the exception, because they are hand-written and assemble their own paths:
`pro comp erase` and `pro comp remove-mdm` were pinned to `/v1/computer-inventory/{id}/…`
and are now pinned to `/v4/computers-inventory/{id}/…`, with no fallback. Against an
instance that does not serve v4 they answer 404 rather than retrying v1.

The v4 endpoints also arrive as generated `erase` and `remove-mdm-profile` subcommands, and
those are suppressed in favour of the hand-written pair — a duplicate subcommand name is not
something cobra can resolve. So the names, targeting and safety behaviour are unchanged:
`pro comp erase` and `pro comp remove-mdm` still take `--serial`, `--name`, `--group` or
`--from-file`, confirm a destructive action, honour `-n, --dry-run`, and accept the Find My
PIN body through `--body-file`.

### Classic writes gained `--scaffold`, `--set` and field help

`create`, `update` and `apply` on 44 of the 54 Classic resources — 114 of 117 commands —
now take `--scaffold` (print an XML body template and exit) and `--set key=value` (build
the body from flags), and their `--help` lists required fields and enum values. The body
shapes come from a committed schema artifact derived from the same Classic spec the gateway
coverage manifest reads. The three commands without them are `pro classic-computer-configs`
`create`, `update` and `apply`: the resource is dead, and a Jamf Pro instance 404s it too.

`--set` **builds the whole body** and is therefore mutually exclusive with `--from-file`,
unlike the Platform and Security Cloud `--set`, which overlays onto a `--file` body. That is
workable because a Classic `PUT` is a partial update: a body carrying only `<name>` renames
the object and leaves everything else intact, so `--set name=…` alone is a valid update with
no fetch-merge cycle.

`--set` also refuses three things the server accepts, which is the only validation the CLI
does that Jamf Pro does not:

- **An unknown field**, because the Classic API answers `201` and silently drops it.
- **An out-of-enum value**, because the Classic API answers `201` and reads back its default
  (`frequency: "Twice per fortnight"` becomes `Once per computer`).
- **A credential field** — a distribution point, SMTP server, LDAP server, directory
  binding, VPP account and disk-encryption configuration all carry one. A password in a flag
  value lands in shell history, `ps` output and CI logs. Put the whole body in a file and use
  `--from-file`.

A scaffold is a template to edit, not to pipe unread. A policy scaffold in particular cannot
be sent as-is: `general.category.id`'s spec example is `0`, which answers
`409 No match found for category 0`, and the `scope` and `account_maintenance` specimens
reference objects that do not exist on your instance. Delete the sections you do not need.

### `--file` accepts YAML on Platform and Security Cloud commands

Every generated Platform and Security Cloud `--file` now sniffs the content and accepts YAML
as well as JSON, matching what Pro's `--from-file` already did. `--scaffold` still prints
JSON; a YAML file is converted before the request is built, so `--set` overlays behave
identically either way.

A YAML body carrying a timestamp scalar or a non-string mapping key — both legal YAML, and
neither expressible in JSON — used to be reported as malformed input. It is now converted
rather than refused.

### An empty list prints `[]`, not `null`

`list -o json` on an empty collection previously answered `null` for any command that
aggregates pages: Pro's `list --all`, and every Platform and Security Cloud list. Anything
piping to `jq` failed with "Cannot iterate over null" on exactly the tenants where the
collection was empty. All three now emit `[]`.

### `security device-groups update` sends v2 and prints nothing

The endpoint moved. `PUT /securitycloud/v1/groups/{groupId}` was deprecated in favour of
`PUT /securitycloud/v2/groups/{groupId}`, and the published spec has now withdrawn the v1
form outright along with the v1 list. `list` and `update` send v2; `create`, `get` and
`delete` stay on v1, which is where the spec keeps them.

The v2 handler answers `204` with no body where v1 answered `200` with the updated group, so
**a successful `update` now prints nothing** and exits 0. Read the result back with
`security device-groups get <id>` if you need it.

The CLI withheld this operation until now, because the v2 handler did not work: it answered
`403 BAD_PERMISSIONS` until 2026-09-03, then a bare `404` on a group its own `list` had just
returned. It was fixed on 2026-09-04, and a rename through the CLI now returns 204 and reads
back, so the command is generated from the spec with no override.

### `config list` shows the scope level

The table, CSV and plain output of `jamf-cli config list` now always carries an
`environment-id` column and a `default` column, and no longer carries `tenant-id`.
`-o json` and `-o yaml` are unchanged and still include `tenant-id`.

Two reasons. A table's columns are the keys of its *first* row, so a field that is only
sometimes present was only sometimes a column — profiles are listed alphabetically, so one
instance profile sorting first hid the scope of every platform profile below it, and hid
which profile was the default. And one scope column rather than two, because environment is
the level Jamf wants integrations created at. A tenant-scoped profile is not left
unexplained: `auth-method` still reads `platform`, and `-o json` still carries the ID.

If you parse `config list` in a script, read `-o json` rather than the table.

### A CDN refusal is named as one

The GA gateway sits behind a CDN whose WAF refuses some requests before Jamf sees them.
Left alone that arrives as `permission denied (HTTP 403)` with an HTML error page in the
message and a hint about API roles — wrong twice over, since the credential is fine and no
role change helps. The CLI now recognises the response and says so:

```
Error: request blocked at the Jamf gateway edge (HTTP 403), before it reached Jamf
hint: This is the gateway's CDN/WAF, not Jamf and not your API privileges, so no role change
will help. Known triggers: "file://" anywhere in the request body (a legitimate value in
some Classic payloads), .pkg upload content, and a burst of writes. The response cannot say
which one fired. There is no client-side fix — retry a single request cold, and report it to
Jamf.
```

It exits 5, like any other permission failure. The hint names every known trigger and does
not claim which one fired, because the response carries no `traceId` and nothing identifying
the rule — the same page comes back for a content match and for a volume block. There is
deliberately no client-side workaround: rewriting a body to dodge a WAF rule would be
silent, lossy where the content is meaningful, and would go on happening after the rule was
fixed.

The practical consequence today is that a `.pkg` upload through a gateway profile is refused
(the match is inside the package's table of contents). Upload through a Jamf Pro instance
profile.

### Building from source needs Go 1.27

`go.mod` declares `go 1.27.0`, up from `1.26.6`. With the default `GOTOOLCHAIN=auto` an
older local toolchain downloads it, so `go install` keeps working; a pinned
`GOTOOLCHAIN=go1.26.x` does not. Binary releases and the Homebrew formula are unaffected.
