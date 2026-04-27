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
| Add/remove a resource in the `backup`/`diff` commands | `internal/commands/pro_resources.go` (curated allowlist; endpoints come from generated `backup_registry.go`) |
| Add a new Jamf Pro handwritten command | `internal/commands/pro_*.go` (new file + wire in `pro.go`) |
| Add a new Platform API command (blueprints, etc.) | `internal/commands/pro_blueprints.go`, `pro_compliance_benchmarks.go`, etc. (wire in `pro.go`) |
| Change Platform name-to-ID resolution | `internal/platform/resolve.go` |
| Add a new Jamf Protect command | `internal/commands/protect_*.go` (new file + wire in `protect.go`) |
| Change Protect name-to-ID resolution | `internal/protect/resolve.go` |
| Change Protect YAML import/export schemas | `internal/commands/protect_analytics.go`, `protect_ulf.go` |
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
make sync-specs             # Copy per-resource specs from jamf-pro-server repo checkout, then regenerate
make sync-spec JAMF_MONOLITH_SPEC=./monolith.json  # Split a consolidated /api/schema/ JSON into specs/, then regenerate
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
  blueprintcomponents/   Generated DDM component scaffolds (example JSON for each component type)
  profileconvert/        Mobileconfig/plist → DDM conversion, Apple schema fetching, legacy-to-native DDM payload converters (ddm_*.go)
  scope/                 Classic API scope XML types (shared by profile import and scope resolution)
  platform/              Jamf Platform helpers: name-to-ID Resolver, PrintList/PrintOne output
  protect/               Jamf Protect helpers: name-to-ID Resolver, PrintList/PrintOne output
  commands/
    root.go              Root command, shared flags, product-aware auth resolution
    groups.go / aliases.go  Help groups and short aliases (root + pro + protect + platform)
    protect_helpers.go   Shared helpers: input reading, confirmation, export formatting (Protect + Platform)
    pro_platform_helpers.go  Platform-specific helpers: requirePlatformClient gate, printScaffold
    pro.go / protect.go  Bridges wiring all subcommands under each product
    pro_*.go             Jamf Pro handwritten + Platform API commands
    protect_*.go         Jamf Protect commands
    pro/generated/       Pro generated commands from OpenAPI specs + Classic manifest
docs/site/               GitHub Pages showcase site (HTML/CSS/JS, deployed via GH Action)
generator/
  parser/                Modern API generator (OpenAPI → commands)
  classic/               Classic API generator (YAML manifest → commands)
  blueprintcomponents/   DDM component scaffold generator
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

