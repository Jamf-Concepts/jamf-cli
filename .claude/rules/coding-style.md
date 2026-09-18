---
# Coding Style and Conventions

## Output Routing — Always `printRows`, Never `output.New`

**Print rows through `printRows`, never `output.New`.** `internal/output.New` sets 3 of the formatter's 8 configured fields. The other five (`--out-file`, `--select`, `--compact`, `--quiet`, `--no-hints`) are applied to the one formatter `PersistentPreRunE` builds and stores as `cliCtx.Output`. A command that constructs its own parses all six global output flags and then discards them, with no symptom: `--out-file` exits 0 and leaves the file at 0 bytes while the bytes go to stdout. 24 commands did this.

The three sanctioned routes:
- `printRows` — rows
- `formatterFor` — a command whose own argument names the format (e.g. `multi`, `group-tools export`)
- `writerFor` — a bespoke text renderer that takes a writer (in `internal/commands/doctor.go`)

The first two are in `internal/commands/output_route.go`. `TestNoFileBuildsItsOwnOutputFormatter` refuses a formatter built outside these three and names its reason at each site. It resolves the import rather than matching construction syntax, so a file that cannot name `internal/output` cannot trip it in any form.

Reach for `output.New` only where the contract is deliberately not the global one. That is the two `--yes` preview tables, which stay a table whatever `-o` says and must not follow `--out-file` into the data file. Record why in the sanctioned-sites map.

## Flag Rules

**Never declare a local flag whose name matches a root persistent one.** Cobra's `AddFlagSet` skips an inherited flag whose name is already taken and the *shorthand goes with it*, so a local `--dry-run` removed `-n` entirely (`unknown shorthand flag: 'n'`, exit 2) and the flag stopped appearing in that command's Global Flags list. Read the package var instead, the way `pro backup` and `protect backup` read `allowPartialFailure` and `protect restore` reads `dryRun`.

Where the collision is unavoidable — `--output` on both `backup` commands is a destination directory, not an output format — the command's `Long` has to say the shorthand is gone, because `JAMF_CLI_ARGS='-o json'` is a documented CI mechanism that then exits 2.

**The adjacent failure is inheriting a flag cleanly and then ignoring it.** A root persistent flag appears in a command's own `--help` whether the code reads it or not, so `protect backup` advertised `-n, --dry-run` and pruned files anyway — a documented flag that does nothing is worse than an absent one. Every command that inherits `-n` either honours it or says in its `Long` what it does not cover.

## Positional Contract — `guardStrayPositionals`

**A command's `Use` string is its enforced positional contract.** `guardStrayPositionals` (`internal/commands/root.go`) walks the assembled tree beside `guardUnknownSubcommands` and gives every leaf that documents no placeholder a validator that refuses one, covering 736 leaves without a second declaration in each.

**A leaf that reads `args` must document the placeholder in `Use`, or the guard clamps it and the positional silently stops working.**

`refuseStrayPositionals` returns an `*exitcode.Error` (not a plain error) because that carries the `Hint`; the exit code is not the reason — `classifyArgsErrors` codes every argument error at cobra's own call site, so a plain error from there still exits 2. The guard also sets `ValidArgsFunction`, since cobra derives no completion from `Args` and the leaf would otherwise offer filenames for a positional it refuses.

**`guardStrayPositionals` must run before `classifyArgsErrors` in `NewRootCmd`.** The guard installs a validator on every leaf that had none and the classifier wraps only what it finds, so reversing the two leaves unwrapped every refusal the walk installs. Swapping the calls left the whole suite green, so `TestEveryLeafRefusesAnUndocumentedPositional` also asserts that no zero-arity leaf's `Args` is `refuseStrayPositionals` by pointer identity — if it is, the wrap did not reach it.

**Five tests hold the whole positional tree:**

- `TestEveryLeafRefusesAnUndocumentedPositional` — every leaf against its own `Use`, plus the refusal's wording and hint. (`pro report software-installs` once declared `cobra.NoArgs` and answered cobra's "unknown command" phrasing with no hint, because the walk read only the exit code.)
- `TestScaffoldKeepsTheDeclaredPositionalCeiling` — the validator a `--scaffold` flag swaps in at runtime.
- `TestNoExampleDocumentsAnUndeclaredPositional` — each leaf's own `Example`. (The generator once rendered `delete 1` and `history 1` on 22 singletons whose `Use` takes no id, so `--help` taught a form the guard refused.)
- `TestEveryExampleInvocationNamesACommandThatExists` — every `jamf-cli` on an `Example` line. (The filter previously hid the head of every pipe; 13 lines on 8 resources opened with `<resource> get` against a resource shipping no `get`.)
- `TestNoCommandLiteralReadsAnUndeclaredPositional` — an AST scan requiring any `cobra.Command` literal that mentions `args` to declare `Args`.

