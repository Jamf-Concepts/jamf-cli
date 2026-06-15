---
name: jamf-cli
description: >
  Set up and use the Jamf CLI for the session. Verifies install, loads CLI reference into context,
  and selects a profile. Run this before any Jamf-related task.

  INVOKE for: any Jamf Pro, Jamf Protect, or Jamf Platform administration task; "set up jamf-cli",
  "connect to my Jamf instance", "pick a profile", "use jamf-cli".
tools: Bash, WebFetch, AskUserQuestion
---

# Jamf CLI Session Setup

Work through these steps in order. Do not skip ahead.

## Step 0 — Verify install

Run via Bash:

```bash
which jamf-cli 2>/dev/null && jamf-cli version 2>/dev/null
```

**If the command returns nothing (not installed):**

Tell the user `jamf-cli` is not installed and use `AskUserQuestion` to offer:
- **Install via Homebrew (recommended)** — run via Bash in sequence:
  ```bash
  brew tap Jamf-Concepts/tap
  brew trust Jamf-Concepts/tap
  brew install jamf-cli
  ```
  After install, verify with `jamf-cli version`, then re-run `/jamf-cli` to continue setup.
- **Install manually** — tell the user to visit `https://jamf-concepts.github.io/jamf-cli/` for all install options.
- **Skip** — note that the CLI is unavailable and proceed without it.

Do NOT continue to Step 1 until `jamf-cli` is installed and `which jamf-cli` returns a path.

## Step 1 — Load CLI reference

Fetch the full command reference using WebFetch:

URL: `https://jamf-concepts.github.io/jamf-cli/llms-full.txt`

Prompt: "Return the full content verbatim — this is a structured CLI reference that will be used as the command map for the rest of the session."

Treat the fetched content as the authoritative source for all available commands, subcommands, flags, and resource names for the remainder of the session.

## Step 2 — Select a profile

Run via Bash:

```bash
jamf-cli config list 2>/dev/null
```

Parse the profile names from the output. Use `AskUserQuestion` with:
- One option per profile name found
- A **"Create new profile"** option

If no profiles exist, skip the question and go directly to the creation instructions below.

**Existing profile selected:** remember it as the active profile for this session. Confirm: "Using profile `<name>`. Ready for Jamf tasks."

**"Create new profile" selected:** tell the user to run one of the following (type `! <command>` to run it here):
- `jamf-cli pro setup` — Jamf Pro or Platform API
- `jamf-cli protect setup` — Jamf Protect

Then ask them to re-run `/jamf-cli` to pick the new profile.

## Session rules (apply for the rest of the session)

**Always use `jamf-cli` via the Bash tool for all Jamf operations.**

Command pattern: `jamf-cli -p <active-profile> <subcommand> [flags]`

- Never call Jamf APIs directly — no curl or HTTP calls to Jamf endpoints
- Use the CLI reference loaded in Step 1 to map user intent to the correct subcommand
- When unsure of flags or subcommand shape: `jamf-cli -p <profile> <resource> --help`
- For programmatic/piped results use `--output json`; omit for human-readable table output
- Before any destructive operation (`delete`, `apply` replacing existing): confirm with the user
- Before bulk operations: show the full command, get confirmation, then run
