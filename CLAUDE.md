# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## CRITICAL: Credential Input Policy

**Never accept credentials (passwords, tokens, client secrets) via CLI flags or stdin.** This prevents exposure in shell history, `ps` output, and CI/CD logs.

- **Human credentials** (username, password): Interactive prompts only (`term.ReadPassword`). No flags, no env vars, no stdin.
- **Machine credentials** (token, client-id, client-secret): Environment variables (`JAMF_*`, `JAMFPROTECT_*`) for CI/CD. Interactive prompts for manual use. Config profiles with `keychain:` references for persistent storage. `--token-file` for file-based CI/CD.
- **Never add** `--password`, `--token`, `--client-secret`, `--token-stdin`, or `--client-secret-stdin` flags to any command.
- **Setup commands** (`pro setup`, `protect setup`, `config add-profile`) must always prompt interactively for credentials — no flag or env var bypass.

## CRITICAL: Generated Code Boundary

**Never edit files in `internal/commands/pro/generated/`** — they are overwritten by `make generate`.

To change generated command behavior, edit the **generator templates**:
- **Modern API commands:** `generator/parser/generator.go` → `resourceTemplate` const
- **Classic API commands:** `generator/classic/generator.go` → `classicResourceTemplate` const
- **Modern registry:** `generator/parser/generator.go` → `registryTemplate` const
- **Classic registry:** `generator/classic/generator.go` → `classicRegistryTemplate` const

Templates are Go `const` strings embedded in the generator source — NOT separate `.tmpl` files.

After modifying a template: `make generate && make test`

## Where to Make Changes

| I want to... | Edit this file |
|---|---|
| Change behavior of all modern API commands | `generator/parser/generator.go` (`resourceTemplate`) |
| Change behavior of all classic API commands | `generator/classic/generator.go` (`classicResourceTemplate`) |
| Change how OpenAPI specs are parsed | `generator/parser/parser.go` |
| Change singleton detection logic | `generator/parser/parser.go` → `detectSingleton()` |
| Change multi-family spec splitting | `generator/parser/parser.go` → `splitByPathFamilies()` |
| Add/change alternate lookup fields (--serial, --udid) | `generator/parser/parser.go` → `resourceLookupFields` map |
| Fix a resource name auto-pluralization issue | `generator/parser/parser.go` → `resourceNameOverrides` map |
| Fix wrong RSQL filter field for --name lookup | `generator/parser/parser.go` → `resourceNameFieldOverrides` map |
| Fix wrong ID field extracted from list response | `generator/parser/parser.go` → `resourceIDFieldOverrides` map |
| Change how classic YAML manifest is parsed | `generator/classic/parser.go` |
| Add a new resource to the classic API | `specs/classic/resources.yaml` |
| Add/modify DDM component scaffolds | `generator/blueprintcomponents/generator.go` (generator) or `internal/blueprintcomponents/scaffolds.go` (generated output) |
| Add a new legacy-to-DDM payload converter | `internal/profileconvert/ddm_<name>.go` (new converter + register in `ddm_converter.go` init) |
| Add a new Jamf Pro handwritten command | `internal/commands/pro_*.go` (new file + wire in `pro.go`) |
| Add a new Platform API command (blueprints, etc.) | `internal/commands/pro_blueprints.go`, `pro_compliance_benchmarks.go`, etc. (wire in `pro.go`) |
| Change Platform name-to-ID resolution | `internal/platform/resolve.go` |
| Add a new Jamf Protect command | `internal/commands/protect_*.go` (new file + wire in `protect.go`) |
| Change Protect name-to-ID resolution | `internal/protect/resolve.go` |
| Change Protect YAML import/export schemas | `internal/commands/protect_analytics.go`, `protect_ulf.go` |
| Add a new cross-product command | `internal/commands/` (new file + wire in `root.go`) |
| Add a new product namespace | `internal/commands/` (e.g., `newproduct.go` + `newproduct_*.go` files) |
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
| Add a new product color to the site | CSS vars in `docs/site/style.css` (search "add new products here") + `PRODUCT_LABELS` and `CORE_SUBGROUPS` in `docs/site/catalog.js` |
| Reclassify a command group on the site | `catalog.js` — `CORE_SUBGROUPS` map, `GROUP_ORDER` array, `reclassifyCoreCommands()` |
| Add a new Jamf product namespace | Also update site: `PRODUCT_LABELS` in `catalog.js`, `--product-*` CSS vars, product badge selectors |

