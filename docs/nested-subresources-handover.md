# Handover: nest independently-writable sub-resources under their parent

**Branch to build on:** `feat/spec-ingest-path-derived-naming` (12 commits, clean
tree, suite/lint/`verify-*` all green as of 2026-09-09)
**Status:** designed and measured, not started. **Delete this file before merge.**

Read `docs/spec-ingest-handover.md` first — it covers the tag+path naming change
this sits on top of.

## The goal, in one line

`pro sso-settings cert update` instead of `pro sso-settings update-cert`, derived
from the spec, for every sub-path the API makes independently writable.

## Why

A tag can cover several path roots, and the grouping merges them into one
resource. Flattening a sub-resource into its parent produces a plain CRUD verb
that silently belongs to the sub-resource. On `pro sso-settings` today:

| command | endpoint | what it actually acts on |
|---|---|---|
| `get` / `update` | `GET`/`PUT /v3/sso` | the SSO configuration — correct |
| `cert` | `GET /v2/sso/cert` | the certificate (JSON metadata) |
| `create-cert` / `update-cert` | `POST`/`PUT /v2/sso/cert` | the certificate |
| **`delete`** | `DELETE /v2/sso/cert` | **the certificate, not the config** |
| **`download`** | `GET /v2/sso/cert/download` | **the certificate file** (`text/plain`) |
| **`parse`** | `POST /v2/sso/cert/parse` | **the certificate** |

`delete`, `download` and `parse` collide with nothing, so they keep the plain
name and read as operations on the SSO configuration. `resolveNoParamConflicts`
only renames a verb that *collides*; a lone verb on a sub-path is not reached by
any existing pass. Wanted:

```
pro sso-settings get|update                      # /v3/sso
pro sso-settings cert get|create|update|delete   # /v2/sso/cert
pro sso-settings cert download|parse             # /v2/sso/cert/{download,parse}
pro sso-settings failover get|generate           # /v1/sso/failover
pro sso-settings history|add-history-note        # /v3/sso/history
```

## The rule that decides what is a sub-resource

**A sub-path that is independently writable — carries `PUT`, `PATCH` or `DELETE`
on the sub-path itself — is a sub-resource. A `POST`-only sub-path is not.**

A `POST` on a sub-path is an append or a command submission; `PUT`/`PATCH`/`DELETE`
is ownership of a separately-writable object. Derived from the methods the spec
declares, so a new tag or a path added to an existing tag is classified by the
same test with nothing to update.

Measured over the committed specs — this is the whole partition, and it needs no
override table:

**9 sub-resources**

| parent | sub-path | methods |
|---|---|---|
| `sso-settings` | `/sso/cert` | GET POST PUT DELETE |
| `app-installers` | `/app-installers/global-settings` | GET PUT |
| `enrollment` | `/adue-session-token-settings` | GET PUT |
| `local-admin-password` | `/local-admin-password/settings` | GET PUT |
| `self-service` | `/self-service/settings` | GET PUT |
| `self-service-plus` | `/self-service-plus/settings` | GET PUT |
| `managed-software-updates-plans` | `/managed-software-updates/plans/feature-toggle` | GET PUT |
| `csa` | `/csa/token` | GET DELETE |

(Eight rows, nine paths: `sso-settings` also has `/sso/failover`, GET plus
`POST /sso/failover/generate` — see "Judgement calls left open".)

**22 sub-paths that must NOT split.** 18 are `/history` (GET+POST); the other
four are `enrollment/access-management`, `enrollment-customizations`,
`inventory-preload/csv` and `mdm/commands`, all GET+POST. The exclusion is by the
stated rule, not by a `history` special case — which is the right outcome: if
Jamf ever ships `DELETE /x/history`, splitting it is defensible.

Reproduce the partition with a throwaway test in `generator/parser` that dumps
`(resource, op, method, path)` from `parseCommittedSpecs` and groups the
no-param paths; the measurement script is in this session's transcript and takes
about ten minutes to rewrite from the rule above.

## What A requires

Five pieces. The third is the one that makes A bigger than it looks.

### 1. A third level in the parser's model

`parser.Resource` (`generator/parser/types.go:37`) has `Name` and `Operations`
and nothing between them. Add sub-resources — a name plus its own `[]*Operation`
— and move the matching operations off the parent. The partition happens in
`GroupPathsByTagAndCollection` (`generator/parser/taggroup.go:64`), which already
computes each group's `Root`; the sub-resource test is a second pass over the
group's own paths.

