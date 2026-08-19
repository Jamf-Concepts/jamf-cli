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

Generated commands automatically get: `apply` (name-based upsert); `get`/`update`/`delete`/`patch` with `--name` (and per-resource `--serial`/`--udid`) for single-`{id}` paths; `patch` with JSON Merge Patch (RFC 7386) + `--set key=value` + shell completion of scalar fields; `--scaffold` on `create`/`update`/`patch`. When a spec declares BOTH a per-`{id}` x-action and a collection-level sibling at the same path minus `{id}` (e.g. `/deployments/{id}/computers/installation-retry` + `/deployments/computers/installation-retry`), `pairCollectionBulkActions` (in `parser.go`, run before name disambiguation) drops the bulk op and records its path on the per-`{id}` op as `BulkActionPath`; the template then adds an `--all` flag that hits the collection-level endpoint in one server-side call instead of the `{id}` one. Each generated Pro and Platform command also carries a `jamf:privileges` annotation (populated from `x-required-privileges` in the spec via `opAnnotations`) surfaced in the `commands -o json` catalog as a `privileges` array; for Pro, the privilege names are additionally appended to the 403 `permission_denied` hint at runtime (the Platform 403 hint is not wired). Classic commands carry no privilege data. All behavior lives in the templates — don't re-document here, read `generator/parser/generator.go`.

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

Three methods: **token** (pre-existing bearer), **oauth2** (client credentials against instance `/api/oauth/token`), **platform** (client credentials against Jamf Platform Gateway, e.g. `https://{region}.apigw.jamf.com/auth/token`; requires `--tenant-id`; Classic paths routed through `/api/proclassic/tenant/{id}/`, modern through `/api/pro/tenant/{id}/`; also constructs `jamfplatform-go-sdk` client enabling Platform API commands).

Secrets in config use prefixed references: `env:VAR`, `file:/path`, `keychain:service/account`. Bare values in `config add-profile` are stored in keychain automatically.

### Jamf Protect Integration

Uses `jamfprotect-go-sdk` (GraphQL, SDK manages tokens/retry/pagination). No use of `internal/auth/` or `internal/client/`. Env vars: `JAMFPROTECT_URL`, `JAMFPROTECT_CLIENT_ID`, `JAMFPROTECT_CLIENT_SECRET` (falls back to `JAMF_*`).

Patterns: positional `<name>` args (not `--name` flag) resolved via `internal/protect/Resolver`. `apply` (upsert from JSON or YAML) instead of separate create/update — output of `export` can be piped to `apply`. List commands flatten to essential fields for table output; `get`/`apply` use `printResult()` (flatten for table/csv/plain, full JSON for json/yaml). Delete and apply-replace require `--yes` or interactive confirm. Granular mutations (`add-analytic`/`remove-analytic`, `add-exception`/`remove-exception`, `add-rule`/`remove-rule`, `add-filter`/`remove-filter`) use read-modify-write and are idempotent.

`plans apply` only sends its cross-resource reference fields (`exceptionSets`, `analyticSets`, `unifiedLoggingFilterSets`, `usbControlSet`) when the input list is non-empty, so omitting or emptying one leaves the plan's existing membership unchanged rather than clearing it — use the granular `remove-*` subcommands to detach. This does **not** apply to a set's own membership list: `ulfs apply` (`filters`) and `analytic-sets apply` (`analytics`) always send the field, so an omitted or empty list clears it.

Analytics and ULF additionally support `import --file`/`--dir` matching `jamf/jamfprotect` community repo YAML schema. Both resources' `apply` accepts **either** that community schema or the SDK input shape, sniffed per document (`analyticInputFromDocument`, `ulfInputFromDocument`) — the two disagree on field names (community `actions` are objects vs the input's `analyticActions`; community `predicate` vs the input's `filter`), so decoding one as the other silently produced an empty field and the server refused it. `export | apply` is a documented pipe for both and is covered by tests for both.

`unified-logging-filter-sets` (`ulfs`, SDK v0.8.0+) groups unified logging filters for assignment to plans — the ULF analogue of `analytic-sets`. Sets carry a read-only `plans` back-reference (excluded from `export`, which stays portable by using filter names); filters carry a read-only `sets` back-reference. Attach sets to a plan via `unifiedLoggingFilterSets` in `plans apply`. The server refuses to delete a set that is still used by a plan; deleting a filter cascades it out of any sets.

