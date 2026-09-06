---
title: "Every new command must honor the global output/flag matrix"
date: 2026-05-08
category: conventions
module: internal/commands
problem_type: convention
severity: medium
applies_when:
  - "Adding a new top-level command or subcommand"
  - "Adding a new code path that writes to stdout or stderr (status messages, hints, spinners)"
  - "Adding a verbose mode to an existing command"
tags:
  - output
  - flags
  - no-color
  - quiet
  - structured-output
  - cross-flag-honoring
---

# Every new command must honor the global output/flag matrix

## Context

Two follow-up fix PRs shipped immediately after the introduction of two new
commands, both because the original work didn't exercise the cross-flag matrix:

- **PR #194 (spinner+NO_COLOR):** The Pro/Protect/School/Platform spinners
  emit `\r` + `\033[K` ANSI to stderr. The wrapping decision consulted
  `--quiet` and `-v` but not `--no-color` or the `NO_COLOR` env. Users who set
  `NO_COLOR=1` expect ANSI noise to stop, period — that's the
  [no-color.org](https://no-color.org) contract.

- **PR #195 (`version -v` ignoring `-o json/yaml`):** The verbose `version -v`
  path hardcoded text output. Passing `-o json` silently still printed text.
  Worst of both worlds: the user asked for structured output and got prose.
  Fixed by routing the verbose path through the formatter the same way
  `doctor` does, with a partitioned `specSources` shape.

Both fixes are small and obvious in hindsight. The cost was a follow-up PR,
review cycle, and merge for each.

## Guidance

For **every** new command or new output path, exercise this matrix before
considering the work complete:

| Flag / env | Expected behavior |
|------------|-------------------|
| (default) | Human-friendly output |
| `-o json` | Structured JSON; ANSI off; ready to pipe |
| `-o yaml` | Structured YAML |
| `-o csv` | Tabular, where the data is row-shaped |
| `-o table` | Default for lists |
| `--quiet` / `-q` | Suppress advisory output (hints, spinners, progress) |
| `--no-color` or `NO_COLOR=1` | No ANSI escapes anywhere — stdout AND stderr |
| `--out-file <path>` | Output goes to file; also disable color |
| `--field <path>` | Single-field extraction |
| `--select <paths>` | Multi-field projection |
| `--compact` | Drop arrays and nested objects |

### Specific rules

1. **Anything writing ANSI to stderr** (spinners, status updates, hints) must
   consult `noColor` (or `os.Getenv("NO_COLOR") != ""`) alongside `quiet` and
   `verboseLevel`. See `shouldShowSpinner()` in `internal/commands/root.go` for
   the canonical helper.

2. **Verbose/structured commands** (commands with a `-v` mode like `doctor`
   and `version`) must route their verbose output through the shared formatter,
   not through hand-written `fmt.Fprintf` on `os.Stdout`. The formatter knows
   how to honor the global flags; hand-print paths don't.

   **The route is `printRows` for rows, `printSection` for a row set under a
   section header, and `writerFor` for a bespoke text renderer that takes a
   writer** — all in `internal/commands/output_route.go`, and
   `TestNoFileBuildsItsOwnOutputFormatter` refuses a formatter built anywhere
   but the three sanctioned sites. `cliCtx.Output.PrintRaw(...)` is the
   **wire-bytes** path only, for a response body that is already serialised.
   Do not reach for it with rows in hand: it parses them back to the type they
   already were, which measured ~1.0s and up to 1.5GB on a fleet-sized report,
   and it lands `-o raw` and `-o xml` on a renderer the formatter's own
   dispatch never selects.

3. **Marshal structs; don't hand-format prose** when the output is structured.
   Define an explicit response struct with JSON tags. Let the formatter handle
   indentation, projection, and filtering.

4. **Test what you ship.** Before merge, run the new command with at least:
   ```bash
   bin/jamf-cli <new-cmd>                # default
   bin/jamf-cli <new-cmd> -o json
   bin/jamf-cli <new-cmd> -o yaml
   bin/jamf-cli <new-cmd> --quiet
   bin/jamf-cli <new-cmd> --no-color
   bin/jamf-cli <new-cmd> --out-file /tmp/out.txt
   ```

   For commands producing lists, also test `--compact`, `--select`, and
   `--field`.

## Why this beats the alternative

The alternative is "land the command, fix the flag interactions in follow-up."
That's what happened in #194 and #195. Each follow-up costs a PR, a review,
and a merge — and exposes users to surprising behavior between releases.

The matrix is short enough to walk every time. A `make smoke-cli` target that
runs each new command through the matrix would automate this; until then, the
checklist above is the discipline.
