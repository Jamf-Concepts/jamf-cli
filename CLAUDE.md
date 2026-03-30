# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

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
| Change how classic YAML manifest is parsed | `generator/classic/parser.go` |
| Add a new resource to the classic API | `specs/classic/resources.yaml` |
| Add a new Jamf Pro handwritten command | `internal/commands/pro_*.go` (new file + wire in `pro.go`) |
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

## Build & Dev Commands

```bash
make build                  # Build binary to bin/jamf-cli
make test                   # Run all tests (-v)
make lint                   # golangci-lint (skips generated code via .golangci.yml)
make generate               # Regenerate commands from OpenAPI specs + Classic manifest
make sync-specs             # Copy specs from jamf/jss repo, then regenerate
make verify-generated       # Check that generated code is up to date (CI-safe)
make fmt                    # go fmt + gofumpt
go test -v -run TestFoo ./internal/commands/...  # Run a single test
```

### Running the CLI

```bash
bin/jamf-cli pro setup                    # Interactive first-time config (creates Jamf Pro profile)
bin/jamf-cli protect setup                # Interactive first-time config (creates Jamf Protect profile)
bin/jamf-cli --url https://... --token ... pro computers list  # One-off with flags
bin/jamf-cli -p my-protect-profile protect overview            # Use a named protect profile
```

## Architecture

This is a CLI for the Jamf platform. The root command holds shared infrastructure (config, auth, completion). Each Jamf product gets its own namespace — `pro` for Jamf Pro and `protect` for Jamf Protect.

### Project Structure

```
internal/
  registry/              Shared interfaces: CLIContext, HTTPClient, OutputFormatter, ProtectClient
  auth/                  Auth providers (OAuth2, Platform, Token) — Jamf Pro only
  client/                HTTP client with retry, auth injection, exit-code mapping — Jamf Pro only
  config/                YAML config, secret resolution, auto-migration
  protect/               Jamf Protect helpers: name-to-ID Resolver, PrintList/PrintOne output
  commands/
    root.go              Root command, shared flags, product-aware auth resolution
    config.go            Config subcommands (shared)
    completion.go        Shell completion (shared)
    groups.go            Help groups for root + pro + protect
    aliases.go           Aliases for root + pro + protect
    protect_helpers.go   Shared Protect helpers: input reading, confirmation, export formatting
    pro.go               Bridge: wires all Jamf Pro commands under "pro"
    pro_*.go             Jamf Pro handwritten commands (overview, audit, etc.)
    protect.go           Bridge: wires all Jamf Protect commands under "protect"
    protect_*.go         Jamf Protect commands (CRUD, import/export, granular mutations)
    pro/
      generated/         Pro generated commands from OpenAPI specs + Classic manifest
```

### Code Generation Pipeline

```
specs/*.yaml ──────────────► generator/parser/   ──► internal/commands/pro/generated/*.go
                               ParseSpec()            + registry.go
                               Generator.Generate()

specs/classic/resources.yaml ► generator/classic/ ──► internal/commands/pro/generated/classic_*.go
                               ParseManifest()        + classic_registry.go
                               Generator.Generate()

Entrypoint: generator/main.go (runs both generators)
```

Key types available in templates:
- **`parser.Resource`** — `Name`, `NameSingular`, `GoName`, `Description`, `Operations`, `Schemas`
- **`parser.Operation`** — `Name`, `Method`, `Path`, `Parameters`, `RequestBody`, `IsList`, `IsDestructive`
- **`classic.ClassicResource`** — `Name`, `Path`, `CLIName`, `GoName`, `Singular`, `Operations`, `Lookups`

See `generator/README.md` for full template function reference and testing workflow.

### Runtime Flow

`cmd/jamf-cli/main.go` --> `commands.NewRootCmd()` --> `PersistentPreRunE` (resolves auth + config) --> `pro` subcommand --> generated commands

`PersistentPreRunE` in `root.go` is the critical path: it determines the product type from the command hierarchy (commands under `protect` → Protect, under `pro` → Pro), resolves credentials through a priority chain (flags > env vars > config profile), and builds the appropriate client. For Pro, it builds the HTTP client with spinner/dry-run decorators. For Protect, it constructs a `jamfprotect.NewClient()` SDK client. Commands in `skipCommands` (config, completion, version, commands, diff, setup) bypass auth.

### Key Packages