**Export output shape changed for four resources** (groups, users, api-clients, exception-sets). Cross-resource references now export as **names**, not server IDs: `roles:`/`groups:`/`connection:` in place of `RoleIDs`/`GroupIDs`/`ConnectionID`, and an exception's `analytic:` alongside its `analyticUuid`. Role IDs are small sequential integers, so an ID-shaped document applied to another tenant bound to whatever role held that integer — the wrong grant, silently. Input stays backwards compatible in both directions: `rbacDocumentUsesIDs` routes an ID-shaped document down the old path, exception sets accept a uuid-only exception, and JSON keys that were PascalCase still bind because `encoding/json` matches case-insensitively (yaml.v3 does **not** — it matches the lowercased Go field name exactly, which is why the exception export deliberately carries json tags and no yaml tags except on the one genuinely new field). Anything that *parses* these commands' output needs updating.

`protect analytics overrides` manages the **tenant overlay on Jamf-managed analytics**. Jamf publishes analytics centrally: `analytics list` in a stock tenant returns ~156 rows all flagged `jamf: true`, and the server refuses `updateAnalytic` against any of them (`"This mutation may only be used for custom analytics"`). What a tenant *can* change is `tenantSeverity` and `tenantActions`, written by the separate `updateInternalAnalytic` mutation (`UpdateInternalAnalytic` in the SDK, served on `/app`). Since the definitions are identical in every tenant, that overlay is the only part of a Jamf-managed analytic worth capturing — so `overrides export` emits it keyed by analytic **name** and `overrides apply` replays it into another tenant, skipping entries that are absent or custom. `overrides apply` treats an absent half as "no override" and clears it, which is what makes re-applying the same document idempotent; use `overrides set` for partial edits, where an omitted flag leaves that half untouched.

Two traps this exposes. `analytics list` reports Jamf's **baseline** severity, not the effective one, so an analytic overridden to `Low` still displays as `High` — `overrides list` shows baseline, tenant and effective side by side. And `analytics export` emits only the community YAML definition, dropping `tenantSeverity`/`tenantActions` entirely, so it silently discards exactly the customisation a customer made. `Analytic.hash` is a revision stamp, not a content hash — it changes across a mutate-and-revert cycle that leaves content identical, so it cannot be used as a change-detection or cross-tenant identity key.

`protect backup` and `protect restore` capture and replay a whole tenant. Backup writes each object to its own file under a per-resource directory in the same portable form the matching `export` produces; restore walks that directory in dependency order, resolving every reference by name against the target as it goes. Both take `--resources` (allowlist) and `--exclude` (denylist), which compose and both shell-complete; restore adds `--dry-run` and `--include-defaults`. `protectResourceListHelp` renders the vocabulary into each command's `--help` from the table itself — hand-writing it would drift, and before that the only ways to discover a resource name were completion and reading the error from a wrong value. Backup exits non-zero when any resource failed — matching `pro backup`, because a backup that exits 0 with a resource missing is indistinguishable from a good one to the job that scheduled it — with `--allow-partial-failure` to downgrade. Object names are free text and `protectFileNameSafe` is lossy in both directions (runs of illegal characters collapse; a case-insensitive filesystem folds case), so `protectNameAllocator` appends a name-derived discriminator on collision rather than letting one object overwrite another while the count still reports two. Restore updates what exists and creates what does not — it never deletes, so an object in the tenant but absent from the backup survives. Per-object granularity is the filesystem: delete files from the backup directory to skip them (which is how you restore synthetic users while holding back real ones).

Insights and identity provider connections are captured as state documents rather than objects: the insight catalogue is Jamf-published so only the enabled/disabled split is a tenant's own data, and connections have no create API at all, so they are recorded for reference (a user or group naming one restores only where that name exists) and never replayed.

`protectResources()` in `protect_backup.go` owns the set, and its `Order` field is the correctness argument — members before the sets naming them, sets before the plans binding them, roles before groups before users. A test asserts that chain rather than the numbers. Deliberately not replayed: Jamf-managed content, tenant defaults (`protectDefaultObjects`), API clients (a new secret is issued on create), and data forwarding (its response is not its update shape and its `cloudformation` blob embeds a tenant-specific IAM ExternalId).

Wire facts established by cloning between two live tenants, none of which are guessable from the specs:

