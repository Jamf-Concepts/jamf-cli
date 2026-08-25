# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Read first

- `docs/GLOSSARY.md` — canonical terms for Pro vs Platform vs Classic, blueprint vs config profile, smart vs static groups, scope vs target, etc. Consult before guessing.
- `docs/solutions/` — categorized postmortems and design-pattern docs (e.g., `conventions/output-flag-matrix-2026-05-08.md`, `design-patterns/cobra-annotations-as-policy-2026-05-11.md`). When starting work in a package, grep `docs/solutions/` for matching `module:` or `tags:` frontmatter.

## CRITICAL: Credential Input Policy

**Never accept credentials (passwords, tokens, client secrets) via CLI flags or stdin.** This prevents exposure in shell history, `ps` output, and CI/CD logs.

- **Human credentials** (username, password): Interactive prompts only (`term.ReadPassword`). No flags, no env vars, no stdin.
- **Machine credentials** (token, client-id, client-secret): Environment variables (`JAMF_*`, `JAMFPROTECT_*`, `JAMFSCHOOL_*`, `JAMFSECURITY_*`) for CI/CD. Interactive prompts for manual use. Config profiles with `keychain:` references for persistent storage. `--token-file` for file-based CI/CD.
- **Never add** `--password`, `--token`, `--client-secret`, `--token-stdin`, or `--client-secret-stdin` flags to any command.
- **Setup commands** (`pro setup`, `protect setup`, `school setup`, `security setup`, `config add-profile`) must always prompt interactively for credentials — no flag or env var bypass.

## CRITICAL: Generated Code Boundary

**Never edit files in `internal/commands/pro/generated/`** — they are overwritten by `make generate`.

**Never edit files in `internal/commands/platform/generated/`** — they are overwritten by `make generate`.

**Never edit files in `internal/commands/security/generated/`** — they are overwritten by `make generate`.

To change generated command behavior, edit the **generator templates**:
- **Modern API commands:** `generator/parser/generator.go` → `resourceTemplate` const
- **Classic API commands:** `generator/classic/generator.go` → `classicResourceTemplate` const
- **Modern registry:** `generator/parser/generator.go` → `registryTemplate` const
- **Classic registry:** `generator/classic/generator.go` → `classicRegistryTemplate` const
- **Jamf Security Cloud commands:** `generator/security/template.go` → `resourceTemplate` const

Templates are Go `const` strings embedded in the generator source — NOT separate `.tmpl` files.

After modifying a template: `make generate && make test`

## Platform Command Contract

Generated platform commands (`internal/commands/platform/generated/`) own **all** CRUD and action operations (list, get, create, update, delete, deploy, undeploy, report, etc.). Hand-written platform commands own **business logic only**: upsert (`apply`), portable export (`export`), profile conversion (`import-profile`), clone, dual-identifier lookup, and operations that orchestrate multiple API calls.

**Rule:** if a new Platform API endpoint maps cleanly to a single HTTP call, it should be a generated command, not hand-written. Wire generated subcommands under the hand-written parent `*cobra.Command`:

```go
for _, sub := range platformgen.NewBlueprintsCmd(cliCtx).Commands() {
    if sub.Name() == "create" { // skip if hand-written apply replaces it
        continue
    }
    cmd.AddCommand(sub)
}
```

CI enforces that `specs/platform/` and `internal/commands/platform/generated/` stay in sync: `make verify-platform-specs`. Run `make sync-platform-specs && make generate` after any spec change.

**Platform specs come from `jamfplatform-go-sdk`'s published `api/` directory.** That is the only place they are normalised and wire-verified against a live tenant, so any other source drifts. Refresh with:

```bash
make sync-platform-specs-from-sdk JAMFPLATFORM_SDK_PATH=/path/to/jamfplatform-go-sdk
```

`specs/platform/` filenames match the SDK's `api/` filenames exactly, so the copy needs no mapping to keep in step. `PLATFORM_SDK_SPECS` in the Makefile is the authoritative set — `sync-platform-specs` copies exactly those and ignores anything else in the drop directory. It is a list rather than a wildcard for two reasons: the SDK's `api/` also holds `pro_api.json`, the Classic documentation and the app-installer specs, which are Jamf Pro APIs generated from this repo's own `specs/*.yaml` and would emit bogus platform commands from Pro paths; and `specs/.platform-source/` is gitignored, so a wildcard made `make verify-platform-specs` depend on whatever a developer last left in a directory that differs per working tree. Adding a platform resource therefore means adding its spec to that list.

This is not housekeeping. A stale copy of the compliance-benchmark spec left `pro rules list` sending `baselineId` after the API renamed it to `baseline-id`; the server ignored the unknown key and answered **0 rules for every baseline**, with a required flag and no error. The test covering it passed the whole time, because it asserted the generator's flag→query plumbing against whatever the stale spec declared rather than the wire contract.

## Where to Make Changes

| I want to... | Edit this file |
|---|---|
| Surface required privileges (annotation, catalog, 403 hint) | `generator/parser/generator.go` (`opAnnotations`) for the annotation; `internal/commands/root.go` for the `privileges` catalog field; `internal/commands/privilege_error.go` for the 403 hint |
| Change behavior of all modern API commands | `generator/parser/generator.go` (`resourceTemplate`) |
| Change behavior of all classic API commands | `generator/classic/generator.go` (`classicResourceTemplate`) |
| Change how OpenAPI specs are parsed | `generator/parser/parser.go` |
| Change singleton detection logic | `generator/parser/parser.go` → `detectSingleton()` |
| Mark a GET-only, no-`{id}` resource as a singleton (so it gets `get`, not `list`) | `generator/parser/parser.go` → `readOnlySingletonPaths` map |
| Let an endpoint's documented non-2xx status (e.g. a check whose 403 body is the answer) render instead of becoming an exit-code error | `generator/parser/parser.go` → `documentedStatusResults` map; plumbing is `registry.WithAllowedStatuses` |
| Change multi-family spec splitting | `generator/parser/parser.go` → `splitByPathFamilies()` |
| Add/change alternate lookup fields (--serial, --udid) — modern API | `generator/parser/parser.go` → `resourceLookupFields` map |
| Add a CLI flag alias for a classic lookup (e.g. `--serial` → `--serialnumber`) | `generator/classic/generator.go` → `lookupFlagAliases` map |
| Fix a resource name auto-pluralization issue | `generator/parser/parser.go` → `resourceNameOverrides` map |
| Fix wrong RSQL filter field for --name lookup | `generator/parser/parser.go` → `resourceNameFieldOverrides` map |
| Fix wrong ID field extracted from list response | `generator/parser/parser.go` → `resourceIDFieldOverrides` map |
| Change how classic YAML manifest is parsed | `generator/classic/parser.go` |
| Add a new resource to the classic API | `specs/classic/resources.yaml` |
| Add server-side subset narrowing (`--subset`) to a classic `get` | `subsets:` list in `specs/classic/resources.yaml` (drives completion; non-id lookups auto-resolve to an id first for gateway compatibility) |
| Add/modify DDM component scaffolds | `internal/blueprintcomponents/scaffolds.go` — SDK-typed components via `example*()` funcs; raw JSON fallback in `rawScaffolds` for components not yet in SDK |
| Add a new legacy-to-DDM payload converter | `internal/profileconvert/ddm_<name>.go` (new converter + register in `ddm_converter.go` init) |
| Add/remove a resource in the `backup`/`diff` commands | `internal/commands/pro_resources.go` (curated allowlist; endpoints come from generated `backup_registry.go`) |
| Add a new Jamf Pro handwritten command | `internal/commands/pro_*.go` (new file + wire in `pro.go`) |
| Add a new Platform API endpoint (CRUD, actions, reports) | Drop/update spec in `specs/.platform-source/`, run `make sync-platform-specs && make generate`. Don't hand-write — generator owns CRUD/actions. |
| Add a new Platform business operation (apply, import-profile, clone, etc.) | Hand-write in the relevant `pro_<resource>.go`; CRUD primitives must come from `internal/commands/platform/generated/` |
| Add or change a generated Platform API resource | `make sync-platform-specs-from-sdk JAMFPLATFORM_SDK_PATH=/path/to/jamfplatform-go-sdk` — the SDK's `api/` is the canonical source; see below |
| Change behavior of all generated Platform commands | `generator/platform/template.go` (`resourceTemplate`) |
| Change Platform spec parsing (tenant prefix, tag grouping) | `generator/parser/platform.go` |
| Change Platform name-to-ID resolution | `internal/platform/resolve.go` (hand-written, typed); `internal/platform/resolve_generic.go` (generated, untyped) |
| Add a new Jamf Protect command | `internal/commands/protect_*.go` (new file + wire in `protect.go`) |
| Add/remove a resource in `protect backup`/`restore` | `internal/commands/protect_backup.go` → `protectResources()` (Export/Restore closures + `Order`) |
| Mark an object as a tenant default that `protect restore` skips | `internal/commands/protect_backup.go` → `protectDefaultObjects` |
| Change Protect name-to-ID resolution | `internal/protect/resolve.go` |
| Change Protect YAML import/export schemas | `internal/commands/protect_analytics.go`, `protect_ulf.go` |
| Add a new Jamf Security Cloud endpoint (Risk, Device Lifecycle, SSE) | Drop the spec into `specs/.security-source/`, run `make sync-security-specs && make generate`. Don't hand-write — `generator/parser/security.go`'s `securityOpsByFile` map owns every known operation. |
| Add a Jamf Security Cloud hand-written command (business logic, not a single HTTP call — currently only `setup`) | `internal/commands/security_*.go` (new file + wire in `security.go`) |
| Change behavior of all generated Security Cloud commands | `generator/security/template.go` (`resourceTemplate`) |
| Change how Security Cloud specs are parsed / which operations map to which resource | `generator/parser/security.go` (`securityOpsByFile`) |
| Add a gateway-served Security Cloud endpoint (DNS, ZTNA, categories, device groups, UEM Connect) | Publish the spec from `jamfplatform-go-sdk`'s `api/`, drop it in `specs/.platform-source/`, `make sync-platform-specs`, then wire the constructor in `internal/commands/security.go` |
| Rename a generated platform resource (collision or a bare-noun tag) | `generator/parser/platform.go` → `platformResourceNameOverrides` (keyed `{service}/{name}`) |
| Rename a generated platform operation whose path-derived verb reads badly | `generator/parser/platform.go` → `platformOperationNameOverrides` (keyed `{METHOD} {full path}`) |
| Stop enforcing a query param the spec marks required but the server ignores | `generator/platform/emitter.go` → `platformIgnoredRequiredParams` |
| Let a platform endpoint's documented non-2xx (e.g. a singleton's "not configured" 404) render instead of becoming an exit-code error | `generator/platform/emitter.go` → `platformDocumentedStatusResults`; runtime is `internal/platform/documented_status.go` |
| Change which columns a platform list renders in table/CSV output | `generator/platform/emitter.go` → `platformTableColumns` (keyed `{service}/{name}` — a bare resource name is not unique across services) |
| Correct a platform path version or success status the spec gets wrong | the SDK's `tools/generate/config.json`, republished as `x-jamf-tenant-path-version` / `x-jamf-expected-status` — not in this repo |
| Change Security Cloud auth (token cache, per-scope credentials, error mapping) | `internal/security/client.go` |
| Add a new cross-product command | `internal/commands/` (new file + wire in `root.go`) |
| Add a new product namespace | `internal/commands/` (e.g., `newproduct.go` + `newproduct_*.go` files); then update site (`index.html`, `style.css`, `catalog.js`) — `make verify-site` enforces |
| Modify auth behavior | `internal/auth/` |
| Change HTTP client / retry / exit codes | `internal/client/` |
| Add or change output formats | `internal/output/` |
| Add a short alias (e.g., `comp` for `computers`) | `internal/commands/aliases.go` |
| Add a command group for `--help` output | `internal/commands/groups.go` |
| Change global flags or root command behavior | `internal/commands/root.go` |
| Change config file handling | `internal/config/` |
| Change shared CLI interfaces (CLIContext, etc.) | `internal/registry/` |
| Modify the GitHub Pages showcase site | `docs/site/index.html`, `docs/site/style.css`, `docs/site/catalog.js`, `docs/site/terminal.js` |
| Change how commands.json is generated for the site | `generator/site/main.go` |

