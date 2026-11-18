---
title: "A list of records is --list-file; --from-file is a request body"
date: 2026-11-18
category: conventions
module: internal/listfile
problem_type: convention
severity: medium
applies_when:
  - "Adding a command that acts on several records named in a file"
  - "Adding a flag whose value is a file of IDs, serial numbers, names or URLs"
  - "Touching the generated delete, the device actions or any bulk command"
tags:
  - flags
  - list-file
  - from-file
  - bulk
  - agents
---

# A list of records is `--list-file`; `--from-file` is a request body

## Context

Issue #342 asked for `pro computer-inventory get --from-file desktops.txt`.
`--from-file` already had two meanings:

| meaning | leaves |
|---|---|
| a request body (JSON, YAML, Classic XML) | ~460 |
| a list of IDs, serials, names, profiles or URLs | ~125 (76 generated `delete`s and destructive actions, 34 device actions, the Classic group-member and policy-toggle commands, `pro bulk`, `multi`, `pro setup`) |

The name alone did not say which one a command took. A human had to read the
usage string, and an agent driving the CLI from the catalog often did not read
it. One command needed both meanings: `computer-inventory erase` takes a list of
devices and an optional PIN body, so its body flag was called `--body-file`.

## Decision

- **`--list-file`** is the one flag for a list of records, one per line.
  `--from-file` is a request body and nothing else. `--file` is still only an
  upload or a YAML import (`TestRequestBodyFlagIsUniformlyFromFile`).
- **Rejected names:**
  - `--file`, which the issue thread suggested. That name was already reserved
    for uploads.
  - `--in-file`. It means the same as `--from-file` and brings the ambiguity
    back.
  - `--targets-file`. "Target" is a Jamf Pro scope term (`docs/GLOSSARY.md`).
  - `--ids-file`. It suggests that names are not accepted.
- **Not a root persistent flag.** It would appear under Global Flags on all
  ~1,800 leaves, and ~1,550 of them (`list`, `create`, `apply`, `config`,
  `setup`, …) cannot honour it. The convention comes from one registration
  helper (`listfile.AddFlag`) and a test that walks the tree, the same pattern
  `guardStrayPositionals` uses for positionals.
- **`-` reads stdin, and nothing else does.** A body flag falls back to piped
  stdin when it is absent. A list flag must not, because a CI runner gives
  every process a non-terminal stdin, and a single-ID call would then read an
  empty list.
- **A deprecated alias for six months.** Every leaf that took a list through
  `--from-file` keeps it through `listfile.WithLegacyAlias()`. The alias is
  hidden, bound to the same variable and mutually exclusive with `--list-file`.
  Using it prints pflag's deprecation warning, and it is removed in
  `listfile.LegacyAliasRemoval` (2027-05). A command that gains a list now gets
  no alias.

## Reused, not rebuilt

| need | existing code |
|---|---|
| parsing (BOM, comments, 255-character limit, hiding values in an MCP child) | `listfile.Read` |
| batched device lookup by ID, serial, UDID, management ID or name, and refusing an ambiguous match | `resolve.ResolveComputerEntries` / `ResolveMobileDeviceEntries` |
| exit 7 and `--allow-partial-failure` | `finishBatch`, `batchDeleteError` |
| the MCP `--input-dir` check | `mcpLocalPathFlags`, `refuseLocalPath` |

`multi` and `pro setup` had their own `bufio.Scanner` readers. Both now go
through `listfile.Read`, so they get the same stdin support, BOM handling and
line validation as everything else.

## Traps

- **Stdin is read once.** `pro packages delete` reads the list to find each
  package's file name, and then the generated delete reads it again.
  `listfile.Read` memoises stdin. Without that, the second read returns an empty
  list.
- **`-` over MCP is refused for what it means, not for its path.** If a file
  called `-` sits inside `--input-dir`, it would pass the path check. The
  explicit refusal in `refuseLocalPath` stops it.
- **`pro setup` prompts on stdin.** It registers the flag with `WithoutStdin()`
  and refuses `-` itself.
- **A destructive Pro op that has both a name lookup and a body still gets no
  body flag** (`opTakesBodyFromFile`). Its `--from-file` is the list alias until
  2027-05. Give it a body `--from-file` only after the alias is removed. Doing
  it sooner would read an old list file as a body.

## Guards

- `TestFromFileIsAlwaysARequestBody` fails on:
  - a non-deprecated `--from-file` whose usage describes a list;
  - an alias with no `--list-file` beside it;
  - a `--list-file` that was not registered through `AddFlag`.
- `TestListFileAliasIsTheSameList`: the alias sets the same variable as
  `--list-file`, on hand-written commands and on generated ones.
- `TestBuildChildArgs_RefusesListFileStdin`, plus the `--list-file` cases in
  the MCP input-directory tests.
