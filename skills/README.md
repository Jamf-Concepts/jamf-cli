# jamf-cli Skills

Conversational Claude Code skills for Jamf administration. Each skill wraps `jamf-cli` commands in an agent workflow — no direct API calls, no JSON wrangling.

## Installation

Add the marketplace, then install the plugin:

```bash
/plugin marketplace add Jamf-Concepts/jamf-cli
/plugin install jamf-cli-skills@jamf-cli
```

## Prerequisites

- macOS with Homebrew — the `/jamf-cli` skill will offer to install `jamf-cli` automatically if it's missing

## Skills

| Skill | Invoke | What it does |
|---|---|---|
| **jamf-cli** | `/jamf-cli` | Checks install, loads CLI reference into context, selects a session profile. **Always run first.** |
| **jamf-audit** | `/jamf-audit` | Instance health audit — prioritizes findings, explains issues, offers remediation |
| **jamf-backup** | `/jamf-backup` | Config backup, diff against previous backup, optional git tracking |
| **jamf-bulk** | `/jamf-bulk` | Bulk operations across devices, policies, or profiles with confirmation gates |
| **jamf-investigate** | `/jamf-investigate` | Ad-hoc investigation — device history, policy scope, group membership |
| **jamf-migrate** | `/jamf-migrate` | Guided migration between Jamf instances or environments |
| **jamf-report** | `/jamf-report` | Compliance, inventory, and usage reports in table, CSV, or JSON |

## Usage

Every session starts with `/jamf-cli` to verify the install, load the CLI reference, and bind a profile. Then invoke task-specific skills as needed:

```text
/jamf-cli          ← always run first
/jamf-audit        ← then any skill
/jamf-backup
```

Skills use the profile selected by `/jamf-cli` for the duration of the session.
