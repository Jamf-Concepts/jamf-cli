# Design: HTML reports over MCP

- **Date:** 2026-08-28
- **Status:** Approved for planning
- **Branch:** `feat/mcp-html-reports`

## Problem

An administrator running the jamf-cli MCP server against Claude Desktop asks
for a fleet report they can share with a manager or act on. Today they cannot
get one.

Three things are in the way.

**The MCP server has no report tool.** `internal/commands/mcp.go` ships two
generic tools — `list_commands` for discovery and `run_command` for execution.
`run_command` returns the child's combined output as `mcp.TextContent`, which
is right for a table or a JSON document and wrong for a 320–800 KB HTML file:
that is 80k–200k tokens, and a truncated HTML document is not a smaller report,
it is a corrupt file.

**The renderer was never merged.** `origin/feat/dashboard` carries a
cross-product HTML dashboard command — inline CSS, one inline `<script>`, no
CDN — that renders Pro fleet/security/patch/OS data alongside Protect coverage
and Platform blueprints and benchmarks. It has been sitting unmerged.

**The skill mandates a tool Claude Desktop does not have.** Rule #1 of
`skills/skills/jamf-report/SKILL.md` reads "Never call the Jamf API directly.
Always use `jamf-cli` via the Bash tool." Claude Desktop exposes MCP tools and
no Bash tool, so the skill's single hard rule is unfollowable in precisely the
environment this work targets.

A reference implementation exists outside the CLI — `report.sh` from the
JamfReport repository — which drives `jamf-cli` subcommands from bash and
renders HTML with Chart.js pulled from a CDN. It demonstrates the demand and
sets the output bar. It is not a starting point: it needs a shell, and its
CDN dependency means a shared report renders blank offline.

## Goals

- An administrator talking to Claude Desktop can ask for a fleet report and
  receive a shareable HTML file plus a conversational summary of what it says.
- The same capability is reachable from a plain terminal with no agent at all.
- The MCP server's existing output boundary is not widened.

## Non-goals

- **Returning HTML over the protocol.** Considered and rejected; see
  "Alternatives".
- **Cross-tenant reports over MCP.** The server pins one profile at launch, so
  an MCP report covers that profile. Cross-product reports stay a CLI
  capability via `dashboard --include-profile`.
- **A model-chosen output path.** See "Security model".
- **Correcting the Bash-tool assumption in the other six skills.** All seven
  carry the same line 11; only `jamf-report` is in scope here.

## Security model

The MCP server's output boundary is enforced by `blockedChildFlags`
(`internal/commands/mcp.go:163`):

```go
var blockedChildFlags = []string{"--profile", "--url", "--token-file", "--tenant-id", "--out-file"}
```

`--out-file` is on that list because it would let a connecting model write
command output to an arbitrary host path. Any report feature has to respect
that, which rules out the obvious shortcut of letting the model pass
`--out-file` when the path happens to sit inside the report directory: that
reopens the exact flag the list exists to close, and a path check is a weaker
guarantee than an absent parameter.

The threat is not hypothetical. Device names, policy names and group names are
admin-controlled free text that flows into report content. A model that can
name an output file can be induced to name `~/.zshrc` by a device called
`../../.zshrc`. So:

- The tool input carries **no path, no filename, no profile**. The model
  chooses what the report says, never where it lands or which tenant it reads.
- The filename is **server-derived**: `jamf-report-<profile>-<UTC>.html`, where
  the timestamp is `20060102T150405Z` — so
  `jamf-report-prod-20260828T104300Z.html`. The profile segment is passed
  through `protectFileNameSafe`
  (`internal/commands/protect_backup.go:899`), and is `default` when the
  server was launched with no profile pinned, since a name is what makes two
  reports in one directory tellable apart.
- The title never reaches the filename. That closes the injection path rather
  than sanitising it, and leaves titles free to contain anything the HTML
  escaper handles.
