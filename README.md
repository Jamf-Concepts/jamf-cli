# jamf-cli

Unified CLI for the Jamf platform. Supports Jamf Pro and Jamf Protect.

**[Documentation Wiki](https://github.com/Jamf-Concepts/jamf-cli/wiki)** — full guides, configuration reference, and workflow recipes.

**[Command Explorer](https://jamf-concepts.github.io/jamf-cli/)** — interactive showcase of all commands, searchable and filterable. Auto-updated on every merge.

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

For interactive use, `jamf-cli pro setup` prompts for credentials so nothing is leaked to shell history, and stores them in the system keychain. Environment variables (`JAMF_CLIENT_ID`, `JAMF_CLIENT_SECRET`, etc.) are intended for automation workflows only — avoid setting them in interactive shells.

### Jamf Pro

```bash
# One-time setup: prompts for credentials and stores them in the system keychain
jamf-cli pro setup --url https://jamf.company.com

# Multi-instance setup (MSPs): bootstrap credentials for many instances at once
jamf-cli pro setup --from-file instances.txt --scope standard

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
jamf-cli pro buildings delete --name "HQ" --yes

# Device actions by serial number
jamf-cli pro comp blank-push --serial C02X1234
jamf-cli pro comp redeploy-framework --serial C02X1234
jamf-cli pro comp erase --serial C02X1234 --yes

# Device actions targeting a group
jamf-cli pro comp blank-push --group "All Macs" --yes
jamf-cli pro md unmanage --group "Retired iPads" --yes --confirm-destructive

# Classic API MDM commands
jamf-cli pro comp lock --serial C02X1234 --yes --confirm-destructive
jamf-cli pro md update-inventory --id 42 --yes

# Device deep-dive
jamf-cli pro device C02X1234

# Fleet security report
jamf-cli pro report security -o table

# Run a command against multiple instances
jamf-cli multi --filter 'pro-*' -- pro buildings apply --from-file building.json --yes
```

See the [Setup Guide](https://github.com/Jamf-Concepts/jamf-cli/wiki/Setup-Guide) for the full walkthrough.

## Features

### Jamf Pro

- **Full API coverage** — Modern API (OpenAPI-generated) and Classic API (`/JSSResource/`) commands
- **`overview`** — Instance dashboard with 37 parallel API calls: inventory, enrollment, MDM, alerts
- **`scope`** — View, add to, and remove from scope on policies, config profiles, restricted software, and apps — no XML editing required
- **Device actions** — Erase, remove MDM, redeploy framework, blank push, DDM sync, renew MDM, lock, enable/disable Remote Desktop (computers); erase, unmanage, restart, shutdown, update inventory (mobile devices). Target by serial number, name, ID, group, or file. Destructive bulk operations require `--confirm-destructive`
- **`device`** — Aggregated device deep-dive: identity, hardware, OS, security posture, user info, MDM command history, policy logs
- **`report security`** — Fleet security posture: FileVault, Gatekeeper, SIP, firewall rates, OS version distribution, flagged devices

### Jamf Platform (via Gateway)

- **Blueprints** — CRUD, deploy/undeploy, clone, scope management (add/remove device groups by name), component scaffolds, import Classic configuration profiles as blueprints with automatic DDM conversion (passcode policies, Safari settings, software update deferrals, software update preferences are promoted to native DDM components)
- **Compliance Benchmarks** — Benchmark CRUD, baselines, rules, device compliance results, stats
- **Platform Devices** — Unified device inventory, actions (check-in, erase, restart, shutdown, unmanage)
- **Platform Device Groups** — CRUD, membership management
- **DDM Reports** — Device declaration status, declaration clients

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
- **`patch`** — JSON Merge Patch (RFC 7386): update individual fields without a full replace. Use `--set key=value` for scalar fields or pipe a merge-patch document. Accepts `--name`, `--serial`, `--udid` (resource-dependent) in place of an ID. `--scaffold` prints the patchable field template
- **`--name` flag** — `get`, `update`, `delete`, and `patch` commands all accept `--name` (and resource-specific alternates like `--serial`, `--udid`) in place of a positional ID
- **`--scaffold`** — Print JSON templates for create/update commands with example values
- **Five output formats** — `table`, `json`, `csv`, `yaml`, `plain`
- **Auto-pagination** — `--all` fetches every page; `--limit` caps results
- **Dry-run mode** — `--dry-run` previews writes without executing
- **`multi`** — Run any command against multiple profiles: `jamf-cli multi --filter 'pro-*' -- pro comp list`. Supports glob patterns, file input (profile names or URLs), and interactive selection
- **Destructive safeguards** — Delete and replace operations require `--yes` confirmation
- **`setup`** — Bootstrap API roles and OAuth2 credentials from a username/password. Idempotent (safe to re-run): updates roles and integrations in place without rotating credentials. Use `--rotate-credentials` to explicitly regenerate secrets. Supports multi-instance setup via `--from-file` for MSPs
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

> **Least privilege:** When creating API roles for use with jamf-cli, grant only the privileges required for the endpoints you need to access. Jamf Pro maps each API endpoint to a specific privilege — consult the [Privileges and Deprecations](https://developer.jamf.com/jamf-pro/docs/privileges-and-deprecations) reference to determine the minimum set of permissions for your workflow.

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
| Pro | `device` | `dev` |
| Pro | `blueprints` | `bp` |
| Pro | `compliance-benchmarks` | `cb` |
| Pro | `platform-devices` | `pdev` |
| Pro | `platform-device-groups` | `pdg` |
| Pro | `ddm-reports` | `ddm` |
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


## Troubleshooting

### Debug output

Add `--verbose` (or `-v`) to any command to print HTTP request and response details to stderr:

```bash
jamf-cli pro comp list --verbose
```

To capture debug output to a file:

```bash
jamf-cli pro comp list --verbose 2>debug.log
```

### Authentication errors (exit code 3)

- Run `jamf-cli pro setup` (or `jamf-cli protect setup`) to reconfigure credentials.
- Verify the active profile with `jamf-cli config list`.
- Check that env vars (`JAMF_CLIENT_ID`, `JAMF_CLIENT_SECRET`, `JAMF_URL`) are not overriding your config profile unintentionally.
- For OAuth2, confirm the API client is enabled in Jamf Pro and has the required privileges.

### Not found / permission errors (exit codes 4–5)

- Confirm the resource exists: try a `list` command first.
- Check that the API role has the minimum privileges for the endpoint. See [Privileges and Deprecations](https://developer.jamf.com/jamf-pro/docs/privileges-and-deprecations).

### Rate limiting (exit code 6)

jamf-cli retries automatically with exponential backoff when rate-limited. If you're consistently hitting limits, add `--limit` to reduce page sizes or introduce delays between commands in scripts.

### Previewing changes safely

Use `--dry-run` (`-n`) to see what a write command would do without executing it:

```bash
jamf-cli pro buildings apply --from-file building.json --dry-run
```


### Bugs and feature requests

Please file an issue in [GitHub Issues](https://github.com/Jamf-Concepts/jamf-cli/issues).


## License

Copyright (c) 2026 Jamf Software LLC.

This project is distributed under the [MIT License](LICENSE).
