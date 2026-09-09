# Handover: path+tag derived `pro` command names

**Branch:** `feat/spec-ingest-path-derived-naming` · **Started:** 2026-09-09
**Status:** mechanism, tag naming and the deprecation aliases all landed and
guarded. 13 tests failing — 6 in one known class, 7 stale name/count assertions.
**Delete this file before merge.**

## The goal, in one line

`pro` command names come from the spec (URL paths, bounded by OpenAPI tags)
instead of from the names of 165 spec files, and `specs/` holds one normalised
document instead of 165 carved-up ones.

## Why

A resource's name came from the filename its paths were split into — upstream's
jss module names, which appear in no spec. Four things followed from that
filename with nothing stating them: the command name, the endpoint-version family
(keyed on a `-vN` suffix), whether a `-preview` tag reached a command, and
whether the splitter could delete the file. Two consequences:

- The ingest was idempotent but not reproducible. Wiping `specs/*.yaml` and
  re-ingesting the same live monolith gave 127 files instead of 165, 132
  resources instead of 169, 73 operations dropped, 98 command groups renamed and
  18 `-preview` tags published as commands — at exit 0.
- Version consolidation read a filename suffix, which is how the CLI lost its v4
  computer-inventory endpoints once. It is now a fact about the paths, so that
  class of bug is inexpressible rather than guarded.

## The rule

**The tag decides what belongs together, the path decides where the boundary
falls, and the name comes from the tag.** Measured on the live 11.31.1 monolith
(550 paths / 820 ops), against 163 resources today:

| | resources | unchanged | renamed | merges | splits |
|---|---|---|---|---|---|
| **tag+path (chosen)** | **136** | **60** | **40** | 24 | **13** |
| path only | 148 | 57 | 44 | 21 | 19 |
| tag only | 125 | 52 | 60 | 19 | 1 |

Neither signal works alone, and both failure modes were measured:

- **Paths alone split a resource upstream considers whole.**
  `/v1/computer-inventory/{id}/erase`, `/vN/computers-inventory/…` and
  `/v1/computers-inventory-detail/{id}` are three path roots and one resource.
- **Tags alone are too coarse to be a command.** The `computer-inventory` tag
  carries 56 operations; the `computer-groups` tag carries two complete CRUD
  sets, and `/v3/computer-groups/{smart,static}-groups/{id}` would both want
  `get`/`update`/`delete`. `disambiguateSameTerminalOps` cannot separate them —
  same terminal segment, same parameter count.

Tags are sound here rather than convenient: **no path in the document carries two
different tags**, so the grouping is unambiguous.

Naming came from the path first, to minimise renames, and moved to the tag
because lining up with the API reference is permanent where churn is one-time.
Measured either way, against 163 resources before:

| | resources | unchanged | renamed | merges | splits |
|---|---|---|---|---|---|
| tag names (chosen) | 134 | 53 | 44 | 22 | 13 |
| path names | 136 | 60 | 40 | 24 | 13 |

Seven fewer names survive; the aliases absorb it. The decisive argument was not
the counts but the override table: path naming needed eight entries and seven
were names invented here — a `pki-` prefix the reference does not use,
`ldap-lookups`, `ddm-clients`, `certificate-authorities`,
`team-viewer-remote-administrations`. The tag already said. One override
survives, `policies` → `policy-properties`, because `policies-preview` strips to
a name that describes a surface the modern API does not have.

A tag covering several groups cannot name them all, and 14 do. The primary
group takes the bare tag; siblings append the distinguishing path segments,
reproducing the names they already have (`computer-groups-smart-groups`).

## What is done

| file | what |
|---|---|
| `generator/parser/pathgroup.go` | path-root rule, name overrides, root merges, drop tables |
| `generator/parser/taggroup.go` | the tag+path hybrid (`GroupPathsByTagAndCollection`) |
| `generator/parser/monolithparse.go` | `ParseMonolith`, `MergeDocuments`, `LoadDocuments`, per-resource `$ref` closure |
| `generator/monolith/document.go` | was `split.go` (907→~480 lines): `Normalise`, `PruneStaleSpecs`, document I/O |
| `generator/monolith/overrides.go` | 109→28 lines; `TagFilenameOverrides`, `DroppedTags`, `PreservedSpecs` deleted |
| `specs/` | 165 files → `JamfProAPI.yaml` (1.58 MB sorted YAML) + `AppInstallers.yaml` |
| `internal/commands/groups.go` | `proGroupMap` re-keyed (87 renames, 24 merges deduped) |
| `internal/commands/pro.go` | suppressions retargeted; `computers` no longer suppressed |

