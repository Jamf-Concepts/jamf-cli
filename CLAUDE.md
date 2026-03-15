# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Dev Commands

```bash
make build                  # Build binary to bin/jamfpro-cli
make test                   # Run all tests (-v)
make lint                   # golangci-lint
make generate               # Regenerate commands from OpenAPI specs + Classic manifest
make sync-specs             # Copy specs from jamf/jss repo, then regenerate
make fmt                    # go fmt + gofumpt
go test -v -run TestFoo ./internal/commands/...  # Run a single test
```

## Architecture

This is a Jamf Pro Server API CLI. Commands are **code-generated** from OpenAPI specs (modern API) and a YAML manifest (Classic API). The handwritten code is thin glue.

### Code Generation Pipeline

`specs/*.yaml` + `specs/classic/resources.yaml` --> `generator/` --> `internal/commands/generated/`

- **`generator/parser/`** — Parses OpenAPI YAML specs, produces `Resource` structs, generates Go command files with cobra subcommands (list, get, create, update, delete) and auto-pagination.
- **`generator/classic/`** — Parses `specs/classic/resources.yaml` manifest, generates Classic API (`/JSSResource/...`) commands with JSON envelope unwrapping.
- **`generator/main.go`** — Entrypoint: runs both generators, produces per-resource `.go` files plus `registry.go` / `classic_registry.go` that wire everything into cobra.

**Never hand-edit files in `internal/commands/generated/`** — they are overwritten by `make generate`.

### Runtime Flow

`cmd/jamfpro-cli/main.go` --> `commands.NewRootCmd()` --> `PersistentPreRunE` (resolves auth + config) --> generated commands

`PersistentPreRunE` in `root.go` is the critical path: it resolves credentials through a priority chain (flags > env vars > config profile), builds the auth provider, and wires up the HTTP client with optional spinner/dry-run decorators.

### Key Packages

| Package | Purpose |
|---------|---------|
| `internal/commands/` | Handwritten commands (config, setup, overview, completion, aliases, groups) |
| `internal/commands/generated/` | **Generated** — all API resource commands + registry |
| `internal/client/` | HTTP client with auth injection, retry (exponential backoff, respects `Retry-After`), and exit-code mapping |
| `internal/auth/` | Provider interface with OAuth2 (client credentials) and Token impls |
| `internal/config/` | YAML config load/save, secret resolution (`env:`, `file:`, `keychain:` prefixes) |
| `internal/output/` | Multi-format output: table, JSON, CSV, YAML, plain. Table has smart column selection, date formatting, status colorization |
| `internal/keychain/` | System keychain abstraction (macOS Keychain, Linux secret-service) |
| `internal/exitcode/` | Structured exit codes (0-6) mapped from HTTP status codes |
| `internal/spinner/` | Terminal spinner for non-quiet, non-verbose mode |

### Auth Resolution Order

1. CLI flags (`--token`, `--client-id`/`--client-secret`)
2. Environment variables (`JAMF_TOKEN`, `JAMF_CLIENT_ID`, `JAMF_CLIENT_SECRET`)
3. Config profile (resolved via `--profile`, `JAMF_PROFILE`, or `default-profile` in config)

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
- Classic API paths start with `/JSSResource/` and bypass the `/api` prefix that `client.Do()` adds for modern paths.
- `NO_COLOR` env var is respected for CI/scripting (https://no-color.org).
