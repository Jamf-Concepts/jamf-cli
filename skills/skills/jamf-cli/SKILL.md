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

### Object ID lookup policy (get / update / delete)

**There is no `--id` flag.** IDs are always positional arguments placed directly after the subcommand:

```
jamf-cli -p <profile> pro <resource> get <id>
jamf-cli -p <profile> pro <resource> update <id>
jamf-cli -p <profile> pro <resource> delete <id>
```

**`--name` is not universal.** Only resources with name-based lookup support it — check `--help` for the resource before assuming it exists. Never attempt `--name` and fall back if it fails; check first.

**Standard lookup pattern when you have a name but need an ID:**

```bash
# 1. List to find the ID
jamf-cli -p <profile> pro <resource> list -o json | jq '.results[] | select(.name == "My Resource") | .id'

# 2. Use the ID positionally
jamf-cli -p <profile> pro <resource> get <id>
```

Do not attempt to guess an ID or try `--name` on a resource that does not advertise it in `--help`.

### Request body policy (create / update / apply / patch)

**Never guess the shape of a request body.** Before constructing any JSON or YAML input for a mutating operation (`create`, `update`, `apply`, `patch`), load the authoritative OpenAPI spec for that resource.

Specs live in three locations depending on the command namespace:

| Command namespace | Spec location | Naming convention | Example |
|---|---|---|---|
| `pro <resource>` (modern API) | `specs/<ResourceName>.yaml` | PascalCase singular | `Category.yaml`, `MobileDevice.yaml` |
| `pro <resource>` (Classic API) | `specs/classic/resources.yaml` | single manifest | `specs/classic/resources.yaml` |
| `pro blueprints`, `pro compliance-benchmarks`, `pro platform-devices`, `pro platform-device-groups`, `pro ddm-reports` (Platform API) | `specs/platform/` (see mapping below) | abbreviated, **not** derivable from the command name | `compliance-benchmarks` → `jamf-compliance-benchmark-engine-api.json` |

Platform spec filenames do **not** follow a `<resource>-api.json` rule — they are abbreviated and must be looked up explicitly:

| Command namespace | Spec file under `specs/platform/` |
|---|---|
| `pro blueprints` | `blueprints-api.json` |
| `pro compliance-benchmarks` | `jamf-compliance-benchmark-engine-api.json` |
| `pro platform-devices` | `device-inventory-api.json` (+ `device-management-actions-api.json` for actions) |
| `pro platform-device-groups` | `device-groups-api.json` |
| `pro ddm-reports` | `Declaration-reporting-openapi.json` |

**Steps:**

1. Determine which namespace the command belongs to (shown in `--help` under "Platform:" vs other groups). Pick the correct spec location from the table above.

2. Fetch the raw spec via WebFetch using the appropriate URL:
   - Pro modern: `https://raw.githubusercontent.com/Jamf-Concepts/jamf-cli/main/specs/<ResourceName>.yaml`
   - Pro classic: `https://raw.githubusercontent.com/Jamf-Concepts/jamf-cli/main/specs/classic/resources.yaml`
   - Platform: `https://raw.githubusercontent.com/Jamf-Concepts/jamf-cli/main/specs/platform/<file>` — resolve `<file>` from the Platform mapping table above (e.g. `jamf-compliance-benchmark-engine-api.json`), do not assume `<resource>-api.json`

3. Extract the request schema (`requestBody` → `content` → `application/json` → `schema`). Use that schema — and only that schema — to construct the body.

4. If you cannot confidently map the resource to a spec file, tell the user and ask them to confirm before proceeding. Do not fall back to guessing.

### apply vs create / update

Prefer `apply` over `create`/`update` whenever it is available for the resource.

- `apply` is a name-based upsert: it creates the resource if it does not exist, or replaces it if it does. It is idempotent and the correct default for configuration management.
- `create` and `update` are lower-level primitives. `create` will fail if the resource already exists; `update` requires knowing the ID and that the resource already exists.
- Singletons (settings-style resources with no `{id}` in their path) have no `apply` — use `get` and `update` directly.

Check `--help` to confirm whether `apply` is available before reaching for `create`/`update`.

### --scaffold before writing a body

Before writing any JSON body for `create`, `update`, or `patch`, run:

```bash
jamf-cli -p <profile> pro <resource> create --scaffold
```

This prints the exact JSON template the server expects. Use it together with the spec (see request body policy above) to construct a valid payload. Never write a body from memory.

### Singleton resources (no ID, no name)

Some resources are singletons — a single settings object with no collection. These use `get` and `update` with no ID or name argument:

```bash
jamf-cli -p <profile> pro <resource> get
jamf-cli -p <profile> pro <resource> update
```

Passing an ID or `--name` to a singleton command will fail. Singletons are identifiable in `--help` by the absence of a positional `<id>` argument and the absence of a `list` subcommand.

### Classic API payloads are XML

Classic API resources (commands under `pro` that map to `/JSSResource/` paths) use XML request and response bodies — not JSON. When constructing a `--from-file` payload or piping input, the content must be valid XML. Check `specs/classic/resources.yaml` for the expected structure.

### List response shape varies — don't assume `.id`

The field name holding the object ID in a `list` response differs by resource. Do not assume `.id`. After running `list -o json`, inspect the actual response shape before writing a `jq` filter:

```bash
jamf-cli -p <profile> pro <resource> list -o json | jq '.' | head -40
```

Common patterns: `.id`, `.computerAppleId`, `.udid`. Always derive the field name from the actual output.

### Platform resource group operations

When the task involves Platform resources (blueprints, compliance-benchmarks, ddm-reports, platform-devices), **always use `pro platform-device-groups` for all group operations** — creating test groups, listing groups, resolving group IDs, assigning scope.

**Never use Classic or Pro API group commands** (`pro smart-computer-groups`, `pro static-computer-groups`, `pro computer-groups`, `pro mobile-device-groups`) for Platform workflows. These are incompatible:

| API | Group ID type | Works with Platform resources? |
|---|---|---|
| `pro platform-device-groups` | UUID (`cda24521-…`) | Yes |
| `pro smart-computer-groups` / `pro static-computer-groups` | Integer (`42`) | No |

Blueprint and benchmark scope fields accept only Platform device group UUIDs. Passing a Classic integer group ID will fail silently or be rejected by the API.

**Pattern for creating a test group and scoping a blueprint to it:**

```bash
# 1. Create the Platform device group
echo '{"name":"Test Group","deviceType":"COMPUTER","groupType":"STATIC"}' \
  | jamf-cli -p <profile> pro platform-device-groups create

# 2. Get the new group's UUID
jamf-cli -p <profile> pro platform-device-groups list -o json \
  | jq -r '.results[] | select(.name == "Test Group") | .id'

# 3. Scope the blueprint using that UUID
jamf-cli -p <profile> pro blueprints get "My Blueprint" -o json \
  | jq '.scope.deviceGroups += ["<uuid>"]' \
  | jamf-cli -p <profile> pro blueprints apply
```
