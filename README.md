# jamf-cli

Unified CLI for the Jamf platform. Currently supports Jamf Pro, with more products coming.

**[Documentation Wiki](https://github.com/Jamf-Concepts/jamf-cli/wiki)** — full guides, configuration reference, and workflow recipes.

![jamf-cli demo](docs/demo.gif)

## Installation

### Homebrew (macOS and Linux)

```bash
brew install Jamf-Concepts/tap/jamf-cli
```

### Binary releases

Download from [GitHub Releases](https://github.com/Jamf-Concepts/jamf-cli/releases).

### From source

```bash
go install github.com/Jamf-Concepts/jamf-cli/cmd/jamf-cli@latest
```

## Quick Start

```bash
# One-time setup: create OAuth2 credentials from an admin account
jamf-cli pro setup --url https://jamf.company.com

# Or configure manually with existing credentials
jamf-cli config add-profile prod \
  --url https://jamf.company.com \
  --auth-method oauth2 \
  --client-id abc123 \
  --client-secret "env:JAMF_CLIENT_SECRET"

# Or use Jamf Platform Gateway auth
jamf-cli config add-profile gateway \
  --url https://us.apigw.jamf.com \
  --auth-method platform \
  --client-id abc123 \
  --client-secret "env:PLATFORM_SECRET" \
  --tenant-id e5b39e85-5ecd-4d40-9d13-02c7cf21c762

# Instance health dashboard
jamf-cli pro overview

# List computers
jamf-cli pro comp list -o table

# Extract just the names
jamf-cli pro comp list --field name

# Export inventory
jamf-cli pro comp list -o csv --out-file inventory.csv

# Show the JSON template for creating a building
jamf-cli pro buildings create --scaffold
```

See the [Setup Guide](https://github.com/Jamf-Concepts/jamf-cli/wiki/Setup-Guide) for the full walkthrough.

## Features

### Jamf Pro

- **Full API coverage** — Modern API (OpenAPI-generated) and Classic API (`/JSSResource/`) commands
- **`overview`** — Instance dashboard with 37 parallel API calls: inventory, enrollment, MDM, alerts

### Cross-product

- **`--field`** — Extract a single field from any response: `jamf-cli pro comp list --field id`
- **`--scaffold`** — Print JSON templates for create/update commands with example values
- **Five output formats** — `table`, `json`, `csv`, `yaml`, `plain`
- **Auto-pagination** — `--all` fetches every page; `--limit` caps results
- **Dry-run mode** — `--dry-run` previews writes without executing
- **Destructive safeguards** — Delete/erase/wipe require `--yes` confirmation
- **System keychain** — Secrets stored via macOS Keychain or Linux secret-service
- **Jamf Platform Gateway** — Route through regional gateways with `--tenant-id`

## Configuration

Config file: `~/.config/jamf-cli/config.yaml`

```yaml
default-profile: prod
default-output: table

profiles:
  prod:
    url: https://jamf.company.com
    auth-method: oauth2
    client-id: abc123
    client-secret: env:JAMF_PROD_SECRET

  # Platform Gateway auth (routes through regional gateway)
  platform-prod:
    url: https://us.apigw.jamf.com
    auth-method: platform
    client-id: env:PLATFORM_CLIENT_ID
    client-secret: env:PLATFORM_CLIENT_SECRET
    tenant-id: e5b39e85-5ecd-4d40-9d13-02c7cf21c762
```

Three auth methods: `oauth2` (client credentials), `token` (static bearer), and `platform` (Jamf Platform Gateway). Three secret formats: `env:VAR`, `file:/path`, `keychain:service/account`.

See the wiki for full details: [Configuration & Profiles](https://github.com/Jamf-Concepts/jamf-cli/wiki/Configuration-&-Profiles) · [Secrets & Keychain](https://github.com/Jamf-Concepts/jamf-cli/wiki/Secrets-&-Keychain)

## Command Structure

All Jamf Pro commands live under the `pro` namespace:

```bash
jamf-cli pro <command> [subcommand] [flags]
```

### Aliases

| Command | Alias |
|---------|-------|
| `computers` | `comp` |
| `mobile-devices` | `md` |
| `scripts` | `scr` |
| `buildings` | `bld` |
| `categories` | `cat` |
| `departments` | `dept` |
| `config` | `cfg` |

Full command catalog: [Command Reference](https://github.com/Jamf-Concepts/jamf-cli/wiki/Command-Reference) · [Output Formats](https://github.com/Jamf-Concepts/jamf-cli/wiki/Output-Formats) · [Common Workflows](https://github.com/Jamf-Concepts/jamf-cli/wiki/Common-Workflows)

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error |
| 2 | Invalid usage |
| 3 | Authentication error |
| 4 | Not found |
| 5 | Permission denied |
| 6 | Rate limited |

See [Error Handling & Exit Codes](https://github.com/Jamf-Concepts/jamf-cli/wiki/Error-Handling-&-Exit-Codes) for structured JSON errors, retry logic, and scripting patterns.

## Shell Completion

```bash
jamf-cli completion install
```

Supports bash, zsh, fish, and PowerShell. See the [Setup Guide](https://github.com/Jamf-Concepts/jamf-cli/wiki/Setup-Guide#shell-completion) for manual installation.

## Development

```bash
make build       # Build binary
make test        # Run tests
make lint        # Lint code
make generate    # Generate commands from OpenAPI specs
```

See [Architecture & Development](https://github.com/Jamf-Concepts/jamf-cli/wiki/Architecture-&-Development) for project structure and contributing guidelines.

## License

Copyright (c) 2025 Jamf. All rights reserved.

This project is distributed under the [Jamf Concepts Use Agreement](LICENSE). See the LICENSE file for details.
