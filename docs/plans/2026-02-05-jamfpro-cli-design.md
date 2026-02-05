# jamfpro-cli Design Document

**Date:** 2026-02-05
**Status:** Draft
**Purpose:** Admin automation CLI for Jamf Pro Server API

## Overview

jamfpro-cli is a Go-based CLI tool auto-generated from Jamf Pro Server's OpenAPI specifications. It provides full API coverage (700+ endpoints) for admin automation workflows including device management, inventory/reporting, and configuration management.

## Key Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Generation approach | Auto-generated from OpenAPI | 485 existing specs, keeps CLI in sync with API |
| Language | Go | Single binary, fast, excellent CLI ecosystem (cobra, viper) |
| Auth methods | All (token, basic, OAuth2) | Flexibility for different admin setups |
| Output formats | JSON, table, CSV, YAML, plain | Different workflows need different formats |
| Repository | Separate from jamf-pro-server | Independent release cycle, cleaner separation |
| Command structure | Resource-based subcommands | Industry standard (kubectl, gh, aws), discoverable |
| Config location | XDG-compliant | `~/.config/jamfpro-cli/` preferred, fallback to `~/.jamfpro-cli/` |

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    jamfpro-cli binary                       │
├─────────────────────────────────────────────────────────┤
│  Command Layer (generated)                               │
│  ├── computers/  (get, list, create, update, delete,    │
│  │                erase, lock, history, export...)       │
│  ├── mobile-devices/                                     │
│  ├── buildings/                                          │
│  ├── policies/                                           │
│  └── ... (59+ resource groups)                          │
├─────────────────────────────────────────────────────────┤
│  Shared Services                                         │
│  ├── Auth (token, basic, oauth2)                        │
│  ├── HTTP Client (retry, rate-limit handling)           │
│  ├── Output Formatter (json, table, csv, yaml)          │
│  └── Config Manager (profiles, credentials)             │
├─────────────────────────────────────────────────────────┤
│  Code Generator (build-time)                             │
│  └── OpenAPI specs → Go commands                        │
└─────────────────────────────────────────────────────────┘
```

### Key Design Principles

- **Single binary** - No runtime dependencies, easy distribution
- **Profile-based config** - Support multiple Jamf Pro instances (`--profile prod`)
- **Automatic pagination** - `list` commands fetch all pages by default, `--limit` to cap
- **RSQL passthrough** - `--filter` accepts the same RSQL syntax as the API

## Code Generation

### Generator Pipeline

```
OpenAPI YAML files
       ↓
   Parser (kin-openapi)
       ↓
   Template Engine (Go templates)
       ↓
   Generated Go code
       ↓
   go build → jamfpro-cli binary
```

### What Gets Generated

For each OpenAPI spec, the generator produces:

1. **Resource command group** - `computers.go` with subcommands
2. **Request/response structs** - Typed Go structs from schemas
3. **Flag definitions** - CLI flags from query params and request bodies
4. **Help text** - Descriptions pulled from OpenAPI `summary` and `description`

### Mapping Rules

| OpenAPI | CLI |
|---------|-----|
| `GET /v1/computers` | `jamfpro-cli computers list` |
| `GET /v1/computers/{id}` | `jamfpro-cli computers get <id>` |
| `POST /v1/computers` | `jamfpro-cli computers create` |
| `PUT /v1/computers/{id}` | `jamfpro-cli computers update <id>` |
| `DELETE /v1/computers/{id}` | `jamfpro-cli computers delete <id>` |
| `POST /v1/computers/{id}/erase` | `jamfpro-cli computers erase <id>` |
| `GET /v1/computers/{id}/history` | `jamfpro-cli computers history <id>` |
| `POST /v1/computers/export` | `jamfpro-cli computers export` |

### Handling Versioned Endpoints

Some resources have multiple API versions (v1, v2, v3). Strategy:
- Default to latest stable version
- `--api-version v2` flag to override when needed

## Global Flags

All commands support these flags:

| Flag | Short | Description |
|------|-------|-------------|
| `--help` | `-h` | Show help |
| `--version` | | Print version |
| `--profile` | `-p` | Config profile to use |
| `--output` | `-o` | Output format: `table`, `json`, `csv`, `yaml`, `plain` |
| `--quiet` | `-q` | Suppress non-error output |
| `--verbose` | `-v` | Show debug info (HTTP requests, etc.) |
| `--no-input` | | Never prompt; fail if input required |
| `--no-color` | | Disable colored output |
| `--dry-run` | `-n` | Preview changes without executing |

## Authentication & Configuration

### Config File Structure

Located at `~/.config/jamfpro-cli/config.yaml` (XDG) or `~/.jamfpro-cli/config.yaml` (fallback):

```yaml
default-profile: prod