Deleted: the layout scan, the tag-derived filename fallback, shared/exclusive
component partitioning, `_MonolithLibrary.yaml`, `PreservedSpecs`,
`DeduplicateVersioned` from the pipeline.

### Guards

- `TestParseMonolith_LosesNoEndpoint` — **the guard for the whole change.**
  Compares against `generator/parser/testdata/endpoints-before-path-grouping.tsv`,
  a committed snapshot of every endpoint reachable under the 165-file layout,
  taken at that layout's final commit. A snapshot because the parse it describes
  no longer exists: a redesign that removes its own baseline has to carry it.
  Delete it when the deprecated aliases go.
- `TestParseMonolith_ScopesSchemasToTheResource` — 703 endpoints compared for
  `NameField`/`IDField` parity. The `$ref` closure moved out of `split.go` into
  the parser; without it every resource would see all 763 schemas and
  `detectNameField` would answer from an unrelated resource.
- `TestDroppedTagsDoNotTakeAVersionedPathWithThem`, `TestKeepPath_*`,
  `TestGroupPathsByCollection_*`, `TestMergeDocuments_*`, `TestNormalise_*`.

## What is left

### 1. The successors table emptied — 6 failing tests

`internal/gateway/note.go`'s `successors` held one entry,
`pro static-computer-groups`, whose v2 resource folded into the v3-served
`computer-groups-static-groups`. Nothing is refused there any more, so the table
is legitimately empty — and 6 tests assert a non-empty one:

```
TestEveryCommandEntryFieldReachesTheCatalog     TestCatalogCarriesTheSuccessorForARefusedCommand
TestTheRefusalNamesAWorkingSuccessorWhereOneShips  TestCatalogJSONCarriesTheSuccessorKey
TestRefusalNamesACuratedSuccessorFirst          TestSuccessorMatchesTheLongestCommandPathPrefix
TestGroupHelpCarriesTheCaveatWhenEveryLeafIsRefused
```

I checked all 67 refused commands for a genuine successor and found none
(`api-roles`, `api-integrations`, `classic-computer-configs`, `system`,
`oauth`/`oauth2`, `environment-type`, `authentications`, `mdm`,
`macos-managed-software-updates` — no gateway-served replacement for any).

**Recommended:** pin the mechanism with a test-local entry, which is this repo's
documented idiom for a live case that resolved — see
`gateway.TestProbedEntriesCarryTheProbeBasis` and
`TestDropUnroutedPlatformOps`. `successors` is unexported, so `internal/gateway`
tests can inject; the two catalog tests in `internal/commands` cannot and should
skip with a stated reason.

### 2. Stale name and count assertions — 7 failing tests

`TestApplyAliases`, `TestCollectCommands` (×3), `TestRequestBodyFlagIsUniformly
FromFile`, `TestNoExampleDocumentsAnUndeclaredPositional`,
`TestGroupHelpCarriesTheCaveatWhenEveryLeafIsRefused`. All reference a resource
name that moved, or a hardcoded count that shifted. Mechanical.

### 2b. Earlier name updates

- `TestChainSkip_RootOnlyNamesDoNotSkipNestedCommands` expects
  `pro mdm-commands`; the operation is now `pro mdm commands`.
- `TestNoExampleDocumentsAnUndeclaredPositional` asserts a hardcoded count of 21
  unmatched leaves; it is 20.

### 3. The renames themselves — **as separate commits**

Nothing after this point is required for a working tree. The 40 renames, 24
merges and 13 splits should land as small reviewable batches, each with its
alias entries, not as one diff. Rough priority:

1. The 10 genuine fixes: 8 double-plural bugs (`csas`, `slasas`, `oidcs`,
   `cloud-informations`, `inventory-informations`, `jamf-pro-informations`,
   `device-compliance-informations`, `jamf-remote-assist-session-histories`),
   `mac-os-managed-software-updates` → `macos-…`, and two names that are simply
   wrong (`patch-titles` holds one POST to accept a disclaimer; `dss-proxies`
   holds `/v1/dss-declarations/{id}`).
2. The 24 merges. Aliases cover these cleanly — one old name, one new target.
3. The 13 splits. **These are the hard ones**: an alias can point at only one
   target, so each needs a judgement about which half inherits the name.

### 4. The alias + deprecation layer — done