- The file is opened `O_CREATE|O_EXCL`, so a collision errors rather than
  overwriting.
- The child is still launched through `buildChildArgs`, inheriting the pinned
  `--profile` and the enforced `--no-input` the model cannot disable.

## Design

### 1. `report-dir` config field (implemented — `bebf384`)

A single global config field, not per-profile, surfaced through
`pro setup`'s interactive flow where the administrator is already configuring
the CLI.

```yaml
report-dir: ~/Documents/JamfReports
```

- `Config.ReportDir` with `yaml:"report-dir,omitempty"`.
- `Config.ReportDirPath()` expands a leading `~` (`~` and `~/x` only —
  `~admin/x` names a directory literally called `~admin` and is left alone).
- `pro setup` prompts for it once, after the interactive-credentials guard, and
  `MkdirAll`s it at `0o700` to match `config.Save()`. Skipped when the config
  already carries one; `--report-dir` changes it. A directory path is not a
  credential, so a flag is permitted here.
- `config show` reports it; `config validate` **fails** rather than warns when
  it is set but inaccessible, because the MCP server writes there and has no
  path parameter to fall back to.

It is load-bearing only on the MCP path. The CLI treats it as a default
destination — enforcing a directory in a shell the administrator already
controls is theatre.

### 2. The dashboard command (implemented — `f6fda1c`, `c1a5033`)

`jamf-cli dashboard` renders the cross-product HTML report. Revived from
`origin/feat/dashboard`, which was left untouched; the 13 commits were
collapsed to one on a local scratch copy and carried across by explicit file
checkout.

Three local flags only — `--include-profile`, `--title`, `--smart-groups`. It
declares no local `--profile` and no local `--out-file`: Cobra's `AddFlagSet`
skips an inherited flag whose name is already taken *and the shorthand goes
with it*, so the original branch had silently removed `-p` and `-o` from the
command. It writes through `writerFor(cliCtx)` so the root `--out-file` works,
rather than calling `os.Create` itself. `TestDashboardInheritsRootFlags` guards
the trap.

HTML goes to stdout; every partial-failure warning goes to stderr
(`dashboard_pro.go:29`, `dashboard_protect.go:26`,
`dashboard_platform.go:31`).

### 3. The `generate_report` MCP tool

A third tool beside `list_commands` and `run_command`.

```go
type generateReportInput struct {
	Title       string   `json:"title,omitempty" jsonschema:"report title shown in the HTML heading"`
	SmartGroups []string `json:"smart_groups,omitempty" jsonschema:"smart group names to visualize"`
}
```

The handler calls `config.Load()` itself — `newMCPCmd()` and
`newMCPServeCmd()` take no arguments, so there is no `CLIContext` to thread.

Refusals, all before any child process starts:

- `cfg.ReportDirPath()` empty → point the administrator at
  `jamf-cli pro setup --report-dir <dir>`. **Not** `config set report-dir`:
  `config` has no `set` subcommand (`show`, `path`, `list`, `add-profile`,
  `remove-profile`, `set-default`, `validate`), and naming a command that does
  not exist is worse than naming none.
- Directory missing or not a directory → refuse rather than create it. A
  typo'd `report-dir` silently materialising a directory tree is worse than an
  error, and `pro setup` already does the `MkdirAll` when the administrator
  names it.

The arg vector is `dashboard`, plus `--title <title>` when a title was given
and one `--smart-groups <name>` per requested group (the flag is a repeatable
`StringArrayVar`, `dashboard.go:80`). No `--include-profile`: an MCP report
covers the pinned profile only.

Execution keeps the two streams apart, which is why this cannot reuse
`runChild`: that calls `CombinedOutput()`, which would interleave
`WARNING: patch compliance: …` into the middle of the HTML document and
corrupt it.

