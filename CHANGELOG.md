# Changelog

Per-release notes — the full list of merged pull requests — are generated on
[GitHub Releases](https://github.com/Jamf-Concepts/jamf-cli/releases). This file exists for
what that list cannot say: which changes are **breaking**, what the migration is, and why.
So it records breaking changes, behaviour changes visible to a script, and removals. A
release with none of those gets no entry here.

Versions follow the `vMAJOR.MINOR.PATCH` tags in this repository, and headings match the
commit types the repo already uses (`feat!`/`build!` for a breaking change).

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
  and `settings`; `pro computers-inventory` loses `lock`, `restart`, `shutdown`,
  `enable-remote-desktop`, `disable-remote-desktop`, `set-recovery-lock`,
  `set-auto-admin-password` and `settings`. The gateway's published API declares GET on
  those paths but not POST, so the refusal is per method rather than per resource.
  `pro comp erase` and `pro comp remove-mdm` are hand-written and are **not** affected.
  The other 43 are `pro api-integrations` (7), `pro classic-computer-configs` (7),
  `pro api-roles` (6), `pro authentications` (6),
  `pro static-computer-groups` (6, use `pro computer-groups-static-groups`),
  `pro api-roles-privileges` (2), `pro policy-properties` (2), `pro systems` (2),
  and one each of `pro database-connections`, `pro environment-type`,
  `pro mac-os-managed-software-updates`, `pro mdm-commands commands` and
  `pro oauth-token-sessions`.
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
  output formatter, which receives none of the global flags, so the flag was accepted and
  then ignored: `--out-file` exited 0 and left the file at 0 bytes while the payload went to
  stdout. Two script-visible consequences. A job passing `--out-file` and reading **stdout**
  now reads nothing, because the payload goes to the file it asked for. And a job passing
  `--select` or `--compact` and parsing whole rows now receives narrowed rows. Affected are
  the `pro report` family, `pro audit`, `pro overview`, `protect overview`,
  `school overview`, `pro group-tools`, `pro classic app-usage`, `multi` and `commands`.
  `--select` and `--compact` narrow rows; they do **not** remove them, so a projection that
  matches no field in a row leaves that row present and empty, as it always has on the 200+
  generated commands. Three consequences worth knowing. Under `table` and `csv` the column
  set is the **union across rows** while either flag is active, because both make rows
  heterogeneous — a field only some rows carry used to be a column for none of them;
  without a projector the first row decides, as before. `plain` takes the same union, so
  every line carries the same field count. Under `table`, `csv`, `plain` and the
  single-object detail view, a projection that matches nothing in any row now renders
  **nothing**, where it used to print a row count above a blank header; the miss is named
  on stderr, and neither `--quiet` nor `--no-hints` silences it, because under those
  formats it is the only thing separating a mistyped field from an empty collection. The
  large-result hint is withheld while either flag is active, because it recommended more
  of the flag that had just emptied the output. And `--select` bypasses the table's
  default-column heuristic, so a named field is shown without needing `--wide`.
- **`pro audit -o raw --out-file f` and `-o xml --out-file f` now write a table, not JSON.**
  That command marshalled its rows and handed the bytes to `PrintRaw`, which passes JSON
  through unchanged for exactly those two formats, and the branch was reached only when
  `--out-file` was set. The shared formatter's own dispatch has no case for either format
  and renders a table. Use `-o json` for JSON. Without `--out-file` the output is
  byte-identical, and so is every other format. The six `pro report` multi-section reports
  already rendered `raw` and `xml` as tables, so nothing about their rendering moved — what
  changed for them is that `--out-file` receives it.
- **A section banner is no longer written for a format a parser reads.** `json`, `yaml`,
  `ndjson`, `csv` and `plain` get no `── Section ──` lines; `xml` and `raw` keep theirs,
  because they render as tables. A multi-section report still emits **one block per
  section** under `csv`, `ndjson` and `plain`, and now with nothing between them, so use
  `-o json` or `-o yaml` for a single parseable file. Every affected command says so in
  its `--help`. Under `json` and `yaml`,
  `pro report patch-status --scan-failures` now writes **one** array of labelled
  sections where it wrote three separate documents into the same file, and each section
  carries a `fetch_error` field: an empty section and a section whose fetch failed were
  byte-identical before, so a scheduled job read "no failures" from a check that never
  ran. An empty section is `[]`, never `null`.
- **`--field` naming a field no row carries reports that on stderr**, and `--quiet` and
  `--no-hints` do not silence it: a `--field` miss produces no output at all, so the note
  is the only signal distinguishing a wrong field name from an empty result.
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