`internal/commands/deprecated_names.go`. 102 aliases plus 3 withdrawals that
refuse with an explanation. Expires 2027-03-09;
`TestDeprecatedNamesHaveNotExpired` fails the build then, naming every entry to
delete, and `TestDeprecatedNamesExpiryFiresOnTheDate` exercises the clock either
side so the guard cannot go silently dead. The warning is not silenced by
`--quiet` or `--no-hints`.

**Cobra note, corrected.** `cmd.CalledAs()` does *not* work for this:
cobra records the matched name on every command it traverses but flips
`called` to true only on the executed leaf, so a parent's alias reads as `""`.
`pro icons get 1` resolves `icons` to `icon` and then `get` beneath it, and the
alias is two levels above the leaf. The warning reads the resource token out of
argv instead — exact for every ordinary invocation; a flag interleaved between
the product and the resource misses the warning rather than inventing one.

When this expires, delete `deprecated_names.go`, its wiring in `pro.go` and
`root.go`, `deprecated_names_test.go`, and
`generator/parser/testdata/endpoints-before-path-grouping.tsv`.

### 5. Surfaces carrying command names

`docs/site/catalog.js`, the wiki (separate `.wiki.git` repo), `skills/jamf-cli/SKILL.md`,
`CHANGELOG.md`, `docs/GLOSSARY.md`, and CLAUDE.md's own examples.

## Override tables, and their size

21 entries: **1** name override, 2 root merges, 5 dropped tags, 13 dropped
paths. Tag naming removed seven of the eight name overrides. Every entry carries
its reason in a comment.

Two key-format traps, both of which cost a cycle:
- `pathGroupNameOverrides` keys on the root joined with **`-`** (what `groupName`
  builds); `pathGroupRootMerges` keys on the root joined with **`/`** (the
  grouping map's key, before any name exists).
- A tag is **not** a safe drop unit. `policies-preview` tags the legacy
  `/settings/obj/policyProperties` *and* the live `/v1/policy-properties`; a
  tag-keyed drop deletes a working command silently, because a resource that
  stops being generated reports nothing.

## Corrections to earlier claims in this work

- **`PUT /v1/inventory-preload/{id}` was never a lost capability.** I recorded it
  as one, in a test and in CLAUDE.md. v2 moved every record operation under
  `records/`, so the update lives at `PUT /v2/inventory-preload/records/{id}` and
  is served. The v1 family is gateway-withdrawn *and* fully superseded, and
  `droppedPaths` drops it — left in, the refused v1 paths took the plain `list`,
  `get` and `update` names while the served v2 CRUD sat under a second command.
  CLAUDE.md still cites inventory-preload as the case
  `DeduplicateVersioned`'s base-suppression rule was written for; that reference
  needs revisiting when CLAUDE.md is updated.
- **`GET /v1/branding-images/download/{id}` genuinely was unreachable** —
  `filterToCanonicalPrefix` discarded it from `Icon.yaml` with a warning on every
  generate. That one stands, and it ships as `pro branding`.

## Guards that caught real bugs, so do not weaken them

Each of these failed on a genuine defect during the work rather than being
written afterwards to describe it:

- `TestDeprecatedNamesPointAtCommandsThatShip` — caught `pro.go` still removing
  `computer-inventory` from when that name meant the stray erase/remove-mdm
  pair. Under tag naming it is the primary computer resource, so the removal
  would have deleted `pro comp list`.
- `TestProWiringNamesCommandsThatShip` — `addSubcommand`, `removeSubcommand` and
  `replaceSubcommand` all no-op silently when a parent is renamed. Four parents
  moved (`computers-inventory`, `jamf-protects`,
  `jamf-protect-deployment-tasks`, `jcds`) and every one was silent.
- `TestDroppedTagsDoNotTakeAVersionedPathWithThem` — a tag is not a safe drop
  unit; `policies-preview` covers a live versioned path.
- `TestEveryFormerResourceNameStillResolves` — 163 former names: 71 live, 102
  aliased, 3 withdrawn, nothing orphaned.

## Reproducing the measurements

The live monolith is not committed. Fetch and re-measure with:

```bash
TOK=$(bin/jamf-cli -p default pro auth token --field token)
curl -sH "Authorization: Bearer $TOK" https://nmartin.jamfcloud.com/api/schema/ -o /tmp/monolith.json
make sync-spec JAMF_MONOLITH_SPEC=/tmp/monolith.json JAMF_PRO_VERSION=11.31.1
```

A full ingest of that document produced zero changes to `specs/` and zero to
generated code once the layout settled, which is the reproducibility property
the whole change exists to establish.