## Build & Dev Commands

```bash
make build                  # Build binary to bin/jamf-cli
make test                   # Run all tests (-v)
make lint                   # golangci-lint (skips generated code via .golangci.yml)
make generate               # Regenerate commands from OpenAPI specs, Classic manifest, and DDM component scaffolds
make sync-specs JAMF_SERVER_PATH=/path/to/jss JAMF_PRO_VERSION=11.31.0  # Copy per-resource specs from jamf-pro-server repo checkout, then regenerate
make sync-spec JAMF_MONOLITH_SPEC=./monolith.json JAMF_PRO_VERSION=11.31.0  # Split a consolidated /api/schema/ JSON into specs/, then regenerate
make verify-generated       # Check that generated code is up to date (CI-safe)
make verify-site            # Check that site supports all product namespaces (CI-safe)
make site                   # Build binary, generate commands.json, serve site locally at :8080
make fmt                    # go fmt + gofumpt
go test -v -run TestFoo ./internal/commands/...  # Run a single test
```

### Running the CLI

```bash
bin/jamf-cli pro setup                    # Interactive first-time config (creates Jamf Pro profile)
bin/jamf-cli protect setup                # Interactive first-time config (creates Jamf Protect profile)
JAMF_URL=https://... JAMF_TOKEN=... bin/jamf-cli pro computers list  # One-off with env vars
bin/jamf-cli -p my-protect-profile protect overview                 # Use a named protect profile
JAMF_CLI_ARGS='--quiet --no-input' bin/jamf-cli pro computers list  # Prepend default flags (CI/CD)

# Platform gateway auth (enables both Pro API and Platform API commands)
bin/jamf-cli config add-profile my-platform --url https://eu.apigw.jamf.com --auth-method platform --tenant-id <id>
bin/jamf-cli -p my-platform pro blueprints list           # Platform API command
bin/jamf-cli -p my-platform pro computers list            # Pro API routed through gateway
```

## Architecture

CLI for the Jamf platform. Root command holds shared infrastructure (config, auth, completion). Each Jamf product gets its own namespace — `pro` for Jamf Pro, `protect` for Jamf Protect. Platform API commands live under `pro`.

### Project Structure

```
internal/
  registry/              Shared interfaces: CLIContext, HTTPClient, OutputFormatter, ProtectClient, PlatformClient
  auth/                  Auth providers (OAuth2, Platform, Token) — Jamf Pro only
  client/                HTTP client with retry, auth injection, exit-code mapping — Jamf Pro only
  config/                YAML config, secret resolution, auto-migration
  blueprintcomponents/   DDM component scaffolds — SDK-typed structs (10 components) + raw JSON fallback (4 pending SDK support)
  profileconvert/        Mobileconfig/plist → DDM conversion, Apple schema fetching, legacy-to-native DDM payload converters (ddm_*.go)
  scope/                 Classic API scope XML types (shared by profile import and scope resolution)
  platform/              Jamf Platform helpers: name-to-ID Resolver, PrintList/PrintOne output
  protect/               Jamf Protect helpers: name-to-ID Resolver, PrintList/PrintOne output
  security/              Jamf Security Cloud client: 3 independent scoped-credential token caches
                         (Risk/Device Lifecycle/SSE), DoExpect{Risk,Lifecycle,SSE}, ReadBody, ConfirmAction
  commands/
    root.go              Root command, shared flags, product-aware auth resolution
    groups.go / aliases.go  Help groups and short aliases (root + pro + protect + school + security + platform)
    protect_helpers.go   Shared helpers: input reading, confirmation, export formatting (Protect + Platform)
    pro_platform_helpers.go  Platform-specific helpers: requirePlatformClient gate, printScaffold
    pro.go / protect.go / school.go / security.go  Bridges wiring all subcommands under each product
    pro_*.go             Jamf Pro handwritten + Platform API commands
    protect_*.go         Jamf Protect commands
    security_setup.go    Jamf Security Cloud hand-written command (credential setup only — CRUD is generated)
    pro/generated/       Pro generated commands from OpenAPI specs + Classic manifest
    security/generated/  Jamf Security Cloud generated commands from specs/security/*.json
docs/site/               GitHub Pages showcase site (HTML/CSS/JS, deployed via GH Action)
generator/
  parser/                Modern API generator (OpenAPI → commands); also parses Security Cloud specs (security.go)
  classic/               Classic API generator (YAML manifest → commands)
  security/              Jamf Security Cloud command generator (LoadResources + template-driven Generate)
  monolith/              Consolidated OpenAPI splitter: monolith → per-resource specs/*.yaml
  site/                  Site data generator: introspects binary → commands.json
```

### Code Generation Pipeline

```
specs/*.yaml ──────────────► generator/parser/   ──► internal/commands/pro/generated/*.go
                               ParseSpec()            + registry.go
                               Generator.Generate()

specs/classic/resources.yaml ► generator/classic/ ──► internal/commands/pro/generated/classic_*.go
                               ParseManifest()        + classic_registry.go

All resources ────────────────► smoke_registry.go (every GET for smoke tests)
                               ► backup_registry.go (list+get pairs for backup/diff)

Entrypoint: generator/main.go
```

Key types in templates: `parser.Resource` (`Name`, `NameSingular`, `GoName`, `Operations`, `IsSingleton`), `parser.Operation` (`Name`, `Method`, `Path`, `IsList`, `IsPaginated`, `IsDestructive`), `classic.ClassicResource`. `IsPaginated` (any GET with `page`/`page-size` params) is broader than `IsList` (list/history only) and gates `--all`/`--limit` auto-pagination so report/action GETs like `patch-report` page through all results.

`ParseSpec` returns `[]*Resource` — most specs produce one, but multi-family specs (e.g. `SelfServiceBranding.yaml`) produce one per family. `IsSingleton` is true for settings-style resources (GET+PUT, no `{id}`) — they get `get` instead of `list`, skip `apply`.

See `generator/README.md` for full template reference.

### Generated Command Features

Generated commands automatically get: `apply` (name-based upsert); `get`/`update`/`delete`/`patch` with `--name` (and per-resource `--serial`/`--udid`) for single-`{id}` paths; `patch` with JSON Merge Patch (RFC 7386) + `--set key=value` + shell completion of scalar fields; `--scaffold` on `create`/`update`/`patch` (rendered by `parser.ScaffoldJSON`, shared by all three generators — it skips read-only fields, keeps write-only ones, prefers a spec example over a placeholder, and shows one element inside an array whose element is an object). When a spec declares BOTH a per-`{id}` x-action and a collection-level sibling at the same path minus `{id}` (e.g. `/deployments/{id}/computers/installation-retry` + `/deployments/computers/installation-retry`), `pairCollectionBulkActions` (in `parser.go`, run before name disambiguation) drops the bulk op and records its path on the per-`{id}` op as `BulkActionPath`; the template then adds an `--all` flag that hits the collection-level endpoint in one server-side call instead of the `{id}` one. Each generated Pro and Platform command also carries a `jamf:privileges` annotation (populated from `x-required-privileges` in the spec via `opAnnotations`) surfaced in the `commands -o json` catalog as a `privileges` array; for Pro, the privilege names are additionally appended to the 403 `permission_denied` hint at runtime (the Platform 403 hint is not wired). Classic commands carry no privilege data. All behavior lives in the templates — don't re-document here, read `generator/parser/generator.go`.