## Build & Dev Commands

```bash
make build                  # Build binary to bin/jamf-cli
make test                   # Run all tests (-v)
make lint                   # golangci-lint (skips generated code via .golangci.yml)
make generate               # Regenerate commands from OpenAPI specs, Classic manifest, and DDM component scaffolds
make sync-specs             # Copy specs from jamf/jss repo, then regenerate
make verify-generated       # Check that generated code is up to date (CI-safe)
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
JAMF_CLI_ARGS='--profile "My CI Profile"' bin/jamf-cli pro computers list  # Quoted values supported

# Platform gateway auth (enables both Pro API and Platform API commands)
bin/jamf-cli config add-profile my-platform --url https://eu.apigw.jamf.com --auth-method platform --tenant-id <id>
bin/jamf-cli -p my-platform pro blueprints list           # Platform API command
bin/jamf-cli -p my-platform pro computers list            # Pro API routed through gateway
JAMF_URL=https://eu.apigw.jamf.com JAMF_CLIENT_ID=... JAMF_CLIENT_SECRET=... JAMF_TENANT_ID=... bin/jamf-cli pro bp list  # Env vars
```

## Architecture

This is a CLI for the Jamf platform. The root command holds shared infrastructure (config, auth, completion). Each Jamf product gets its own namespace — `pro` for Jamf Pro and `protect` for Jamf Protect.

### Project Structure

```
internal/
  registry/              Shared interfaces: CLIContext, HTTPClient, OutputFormatter, ProtectClient, PlatformClient
  auth/                  Auth providers (OAuth2, Platform, Token) — Jamf Pro only
  client/                HTTP client with retry, auth injection, exit-code mapping — Jamf Pro only
  config/                YAML config, secret resolution, auto-migration
  blueprintcomponents/   Generated DDM component scaffolds (example JSON for each component type)
  profileconvert/        Mobileconfig/plist → DDM conversion, Apple schema fetching for default stripping, legacy-to-native DDM payload converters
  scope/                 Classic API scope XML types (shared by profile import and scope resolution)
  platform/              Jamf Platform helpers: name-to-ID Resolver, PrintList/PrintOne output
  protect/               Jamf Protect helpers: name-to-ID Resolver, PrintList/PrintOne output
  commands/
    root.go              Root command, shared flags, product-aware auth resolution
    config.go            Config subcommands (shared)
    completion.go        Shell completion (shared)
    groups.go            Help groups for root + pro + protect + platform
    aliases.go           Aliases for root + pro + protect + platform
    protect_helpers.go   Shared helpers: input reading, confirmation, export formatting (used by both Protect and Platform commands)
    pro_platform_helpers.go  Platform-specific helpers: requirePlatformClient gate, printScaffold
    pro.go               Bridge: wires all Jamf Pro + Platform commands under "pro"
    pro_*.go             Jamf Pro handwritten commands (overview, audit, etc.)
    pro_blueprints.go    Platform API: blueprint CRUD, deploy/undeploy, clone, scope, components, import-profile
    pro_compliance_benchmarks.go  Platform API: benchmark CRUD, baselines, rules, reporting
    pro_platform_devices.go       Platform API: unified device inventory + actions
    pro_platform_device_groups.go Platform API: device groups CRUD + membership
    pro_ddm_reports.go   Platform API: DDM declaration status reports
    protect.go           Bridge: wires all Jamf Protect commands under "protect"
    protect_*.go         Jamf Protect commands (CRUD, import/export, granular mutations)
    pro/
      generated/         Pro generated commands from OpenAPI specs + Classic manifest
docs/
  site/                  GitHub Pages showcase site (HTML/CSS/JS, deployed via GH Action)
generator/
  blueprintcomponents/   DDM component scaffold generator: parses OpenAPI specs → scaffolds.go
  site/                  Site data generator: introspects binary → commands.json
```

### Code Generation Pipeline

```
specs/*.yaml ──────────────► generator/parser/   ──► internal/commands/pro/generated/*.go
                               ParseSpec()            + registry.go
                               Generator.Generate()

specs/classic/resources.yaml ► generator/classic/ ──► internal/commands/pro/generated/classic_*.go
                               ParseManifest()        + classic_registry.go
                               Generator.Generate()

specs/blueprint-               generator/blueprint-   internal/blueprintcomponents/scaffolds.go
  components/*.json ──────────► components/         ──► (Scaffolds map, ShortNames map,
                               Generate()               Identifiers func)

Entrypoint: generator/main.go (runs all three generators)
```