**Exclude the group's declared root from the test.** A probe using a heuristic
root ("the path every other sits beneath") reported `/enrollment` itself as a
split candidate, because `/adue-session-token-settings` is not beneath it. The
grouping pass has the real root, so use it.

### 2. Emit the nesting

`resourceTemplate` (`generator/parser/generator.go:1975`) emits a flat
`newXxxCmd` that `AddCommand`s one constructor per operation. It needs to emit a
parent per sub-resource whose children are that sub-resource's operations, and
the operation names inside a sub-resource become plain verbs (`get`, `create`,
`update`, `delete`) — that is the whole point, so the renaming passes must run
per sub-resource rather than over the parent's whole op list.

**No generated Pro resource nests today.** The only depth-4 `pro` commands are
`pro blueprints components …`, hand-written in `pro_blueprints.go:637`. There is
no precedent in the generated tree to copy.

### 3. A redirect mechanism the deprecation layer does not have — the blocker

`applyDeprecatedNames` (`internal/commands/deprecated_names.go:204`) builds
`byName` from `pro.Commands()` and redirects with
`target.Aliases = append(target.Aliases, old)`. A cobra alias is a name **on one
command**, so it can only ever point at a direct child of `pro`.

Under A, `main`'s `sso-settings-cert` has to resolve to `pro sso-settings cert` —
two tokens — which that mechanism cannot express. Options, in preference order:

1. **A stub command that re-executes.** Register `pro sso-settings-cert` as a
   parent whose subcommands mirror the nested ones and whose `RunE` prints the
   deprecation warning and delegates. Honest and discoverable; needs the mirror
   to stay in step, so it should be generated from the same table rather than
   hand-written.
2. **Rewrite argv in `PersistentPreRunE`** before cobra resolves. One place, no
   mirrored tree — but it fights cobra's own resolution and would need care
   around flags interleaved between the product and the resource, which is the
   trap `docs/spec-ingest-handover.md` records for `cmd.CalledAs()`.
3. **A `nestedAliases` table** mapping an old resource name to a two-token path,
   consumed by whichever of the above is chosen. Needed either way; keep it
   beside `deprecatedNames` so one expiry date governs both.

Whatever is chosen must keep `TestEveryFormerResourceNameStillResolves` passing —
163 former names, 60 live, 103 aliased, 3 withdrawn, nothing orphaned.

### 4. Thread the third level through the consumers keyed on `(resource, op)`

Each of these keys on a resource name plus an operation name and will silently
miss a nested one — silently, because a lookup that misses falls back to a
default with nothing reported. That failure mode has already cost this branch six
user-visible defects (see `TestEveryResourceKeyedOverrideNamesALiveResource`).

- `smoke_registry.go` — entries are `{Resource, Operation, Method, Path, …}`
  (e.g. `{Resource: "sso-settings", Operation: "failover", …}`).
- `backup_registry.go` — list+get pairs per resource.
- The twelve resource-name-keyed tables in `generator/parser/parser.go`, listed
  in `resourceKeyedOverrides()` in `generator/parser/override_keys_test.go`.
  `resourceTableColumns` and `resourceDefaultSections` are the ones most likely
  to want a sub-resource key.
- The gateway annotation stamping (`generator/gateway`) and `applyGatewayAnn` /
  `gatewayPrivAnn` — a nested command still needs `jamf:gateway*` or
  `checkAPIMatch` cannot refuse it pre-flight.
- `collectCommands` (`internal/commands/root.go`) — product and group are
  inherited down the tree; check a nested leaf still reports the right `group`.
- `proGroupMap` (`internal/commands/groups.go:158`) — keyed on a **direct child
  of `pro`**, so a nested sub-resource inherits its parent's help group whether
  that is right or not. Decide whether that is acceptable or whether the map
  needs to accept a two-token key.

### 5. Guards

- **Pin the partition.** A committed test listing the 9 sub-resources and the 22
  excluded sub-paths, so a spec drop that moves one fails visibly instead of
  silently renaming commands. This is the reproducibility property the whole
  naming change exists to establish.
- **No ambiguous plain verb.** Assert that no plain CRUD verb on a parent
  resource maps to an endpoint outside the parent's own root — the defect that
  motivates A. Written against the shipped tree, it is the test that would have
  caught `pro sso-settings delete`.