| Package | Purpose |
|---------|---------|
| `internal/registry/` | Shared interfaces: `CLIContext`, `HTTPClient`, `OutputFormatter`, `ProtectClient` |
| `internal/commands/` | Root command, config, completion, product bridges, Pro (`pro_*.go`) and Protect (`protect_*.go`) handwritten commands |
| `internal/protect/` | Protect helpers: `Resolver` (name-to-ID mapping), `PrintList`/`PrintOne` (SDK struct output) |
| `internal/commands/pro/generated/` | **Generated** — all Jamf Pro API resource commands + registries |
| `internal/client/` | HTTP client with auth injection, retry (exponential backoff, respects `Retry-After`), and exit-code mapping |
| `internal/auth/` | Provider interface with OAuth2, Platform OAuth2, and Token impls |
| `internal/config/` | YAML config load/save, secret resolution (`env:`, `file:`, `keychain:` prefixes), auto-migration from legacy path |
| `internal/output/` | Multi-format output: table, JSON, CSV, YAML, plain. Table has smart column selection, date formatting, status colorization |
| `internal/keychain/` | System keychain abstraction (macOS Keychain, Linux secret-service) |
| `internal/exitcode/` | Structured exit codes (0-6) mapped from HTTP status codes |
| `internal/spinner/` | Terminal spinner for non-quiet, non-verbose mode |

### Auth Resolution Order

1. CLI flags (`--token`, `--client-id`/`--client-secret`, `--tenant-id`)
2. Environment variables (`JAMF_TOKEN`, `JAMF_CLIENT_ID`, `JAMF_CLIENT_SECRET`, `JAMF_TENANT_ID`)
3. Config profile (resolved via `--profile`, `JAMF_PROFILE`, or `default-profile` in config)

Three auth methods are supported:
- **token** — Pre-existing bearer token, passed directly in Authorization header
- **oauth2** — Client credentials flow against the instance's `/api/oauth/token` endpoint
- **platform** — Client credentials flow against the Jamf Platform Gateway (e.g., `https://{region}.apigw.jamf.com/auth/token`). Requires `--tenant-id` for URL path rewriting: Classic API paths are routed through `/api/proclassic/tenant/{id}/`, modern API paths through `/api/pro/tenant/{id}/`

Secret values in config use prefixed references: `env:VAR`, `file:/path`, `keychain:service/account`. Bare values passed to `config add-profile` are stored in the system keychain automatically.

### Generated Command Interfaces

Generated commands depend on shared interfaces defined in `internal/registry/`:
- `HTTPClient` — `Do(ctx, method, path, body) (*http.Response, error)`
- `OutputFormatter` — `PrintResponse(resp)` and `PrintRaw(data)`

`root.go` bridges these with adapter types (`cliClient`, `cliOutput`) and decorators (`spinnerClient`, `dryRunClient`).

`cliOutput.PrintRaw` has a `--field` override: when `fieldName` is set, it parses JSON and extracts the named field from each object instead of delegating to the formatter. Generated `create`/`update` commands include a `--scaffold` flag that prints a JSON template (produced by `scaffoldJSON()` in the generator).

### Jamf Protect Integration

Protect uses the `jamfprotect-go-sdk` (GraphQL-based, not REST). The SDK handles its own OAuth2 auth, retry, and pagination. Key differences from Pro:

- **Auth**: SDK manages tokens internally. `PersistentPreRunE` resolves credentials and passes them to `jamfprotect.NewClient()`. No use of `internal/auth/` or `internal/client/`. Env vars: `JAMFPROTECT_URL`, `JAMFPROTECT_CLIENT_ID`, `JAMFPROTECT_CLIENT_SECRET` (also falls back to `JAMF_URL`, `JAMF_CLIENT_ID`, `JAMF_CLIENT_SECRET`).
- **Config profiles**: Protect profiles have `product: protect` and `auth-method: oauth2`. Product type is also auto-detected from the command hierarchy (`jamf-cli protect ...` always uses Protect auth).
- **Name resolution**: All `get`/`delete`/`export` commands take a positional `<name>` arg (not `--name` flag), matching Pro's pattern. `internal/protect/Resolver` maps names to IDs via lazy-cached list calls.
- **CRUD pattern**: Protect uses `apply` (upsert) instead of separate `create`/`update`. `apply` reads JSON or YAML input (from `--from-file` or stdin), checks if the resource exists by name, and creates or updates accordingly. Replacing an existing resource prompts for confirmation (skippable with `--yes`).
- **Export**: Every resource has an `export <name>` command that outputs the SDK input format (not the API response). Respects `-o` flag: JSON by default, YAML with `-o yaml`. Export output can be piped directly to `apply`.
- **Output**: List commands use flatten functions to show only essential fields in table mode. `get`/`apply`/mutation commands use `printProtectResult()` which flattens for table/csv/plain and shows full JSON for json/yaml output.
- **YAML import/export**: Analytics and unified logging filters additionally support `import --file`/`--dir` with YAML files matching the `jamf/jamfprotect` community repo schema. Import is upsert (creates or updates by name).
- **Granular mutations**: `add-analytic`/`remove-analytic` on analytic sets, `add-exception`/`remove-exception` on exception sets, `add-rule`/`remove-rule` on removable storage control sets use read-modify-write pattern. These are idempotent: adding a duplicate is a no-op (or replaces for rules, with `--yes` confirmation).
- **Downloads**: `protect downloads` has subcommands for downloading actual files: `installer`, `uninstaller`, `pppc-profile`, `tamper-prevention-profile`, `root-ca`, `csr`, `websocket-auth`, `summary`. Profiles/certs are base64-decoded and written as files. Packages are downloaded via authenticated HTTP.
- **Plans config-profile**: `protect plans config-profile <name>` downloads a `.mobileconfig` file with all payloads included by default. Use `--no-*` flags to exclude specific payloads (pppc, token, ca, csr, websocket, system-extension, service-management). `--sign` cryptographically signs the profile.

### Protect Command Helpers (`protect_helpers.go`)

| Helper | Purpose |
|--------|---------|
| `readProtectInput(fromFile)` | Reads JSON/YAML from `--from-file` or stdin pipe |
| `unmarshalProtectInput(data, target)` | Tries JSON then YAML unmarshal |
| `printProtectResult(out, item, flattened)` | Table-aware output (flatten for table, full struct for json/yaml) |
| `printProtectExport(data)` | Export as JSON or YAML based on `-o` flag |
| `confirmProtectDelete(type, name, yes)` | Delete confirmation with dry-run support |
| `confirmProtectReplace(type, name, yes)` | Replace confirmation for `apply` upsert |

### Config File

`~/.config/jamf-cli/config.yaml` (XDG-compliant, auto-migrated from `~/.config/jamfpro-cli/` on first run)

## Conventions

- Global flags are package-level vars in `root.go` (not struct fields) — accessed by generated commands via the `CLIContext` struct.
- Jamf Pro commands use the `pro_` filename prefix (e.g., `pro_overview.go`, `pro_audit.go`).
- Jamf Protect commands use the `protect_` filename prefix (e.g., `protect_overview.go`, `protect_analytics.go`).
- Command grouping for `--help` output is maintained in `groups.go` — root groups, pro groups, and protect groups are separate.
- Short aliases (e.g., `comp` for `computers`) are in `aliases.go` — split into root, pro, and protect aliases.
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
   - `newProtectMyResourceGetCmd()` — positional `<name>` arg, resolve via `protect.NewResolver`, use `printProtectResult` for table-aware output
   - `newProtectMyResourceApplyCmd()` — upsert with `readProtectInput`/`unmarshalProtectInput`, resolve name, create-or-update, `--yes` for replace confirmation
   - `newProtectMyResourceDeleteCmd()` — positional `<name>` arg, `--yes` flag, `confirmProtectDelete`
   - `newProtectMyResourceExportCmd()` — positional `<name>` arg, convert API response to input type, output via `printProtectExport`
   - `flattenMyResource()` — essential fields only for table output
   - `myResourceToInput()` — strip server fields for export
2. Wire into `protect.go`: `cmd.AddCommand(newProtectMyResourceCmd(cliCtx))`
3. Add to `groups.go` (`protectGroupMap`)
4. Optionally add alias in `aliases.go` (`protectAliases`)
5. Add resolver method in `internal/protect/resolve.go`
6. Add tests in `protect_test.go` (subcommands), `protect_conversions_test.go` (flatten/toInput)

### Adding a new product namespace

1. Create `internal/commands/newproduct.go` as the bridge (like `pro.go`)
2. Create `internal/commands/newproduct_*.go` for handwritten commands
3. If the product has generated commands, create `internal/commands/newproduct/generated/`
4. Wire the bridge into `root.go`
5. Add root group mapping in `groups.go` (`rootGroupMap`)