- **Jamf-published analytic UUIDs are stable across tenants** — `BlazingKeylogger` is `da360eb3-…` in both. Custom analytics get per-tenant UUIDs. An exception set therefore exports its target analytic by **name** (`analytic:`) as well as uuid, and apply rebinds the name against the target; the uuid alone was portable only for Jamf analytics. **A foreign `analyticUuid` is rejected, not silently accepted** — the server answers `createExceptionSet: Action blocked due to dependencies on this resource.`, which names neither the analytic nor the uuid nor the reason. Established by applying two documents differing only in that uuid. `exception-sets` therefore sits at `Order` 20, not alongside the analytics: the name only resolves once the analytic exists in the target.
- **Identity provider connection names are tenant-specific** (`jamf-id-db` exists in both, but `jamfmspservices-protect-jamfcloud-com` only in one), so a user or group naming a connection restores only where that name also exists.
- **RBAC reference lists must marshal as `[]`, never `null`** — a user in groups but holding no direct role otherwise fails with `input → roleIds: None is not of type 'array'`.
- **A plan's `customEngineConfig` has PascalCase JSON keys and all-lowercase YAML keys** (`MalwareRiskware` vs `malwareriskware`) because `CustomEngineConfigInput` carries json tags and no yaml tags. Modes are `PREVENT`/`REPORT`/`DISABLED`. Backup and restore are self-consistent; only a hand-written document trips on it. `plans apply` also *requires* `actionConfig` — omitting it answers `actionConfigs: contains invalid characters`, because the SDK sends the field regardless.
- **Action config `params` is `AWSJSON!`**: read back as an object, but the input wants a JSON-encoded *string*, and it cannot be omitted. The response also fills every member of the `ReportClientParams` union with zero values, and `batchConfig.sizeInBytes` returns 0 against a documented minimum of 1000. The union's `HttpClientParams` selects `headers { header value }` in full, so an HTTP report client's bearer token or API key is captured verbatim and cannot be redacted without breaking restore — `action-configs` and `data-forwarding` therefore carry a `SensitiveReason`, are written `0600` rather than `0644`, and are reported before the operator commits the tree. The legacy `ForwardSentinel.sharedKey` *is* redacted, since data forwarding is never replayed. (`ForwardSentinelV2` got this right and reports `secretExists` instead.)
- **USB control vendor and product IDs need a `0x` prefix** (`0x0781`); bare hex is rejected as "contains invalid characters".
- **`commsConfig` is always sent** by the SDK, so `protocol` must be one of `mqtt`/`wss/mqtt`/`auto`; its `fqdn` is the region-assigned IoT endpoint and the target keeps its own. That and an exception's rebound `analyticuuid` are the only two fields that legitimately differ after a clone — for the latter, equality would mean the rebinding *failed*. Verified cross-tenant on `0cf1041`: 26 documents applied, 0 failed, re-run idempotent, 24 of 32 files byte-identical and all 8 differences accounted for.
- **Data retention updates are rate-limited to once per 24 hours**, which is why `data-retention` restore compares `dataRetentionToInput(current)` against the document and writes only a real change — an unconditional write made every re-run inside that window report a failure for a resource already in the desired state.
- **Writing a setting the tenant already holds can be refused.** Disabling a change freeze that is not on answers "Tenant '...' is not in a change freeze", so `config-freeze` and `insights` compare before writing and only send real changes. `data-retention` does the same for the rate limit above. One case is still unguarded and fails a re-run: `accessGroup: true` on a connection-less local group, which `createGroup` accepts but `updateGroup` refuses with "Local groups cannot be designated as access groups".
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
- Filename prefixes: `pro_` for Jamf Pro + Platform handwritten commands, `protect_` for Jamf Protect, `school_` for Jamf School, `security_` for Jamf Security Cloud (currently just `setup` — the eleven Risk/Device Lifecycle/SSE operations are all generator-owned). Platform uses `pro_platform_` infix where resource name overlaps with existing Pro resources.
- Help groups in `groups.go`, short aliases in `aliases.go` — each split into root / pro (including platform: `bp`, `cb`, `pdev`, `pdg`, `ddm`) / protect / school / security.
- Pro `overview` makes ~41 parallel API calls; Protect `overview` makes ~17.
- Classic API paths start with `/JSSResource/` and bypass `/api` prefix added by `client.Do()`. In platform gateway mode, rewritten to `/api/proclassic/tenant/{id}/`.
- `NO_COLOR` env var respected (https://no-color.org).
- `--no-hints` flag / `JAMF_CLI_NO_HINTS` env (value-parsed via `strconv.ParseBool`) suppress advisory hints only, leaving the spinner and progress output intact; `--quiet` remains a strict superset that also silences both.

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
