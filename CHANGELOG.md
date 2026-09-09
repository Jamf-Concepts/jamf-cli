# Changelog

Per-release notes — the full list of merged pull requests — are generated on
[GitHub Releases](https://github.com/Jamf-Concepts/jamf-cli/releases). This file exists for
what that list cannot say: which changes are **breaking**, what the migration is, and why.
So it records breaking changes, behaviour changes visible to a script, and removals. A
release with none of those gets no entry here.

Versions follow the `vMAJOR.MINOR.PATCH` tags in this repository, and headings match the
commit types the repo already uses (`feat!`/`build!` for a breaking change).

## Unreleased

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

#### 46 invocations an alias cannot cover — the operation name moved too

An alias maps one resource name to one replacement. Where a resource **split**, or
where an operation's name is derived from a path segment that now sits under a
different resource, the resource alias resolves and the subcommand then does not
exist. The endpoint is unchanged in every case, so only the command name moved.

**All 46 answer at runtime with the new invocation named**, in place of cobra's
`unknown command`, and both spellings of each reach it — the old resource name through
its alias, and the new one directly. `pro schedulers triggers` and `pro scheduler
triggers` both report that the operation moved and name `pro scheduler-jobs triggers`.
Exit code 2, the same as every other command that does not exist: what a caller is
missing here is not a classification but the pointer, and a second exit code for one
class would only make a wrapper script harder to write. Two old invocations collapse
onto `pro enrollment list`, whose endpoints went to different places, so that one names
both. Three of the 46 are a verb whose *meaning* moved rather than its name —
`create`, `update` and `delete` under `pro enrollment-customization-panels`, which
resolve to a live command that addresses the customization instead of the panel, so
they are refused where the other 43 would have got `unknown command`. The refusals
retire with the aliases on 2027-03-09.

The subsections after this one cover the rest: 7 endpoints that are no longer ingested
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
| `pro policy-properties policy-properties` | `pro policy-properties get` | `GET /v1/policy-properties` |
| `pro policy-properties update-policy-properties` | `pro policy-properties update` | `PUT /v1/policy-properties` |
| `pro schedulers summary` | `pro scheduler list` | `GET /v1/scheduler/summary` |
| `pro schedulers triggers` | `pro scheduler-jobs triggers` | `GET /v1/scheduler/jobs/{jobKey}/triggers` |
| `pro self-service-plus get` | `pro self-service-plus settings get` | `GET /v1/self-service-plus/settings` |
| `pro self-service-plus update` | `pro self-service-plus settings update` | `PUT /v1/self-service-plus/settings` |
| `pro sso-failovers list` | `pro sso-settings failover` | `GET /v1/sso/failover` |
| `pro sso-settings-cert cert` | `pro sso-settings cert create` | `POST /v2/sso/cert` |

#### 7 endpoints are no longer ingested

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

#### Also fixed by the same change

- `pro computer-groups` was registered twice — the generated registry called
  `NewComputerGroupsCmd` for two filenames naming one resource, so `pro --help`
  printed the row twice and every path beneath it resolved to the first copy. Resource
  identity comes from the paths now, so there is one subtree.
- `GET /v1/branding-images/download/{id}` was unreachable: the old per-file filter
  discarded it with a warning on every generate. It ships as `pro branding download`.


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
  as tables. What changed for them is that `--out-file` now receives the output.
- **A section banner is no longer written for a format a parser reads.** `json`, `yaml`,
  `ndjson`, `csv` and `plain` get no `── Section ──` lines. `xml` and `raw` keep theirs,
  because they render as tables. A multi-section report still emits one block per section
  under `csv`, `ndjson` and `plain`, now with nothing between the blocks. Use `-o json` or
  `-o yaml` for a single parseable file. All seven `pro report` commands with more than one
  section say so in their `--help`, from one shared string, and so does `multi`, whose
  aggregation renders one section per merged key.
- **`pro report patch-status --scan-failures -o json` now writes one array of labelled
  sections.** It wrote three separate documents into the same file, which `jq` rejects at
  the second one. Each section carries a `fetch_error` field. An empty section and a
  section whose fetch failed used to be byte-identical, so a scheduled job read "no
  failures" from a check that never ran. An empty section is `[]`, never `null`.
- **`--field` naming a field no row carries now reports that on stderr.** `--quiet` and
  `--no-hints` do not silence it. A `--field` miss produces no output at all, so nothing
  else distinguishes a wrong field name from an empty result.
- **`--field` now extracts on `pro group-tools export` and on a `multi` aggregation.** Both
  render through their own `--format` argument rather than the global `-o`, so the flag was
  parsed, listed in Global Flags and discarded. A `multi` aggregation applies it per
  section, the way a `pro report` section does.

- **A command that documents no positional argument now refuses one**, with exit 2
  (`usage`). 736 leaf commands used to accept any positional and discard it in silence, so
  `pro categories list junkarg` ran the list and `pro backup /tmp/out` ran a whole backup
  and ignored the directory that `--output` takes. A wrapper or a CI job that passes a
  stray token now fails where it used to succeed. Remove the argument, or move it to the
  flag that takes it; the refusal names the command and the value, `--help` lists every
  flag, and any required flag that is also unset is named too — so
  `pro diff staging production` still reports `--source` and `--target` rather than
  replacing that answer with the positional complaint. That clause is conditional: a
  refusal that can suggest a subcommand instead names no flag, so
  `pro backup list-resourcez` suggests `list-resources` and stays silent about `--output`,
  which there is the root format flag rather than a directory. The value is replaced with
  `<redacted>` when the command registers a credential flag (`--new-password`, `--pin`,
  `--unlock-token`) or when the positional is itself a `key=value` pair whose key names a
  credential, such as a dropped `--set account.password=…`, because this message reaches
  stdout as JSON when output is piped and from there a CI log. The key is read
  case-insensitively and split on both separators and camelCase boundaries, so `TOKEN=`,
  `CLIENT_SECRET=`, `CLIENTSECRET=` and `clientsecret=` all redact. A positional redacts too when a
  supplied `--set` element itself carries no `=`, which is the signature
  `--set <key> <value>` leaves behind when the `=` is lost: cobra takes the key as the
  flag's element and the credential becomes the positional. The test is on the flag set
  rather than on the value, so a secret containing `=` — base64 padding, or the character
  itself — is still covered, while a mistyped filename beside a well-formed pair still
  names itself. A command that documents a
  placeholder is unchanged for an ordinary invocation. Under `--scaffold` it is now bounded
  too: 43 classic and platform leaves used to accept and discard any number of extra
  positionals with that flag set, and now enforce the declared ceiling, so
  `<resource> update <id> extra --scaffold` is refused where it exited 0 before. `multi`
  still forwards every positional to its inner command, and 22 singleton `delete` and
  `history` commands had their `--help` examples corrected, because those examples showed
  an id the command never accepted. A further 17 `--help` example lines on 12 resources are
  corrected for the same reason on the other half of the line: a `create`, `update` or
  `patch` example opened with a `get` the resource does not ship, or one with a different
  arity, so half of a documented pipe could not run.
- **An error raised before the output format is resolved now follows `default-output`.** The
  `-o` default was `json`, so a flag-parse error and an argument refusal always rendered the
  JSON envelope on stdout — even where the profile pinned another format, and even on a
  terminal — while the same profile's `RunE` errors, raised after resolution, rendered plain
  text. Two answers to one question. The default is now empty, meaning unresolved, and both
  paths answer the same way. A profile pinning `default-output` to a non-json format
  therefore gets plain text on stderr for those two error classes where it previously got
  the envelope on stdout, **piped runs included**: a wrapper parsing that envelope must key
  on the plain-text form, or pass `-o json` explicitly. With no `default-output` set a piped
  run still gets the envelope, so an unconfigured CI job is unaffected.
- **A cobra usage error now exits 2, not 1.** Four classes move: a missing required flag, a
  flag group with no member set, a flag group with mutually exclusive members set together,
  and the wrong number of positional arguments. An unknown flag and an unknown subcommand
  already exited 2, so the two halves of one mistake answered differently. `pro backup
  --nosuchflag` exited 2 and `pro backup` with no `--output` exited 1. Exit 1 is the generic
  failure code, so a script could not tell a bad invocation from a failed request. The scope
  is wide: 48 call sites declare a required flag, 118 declare a flag group, and 671 validate
  an argument count. A wrapper that treats exit 1 as a bad invocation must key on 2 instead.
- **`pro ddm-reports declaration get` and `pro ddm-reports device get` are removed.** Both
  endpoints were deprecated upstream in favour of a sibling the CLI already shipped:
  `declaration devices <id> --filter …` and `device declarations <id> --filter …`. The
  successors declare `filter` required, so there is no unfiltered read left; use
  `--filter 'active=in=(true,false)'` where you want everything.
- **`pro comp erase` and `pro comp remove-mdm` send `/v4/computers-inventory/{id}/…`**,
  where they were pinned to `/v1/computer-inventory/{id}/…`. Neither has a version fallback,
  so an instance that does not serve v4 answers 404. Flags, targeting and confirmation are
  unchanged.
- **`jamf-cli config list` no longer has a `tenant-id` column** in `table`, `csv` and
  `plain` output; it always has `environment-id` and `default` instead. `-o json` and
  `-o yaml` are unchanged and still carry `tenant-id`. Parse the JSON, not the table.
- **Building from source needs Go 1.27** (`go.mod` declares `go 1.27.0`, up from `1.26.6`).
  The default `GOTOOLCHAIN=auto` fetches it; a pinned older toolchain fails. Binary releases
  are unaffected.

### Changed

- **An empty list prints `[]`, not `null`.** Applies to Pro's `list --all` and to every
  Platform and Security Cloud list. `jq` pipelines previously failed with "Cannot iterate
  over null" on exactly the tenants where a collection was empty.
- **`pro computers-inventory` sends `/v4` instead of `/v3`**, and `get` reads the v4 detail
  endpoint. Generated subcommands retry the `/v1` path on a 404 and warn on stderr.
- **Eight Classic patch-management commands are no longer refused on a gateway profile.**
  `pro classic-patch-reports` (both subcommands), `pro classic-patch-titles` `list`, `get`,
  `update`, `delete` and `apply`, and `pro classic-patch-policies list` were refused in
  v1.28.0 because a published Classic API build had withdrawn `/patches`, `/patchreports`,
  `/patchsoftwaretitles` and two `/patchpolicies` reads. Upstream restored all of them, on
  the stated reasoning that patch management is where Classic API callers are most
  concentrated. `pro classic-patch-titles` also gained `--scaffold` and `--set`, its body
  schema having come back with the endpoints.
  `/pro/v3/computers-inventory` was restored in the same build — 13 operations, deprecated
  too close to the removal for callers to reach v4 — which changes the coverage manifest and
  no command, since the CLI sends v4.
- **`pro computer-inventory-collection-settings custom-path` sends v2, whose `scope` accepts
  only `APP`.** v1 served `[APP, FONT, PLUGIN]`, so a `FONT` or `PLUGIN` path that worked
  before now answers a 400. Nothing in `--help` says so — the Pro generator renders no
  allowed-value lists.
- **A 403 names the permission in the vocabulary of the API that answered** — capability
  permissions with Jamf Account's own section and permission names for a gateway request,
  Jamf Pro API-role privilege names for an instance request. `commands -o json` carries both
  (`privileges`, `gatewayPrivileges`, `gatewayPermissions`) plus an `api` field naming the
  serving API.
- **`commands -o json`'s `gatewaySuccessor` reports a value**, where it was documented but
  emitted on no row at all: six refused commands now name the replacement the runtime
  refusal and the `--help` caveat already named. A script reading the catalog rather than
  running `--help` was told only that a command was `unserved`.
- **A CDN/WAF refusal is reported as one** rather than as `permission denied (HTTP 403)`
  with an HTML page in the message and a hint about API roles. Known triggers: `file://`
  anywhere in a request body, `.pkg` upload content, a burst of writes. A `.pkg` upload
  through a gateway profile is currently refused; upload through an instance profile.
- **`-n, --dry-run` is honoured on Platform and Security Cloud writes**, printing method,
  resolved path and body to stderr. Hand-written platform writes with no per-command
  preview are refused under `-n` rather than executed.
- **`-v` labels retried requests** with the attempt number and the wait, so a slow call is
  distinguishable from a retry sequence.

### Added

- **Classic writes gained `--scaffold` and `--set`**, plus required-field and enum lists in
  `--help`: 114 of 117 `create`/`update`/`apply` commands, across 44 of the 54 Classic
  resources. The three without them are `pro classic-computer-configs` `create`, `update`
  and `apply`: the resource is dead, and a Jamf Pro instance 404s it too.
  `--set` builds the whole body and is mutually exclusive with `--from-file`; it refuses an
  unknown field, an out-of-enum value and a credential field, because the Classic API
  answers `201` and silently drops or defaults the first two.
- **`commands -o json` carries a `scopes` array** for every generated Platform command: the
  Jamf Platform API scope levels its spec declares a credential must be created at
  (`environment`, or `environment,tenant`). Nothing is refused on it — the specs are
  currently stricter than the gateway, and a tenant credential still reaches
  `pro platform-devices list` and `pro platform-device-groups list` despite both being
  declared environment-only — so it is reported, appended to the gateway's own scope errors,
  and used to assemble `platform setup`'s summary. Absent means the spec is silent, not that
  any level works: the three Jamf Account APIs declare nothing and are organization-scoped.
- **`--file` accepts YAML** on generated Platform and Security Cloud commands, matching
  Pro's `--from-file`.
- **`protect backup` and `protect restore`** capture and replay a whole Jamf Protect tenant.
  Each object is written to its own file in the same portable, name-referenced form the
  matching `export` produces; restore walks the tree in dependency order, resolving
  references by name against the target, and never deletes. Both take `--resources` and
  `--exclude`. Backup prunes documents an earlier run left that no longer match the tenant
  (`--no-prune` keeps them), and refuses to prune a directory another tenant has written to.
  Two files carry secrets verbatim and are written `0600` — `action-configs` and
  `data-forwarding`, because an HTTP report client's bearer token cannot be redacted without
  breaking restore. Note git records no non-exec permissions, so a clone of a backup repo
  hands them back `0644`. A mixed result exits 7.
- **`protect analytics overrides`** manages the tenant overlay on Jamf-managed analytics
  (`tenantSeverity`, `tenantActions`) — `list`, `get`, `set`, `apply`, `export`, `clear`.
  `analytics list` reports Jamf's baseline severity rather than the effective one, and
  `analytics export` omits the overlay entirely, so this is the only route to the
  customisation a tenant actually made.
- **Jamf AI Governance:** `platform ai-policies`, `platform ai-tools`.
- **Jamf Account:** `platform account-licenses`, `deal-registrations`,
  `distributor-configuration`, `distributor-purchase-orders`, `distributor-quotes`,
  `sso-connections`, `sso-domains`. Organization scope, and US-only — a non-US profile is
  refused before sending.
- **Platform audit:** `platform audit` (environment scope only). Not `pro audit`, which runs
  health checks against a Jamf Pro instance.
- **Jamf Security Cloud through the gateway:** `security dns-*`, `ztna-*`,
  `content-categories`, `device-groups`, `uem-*`, `enrollment-activation-profiles`. Every
  `security` command's `Short` says which API serves it — platform gateway or Radar — since
  the two halves take different credentials.
- **App Installers on the gateway.** The endpoints are published upstream now, so the
  commands are generated from that spec rather than a reverse-engineered one and are no
  longer refused on a gateway profile. New: `pro app-installers get`,
  `app-installer-titles versions`, `app-installer-global-settings deployment-controls`,
  `history` and `add-history-note`.
- `pro ddm-reports declaration devices` and `device declarations` gained `--page`; only the
  first page was reachable before.

### Fixed

- A YAML request body carrying a timestamp scalar or a non-string mapping key — both legal
  YAML, neither expressible in JSON — was reported as malformed input.
- `pro platform-device-groups` name lookups built a stale `/tenant/{id}/` path, which
  collapsed to `/tenant//` under environment or organization scope.
- `JAMF_ENVIRONMENT_ID` now overrides a tenant-scoped profile rather than colliding with it,
  the way every other environment variable here overrides the profile.
- **`platform setup` no longer checks any product's access or permissions.** It used to
  read `content-categories` and report a Jamf Security Cloud entitlement verdict, which
  asked the wrong question: a capability permission is granted per operation when the
  integration is created, so one read says nothing about the other 28 resources, and a
  gateway 403 already names the permission it wanted in the wording Jamf Account's picker
  uses. The verdict was also wrong in the ordinary case. Wire-checked in one organization, a
  **tenant** credential answered `BAD_PERMISSIONS` there while an **environment** credential
  in the same organization answered 200 — so the summary told a demonstrably entitled
  organization it had no entitlement, and subtracted all sixteen Security Cloud resources
  from what the profile reaches. That credential's summary now reports 16 of the 29
  reachable, and closes by saying where a permissions answer comes from.

  Setup still validates the **scope ID**, because the token exchange sends no scope header:
  credentials that authenticate say nothing about the ID just typed, so a mis-pasted one
  saved cleanly and then refused every command. The gateway resolves the scope at the edge,
  before routing and before capability, so this is not a product check either — only
  `404 ENVIRONMENT_NOT_FOUND` and `403 OWNERSHIP_FORBIDDEN` reject an ID, and everything
  else, `BAD_PERMISSIONS` included, leaves it unjudged.
- **An unknown platform environment ID produced a bare 404 with no explanation.**
  `ENVIRONMENT_NOT_FOUND` is a 404, so it reached neither the 403 privilege hint nor the
  missing-scope note. A tenant ID pasted into `environment-id` is exactly this, and it is
  now annotated with the two IDs it confuses and the profile field to check.
- **`platform setup`'s closing summary said two things that were not true.** A tenant-scoped
  profile was told it served "the Pro API and Platform API commands" when six Platform specs
  declare environment scope only, and an organization-scoped profile was told AI Governance
  was served, which answers `400 REQUEST_CONTEXT_NOT_PROVIDED` with no scope header. The
  summary is now assembled from the commands' declared scope levels, so it cannot drift from
  the specs they were generated from.
- **`platform setup` called a refused environment ID a tenant ID.** The gateway's
  `OWNERSHIP_FORBIDDEN` is reachable at either level, and the refusal was worded tenant-only
  while the closing summary named the same value an environment ID — so setup told an
  operator to use the prompt they had just used. Both now read from the level that was
  supplied.
- **A withheld scope level on a `school` profile reported missing credentials.**
  `school blueprints` and `school ddm-reports` need a platform client, and the school
  resolver requires a tenant ID before building one — so a level withheld by the rule above
  left no client and the command answered "this command requires platform gateway auth",
  with the profile, the client ID and the secret all present. It now names the profile, the
  withheld level, and `JAMF_TENANT_ID` / `--tenant-id` — the one level that resolver can
  build. This is the one path that sends no request, so the note the other two ladders get
  from the gateway's 400 had nowhere to appear.
- **`pro classic-macos-config-profiles --scaffold` named the wrong element inside
  `scope.jss_user_groups`.** It rendered `<jss_user_group>` where the wire answers
  `<user_group>` — an upstream typo in the Classic spec, confined to that one property while
  the resource's own `scope.exclusions.jss_user_groups` and all seven sibling resources
  carrying the same scope block declared it correctly. Corrected upstream and ingested with
  SDK v0.22.1. Writes were unaffected either way: the Classic API accepts both spellings and
  reads the scope back as `<user_group>`.
- **`pro classic-mobile-config-profiles` writes gained `display_in` inside
  `self_service.self_service_categories`, and it is the field that decides whether the
  category is stored at all.** The Classic spec had this one resource `$ref` the shared
  `category` schema (`{id, name, priority}`), where its five siblings declare the item
  inline with `display_in`; corrected upstream and ingested with SDK v0.22.2. Wire law, on
  Jamf Pro 11.31.1: a `<category>` carrying only `<id>`, or `<id>` plus `<name>`, is
  **silently discarded with a 201**, and `display_in=false` is a deletion gesture rather
  than a stored value. So `--scaffold` renders `<display_in>false</display_in>` — the
  boolean placeholder — and that is the one value in the template that must be changed
  rather than merely filled in. `display_in` is also **write-only on this resource alone**:
  the GET echoes `<id>` and `<name>` only, so a scaffold round trip cannot recover it. Both
  facts are carried in the field's description in `specs/classic/schemas.json`.
