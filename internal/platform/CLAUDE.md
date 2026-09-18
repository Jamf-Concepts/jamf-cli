### Jamf Platform API Integration

Uses `jamfplatform-go-sdk` (REST, SDK manages tokens/retry/pagination). Platform commands live under `pro` and appear in "Platform:" help group — the exceptions are **AI Governance, Jamf Account and audit, which live under `platform`** because they are not Jamf Pro surfaces at all (see their sections below). A single `auth-method: platform` profile enables both Pro API (routed through gateway) and Platform API (via SDK).

Runtime-gated: commands are always registered but `RunE` starts with `requirePlatformClient(cliCtx)` and errors with setup instructions when platform auth not configured.

Name resolution via `internal/platform/Resolver` (blueprints by name, benchmarks by title, baselines by title, device groups by name). Devices use SDK filter methods directly (UUID vs serial auto-detected by hyphen presence).

CRUD pattern: `apply` (upsert, blueprint uses merge-patch; benchmark is create-only — SDK has no update), `get`, `delete`, `export`. `apply --scaffold` prints create request template (auth skipped for scaffold).

**Identifier convention differs from Protect.** Protect takes a positional `<name>`; Platform takes a positional **`<id>`** plus a `--name` flag (`delete <uuid>` or `delete --name "My Blueprint"` — passing a name positionally is sent as an ID and 404s). This holds for both the generated commands (template: `generator/platform/template.go`, gated on `SupportsNameLookup`) and the hand-written ones (`resolveBlueprintID` in `pro_blueprints.go`), which reject `<id>` and `--name` together. Exceptions: `blueprints clone <source-name> <new-name>` takes names positionally; `blueprints import-profile [<id>]` sniffs its positional (`isClassicID` in `pro_blueprints.go`) — an all-digit argument is a Classic ID, anything else a display name — to keep pre-#316 name-based invocations working. Consequence: a profile whose display name is all digits must be looked up with `--name`. Do not copy the sniffing into new commands without a comparable compat reason.

Naming: `platform-` prefix where overlap with existing Pro resources (`platform-devices`, `platform-device-groups`); no prefix for unique resources (`blueprints`, `compliance-benchmarks`, `ddm-reports`).

Commands: `blueprints` (`bp`) — CRUD, deploy/undeploy, clone, scope, components, import-profile (auto DDM conversion), report. `compliance-benchmarks` (`cb`) — benchmark CRUD (apply/get/list/clone/delete/export; create-only, no update). `baselines`/`rules` — read-only mSCP reference data (list). `benchmark-reports` — compliance reporting (`rules`/`devices`/`compliance-percentage`, keyed by benchmark ID). `platform-devices` (`pdev`), `platform-device-groups` (`pdg`), `ddm-reports` (`ddm`).

**`ddm-reports declaration get` and `device get` are gone as of the v1942 declaration-reporting spec.** Both were deprecated in favour of a sibling on the same resource that the CLI already shipped — `GET /v1/declarations/{id}` → `.../devices`, `GET /v1/devices/{id}` → `.../declarations` — so the capability survives under `declaration devices` and `device declarations`. The one difference that matters: **both successors declare `filter` required** where the deprecated pair did not, so there is no longer an unfiltered read. `ddmAllDeclarationsFilter` (`internal/commands/pro_ddm_reports.go`) is the tautology that stands in for one, `active=in=(true,false)`, and the hand-written commands (`pro device`, `pro report`, `pro audit`) were already using the filtered successors, so nothing hand-written broke. A CLI user needs `--filter 'active=in=(true,false)'`.

---

## GA Base URL

The GA base URL is `https://{region}.api.jamfcloud.com` — **the `/api` segment is gone.** The gateway mounts each namespace at the root: `/blueprints/v1/blueprints` serves, `/api/blueprints/v1/blueprints` answers `404 page not found`. The pre-GA host `{region}.apigw.jamf.com` is retired.