Key types available in templates:
- **`parser.Resource`** — `Name`, `NameSingular`, `GoName`, `Description`, `Operations`, `Schemas`, `IsSingleton`
- **`parser.Operation`** — `Name`, `Method`, `Path`, `Parameters`, `RequestBody`, `IsList`, `IsDestructive`
- **`classic.ClassicResource`** — `Name`, `Path`, `CLIName`, `GoName`, `Singular`, `Operations`, `Lookups`

`ParseSpec` returns `[]*Resource` — most specs produce one resource, but specs with multiple sibling CRUD families (e.g. `SelfServiceBranding.yaml` → macos + ios) produce one per family.

`IsSingleton` is true for settings-style resources (GET+PUT, no `{id}` in any path). Singletons get a `get` command instead of `list`, use a singular CLI name, and skip `apply` (no name-based upsert for single-instance resources).

See `generator/README.md` for full template function reference and testing workflow.

### GitHub Pages Site

The site at `docs/site/` auto-deploys on every push to `main` via `.github/workflows/deploy-site.yaml`. It introspects the built binary to generate `commands.json` — no manual updates needed when commands change.

**What auto-updates:** command list, counts, groups, products, flags, aliases, version, timestamp, "New" badges (diff against previous deploy).

**What needs manual updates when adding a product:**
1. `docs/site/style.css` — add `--product-<name>` CSS variable and all `[data-product="<name>"]` selectors (search "add new products here")
2. `docs/site/catalog.js` — add to `PRODUCT_LABELS` map

**What needs manual updates when reclassifying groups:**
1. `docs/site/catalog.js` — `GROUP_ORDER` array (display order), `CORE_SUBGROUPS` map (splits Core Commands), `GETTING_STARTED_ORDER` (sort within Getting Started)

**Local dev:** `make site` builds the binary, generates `commands.json`, and serves at localhost:8080.

### Runtime Flow

`cmd/jamf-cli/main.go` --> `commands.NewRootCmd()` --> `PersistentPreRunE` (resolves auth + config) --> `pro` subcommand --> generated commands

`PersistentPreRunE` in `root.go` is the critical path: it determines the product type from the command hierarchy (commands under `protect` → Protect, under `pro` → Pro), resolves credentials through a priority chain (flags > env vars > config profile), and builds the appropriate client. For Pro, it builds the HTTP client with spinner/dry-run decorators. When platform gateway auth is detected (`PlatformOAuth2Provider`), it additionally constructs a `jamfplatform.NewClient()` SDK client for Platform API commands (blueprints, compliance-benchmarks, etc.) — both the HTTP client and Platform SDK client coexist in `CLIContext`. For Protect, it constructs a `jamfprotect.NewClient()` SDK client. Commands in `skipCommands` (config, completion, version, commands, diff, setup) bypass auth.

### Key Packages

| Package | Purpose |
|---------|---------|
| `internal/registry/` | Shared interfaces: `CLIContext`, `HTTPClient`, `OutputFormatter`, `ProtectClient`, `PlatformClient` |
| `internal/commands/` | Root command, config, completion, product bridges, Pro (`pro_*.go`), Platform (`pro_blueprints.go`, `pro_platform_*.go`, `pro_compliance_benchmarks.go`, `pro_ddm_reports.go`), and Protect (`protect_*.go`) handwritten commands |
| `internal/platform/` | Platform helpers: `Resolver` (name-to-ID mapping), `PrintList`/`PrintOne` (SDK struct output) |
| `internal/protect/` | Protect helpers: `Resolver` (name-to-ID mapping), `PrintList`/`PrintOne` (SDK struct output) |
| `internal/commands/pro/generated/` | **Generated** — all Jamf Pro API resource commands + registries |
| `internal/blueprintcomponents/` | **Generated** — DDM component scaffold JSON templates (from OpenAPI specs) |
| `internal/profileconvert/` | Mobileconfig/plist → DDM conversion, Apple schema fetching for default stripping, legacy-to-native DDM payload converters (`ddm_*.go`) |
| `internal/scope/` | Classic API scope XML types (used by profile import scope resolution) |
| `internal/client/` | HTTP client with auth injection, retry (exponential backoff, respects `Retry-After`), and exit-code mapping |
| `internal/auth/` | Provider interface with OAuth2, Platform OAuth2, and Token impls |
| `internal/config/` | YAML config load/save, secret resolution (`env:`, `file:`, `keychain:` prefixes), auto-migration from legacy path |
| `internal/output/` | Multi-format output: table, JSON, CSV, YAML, plain. Table has smart column selection, date formatting, status colorization |
| `internal/keychain/` | System keychain abstraction (macOS Keychain, Linux secret-service) |
| `internal/exitcode/` | Structured exit codes (0-6) mapped from HTTP status codes |
| `internal/spinner/` | Terminal spinner for non-quiet, non-verbose mode |