`TestNoCommandLiteralReadsAnUndeclaredPositional` has two blind spots: it sees **composite literals only**, so `cmd.RunE = func(…)` assigned afterwards (as in `platform_account.go` and `platform_audit.go`) is invisible. And it keys on the **spelling** `args` — a closure written `func(cmd *cobra.Command, positional []string)` that reads `positional[0]` passes the scan, gets clamped by the guard, and its positional stops working silently. Every closure in the tree spells it `args` or `_`.

The generator-side guard is `getPipeBlock` / `getByNamePipeBlock` (`generator/parser/generator.go`), which render a read-edit-write example only when the resource ships a `get`, taking positionals from `get`'s own path parameters rather than assuming them.

## Request Body Flag — `--from-file` Uniformity

**The request-body flag is `--from-file` in all four products; `--file` means only an upload.** Platform and Security Cloud generators use `--from-file`; `--file` survives only on the 9 multipart uploads and the two `--file`/`--dir` YAML imports (11 leaves total). Do not rename those — a `--from-file` promises stdin delivery, and a multipart upload cannot keep that promise.

`TestRequestBodyFlagIsUniformlyFromFile` walks the assembled tree and refuses a `--file` that is not on the named upload list.

**A removed flag needs a looked-up hint, not an edit-distance one.** `suggestFlag` anchors on the first letter and takes the nearest flag within two edits — `file` is two edits from `field` and five from `from-file` — so every caller migrating off `--file` was pointed at `--field`. `renamedFlags` (`root.go`) is consulted first and carries both directions. Each entry fires **only where its destination is a real flag on the command in hand**: no command declares both, and `TestSuggestFlag_RenameIsScopedToCommandsThatHaveTheReplacement` asserts that.

**The rename was the smaller half; the stdin gap was the real defect.** `ReadBody` in both `internal/platform` and `internal/security` was `os.ReadFile(file)` only. Both now read stdin when the flag is absent. **An empty pipe is not a body** and an empty named file is: a CI runner hands every process a non-character-device stdin, so `file != ""` in `hasInput` is what keeps `bodyinput.Normalize`'s empty-input error reachable.

## Empty Lists Print `[]`, Never `null`

**An empty list prints `[]`, never `null`.** A nil slice marshals to `null` — so `security ztna-gateways list -o json` answered `null` on a tenant with no gateways while `dns-zones list` answered `[]` for a byte-identical wire response, the difference being only which spec declares `page`/`page-size`. Anything piping to `jq` then failed with "Cannot iterate over null" on exactly the tenants where the collection was empty.

The slices are initialised empty in `generator/platform/template.go`, `generator/security/template.go` and `generator/parser/generator.go` (the Pro `list --all` path). Keep it that way when touching the aggregation loops.

## Go Toolchain Pin

**The Go toolchain is pinned in three places so an artifact cannot depend on which `go` a developer happens to have.** go.mod declares **1.27.0**, CI reads `go-version-file: go.mod`, and `make generate` / `make fmt` export `GOTOOLCHAIN=$(GO_PINNED_TOOLCHAIN)` derived from that same line.

`GOTOOLCHAIN=auto` treats go.mod's version as a minimum, so a newer local toolchain is used silently — and gofmt's alignment rules move between releases. Go 1.26 breaks a map literal's alignment group at a long key where 1.27 keeps one group; on 1.26 a single gofmt pass over generator output was not even a fixed point. It failed in CI on a pure-whitespace diff in a file nobody had touched.

Two consequences:
- **`make fmt` skips the generated trees** (`FMT_GO`), because gofumpt's stricter style disagrees with the plain `go fmt` the generator runs and the two rewrote each other in turn.
- The repo was moved to 1.27.0 rather than left straddling two versions.

## YAML/JSON Decoding

**`encoding/json` matches struct field names case-insensitively; `yaml.v3` does NOT** — it matches the lowercased Go field name exactly. This is why exception export carries json tags and no yaml tags except on genuinely new fields, and why PascalCase keys in a document still bind through JSON but silently drop on yaml.v3 decode.

