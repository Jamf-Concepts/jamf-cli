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

## `get --list-file` and the shared resolver

Issue #342 asked for this command: `pro computer-inventory get --list-file
desktops.txt`.

- **One resolver for every list.**
  - A device resource goes to the device resolver, which already existed for
    the device actions. One line may be an ID, serial number, UDID, management
    ID or name. Each kind is looked up in its own batched `=in=` query and
    resolved to the Jamf Pro ID.
  - Any other resource goes to `resolve.ResolveNamedFile`. An all-digit line is
    always an ID. Every name is resolved with one `NameLookup` call:
    - The Pro API version, `ProNameLookup`, sends chunked
      `name=in=(…)` filters. If the server answers 400, it retries with OR'd
      `==` clauses, then lists the whole collection.
    - The Classic version, `classicNameLookup`, makes one collection GET. After
      that it calls the resource's other lookup endpoints (for example
      `/invitation/{v}`) only for names the collection does not hold.
- **The template code it replaced**, from the generated `delete`:
  - It looped over the lines and resolved each one separately: one request per
    name on the Pro API, and one *whole collection* per name on Classic.
    Verified on the wire: 3 names cost 3 GETs, and 2 Classic names cost 2 full
    `/printers` listings. They now cost 1 and 1.
  - It stopped at the first line it could not resolve, and prompted when a name
    matched several records.
  - Both paths now follow the device rule: warn, skip, count towards exit 7.
- **Fetching a record is the `get <id>` request.**
  - On the Pro side, the template wraps the single-record request in
    `fetchOne`, so section handling, the detail-path override and the version
    fallback all apply the same way to every record in a list.
  - Classic records go through the same MCP-child redaction as `get <id>`
    (`TestClassicListGet_RedactsInAnMCPChild`, mutation-checked).
- **Output.**
  - Pro: one JSON array in list order, printed through `PrintRaw`, so every
    `-o` format, `--select` and `--out-file` applies.
  - Classic, default format: one XML document shaped like a Classic list.
  - Classic, any other format: an array of unwrapped records.
- **Live results** on `pro-nmartin` (direct) and `platform-nmartin` (EU gateway).
  - A 6-line mixed file (ID, serial, UDID, management ID, name, one bad line)
    cost 4 resolution GETs plus 5 record GETs, and exited 7.
  - `iPhone` was skipped as ambiguous (it matches mobile devices 8 and 10).
- **`TestEveryNamedGetTakesAListFile`** holds every Classic `get` and every Pro
  `get` that takes `--name` to the convention. Each exemption carries a reason,
  and a stale exemption fails.