### Auth Resolution Order

1. Environment variables (`JAMF_TOKEN`, `JAMF_CLIENT_ID`, `JAMF_CLIENT_SECRET`, `JAMF_TENANT_ID`)
2. Config profile (resolved via `--profile`, `JAMF_PROFILE`, or `default-profile` in config)

Three auth methods are supported:
- **token** — Pre-existing bearer token, passed directly in Authorization header
- **oauth2** — Client credentials flow against the instance's `/api/oauth/token` endpoint
- **platform** — Client credentials flow against the Jamf Platform Gateway (e.g., `https://{region}.apigw.jamf.com/auth/token`). Requires `--tenant-id` for URL path rewriting: Classic API paths are routed through `/api/proclassic/tenant/{id}/`, modern API paths through `/api/pro/tenant/{id}/`. Additionally constructs a `jamfplatform-go-sdk` client (`CLIContext.PlatformClient`) enabling Platform API commands (blueprints, compliance-benchmarks, devices, device-groups, DDM reports)

Secret values in config use prefixed references: `env:VAR`, `file:/path`, `keychain:service/account`. Bare values passed to `config add-profile` are stored in the system keychain automatically.

### Generated Command Interfaces

Generated commands depend on shared interfaces defined in `internal/registry/`:
- `HTTPClient` — `Do(ctx, method, path, body) (*http.Response, error)`
- `OutputFormatter` — `PrintResponse(resp)` and `PrintRaw(data)`

`root.go` bridges these with adapter types (`cliClient`, `cliOutput`) and decorators (`spinnerClient`, `dryRunClient`).

`cliOutput.PrintRaw` has a `--field` override: when `fieldName` is set, it parses JSON and extracts the named field from each object instead of delegating to the formatter. Generated `create`/`update` commands include a `--scaffold` flag that prints a JSON template (produced by `scaffoldJSON()` in the generator).

### Generated Apply (Upsert) Commands

Resources with both `create` (POST) and `update` (PUT) operations automatically get an `apply` subcommand, **unless they are singletons** (no `{id}` path — upsert by name makes no sense when there is only one instance). Classic API resources additionally require `name` in their lookups. Apply performs a name-based upsert:

1. Reads input from `--from-file` or stdin (JSON for modern API, XML for classic API)
2. Extracts the name field from input (uses `NameField` — either `name` or `displayName` for modern; searches `<name>` or `<general><name>` for classic XML)
3. Checks if a resource with that name already exists
4. **0 matches** → creates the resource (POST)
5. **1 match** → confirms replacement (`--yes` skips), then updates (PUT with matched ID)
6. **2+ matches** → collision: prompts user to pick which ID to replace interactively; errors if `--no-input` is set

Flags: `--from-file`, `--yes` (skip replacement confirmation only — not collision resolution), `--dry-run` (resolves existence then prints what would happen).

### Generated Lookup Flags on get/update/delete/patch

Any operation with a single `{id}` path parameter on a non-singleton, listable resource gets `--name` (and per-resource fields like `--serial`, `--udid`) folded directly into that command — no separate `get-by-name` or `delete-by-name` subcommands. These flags are defined in `opHasNameLookup` (modern) and the classic template (classic).

- `get [<id>]` — accepts `--name` to resolve by name via RSQL filter
- `update [<id>]` — accepts `--name` to resolve before reading stdin body
- `delete [<id>]` — accepts `--name` to resolve, then confirms and deletes
- `patch [<id>]` — accepts `--name` (same pattern, implemented via `patchHasLookup`)

Classic API: `get` and `update` use URL-based name resolution (`/JSSResource/path/name/<value>`); `delete` uses `resolveClassicNameToIDForApply` (list + client-side filter).

### Generated Patch Commands