specs/blueprint-               generator/blueprint-   internal/blueprintcomponents/scaffolds.go
  components/*.json ──────────► components/

All resources ────────────────► smoke_registry.go (every GET for smoke tests)
                               ► backup_registry.go (list+get pairs for backup/diff)

Entrypoint: generator/main.go
```

Key types in templates: `parser.Resource` (`Name`, `NameSingular`, `GoName`, `Operations`, `IsSingleton`), `parser.Operation` (`Name`, `Method`, `Path`, `IsList`, `IsDestructive`), `classic.ClassicResource`.

`ParseSpec` returns `[]*Resource` — most specs produce one, but multi-family specs (e.g. `SelfServiceBranding.yaml`) produce one per family. `IsSingleton` is true for settings-style resources (GET+PUT, no `{id}`) — they get `get` instead of `list`, skip `apply`.

See `generator/README.md` for full template reference.

### Generated Command Features

Generated commands automatically get: `apply` (name-based upsert, skipped for singletons); `get`/`update`/`delete`/`patch` with `--name` (and per-resource `--serial`/`--udid`) for single-`{id}` paths on listable non-singletons; `patch` with JSON Merge Patch (RFC 7386) + `--set key=value` + shell completion of scalar fields; `--scaffold` on `create`/`update`/`patch`. All behavior lives in the templates — don't re-document here, read `generator/parser/generator.go`.

Name-resolution helpers (in `registry.go` / `classic_registry.go`): `readApplyInput`, `extractJSONField`, `resolveNameToIDForApply`, `extractClassicName`, `resolveClassicNameToIDForApply`.

### GitHub Pages Site

Site at `docs/site/` auto-deploys on push to `main` via `.github/workflows/deploy-site.yaml`. Introspects the built binary to generate `commands.json` — command list, counts, groups, products, flags, aliases auto-update.

Adding a product or reclassifying groups needs manual edits in `index.html`, `style.css`, `catalog.js` — `make verify-site` enforces in CI. Local dev: `make site`.

### Runtime Flow

`cmd/jamf-cli/main.go` → `commands.NewRootCmd()` → `PersistentPreRunE` (resolves auth + config, determines product from command hierarchy, builds HTTP client and/or Platform SDK client) → subcommand.

Commands in `skipCommands` (config, completion, version, commands, diff, setup) bypass auth.

### Auth

Resolution order: env vars (`JAMF_TOKEN`, `JAMF_CLIENT_ID`, `JAMF_CLIENT_SECRET`, `JAMF_TENANT_ID`) > config profile.

Three methods: **token** (pre-existing bearer), **oauth2** (client credentials against instance `/api/oauth/token`), **platform** (client credentials against Jamf Platform Gateway, e.g. `https://{region}.apigw.jamf.com/auth/token`; requires `--tenant-id`; Classic paths routed through `/api/proclassic/tenant/{id}/`, modern through `/api/pro/tenant/{id}/`; also constructs `jamfplatform-go-sdk` client enabling Platform API commands).

Secrets in config use prefixed references: `env:VAR`, `file:/path`, `keychain:service/account`. Bare values in `config add-profile` are stored in keychain automatically.

### Jamf Protect Integration

Uses `jamfprotect-go-sdk` (GraphQL, SDK manages tokens/retry/pagination). No use of `internal/auth/` or `internal/client/`. Env vars: `JAMFPROTECT_URL`, `JAMFPROTECT_CLIENT_ID`, `JAMFPROTECT_CLIENT_SECRET` (falls back to `JAMF_*`).

Patterns: positional `<name>` args (not `--name` flag) resolved via `internal/protect/Resolver`. `apply` (upsert from JSON or YAML) instead of separate create/update — output of `export` can be piped to `apply`. List commands flatten to essential fields for table output; `get`/`apply` use `printResult()` (flatten for table/csv/plain, full JSON for json/yaml). Delete and apply-replace require `--yes` or interactive confirm. Granular mutations (`add-analytic`/`remove-analytic`, `add-exception`/`remove-exception`, `add-rule`/`remove-rule`) use read-modify-write and are idempotent.

Analytics and ULF additionally support `import --file`/`--dir` matching `jamf/jamfprotect` community repo YAML schema.

`protect downloads` subcommands fetch installer/uninstaller/pppc-profile/tamper-prevention-profile/root-ca/csr/websocket-auth/summary files. `protect plans config-profile <name>` downloads a `.mobileconfig` (use `--no-*` to exclude payloads, `--sign` to sign).

### Jamf Platform API Integration

Uses `jamfplatform-go-sdk` (REST, SDK manages tokens/retry/pagination). Platform commands live under `pro` and appear in "Platform:" help group. A single `auth-method: platform` profile enables both Pro API (routed through gateway) and Platform API (via SDK).

Runtime-gated: commands are always registered but `RunE` starts with `requirePlatformClient(cliCtx)` and errors with setup instructions when platform auth not configured.

Name resolution via `internal/platform/Resolver` (blueprints by name, benchmarks by title, baselines by title, device groups by name). Devices use SDK filter methods directly (UUID vs serial auto-detected by hyphen presence).

CRUD pattern matches Protect: `apply` (upsert, blueprint uses merge-patch; benchmark is create-only — SDK has no update), `get <name>`, `delete <name>`, `export <name>`. `apply --scaffold` prints create request template (auth skipped for scaffold).

Naming: `platform-` prefix where overlap with existing Pro resources (`platform-devices`, `platform-device-groups`); no prefix for unique resources (`blueprints`, `compliance-benchmarks`, `ddm-reports`).

Commands: `blueprints` (`bp`) — CRUD, deploy/undeploy, clone, scope, components, import-profile (auto DDM conversion), report. `compliance-benchmarks` (`cb`) — baselines, benchmark CRUD, rules, stats, device-results, compliance. `platform-devices` (`pdev`), `platform-device-groups` (`pdg`), `ddm-reports` (`ddm`).

### Legacy-to-DDM Payload Conversion

When `import-profile` processes a mobileconfig, compatible legacy payloads auto-convert to native DDM blueprint components instead of wrapping in `com.jamf.ddm-configuration-profile`. `--legacy` skips all conversion. Unsupported payload types filtered by default; `--include-unsupported` overrides.

Converters live in `internal/profileconvert/ddm_*.go`. Registry orchestration in `ddm_converter.go` (`ConvertToDDMComponents()`); converters register in `init()`. Current: passcode, safari, software-update deferrals, RSR, SoftwareUpdate profile. Multiple converters targeting the same component ID (e.g. deferrals + RSR + SoftwareUpdate → `software-update-settings`) deep-merge; orchestrator backfills missing scaffold sections.

Partial converters return `remaining` keys (extracted from shared payload types like `applicationaccess`); full converters return nil `remaining`. Components requiring complex schemas read base config from `blueprintcomponents.Scaffolds` at runtime, `clearIncluded()`, then overlay converted keys with `Included: true`. Jamf UI requires every section present — omitting sections blanks the panel.

Adding a converter: create `ddm_<name>.go`, implement `convertFunc`, register via `newXxxConverter()` in `ddm_converter.go` `init()`, add tests in `ddm_converter_test.go`.

### Shared Command Helpers

`internal/commands/protect_helpers.go` (Protect + Platform): `readInput`, `unmarshalInput`, `printResult`, `printExport`, `confirmDelete`, `confirmReplace`. `internal/commands/pro_platform_helpers.go` (Platform only): `requirePlatformClient`, `printScaffold`.

### Config File

`~/.config/jamf-cli/config.yaml` (XDG-compliant, auto-migrated from `~/.config/jamfpro-cli/` on first run)

## Conventions

- Global flags are package-level vars in `root.go` — accessed by generated commands via `CLIContext`.
- Filename prefixes: `pro_` for Jamf Pro + Platform handwritten commands, `protect_` for Jamf Protect. Platform uses `pro_platform_` infix where resource name overlaps with existing Pro resources.
- Help groups in `groups.go`, short aliases in `aliases.go` — each split into root / pro (including platform: `bp`, `cb`, `pdev`, `pdg`, `ddm`) / protect.
- Pro `overview` makes ~37 parallel API calls; Protect `overview` makes ~14.
- Classic API paths start with `/JSSResource/` and bypass `/api` prefix added by `client.Do()`. In platform gateway mode, rewritten to `/api/proclassic/tenant/{id}/`.
- `NO_COLOR` env var respected (https://no-color.org).

## Common Workflows

### Adding a feature to all generated commands

1. Edit the template `const` in `generator/parser/generator.go` (or `classic/generator.go`).
2. If new template data needed, update `parser.Resource` / `parser.Operation` in `parser/types.go`.
3. `make generate && make test && make verify-generated`.

### Syncing specs for a new Jamf Pro version

**A. Monorepo checkout:** `make sync-specs JAMF_SERVER_PATH=/path/to/jss` → review `git diff --stat -- internal/commands/pro/generated/` → `make test`.

**B. Consolidated `/api/schema/` monolith:**
1. Fetch (needs auth): `curl -H "Authorization: Bearer $JAMF_TOKEN" https://<instance>/api/schema/ -o monolith.json`
2. `make sync-spec JAMF_MONOLITH_SPEC=./monolith.json`
3. Review `git diff --stat -- specs/ internal/commands/pro/generated/` → `make test`.

Splitter routes each path into the filename that owns it under `specs/` (path-based layout). New paths fall through to `firstTag → TagFilenameOverrides → PascalSingular(tag)`. Components classified as **exclusive** (inlined into owning file) or **shared** (emitted to `specs/_MonolithLibrary.yaml` and referenced via external $ref).

Knobs in `generator/monolith/overrides.go`:
- `TagFilenameOverrides` — explicit tag → filename map where auto-derived PascalSingular is wrong.
- `DroppedTags` — tags whose paths must never be emitted (legacy preview endpoints shadowing canonical resources).
- `PreservedSpecs` — spec files sourced outside the public monolith (private endpoints). Splitter leaves them untouched; library files they reference are auto-preserved via $ref scan.

After ingest, any **new tag** surfaces as a new resource command and trips `TestApplyProGroups_AllCommandsGrouped` — wire into the correct `proGroupMap` entry in `internal/commands/groups.go`.

### Adding handwritten commands (Pro, Protect, Platform, new product)

See the "Where to Make Changes" table for file locations. Common pattern:
1. Create new file with appropriate prefix (`pro_`, `protect_`, or new product's).
2. Wire into the product's bridge (`pro.go`, `protect.go`, or `root.go`).
3. Add to `groups.go` and optionally `aliases.go`.
4. For resources needing name-to-ID lookup: add resolver method in `internal/platform/resolve.go` or `internal/protect/resolve.go`.
5. Platform commands gate `RunE` with `requirePlatformClient(cliCtx)`.
6. New product namespace: also update site (`index.html`, `style.css`, `catalog.js`) — `make verify-site` enforces.