- **Every former invocation still resolves.** Extend the sweep described under
  "Verifying" below into a committed test if the baseline can be committed as a
  fixture, the way `endpoints-before-path-grouping.tsv` already is.

## Judgement calls left open

- **`/sso/failover`** is GET plus `POST /sso/failover/generate`. By the rule it
  is not a sub-resource (no PUT/PATCH/DELETE), so it stays flat as `failover` and
  `generate` — and `generate` is then another lone verb on the parent. Either
  accept it, or widen the rule to "a sub-path with any deeper operation of its
  own". Widening pulls in `inventory-preload/csv` and `mdm/commands`; decide
  deliberately and record which.
- **A create-and-read-only sub-resource** (GET+POST, no PUT/DELETE) will not
  split. None exists today. The partition guard is what makes its arrival visible.
- **`csa/token`** splits to `csa token get|delete`, leaving `csa` with
  `tenant-id` and nothing else. Check that reads sensibly.

## What is already done on the branch — do not redo

Four commits landed in this session, all green:

- `a2c530a9` — `gateway.successors` is legitimately empty; the guards stay live
  through `gateway.InstallSuccessorForTest` plus delegation assertions.
- `e1ef30c0` — thirteen resource-name-keyed override keys re-keyed after the
  rename (this is the six-defect commit: `pro packages --name` sent `name` where
  the endpoint demands `packageName` and answered `400 INVALID_FIELD`), plus
  `representationSchemas` so `detectNameField` stops reading `x-action` payloads.
- `50a8677e` — the resource root keeps its plain verb.
  **`isResourceRootPath` and the `resourceRoot` parameter threaded into
  `resolveNoParamConflicts` and `renameSingletonRootGet` are the hooks A builds
  on.**
- `7aca1f8a` — CHANGELOG migration table, plus README/GLOSSARY/SKILL.md/CLAUDE.md
  corrections.

**The trap this branch hit three times, so do not reintroduce it:** three
separate passes encoded *"the resource is one path root"* — true while a
resource's identity came from a filename, false once a tag merges several roots.
`resolveNoParamConflicts`, `reclassifyMisannotatedCreates` and
`renameSingletonRootGet` each shipped a differently-wrong command name, at exit
0. A adds a fourth level of structure to the same model; assume every pass that
reasons about "the resource's path" needs re-reading rather than trusting it.

Also: **`specs/AppInstallers.yaml` is derived by `make sync-gateway-coverage`,
not by `make generate`.** A change to `generator/monolith/subtree.go` has no
effect until that target runs. It cost a cycle here.

## Verifying

Live EU environment credentials (authorised for mutation — it is a test tenant):

```bash
export JAMF_URL=https://eu.api.jamfcloud.com
export JAMF_CLIENT_ID=<from the session that produced this brief>
export JAMF_CLIENT_SECRET=<...>
export JAMF_ENVIRONMENT_ID=aee3ec71-d162-4a30-9d90-536ea3dc4f79
```

The reads that exercise every affected resource:

```bash
bin/jamf-cli pro sso-settings get                       # GET /v3/sso
bin/jamf-cli pro sso-settings cert get                  # GET /v2/sso/cert
bin/jamf-cli pro app-installers global-settings get
bin/jamf-cli pro enrollment get
bin/jamf-cli pro self-service settings get
bin/jamf-cli pro local-admin-password settings get
bin/jamf-cli pro packages get --name Homebrew.pkg --field id   # expect 108
```

**The regression sweep that found the six defects, and which A should be checked
against the same way.** Build `main` in a worktree, dump both trees' leaf sets
from `commands -o json`, then resolve every `main` invocation through the new
tree's `root.Find` — checking `len(rest) == 0` so a fall-back to an ancestor
counts as unreachable. Do **not** probe by running the command with a bogus flag:
cobra reports the flag error before the unknown subcommand, which reported 0
broken when 61 were.

Baseline before A: 54 of `main`'s 1355 `pro` invocations unreachable, all
accounted for in CHANGELOG.md — 36 operation renames, 7 withdrawn endpoints, 11
hand-written commands that resolve under new resource names.

Also diff every `resolveNameToID` call site between the trees
(`grep -rhoE 'resolveNameToID(ForApply)?\(reqCtx, ctx\.Client, "[^"]+", "[^"]+", "[^"]+"'`)
— that is what surfaced the six override defects, and A moves the same
resource/op keys around.