profiles:
  prod:
    url: https://jamf.company.com
    auth-method: oauth2
    client-id: abc123
    client-secret: env:JAMF_PROD_SECRET      # env var reference
    # or: client-secret: file:~/.jamf-secrets/prod.txt

  staging:
    url: https://jamf-staging.company.com
    auth-method: token
    token: env:JAMF_STAGING_TOKEN

  dev:
    url: https://localhost:8443
    auth-method: basic
    username: admin
    # password prompted interactively or via --password-stdin
```

**Security note:** Config files should reference secrets via `env:` or `file:` prefixes, never store plaintext secrets.

### Auth Methods

| Method | Flags | Use Case |
|--------|-------|----------|
| Bearer token | `--token` or `--token-file` | Pre-generated API tokens |
| Basic → token | `--username` + `--password-stdin` | Interactive use, auto-fetches token |
| OAuth2 | `--client-id` + `--client-secret-file` | Service accounts, automation |

**Never pass secrets as flag values.** Use `--password-stdin`, `--token-file`, or `--client-secret-file`.

### Auth Flow

1. Check CLI flags first
2. Fall back to profile config
3. Fall back to environment variables (`JAMF_URL`, `JAMF_TOKEN`, etc.)
4. Prompt interactively if `--username` provided without `--password`

### Profile Management Commands

```bash
jamfpro-cli --profile staging computers list
jamfpro-cli config set-default prod
jamfpro-cli config add-profile test --url https://test.jamf.com
jamfpro-cli config show                    # show resolved config with sources
jamfpro-cli config show --profile staging  # show specific profile
```

## Output Formatting

### Output Formats

```bash
# Table (default for interactive TTY)
jamfpro-cli computers list
ID      NAME           OS_VERSION    LAST_CHECK_IN
123     MacBook-001    14.3.1        2026-02-05 09:30
456     MacBook-002    14.2.0        2026-02-05 08:15

# JSON (for scripting with jq)
jamfpro-cli computers list -o json
[{"id": 123, "name": "MacBook-001", ...}]

# CSV (for spreadsheets)
jamfpro-cli computers list -o csv > report.csv

# YAML (for config files)
jamfpro-cli computers get 123 -o yaml

# Plain (stable line-based text for piping)
jamfpro-cli computers list -o plain
123	MacBook-001	14.3.1	2026-02-05 09:30
456	MacBook-002	14.2.0	2026-02-05 08:15
# tab-separated, no headers, one line per item
```

**TTY detection:** When stdout is not a TTY, default to `plain` instead of `table`.

### Filtering & Sorting

Pass-through to the API's RSQL filtering:

```bash
# Filter
jamfpro-cli computers list --filter "osVersion==14.*"
jamfpro-cli computers list --filter "lastCheckIn>2026-01-01"

# Sort
jamfpro-cli computers list --sort "name,asc"
jamfpro-cli computers list --sort "lastCheckIn,desc"

# Combine
jamfpro-cli computers list \
  --filter "building.name==HQ" \
  --sort "name,asc" \
  --output csv
```

### Pagination Control

```bash
# Fetch all (default)
jamfpro-cli computers list

# Limit results
jamfpro-cli computers list --limit 100

