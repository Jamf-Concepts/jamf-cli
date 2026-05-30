# Design: `--no-hints` flag + `JAMF_CLI_NO_HINTS` env var

- **Issue:** [#214 — Way to disable hints without using --quiet](https://github.com/Jamf-Concepts/jamf-cli/issues/214)
- **Date:** 2026-05-30
- **Status:** Approved for planning

## Problem

`--quiet` / `-q` is overloaded. A single flag drives three independent
suppression paths:

```
--quiet / -q  ──┬─→ Formatter.SetQuiet()   → suppresses the list-size "hint:" line (internal/output/output.go:190)
                ├─→ shouldShowSpinner()     → suppresses the "Loading…" spinner      (internal/commands/root.go:146)
                └─→ (implicit) progress msgs many commands gate stderr chatter on it
```

A user who only wants the advisory hint gone
(`hint: 73 results returned. Narrow with --select=<fields>, --compact, …`)
must currently also disable the loading spinner and all progress feedback.
The issue requests a narrower opt-out, suggesting `--no-hint`.

Today there is exactly **one** hint: `maybePrintListHint` in
`internal/output/output.go:189`, which fires only for non-table output formats,
when the row count is ≥ `listHintThreshold` (50), and when not quiet.

## Goals

- Add a flag that suppresses advisory hints **without** affecting the spinner
  or progress output.
- Provide a persistent, discoverable way to set it (env var) for users who
  always want hints off.
- Keep `--quiet` behavior unchanged (it remains a strict superset).

## Non-goals

- Splitting `--quiet` into component flags. `--quiet` suppressing the spinner
  and progress output is existing, relied-upon behavior — the documented CI
  pattern is `JAMF_CLI_ARGS='--quiet --no-input'` (`root.go:417`). Changing what
  `--quiet` does would be a silent breaking change.
- A config-file setting for hints (`disable-hints: true`). Out of scope; the
  env var plus the existing `JAMF_CLI_ARGS` mechanism cover persistence.
- New hint types. `hints` is framed as a category for naming durability only.
- A "force hints on" override flag. No use case; matches `NO_COLOR`'s one-way
  model.

## Behavior contract

| Input | Hint | Spinner | Progress msgs |
|---|---|---|---|
| (default) | shown | shown | shown |
| `--no-hints` or `JAMF_CLI_NO_HINTS=1` | **off** | shown | shown |
| `--quiet` / `-q` | off | off | off |

`--no-hints` and `--quiet` compose: the hint is suppressed if **either** is set.
`--quiet` remains a strict superset of `--no-hints`.

## Naming

- Flag: `--no-hints` (plural — treats hints as a category; matches the existing
  `--no-color` / `--no-input` `no-*` convention; survives a future second hint
  without a rename).
- Env var: `JAMF_CLI_NO_HINTS` (mirrors the `JAMF_*` env namespace; parallels
  `NO_COLOR`).

## Design

The change is isolated. The hint is emitted **inside** the `Formatter`, so
generated and hand-written commands need no edits — wiring one `Formatter`
field plus one global flag covers every command at once. Six edit sites total.

### 1. `internal/output/output.go` — the only behavioral logic

- Add field to `Formatter`:
  ```go
  noHints bool
  ```
- Add setter (mirrors `SetQuiet`):
  ```go
  // SetNoHints suppresses advisory hints (e.g. the list-size hint) written
  // to stderr, without affecting the spinner or progress output. A narrower
  // opt-out than SetQuiet.
  func (f *Formatter) SetNoHints(v bool) {
      f.noHints = v
  }
  ```
- Gate in `maybePrintListHint` (currently line 190):
  ```go
  if f.quiet || f.noHints || rowCount < listHintThreshold {
      return
  }
  ```

### 2. `internal/commands/root.go` — flag, env, wiring

- New package-level var alongside the other global flags (`var (… quiet bool …)`):
  ```go
  noHints bool
  ```
- Env honoring in `PersistentPreRunE`, immediately after the existing
  `NO_COLOR` block (~line 425):
  ```go
  // JAMF_CLI_NO_HINTS disables advisory hints (parallels NO_COLOR, but
  // value-parsed so JAMF_CLI_NO_HINTS=0 leaves hints on).
  if b, err := strconv.ParseBool(os.Getenv("JAMF_CLI_NO_HINTS")); err == nil && b {
      noHints = true
  }
  ```
  Requires adding `"strconv"` to the import block.
- Wire into the formatter immediately after `formatter.SetQuiet(quiet)` (line 458):
  ```go
  formatter.SetNoHints(noHints)
  ```
- Register the flag near the other persistent flags (~line 580):
  ```go
  cmd.PersistentFlags().BoolVar(&noHints, "no-hints", false,
      "suppress advisory hints (e.g. large-result narrowing tips); keeps spinner and progress output")
  ```

### Env-var parsing rationale

`NO_COLOR` (the adjacent precedent) is presence-based: any value, even empty or
`0`, enables it. For our own `JAMF_CLI_NO_HINTS` we deliberately diverge and use
`strconv.ParseBool`, so `JAMF_CLI_NO_HINTS=0` / `=false` leaves hints **on**.
This avoids the well-known `NO_COLOR=0`-still-disables footgun for a Jamf-owned
variable where users will reasonably expect `=0` to mean "off". The flag default
is `false`; both the flag and the env var only ever turn the suppression **on**,
so there is no flag-vs-env conflict to resolve.

### 3. Docs

- Add a one-line mention of `JAMF_CLI_NO_HINTS` to the root command `Long`
  help text, next to where `JAMF_CLI_ARGS` / `NO_COLOR` are already documented
  (`root.go` ~line 416).
- CLAUDE.md `Conventions` note about `NO_COLOR` may optionally gain a parallel
  line — decided during spec review, not required for the feature.

## Testing

Per the project's output-flag-matrix convention
(`docs/solutions/conventions/output-flag-matrix-2026-05-08.md`):

- **`internal/output/list_hint_test.go`**: add `TestListHint_SuppressedByNoHints`,
  mirroring the existing `TestListHint_SuppressedByQuiet` — set `SetNoHints(true)`,
  print ≥ threshold rows in a non-table format, assert stderr is empty. The
  existing fire/threshold/table/format tests already cover the
  hints-still-shown-by-default cases.
- **`internal/commands/root_test.go`**: add a test that `JAMF_CLI_NO_HINTS=1`
  (via `t.Setenv`) results in the hint being suppressed end-to-end, mirroring
  the existing `NO_COLOR` test (~line 1014); and a paired case asserting
  `JAMF_CLI_NO_HINTS=0` leaves the hint shown (locks in the `ParseBool` choice).
- **Manual matrix check before declaring done**:
  - `-o json` on a list returning ≥ 50 rows with `--no-hints`: hint gone, JSON
    output intact, spinner still appears during the request.
  - Same with `JAMF_CLI_NO_HINTS=1` and no flag.
  - `--quiet` still suppresses both hint and spinner (unchanged).
  - `JAMF_CLI_NO_HINTS=0` shows the hint.

## Files touched

| File | Change |
|---|---|
| `internal/output/output.go` | `noHints` field, `SetNoHints` setter, gate in `maybePrintListHint` |
| `internal/commands/root.go` | `noHints` var, `strconv` import, env parse, `SetNoHints` wiring, flag registration, `Long` help line |
| `internal/output/list_hint_test.go` | `TestListHint_SuppressedByNoHints` |
| `internal/commands/root_test.go` | `JAMF_CLI_NO_HINTS` env tests (on + off) |
