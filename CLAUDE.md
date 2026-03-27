# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## CRITICAL: Generated Code Boundary

**Never edit files in `internal/commands/generated/`** — they are overwritten by `make generate`.

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
| Add a new handwritten command | `internal/commands/` (new file + wire in `root.go`) |
| Modify auth behavior | `internal/auth/` |
| Change HTTP client / retry / exit codes | `internal/client/` |
| Add or change output formats | `internal/output/` |
| Add a short alias (e.g., `comp` for `computers`) | `internal/commands/aliases.go` |
| Add a command group for `--help` output | `internal/commands/groups.go` |
| Change global flags or root command behavior | `internal/commands/root.go` |
| Change config file handling | `internal/config/` |

## Build & Dev Commands

```bash
make build                  # Build binary to bin/jamfpro-cli
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
bin/jamfpro-cli setup                     # Interactive first-time config (creates profile)
bin/jamfpro-cli --url https://... --token ... computers list  # One-off with flags
```

## Architecture

This is a Jamf Pro Server API CLI. Commands are **code-generated** from OpenAPI specs (modern API) and a YAML manifest (Classic API). The handwritten code is thin glue.

### Code Generation Pipeline

```
specs/*.yaml ──────────────► generator/parser/   ──► internal/commands/generated/*.go
                               ParseSpec()            + registry.go
                               Generator.Generate()

specs/classic/resources.yaml ► generator/classic/ ──► internal/commands/generated/classic_*.go
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

`cmd/jamfpro-cli/main.go` --> `commands.NewRootCmd()` --> `PersistentPreRunE` (resolves auth + config) --> generated commands

`PersistentPreRunE` in `root.go` is the critical path: it resolves credentials through a priority chain (flags > env vars > config profile), builds the auth provider, and wires up the HTTP client with optional spinner/dry-run decorators.

### Key Packages

| Package | Purpose |
|---------|---------|
| `internal/commands/` | Handwritten commands (config, setup, overview, completion, aliases, groups) |
| `internal/commands/generated/` | **Generated** — all API resource commands + registry |
| `internal/client/` | HTTP client with auth injection, retry (exponential backoff, respects `Retry-After`), and exit-code mapping |
| `internal/auth/` | Provider interface with OAuth2, Platform OAuth2, and Token impls |
| `internal/config/` | YAML config load/save, secret resolution (`env:`, `file:`, `keychain:` prefixes) |
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

Generated commands depend on two interfaces defined in `registry.go`:
- `HTTPClient` — `Do(ctx, method, path, body) (*http.Response, error)`
- `OutputFormatter` — `PrintResponse(resp)` and `PrintRaw(data)`

`root.go` bridges these with adapter types (`cliClient`, `cliOutput`) and decorators (`spinnerClient`, `dryRunClient`).

`cliOutput.PrintRaw` has a `--field` override: when `fieldName` is set, it parses JSON and extracts the named field from each object instead of delegating to the formatter. Generated `create`/`update` commands include a `--scaffold` flag that prints a JSON template (produced by `scaffoldJSON()` in the generator).

### Config File

`~/.config/jamfpro-cli/config.yaml` (XDG-compliant)

## Conventions

- Global flags are package-level vars in `root.go` (not struct fields) — accessed by generated commands via the `CLIContext` struct.
- Command grouping for `--help` output is maintained in `groups.go` — add new commands there.
- Short aliases (e.g., `comp` for `computers`) are in `aliases.go`.
- The `overview` command makes ~37 parallel API calls to produce an instance dashboard — it's the most complex handwritten command.
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
2. Review: `git diff --stat -- internal/commands/generated/`
3. Run: `make test`

### Adding a new handwritten command

1. Create new file in `internal/commands/` (e.g., `mycommand.go`)
2. Wire it into the root command in `root.go`
3. Add to the appropriate group in `groups.go`
4. Optionally add a short alias in `aliases.go`