`refuseRetiredGatewayURL` inside `newPlatformSDKClient` refuses a profile still naming the retired host before any request. **Lives inside `newPlatformSDKClient`, not beside its callers** — it is the one constructor every platform path calls, so a guard on it cannot be forgotten by the next caller. `ResolveAuthForProfile` calls the same function rather than spelling the message twice.

Four things moved together when the URL changed:
- `serviceSegment` (`generator/parser/platform.go`) now reads the namespace off `servers[0].url`'s **path** instead of cutting on `/api/`. That marker does not appear in `{region}.api.jamfcloud.com` — the host's `api` is dot-delimited — so the old implementation returned `""` for every v1807 spec, silently dropping the namespace from every generated path.
- `normalisePlatformPaths` prefixes `/{service}` rather than `/api/{service}`.
- `rewritePathForGateway` (`internal/client/client.go`) maps `/api/v1/x` → `/pro/v1/x` and `/JSSResource/x` → `/proclassic/x`.
- `pdgListPath` (`internal/commands/pro_platform_device_groups.go`), the one hand-written platform path builder.

`TestServiceSegment` pins both URL shapes, because one spec drop legitimately mixes them: Platform specs are v1807 and carry no `/api`, while Security Cloud ones still declare it.

## Tenant-Path to Header Migration

Every Jamf gateway path used to embed the scope — `/api/{namespace}/{version}/tenant/{tenantId}/{resource}`. Prod gained `header` as an allowed source on 2026-08-25 and the published specs dropped the segment in favour of `X-Tenant-Id`/`X-Environment-Id`. This repo follows the SDK onto headers only — a second code path nothing exercises is how the previous URL-shape bug survived weeks.

Three copies of that rule exist and all moved together:
- `normalisePlatformPaths` strips `/tenant/{tenantId}` from every platform spec path.
- `rewritePathForGateway` maps instance paths onto their gateway namespace.
- `setScopeHeader` stamps the header on both the request and the multipart-upload path.

`TestLoadResourcesDropsTheTenantFromEveryPath` and `TestGatewayTenantTravelsInHeader` fail if a tenant segment reappears.

**`pdgListPath` was building the path from `TenantID()` after the scope moved into a header**, and nothing failed during the transition window. It only broke for the scope Jamf wants integrations to use — `TenantID()` answers `""` for environment scope and for organization scope, so the path collapsed to `/tenant//device-groups`, which the gateway answers **301** rather than anything actionable. Prefer `Client.Scope() (ScopeKind, string)` (SDK v0.18.0) over `TenantID()` — an accessor for one kind cannot express a three-valued property.

## x-scope-types and the Scope Annotation

**Build v2082 declared a scope level for every Platform spec.** Six specs went tenant → environment only (`X-Environment-Id` required, `X-Tenant-Id` deleted): blueprints, device-groups, devices, device-management-action, declaration-reporting, compliance-benchmarks. jpapi, capi and all six Security Cloud specs went tenant → tenant and environment. Three Jamf Account specs declare nothing (organization-scoped).

**The gateway has not followed the withdrawal, so nothing in this CLI refuses on it.** What is done with `x-scope-types` instead:
- **`commands -o json` catalog carries it** as `scopes` — the one requirement a 403 cannot teach.
- **`AnnotateScopeLevelError`** (`internal/commands/platform_scope.go`) appends declared levels and the level in use to `REQUEST_CONTEXT_NOT_PROVIDED`, only when the credential is not already at a declared level. Says "declares", never "requires". `OWNERSHIP_FORBIDDEN` is handled by `scopeMismatchHint` instead (names declared levels); `INVALID_REQUEST_CONTEXT_TYPE` gets nothing (the gateway already names both levels).
- **`platform setup`'s closing summary** (`printScopeSummary`) partitions resource groups by declared levels — cannot claim a surface the specs say the level cannot reach. An earlier hand-written sentence had gone stale once.

