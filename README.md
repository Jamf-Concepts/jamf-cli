# jamfpro-cli

CLI tool for Jamf Pro Server API automation.

**[Documentation Wiki](https://github.com/jamf/jamfpro-cli/wiki)** — full guides, configuration reference, and workflow recipes.

![jamfpro-cli demo](docs/demo.gif)

## Installation

### Binary releases

Download from [GitHub Releases](https://github.com/jamf/jamfpro-cli/releases).

### From source

```bash
go install github.com/jamf/jamfpro-cli/cmd/jamfpro-cli@latest
```

## Quick Start

```bash
# One-time setup: create OAuth2 credentials from an admin account
jamfpro-cli config setup --url https://jamf.company.com

# Or configure manually with existing credentials
jamfpro-cli config add-profile prod \
  --url https://jamf.company.com \
  --auth-method oauth2 \
  --client-id abc123 \
  --client-secret "env:JAMF_CLIENT_SECRET"

# Instance health dashboard
jamfpro-cli overview

# List computers
jamfpro-cli comp list -o table

# Extract just the names
jamfpro-cli comp list --field name

# Export inventory
jamfpro-cli comp list -o csv --out-file inventory.csv

# Show the JSON template for creating a building
jamfpro-cli buildings create --scaffold
```

See the [Setup Guide](https://github.com/jamf/jamfpro-cli/wiki/Setup-Guide) for the full walkthrough.

## Features

- **Full API coverage** — Modern API (OpenAPI-generated) and Classic API (`/JSSResource/`) commands
- **`--field`** — Extract a single field from any response: `jamfpro-cli comp list --field id`
- **`--scaffold`** — Print JSON templates for create/update commands with example values
- **`overview`** — Instance dashboard with 30+ parallel API calls: inventory, enrollment, MDM, alerts
- **Five output formats** — `table`, `json`, `csv`, `yaml`, `plain`
- **Auto-pagination** — `--all` fetches every page; `--limit` caps results
- **Dry-run mode** — `--dry-run` previews writes without executing
- **Destructive safeguards** — Delete/erase/wipe require `--yes` confirmation
- **System keychain** — Secrets stored via macOS Keychain or Linux secret-service

## Configuration

Config file: `~/.config/jamfpro-cli/config.yaml`

```yaml
default-profile: prod
default-output: table

profiles:
  prod:
    url: https://jamf.company.com
    auth-method: oauth2
    client-id: abc123
    client-secret: env:JAMF_PROD_SECRET
```

Two auth methods: `oauth2` (client credentials) and `token` (static bearer). Three secret formats: `env:VAR`, `file:/path`, `keychain:service/account`.

See the wiki for full details: [Configuration & Profiles](https://github.com/jamf/jamfpro-cli/wiki/Configuration-&-Profiles) · [Secrets & Keychain](https://github.com/jamf/jamfpro-cli/wiki/Secrets-&-Keychain)

## Command Aliases

| Command | Alias |
|---------|-------|
| `computers` | `comp` |
| `mobile-devices` | `md` |
| `scripts` | `scr` |
| `buildings` | `bld` |
| `categories` | `cat` |
| `departments` | `dept` |
| `config` | `cfg` |

Full command catalog: [Command Reference](https://github.com/jamf/jamfpro-cli/wiki/Command-Reference) · [Output Formats](https://github.com/jamf/jamfpro-cli/wiki/Output-Formats) · [Common Workflows](https://github.com/jamf/jamfpro-cli/wiki/Common-Workflows)

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

See [Error Handling & Exit Codes](https://github.com/jamf/jamfpro-cli/wiki/Error-Handling-&-Exit-Codes) for structured JSON errors, retry logic, and scripting patterns.

## Shell Completion

```bash
jamfpro-cli completion install
```

Supports bash, zsh, fish, and PowerShell. See the [Setup Guide](https://github.com/jamf/jamfpro-cli/wiki/Setup-Guide#shell-completion) for manual installation.

## Development

```bash
make build       # Build binary
make test        # Run tests
make lint        # Lint code
make generate    # Generate commands from OpenAPI specs
```

See [Architecture & Development](https://github.com/jamf/jamfpro-cli/wiki/Architecture-&-Development) for project structure and contributing guidelines.