**Name resolution needs a GET-serving collection.** `apply`, `--name`, `--serial` and `--udid` all work by GETting the resource's collection and RSQL-filtering it, so they are generated only when `nameResolutionPath` (`generator/parser/generator.go`) is non-empty. That is deliberately stricter than `collectionPath`, which answers "what is this resource's collection URL" and to do so falls back to the create-POST path and to the `{id}` path minus its last segment — neither implying the server answers a GET there. Resources whose modern API is POST-collection + GET-`{id}` only (dock-items, venafis, cloud-azure, cloud-ldaps, the PKI settings resources) therefore ship ID-only CRUD: no `--name`, no `apply`. Add a `resourceNameLookupPathOverrides` entry if a sibling endpoint can serve the lookup; that override bypasses the check.

Name-resolution helpers (in `registry.go` / `classic_registry.go`): `readApplyInput`, `extractJSONField`, `resolveNameToIDForApply`, `extractClassicName`, `resolveClassicNameToIDForApply`.

### GitHub Pages Site

Site at `docs/site/` auto-deploys on push to `main` via `.github/workflows/deploy-site.yaml`. Introspects the built binary to generate `commands.json` — command list, counts, groups, products, flags, aliases auto-update.

Adding a product or reclassifying groups needs manual edits in `index.html`, `style.css`, `catalog.js` — `make verify-site` enforces in CI. Local dev: `make site`.

### Runtime Flow

`cmd/jamf-cli/main.go` → `commands.NewRootCmd()` → `PersistentPreRunE` (resolves auth + config, determines product from command hierarchy, builds HTTP client and/or Platform SDK client) → subcommand.

Commands matched by the `chainSkip` set in `root.go` (`completion`, `help`, `version`, `config`, `diff`, `setup`, `multi`, `doctor`, `mcp`, `agent-context`, plus root-level `commands` and any `--scaffold` invocation) bypass auth.

### Auth

Resolution order: env vars (`JAMF_TOKEN`, `JAMF_CLIENT_ID`, `JAMF_CLIENT_SECRET`, `JAMF_TENANT_ID`) > config profile.

Three methods: **token** (pre-existing bearer), **oauth2** (client credentials against instance `/api/oauth/token`), **platform** (client credentials against Jamf Platform Gateway, e.g. `https://{region}.apigw.jamf.com/auth/token`; requires `--tenant-id`; Classic paths routed through `/api/proclassic/`, modern through `/api/pro/{version}/`, with the tenant in an `X-Tenant-Id` header; also constructs `jamfplatform-go-sdk` client enabling Platform API commands).

Secrets in config use prefixed references: `env:VAR`, `file:/path`, `keychain:service/account`. Bare values in `config add-profile` are stored in keychain automatically.

### Jamf Protect Integration

Uses `jamfprotect-go-sdk` (GraphQL, SDK manages tokens/retry/pagination). No use of `internal/auth/` or `internal/client/`. Env vars: `JAMFPROTECT_URL`, `JAMFPROTECT_CLIENT_ID`, `JAMFPROTECT_CLIENT_SECRET` (falls back to `JAMF_*`).

Patterns: positional `<name>` args (not `--name` flag) resolved via `internal/protect/Resolver`. `apply` (upsert from JSON or YAML) instead of separate create/update — output of `export` can be piped to `apply`. List commands flatten to essential fields for table output; `get`/`apply` use `printResult()` (flatten for table/csv/plain, full JSON for json/yaml). Delete and apply-replace require `--yes` or interactive confirm. Granular mutations (`add-analytic`/`remove-analytic`, `add-exception`/`remove-exception`, `add-rule`/`remove-rule`, `add-filter`/`remove-filter`) use read-modify-write and are idempotent.

`plans apply` only sends its cross-resource reference fields (`exceptionSets`, `analyticSets`, `unifiedLoggingFilterSets`, `usbControlSet`) when the input list is non-empty, so omitting or emptying one leaves the plan's existing membership unchanged rather than clearing it — use the granular `remove-*` subcommands to detach. This does **not** apply to a set's own membership list: `ulfs apply` (`filters`) and `analytic-sets apply` (`analytics`) always send the field, so an omitted or empty list clears it.

**`protect restore` is the exception: it clears.** Its contract is that the target ends up matching the document, so an absent membership list has to be *sent* as an empty one — the SDK's `buildPlanVariables` omits a nil list from the GraphQL variables and the server then leaves the field alone, which would let a binding added after the backup survive a rollback. `planExportToInput`'s `clearAbsent` parameter is the switch: restore passes true, `plans apply` passes false. Telemetry needs the SDK's `TelemetryV2Null` rather than an empty value, being a single reference; `usbControlSet` has no explicit-null mechanism in the SDK at all, so a USB control set detached after the backup is the one binding restore cannot converge.

Analytics and ULF additionally support `import --file`/`--dir` matching `jamf/jamfprotect` community repo YAML schema. Both resources' `apply` accepts **either** that community schema or the SDK input shape, sniffed per document (`analyticInputFromDocument`, `ulfInputFromDocument`) — the two disagree on field names (community `actions` are objects vs the input's `analyticActions`; community `predicate` vs the input's `filter`), so decoding one as the other silently produced an empty field and the server refused it. `export | apply` is a documented pipe for both and is covered by tests for both.

`unified-logging-filter-sets` (`ulfs`, SDK v0.8.0+) groups unified logging filters for assignment to plans — the ULF analogue of `analytic-sets`. Sets carry a read-only `plans` back-reference (excluded from `export`, which stays portable by using filter names); filters carry a read-only `sets` back-reference. Attach sets to a plan via `unifiedLoggingFilterSets` in `plans apply`. The server refuses to delete a set that is still used by a plan; deleting a filter cascades it out of any sets.

**Export output shape changed for four resources** (groups, users, api-clients, exception-sets). Cross-resource references now export as **names**, not server IDs: `roles:`/`groups:`/`connection:` in place of `RoleIDs`/`GroupIDs`/`ConnectionID`, and an exception's `analytic:` alongside its `analyticUuid`. Role IDs are small sequential integers, so an ID-shaped document applied to another tenant bound to whatever role held that integer — the wrong grant, silently. Input stays backwards compatible in both directions: `rbacDocumentUsesIDs` routes an ID-shaped document down the old path, exception sets accept a uuid-only exception, and JSON keys that were PascalCase still bind because `encoding/json` matches case-insensitively (yaml.v3 does **not** — it matches the lowercased Go field name exactly, which is why the exception export deliberately carries json tags and no yaml tags except on the one genuinely new field). Anything that *parses* these commands' output needs updating. The reference fields carry no `omitempty`, so `connection: ""` and `roles: []` are written out rather than omitted: an absent list and an empty one mean the same thing to the converters (they always send `[]`), so writing them keeps a document self-describing — which is what lets `apply --scaffold` teach the shape at all — and keeps two tenants' copies of the same group diffable when one holds no roles.

`protect analytics overrides` manages the **tenant overlay on Jamf-managed analytics**. Jamf publishes analytics centrally: `analytics list` in a stock tenant returns ~156 rows all flagged `jamf: true`, and the server refuses `updateAnalytic` against any of them (`"This mutation may only be used for custom analytics"`). What a tenant *can* change is `tenantSeverity` and `tenantActions`, written by the separate `updateInternalAnalytic` mutation (`UpdateInternalAnalytic` in the SDK, served on `/app`). Since the definitions are identical in every tenant, that overlay is the only part of a Jamf-managed analytic worth capturing — so `overrides export` emits it keyed by analytic **name** and `overrides apply` replays it into another tenant, skipping entries that are absent or custom. `overrides apply` treats an absent half as "no override" and clears it, which is what makes re-applying the same document idempotent; use `overrides set` for partial edits, where an omitted flag leaves that half untouched. Both `overrides apply` and the `analytic-overrides` restore closure report a refused entry and continue, then fail at the end with both counts — aborting at the first failure left the earlier entries written and unreported and the rest never attempted, which made a retry impossible to reason about.

Two traps this exposes. `analytics list` reports Jamf's **baseline** severity, not the effective one, so an analytic overridden to `Low` still displays as `High` — `overrides list` shows baseline, tenant and effective side by side. And `analytics export` emits only the community YAML definition, dropping `tenantSeverity`/`tenantActions` entirely, so it silently discards exactly the customisation a customer made. `Analytic.hash` is a revision stamp, not a content hash — it changes across a mutate-and-revert cycle that leaves content identical, so it cannot be used as a change-detection or cross-tenant identity key.