# Specific page (for debugging)
jamfpro-cli computers list --page 2 --page-size 50
```

## Error Handling

### Exit Codes

```
0 = success
1 = general/runtime error
2 = invalid usage (bad flags, parse error, validation)
3 = authentication error
4 = not found (resource doesn't exist)
5 = permission denied
6 = rate limited (with retry-after hint)
```

Exit code 2 for usage errors follows POSIX convention.

### Error Output

```bash
# Human-readable (errors always to stderr)
jamfpro-cli computers get 99999
Error: computer not found (id: 99999)
Exit code: 4

# JSON for scripting (error in JSON on stdout, message on stderr)
jamfpro-cli computers get 99999 -o json
{"error": "not_found", "message": "computer not found", "id": "99999"}
```

### Rate Limit Handling

Automatic retry with exponential backoff when API returns 429.

## Bulk Operations

```bash
# Bulk delete (API-native)
jamfpro-cli computers delete-multiple --ids 123,456,789

# Pipe-based workflows
jamfpro-cli computers list --filter "lastCheckIn<2025-01-01" -o json \
  | jq -r '.[].id' \
  | xargs -I {} jamfpro-cli computers delete {} --yes

# Read IDs from stdin (use - for stdin)
cat ids.txt | jamfpro-cli computers delete -

# Bulk from file
jamfpro-cli buildings create --from-file buildings.json
```

### Confirmation for Destructive Actions

```bash
# Dry run: preview without executing
jamfpro-cli computers erase 123 --dry-run
Would erase computer "MacBook-001" (id: 123)

# Interactive: requires confirmation
jamfpro-cli computers erase 123
⚠️  This will erase computer "MacBook-001" (id: 123)
Type 'yes' to confirm: yes

# Non-interactive: skip confirmation for automation
jamfpro-cli computers erase 123 --yes

# Full automation mode: no prompts, fail if input needed
jamfpro-cli --no-input computers erase 123 --yes
```

## Project Structure

```
jamfpro-cli/
├── cmd/
│   └── jamfpro-cli/
│       └── main.go              # Entry point
├── internal/
│   ├── auth/                    # Auth providers
│   ├── client/                  # HTTP client, retry logic
│   ├── config/                  # Profile management
│   ├── output/                  # Formatters (json, table, csv, yaml)
│   └── commands/                # Generated command code
├── generator/
│   ├── main.go                  # Generator entry point
│   ├── parser/                  # OpenAPI parsing
│   └── templates/               # Go templates for codegen
├── specs/                       # Copied OpenAPI YAML files
├── Makefile
├── go.mod
└── .goreleaser.yaml             # Cross-platform releases
```

## Build Commands

```bash
# Fetch latest specs from jamf-pro-server repo
make sync-specs

# Regenerate CLI code from specs
make generate

# Build for current platform
make build

# Run tests
make test

# Release (cross-compile for macOS, Linux, Windows)
make release
```

## Release Artifacts

- `jamfpro-cli-darwin-amd64` / `jamfpro-cli-darwin-arm64`
- `jamfpro-cli-linux-amd64` / `jamfpro-cli-linux-arm64`
- `jamfpro-cli-windows-amd64.exe`
- Homebrew formula, `.deb`, `.rpm` packages via GoReleaser

## Implementation Phases

### Phase 1: Foundation
- Set up repository structure
- Implement shared services (auth, config, output formatters, HTTP client)
- Build generator scaffolding with Go templates

### Phase 2: Core Generator
- OpenAPI parser using kin-openapi
- Template-based code generation for commands
- Handle standard CRUD operations

### Phase 3: Advanced Features
- Action endpoints (erase, lock, etc.)
- Bulk operations
- Export/import workflows
- Versioned endpoint handling

### Phase 4: Polish & Release
- Comprehensive error handling
- Shell completions (bash, zsh, fish, powershell)
  - `jamfpro-cli completion bash` - output script
  - `jamfpro-cli completion install` - auto-detect shell and install
- Documentation generation
- GoReleaser setup for cross-platform builds
