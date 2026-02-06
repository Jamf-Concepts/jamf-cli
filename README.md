# jamfpro-cli

CLI tool for Jamf Pro Server API automation.

## Installation

### Homebrew (macOS/Linux)

```bash
brew install ktn-jamf/tap/jamfpro-cli
```

### Binary releases

Download from [GitHub Releases](https://github.com/ktn-jamf/jamfpro-cli/releases).

### From source

```bash
go install github.com/ktn-jamf/jamfpro-cli/cmd/jamfpro-cli@latest
```

## Quick Start

```bash
# Configure a profile
jamfpro-cli config add-profile prod --url https://jamf.company.com

# Authenticate (token will be prompted or read from stdin)
export JAMF_TOKEN="your-api-token"

# List computers
jamfpro-cli computers list

# Get a specific computer
jamfpro-cli computers get 123

# Filter and export
jamfpro-cli computers list --filter "osVersion==14.*" -o csv > report.csv
```

## Configuration

Config file location (XDG-compliant):
- `~/.config/jamfpro-cli/config.yaml` (preferred)
- `~/.jamfpro-cli/config.yaml` (fallback)

Example config:

```yaml
default-profile: prod
default-output: table

profiles:
  prod:
    url: https://jamf.company.com
    auth-method: oauth2
    client-id: abc123
    client-secret: env:JAMF_PROD_SECRET

  staging:
    url: https://jamf-staging.company.com
    auth-method: token
    token: env:JAMF_STAGING_TOKEN
```

## Global Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--profile` | `-p` | Config profile to use |
| `--output` | `-o` | Output format: table, json, csv, yaml, plain |
| `--wide` | `-w` | Show all columns in table output |
| `--quiet` | `-q` | Suppress non-error output |
| `--verbose` | `-v` | Show debug info |
| `--no-input` | | Never prompt; fail if input required |
| `--no-color` | | Disable colored output |
| `--dry-run` | `-n` | Preview changes without executing |

## Output Formats

```bash
# Table (default for TTY)
jamfpro-cli computers list

# JSON (for jq)
jamfpro-cli computers list -o json | jq '.[] | .name'

# CSV (for spreadsheets)
jamfpro-cli computers list -o csv > report.csv

# Plain (tab-separated, no headers - for piping)
jamfpro-cli computers list -o plain | cut -f1
```

### Table Output

Table output shows a clean, formatted view with smart column defaults:

```bash
# Default - shows key columns only
jamfpro-cli computers list -o table

 ID   NAME              ISMANAGED   OPERATINGSYSTEMVERSION   SERIALNUMBER   LASTCONTACTDATE      LASTREPORTDATE
────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 36   MacBook Pro       ● true      15.2                     C02X1234       Jan 15, 2026 3:04 PM Jan 15, 2026 3:04 PM
 42   iMac Office       ● true      14.6.1                   C02Y5678       Jan 14, 2026 9:30 AM Jan 14, 2026 9:30 AM

# Wide mode - shows all columns
jamfpro-cli computers list -o table --wide
```

**Default columns shown (in order):**
- `id`, `name`, `isManaged`
- `operatingSystemVersion`, `serialNumber`
- `lastContactDate`, `lastReportDate`
- Additional status fields when present

**Status indicators:**
- `● Active/Enabled` (green)
- `○ Inactive/Disabled` (dim)
- `◐ Pending` (yellow)
- `● Failed/Error` (red)

## Available Commands

### Device Management

| Command | Description |
|---------|-------------|
| `computers` | Manage computers |
| `mobile-devices` | Manage mobile devices |
| `computer-groups` | Manage computer groups |
| `computer-smart-groups` | Manage computer smart groups |
| `mobile-device-groups` | Manage mobile device groups |
| `mobile-device-smart-groups` | Manage mobile device smart groups |

### Enrollment & Prestage

| Command | Description |
|---------|-------------|
| `computer-prestages-v-3s` | Manage computer prestages |
| `computer-prestage-scope-v-2s` | Manage computer prestage scopes |
| `mobile-device-prestages-v-3s` | Manage mobile device prestages |
| `mobile-device-prestage-scope-v-2s` | Manage mobile device prestage scopes |
| `device-enrollment-instances` | Manage device enrollment instances |
| `enrollment-settings` | Manage enrollment settings |
| `enrollment-customization-panels` | Manage enrollment customization panels |
| `reenrollments` | Manage reenrollments |

### Inventory & Reporting

| Command | Description |
|---------|-------------|
| `mobile-device-inventory-details` | Manage mobile device inventory details |
| `inventory-preloads` | Manage inventory preloads |
| `inventory-preload-v-2s` | Manage inventory preloads (v2) |
| `inventory-informations` | Manage inventory information |

### Configuration

| Command | Description |
|---------|-------------|
| `scripts` | Manage scripts |
| `categories` | Manage categories |
| `departments` | Manage departments |
| `buildings` | Manage buildings |
| `sites` | Manage sites |
| `ebooks` | Manage ebooks |
| `mobile-device-apps` | Manage mobile device apps |
| `mobile-device-extension-attributes` | Manage mobile device extension attributes |

### MDM & Security

| Command | Description |
|---------|-------------|
| `mdm-renewals` | Manage MDM renewals |
| `renew-mdm-profiles` | Manage MDM profile renewals |
| `remove-computer-mdm-profiles` | Remove computer MDM profiles |
| `remove-mobile-device-mdm-profiles` | Remove mobile device MDM profiles |
| `erase-device-computers` | Erase computers |
| `erase-device-mobiles` | Erase mobile devices |
| `local-admin-password-v-2s` | Manage local admin passwords (LAPS) |
| `certificate-authorities` | Manage certificate authorities |

### Users & Groups

| Command | Description |
|---------|-------------|
| `users` | Manage users |
| `user-smart-groups` | Manage user smart groups |
| `static-user-groups` | Manage static user groups |
| `user-preferences` | Manage user preferences |
| `ldap-rs` | Manage LDAP servers |

### Server Administration

| Command | Description |
|---------|-------------|
| `jamf-pro-versions` | Get Jamf Pro version info |
| `jamf-pro-informations` | Get Jamf Pro server info |
| `jamf-pro-server-urls` | Manage server URLs |
| `servers` | Manage servers |
| `systems` | Manage system settings |
| `caches` | Manage caches |
| `database-connections` | Manage database connections |
| `activation-codes` | Manage activation codes |

### Self Service

| Command | Description |
|---------|-------------|
| `self-service-settings` | Manage Self Service settings |
| `self-service-brandings` | Manage Self Service branding |
| `notifications` | Manage notifications |

### Other

| Command | Description |
|---------|-------------|
| `config` | Manage CLI configuration and profiles |
| `completion` | Generate shell completion scripts |
| `version` | Print version information |

Use `jamfpro-cli [command] --help` for detailed usage of any command.

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error |
| 2 | Invalid usage (bad flags) |
| 3 | Authentication error |
| 4 | Not found |
| 5 | Permission denied |
| 6 | Rate limited |

## Shell Completion

```bash
# Bash
jamfpro-cli completion bash > /etc/bash_completion.d/jamfpro-cli

# Zsh
jamfpro-cli completion zsh > "${fpath[1]}/_jamfpro-cli"

# Fish
jamfpro-cli completion fish > ~/.config/fish/completions/jamfpro-cli.fish
```

## Development

```bash
# Build
make build

# Test
make test

# Lint
make lint

# Generate commands from OpenAPI specs
make generate
```