**What `encoding/json` refuses is a moving target.** `Normalize` (`internal/bodyinput`) and `normalizeInputToJSON` in the Pro template convert YAML → JSON by round-tripping through `json.Marshal` — which refuses the shapes yaml.v3 produces: a mapping with a non-string key (`map[any]any`) and a timestamp scalar (`time.Time`). `jsonSafe` / `jsonSafeYAML` walk the value first. Go 1.26 refuses any non-string key; 1.27 spells an integer key and still refuses boolean, float or null keys — all of which YAML permits. Tests assert the resulting *shapes* rather than error strings, since an integer key no longer catches a regression on 1.27.

## Table Column Visibility

**A table's columns are the keys of its *first* row, so `omitempty` on a row field means "sometimes not a column at all".** `printTable` and `printCSV` (`internal/output/output.go`) both read `sortedKeys(rows[0])`; `--wide` reads it too. `config list` sorts profiles alphabetically, which made one instance profile sorting first hide the scope of every platform profile below it.

The fix is per-command, not in the formatter: `listRowsForFormat` (`internal/commands/config.go`) emits a separate row type for `table`/`csv`/`plain` with no `omitempty` on fields that must always be columns, and leaves `json`/`yaml` untouched — the same narrow-for-columns, full-for-structured split as Protect's `printResult`. A new list command with an optional field needs the per-command treatment or the column silently comes and goes with sort order.

`config list`'s table shows `environment-id` and not `tenant-id` — one scope column, the level Jamf wants integrations created at — with `tenant-id` still in `-o json` for anything parsing it.

## `--dry-run` Per Product

**`--dry-run` is honoured per product, not centrally.** Jamf Pro gets it from `dryRunClient`, which wraps the `HTTPClient`. The Platform SDK client and Security Cloud client cannot be wrapped that way — their transports assert an exact success status and a synthetic response would have to guess 200 vs 201 vs 204 per operation.

Both Platform and Security Cloud generators emit a `cliCtx.DryRun` check on every non-GET operation, reporting method, resolved path and body to **stderr** and returning. `cliCtx.DryRun` is set right after the output formatter in `PersistentPreRunE`, *before* the product branches, because Protect, School and Security Cloud all return from there directly.

Hand-written platform commands have no per-command preview, so `dryRunGuardTransport` refuses their writes with a 412 carrying a `DRY_RUN` code. (A transport *error* would be retried by the SDK, hanging `-n` through the whole backoff ladder.)

**The preview comes before the confirmation, in both templates.** Previously reversed: `ConfirmAction` errors when `--yes` is absent and stdin is not a terminal, so `--no-input -n pro blueprints delete <id>` reported "requires --yes" and previewed nothing. The operator's fix of `-n --yes` would execute the delete if `-n` was dropped. Interactively the old order prompted "Continue? [y/N]" and *then* printed `[dry-run]`, teaching the operator that confirming is harmless. Name→ID resolution stays ahead of both; validations also stay ahead. `confirmStmt` in both emitters keeps the paginated and single-request branches from drifting apart.

## Platform SDK HTTP Client

**Never hand the platform SDK a retry client.** `jamfplatform.WithHTTPClient` assigns whatever is given to the SDK's `retry.HTTPClient`, so an injected retryablehttp client becomes an inner retry loop whose policy wins. Pass a plain `*http.Client` carrying only the timeout, jar and the verbose/spinner transports.

## Output Flags

- `NO_COLOR` env var respected (https://no-color.org).
- `--no-hints` flag / `JAMF_CLI_NO_HINTS` env (value-parsed via `strconv.ParseBool`) suppress advisory hints only, leaving the spinner and progress output intact.
- `--quiet` remains a strict superset of `--no-hints` — it silences both hints and spinner/progress.

## Filename Prefixes

- `pro_` — Jamf Pro + Platform handwritten commands
- `protect_` — Jamf Protect
- `school_` — Jamf School
- `security_` — Jamf Security Cloud (currently just `setup`; all generated Security Cloud commands are generator-owned)
- `pro_platform_` — Platform infix where resource name overlaps with existing Pro resources

Help groups in `groups.go`, short aliases in `aliases.go` — each split into root / pro (including platform: `bp`, `cb`, `pdev`, `pdg`, `ddm`) / protect / school / security.

## `apply` Name Resolution

**The name comes out of the body, never a flag** (`platform.ApplyName`). `apply`'s contract is that the input is the desired state and carries its own identity. A `--name` beside it would be a second source of truth with no correct resolution when the two disagree.

A non-string or empty name is refused before anything is sent, because the value is matched against list results and `"1"` matching an item named `1` is a coincidence rather than a resolution.

**Only a genuine `ErrNotFound` takes the create branch.** Treating an auth error or a 5xx during the lookup as absence is how a failed read becomes a duplicate.