`protect backup` and `protect restore` capture and replay a whole tenant. Backup writes each object to its own file under a per-resource directory in the same portable form the matching `export` produces; restore walks that directory in dependency order, resolving every reference by name against the target as it goes. Both take `--resources` (allowlist) and `--exclude` (denylist), which compose and both shell-complete; restore adds `--include-defaults` and reads the root `-n, --dry-run`. `protectResourceListHelp` renders the vocabulary into each command's `--help` from the table itself — hand-writing it would drift, and before that the only ways to discover a resource name were completion and reading the error from a wrong value. Backup exits non-zero when any resource failed — matching `pro backup`, because a backup that exits 0 with a resource missing is indistinguishable from a good one to the job that scheduled it — with `--allow-partial-failure` to downgrade. Backup prunes documents an earlier run left that no longer match the tenant, reporting each — without it a backup directory is the union of every run that wrote to it, and since restore applies whatever it finds, a deleted object gets silently recreated (switching `--format` had the same effect, leaving both extensions on disk for restore to apply twice). `pro backup` does **not** prune; the divergence is deliberate, because Pro has no restore so a stale file there only misleads a diff. `protectPruneStale` only ever considers files this command could have written — inside a directory named after one of its own resources, or the single file a singleton owns, and only with an extension restore reads — and never prunes a resource whose export failed, since the true object set is unknown. `--no-prune` keeps them. **Pruning is refused when the directory's `_meta` records another tenant having written to it**, because it is keyed on "the object set of the tenant being backed up right now": pointed at another tenant's backup it would delete that tenant's documents for every object this one lacks, reported only as a prune count, with no `jamf:destructive` annotation and no confirmation. That record is **cumulative and written first**, which is what makes the guard trustworthy: `TenantURL` alone was last-writer-wins, so a `--no-prune` run — the very thing the refusal recommends — relabelled the directory and the *next* run by that tenant pruned the first tenant's documents; and writing `_meta` last left an interrupted run's documents with no provenance for the next run to refuse on. Each run appends its own normalised URL to `Tenants` (the comparison folds case and a trailing slash, since `resolveProtectClient` takes the URL verbatim from four sources), claims the directory before exporting anything, and replaces the manifest with counts at the end, removing any copy an earlier `--format` left in the other extension so one manifest is authoritative. A `_meta` that is present but unparseable beside documents **refuses the run outright**, `--no-prune` included: rewriting a manifest it could not read would drop another tenant's claim and hand the next run a guard that passes. A manifest that is **absent, zero-length, or parses to no tenant** beside documents refuses the *prune* — not the run — for the same reason one run later: both used to read as "fresh directory" and permit it, and a zero-length file is exactly what an interrupted or out-of-space write leaves, so the state the write-the-claim-first ordering exists to survive landed in the one branch the guard did not cover, looking clean rather than broken. That case is a pruning question rather than a provenance-rewrite one — there is no claim to overwrite and lose — so `--no-prune` is a real way forward there and the refusal says so: it records the tenant, and the run after it prunes normally. The root `-n, --dry-run` covers the prune and nothing else: documents are still written (that is what `--output` asked for, and `pro backup -n` writes its files too), and every removal the run would have made is reported `[dry-run]`. Gating the `protectPruneStale` *call* instead of its `os.Remove` would delete nothing and report nothing, which reads as "there was nothing to prune"; the claim manifest also carries the previous run's `Resources`/`Counts` forward rather than blanking them, so an interrupted run degrades to a stale inventory instead of describing a backup that never happened. Restore reports a mixed result the same way backup does — `exitcode.PartialFailure` (7) rather than `General` (1), downgradable with the same `--allow-partial-failure` — so one job can observe both halves of a round trip identically. Both commands read the extension set from `protectRestoreExts`, so what backup prunes is exactly what restore applies; accepting only `.yaml`/`.json` for singletons made a hand-written `insights.yml` a file backup deleted and restore ignored. Object names are free text and `protectFileNameSafe` is lossy in both directions (runs of illegal characters collapse; a case-insensitive filesystem folds case), so `protectNameAllocator` appends a name-derived discriminator on collision rather than letting one object overwrite another while the count still reports two. Restore updates what exists and creates what does not — it never deletes, so an object in the tenant but absent from the backup survives. Per-object granularity is the filesystem: delete files from the backup directory to skip them (which is how you restore synthetic users while holding back real ones).

Insights and identity provider connections are captured as state documents rather than objects: the insight catalogue is Jamf-published so only the enabled/disabled split is a tenant's own data, and connections have no create API at all, so they are recorded for reference (a user or group naming one restores only where that name exists) and never replayed.

`protectResources()` in `protect_backup.go` owns the set, and its `Order` field is the correctness argument — members before the sets naming them, sets before the plans binding them, roles before groups before users. A test asserts that chain rather than the numbers. Deliberately not replayed: Jamf-managed content, tenant defaults (`protectDefaultObjects`), API clients (a new secret is issued on create), and data forwarding (its response is not its update shape and its `cloudformation` blob embeds a tenant-specific IAM ExternalId).

Wire facts established by cloning between two live tenants, none of which are guessable from the specs:

- **Jamf-published analytic UUIDs are stable across tenants** — `BlazingKeylogger` is `da360eb3-…` in both. Custom analytics get per-tenant UUIDs. An exception set therefore exports its target analytic by **name** (`analytic:`) as well as uuid, and apply rebinds the name against the target; the uuid alone was portable only for Jamf analytics. **A foreign `analyticUuid` is rejected, not silently accepted** — the server answers `createExceptionSet: Action blocked due to dependencies on this resource.`, which names neither the analytic nor the uuid nor the reason. Established by applying two documents differing only in that uuid. `exception-sets` therefore sits at `Order` 20, not alongside the analytics: the name only resolves once the analytic exists in the target.
- **Identity provider connection names are tenant-specific** (`jamf-id-db` exists in both, but `jamfmspservices-protect-jamfcloud-com` only in one), so a user or group naming a connection restores only where that name also exists.
- **RBAC reference lists must marshal as `[]`, never `null`** — a user in groups but holding no direct role otherwise fails with `input → roleIds: None is not of type 'array'`.
- **A plan's `customEngineConfig` has PascalCase JSON keys and all-lowercase YAML keys** (`MalwareRiskware` vs `malwareriskware`) because `CustomEngineConfigInput` carries json tags and no yaml tags. Modes are `PREVENT`/`REPORT`/`DISABLED`. Backup and restore are self-consistent; only a hand-written document trips on it. `plans apply` also *requires* `actionConfig` — omitting it answers `actionConfigs: contains invalid characters`, because the SDK sends the field regardless.
- **Action config `params` is `AWSJSON!`**: read back as an object, but the input wants a JSON-encoded *string*, and it cannot be omitted. The response also fills every member of the `ReportClientParams` union with zero values, and `batchConfig.sizeInBytes` returns 0 against a documented minimum of 1000. The union's `HttpClientParams` selects `headers { header value }` in full, so an HTTP report client's bearer token or API key is captured verbatim and cannot be redacted without breaking restore — `action-configs` and `data-forwarding` therefore carry a `SensitiveReason`, are written `0600` rather than `0644`, and are reported before the operator commits the tree. The mode needs an explicit `os.Chmod` after the write: `os.WriteFile` passes `perm` to `OpenFile(O_CREATE|O_TRUNC)`, which applies it *only when the file is created*, and git records no non-exec permissions — so a clone of a backup repo hands every file back `0644` and a re-run would have kept it there while still reporting `0600`. The legacy `ForwardSentinel.sharedKey` *is* redacted, since data forwarding is never replayed. (`ForwardSentinelV2` got this right and reports `secretExists` instead.)
- **USB control vendor and product IDs need a `0x` prefix** (`0x0781`); bare hex is rejected as "contains invalid characters".
- **`commsConfig` is always sent** by the SDK, so `protocol` must be one of `mqtt`/`wss/mqtt`/`auto`; its `fqdn` is the region-assigned IoT endpoint and the target keeps its own. That and an exception's rebound `analyticuuid` are the only two fields that legitimately differ after a clone — for the latter, equality would mean the rebinding *failed*. Verified cross-tenant on `0cf1041`: 26 documents applied, 0 failed, re-run idempotent, 24 of 32 files byte-identical and all 8 differences accounted for.
- **Data retention updates are rate-limited to once per 24 hours**, which is why `data-retention` restore compares `dataRetentionToInput(current)` against the document and writes only a real change — an unconditional write made every re-run inside that window report a failure for a resource already in the desired state.
- **Writing a setting the tenant already holds can be refused.** Disabling a change freeze that is not on answers "Tenant '...' is not in a change freeze", so `config-freeze` and `insights` compare before writing and only send real changes. `data-retention` does the same for the rate limit above. **`accessGroup: true` on a connection-less local group is the sharpest case**: `createGroup` accepts it and stores `true`, but `updateGroup` refuses it *even when `true` is already the stored value* — so a restore succeeded once and failed on every re-run. It can be turned off via update and never back on. `protectGroupUpdateSatisfied` therefore skips the update when the target already holds the desired state, and when it does not, says the operation is create-only rather than passing through the server's bare message. It is shared by `restoreGroup` and `groups apply`, because the asymmetry is a property of the API rather than of restore — `groups export | groups apply` hit it too. **"Skips the update" has to mean "there was nothing to send", not "the flag matches"**: the first version of the guard returned satisfied on `accessGroup` alone, so a document that also added a role printed `nothing to update` and exited 0 with the role grant silently dropped — a worse failure than the raw server error it replaced. `groupUpdateWouldChange` compares every field `updateGroup`'s mutation can carry (name, accessGroup, roleIds — sorted, since the server returns roles in its own order) plus the connection it cannot, and a non-empty diff is an error naming the fields that could not be applied.
- **Telemetry `events` is `[String]!` in the schema but validated against an undeclared server-side list** — a wrong value answers "This mutation may only use predefined types". `network_connect` is valid; `network`, `network_listen`, `network_accept`, `dns_request`, `process_exec` and `file_create` are all rejected, so the list is narrow and does not follow the `GP*Event` naming used by analytic `inputType`.