Resources with a PATCH endpoint (JSON, not multipart) automatically get a `patch` subcommand using JSON Merge Patch semantics (RFC 7386): omit a field to leave it unchanged, set to `null` to clear it. The `Content-Type` is always `application/merge-patch+json`.

The `patch` command accepts an optional `[<id>]` positional argument. For resources with a list endpoint and resolvable ID (`patchHasLookup`), additional lookup flags are generated:
- `--name` — look up by name via RSQL filter
- Per-resource lookup fields (e.g. `--serial`, `--udid` for computers and mobile devices) — defined in `resourceLookupFields` in `parser.go`

If no ID or lookup flag is provided the command errors with a clear message listing the available options.

Flags: `--set key=value` (repeatable, dot-notation scalar fields), `--from-file` (JSON merge-patch file), `--scaffold` (print patchable field template), `--name`/`--serial`/`--udid` etc. (lookup).

Shell completion for `--set` is baked in at code-gen time: all non-read-only scalar fields from the PATCH request body schema, including nested object fields resolved via `Property.Nested` (populated from kin-openapi's inline `$ref` resolution).

Resource name overrides (e.g. `computers-inventories` → `computers-inventory`) are applied by `ApplyNameOverrides` after `DeduplicateVersioned`. Add entries to `resourceNameOverrides` in `parser.go` for any future auto-pluralization issues.

### Name Resolution Helpers (Generated)

Helper functions in generated code (shared by `apply`, `get`, `update`, `delete`, and `patch`):
- `readApplyInput(fromFile)` — reads from file or stdin (in `registry.go`)
- `extractJSONField(data, field)` — extracts name from JSON (in `registry.go`)
- `resolveNameToIDForApply(ctx, client, listPath, nameField, name, noInput)` — collision-aware RSQL filter lookup (in `registry.go`)
- `extractClassicName(data, singularKey)` — extracts name from XML via `xmlconv.ToMap` (in `classic_registry.go`)
- `resolveClassicNameToIDForApply(ctx, client, apiPath, wrapperKey, name, noInput)` — fetches list, filters client-side (in `classic_registry.go`)

### Jamf Protect Integration

Protect uses the `jamfprotect-go-sdk` (GraphQL-based, not REST). The SDK handles its own OAuth2 auth, retry, and pagination. Key differences from Pro:

- **Auth**: SDK manages tokens internally. `PersistentPreRunE` resolves credentials and passes them to `jamfprotect.NewClient()`. No use of `internal/auth/` or `internal/client/`. Env vars: `JAMFPROTECT_URL`, `JAMFPROTECT_CLIENT_ID`, `JAMFPROTECT_CLIENT_SECRET` (also falls back to `JAMF_URL`, `JAMF_CLIENT_ID`, `JAMF_CLIENT_SECRET`).
- **Config profiles**: Protect profiles have `product: protect` and `auth-method: oauth2`. Product type is also auto-detected from the command hierarchy (`jamf-cli protect ...` always uses Protect auth).
- **Name resolution**: All `get`/`delete`/`export` commands take a positional `<name>` arg (not `--name` flag), matching Pro's pattern. `internal/protect/Resolver` maps names to IDs via lazy-cached list calls.
- **CRUD pattern**: Protect uses `apply` (upsert) instead of separate `create`/`update`. `apply` reads JSON or YAML input (from `--from-file` or stdin), checks if the resource exists by name, and creates or updates accordingly. Replacing an existing resource prompts for confirmation (skippable with `--yes`).
- **Export**: Every resource has an `export <name>` command that outputs the SDK input format (not the API response). Respects `-o` flag: JSON by default, YAML with `-o yaml`. Export output can be piped directly to `apply`.
- **Output**: List commands use flatten functions to show only essential fields in table mode. `get`/`apply`/mutation commands use `printResult()` which flattens for table/csv/plain and shows full JSON for json/yaml output.
- **YAML import/export**: Analytics and unified logging filters additionally support `import --file`/`--dir` with YAML files matching the `jamf/jamfprotect` community repo schema. Import is upsert (creates or updates by name).
- **Granular mutations**: `add-analytic`/`remove-analytic` on analytic sets, `add-exception`/`remove-exception` on exception sets, `add-rule`/`remove-rule` on removable storage control sets use read-modify-write pattern. These are idempotent: adding a duplicate is a no-op (or replaces for rules, with `--yes` confirmation).
- **Downloads**: `protect downloads` has subcommands for downloading actual files: `installer`, `uninstaller`, `pppc-profile`, `tamper-prevention-profile`, `root-ca`, `csr`, `websocket-auth`, `summary`. Profiles/certs are base64-decoded and written as files. Packages are downloaded via authenticated HTTP.
- **Plans config-profile**: `protect plans config-profile <name>` downloads a `.mobileconfig` file with all payloads included by default. Use `--no-*` flags to exclude specific payloads (pppc, token, ca, csr, websocket, system-extension, service-management). `--sign` cryptographically signs the profile.

### Jamf Platform API Integration

Platform commands use the `jamfplatform-go-sdk` (REST-based). The SDK handles its own OAuth2 auth, retry, and pagination. Platform commands live under the `pro` namespace (not a separate product) and appear in the "Platform:" help group.

- **Auth**: SDK manages tokens internally. `PersistentPreRunE` detects `PlatformOAuth2Provider`, extracts credentials via `ClientID()`/`ClientSecret()` getters, and passes them to `jamfplatform.NewClient()`. The Platform SDK client coexists with the Pro HTTP client in `CLIContext` — a single `auth-method: platform` profile enables both Pro API commands (routed through gateway) and Platform API commands (via SDK).
- **Runtime gating**: Platform commands are always registered (visible in `pro --help`) but check `requirePlatformClient(cliCtx)` at the top of `RunE`. When platform auth isn't configured, users get a clear error with setup instructions.
- **Name resolution**: `internal/platform/Resolver` maps names to IDs via lazy-cached list calls (blueprints by name, benchmarks by title, baselines by title, device groups by name). Devices use SDK filter methods directly (serial number auto-detection: UUIDs contain hyphens, serials don't).
- **CRUD pattern**: Same as Protect — `apply` (upsert with `--from-file`/stdin), `get <name>`, `delete <name>` with confirmation, `export <name>` as JSON/YAML. Blueprint `apply` uses merge-patch for updates. Benchmark `apply` is create-only (SDK has no update).
- **Scaffold**: `apply --scaffold` prints a JSON template of the create request type with placeholder values. Auth is skipped for scaffold (global behavior in `PersistentPreRunE`).
- **Naming**: `platform-` prefix where overlap with existing Pro API resources (`platform-devices`, `platform-device-groups`). No prefix for unique resources (`blueprints`, `compliance-benchmarks`, `ddm-reports`).

**Platform commands:**
- `pro blueprints` (`bp`) — CRUD, deploy/undeploy, clone, scope (add/remove/list), components (list/get/scaffold/configuration-profile/configuration-profile-plist), import-profile (with automatic DDM conversion), report
- `pro compliance-benchmarks` (`cb`) — baselines, benchmark CRUD, rules, stats, device-results, compliance
- `pro platform-devices` (`pdev`) — list, get, update, delete, apps, groups, user, check-in, erase, restart, shutdown, unmanage
- `pro platform-device-groups` (`pdg`) — CRUD, members, add-members, remove-members
- `pro ddm-reports` (`ddm`) — device declaration report, declaration clients

### Legacy-to-DDM Payload Conversion (`internal/profileconvert/ddm_*.go`)

When `import-profile` processes a mobileconfig, it automatically converts compatible legacy payloads to native DDM blueprint components instead of wrapping everything in `com.jamf.ddm-configuration-profile`. Payloads without a converter are still wrapped.

**Converter registry** (`ddm_converter.go`): `ConvertToDDMComponents()` orchestrates conversion. For each payload, it checks `findConverters(payloadType)` and runs matching converters sequentially. For payload types with multiple converters (e.g. `com.apple.applicationaccess` has both safari and software-update), each converter extracts its keys and passes the remainder to the next. Final remaining keys go to the configuration-profile wrapper.

**Current converters:**

| Converter | Legacy Payload | DDM Component | Notes |
|-----------|---------------|---------------|-------|
| `ddm_passcode.go` | `com.apple.mobiledevice.passwordpolicy` | `com.jamf.ddm.passcode-settings` | Full 1:1 conversion. 12 key mappings + `customRegex` nested restructure. `allowSimple` → `RequireComplexPasscode` boolean inversion. Adds `version: "2"`. |
| `ddm_safari.go` | `com.apple.applicationaccess` (safari keys) | `com.jamf.ddm.safari-settings` | Partial — extracts 7 safari-prefixed keys, leaves rest in wrapper. Cookie policy numeric→enum conversion. Requires macOS/iOS 26+. |
| `ddm_softwareupdate.go` | `com.apple.applicationaccess` (deferral keys) | `com.jamf.ddm.software-update-settings` | Partial — extracts `forceDelayed*`/`enforced*Delay` keys. Builds full component schema from `blueprintcomponents.Scaffolds` with `clearIncluded()`, then overlays converted deferrals. |

**Key design decisions:**
- Key mapping tables are static (legacy key names differ completely from DDM names — no algorithmic derivation possible). Validated against Apple's published schemas.
- Each converter returns `(config, remaining, warnings, error)`. `remaining` is nil for full converters (passcode), non-nil for partial converters (safari, software-update).
- The software-update converter reads its base config from `blueprintcomponents.Scaffolds` at runtime, so it auto-updates when `make generate` runs against new OpenAPI specs. The scaffold's placeholder values are sanitised (Beta section stripped of empty strings that fail API validation). All `Included` flags are set to `false` on the base, then converted keys overlay with `Included: true`.
- The Jamf UI requires every section of a component to be present in the JSON — omitting sections causes the component panel to render blank, even if other sections have valid data.

**Adding a new converter:**
1. Create `internal/profileconvert/ddm_<name>.go`
2. Implement a `convertFunc` and register via `newXxxConverter()` in `ddm_converter.go` `init()`
3. For partial converters (extracting from a shared payload type like `applicationaccess`): return unconsumed keys in `remaining`
4. For components with complex schemas: read base config from `blueprintcomponents.Scaffolds`, `clearIncluded()`, then overlay
5. Add tests in `ddm_converter_test.go`

### Shared Command Helpers (`protect_helpers.go`)

These helpers are shared by both Protect and Platform commands (originally Protect-only, renamed to generic names):

| Helper | Purpose |
|--------|---------|
| `readInput(fromFile)` | Reads JSON/YAML from `--from-file` or stdin pipe |
| `unmarshalInput(data, target)` | Tries JSON then YAML unmarshal |
| `printResult(out, item, flattened)` | Table-aware output (flatten for table, full struct for json/yaml) |
| `printExport(data)` | Export as JSON or YAML based on `-o` flag |
| `confirmDelete(type, name, yes)` | Delete confirmation with dry-run support |
| `confirmReplace(type, name, yes)` | Replace confirmation for `apply` upsert |

### Platform-Specific Helpers (`pro_platform_helpers.go`)

| Helper | Purpose |
|--------|---------|
| `requirePlatformClient(cliCtx)` | Returns error if Platform SDK client is nil (no platform auth) |
| `printScaffold(v)` | Marshals a struct to indented JSON for `--scaffold` output |

### Config File

`~/.config/jamf-cli/config.yaml` (XDG-compliant, auto-migrated from `~/.config/jamfpro-cli/` on first run)

## Conventions

- Global flags are package-level vars in `root.go` (not struct fields) — accessed by generated commands via the `CLIContext` struct.
- Jamf Pro commands use the `pro_` filename prefix (e.g., `pro_overview.go`, `pro_audit.go`).
- Platform API commands under Pro use `pro_blueprints.go`, `pro_compliance_benchmarks.go`, `pro_platform_devices.go`, `pro_platform_device_groups.go`, `pro_ddm_reports.go`. The `platform_` infix is used where the resource name overlaps with existing Pro resources.
- Jamf Protect commands use the `protect_` filename prefix (e.g., `protect_overview.go`, `protect_analytics.go`).
- Command grouping for `--help` output is maintained in `groups.go` — root groups, pro groups (including platform), and protect groups are separate. Platform commands use the `groupPlatform` group.
- Short aliases (e.g., `comp` for `computers`) are in `aliases.go` — split into root, pro (including platform: `bp`, `cb`, `pdev`, `pdg`, `ddm`), and protect aliases.
- The Pro `overview` command makes ~37 parallel API calls to produce an instance dashboard — it's the most complex handwritten command. The Protect `overview` makes ~14 parallel calls.
- Protect `apply` commands accept both JSON and YAML input. `export` output can be piped directly to `apply` for round-tripping.
- Protect list commands use flatten functions for clean table output (essential fields only). Full detail is available via `get` (json/yaml output) or `export` (input-compatible format).
- Protect delete and apply-replace operations require `--yes` or interactive confirmation. Dry-run mode (`-n`) prints what would happen without executing.
- Classic API paths start with `/JSSResource/` and bypass the `/api` prefix that `client.Do()` adds for modern paths. In platform gateway mode, they are rewritten to `/api/proclassic/tenant/{id}/` paths.
- `NO_COLOR` env var is respected for CI/scripting (https://no-color.org).

## Common Workflows

### Adding a feature to all generated commands

1. Edit the template `const` in `generator/parser/generator.go` (or `classic/generator.go`)
2. If new template data is needed, update `parser.Resource` / `parser.Operation` in `parser/types.go`
3. Run: `make generate && make test`
4. Verify: `make verify-generated`

### Syncing specs for a new Jamf Pro version

1. `make sync-specs JAMF_SERVER_PATH=/path/to/jss`
2. Review: `git diff --stat -- internal/commands/pro/generated/`
3. Run: `make test`

### Adding a new Jamf Pro handwritten command

1. Create new file in `internal/commands/` with `pro_` prefix (e.g., `pro_mycommand.go`)
2. Wire it into the pro command in `pro.go`
3. Add to the appropriate group in `groups.go` (`proGroupMap`)
4. Optionally add a short alias in `aliases.go` (`commandAliases`)

### Adding a new Jamf Protect CRUD resource

1. Create `internal/commands/protect_myresource.go` with:
   - `newProtectMyResourceCmd()` — parent, wires subcommands
   - `newProtectMyResourceListCmd()` — flatten output for table, use `json.Marshal(rows)` + `PrintRaw`
   - `newProtectMyResourceGetCmd()` — positional `<name>` arg, resolve via `protect.NewResolver`, use `printResult` for table-aware output
   - `newProtectMyResourceApplyCmd()` — upsert with `readInput`/`unmarshalInput`, resolve name, create-or-update, `--yes` for replace confirmation
   - `newProtectMyResourceDeleteCmd()` — positional `<name>` arg, `--yes` flag, `confirmDelete`
   - `newProtectMyResourceExportCmd()` — positional `<name>` arg, convert API response to input type, output via `printExport`
   - `flattenMyResource()` — essential fields only for table output
   - `myResourceToInput()` — strip server fields for export
2. Wire into `protect.go`: `cmd.AddCommand(newProtectMyResourceCmd(cliCtx))`
3. Add to `groups.go` (`protectGroupMap`)
4. Optionally add alias in `aliases.go` (`protectAliases`)
5. Add resolver method in `internal/protect/resolve.go`
6. Add tests in `protect_test.go` (subcommands), `protect_conversions_test.go` (flatten/toInput)

### Adding a new Platform API resource

1. Create `internal/commands/pro_myresource.go` (use `pro_platform_` prefix if name overlaps with existing Pro resources) with:
   - `newMyResourceCmd()` — parent, wires subcommands
   - Each subcommand starts with `requirePlatformClient(cliCtx)` gate
   - `newMyResourceListCmd()` — flatten output for table, `json.Marshal(rows)` + `PrintRaw`
   - `newMyResourceGetCmd()` — positional `<name>` arg, resolve via `platform.NewResolver`, use `printResult` for table-aware output or `platform.PrintOne` for full JSON
   - `newMyResourceApplyCmd()` — upsert with `readInput`/`unmarshalInput`, resolve name, create-or-update. Add `--scaffold` flag with `printScaffold()`. `--yes` for replace confirmation
   - `newMyResourceDeleteCmd()` — positional `<name>` arg, `--yes` flag, `confirmDelete`
   - `newMyResourceExportCmd()` — positional `<name>` arg, strip server fields, output via `printExport`
   - `flattenMyResource()` — essential fields only for table output
   - `myResourceScaffold()` — returns example create request struct for `--scaffold`
2. Add the `PlatformClient` interface methods in `internal/registry/registry.go` if wrapping new SDK methods
3. Wire into `pro.go`: `cmd.AddCommand(newMyResourceCmd(cliCtx))`
4. Add to `groups.go` (`proGroupMap` → `groupPlatform`)
5. Optionally add alias in `aliases.go` (`commandAliases`)
6. Add resolver method in `internal/platform/resolve.go` if name-to-ID lookup needed

### Adding a new product namespace

1. Create `internal/commands/newproduct.go` as the bridge (like `pro.go`)
2. Create `internal/commands/newproduct_*.go` for handwritten commands
3. If the product has generated commands, create `internal/commands/newproduct/generated/`
4. Wire the bridge into `root.go`
5. Add root group mapping in `groups.go` (`rootGroupMap`)
