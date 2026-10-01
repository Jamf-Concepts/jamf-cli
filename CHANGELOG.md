# Changelog

Per-release notes — the full list of merged pull requests — are generated on
[GitHub Releases](https://github.com/Jamf-Concepts/jamf-cli/releases). This file exists for
what that list cannot say: which changes are **breaking**, what the migration is, and why.
So it records breaking changes, behaviour changes visible to a script, and removals. A
release with none of those gets no entry here.

Versions follow the `vMAJOR.MINOR.PATCH` tags in this repository, and headings match the
commit types the repo already uses (`feat!`/`build!` for a breaking change).

## Unreleased

### Breaking — device secrets are read from a file, not a flag value

Three flags took a device secret as their value, where `ps`, shell history and
a CI log could read it. Each is removed and replaced by a file flag on the same
command, with no alias. The old name now fails with `unknown flag` and a hint
naming the new one.

| command | removed | use instead |
|---|---|---|
| `pro computer-inventory set-recovery-lock` | `--new-password <pw>` | `--new-password-file <path>`, or the no-echo prompt |
| `pro computer-inventory set-recovery-lock` | omitting `--new-password` to clear | `--clear` |
| `pro mobile-devices lock` | `--pin <pin>` | `--pin-file <path>` |
| `pro mobile-devices clear-passcode` | `--unlock-token <token>` | `--unlock-token-file <path>`, or `-` for stdin |

Clearing a Recovery Lock is now something a caller asks for. With neither
`--new-password-file` nor `--clear`, `set-recovery-lock` prompts on a terminal,
and under `--no-input` it refuses. It used to clear the password, so a script
that relied on "no flag means clear" has to pass `--clear`. An empty file or an
empty prompt is refused too, rather than read as a clear. A trailing line
ending in any of the files is dropped. An error reading one of the files names
the flag and not the path, so a secret typed where its path belongs is not
echoed.

### Behaviour — `-vvv`, `--dry-run` and error messages redact more credentials

The `-vvv` body log redacted passwords, secrets and the OAuth `access_token`,
and missed every other credential a body carries: `token`, `pin`,
`unlockToken`, `accessToken`, `serverToken`, `bootstrapToken`, `encodedToken`,
`identityKeystore`, `gsxKeystore.keystoreBytes`, an XML `<token>`, a form
`token=`, a FileVault institutional recovery key's `<key>` and `<data>`, and
any secret in a configuration profile's plist. Now:

- A field is redacted when its name, split on camelCase and `_ - .`, ends in
  `token`, `pin`, `passcode`, `keystore`, `keystore bytes`, `keystore file`,
  `authorization`, `authorization header`, `challenge`, `signature`,
  `credential` or `credentials`, or when it holds one of the old credential
  words anywhere. A field that only starts with one of these words is left
  alone, so `tokenUrl`, `tokenEndpointAuthMethod`, `token_type`, `pinned`,
  `keystoreFileName` and `authorizationEndpoint` still read in full.
- Every value inside an `institutional_recovery_key` element or object is
  redacted. The inventory status form,
  `<institutional_recovery_key>Not Present</institutional_recovery_key>`, is
  not.
- A plist `<key>Password</key><string>…</string>` pair is redacted by its key,
  raw, entity-escaped inside a Classic `<payloads>` element, or inside a JSON
  string.
- A string array under a credential name is redacted. A number is redacted only
  under a `pin` or `passcode` name, so `passwordMinLength` stays readable. A
  boolean never is.

`--dry-run` printed request bodies with no redaction at all, on Jamf Pro and
Classic writes, on Platform gateway writes (including the gateway-served
Security Cloud commands) and on Security Cloud Radar writes. All three previews
now go through the same redactor as `-vvv`. So do the response bodies quoted
into an HTTP error message and the JSON error envelope, and the query string of
the `-v` request line. A JCDS download that fails before a response no longer
prints the pre-signed URL's query, which is its credential. A script that read a
credential back out of any of these gets `[REDACTED]`.

### Breaking — `--set` refuses credential fields in Pro, Platform and Security Cloud

A `--set` value is on the command line, so it lands in shell history, in `ps`
output and in CI job logs. Classic `--set` already refused a credential field.
The generated Pro `patch` and `update`, Platform and Security Cloud commands
accepted one, and the Pro `update` warning for a write-only field told you to
pass it as `--set <field>=<value>`.

`--set` now refuses a field that carries a secret: a password, client or shared
secret, token, API or access key, private key, keystore, authorization header or
device PIN, and any string the spec marks write-only. The refusal also catches a
JSON value that carries one, such as `--set deviceSyncAuth='{"clientSecret":"…"}'`.
A private-key blob is refused too: ADCS and DigiCert `clientCert.data` and the
cloud LDAP `server.keystore.fileBytes`, while the public `serverCert.data` stays
settable. A key is matched however it is spelled (`pass_word`,
`basic_auth_credentials.password`), and on an operation that carries a secret a
key with an empty segment, a `[` or surrounding whitespace is refused.
Examples are `security uem-connectors create --set deviceSyncAuth.clientSecret=…`,
`security stream update --set delivery.authorization_header=…` and
`pro computer-prestages update --set recoveryLockPassword=…`.

**Migration.** Put the secret in the request body and pass it with
`--from-file <file>` or on stdin. On Platform and Security Cloud, `--set` still
overrides the other fields of that body. A Pro `update` reads its body from
stdin only, so pipe the whole record, secret included. Pro `update --set` on a
resource with a write-only secret now always blanks that secret, and its
warning says so. Pro `--help` and shell completion no longer list credential
fields; the help names them and says where they go.

### Behaviour — `--url` and a profile's `url` no longer redirect the Security Cloud Radar login

`security` commands that use the Risk, Device Lifecycle or SSE credentials
took the Radar API host from `--url`, then `JAMFSECURITY_URL`, then the
profile's `url`. That field holds the Jamf Pro instance or, after
`platform setup`, the platform gateway, so the login sent the Radar client ID
and secret as Basic auth to that host. The Radar host is now
`api.wandera.com` unless `JAMFSECURITY_URL` is set.

Migration: a script that pointed the Radar client elsewhere with `--url` or a
profile `url` sets `JAMFSECURITY_URL` instead. `--url` still selects the
gateway for the gateway-served commands (`dns-*`, `ztna-*`, and the rest).

### Behaviour — the MCP `list_commands` tool browses and searches the catalog

`list_commands` returned the whole catalog in one result. That result was
larger than the 256 KiB cap on a child's output, so the tool cut it
mid-string: the model got invalid JSON with no Protect, School or Security
Cloud commands.

The tool now takes two optional arguments, `prefix` and `query`, and returns
one JSON object per line:

- With no arguments, it lists the top level. A row with `"subcommands": N`
  has N commands under it.
- `prefix` opens one command path, for example `"pro"` or `"pro computers"`.
  A runnable command is listed with `description`, `destructive` and `flags`.
- `query` returns the commands whose path, description or aliases contain
  every word, for example `"delete policy"`.

Each result is kept under 40 KiB. Claude Code saves a text tool result
longer than 50,000 characters to a file, and gives the model only the file
path. An MCP client that parsed the old array gets NDJSON rows
now. The old result was always cut and invalid, so no client parsed it.

`jamf-cli commands` takes the same selection as `--prefix <path>`,
`--children` and `--search <words>`. With no flags it prints the whole
catalog, as before.

### Behaviour — computer group member counts come from the collection that carries one

`pro group-tools` and `pro audit` read member counts from
`/v3/computer-groups/{smart,static}-groups` instead of `/v1/computer-groups`.
The v1 collection carries no count at all — wire-checked against Jamf Pro 11.32
on 2026-09-18, it answers `description`, `id`, `name` and `smartGroup` and
nothing else — so the `memberCount` these commands type-asserted was **always
absent and every group reported 0 members**. Three consequences a script can
see:

- **`memberCount` can now be the string `"unknown"`.** A group the collection
  listed without a readable count is reported as such rather than as empty: an
  absent count is not evidence that a group has no members. Anything doing
  arithmetic on `memberCount` needs to handle a non-numeric value, and a stderr
  warning names how many rows are affected.

- **`pro group-tools list --empty` and `analyze --unused` return fewer rows.**
  Both used to list every group, because every count read as 0. They now list
  only groups proved empty — an unknown count is excluded from both, and
  `--empty` says on stderr how many groups it left out for that reason.

- **`pro audit` gains an `Empty smart groups` finding.** It could not fire
  before: the count was always absent, so its `ok` was always false,
  `emptyCount` never incremented and the check returned no finding on every
  instance, including ones with dozens of empty smart groups.

The two v3 collections share v1's id space, and both count fields are
wire-confirmed — `membershipCount` for smart groups, `count` for static ones,
the latter checked by loading 12 computers into an empty static group and
reading 12 back. Same request cost as before: one paginated sweep per
collection.

### Behaviour — `--all` chooses its own page size, and `--page 0` means page 0

Fixes [#385](https://github.com/Jamf-Concepts/jamf-cli/issues/385). Three changes
a script can see:

- **`--all` ignores `--page-size` and requests the largest page the endpoint is
  known to honour** — 2000 on the Jamf Pro API's `{totalCount, results}`
  endpoints, 1000 on `/v1/users` and Platform `devices/v1`, 500 on AI
  Governance, 100 where neither the spec nor a wire probe says more. A
  9000-computer inventory pull is 5 requests instead of 95. `--page-size` still
  applies to a single page (`--all=false`), clamped to the same ceiling.

  Ignoring the flag is the safe reading, not the lazy one: the Jamf Pro API
  answers an oversized `page-size` by **silently clamping** it, and the walk
  read a short page as the last page — so passing `--page-size 2500` through
  would have returned 2000 of 2601 records at exit 0. Wire-checked on
  `/v1/departments` 2026-09-18.

  Both substitutions are now reported on stderr naming the page size actually
  used. Suppressed by `--quiet`; **not** suppressed by `--no-hints`, which turns
  off advisory tips, and a flag that did nothing is not a tip.

- **`--page 0` returns the first page alone.** It used to be read as "not set"
  and fell through to fetching every page, leaving page 0 reachable only as
  `--all=false` with no `--page`. Any invocation relying on `--page 0` meaning
  "all pages" now gets one page.

- **`--page` and `--page-size` have help text.** The published spec describes
  neither, so both rendered blank, with nothing saying `--page` is zero-based or
  what the page-size ceiling is.

Every fetch-everything path moved with it, not just the generated `list`
commands: `pro report *`, `pro audit`, `pro backup`, `pro group-tools`,
`pro dashboard`, the `pro overview` JCDS and VPP tiles, and Jamf Protect
deployment tasks. Platform `blueprints` and `device-groups` stay at 100 — no
declared maximum and no probe — as does the generic Platform name resolver.
Reasoning and the wire evidence:
`docs/solutions/logic-errors/all-ignored-page-size-2026-09-18.md`.

### Breaking — `pro` command names come from the spec, not from spec filenames

- **Resource names are derived from the API's own OpenAPI tags and URL paths.** They
  used to come from the filename each resource's paths were split into — upstream's
  jss module names, which appear in no published spec and in no API reference. Four
  things followed from that filename with nothing stating them: the command name, the
  endpoint-version family, whether a `-preview` tag reached a command at all, and
  whether the ingest could delete the file.

  Two things were wrong as a result. The ingest was **not reproducible** — wiping
  `specs/` and re-ingesting the same document gave 132 resources instead of 169, 73
  operations dropped and 18 `-preview` endpoints published as commands, at exit 0. And
  version consolidation keyed on a `-vN` filename suffix, which is how this CLI once
  sent `/v3/computers-inventory` while `/v4` was the served version. Both are now facts
  about the paths rather than about a filename.

  `specs/` holds one normalised document (`JamfProAPI.yaml`) instead of 165 carved-up
  ones.

- **163 resources became 135**: 38 renamed, 24 merged, 13 split. The names now match
  what the Jamf Pro API reference calls these resources. Ten were outright bugs the
  filenames had baked in — eight double plurals (`csas`, `slasas`, `oidcs`,
  `cloud-informations`, `inventory-informations`, `jamf-pro-informations`,
  `device-compliance-informations`, `jamf-remote-assist-session-histories`),
  `mac-os-managed-software-updates`, and two names that described the wrong thing
  (`patch-titles` held a single POST accepting a disclaimer; `dss-proxies` held
  `/v1/dss-declarations/{id}`).

  **`pro users` and `pro patch-policies` keep their names**, and both were briefly on
  the renamed list. A resource whose paths carry two tags has to be named by one of
  them, and the tag was being taken from whichever of its paths sorted last — which
  named the `/v1/users` CRUD after two `recalculate` actions tagged `smart-user-groups`,
  and `/v2/patch-policies` after its own logs sub-path. A merged root is named by the
  tag of its own root collection now, so `pro users list` and `pro patch-policies list`
  are the canonical names they always were. `pro user-smart-groups` and
  `pro patch-policy-logs` are the aliases, both resources having folded into the one
  they act on, and every verb from each survives under its new parent unchanged.

- **Every former resource name still works until 2027-03-09.** 100 of them resolve to
  their replacement and print a warning naming it; 3 refuse with an explanation
  because their endpoints are no longer ingested (`pro servers`,
  `pro remote-administration-configurations`,
  `pro redeploy-jamf-management-frameworks`). The warning is not silenced by `--quiet`
  or `--no-hints`. After that date the build fails until the aliases are deleted, so
  they cannot rot silently — and 60 days ahead of it a weekly scheduled build starts
  failing instead, so the removal PR gets written rather than discovered.

  `pro jcds` is the one former name that is **not** on the expiring list. It is a
  curated short alias now, along with `pro jcds-files` for the half the sub-resource
  split gave a command of its own — 37 characters is not a name anyone types.

#### 48 invocations an alias cannot cover — the operation name moved too

An alias maps one resource name to one replacement. Where a resource **split**, or
where an operation's name is derived from a path segment that now sits under a
different resource, the resource alias resolves and the subcommand then does not
exist. The endpoint is unchanged in every case, so only the command name moved.

**All 48 answer at runtime with the new invocation named**, in place of cobra's
`unknown command`, and both spellings of each reach it — the old resource name through
its alias, and the new one directly. `pro schedulers triggers` and `pro scheduler
triggers` both report that the operation moved and name `pro scheduler-jobs triggers`.
Exit code 2, the same as every other command that does not exist: what a caller is
missing here is not a classification but the pointer, and a second exit code for one
class would only make a wrapper script harder to write. Two old invocations collapse
onto `pro enrollment list`, whose endpoints went to different places, so that one names
both. `pro mobile-device-prestages delete-multiple` and
`pro mobile-device-prestage-scopes delete-multiple` collapse onto one path the same
way, and also name both. Three of the 48 are a verb whose *meaning* moved rather than
its name —
`create`, `update` and `delete` under `pro enrollment-customization-panels`, which
resolve to a live command that addresses the customization instead of the panel, so
they are refused where the other 45 would have got `unknown command`. The refusals
retire with the aliases on 2027-03-09.

The subsections after this one cover the rest: 9 endpoints that are no longer ingested
at all, 3 `apply` commands that were never expressible, and 3 paths that became command
*groups*.

| was | is now | endpoint |
|---|---|---|
| `pro access-managements list` | `pro enrollment access-management` | `GET /v4/enrollment/access-management` |
| `pro activation-codes patch` | `pro activation-code organization-name patch` | `PATCH /v1/activation-code/organization-name` |
| `pro api-roles-privileges api-role-privileges` | `pro api-role-privileges list` | `GET /v1/api-role-privileges` |
| `pro app-requests create` | `pro app-request-form-input-fields create` | `POST /v1/app-request/form-input-fields` |
| `pro app-requests delete` | `pro app-request-form-input-fields delete` | `DELETE /v1/app-request/form-input-fields/{id}` |
| `pro app-requests list` | `pro app-request-form-input-fields list` | `GET /v1/app-request/form-input-fields` |
| `pro app-requests settings` | `pro app-request get` | `GET /v1/app-request/settings` |
| `pro app-requests update-settings` | `pro app-request update` | `PUT /v1/app-request/settings` |
| `pro cloud-distribution-points cloud-distribution-point` | `pro cloud-distribution-point list` | `GET /v1/cloud-distribution-point` |
| `pro computer-inventory-collection-settings create` | `pro computer-inventory-collection-settings-custom-path create` | `POST /v2/computer-inventory-collection-settings/custom-path` |
| `pro computer-inventory-collection-settings delete` | `pro computer-inventory-collection-settings-custom-path delete` | `DELETE /v2/computer-inventory-collection-settings/custom-path/{id}` |
| `pro csas delete` | `pro csa token delete` | `DELETE /v1/csa/token` |
| `pro enrollment-languages filtered-language-codes` | `pro enrollment filtered-language-codes` | `GET /v3/enrollment/filtered-language-codes` |
| `pro enrollment-languages language-codes` | `pro enrollment language-codes` | `GET /v3/enrollment/language-codes` |
| `pro enrollment-settings create` | `pro enrollment-access-groups create` | `POST /v3/enrollment/access-groups` |
| `pro enrollment-settings delete` | `pro enrollment-access-groups delete` | `DELETE /v3/enrollment/access-groups/{id}` |
| `pro enrollment-settings enrollment` | `pro enrollment get` | `GET /v4/enrollment` |
| `pro enrollment-settings list` | `pro enrollment-access-groups list` | `GET /v3/enrollment/access-groups` |
| `pro enrollment-settings update-enrollment` | `pro enrollment update` | `PUT /v4/enrollment` |
| `pro health-checks health-check` | `pro health-check list` | `GET /v1/health-check` |
| `pro jamf-connects jamf-connect` | `pro jamf-connect list` | `GET /v1/jamf-connect` |
| `pro jamf-connects update` | `pro jamf-connect-config-profiles update` | `PUT /v1/jamf-connect/config-profiles/{id}` |
| `pro jcds delete` | `pro jamf-cloud-distribution-service-files delete` | `DELETE /v1/jcds/files/{fileName}` |
| `pro jcds files` | `pro jamf-cloud-distribution-service-files create` | `POST /v1/jcds/files` |
| `pro jcds get` | `pro jamf-cloud-distribution-service-files get` | `GET /v1/jcds/files/{fileName}` |
| `pro jcds list` | `pro jamf-cloud-distribution-service-files list` | `GET /v1/jcds/files` |
| `pro local-admin-passwords update` | `pro local-admin-password settings update` | `PUT /v2/local-admin-password/settings` |
| `pro log-flushings delete` | `pro log-flushing-task delete` | `DELETE /v1/log-flushing/task/{id}` |
| `pro log-flushings get` | `pro log-flushing-task get` | `GET /v1/log-flushing/task/{id}` |
| `pro log-flushings log-flushing` | `pro log-flushing list` | `GET /v1/log-flushing` |
| `pro log-flushings task` | `pro log-flushing-task create` | `POST /v1/log-flushing/task` |
| `pro managed-software-updates-plans abandon` | `pro managed-software-updates-plans feature-toggle abandon` | `POST /v1/managed-software-updates/plans/feature-toggle/abandon` |
| `pro managed-software-updates-plans status` | `pro managed-software-updates-plans feature-toggle status` | `GET /v1/managed-software-updates/plans/feature-toggle/status` |
| `pro managed-software-updates-plans update` | `pro managed-software-updates-plans feature-toggle update` | `PUT /v1/managed-software-updates/plans/feature-toggle` |
| `pro mdm-renewals patch` | `pro mdm-renewal-device-common-details patch` | `PATCH /v1/mdm-renewal/device-common-details` |
| `pro mobile-device-prestage-scopes delete-multiple` | `pro mobile-device-prestages scope-delete-multiple` | `POST /v2/mobile-device-prestages/{id}/scope/delete-multiple` |
| `pro mobile-device-prestages delete-multiple` | `pro mobile-device-prestages attachments-delete-multiple` | `POST /v3/mobile-device-prestages/{id}/attachments/delete-multiple` |
| `pro policy-properties policy-properties` | `pro policy-properties get` | `GET /v1/policy-properties` |
| `pro policy-properties update-policy-properties` | `pro policy-properties update` | `PUT /v1/policy-properties` |
| `pro schedulers summary` | `pro scheduler list` | `GET /v1/scheduler/summary` |
| `pro schedulers triggers` | `pro scheduler-jobs triggers` | `GET /v1/scheduler/jobs/{jobKey}/triggers` |
| `pro self-service-plus get` | `pro self-service-plus settings get` | `GET /v1/self-service-plus/settings` |
| `pro self-service-plus update` | `pro self-service-plus settings update` | `PUT /v1/self-service-plus/settings` |
| `pro sso-failovers list` | `pro sso-settings failover` | `GET /v1/sso/failover` |
| `pro sso-settings-cert cert` | `pro sso-settings cert create` | `POST /v2/sso/cert` |

#### 9 endpoints are no longer ingested

- `GET`/`POST`/`PUT`/`DELETE /v1/inventory-preload{,/{id}}` (5) — v1 is withdrawn from
  the gateway's published API *and* fully superseded: v2 moved every record operation
  under `records/`, so the CRUD lives at `pro inventory-preload-records`. Left in, the
  refused v1 paths took the plain `list`, `get` and `update` names while the served v2
  CRUD sat under a second command.
- `GET /preview/remote-administration-configurations` — the bare collection stub above
  the team-viewer family. Dropping it leaves
  `/preview/remote-administration-configurations/team-viewer/…` intact, which is the
  surface the gateway publishes.
- `POST /settings/issueTomcatSslCertificate` — unversioned legacy with no replacement
  in the versioned API.
- `POST /v1/computer-inventory/{id}/{erase,remove-mdm-profile}` (2) — the
  inventory-preload case with the noun changed: declared `deprecated` upstream,
  withdrawn from the gateway, and superseded by `/v4/computers-inventory/{id}/…`,
  which spells the collection segment differently. See the section below for what
  keeping them cost.

#### Operation names on a merged resource

Where a tag merges what were several resources, the resource's own root endpoint
keeps `get`/`list`/`update` and its siblings are named after their path segment. The
first cut renamed **both** sides of the collision, so the primary endpoint lost its
verb — `pro sso-settings sso` for `GET /v3/sso` beside `cert` for `/v2/sso/cert`, and
`pro enrollment enrollment` beside `language-codes`. 15 resources, 19 operations. A
resource with no root endpoint keeps segment naming throughout: `pro ldap` has
`groups`, `servers` and `ldap-servers` and no `list`, because there is no
`GET /v1/ldap` and a plain `list` would point at one arbitrary sub-collection with
nothing in the name saying which.

The same "the resource is one path root" assumption was in three separate places and
cost: three collection POSTs their `create`
(`pro jamf-cloud-distribution-service-files create`, `pro log-flushing-task create`,
`pro app-installers-deployments create`); two collection GETs their `list`
(`pro app-installers-titles list`, `pro app-installers-deployments list`); and two
singletons their `get` (`pro jamf-protect`, `pro cloud-distribution-point`), whose
root reads shipped as `list` because the singleton rename gave up as soon as *any*
operation on the resource carried a path parameter — which a merged resource always
has.

Where a sub-path gives up a plain verb to the resource root, the sub-path is named
after its segment: `pro sso-settings get`/`update` are `GET`/`PUT /v3/sso`. A verb
that collides with *nothing*, though, keeps the plain name even on a sub-path — see
the next section, which is what that cost and how it is fixed.

#### Independently-writable sub-paths are commands of their own

A tag can cover several path roots, and the grouping merges them into one resource.
That is right for the resource's identity and wrong for its verbs: an
independently-writable sub-path flattened into its parent produces a plain CRUD verb
that silently belongs to the sub-path, and because a lone verb collides with nothing,
no naming pass reached it.

Three examples of what that cost, all live before this change:

- **`pro sso-settings delete` sent `DELETE /v2/sso/cert`.** It deleted the SSO
  certificate, not the SSO configuration its name names. `download` and `parse`
  belonged to the certificate too.
- **`pro managed-software-updates-plans update` sent
  `PUT /v1/managed-software-updates/plans/feature-toggle`** on a resource whose
  `list`, `get` and `create` are real plan CRUD — so the one verb in the set that
  mutated pointed at a different object entirely.
- **`pro csa delete` deleted the CSA token.**

**The rule is derived from the spec.** A sub-path that carries `PUT`, `PATCH` or
`DELETE` on the sub-path *itself* is a separately-writable object and becomes a
command of its own; a `POST`-only sub-path is an append or a command submission and
stays flat. Measured over the committed document that admits **nine** sub-paths and
refuses every other one, including all 18 GET+POST `/history` pairs — by the rule,
not by a `history` special case. One further condition: a sub-path holding *every*
operation in its group does not split, because there is no sibling verb for the plain
one to be confused with (`pro app-request get` and
`pro service-discovery-enrollment get` stay as they are).

| new command | endpoint |
|---|---|
| `pro activation-code organization-name` | `/v1/activation-code/organization-name` |
| `pro app-installers global-settings` | `/v1/app-installers/global-settings` |
| `pro csa token` | `/v1/csa/token` |
| `pro enrollment adue-session-token-settings` | `/v1/adue-session-token-settings` |
| `pro local-admin-password settings` | `/v2/local-admin-password/settings` |
| `pro managed-software-updates-plans feature-toggle` | `/v1/managed-software-updates/plans/feature-toggle` |
| `pro self-service settings` | `/v1/self-service/settings` |
| `pro self-service-plus settings` | `/v1/self-service-plus/settings` |
| `pro sso-settings cert` | `/v2/sso/cert` |

Their verbs are plain again, so `pro sso-settings cert get|create|update|delete` and
`pro sso-settings cert download|parse`. Every moved invocation is in the table above.

**Four retired resource names now redirect two tokens deep.** A cobra alias is a name
on one command, so it can only resolve to a direct child of `pro`; these four are
registered as hidden second instances of the nested subtree, built from the same
generated constructor so the redirect cannot drift from what it redirects to. Each
warns and works: `pro sso-settings-cert`, `pro app-installer-global-settings`,
`pro self-service-settings`, `pro account-driven-user-enrollment-session-token-settings`.
`pro self-service-settings get` in particular resolved *correctly* before the nesting
and would have answered `unknown command "get"` after it.

**Two silent meaning changes are repaired by this, both introduced by the rename
above and neither visible to a test that only checks that a command resolves:**

- `pro sso-settings download` sent `GET /v3/sso/metadata/download` — the SAML metadata
  — before the rename and `GET /v2/sso/cert/download` after it, because the
  certificate's `download` won the collision and the metadata download was
  disambiguated away to `v-3-metadata-download`. It sends the SAML metadata again, and
  the certificate is at `pro sso-settings cert download`.
- `pro sso-settings delete` no longer exists rather than deleting the certificate.

#### 3 paths became command groups — help text at exit 0

The sharpest edge of the rename, because the failure is silent in the worst way: the
path still resolves, still exits **0**, and prints usage text where it used to return
data. A script piping one of these into `jq` gets help.

| was (returned data) | is now | the data is at |
|---|---|---|
| `pro csas token` | the `pro csa token` group | `pro csa token get` |
| `pro local-admin-passwords settings` | the `pro local-admin-password settings` group | `pro local-admin-password settings get` |
| `pro managed-software-updates-plans feature-toggle` | the `… feature-toggle` group | `pro managed-software-updates-plans feature-toggle get` |

A parent that prints help on exit 0 is this CLI's convention everywhere — `pro
categories` does it too — so a bare invocation still does exactly that; what changed is
that these three specific paths used to be leaves.

**An invocation that asked for data is refused instead.** Where `--output`, `--field`,
`--select` or `--out-file` reaches one of the three, it exits 2 naming the leaf rather
than printing help at 0 — so `pro csas token -o json | jq -r .value` fails at the CLI
with the answer in it, instead of feeding jq a usage message. A bare `pro csa token`,
and a typo beneath it, are unchanged.

#### 3 `apply` commands are gone, and none of them worked

`apply` is synthesized from a resource's `list`, `create` and `update`. Where a
resource split, those three no longer co-reside, so it is no longer generated:
`pro app-requests apply`, `pro enrollment-settings apply` and
`pro managed-software-updates-plans apply`.

The last is worth stating plainly, because it was **destructive rather than merely
absent**. `/v1/managed-software-updates/plans` publishes a collection `POST` and a
`{id}` `GET` and no update at all, so the only `PUT` the resource carried was the
feature toggle's — and `apply` composed it. Given the name of an existing plan it
resolved the name to an id and then `PUT` the plan document at
`/v1/managed-software-updates/plans/feature-toggle`, replacing the tenant's managed
software update feature toggle. There is no plan update endpoint to point a working
`apply` at; use `create`.

#### 19 write operations that were being deleted at generate time now ship

A name held by two operations was resolved by keeping one and **discarding the
other**, with a warning on stderr and exit 0. That is the wrong resolution
whichever operation loses, and on this branch the loser was twice the resource's
own root:

- **`pro enrollment-customization create`, `update` and `delete` addressed an
  LDAP panel.** The `enrollment-customization` tag covers a singular root
  carrying four panel families under `{id}` — ldap, sso, text and all — and the
  plural `/v2/enrollment-customizations` carrying the customization's own CRUD.
  Nine operations wanted three names, the panels' won, and the customization's
  POST, PUT and DELETE stopped existing. `apply` was built on top of that:
  it resolved a **customization** name to a customization id, substituted it into
  `{panel-id}` and `PUT` the document at an LDAP panel path, with `{id}` never
  substituted. Same shape as the `managed-software-updates-plans apply` defect
  above, on a different resource.
- **`pro computer-prestage-scopes create-scope` and `update-scope` were gone**,
  and the same two on `pro mobile-device-prestage-scopes`. `GET`, `POST` and `PUT`
  all sit on `/v2/computer-prestages/{id}/scope`, so the disambiguation had
  nothing left to separate them with once the collection-level `GET .../scope`
  took `scope` — and gave all three the same name.

A resource's plain verbs are its root's, so the root keeps `create`, `update` and
`delete` and a sub-path's write is qualified by the segment that owns it. Where
two methods share one path, the method separates them, as it already did for a
collision on the canonical path. Nothing is dropped in either case.

The panel writes that *did* hold the plain verbs are renamed rather than
restored: `pro enrollment-customization ldap-create`, `ldap-update` and
`all-delete` are what `create`, `update` and `delete` used to send.

The same resolution restored **twelve more operations `main` never shipped
either**, every one the loser of a collision between two sub-paths:

| command | endpoint |
|---|---|
| `pro enrollment-customization sso-create` / `sso-update` / `sso-delete` | `POST` `…/{id}/sso`, `PUT`/`DELETE` `…/{id}/sso/{panel-id}` |
| `pro enrollment-customization text-create` / `text-update` / `text-delete` | the same three on `…/{id}/text` |
| `pro enrollment-customization ldap-delete` | `DELETE …/enrollment-customization/{id}/ldap/{panel-id}` |
| `pro packages manifest-delete` | `DELETE /v1/packages/{id}/manifest` |
| `pro venafi proxy-trust-store-create` / `proxy-trust-store-delete` | `POST`/`DELETE /v1/pki/venafi/{id}/proxy-trust-store` |
| `pro patch-software-title-configurations dashboard-delete` | `DELETE /v3/patch-software-title-configurations/{id}/dashboard` |
| `pro computer-inventory attachments-delete` | `DELETE /v4/computers-inventory/{id}/attachments/{attachmentId}` |

**One invocation changes meaning and is refused rather than left to run.**
`enrollment-customizations` and `enrollment-customization-panels` both resolve
onto the one `enrollment-customization` the tag merges them into, and both
shipped a `create`, an `update` and a `delete`. Only one can keep the plain verb
and it is the resource's own root, so those three under the *panels* spelling
refuse with the panel command to use instead — `pro enrollment-customization-panels
create` names `pro enrollment-customization ldap-create`. The panel reads under
that spelling (`all`, `ldap`, `sso`, `text`, `markdown`, `parse-markdown`) are
unchanged.

The surviving 16 drops are pinned in
`generator/parser/testdata/dropped-operations.tsv`, with the reason for the two
that are not simple collisions between sub-paths. A new one fails the build; a
root operation losing its name to a sub-path's fails it outright.

#### No command name carries an API version

Three did. Two operations whose derived names collide are separated by the parts
of their paths that differ, and that included the version segment — so
`/v2/mobile-device-prestages/{id}/scope/delete-multiple` came out as
`v-2-scope-delete-multiple`. A version in a command name is the thing this
change exists to remove: it is how the CLI came to send `/v3` while `/v4` was
the served version.

A version segment names no resource and no longer enters a name. Where a
collision is between two sub-paths and **neither** addresses the resource's own
root, no operation has a claim on the plain verb and each is qualified by the
thing it acts on:

| was | now | endpoint |
|---|---|---|
| `pro mobile-device-prestages delete-multiple` | `pro mobile-device-prestages attachments-delete-multiple` | `POST /v3/mobile-device-prestages/{id}/attachments/delete-multiple` |
| `pro mobile-device-prestage-scopes delete-multiple` | `pro mobile-device-prestages scope-delete-multiple` | `POST /v2/mobile-device-prestages/{id}/scope/delete-multiple` |

Both old invocations refuse and name both replacements, since they collapse onto
one path after alias resolution and went to different places. The bare
`delete-multiple` deleting attachments on one prestage resource while its
computer-prestage sibling's removed scope is also gone with it.

#### The two v4 device actions take their own names, and the deprecated v1 pair is dropped

`POST /v1/computer-inventory/{id}/erase` and `remove-mdm-profile` are declared
`deprecated` upstream and withdrawn from the gateway; `POST
/v4/computers-inventory/{id}/…` serves both and is published. They are the same
endpoint at two path *shapes* — v4 renamed the collection segment from
`computer-inventory` to `computers-inventory` — so version consolidation, which
matches on the shape, saw two unrelated endpoints and kept both.

The deprecated pair took the plain `erase` and `remove-mdm-profile` names, which
had three consequences. The served pair came out as
`v-4-computers-inventory-erase` and `v-4-computers-inventory-remove-mdm-profile`.
`pro.go` suppresses a generated `erase`/`remove-mdm-profile` in favour of the
hand-written pair that targets by serial, name or group — so it suppressed the
*deprecated* operations and the served twins shipped beside `pro comp erase` and
`pro comp remove-mdm`, without the `--confirm-destructive` gate the hand-written
pair require for a bulk destructive operation. And those two paths are the only
ones in the document whose collection segment is singular, so they alone decided
the resource's own name.

The v1 pair is now dropped at ingest, the same way inventory-preload v1 already
was and for the same reason. `main` shipped neither (both were suppressed there
too, as whole resources), so no capability moves.

#### Also fixed by the same change

- `pro computer-groups` was registered twice — the generated registry called
  `NewComputerGroupsCmd` for two filenames naming one resource, so `pro --help`
  printed the row twice and every path beneath it resolved to the first copy. Resource
  identity comes from the paths now, so there is one subtree.
- `GET /v1/branding-images/download/{id}` was unreachable: the old per-file filter
  discarded it with a warning on every generate. It ships as `pro branding download`.
- **A stale wiring key is now a build failure rather than an extra command.** The
  three helpers that wire hand-written commands onto generated parents keyed on
  names and answered a stale key by doing nothing — so a suppression left the
  generated command in place and a replacement was added beside it. Every miss is
  recorded and a test fails on a non-empty record, which found two more dead keys
  the rename had left (`apply` and `get-by-name` on the former `jamf-protects` and
  `jamf-protect-deployment-tasks`); both are removed.

### Breaking — Classic `scope` subcommands take `[<id>]` and `--name`

- **The positional argument is now the id, not the name.** `pro <resource> scope
  get|add|remove` took the name positionally — the only commands in the binary that
  did — so a caller holding an id from `list` had to go and find the name for it, and
  an object whose name looks like an id could not be addressed at all. They now match
  every other Classic command: an optional `[<id>]` plus `--name`, refused together.

  ```
  # before
  jamf-cli pro classic-policies scope add "Deploy Chrome" --computer-group "Lab Macs"
  # after — either of
  jamf-cli pro classic-policies scope add 1 --computer-group "Lab Macs"
  jamf-cli pro classic-policies scope add --name "Deploy Chrome" --computer-group "Lab Macs"
  ```

  A non-numeric positional is refused at exit 2 naming `--name`, rather than being sent
  as an id and answering a 404 whose hint points at `list`.

- **Each command now registers only the scope categories its own resource carries**, so
  `--help` and shell completion stop offering flags the server refuses. Validation had
  two branches — restricted software, and everything else — so a policy accepted
  `--mobile-device-group` and a mobile configuration profile accepted `--computer-group`,
  both spending a GET and a PUT to earn `409 Error: Mobile device groups cannot be
  assigned to an macOS profile`. There are five scope shapes across the eight scopeable
  resources, read off each resource's own GET and cross-checked against
  terraform-provider-jamfplatform's independently wire-probed schemas.

  A category belonging to another device family is now an unknown flag, whose hint names
  the categories this resource does have. A valid category in the wrong section is
  refused naming the section that takes it.

- **`--ibeacon` and `--class` are new.** Both were categories `scope get` listed and
  nothing could write — the worst shape for a gap, since the CLI showed a value it could
  not change. `--ibeacon` is accepted on the three resources that carry iBeacons and
  refused on the five that silently drop them; `--class` on ebooks only.

- **`--user` is now accepted as a restricted-software exclusion.** It was refused
  outright; the wire accepts it and stores it. It is the admin UI's "Directory
  Service/Local Users" exclusion.

- **A scope write sends only `<scope>`.** It used to GET the whole document, splice the
  new scope into its bytes and PUT the entire document back, so one `scope add` cost
  three GETs and re-sent sections the caller had not touched. A Classic PUT is a partial
  update at top-level-section granularity, verified on all eight resources against a
  direct instance and the platform gateway: every non-scope byte comes back identical,
  including a 19 KB configuration profile's `<payloads>`.

- **A scope name that matches more than one record is refused** instead of resolving to
  the first in document order. Only the two VPP resources reach this path, having no
  `/name/` endpoint; Classic names are not unique, and a live tenant carried two ebooks
  sharing one.

- **`-n, --dry-run` on a scope command no longer always fails.** The preview suppressed
  the PUT and the post-write check then reported that the server had not persisted the
  change, at exit 1.

### Fixed — Classic scope writes no longer silently destroy an ebook's class targets

- **Class targets are delivered in two requests, because one cannot work.** Jamf Pro
  stores `<classes>` only while the stored category is empty: a write made while it
  already holds a member clears it, and carrying the identical value, omitting the
  element and every identifier shape all clear it (5/5 each way). Since a scope PUT
  replaces `<scope>` wholesale, no single request can preserve an existing class across
  any other scope change — so `scope add --building` on an ebook holding a class
  destroyed the class and reported success. The intended change is now sent with
  `<classes>` emptied, then the same scope with the classes populated.

  The same defect is a hard `Provider produced inconsistent result after apply` in
  terraform-provider-jamfplatform, reported as
  [#428](https://github.com/jamf/terraform-provider-jamfplatform/issues/428).

- **A write is checked against the whole scope that was sent**, not just the category the
  command changed — which is why the loss above went unreported. Anything the server did
  not keep is named.

- **A Classic HTTP error renders its reason instead of an HTML page.** The Classic API
  answers a refused write with a status page whose one useful sentence was buried in
  ~400 bytes of markup and inline CSS, arriving in the JSON error envelope as a single
  escaped line. `Error: Unable to match computer group` and its siblings are now the
  message.

### Fixed — a destructive `x-action` no longer sends `DELETE`, or describes itself as a delete

- **`--from-file` and `--group` on a destructive action sent the wrong HTTP
  method.** The bulk block is shared by a plain `delete` and by an `x-action` that
  happens to be destructive, and it hardcoded `DELETE`. So
  `pro mobile-device-groups erase --from-file ids.txt` sent
  `DELETE /v2/mobile-device-groups/{id}/erase` — a `POST`-only endpoint — and
  reported `Deleted` for each entry. It sends the operation's own method now.
  Present in `v1.28.0` and earlier.

- **Confirmation prompts named the wrong action.** The bulk prompt said it would
  "delete N <resource>", which for an erase reads as removing inventory records
  rather than wiping devices, and the single-item prompt interpolated the raw
  operation name into the sentence. A `DELETE` still reads `This will delete …`;
  anything else quotes its own action — `This will run "erase" on mobile-device-group
  "…" (id: …)`. There is no English verb for `remove-mdm-profile`, and inventing one
  is how a prompt comes to describe a different action from the one it performs. The
  `--from-file` and `--group` help text and the `[dry-run]` lines follow the same rule.

### Fixed — the deprecation warning fires wherever a global flag is placed

- **A flag between the product token and the resource token silenced both the
  deprecation warning and the moved-verb refusal.** The resource token was read out
  of `argv` by treating anything not starting with `-` as the resource, so
  `jamf-cli pro -p ci-svc icons get 1` read the profile name as the resource. Cobra
  does not require a global flag before the subcommand and `-p` is the documented way
  to select a profile, so that is an ordinary invocation — and for it, the migration
  signal the whole 2027-03-09 window rests on was absent for all 100 retired names,
  and `pro -p … enrollment-customization-panels update 1 2` got cobra's bare arity
  error instead of the refusal naming its replacement. Whether a flag consumes the
  next argument is now asked of the command's own flag set, by cobra's rules.


### Breaking — `--file` is renamed to `--from-file` on Platform and Security Cloud writes

- **Every request-body flag is now `--from-file`.** Platform and Security Cloud commands
  took `--file` while Jamf Pro, Classic, Protect and School took `--from-file`, and
  `pro platform-device-groups` carried both — `patch` and `patch-members` took `--file`,
  `apply` took `--from-file`. 41 flag registrations were renamed, and all 361 commands that
  read a body from a path now agree on the name.

  **There is no compatibility alias.** `--file` answers `unknown flag` (exit 2) on the
  affected commands, so a script passing it fails at once instead of two spellings surviving
  in parallel. The migration is a rename:

  ```bash
  # Before
  jamf-cli security ztna-gateways create --file gateway.yaml
  # After
  jamf-cli security ztna-gateways create --from-file gateway.yaml
  ```

  Passing the old name prints `hint: did you mean --from-file?` before the error. The hint is
  looked up rather than guessed by edit distance, which would have suggested `--field`.

  Affected: every generated `platform` and `security` command that takes a body, plus
  `pro platform-device-groups patch` and `patch-members`.

  **`--file` is unchanged on the 11 commands where it names an upload payload** —
  `pro packages upload`, `pro icon upload`, `pro inventory-preload upload` and
  `csv-validate`, `pro computer-inventory upload`,
  `pro computer-extension-attributes upload`,
  `pro enrollment-customization-images upload`,
  `pro mobile-device-prestages upload`, `pro self-service upload`,
  `protect analytics import` and `protect unified-logging-filters import`. That is a
  different flag with the same name: a `--from-file` body can always arrive on a pipe
  instead, and a multipart upload cannot, because the transport needs a filename and a
  length. Renaming those too would have produced one flag name with two capabilities.

### Behaviour — Platform and Security Cloud bodies can be piped

- **`--from-file` is now optional on those commands: absent, the body is read from stdin.**
  `ReadBody` in both products was `os.ReadFile` and nothing else, where Jamf Pro and Protect
  had always accepted a pipe — so these were the only namespaces where
  `--scaffold | edit | apply` had to route through a temp file.

  ```bash
  jamf-cli security ztna-gateways apply --scaffold | vipe | jamf-cli security ztna-gateways apply --yes
  ```

  An **empty pipe is not a body**: a CI runner hands every process a stdin that is not a
  terminal and carries nothing, so that case still reads as "no body given" and a bodyless
  write is unaffected. A `--from-file` naming an **empty file** remains an error, unchanged.

### Added — `apply` on the platform gateway resources

- **Six gateway resources gained `apply`** (create-if-absent, update-if-present, keyed on the
  `name` in the body): `security dns-zones`, `security ztna-apps`, `security ztna-gateways`,
  `security ztna-grouped-gateways`, `security device-groups` and `platform ai-policies`. The
  verb existed on Jamf Pro and Classic and on no gateway resource.

  Three properties a script should rely on:

  - The name is read from the body, never a flag; a body with no `name`, or a non-string or
    empty one, is refused before any request is sent.
  - Only a genuine "not found" on the exists check takes the create branch. An auth error or
    a 5xx during the lookup aborts.
  - An existing resource is confirmed before being overwritten; `--yes` skips the prompt, and
    is required when stdin is not a terminal.

  These resources update with `PATCH`, so **fields omitted from the body keep their current
  values** — unlike Jamf Pro's and Classic's `apply`, which replace. Each command's `--help`
  states which semantics it has. `platform ai-policies` is the exception within the
  exception: its server replaces `settings` wholesale despite the merge-patch content type,
  and its help says so.

  Two things `apply` cannot do for you, both stated in each command's `--help`:

  - **The exists check and the create are separate requests**, so two runs racing on the same
    absent name — a CI retry, or concurrent jobs — can both create one. Serialise `apply` per
    resource where that matters. No spec here declares a name-uniqueness `409`, so the server
    is not a backstop.
  - **`platform ai-policies apply` writes a draft.** A draft is not enforced until it is
    published, so follow a successful apply with `platform ai-policies publish <id>`. `apply`
    does not publish: publishing is non-idempotent (nothing pending answers `409`), and
    folding it in would leave no way to write a draft alone.

- **The Jamf Security Cloud Radar commands deliberately have no `apply`.** That surface is
  singletons whose `update` is already an idempotent create-or-replace (`stream`, `status`),
  actions (`risk override`, `verification trigger`, `device-lifecycle purge`) and read-only
  documents (`well-known`, `jwks`) — there is no named collection to resolve a name against.

### Behaviour — name lookups report an ambiguity or an unaddressable match instead of guessing

- **A name repeated across a page boundary is now an error.** `--name` and `apply` decided
  matches one page at a time and returned on the first page holding exactly one, so a name
  appearing on both sides of a 100-item boundary resolved to whichever copy sorted first,
  silently. Matches accumulate across every page before the decision, so the ambiguity error
  fires wherever the copies sit.

- **A name that matches an item the list returns no ID for is no longer reported as "not
  found".** Absence and "found it, cannot address it" had the same answer, which reads as a
  typo to a person and as "create it" to `apply`. It now says the list returns no ID for the
  items it matched. Security Cloud's device groups are the live case: the implicit
  "Default Group" is returned with a name and no `id`, so
  `security device-groups apply --set "name=Default Group"` refuses rather than creating a
  second group named the same. Stored groups are unaffected — they carry an `id` and resolve
  normally.

- **The ambiguity error no longer tells the caller to "pass the positional ID".** `apply`
  takes no positional, so on the command that most needs the message it named a remedy the
  command has no argument for.

### Fixed — Platform writes

- **`apply`'s update sends the same `Content-Type` as the resource's own `patch`.** It was
  derived from the media type the spec literally declares, so `platform ai-policies apply`
  sent `application/json` to the URL `platform ai-policies patch` sends
  `application/merge-patch+json` to (the SDK's default for any bodied `PATCH`) — a
  divergence on the wire between two commands documented as having the same semantics.
- **`apply` carries the `jamf:scopes` annotation its sibling verbs carry.** Without it the
  scope-level note could not fire for `apply`, and `apply` was absent from the `scopes` field
  in `jamf-cli commands -o json`.
- **`--set` on a platform command refuses to overwrite a non-object field** rather than
  discarding it: `--set scope.owner=alice` over a body whose `scope` is a string now errors,
  matching what the Security Cloud commands already did.
- **A piped body over 10MB is refused instead of truncated.** The cap returned `io.EOF`,
  indistinguishable from real end-of-input, and a cut JSON or YAML document can still parse —
  so a long desired-state document lost its trailing fields and reported success.
- **A failure to inspect stdin is now reported.** It was collapsed into "no input", so a
  command could silently send no body where one had been piped.
- **The renamed-flag hint reaches `-o json`.** It was written straight to stderr, so it
  landed ahead of the JSON error block in combined output and the envelope's `hint` field
  was empty — on the one hint a CI job would most want to read structurally.

### Added — Jamf Pro 11.32 and jamfplatform-go-sdk v1.1.0

The SDK moved to the `jamf` GitHub org and its module path with it
(`github.com/jamf/jamfplatform-go-sdk`). That is an import change inside this repo and
nothing a CLI user sees. `v1.0.0` under the old `Jamf-Concepts` path keeps working and
gets no further releases.

- **`pro sso-settings oidc-broker-config get` and `update`** are new — `GET` and
  `PUT /v3/sso/oidc-broker-config`, added in Jamf Pro 11.32. A sub-path carrying its own
  `PUT` is an independently-writable object, so its verbs sit one token deeper, the same
  shape as its sibling `pro sso-settings cert`. Nothing moved: there is no earlier
  spelling of this command, the endpoint not having existed before.
- **`pro jamf-pro-notifications delete` gains `--all`**, which dismisses every
  dismissible notification for the user and site in one server-side call
  (`DELETE /v1/notifications`, new in 11.32). It refuses to be combined with an `<id>`
  and prompts for confirmation unless `--yes` is passed, like every other tenant-wide
  `--all`. **`delete <id> <type>` is unchanged** — same positionals, same endpoint.

  `-n, --dry-run` covers it: `delete --all -n` reports what it would send and sends
  nothing. That needed its own code — a destructive generated command declares its own
  `--dry-run`, which shadows the root persistent one, so the command's own branch is the
  only thing honouring `-n` on it, and `--all` returns before that branch is reached.

  Worth knowing why that needed saying: the new collection-level `DELETE` derives the
  same `delete` name as the existing per-notification one, and the collection addresses
  the resource root, so the plain verb went to it and the per-notification operation was
  dropped. Left alone, `pro jamf-pro-notifications delete` would have silently changed
  from removing one notification to dismissing all of them, and the per-notification
  capability would have gone. Both are now the same command.
- **Two filter fields and one preference** arrived with 11.32 and are visible in
  `--help`: `general.awaitingConfiguration` and `security.lockdownModeEnabled` on
  `pro computer-inventory list --filter`, and `showDirectoryGroupUuidColumn` on
  `pro jamf-pro-account-preferences update --set`.

### Changed — Jamf AI Governance commands are marked Preview

Upstream declares all twelve AI Governance operations preview endpoints, subject to
breaking change without warning, with general availability expected by 2027-03-03.

- Each command's `Short` now opens with `Preview - ` and its `Long` with the preview
  notice, both from the published spec. Anything parsing `platform ai-policies --help`
  or `platform ai-tools --help` text sees new wording.
- `jamf-cli commands -o json` carries `"preview": true` on those commands (thirteen: the
  spec's twelve operations plus the synthesized `apply`), which is what to key on rather
  than the help prose. Present only when true, so its absence means "nothing declared"
  rather than "GA"; it is a JSON field only, table and CSV columns coming from the first
  row.
- Help text no longer carries markdown `**bold**` or `_italic_` markers on any platform
  command; backticks, which quote a field or a value, are unchanged.

### Fixed — a YAML export or scaffold carries the same document as the JSON one

`pro blueprints export -o yaml` rendered each component's `configuration` as a sequence
of integers — the bytes of its own JSON text — and lower-cased every key
(`activationpredicate`). Both came from yaml.v3, which reads neither `json` tags nor
encoding/json's treatment of `json.RawMessage`. The same two applied to
`pro blueprints apply --scaffold -o yaml` and to the `--scaffold -o yaml` of
`pro compliance-benchmarks apply` and `pro platform-device-groups create`.

- **A YAML export and a YAML scaffold now carry the keys the JSON one carries.** A
  script keying on `activationpredicate`, or reading `configuration` as a list of
  numbers, has to read `activationPredicate` and a mapping instead. `-o json` is
  unchanged. The Jamf Protect and Jamf School exports are unchanged, their input types
  carrying no `json` tags for a YAML document to follow.
- **The YAML input path binds by `json` tag.** Every `--from-file` reading JSON or YAML
  through this path (`pro blueprints`, `pro compliance-benchmarks`, the platform device
  groups, and the Protect and School applies) now decodes YAML through JSON, so a
  key spelled either way binds — encoding/json matches a key case-insensitively.
- **A blueprint YAML export written by v1.31.0 or earlier is refused rather than sent.**
  Its byte-sequence `configuration` is a valid JSON array, so it would otherwise reach
  the gateway as the component's configuration. Re-export and apply that file.

## v1.28.0

The Jamf Platform API reached general availability on 2026-09-03. Most of this release is
that migration; **[docs/guides/platform-api-ga.md](docs/guides/platform-api-ga.md) is the
migration guide** and carries the detail, the error messages verbatim, and the reasoning.

The gateway coverage and Platform API surface in this release come from
`jamfplatform-go-sdk` v0.22.2 (GitOps build v2082): Jamf Pro API 11.31.0 at 476 paths and
700 operations, Classic API 11.28.0 at 270 paths and 589 operations. Which endpoints the
gateway publishes decides which commands are refused, so that surface is what the numbers
below are counted against — `jamf-cli commands -o json` reports the answer for the binary
in hand.

### Breaking — Platform gateway (only affects `auth-method: platform` profiles)

- **The gateway base URL is `https://{region}.api.jamfcloud.com`.** The pre-GA
  `https://{region}.apigw.jamf.com` is retired, and the `/api` path segment it required is
  gone. A profile still naming the old host is refused **by name** before any request is
  sent, because the wire symptom is an edge-level 403 during the token exchange that names
  neither the host nor the reason. The CLI does not rewrite the URL for you.
- **Public-beta credentials were revoked at GA.** Register a replacement API integration in
  Jamf Account; a beta client cannot be migrated. `jamf-cli platform setup` writes a fresh
  profile.
- **Three scope levels, one per profile: organization, platform environment, tenant.**
  `environment-id` (new; `--environment-id`, `JAMF_ENVIRONMENT_ID`) is the level to prefer;
  `tenant-id` is the legacy one; organization scope carries no ID and is selected by the
  gateway host alone. Supplying two levels at once is refused, in the environment as well as
  in a profile. The scope now travels in an `X-Environment-Id` / `X-Tenant-Id` header
  instead of a `/tenant/{tenantId}` URL segment.
- **67 Jamf Pro and Classic commands are refused on a gateway profile**, before a request is
  sent, with exit code 8 (`Refused by policy`) — they are outside the gateway's published
  API. Several of them still answer today; that is transitional, and refusing now is cheaper
  than the eventual bare `403 BAD_PERMISSIONS`. The remedy is a second `oauth2` profile
  against the instance.
- **24 of those are MDM device actions**, which is the refusal most likely to be felt:
  `pro mobile-devices` loses `lock`, `restart`, `shutdown`, `enable-lost-mode`,
  `disable-lost-mode`, `play-lost-mode-sound`, `clear-passcode`,
  `clear-restrictions-password`, `delete-user`, `log-out-user`, `unlock-user-account`,
  `apply-redemption-code`, `refresh-cellular-plans`, `request-mirroring`, `stop-mirroring`
  and `settings`; `pro computer-inventory` loses `lock`, `restart`, `shutdown`,
  `enable-remote-desktop`, `disable-remote-desktop`, `set-recovery-lock`,
  `set-auto-admin-password` and `settings`. The gateway's published API declares GET on
  those paths but not POST, so the refusal is per method rather than per resource.
  `pro comp erase` and `pro comp remove-mdm` are hand-written and are **not** affected.
  The other 35 are `pro api-integrations` (7), `pro classic-computer-configs` (7),
  `pro api-authentication` (6), `pro api-roles` (6),
  `pro jamf-pro-initialization` (3), `pro api-role-privileges` (2), and one each of
  `pro environment-type`, `pro macos-managed-software-updates`, `pro mdm commands` and
  `pro sso-oauth-session-tokens`. That is 59 in total rather than the 67 this file
  recorded before spec-derived naming, and the gateway un-refused none of them: six were
  `pro static-computer-groups`, the withdrawn v2 command, which stops existing when every
  version of an endpoint lands in one resource, and two were `pro policy-properties`,
  whose unversioned legacy twin `/settings/obj/policyProperties` is no longer ingested at
  all while the `/v1/policy-properties` it shared a tag with is published.
  `jamf-cli commands -o json | jq -r '.[] | select(.gateway=="unserved") | .command'`
  reports the current list for the binary in hand. `JAMF_CLI_ALLOW_UNPUBLISHED=1` downgrades
  an *unpublished* refusal to a stderr warning and sends the request anyway — a stopgap for
  one job, not a mode to settle into, and the warning it substitutes cannot be silenced. The
  reverse direction is refused the same way: a Platform-only command on an instance profile
  exits 8 naming the profile, its resolved auth method and `platform setup`, rather than
  reading as a credential problem.
- **Exit code 8 is new** — `Refused by policy`, for a command that is correctly invoked but
  cannot be served by the resolved credentials. Distinct from 2, which is also every cobra
  flag error.
- **A profile's scope level is no longer attached to credentials supplied for the
  invocation.** If `JAMF_CLIENT_ID` is set for the invocation, the profile's
  `environment-id` and `tenant-id` are both ignored rather than one being used. An
  integration is created at one level in Jamf Account and its credential carries that
  choice, so a profile's level describes the profile's own integration — and an
  organization-scoped credential must send no scope header at all. Before this,
  `JAMF_URL` + `JAMF_CLIENT_ID` + `JAMF_CLIENT_SECRET` for an organization-scoped
  integration sent an `X-Tenant-Id` taken from whatever `default-profile` named, and failed
  with a level the operator never chose. Supply the level for those credentials with
  `--environment-id` / `--tenant-id` or `JAMF_ENVIRONMENT_ID` / `JAMF_TENANT_ID`; the
  resulting error names the profile whose level was passed over and both ways to set one.
  A profile holding `client-id` with only `JAMF_CLIENT_SECRET` injected is unaffected — the
  client ID names the integration, so that is still the profile's. So is a profile whose own
  `client-id` is an `env:JAMF_CLIENT_ID` reference, which is the config's documented way for
  a profile to read its client ID out of the environment: the variable is then how that
  integration supplies its own credential, not a second integration displacing it, so the
  profile keeps its level. A `file:` reference is compared the same way — it is a plain
  read — and when that read fails the error names the path and the read error rather than
  blaming `JAMF_CLIENT_ID`, since nothing later in the invocation opens that file.
  **A profile whose `client-id` is a `keychain:` reference is affected, and that is the
  shape `platform setup` writes.** Resolving one can prompt, on a path that by definition is
  not using the profile's credentials, so the comparison cannot be made and the level is
  withheld. If you run such a profile in a shell that also exports `JAMF_CLIENT_ID` for the
  same integration, either drop that variable so the profile resolves its own credential, or
  set `JAMF_ENVIRONMENT_ID` / `JAMF_TENANT_ID` beside it. The error names the profile, the
  level it holds and both remedies.
- **A platform command's 403 now exits 5, not 1.** Platform commands previously returned the
  SDK's error untouched, so the one failure with a specific remedy exited with the generic
  code.
- **`security device-groups update` prints nothing on success.** It sends
  `PUT /securitycloud/v2/groups/{groupId}`, which answers `204` with no body, where the
  deprecated v1 form answered `200` with the updated group. The published spec has now
  withdrawn v1's update and list outright, and the v2 update handler — broken since it
  appeared, answering `403` and then a `404` on a group its own list returned — was fixed on
  2026-09-04, so the CLI no longer withholds the operation. `list` and `update` send v2;
  `create`, `get` and `delete` stay on v1. Anything reading the group out of an `update` has
  to follow with a `get`.

### Breaking — everything else

- **`--out-file`, `--select`, `--compact`, `--field`, `--quiet` and `--no-hints` now take
  effect on 27 commands that parsed and discarded them.** Those commands built their own
  output formatter, which receives none of the global flags, so each flag was accepted and
  then ignored. `--out-file` exited 0 and left the file at 0 bytes while the payload went
  to stdout. Affected are the `pro report` family, `pro audit`, `pro overview`,
  `protect overview`, `school overview`, `pro group-tools`, `pro classic app-usage`,
  `multi` and `commands`.

  Two changes are visible to a script. A job that passes `--out-file` and reads stdout now
  reads nothing, because the payload goes to the file it asked for. A job that passes
  `--select` or `--compact` and parses whole rows now receives narrowed rows.

  `
  `--select` and `--compact` narrow rows. They do not remove them, so a projection that
  matches no field in a row leaves that row present and empty. The 200+ generated commands
  have always behaved that way. The details:
  - Under `table`, `csv` and `plain`, the column set is the union across rows while either
    flag is active, because both flags make rows heterogeneous. A field only some rows
    carry used to be a column for none of them. Without a projector the first row still
    decides, as before.
  - Under `table`, `csv`, `plain` and the single-object detail view, a projection that
    matches nothing in any row now renders nothing. It used to print a row count above a
    blank header. The miss is named on stderr, and neither `--quiet` nor `--no-hints`
    silences it, because under those four formats no other output reports it.
  - The large-result hint is withheld while either flag is active. It recommended more of
    the flag that had just emptied the output.
  - `--select` bypasses the table's default-column heuristic, so a named field renders
    without `--wide`.
- **`pro audit -o raw --out-file f` and `-o xml --out-file f` now write a table, not JSON.**
  That command marshalled its rows and handed the bytes to `PrintRaw`, which passes JSON
  through unchanged for those two formats. It reached that branch only when `--out-file`
  was set. The shared formatter has no case for either format, so it renders a table. Use
  `-o json` for JSON. Without `--out-file` the output is byte-identical, and so is every
  other format. The six `pro report` multi-section reports already rendered `raw` and `xml`
  as t
