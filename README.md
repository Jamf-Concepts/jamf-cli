# jamf-cli

Unified CLI for the Jamf platform. Supports Jamf Pro and Jamf Protect.

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

### Jamf Pro

```bash
# One-time setup: create OAuth2 credentials from an admin account
jamf-cli pro setup --url https://jamf.company.com

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

# Create or update a building by name (upsert)
echo '{"name":"HQ","streetAddress1":"1 Apple Park Way"}' | jamf-cli pro buildings apply

# Apply from a file without confirmation
jamf-cli pro buildings apply --from-file building.json --yes

# Delete a building by name
jamf-cli pro buildings delete-by-name "HQ" --yes
```

See the [Setup Guide](https://github.com/Jamf-Concepts/jamf-cli/wiki/Setup-Guide) for the full walkthrough.

## Features

### Jamf Pro

- **Full API coverage** — Modern API (OpenAPI-generated) and Classic API (`/JSSResource/`) commands
- **`overview`** — Instance dashboard with 37 parallel API calls: inventory, enrollment, MDM, alerts
- **`scope`** — View, add to, and remove from scope on policies, config profiles, restricted software, and apps — no XML editing required

### Jamf Protect

- **Full SDK coverage** — Plans, analytics, analytic sets, exception sets, USB control, telemetry, prevent lists, unified logging filters, roles, users, groups, API clients, and org settings
- **`overview`** — Instance dashboard with 14 parallel API calls: endpoints, security config, data forwarding, access
- **`apply`** — Idempotent upsert: creates or replaces resources by name, with confirmation
- **`export` / `import`** — Round-trip configuration as JSON or YAML. Plans and analytic sets use names (not IDs) for portability across tenants
- **Community analytics** — Import YAML analytics from the [jamf/jamfprotect](https://github.com/jamf/jamfprotect) repository
- **Downloads** — Installer packages, configuration profiles (.mobileconfig), and certificates
- **Granular mutations** — Add/remove rules on USB control sets, analytics on sets, exceptions on sets

### Cross-product

- **`--field`** — Extract a single field from any response: `jamf-cli pro comp list --field id`
- **`apply`** — Name-based upsert: creates if new, replaces if existing (with confirmation)
- **`delete-by-name`** — Delete a resource by name instead of ID (with collision detection)
- **`--scaffold`** — Print JSON templates for create/update commands with example values
- **Five output formats** — `table`, `json`, `csv`, `yaml`, `plain`
- **Auto-pagination** — `--all` fetches every page; `--limit` caps results
- **Dry-run mode** — `--dry-run` previews writes without executing
- **Destructive safeguards** — Delete and replace operations require `--yes` confirmation
- **System keychain** — Secrets stored via macOS Keychain or Linux secret-service
- **Jamf Platform Gateway** — Route Jamf Pro through regional gateways with `--tenant-id`

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

  protect:
    product: protect
    url: https://tenant.protect.jamfcloud.com
    auth-method: oauth2
    client-id: keychain:jamf-cli/protect/client-id
    client-secret: keychain:jamf-cli/protect/client-secret

  # Platform Gateway auth (routes Jamf Pro through regional gateway)
  platform-prod:
    url: https://us.apigw.jamf.com
    auth-method: platform
    client-id: env:PLATFORM_CLIENT_ID
    client-secret: env:PLATFORM_CLIENT_SECRET
    tenant-id: e5b39e85-5ecd-4d40-9d13-02c7cf21c762
```

Jamf Pro supports three auth methods: `oauth2`, `token`, and `platform`. Jamf Protect uses `oauth2` only. Three secret formats: `env:VAR`, `file:/path`, `keychain:service/account`.

See the wiki for full details: [Configuration & Profiles](https://github.com/Jamf-Concepts/jamf-cli/wiki/Configuration-&-Profiles) · [Secrets & Keychain](https://github.com/Jamf-Concepts/jamf-cli/wiki/Secrets-&-Keychain)

## Command Structure

Each product has its own namespace:

```bash
jamf-cli pro <command> [subcommand] [flags]       # Jamf Pro
jamf-cli protect <command> [subcommand] [flags]    # Jamf Protect
```

### Aliases

| Product | Command | Alias |
|---------|---------|-------|
| Pro | `computers` | `comp` |
| Pro | `mobile-devices` | `md` |
| Pro | `scripts` | `scr` |
| Pro | `buildings` | `bld` |
| Pro | `categories` | `cat` |
| Pro | `departments` | `dept` |
| Protect | `removable-storage-control-sets` | `rscs` |
| Protect | `unified-logging-filters` | `ulf` |
| Protect | `exception-sets` | `es` |
| Protect | `analytic-sets` | `as` |
| Protect | `action-configs` | `ac` |
| Protect | `custom-prevent-lists` | `cpl` |
| Protect | `api-clients` | `apic` |
| Protect | `config-freeze` | `cf` |
| Root | `config` | `cfg` |

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