`AnnotateScopeLevelError` is recorded by `newPlatformSDKClient` into a package var — the one constructor every platform path calls. The withheld-scope note path is covered there too: `platform.ErrNoPlatformClient` is the sentinel when a school client gate withholds the level and sends no request, annotated with the withheld note rather than a generic "requires platform auth" message.

## gzip Bug — `identityEncodingOnWrites`

The gateway drops the `Location` header and nulls `href` whenever the response is **gzipped**. Go's `net/http` sends `Accept-Encoding: gzip` on every request. `identityEncodingOnWrites` (`internal/commands/pro_platform_helpers.go`) sets the header explicitly on POST/PUT/PATCH, opting out of Go's transparent gzip. Mutations' responses are a handful of bytes; reads keep gzip. **Do not remove this transport** — `href` returns null the moment it goes.

## Version Deduplication on Gateway Paths

`stripVersionPrefix` only strips a *leading* version (`/v1/computers-inventory`). Gateway paths have the version after the service namespace (`/securitycloud/v1/groups`), so the two versions hashed to different keys and dedup silently did not fire. `stripVersionSegments` and `apiVersionRank` now read the version wherever it sits.

`FallbackPaths` is populated for platform GETs and **deliberately ignored** by `generator/platform/template.go`. Pro retries a displaced older path on 404 because customers run older Jamf Pro versions; the gateway is a single deployment with no such version skew. An unrouted path answers 403 `BAD_PERMISSIONS`, which is indistinguishable from a real privilege failure — falling back on that would turn a permission problem into a silent downgrade.

## Platform-Specific Generator Knobs

All are in `generator/platform/emitter.go` or `generator/parser/platform.go`:

| Knob | Purpose |
|---|---|
| `platformResourceNameOverrides` | Rename a generated resource; keys tried `{namespace}/{name}`, then `{service}/{name}`, then bare `{name}` |
| `platformOperationNameOverrides` | Rename a generated operation; keyed `{METHOD} {full path}`; applied **last** (after disambiguation passes) |
| `platformIgnoredRequiredParams` | Stop enforcing a query param the spec marks required but the server ignores |
| `platformUnroutedOps` | Withhold an operation the spec declares but the gateway does not route; **currently empty**; needs a recorded probe AND a working operation it would displace |
| `platformNoNameLookup` | Suppress `--name` flag on a specific operation; keyed `{METHOD} {path}` |
| `platformNameLookupFields` | Point `--name` at a property other than name/title/displayName; keyed `{namespace}/{name}` |
| `platformTableColumns` | Columns a platform list renders in table/CSV; keyed `{namespace}/{name}` via `namespaceFromPath` — the **whole** namespace (multi-segment); a bare resource name is not unique across services |
| `platformDocumentedStatusResults` | Let a documented non-2xx (e.g. singleton's "not configured" 404) render instead of exit-code error; matched on status AND error code |
| `platformNoApply` | Blocklist resources with existing hand-written `apply`; currently `blueprints`, `platform-device-groups` |
| `platformPatchDoesNotMerge` | Override where server contradicts its own PATCH method; currently `ai-policies` |

`platformTableColumns` was keyed on `serviceFromPath` (first path segment only). A three-segment namespace (`ai/governance/policies`) produced `ai/ai-policies`, matched no entry, and emitted a list with no columns silently. `namespaceFromPath` now returns the whole namespace. `TestPlatformTableColumnKeys` fails on any key that matches no live list op.

**`platformOperationNameOverrides` applied before disambiguation passes let a pass silently undo it.** Audit's `list` override was discarded by `resolveNoParamConflicts`, shipping the stutter `platform audit audit`. Overrides are now applied last. `TestPlatformOperationNameOverridesWinOverDerivation` asserts the resulting name.

## Namespace Collision — `platformNamespace`

Two specs may tag a resource with the same name. `platformResourceNameOverrides` now tried `{namespace}/{name}` first (namespace = everything before the version segment of the resource's own paths). `platformNamespace` (`generator/parser/platform.go`) is the parser-side twin of the emitter's `namespaceFromPath`. `TestTwoSpecsSharingATagGetDistinctResourceNames` asserts resulting names and paths; fails when an override value stops naming a shipped resource.

## HTTP Client Contract

**Never hand the platform SDK a retry client.** `jamfplatform.WithHTTPClient` assigns whatever is given to the SDK's `retry.HTTPClient`, so an injected retryablehttp client becomes an inner retry loop whose policy wins. Pass a plain `*http.Client` carrying only the timeout, jar and the verbose/spinner transports.

`platformVerboseTransport` wraps the `http.RoundTripper` **inside** retryablehttp's loop, so `-v` shows every attempt. It counts consecutive identical requests and renders `(retry 2, waited 4.3s)`. Two guards: a repeat is only a retry when the previous attempt failed (a poll or `--name` lookup reissuing the same collection is not a retry); the rendered wait is wall-clock since the previous attempt.

## App Installers

Endpoints sit under `hiddenapi/` in `jamf/jss`, so no published spec described them until `public-apis-oas#430` published 24 operations into `pro_api.json` (GitOps v2043, 2026-09-03). `#451` withdrew `POST /v1/app-installers/titles/{id}/cache-update` 80 minutes later.

Specs come from `pro_api.json` via `monolith.ExtractSubtree` (`generator/monolith/subtree.go`). Route table is `AppInstallerSpecs` in `generator/monolith/overrides.go`. All four files stay in `PreservedSpecs` so the monolith splitter cannot delete them. Refresh with `make sync-platform-specs-from-sdk`.

**It is deliberately not `Split`.** `Split` owns all of `specs/`, wipes every root `*.yaml`, and would regenerate all 164 specs from the gateway's version-filtered view, deleting commands for withdrawn operations.

Three transforms turn a gateway-published operation into a Pro-direct one:
- Header parameters dropped (gateway declares `X-Tenant-Id` on every operation; parser turns any declared parameter into a flag)
- `x-required-privileges-legacy` promoted over `x-required-privileges` (Pro field speaks API-role prose; gateway key is capability vocabulary)
- `x-action` stamped on any operation deeper than its collection path whose terminal segment is a literal

`resourceNameFieldOverrides` entry needed for `app-installer-titles --name` — `titleName` is `readOnly` in the published spec, so `detectNameField` skips it.

Wire facts (SDK v0.20.1, 2026-09-03):
- `HrefResponse` returns the Jamf Pro **instance** hostname with `/api` prefix — neither of which exists on the GA gateway. Take the `id`; never dereference the href.
- Both history-note POSTs answer 200 where the spec declares only 201; `x-jamf-expected-status: 200` corrects it.
- An omitted `smartGroupId` reads back as `"-1"`, never `""`.
- Both installation retries answer 404 with an empty `errors` array when there is nothing to retry; `gatewayUnservedNote` stays silent (gated on `BAD_PERMISSIONS` or `404 page not found`).

## AI Governance

Wired under **`platform`, not `pro`** — `platform ai-policies` (`aip`) and `platform ai-tools` (`ait`). AI Governance is not a Jamf Pro surface: scoped at organization or platform-environment level, credential naming no Jamf Pro tenant need exist.

`platformResourceNameOverrides` keys are the full service: `ai/governance/policies/policies` → `ai-policies`, `.../tools` → `ai-tools`.

`platformOperationNameOverrides` for four ops: `versions`/`version`, `deployment`, `schema` — needed because two GETs with a path param both derive `get`.

**`patch` replaces `settings` wholesale** despite sending `application/merge-patch+json`. `settings` and `schemaVersion` are both required, so there is no partial-edit request; `--set settings.x=y` silently discards every other setting. Read with `get`, edit the whole `settings` object, send with `--from-file`. `apply` inherits this; `platformPatchDoesNotMerge` overrides its help text.

Wire facts (2026-08-31, EU sandbox, environment-scoped credential):
- `create` and `patch` write a draft (`hasDraft: true`); `publish` turns draft into immutable version, takes no request body
- `delete` is an archive — `get` still answers 200 afterwards, only a write reveals deletion
- Error envelope is `{traceId, errors}` with no `httpStatus` — unlike gateway's `{httpStatus, traceId, errors}`
- `GET /tools/{toolId}/schemas/{schemaVersion}` for unknown version answers 422 `SCHEMA_VERSION_UNKNOWN`, spec declares only 404

## Jamf Account and Audit

Twenty-two generated commands from four specs, wired under **`platform`**: `account-licenses`, `deal-registrations`, distributor operations, `sso-connections`, `sso-domains`, `audit`.

**The account trio is US-only.** `requireUSGateway` refuses a non-US profile before sending. EU and APAC answer bare `404 page not found` for all three — Tyk's unrouted response, no `traceId`, reads as a wrong path rather than a wrong region.

**`platform audit` requires environment scope.** `X-Tenant-Id` is refused with `400 INVALID_REQUEST_CONTEXT_TYPE`. The guard `AnnotateScopeLevelError` covers the missing-scope 400; `ENVIRONMENT_NOT_FOUND` (a 404) is annotated before the level arms.

**The audit `--page-size` was silently missing.** `buildQueryParams` filtered `page`/`page-size` unconditionally assuming the auto-pagination loop owns them. Cursor pagers (audit uses `page-size` + `cursor`) have no loop, so the flag disappeared. The filter is now conditioned on the op actually paginating. `TestLivePagingFlagsAreNotSilentlyDropped` walks live specs.

**Enum fields in account specs were invisible.** Every enum in the three account specs is authored as `allOf: [{$ref: SomeEnum}]`. SSO's `connection` is a bare `oneOf` over four provider variants each being `allOf[BaseConnectionSettings, {…}]`. `parseSchemaDepth` now:
- Adopts `allOf` items' type and enum when the property declares none of its own
- Shares `composedPropSources` and `composedRequired` between the top-level walk and the union path
- Gives a property whose own schema is a bare union the same first-variant-plus-unioned-enums treatment

**The 18 account privileges are stripped by the SDK's build.** The published `api/account_*.json` carry no `x-required-privileges`. Do not hand-supply the names — a local table shadows the real values the day upstream decouples the two switches.

`annotateDistributorScopeError` (`internal/commands/platform_account.go`) appends an explanation for the Skyway distributor service fault: matches `invalid_scope` + `skyway` (old form) or `skyway distributor service` (new form), case-insensitively, never on status alone. Never names a scope (current form carries none). Re-probe at every ingest — the wording changed once silently.

## Commands Under `pro` vs `platform`

Platform API commands live under `pro` **except**:
- AI Governance → `platform ai-policies`, `platform ai-tools`
- Jamf Account (licensing/partners/SSO) → `platform account-*`, `platform deal-*`, `platform distributor-*`, `platform sso-*`
- Platform audit → `platform audit`

These three surfaces are not Jamf Pro surfaces at all; a `pro ai-policies` would imply a Jamf Pro instance that need not exist.

## `--name` Resolution Details

`platform.ResolveIDByName` filters the collection by name. Gaps:
- **Default Group** in Security Cloud device-groups returns `name` but no `id`. `collectMatches`/`resolveIDByName` report "the list returns no ID for the items it matched" rather than "not found" — distinguishing a server-side gap from a typo.
- **Predefined-derived ZTNA apps** return `name: null`. These apps are invisible to the match; the error reads "not found" indistinguishable from a typo. Use the ID.
- **SSO domain `id`** was a bare integer, not a string. `idString` now handles `string`, `json.Number`, and `float64`. Keep the numeric branches — the server sends quoted strings as of 2026-09-01 but the branches are what made the fix inert.

## Dry Run on Platform Commands

The Platform SDK client cannot be wrapped like `dryRunClient` (transport asserts an exact success status; a synthetic response would have to guess 200 vs 201 vs 204 per operation). Both generators emit a `cliCtx.DryRun` check on every non-GET operation. `dryRunGuardTransport` refuses hand-written platform command writes with 412 carrying `DRY_RUN` (a transport *error* would be retried).

**The preview comes before the confirmation.** `ConfirmAction` errors when `--yes` is absent and stdin is not a terminal, so `--no-input -n delete <id>` used to report "requires --yes" and preview nothing. Fix: template emits the dry-run check before `ConfirmAction`. Name→ID resolution stays ahead of both; validations also stay ahead.

## Groups and Aliases for the `platform` Namespace

Aliases live in `platformAliases` (`internal/commands/aliases.go`) and groups in
`platformGroups` (`groups.go`): `groupPlatformAI`, `groupPlatformAccount`
("Jamf Account (US-only):" — the group title carries the constraint so
`platform --help` says it without opening a subcommand) and
`groupPlatformAudit`. `applyAccountUSOnly`
(`internal/commands/platform_account.go`) appends the constraint to every
account leaf's `Long` and wraps every leaf's `RunE`; the guard passes through a
**nil** platform client rather than inventing a region complaint, because
`--scaffold` runs without auth and `platform.RequirePlatformClient` already has
a clearer error waiting.

Short aliases: `lic`, `deals`, `dcfg`, `dpo`, `dq`, `ssoc`, `ssod`, `aip`,
`ait`.

## AI Governance Wire Facts

Probed 2026-08-31, EU sandbox, environment-scoped credential. The whole surface
is exercised through the CLI, writes included.

- **`patch` replaces `settings` wholesale — it does not merge.** The method is
  PATCH and the CLI sends `application/merge-patch+json`, but the server treats
  `settings` as a full replacement: seed `{permissions:…, env:…}`, patch with
  `{permissions:…}` alone, and `env` is gone. `name` and `description` *are*
  leave-unchanged-if-omitted, so the two halves of one request body behave
  differently. Both `schemaVersion` and `settings` are required, so there is no
  partial-edit request to build in the first place — **`--set settings.x=y`
  silently discards every other setting.** Read with `get`, edit the whole
  `settings` object, send it with `--from-file`. `apply` inherits this, which is
  what `platformPatchDoesNotMerge` (`generator/platform/emitter.go`) is for.
- **A draft is separate from a published version.** `create` and `patch` write a
  draft (`hasDraft: true`, `currentVersionNumber: null` on a fresh policy);
  `publish` turns the draft into an immutable version and takes **no request
  body**. Publishing with nothing pending answers `409 NO_DRAFT_TO_PUBLISH`, so
  `publish` is not idempotent.
- **Whether an unknown `settings` key is rejected is the vendor schema's
  decision, not the API's.** `com.anthropic.claudecode` declares
  `additionalProperties: true` and stores a bogus key silently;
  `com.anthropic.claudefordesktop` omits the keyword (same effect);
  `com.openai.codex` sets `false` and answers `422 SCHEMA_VALIDATION_FAILED`. So
  a typo in `--set settings.…` is caught for some tools and silently persisted
  for others — read `platform ai-tools schema <toolId> <schemaVersion>` to know
  which. A *declared* field with a bad value is always caught, and the error
  names the enum.
- **Pagination and sort are honoured** — genuinely unusual on this gateway.
  `page` is 0-based, `totalCount` is the unfiltered total, and `sort` is
  `property:(asc|desc)` over `createdAt`/`updatedAt`/`name`, with anything else
  a `400 VALIDATION_FAILED`. The generated `list` auto-paginates at 100.
- **`schema-drift` is a flag, not a boolean filter.** `schema-drift=true`
  narrows to drifted policies; `false` — and `maybe`, which is not rejected —
  returns everything. The generated `--schema-drift` only sends the parameter
  when true, which is correct by accident rather than design: keep it that way.
- **`delete` is an archive and is not idempotent.** 204, then the policy is
  invisible to `get` and `list` alike, and a second `delete` answers
  `404 POLICY_NOT_FOUND`.
- **`href` on `create` and `publish` is populated by the service**, not by the
  gateway's href-injection plugin, so the gzip bug does not apply here. Neither
  call returns a `Location` header.
- **The error envelope is the service's own**, and v2121 finally declared all of
  it: `httpStatus` is now required on `ApiError` and the SDK's probes confirm the
  wire has always sent it, so this was a spec gap rather than a second envelope
  shape. Codes worth knowing: `TOOL_ID_UNKNOWN`, `SCHEMA_VERSION_UNKNOWN`,
  `SCHEMA_VALIDATION_FAILED`, `POLICY_NOT_FOUND`, `NO_DRAFT_TO_PUBLISH`.
- `GET /tools/{toolId}/schemas/{schemaVersion}` for an unknown version answers
  **422 `SCHEMA_VERSION_UNKNOWN`**, declared at v2121. A non-UUID `policyId` is
  a 404, not a 400.
- **`ai-tools schema` returns ~250 KB of JSON Schema** for
  `com.anthropic.claudecode`. There is no table shape for it and none is
  offered.
- **v2121 added an ETag/If-Match concurrency protocol, and this CLI can reach
  only the read half.** `PolicyDetail` gains a nullable `version` counter, the
  detail `GET` returns it as a strong `ETag`, and `PATCH` takes an `If-Match`
  precondition answering **409 `POLICY_VERSION_CONFLICT`**. Every part is
  enforced on the wire: the server compares the *number* and tolerates bare `3`,
  strong `"3"` and weak `W/"3"` alike, `*` and an absent header are
  unconditional, and `garbage` is a 400 — so the header is parsed, not ignored.
  **Every `PATCH` increments `version` whether or not it changes anything**, and
  `version` is independent of `currentVersionNumber`, which only `publish`
  moves. `version: null` is not an unpopulated field but a **legacy document
  with no ETag at all**, so those can never be updated conditionally by anyone.
  The platform emitter renders **query parameters only**, so a declared
  `in: header` parameter produces no flag and no error: `patch` and `apply` send
  no `If-Match` and are therefore always unconditional. That is the one real
  capability gap this ingest opens.
- **v2121 declared a `409 POLICY_IN_USE` on `delete`, and it is unverified — the
  last probe says the opposite.** The 2026-08-30 full-surface probe found
  archiving a blueprint-referenced policy answering a clean **204** and leaving
  the blueprint pointing at a policy nothing can read. So the help text is now
  ahead of the wire in the *safe* direction; treat the guard as a declaration
  and not a fact.
- **`x-preview` is per operation as of SDK v1.1.0, and it is read now.** v2192
  declares it on all twelve operations, adds `Preview - ` to every summary and
  opens every description with two markdown banners naming **2027-03-03** as the
  expected GA date. Key on the catalog's `preview` field, the prose being
  upstream's to reword. The two banners also broke `firstParagraph` — see the
  `Long` rendering note.
- **`ai/governance/visibility` is routed** (403 `BAD_PERMISSIONS` on a credential
  holding the policies grants) and still has no published spec — the tell that
  `ai/governance` is the product and `policies`/`visibility` are capabilities
  beneath it. The hyphenated `ai-governance/policies` remains a bare
  `404 page not found`.

## Jamf Account Wire Facts

- **Three error envelopes, none of them the gateway's.** The account services
  answer `{classification, fields, message}` — not the gateway's
  `{httpStatus, traceId, errors}`, nor AI Governance's `{traceId, errors}`.
- **The account list endpoints served a bare JSON array until 2026-09-01 and the
  declared `{results, totalCount}` envelope after.** The CLI needed no change
  either way — the generated unwrap is `if obj, ok := result.(map[string]any)`
  over a `result any`, so an array falls through and an envelope unwraps — but
  **that tolerance is now the load-bearing part rather than luck**: the SDK,
  which had hardcoded the array shape, failed *every* call in production for the
  days between. So a generated platform list must keep decoding into `any`
  rather than a declared struct.
- **A spec-required query param is enforced client-side by `MarkFlagRequired`,
  not by the emitted request**, and the two differ for an explicit empty value:
  the template emits every query param behind an `if v != ""` guard, so
  `--since ""` passes cobra's was-it-set check and then sends a request with
  `since` missing. The server's answer to *absent* is worse than to *empty*
  (`400 BAD_REQUEST "Required parameter 'since' is not present."` with no
  `errors[]`, versus a field-attributed `400 INVALID_FIELD`). Worth knowing
  before adding a required param whose zero value is meaningful — and note the
  SDK's carve-out: a `required: true` param carrying a substantive schema
  `default` keeps its guard, because for Pro's `columns-to-export` sending it
  empty answers 500 while omitting it answers 400.
- `licensing/v1/licenses` returned 16 licences on the probe org. The
  **activation code is deliberately absent** from `platformTableColumns`: it is
  the value that entitles an installation, so it belongs in a JSON read someone
  asked for rather than in the output of a bare `list`.
- SSO domains carry statuses `VERIFIED` and `MANUALLY_VERIFIED`; `DELETE` and
  `actions/verify` on a bogus `domainId` answer a clean 404, so those paths are
  exercised without touching real identity config. `id` and `verifiedTldId` are
  the quoted strings the spec always declared, as of 2026-09-01.
- **`Region` carries `RAMP`, and it is an SDK patch rather than an upstream
  declaration.** Upstream's `Region` enum omits it while the wire both returns
  and accepts it. The SDK adds it through an `enumAdditions` key that panics
  once the spec declares the value, so the patch self-expires. Two adjacent
  tells: `connection: null` and `connection: {}` both answer
  `400 "A connection requires a region"`, and region validation runs ahead of
  the upstream call.
- **Tokens are not portable across regions.** The same client credentials answer
  `401 invalid_client` at `eu.api.jamfcloud.com/auth/token`. (The "portable
  between them" note is about the retired `apigw` host versus the GA host, not
  about regions.)
- **A fifth account namespace, `agreements`, is routed in prod and has no
  published spec.** `/agreements/v1/...` answers `403 BAD_PERMISSIONS` with a
  proper envelope — routed, ungranted. Nothing to ingest until a spec exists.
- **`audit` pages by cursor**, so `--all` does not exist for it: it declares
  `page-size` + `cursor`, not `page` + `page-size`. Two required-parameter
  rules, not one: `since` is required *and* at least one of `actor`,
  `audit-source`, `audit-type`, `resource-id` — omitting the second answers
  `400 MISSING_REQUIRED_FILTER`, which the spec states in the operation's
  description so the generated `Long` renders it even though `MarkFlagRequired`
  cannot enforce it. An event carries
  `{actor{id,kind}, auditId, auditSource, auditType, environmentId, orgId,
  requestContext{method,path,remoteIp,userAgent,…}, resourceId, tenantId, time,
  txId}`. **Ownership is checked before capability**, so a foreign
  `X-Environment-Id` masks whether the credential holds the grant, and a 502 is
  evidence of a grant rather than of a missing one.
- **`benchmarks create --scaffold` renders a usable `odv` again.** Upstream
  restored `OsSpecificRuleInfo` and `OdvRecommendation` in v1671, so the scaffold
  emits `"odv": {"value": "365"}` where it previously emitted the uninstructive
  `"odv": null`. The same ingest adds `VISION_OS` to
  `selectedOsVersions[].osType` and `BOOLEAN` to the ODV type.