`protect downloads` subcommands fetch installer/uninstaller/pppc-profile/tamper-prevention-profile/root-ca/csr/websocket-auth/summary files. `protect plans config-profile <name>` downloads a `.mobileconfig` (use `--no-*` to exclude payloads, `--sign` to sign).

### Jamf Platform API Integration

Uses `jamfplatform-go-sdk` (REST, SDK manages tokens/retry/pagination). Platform commands live under `pro` and appear in "Platform:" help group. A single `auth-method: platform` profile enables both Pro API (routed through gateway) and Platform API (via SDK).

Runtime-gated: commands are always registered but `RunE` starts with `requirePlatformClient(cliCtx)` and errors with setup instructions when platform auth not configured.

Name resolution via `internal/platform/Resolver` (blueprints by name, benchmarks by title, baselines by title, device groups by name). Devices use SDK filter methods directly (UUID vs serial auto-detected by hyphen presence).

CRUD pattern: `apply` (upsert, blueprint uses merge-patch; benchmark is create-only — SDK has no update), `get`, `delete`, `export`. `apply --scaffold` prints create request template (auth skipped for scaffold).

**Identifier convention differs from Protect.** Protect takes a positional `<name>`; Platform takes a positional **`<id>`** plus a `--name` flag (`delete <uuid>` or `delete --name "My Blueprint"` — passing a name positionally is sent as an ID and 404s). This holds for both the generated commands (template: `generator/platform/template.go`, gated on `SupportsNameLookup`) and the hand-written ones (`resolveBlueprintID` in `pro_blueprints.go`), which reject `<id>` and `--name` together. Exceptions: `blueprints clone <source-name> <new-name>` takes names positionally; `blueprints import-profile [<id>]` sniffs its positional (`isClassicID` in `pro_blueprints.go`) — an all-digit argument is a Classic ID, anything else a display name — to keep pre-#316 name-based invocations working. Consequence: a profile whose display name is all digits must be looked up with `--name`. Do not copy the sniffing into new commands without a comparable compat reason.

Naming: `platform-` prefix where overlap with existing Pro resources (`platform-devices`, `platform-device-groups`); no prefix for unique resources (`blueprints`, `compliance-benchmarks`, `ddm-reports`).

Commands: `blueprints` (`bp`) — CRUD, deploy/undeploy, clone, scope, components, import-profile (auto DDM conversion), report. `compliance-benchmarks` (`cb`) — benchmark CRUD (apply/get/list/clone/delete/export; create-only, no update). `baselines`/`rules` — read-only mSCP reference data (list). `benchmark-reports` — compliance reporting (`rules`/`devices`/`compliance-percentage`, keyed by benchmark ID). `platform-devices` (`pdev`), `platform-device-groups` (`pdg`), `ddm-reports` (`ddm`).

### Jamf Security Cloud Integration

Uses a hand-rolled client (`internal/security`) — no product Go SDK exists. Auth is a Basic-login-for-JWT exchange (`POST /v1/login` with `base64(clientId:clientSecret)` → a 15-minute JWT), not OAuth2 client-credentials. Critically, each of the three APIs — Risk, Device Lifecycle, and Shared Signals & Events (SSE) — is provisioned as its own "Security Integration" in the Radar portal with its own application ID/secret, and the resulting JWT is scoped to exactly one API via its `aud` claim. So `internal/security.Client` tracks three independent credential pairs and token caches (`DoExpectRisk`/`DoExpectLifecycle`/`DoExpectSSE`); any subset may be configured, and commands for an unconfigured API fail with a "run security setup" hint rather than the whole product refusing to start.

Risk and Device Lifecycle share one host (`api.wandera.com`, which also serves `/v1/login`); SSE lives on `sse.jamf.com` per its own OpenID SSE framework discovery document. Unlike Pro/Protect/School, there's no per-tenant URL — tenancy is carried inside the JWT's `customer_id` claim, so `security setup` never prompts for a URL (env/profile overrides exist for `--url`/`JAMFSECURITY_URL`/`JAMFSECURITY_SSE_URL` in case Jamf ever stands up regional or sandbox hosts).