- `child.Stdout` is the `*os.File` created inside `report-dir`.
- `child.Stderr` is a `bytes.Buffer` whose contents are truncated to the last
  4 KB before they reach the result, so a child that fails once per device
  cannot spend the conversation's context on repeated warnings. The tail rather
  than the head, because the last line is the one that explains the exit.
- Non-zero exit removes the partial file and returns that tail.

The result is the path, the byte size, and the collected warnings — never the
HTML.

### 4. The `jamf-report` skill

Rule #1 stops naming a tool and starts naming the prohibition plus a
two-branch route to `jamf-cli`:

- MCP tools present → use them, and surface the returned path, since that is
  the shareable artifact.
- Otherwise → Bash, as today.

Both branches route through `jamf-cli`, so the prohibition on direct API
access is unchanged.

The five existing text reports (patch-status, device-compliance,
inventory-summary, software-installs, ea-results) stay as they are — they
answer questions in-conversation, where a table beats a file. The dashboard is
added as a distinct capability with its own trigger: when the administrator
wants something to share or action rather than read now. Both invocations are
documented side by side so the skill teaches one capability:

```
MCP:  generate_report { "title": "Q3 Fleet Review" }
CLI:  jamf-cli dashboard --title "Q3 Fleet Review" --out-file ~/Reports/q3.html
```

The presentation workflow gains a fork: for a shareable artifact, generate the
dashboard, report the path, and summarise the highlights in conversation so
the administrator knows what they are about to send on without opening it.

Two limits the skill must state, both consequences of the design above: an MCP
report covers the pinned profile only, and the MCP path cannot choose its
destination. In both cases the skill names the CLI form as the way through.

`skills/.claude-plugin/plugin.json` goes 0.2.0 → 0.3.0 with this section,
which is the only one that changes a skill.

## Testing

`internal/commands/mcp_test.go` exists to pin the output boundary — its header
records that "A connecting model must not be able to redirect to a different
instance or swap credentials by smuggling those flags into the run_command
args array." Any change to that boundary extends it. New cases:

- the filename derives from profile + timestamp and is unaffected by a title
  containing `../`, `/`, or a NUL
- a title with path separators still yields a filename inside `report-dir`
- refusal when `report-dir` is unset, and when it names a non-directory
- the report child's arg vector carries the pinned `--profile` and
  `--no-input`, and no `--out-file`
- an `O_EXCL` collision is an error, not an overwrite

Existing coverage retained: ten `buildChildArgs` boundary tests; the dashboard
renderer tests; `TestDashboardInheritsRootFlags`;
`TestDashboardProfileNames`; `TestReportDirPath`.

## Alternatives considered

**Return the HTML over the protocol.** Rejected. A report is 320–800 KB
≈ 80k–200k tokens, and truncation produces a corrupt file rather than a
shorter report. Pinning a directory costs one config field and yields an
artifact the administrator can open, mail, or attach.

**Permit `--out-file` when the path is inside `report-dir`.** Rejected. It
reopens the flag `blockedChildFlags` exists to close, and substitutes a path
check for an absent parameter. An absent parameter cannot be bypassed by a
path-normalisation bug.

**Skill only, no MCP change.** Impossible. The skill needs a tool to call, and
Claude Desktop has no Bash tool.

**An `mcp serve --report-dir` flag or an env var.** Rejected in favour of the
config field. The administrator is already in `pro setup` configuring
credentials; a flag on `mcp serve` puts the setting in a launcher config file
they edit once and never see again, and an env var is invisible to
`config validate`.

## Known gaps

No Go toolchain on this machine can build the module: `/usr/local/go/bin/go` is
1.13.4 and `go.mod` requires 1.26.6 (`malformed module path "cmp"`). Its gofmt
also cannot parse generics, so formatting checks were narrowed to specific
files. `f6fda1c`, `c1a5033` and `bebf384` are therefore unverified by compiler
or test run. This must be resolved before implementing sections 3 and 4 —
section 3 is boundary-enforcement code, which is exactly the kind that must be
test-driven rather than eyeballed.
