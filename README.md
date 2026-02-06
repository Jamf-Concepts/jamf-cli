# 🍎 jamfpro-cli

CLI tool for Jamf Pro Server API automation.

## 📦 Installation

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

## 🚀 Quick Start

```bash
# One-time setup: create OAuth2 credentials from an admin account
jamfpro-cli config setup --url https://jamf.company.com

# Or configure manually with an existing token
jamfpro-cli config add-profile prod \
  --url https://jamf.company.com \
  --auth-method oauth2 \
  --client-id abc123 \
  --client-secret "env:JAMF_CLIENT_SECRET"

# Verify your config is valid
jamfpro-cli config validate

# List computers as a table
jamfpro-cli comp list -o table

RESULTS (3 total)

 ID   NAME              ISMANAGED   SERIALNUMBER   LASTCONTACTDATE
──────────────────────────────────────────────────────────────────
 36   MacBook Pro 16    ● true      C02X1234       3h ago
 42   iMac Office       ● true      C02Y5678       2d ago
 99   Mac mini Server   ● true      C02Z9012       1w ago

# Get details for a single mobile device
jamfpro-cli md get 42

# Export a full inventory report
jamfpro-cli comp list -o csv --out-file inventory.csv
```

## ⚙️ Configuration

Config file location (XDG-compliant):

- `~/.config/jamfpro-cli/config.yaml` (preferred)
- `~/.jamfpro-cli/config.yaml` (fallback)

### Example config

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

### 🔐 Secrets

Secret values support three formats:

- **Literal:** `my-secret-value`
- **Environment variable:** `env:JAMF_CLIENT_SECRET`
- **File:** `file:/run/secrets/jamf-token`

### ✅ Validate

Check your config file for errors before running commands:

```bash
$ jamfpro-cli config validate
Config file: /Users/admin/.config/jamfpro-cli/config.yaml
  ✓ File exists
  ✓ Valid YAML
  ✓ Default output format: table
  ✓ Default profile: prod

Profile "prod":
  ✓ URL: https://jamf.company.com
  ✓ Auth method: oauth2
  ✓ client-id resolvable
  ✓ client-secret resolvable

✓ All checks passed.

# Also test server reachability
$ jamfpro-cli config validate --connectivity
```

## 🚩 Global Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--profile` | `-p` | Config profile to use |
| `--output` | `-o` | Output format: table, json, csv, yaml, plain |
| `--wide` | `-w` | Show all columns in table output |
| `--out-file` | | Write output to file instead of stdout |
| `--quiet` | `-q` | Suppress non-error output |
| `--verbose` | `-v` | Show debug info |
| `--no-input` | | Never prompt; fail if input required |
| `--no-color` | | Disable colored output |
| `--dry-run` | `-n` | Preview changes without executing |

## 📋 Output Formats

### Table

The default for interactive use. Smart column selection, status indicators, and relative timestamps:

```text
$ jamfpro-cli comp list -o table
RESULTS (4 total)

 ID   NAME              ISMANAGED   SERIALNUMBER   LASTCONTACTDATE
──────────────────────────────────────────────────────────────────
 36   MacBook Pro 16    ● true      C02X1234       just now
 42   iMac Office       ● true      C02Y5678       5h ago
 71   MacBook Air       ○ false     C02Z9012       3d ago
 99   Mac mini Server   ● true      C02W3456       Sep 15, 2025 2:30 PM
```

- **Relative timestamps** for dates within 30 days (`just now`, `5m ago`, `3h ago`, `2d ago`, `1w ago`)
- **Absolute dates** for older dates and in `--wide` mode
- **Status indicators:** `● active` (green) `○ inactive` (dim) `◐ pending` (yellow) `● error` (red)
- **Smart columns:** shows id, name, status, and key fields by default; `--wide` shows everything

### JSON

For scripting and piping to `jq`:

```bash
# Get all computer names
jamfpro-cli comp list -o json | jq '.[].name'

# Find computers with old OS versions
jamfpro-cli comp list -o json | jq '.[] | select(.operatingSystemVersion < "15")'
```

### CSV

For spreadsheets and data analysis:

```bash
# Export to file
jamfpro-cli comp list -o csv --out-file fleet-report.csv

# Pipe to other tools
jamfpro-cli comp list -o csv | column -t -s,
```

### Plain

Tab-separated, no headers. Built for Unix pipelines:

```bash
# Get just the IDs
jamfpro-cli comp list -o plain | cut -f1

# Count managed devices
jamfpro-cli comp list -o plain | awk -F'\t' '$5=="true"' | wc -l
```

## ⌨️ Command Aliases

Common commands have short aliases:

| Command | Alias |
|---------|-------|
| `computers` | `comp` |
| `mobile-devices` | `md` |
| `scripts` | `scr` |
| `buildings` | `bld` |
| `categories` | `cat` |
| `departments` | `dept` |
| `config` | `cfg` |

```bash
jamfpro-cli comp list          # same as: jamfpro-cli computers list
jamfpro-cli md list -o table   # same as: jamfpro-cli mobile-devices list -o table
jamfpro-cli scr list           # same as: jamfpro-cli scripts list
```

## 💡 Common Workflows

### Daily fleet check

```bash
# Quick overview of your fleet
jamfpro-cli comp list -o table

# Devices that haven't checked in recently (pipe JSON through jq)
jamfpro-cli comp list -o json \
  | jq '[.[] | select(.lastContactDate < "2025-01-01")] | length'
```

### Inventory export

```bash
# Full inventory to CSV
jamfpro-cli comp list -o csv --out-file computers.csv
jamfpro-cli md list -o csv --out-file mobile-devices.csv

# Specific fields via jq
jamfpro-cli comp list -o json \
  | jq -r '.[] | [.id, .name, .serialNumber] | @csv' > serials.csv
```

### Multi-environment management

```bash
# Compare device counts across environments
echo "Prod:" && jamfpro-cli comp list -p prod -o json | jq length
echo "Staging:" && jamfpro-cli comp list -p staging -o json | jq length
```

### Scripting with plain output

```bash
# Delete all scripts by ID
jamfpro-cli scr list -o json | jq -r '.[].id' | while read id; do
  jamfpro-cli scr delete "$id"
done

# Check if a serial number exists
jamfpro-cli comp list -o json | jq -e '.[] | select(.serialNumber=="C02X1234")' > /dev/null \
  && echo "Found" || echo "Not found"
```

## 📚 Available Commands

### 💻 Device Management

| Command | Description |
|---------|-------------|
| `computers` | Manage computers |
| `mobile-devices` | Manage mobile devices |
| `computer-groups` | Manage computer groups |
| `computer-smart-groups` | Manage computer smart groups |
| `mobile-device-groups` | Manage mobile device groups |
| `mobile-device-smart-groups` | Manage mobile device smart groups |

### 📲 Enrollment & Prestage

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

### 📊 Inventory & Reporting

| Command | Description |
|---------|-------------|
| `mobile-device-inventory-details` | Manage mobile device inventory details |
| `inventory-preloads` | Manage inventory preloads |
| `inventory-preload-v-2s` | Manage inventory preloads (v2) |
| `inventory-informations` | Manage inventory information |

### 🔧 Configuration

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

### 🔒 MDM & Security

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

### 👥 Users & Groups

| Command | Description |
|---------|-------------|
| `users` | Manage users |
| `user-smart-groups` | Manage user smart groups |
| `static-user-groups` | Manage static user groups |
| `user-preferences` | Manage user preferences |
| `ldap-rs` | Manage LDAP servers |

### 🖥️ Server Administration

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

### 🛍️ Self Service

| Command | Description |
|---------|-------------|
| `self-service-settings` | Manage Self Service settings |
| `self-service-brandings` | Manage Self Service branding |
| `notifications` | Manage notifications |

### 🔹 Other

| Command | Description |
|---------|-------------|
| `config` | Manage CLI configuration and profiles |
| `config setup` | Bootstrap OAuth2 credentials from admin account |
| `config validate` | Validate config file and profile settings |
| `completion` | Generate shell completion scripts |
| `version` | Print version information |

Use `jamfpro-cli [command] --help` for detailed usage of any command.

## 🚦 Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error |
| 2 | Invalid usage (bad flags) |
| 3 | Authentication error |
| 4 | Not found |
| 5 | Permission denied |
| 6 | Rate limited |

## 🐚 Shell Completion

```bash
# Bash
jamfpro-cli completion bash > /etc/bash_completion.d/jamfpro-cli

# Zsh
jamfpro-cli completion zsh > "${fpath[1]}/_jamfpro-cli"

# Fish
jamfpro-cli completion fish > ~/.config/fish/completions/jamfpro-cli.fish
```

## 🛠️ Development

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