Commands are **generator-owned**, same as Platform — the eleven total operations across the three specs are hand-mapped (not tag/family auto-detected like Platform's parser) because they span wildly different shapes (a paginated list, singleton-style get/update/delete, bulk actions with no `{id}` in their path at all) that are too few and too irregular to benefit from generic detection. See `generator/parser/security.go`'s `securityOpsByFile` map. Commands: `risk` (`list`/`override`), `device-lifecycle` (`purge` — destructive, `{customerId}` filled at request time from the Device Lifecycle JWT, never user-facing), `stream` (SSE singleton-style get/update/delete), `status` (get/update), `verification` (`trigger`), `jwks`/`well-known` (SSE discovery, read-only). No name-to-ID resolution — every identifier (guid/externalId) is supplied directly.


#### The gateway-served half of Security Cloud

Part of Jamf Security Cloud is served on the **platform gateway** (`/api/securitycloud/...`) rather than on `api.wandera.com`, and is reached with platform client-credentials plus a Security Cloud tenant ID — not the scoped Basic-login pairs above. Five specs, 47 operations: DNS (`dns-zones`, `dns-search-domains`, `dns-custom-hostname-mappings`), ZTNA (`ztna-apps`, `ztna-gateways`, `ztna-grouped-gateways`, plus read-only `ztna-shared-gateways` / `ztna-predefined-apps`), `content-categories`, `device-groups`, and UEM Connect (`uem-connectors`, `uem-connector-enablement`, `uem-sync-settings`, `uem-sync`, `uem-activation-profiles`).

These are generated by the **Platform** generator (`generator/platform/`) into `internal/commands/platform/generated/`, then wired under `security` in `security.go` — the same namespace as the Wandera-served commands, because it is one product to whoever is typing, and `pro` already mixes two auth paths under one namespace.

Because the two halves take **different credentials**, every command says which API serves it. The `Short` carries it in words — `(Security Cloud · platform gateway)` vs `(Security Cloud · Radar API)` — which is what `security <TAB>` shows, since Cobra uses `Short` as the shell-completion description. The `jamf:api` annotation (`platform-gateway` / `radar`) carries the same fact for machines and surfaces in the `commands` catalog as `api`. Both come from the generators, so a new resource gets them without a CLI edit; `TestSecurityCommandsDeclareTheirAPI` fails if one is emitted without the other. `security setup` declares neither — it writes credentials and calls no API.

**Specs are sourced from the SDK, not from `public-apis-oas`.** Drop `jamfplatform-go-sdk`'s published `api/securitycloud_*.json` into `specs/.platform-source/`, then `make sync-platform-specs`. The upstream `-beta` specs are wrong in ways only wire probing revealed, and the corrections live in the SDK's `tools/generate/config.json`, validated by acceptance tests against a live tenant. The SDK publishes them as OpenAPI extensions that this generator reads, so no override table is duplicated here — `specs/platform/securitycloud_*.json` is currently the **GitOps build v1439** drop:

- `x-jamf-tenant-path-version` (spec root) — the URL version segment a spec's paths need but omit. The versionless form answers **403 `BAD_PERMISSIONS`**. **No spec sets it any more**: categories moved the version into its own paths in build v1353, dns and ztna followed in v1416, and build v1495 then dropped the tenant segment entirely in favour of the `X-Tenant-Id` header. The support stays because the next spec to arrive without a version prefix needs it — `normalisePlatformPaths` still consumes it — but it no longer has anything to do with tenancy. A spec that gains the prefix while *keeping* the extension would send `/v1/v1/…`, so check the paths rather than the extension when a spec is bumped.
- `x-jamf-expected-status` (operation) — the success status the server really answers (`PUT .../groups/{id}` → 200, not the declared 204).

Extensions rather than corrected paths because the SDK's `api/` doubles as its own generation fallback, keyed on the source spec's paths.

**The scope travels in a header, not the URL.** Every Jamf gateway path used to embed it — `/api/{namespace}/{version}/tenant/{tenantId}/{resource}` — and Tyk resolved the request context from `path`. Prod gained `header` as an allowed source on 2026-08-25 (tyk-gateway-management `0793131b`, "JSC-73421 Enable header context support - Prod") and the published specs dropped the segment in GitOps build v1495 in favour of a required `X-Tenant-Id` header. Both forms answer during the transition window, so this repo follows the SDK onto headers only rather than keeping a selectable mode — a second code path nothing exercises is how the previous URL-shape bug survived weeks.

Three copies of that rule exist and all three moved together: `normalisePlatformPaths` (`generator/parser/platform.go`) strips `/tenant/{tenantId}` from every platform spec path — Security Cloud has already lost it upstream, blueprints/benchmarks/devices/pro/classic still declare it; `rewritePathForGateway` (`internal/client/client.go`) maps instance paths onto their gateway namespace (`/JSSResource/x` → `/api/proclassic/x`, `/api/v1/x` → `/api/pro/v1/x`) and `setTenantHeader` stamps the header on both the request and the multipart-upload path; and the SDK's `Transport.APIPrefix` replaces the old `TenantPrefix`. `TestLoadResourcesDropsTheTenantFromEveryPath` and `TestGatewayTenantTravelsInHeader` fail if a tenant segment reappears in either. Wire-verified 2026-08-25: Security Cloud (categories, device-groups, ztna-apps create/get/delete), Pro modern (`/api/pro/v1/categories` list, POST 201, DELETE 204), Classic (`/api/proclassic/allowedfileextensions`) and Platform (`/api/blueprints/v1/blueprints`).

**Two things this migration does *not* settle.** The previous URL shape existed because the gateway's **audit** rules are path globs of the form `/**/v{n}/{service}/…`, and version-first paths matched nothing — most Security Cloud mutations executed and were never recorded (see `docs/solutions/logic-errors/securitycloud-tenant-first-url-audit-2026-08-21.md`, now superseded as a fix but not as a warning). Whether those globs match the header-scoped shape is not verifiable from this repo; it needs confirming with whoever owns the Tyk config, because the failure mode is silent by construction. And `Client.Upload` (JCDS, multipart) now carries the header but has not been exercised on the wire.

**Environment scope exists in the SDK and is not used here.** `internal/client.ScopeEnvironment` sends `X-Environment-Id`, and the gateway lists `[tenant, environment]` as request-context types on securitycloud and compliance-benchmarks. No SDK option sets it (`WithTenantID` is the only one) and no generated operation is environment-scoped, so the CLI cannot send it: tenant IDs stay the only exposed scope. They are expected to become legacy at Platform API GA, when one environment ID will reach several product namespaces and the per-namespace tenant split below disappears.

**Tenant IDs are per-namespace.** Security Cloud is a separate product with its own tenant identifier, so a customer holding both has one for Jamf Pro and another for Security Cloud, both reachable from one platform profile. Configure via `security-cloud-tenant-id` in the profile, `JAMFSECURITY_TENANT_ID`, `platform setup`, or `config add-profile --security-cloud-tenant-id`; generated commands dispatch through `cliCtx.SecurityCloudSDKClient`, a second SDK client built with that tenant — one client carries one `X-Tenant-Id`, so `TenantIDFor(<service>)` had nothing to resolve once the scope left the path (`platformSDKClients` in `internal/commands/pro_platform_helpers.go` builds the pair; they share credentials and therefore the cached token). Wrong tenant → **403 `OWNERSHIP_FORBIDDEN`** (distinct from `BAD_PERMISSIONS`, which means the gateway does not route the path at all). When unset, the same client is returned twice, which keeps the old fallback: Security Cloud paths use the Pro tenant, right only where the two match.

**Which setup command owns what.** `platform setup` owns everything gateway — region, client credentials, the Jamf Pro tenant and the Security Cloud tenant — and is the entry point for the gateway-served commands. `security setup` owns the Radar application pairs only, and closes by pointing at `platform setup`. A platform profile needs **either** tenant, not both: Security Cloud paths carry their own tenant, so a profile holding only `security-cloud-tenant-id` is complete and `config validate` passes it on that check. `platform setup` validates a supplied Security Cloud tenant with one read against `content-categories` and *warns without refusing* — the entitlement may not be provisioned yet, and a hard failure would block a valid Pro-only profile. It also runs with retries disabled, so a mistyped secret fails immediately instead of backing off for ~90s at an interactive prompt.

`resolveSecurityClient` builds the platform SDK client independently of the scoped pairs, so a profile may carry either set or both; the product only refuses to start when neither is configured.

**A new endpoint version takes the plain command name; the old one is dropped.** This is the Jamf Pro rule (`deduplicateVersionedOps`), and it applies to the gateway unchanged: the highest version of an endpoint wins, the lower version stops being a command, and its path is recorded on the winner as a `FallbackPaths` entry. So Security Cloud's device groups expose one `list`, served by `GET /v2/.../groups`, not a `list` on the deprecated v1 beside a `list-v2` on its successor — the unversioned name has to point at the endpoint that is not dying, or the v1 sunset breaks the *primary* command name and forces everyone onto a differently-named one.

Two things made that dedup silently not fire here, both now fixed in `generator/parser/parser.go`. `stripVersionPrefix` only strips a *leading* version, which fits Jamf Pro (`/v1/computers-inventory`) but not the gateway, where the version follows the service namespace (`/api/securitycloud/v1/groups`) or the tenant (`/api/securitycloud/uem-connect/v1/connectors`) — so the two versions hashed to different keys. And `compareAPIVersions` ranked by the leading segment, scoring every gateway path 0, which made "prefer the higher version" a tie decided by map iteration order. Both now read the version wherever it sits (`stripVersionSegments`, `apiVersionRank`); across all Jamf Pro specs and every platform spec this introduces exactly one new collision, the one it is meant to catch.

`FallbackPaths` is populated for platform GETs and deliberately ignored by `generator/platform/template.go`. Pro retries a displaced older path on 404 because customers run older Jamf Pro versions; the gateway is a single deployment with no such version skew, so there is no tenant that has v1 but not v2. An unrouted path answers 403 `BAD_PERMISSIONS`, which is indistinguishable from a real privilege failure — falling back on that would turn a permission problem into a silent downgrade, the same class of failure as the `baselineId` rename answering 0 rules with no error.

**Known upstream gaps** (wire-probed 2026-08-20, tenant `wisconsam`, unless noted):

- `device-groups` declares `customer-id` **required** but the server ignores it; the tenant in the path decides. Suppressed via `platformIgnoredRequiredParams` in `generator/platform/emitter.go` — the one place wire knowledge is duplicated from the SDK, because its generator config cannot express "declared required, actually ignored".
- Pagination is under-declared. Ten list ops return a paged envelope (`totalCount` + `results`) but only `sync/runs` declares `page`/`page-size`. On the wire only `ztna/apps` honours them; `categories`, `ztna/predefined-apps` and `ztna/shared-gateways` demonstrably ignore both and return everything. `ConnectorPage` and `SyncRunPage` are structurally identical yet only the latter declares the params — the tell that this is an omission. Re-probed 2026-08-20: unchanged, and `GET /v2/.../groups` joins the list — it returns a `{groups: []}` envelope with no `totalCount` and ignores both params.
- `PUT /v1/.../groups/{groupId}` answers **200 with the updated group**, but the spec describes a body only under 204, so `successStatus` finds no schema for the status it is told to expect and the generated `update` discards what the server returned — a successful update prints nothing. Still true after the v1401 ingest: re-probed 2026-08-21, `security device-groups update <id> --set name=…` exits 0 printing nothing while the wire answers `200 {id, name}` and a following `get` shows the new name. Fixing this belongs in the SDK's `tools/generate/config.json` (declare the 200 with the group schema), not here; the CLI cannot optimistically decode instead, because the transport errors on an empty body when handed a non-nil result.
- `risk override` declares `deviceIds` as an array with **no `items` schema at all**, so `--scaffold` can only render `[]` and nothing can say what an element looks like. The one array in the Security Cloud surface that is opaque for an upstream reason rather than by design — every other remaining `[]` is an array of plain scalars (IDs, IPs, hostnames, subnets), which `parser.ScaffoldJSON` leaves empty deliberately.
- `--set` cannot express a top-level-array request body (the two DNS whole-list replaces), because it builds an object. `--file` is the only route; `--set` on those endpoints fails with `400 [INVALID_FIELD] Request body could not be read`.
- `uem-connectors create` cannot be built from the published spec at all, and the failure is a **500 rather than a validation error**. `authStrategy` and the credentials under `deviceSyncAuth` are absent upstream; the SDK restores both (`schemaCreations`/`schemaPatches`, self-expiring via `schemaPatchesRequireAbsent`), which is why `--scaffold` renders them as of the v1439 ingest. The error ladder, re-probed here 2026-08-24: no `authStrategy` → `500 INTERNAL_ERROR`; `authStrategy` with no `deviceSyncAuth` → `422 VALIDATION_FAILED ": invalid auth configuration for Jamf PRO"`; a complete body → `409 CONNECTOR_CONFIG_ALREADY_EXISTS`, because **a tenant holds at most one connector whatever its vendor** and that pre-check runs after auth-config validation but before the credentials are checked against the UEM instance. The 500 used to be unreportable as well as slow — the CLI nested its own retryablehttp client inside the SDK's, which retried the POST four times and then swallowed the response, so the answer took past a 2-minute timeout and arrived with no `traceId`. Fixed; it now answers after one attempt with the traceId in the message. The secret belongs in `--file`: `--set deviceSyncAuth.clientSecret=…` puts it in shell history and `ps`, which is what the credential policy at the top of this file exists to prevent. A read returns only `clientId`, `username` and an `empty` flag, so a connector cannot be exported and re-created.
- The ZTNA app's `security` block declares `additionalProperties: false` as of v1439 and the **server does not enforce it**: a create carrying `security.bogusKey` answers 201 and silently drops it (probed 2026-08-24, the app read back with the three declared sections and nothing else). So a mistyped `--set security.riskControls.enabld=true` is accepted and does nothing, with no wire feedback at all.
- `ztna-apps create` requires `categoryName`, and the spec gives it no `example`, so `--scaffold` renders `"categoryName": ""` — the one value the server explicitly rejects (`400 [INVALID_FIELD] categoryName: must not be blank`). Every scaffold is a template to edit, but this is the field where the rendered value is *guaranteed* invalid rather than merely incomplete; `Uncategorized` is what the tenant's own apps carry.
- `ipsec.left.subnets` is still capped at one element while `right` is not, and the rejection names neither the field nor the cardinality: two left subnets answer `400 [INVALID_FIELD] field=ipsec "IPSec configuration is not valid."` (probed 2026-08-24). Deep IPSec validation **does** run alongside a missing required top-level field — a create omitting `contact` and carrying a public left subnet reported both errors in one response — so a probe written to expect only the top-level failure will see more than it asked for.
- Neither `dns-zones create`'s 422s (`GATEWAY_NOT_FOUND`, `NAMESERVER_IP_RESTRICTED`) nor its validation rules are documented in the spec. A zone's `nameServers[].ip` must be **publicly routable** — `10.0.0.53` is rejected `NAMESERVER_IP_RESTRICTED`, `198.51.100.53` is accepted. Note this is the inverse of the ZTNA IPSec rule, where `ipsec.left.subnets[]` must be private.

**Resolved since the 2026-08-18 probe:**

- `GET /v2/.../groups` **is now routed** and returns the same seven groups as v1, field for field; only the envelope differs (`{groups: []}` vs a bare array). It is `list`'s endpoint as of the build v1353 ingest. v1 is deprecated (`x-deprecation-date: 2026-08-12`, `x-sunset-date: 2027-08-12`).
- `dns-search-domains get` no longer reports the empty state as an error. The 404 `SEARCH_DOMAIN_NOT_SET` a tenant with no search domain gets is now carried through `platformDocumentedStatusResults` (`generator/platform/emitter.go`) and `platform.DoExpectDocumented`, which renders `{}` and exits 0. The match is on **status and error code**, so a 404 arriving for any other reason still fails — a bare status allowance would render a mistyped path as an empty answer with exit 0. It renders an empty object rather than the server's error envelope because that envelope is a different schema from the one `get` returns when the setting exists, and its `traceId` changes per call, so two identical reads would not even produce the same output.
- `--scaffold` used to render every array as `[]` with no element shape, which cost a wire round-trip to discover that `dns-custom-hostname-mappings update` wants `{hostname, aRecords[], aaaaRecords[], secureDns, ztna}` — the only feedback on a wrong guess being `400 [INVALID_FIELD] [0].aRecords: Invalid field value`. Fixed by `parser.ScaffoldJSON`, one walker shared by all three generators, which shows a single element whenever the element is an object. `dns-zones create --scaffold` now renders `nameServers` as `[{gatewayId, ip}]`. Two side effects worth knowing: nested read-only fields are now dropped from request templates (ZTNA's `dedicatedIps.ips` is server-allocated, and the old platform scaffold invited you to set it), and a bare-array request body finally gets a `--scaffold` at all. See `docs/solutions/conventions/one-scaffold-walker-2026-08-20.md`. Build v1401 then added `example` values throughout the ZTNA spec, and the walker prefers a spec example over a placeholder, so those scaffolds now render real-looking values rather than empty ones — `ztna-gateways create --scaffold` comes out with `"vendor": "Cisco"`, `"datacenter": "eu-west-1"` and a specimen `tenantIds` UUID, and `ztna-apps create --scaffold` carries `"predefinedAppId": "atlassian-cloud"`, which is irreversible once set and makes `name`/`hostnames` inherited from the template. A scaffold has always been a template to edit, but for these it can no longer be piped through unread.
- `ztna-gateways patch` can express a **partial** IPSec change as of the v1416 ingest. The restructure previewed since v1353 landed: PATCH has its own all-optional `ConnectionConfigPatch{Left,Right}Request` schemas under `GatewayIpSecPatchRequest`, so `--set ipsec.esp.lifetimeInSec=28800` sends only that and the server deep-merges it. Previously the POST-shaped schema forced a caller to resend the whole `ipsec` block, and a partial one earned a malformed-body 400. Wire-checked here on 2026-08-21 against a bogus gateway ID (body validation passed, the only error back being the 404), and end-to-end on 2026-08-24 against a real one: `security ztna-gateways patch <id> --set ipsec.esp.lifetimeInSec=14400` returned 204 and a following `get` showed `esp.lifetimeInSec` changed with `ike`, `left.subnets` and `right.subnets` untouched — the server-side deep merge is real, not merely accepted. Note a successful patch prints **nothing**: the template sends `application/merge-patch+json` and expects 204, so there is no body to render. Sending `application/json` instead answers `415 UNSUPPORTED_MEDIA_TYPE` with a differently shaped envelope (`messageKey`/`logref`/`statusCode` rather than `httpStatus`/`traceId`/`errors`) — the framework rejecting the request before the service sees it, which is why that error carries no `traceId` to quote upstream.
- Enum-constrained request fields now name their values in `--help` ("Allowed values:", one line per dotted field path), from `parser.Property.Enum` and `parser.Schema.Enum`. A `[]` suffix on the path means it is each *element* that is constrained — for an array the enum sits on the element schema, not on the property, so a properties-only walk missed six of the ZTNA gateway's IPSec cipher-suite fields (`ipsec.{esp,ike}.{dhGroups,encryption,integrity}`) while the server was requiring `ipsec.esp` and `ipsec.ike`. The same walk reaches enums nested inside array-of-object elements (`benchmarks create`'s `selectedOsVersions[].osType`, `platform-device-groups`' `criteria[].joinType`, `ztna-apps`' `groupOverrides.routingOverrides[].routing.type`). This is what a `--scaffold` cannot show: it renders an enum as `""`. It matters most for `ipsec.right.vendor`, which build v1353 turned into a fixed **case-sensitive** eleven-value enum — wire-confirmed 2026-08-20 that `"cisco"` for `"Cisco"` is rejected with `400 INVALID_FIELD` carrying **no `field`** and the generic "Request body is missing or malformed.", while the correctly-cased value returns seven properly attributed field errors. The build v1369 ingest adds `datacenter` (13 values) to `ztna-gateways create`/`patch`, which was previously a free string a caller had to guess. That same ingest put the per-datacenter **availability-zone source IPs a peer firewall must allow** into the `availabilityZones` property *description* as a markdown table — and property descriptions are not rendered anywhere in the CLI, only enums are, so that table is reachable only by reading `specs/platform/securitycloud_ztna_api.json`. The build v1401 ingest turned `ztna-grouped-gateways`' `recoveryDelayInSec` into an enum of five integers (300/1800/3600/10800/28800), **required on create**, and `0` — the value a caller gets by forgetting the field — is rejected, for every routing strategy including the two whose own prose says the field is ignored. Wire-confirmed here on `wisconsam` 2026-08-21: `0` and `42` both answer `400 [INVALID_FIELD] recoveryDelayInSec` with the whole legal set spelled out in the description, omitting it or sending `null` answers `400 … must not be null`, and a valid value gets as far as the business rule (`422 SHARED_GATEWAY_MEMBER`) — so field validation runs first and the enum can be exercised on a tenant with no dedicated gateways to group. A JSON *string* `"3600"` is coerced and accepted, which the CLI does not depend on: `--set` parses an integer-looking value into a JSON number (`internal/platform/body.go`). That exposed a gap on this side: `parseSchema` kept only `string` enum values, so a numeric enum was dropped entirely and the help listed nothing for exactly the field that most needed it. `enumValueString` now renders scalars of any type (a `float64` prints without a trailing `.0`), and a composite or null value is still dropped rather than printed as Go's formatting of a map.
- **Security Cloud creates now answer `{id, href}` instead of the created object.** `POST` on `ztna/apps`, `ztna/gateways` and `ztna/grouped-gateways` used to return the full resource; as of build v1439 they return the `CreateResponse` the spec had declared all along, so `security ztna-apps create` and `security ztna-gateways create` print two fields and anything that read `name` or `routing` off a create must now follow with a `get`. **Nothing in the spec diff reveals this** — the server adopted a shape that was already documented — so it is a server change an ingest cannot catch, found here only by running the create. Confirmed 2026-08-24 on apps and gateways alike.
- **`href` came back `null` on every create until the CLI stopped asking for gzip.** The gateway bug is real and unfixed upstream: it drops the `Location` header and nulls `href` whenever the response is **gzipped**, and returns both when it is not — 3/3 each way on `ztna/apps` (curl `--compressed` vs plain, 2026-08-24), matching the SDK's 12/12. Go's `net/http` sends `Accept-Encoding: gzip` on every request and decompresses transparently, so every create saw `null` for a field the schema declares required. `identityEncodingOnWrites` (`internal/commands/pro_platform_helpers.go`) sets the header explicitly on POST/PUT/PATCH, which opts out of Go's transparent gzip; a mutation's response is a handful of bytes, and reads keep gzip. Don't "tidy" that transport away — the null returns the moment it goes.
- v1424 made the spec agree with behaviour that was already live. `contact` is now declared required on `ztna-gateways create` — CLAUDE.md had recorded the server requiring it while the spec did not — and `right.subnets` lost `maxItems: 1` on both the create and patch schemas while `left` kept it. Both re-confirmed on the wire 2026-08-24: omitting `contact` is `400 [INVALID_FIELD] contact: must not be null`, and a create with two right subnets is a 201 that round-trips both — though **the server returns them in its own order**, so an export/re-apply round trip is not byte-stable.
- v1439's only spec change is a spelling fix, `CypherSuiteConfig` → `CipherSuiteConfig`, breaking for SDK consumers and a **no-op here**: the whole ztna family regenerates byte-identically, because schema names never reach a command. Verified by normalising the two specs under `Cypher`→`Cipher`, which leaves exactly the seven v1424 differences. The only generated change in the v1416 → v1439 ingest is `uem-connectors create --scaffold` gaining `authStrategy` and `deviceSyncAuth`.

### Legacy-to-DDM Payload Conversion

When `import-profile` processes a mobileconfig, compatible legacy payloads auto-convert to native DDM blueprint components instead of wrapping in `com.jamf.ddm-configuration-profile`. `--legacy` skips all conversion. Unsupported payload types filtered by default; `--include-unsupported` overrides.

Converters live in `internal/profileconvert/ddm_*.go`. Registry orchestration in `ddm_converter.go` (`ConvertToDDMComponents()`); converters register in `init()`. Current: passcode, safari, software-update deferrals, RSR, SoftwareUpdate profile. Multiple converters targeting the same component ID (e.g. deferrals + RSR + SoftwareUpdate → `software-update-settings`) deep-merge; orchestrator backfills missing scaffold sections.

Partial converters return `remaining` keys (extracted from shared payload types like `applicationaccess`); full converters return nil `remaining`. Components requiring complex schemas read base config from `blueprintcomponents.Scaffolds` at runtime, `clearIncluded()`, then overlay converted keys with `Included: true`. Jamf UI requires every section present — omitting sections blanks the panel.

Adding a converter: create `ddm_<name>.go`, implement `convertFunc`, register via `newXxxConverter()` in `ddm_converter.go` `init()`, add tests in `ddm_converter_test.go`.

### Shared Command Helpers

`internal/commands/protect_helpers.go` (Protect + Platform): `readInput`, `unmarshalInput`, `printResult`, `printExport`, `confirmDelete`, `confirmReplace`. `internal/commands/pro_platform_helpers.go` (Platform only): `requirePlatformClient`, `printScaffold`. `internal/security` (Security Cloud generated + hand-written commands): `ReadBody`, `ConfirmAction` — same `--file`/`--set`/`--scaffold`/`--yes` shape as Platform's `internal/platform` helpers, duplicated rather than shared since the products' generated packages can't import each other's parent (`internal/commands`) without an import cycle.

### Config File

`~/.config/jamf-cli/config.yaml` (XDG-compliant, auto-migrated from `~/.config/jamfpro-cli/` on first run)

## Conventions

- Global flags are package-level vars in `root.go` — accessed by generated commands via `CLIContext`.
- **Never declare a local flag whose name matches a root persistent one.** Cobra's `AddFlagSet` skips an inherited flag whose name is already taken and the *shorthand goes with it*, so a local `--dry-run` removed `-n` entirely (`unknown shorthand flag: 'n'`, exit 2) and the flag stopped appearing in that command's Global Flags list. Read the package var instead, the way `pro backup` and `protect backup` read `allowPartialFailure` and `protect restore` reads `dryRun`. Where the collision is unavoidable — `--output` on both `backup` commands is a destination directory, not an output format — the command's `Long` has to say the shorthand is gone, because `JAMF_CLI_ARGS='-o json'` is a documented CI mechanism that then exits 2. **The adjacent failure is inheriting one cleanly and then ignoring it**: a root persistent flag appears in a command's own `--help` whether the code reads it or not, so `protect backup` advertised `-n, --dry-run` and pruned files anyway — a documented flag that does nothing is worse than an absent one, because the operator used it as documented. Every command that inherits `-n` either honours it or says in its `Long` what it does not cover.
- Filename prefixes: `pro_` for Jamf Pro + Platform handwritten commands, `protect_` for Jamf Protect, `school_` for Jamf School, `security_` for Jamf Security Cloud (currently just `setup` — the eleven Risk/Device Lifecycle/SSE operations and the 47 gateway-served ones are all generator-owned). Platform uses `pro_platform_` infix where resource name overlaps with existing Pro resources.
- Help groups in `groups.go`, short aliases in `aliases.go` — each split into root / pro (including platform: `bp`, `cb`, `pdev`, `pdg`, `ddm`) / protect / school / security.
- Pro `overview` makes ~41 parallel API calls; Protect `overview` makes ~17.
- Classic API paths start with `/JSSResource/` and bypass `/api` prefix added by `client.Do()`. In platform gateway mode, rewritten to `/api/proclassic/` with the tenant in the `X-Tenant-Id` header.
- `NO_COLOR` env var respected (https://no-color.org).
- `--no-hints` flag / `JAMF_CLI_NO_HINTS` env (value-parsed via `strconv.ParseBool`) suppress advisory hints only, leaving the spinner and progress output intact; `--quiet` remains a strict superset that also silences both.
- **`--dry-run` is honoured per product, not centrally.** Jamf Pro gets it from `dryRunClient`, which wraps the `HTTPClient`; the Platform SDK client and the Security Cloud client cannot be wrapped that way, because their transports assert an exact success status and a synthetic response would have to guess 200 vs 201 vs 204 per operation. So both generators emit a `cliCtx.DryRun` check on every non-GET operation, reporting method, resolved path and body to **stderr** and returning — and `cliCtx.DryRun` is set right after the output formatter in `PersistentPreRunE`, *before* the product branches, because Protect, School and Security Cloud all return from there directly. The hand-written platform commands have no per-command preview, so `dryRunGuardTransport` refuses their writes with a 412 carrying a `DRY_RUN` code (a transport *error* would be retried by the SDK, hanging `-n` through the whole backoff ladder). A new generated resource inherits all of this; a new hand-written platform command either honours `-n` itself or is refused by the guard.
- **Never hand the platform SDK a retry client.** `jamfplatform.WithHTTPClient` assigns whatever it is given to the SDK's `retry.HTTPClient`, so an injected retryablehttp client becomes an inner retry loop whose policy wins. That is how `security uem-connectors create` came to send five POSTs after a 500 — retryablehttp retries any method, while the SDK's `isRetryableWriteStatus` deliberately refuses POST/PATCH on anything but 429/503 — and how the `traceId` went missing, since retryablehttp's default `ErrorHandler` discards the final response where the SDK's `PassthroughErrorHandler` keeps it. Pass a plain `*http.Client` carrying only the timeout, jar and the verbose/spinner transports.
- **An empty list prints `[]`, never `null`.** All three generators aggregate paginated results into a slice and marshal it, and a nil slice marshals to `null` — so `security ztna-gateways list -o json` answered `null` on a tenant with no gateways while `dns-zones list` answered `[]` for the byte-identical wire response (`{"totalCount":0,"results":[]}`), the difference being only which spec happens to declare `page`/`page-size`. Anything piping to `jq` then failed with "Cannot iterate over null" on exactly the tenants where the collection was empty. The slices are initialised empty in `generator/platform/template.go`, `generator/security/template.go` and `generator/parser/generator.go` (the Pro one is the `list --all` path); keep it that way when touching the aggregation loops.

## Common Workflows

### Adding a feature to all generated commands

1. Edit the template `const` in `generator/parser/generator.go` (or `classic/generator.go`).
2. If new template data needed, update `parser.Resource` / `parser.Operation` in `parser/types.go`.
3. `make generate && make test && make verify-generated`.

### Syncing specs for a new Jamf Pro version

Both routes require `JAMF_PRO_VERSION` — it is written to `specs/.spec-version`, which the Makefile bakes into the binary as `specProVersion`. Full walkthrough in `docs/sync-specs.md`.

**A. Monorepo checkout:** `make sync-specs JAMF_SERVER_PATH=/path/to/jss JAMF_PRO_VERSION=11.31.0` → review `git diff --stat -- internal/commands/pro/generated/` → `make test`.

**B. Consolidated `/api/schema/` monolith:**
1. Fetch (needs auth): `curl -H "Authorization: Bearer $JAMF_TOKEN" https://<instance>/api/schema/ -o monolith.json`
2. `make sync-spec JAMF_MONOLITH_SPEC=./monolith.json JAMF_PRO_VERSION=11.31.0`
3. Review `git diff --stat -- specs/ internal/commands/pro/generated/` → `make test`.

The public monolith is a **subset** of the monorepo specs — route B legitimately drops private endpoints, which is what `PreservedSpecs` protects.

Splitter routes each path into the filename that owns it under `specs/` (path-based layout). New paths fall through to `firstTag → TagFilenameOverrides → PascalSingular(tag)`. Components classified as **exclusive** (inlined into owning file) or **shared** (emitted to `specs/_MonolithLibrary.yaml` and referenced via external $ref).

Knobs in `generator/monolith/overrides.go`:
- `TagFilenameOverrides` — explicit tag → filename map where auto-derived PascalSingular is wrong.
- `DroppedTags` — tags whose paths must never be emitted (legacy preview endpoints shadowing canonical resources).
- `PreservedSpecs` — spec files sourced outside the public monolith (private endpoints). Splitter leaves them untouched; library files they reference are auto-preserved via $ref scan.

After ingest, any **new tag** surfaces as a new resource command and trips `TestApplyProGroups_AllCommandsGrouped` — wire into the correct `proGroupMap` entry in `internal/commands/groups.go`.

### Adding a new Jamf Security Cloud endpoint

Unlike Platform, dropping a spec into `specs/.security-source/` isn't enough by itself — the eleven known operations are hand-mapped (too few and irregular for tag/family auto-detection), so a genuinely new endpoint needs a new entry too:
1. Drop/update the spec in `specs/.security-source/`, run `make sync-security-specs` to copy it into the committed `specs/security/`.
2. Add an entry to `securityOpsByFile` in `generator/parser/security.go` (resource name, operation name, `isDestructive`/`isList` as appropriate). If it's a new spec file, also add it to `SecurityScopeForFile`.
3. `make generate && make test`.
4. Wire the new resource's `New<Resource>Cmd` into `internal/commands/security.go` if it's a new resource (existing resources just gain a new operation automatically); add to `groups.go`'s `securityGroupMap`.

### Adding handwritten commands (Pro, Protect, School, Security, Platform, new product)

See the "Where to Make Changes" table for file locations. Common pattern:
1. Create new file with appropriate prefix (`pro_`, `protect_`, `school_`, `security_`, or new product's).
2. Wire into the product's bridge (`pro.go`, `protect.go`, `school.go`, `security.go`, or `root.go`).
3. Add to `groups.go` and optionally `aliases.go`.
4. For resources needing name-to-ID lookup: add resolver method in `internal/platform/resolve.go` or `internal/protect/resolve.go`.
5. Platform commands gate `RunE` with `requirePlatformClient(cliCtx)`.
6. New product namespace: also update site (`index.html`, `style.css`, `catalog.js`) — `make verify-site` enforces.
