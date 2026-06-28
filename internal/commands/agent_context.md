# jamf-cli agent operating guide

Durable guidance for AI agents driving jamf-cli. For the live list of every
command, run `jamf-cli commands -o json`. For a command's flags and arguments,
run `<command> --help`.

## Authentication

jamf-cli never accepts passwords, tokens, or client secrets via flags or stdin.
Provide machine credentials through environment variables (CI/CD) or a saved
profile.

Environment variables (override profile config):
- `JAMF_URL` — Jamf Pro instance URL
- `JAMF_TOKEN` — pre-existing bearer token
- `JAMF_CLIENT_ID` / `JAMF_CLIENT_SECRET` — OAuth2 client credentials
- `JAMF_TENANT_ID` — tenant id for Platform gateway auth
- `JAMFPROTECT_URL` / `JAMFPROTECT_CLIENT_ID` / `JAMFPROTECT_CLIENT_SECRET` — Jamf Protect

Profiles: `-p <profile>` (or `JAMF_PROFILE`) selects a saved profile. Create one
with `jamf-cli pro setup` or `jamf-cli protect setup`. Run `jamf-cli doctor` to
check config, credentials, and connectivity without calling the product API.

## Output and agent flags (global)

- `-o, --output <fmt>` — `json` (default when piped), `yaml`, `csv`, `plain`,
  `table` (default on a TTY), `xml`, `raw`
- `--compact` — identity + common fields only; fewer tokens
- `--select <a,b.c>` — project to specific dot-path fields only
- `--field <name>` — print a single field's value
- `-q, --quiet` — suppress all non-error output (hints, spinner, progress); errors still print
- `--no-hints` — suppress advisory hints only
- `--no-input` — never prompt; fail fast if input is required
- `-n, --dry-run` — preview mutations; GET/HEAD still execute
- `--out-file <path>` — write output to a file

Set session defaults with `JAMF_CLI_ARGS`, e.g. `JAMF_CLI_ARGS='--quiet --no-input'`.

## Exit codes

React to a non-zero exit without parsing the message:

| Code | Name              | Agent action                                        |
|------|-------------------|-----------------------------------------------------|
| 0    | success           | —                                                   |
| 1    | general           | unclassified failure                                |
| 2    | usage             | bad flags/args — fix the invocation                 |
| 3    | authentication    | bad/missing credentials — re-check auth, then retry |
| 4    | not_found         | resource missing — list to find valid ids           |
| 5    | permission_denied | account lacks API privileges — not retryable as-is  |
| 6    | rate_limited      | back off and retry                                  |
| 7    | partial_failure   | batch: some succeeded, some failed                  |

Errors print a one-line remediation hint and, with `-o json`, a structured error
envelope.

## Destructive commands

Commands that delete, erase, wipe, lock, restart, shut down, unmanage, or flush
MDM commands require an explicit `--yes`. With `--no-input` and no `--yes` they
refuse to run rather than prompt. In the MCP catalog (`list_commands`) such
commands are marked `"destructive": true`.

## MCP

`jamf-cli mcp serve` exposes the command tree to MCP clients over stdio via two
tools: `list_commands` (catalog) and `run_command` (execute). The server is
pinned to the profile it was launched with; per-command credential/target flags
are rejected.
