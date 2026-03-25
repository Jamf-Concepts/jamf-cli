# Changelog

## v1.0.0

First public release of `jamfpro-cli` — a command-line interface for the Jamf Pro Server API.

### Features

- **Full API coverage** — 130+ commands code-generated from Jamf Pro OpenAPI specs (v11.25.2) and Classic API manifest
- **Instance overview** — `overview` command with parallel API calls, color-coded status indicators, and grouped table output
- **Multiple output formats** — JSON (default), table, CSV, YAML, and plain text via `--output`
- **Field extraction** — `--field` flag to extract a single field from JSON responses
- **JSON scaffolding** — `--scaffold` flag on create/update commands to generate JSON templates
- **OAuth2 and bearer token auth** — client credentials flow with automatic token refresh, or static bearer tokens
- **Secure secret storage** — system keychain integration (macOS Keychain, Linux secret-service) with `env:`, `file:`, and `keychain:` prefixes
- **Multi-profile config** — named profiles in `~/.config/jamfpro-cli/config.yaml` with `--profile` switching
- **Interactive setup** — `setup` command creates API roles, clients, and stores credentials in one flow
- **Dry-run mode** — `--dry-run` flag previews mutating operations without executing
- **Destructive operation safeguards** — delete commands require interactive confirmation
- **Smart retry logic** — exponential backoff with `Retry-After` header support
- **Structured exit codes** — codes 0-6 mapped from HTTP status for scripting
- **Shell completion** — bash, zsh, fish, and PowerShell with `completion install`
- **Command aliases** — short names (`comp`, `md`, `scr`, `bld`, `cat`, `dept`, `cfg`)
- **Command discovery** — `commands` subcommand for scripts and AI agents
- **Classic API support** — policies, profiles, packages, patch titles, and more via `/JSSResource/` endpoints

### Security

- Response body size limits on all handwritten HTTP reads to prevent memory exhaustion
- Error response bodies capped at 1 KB
- Request body buffering capped at 100 MB for retry replay
- No plaintext secret storage — bare values auto-stored in system keychain
- `NO_COLOR` environment variable respected

### Supported Platforms

- macOS (amd64, arm64)
- Linux (amd64, arm64)
- Windows (amd64, arm64)

Available via Homebrew, `.deb`, `.rpm`, and direct binary download from GitHub Releases.
