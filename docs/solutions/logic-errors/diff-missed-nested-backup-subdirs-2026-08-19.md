---
title: "pro diff read one directory level, so every nested resource — config profiles included — silently reported no change"
date: 2026-08-19
category: logic-errors
module: internal/commands
problem_type: bug
severity: high
applies_when:
  - "Adding a BackupResource whose SubDir has more than one segment"
  - "Changing how `diff` loads a backup directory, or how it keys objects"
  - "Adding a field to one side of a resource's backup or diff projection"
  - "A user reports `diff` finding changes for some resources but not others"
  - "Comparing a backup directory against a live instance"
tags:
  - diff
  - backup
  - snapshot
  - directory-layout
  - silent-failure
  - config-profiles
issue: 331
---

# `diff` read one directory level, so nested resources reported no change

## Context

`pro diff` builds a `resourceSnapshot` — `resource → object name → fields` — from
each side, then compares them. A side is either a live instance
(`loadSnapshotFromProfile`) or a backup directory (`loadSnapshotFromDirectory`).
`backup` lays a directory out from each curated resource's `SubDir`
(`internal/commands/pro_resources.go`), and thirteen of those nest two levels:

```
profiles/macos            profiles/ios
prestages/computers       prestages/mobile
extension-attributes/{computer,mobile,user}
smart-groups/{computers,mobile}
static-groups/{computers,mobile}
accounts/{users,groups}
```

## The bug

The directory loader walked exactly one level (`os.ReadDir`, "each subdir is a
resource type") and `readObjectsFromSubdir` skips directory entries. So for
`profiles/` it found zero files, and because a bucket is only added to the
snapshot when it holds at least one object, the key never appeared on *either*
side. `compareSnapshots` had nothing to compare, so it reported nothing.

That is the worst possible failure shape for a diff: not an error, not a warning,
exit 0, and output that is indistinguishable from "these two instances agree".
The reporter saw it as "diff doesn't work with configuration profiles"; profiles
were just the resource they happened to change. Prestages, extension attributes,
smart groups, static groups and accounts were equally invisible.

Two adjacent mismatches came out of the same repro:

- **Directory and live mode bucketed under different keys.** Live mode buckets by
  `FilterName` (`profiles`), the directory loader by top-level directory name. For
  flat resources those strings coincide, which is why the bug looked
  profile-specific. For nested ones, a directory-vs-instance diff had the key
  populated on one side only — so every profile reported as `added` or `removed`.
- **Objects were keyed by filename stem for Classic resources.** The loader read
  `obj["name"]`, but a Classic detail nests its name under `general`, and a
  prestage calls it `displayName`. Falling through to the stem meant the key was
  `SlugifyName` output — possibly with a `DeduplicateSlug` suffix — while live
  mode used the real name from the list item. So `diff --source ./backup --target
  production` reported every Classic object as removed *and* added.

## Fix

`BackupSubDirs()` (`pro_resources.go`) maps each `SubDir` to its `FilterName`, so
the two loaders derive their keys from one table instead of two conventions.
`loadSnapshotFromDirectory` then **reads that table instead of walking the tree**:

- one `os.ReadDir` of the backup root picks up the directories no curated
  `SubDir` claims, keyed by their own name — the SDK-backed resources written
  straight to the root (`blueprints`, `compliance-benchmarks`), any
  hand-assembled tree, and object files left directly in a parent of a nested
  resource (`profiles/*.yaml` beside `profiles/macos/`), where the directory name
  is already the `FilterName`. A failure to read the root is returned as an
  error, not warned about: an empty snapshot there is not a partial result, it is
  "the source was never read", and reporting that as no differences is the same
  silent success this fix exists to remove;
- then every entry of `BackupResources`, in slice order, read at
  `filepath.Join(dir, r.SubDir)`. A missing directory is simply skipped.

Reading the table rather than walking it is what makes the two loaders agree on
more than the key. Both merge shared buckets with `maps.Copy` in
`BackupResources` order, so the entry listed *last* wins a name collision on both
sides — a macOS and an iOS profile both called `Corporate Wi-Fi` resolve to the
same object on disk as they do live. A walk visits lexically (`profiles/ios`
before `profiles/macos`, `accounts/groups` before `accounts/users`) and inverts
that for exactly those two `FilterName`s, which turns a hidden object into a diff
reporting every field of the survivor as modified. Reading by path also bounds
the read to the directories the table names, which is what keeps `packages/files`
(downloaded package binaries) out of the snapshot, and it drops the nested-parent
bookkeeping, the `fs.SkipDir` handling and the depth assumptions with it.

One behaviour is genuinely new rather than preserved: a resource directory may be
a **symlink**. The old loader skipped symlinks (`os.ReadDir` reports a
symlink-to-directory with `IsDir() == false`); reading the curated resources by
path follows them for free, and `entryIsDir` keeps the root-level scan consistent
with that. Worth knowing when handed an untrusted tree — `diff` will read and
print field values from outside it.

`backupObjectName` replaces the inline `obj["name"]` read and takes the
resource's `BackupEndpoint.NameField`, checked first for the same reason
`extractName` checks it first: it is where the resources that do not call their
name `name` keep it — prestages use `displayName`, mobile-device smart and static
groups use `groupName`. Then `name`, then the two shapes no registry field names
(a Classic detail nesting its name under `general`, and `displayName` for a
directory read without a `NameField`), then the filename stem as a last resort.

### The non-standard resources have their own name field

The curated table is not the whole story. `backup` writes the SDK-backed
resources — blueprints and compliance benchmarks — straight to the backup root,
so they have no `BackupEndpoint` and the root scan has no `NameField` to consult.
Blueprints were unaffected because `blueprintToExport` emits a `name`, which
`backupObjectName` finds. Benchmarks keep their name in `title`, so they fell
through to the stem: a disk key of `cis-level-1` against a live key of
`CIS Level 1`, every benchmark reported removed and added.

`nonStandardBackupFilters` (`pro_resources.go`) therefore carries a `NameField`
per entry, and the root scan reads it through `nonStandardBackupNameField`. One
table rather than a second list: a name field listed apart from the filter name
can name a directory that no backup writes, and nothing would catch it.

Keying is only half of matching an object. `diffObjects` unions the key sets of
the two sides and reports any field present on one side only, so two projections
of the same resource make the diff permanently dirty even once the keys agree —
`modified` instead of removed-and-added. Benchmarks had two: `backup` wrote seven
fields and the live loader built five. Both now derive the object from one
`benchmarkToExport`, the way both blueprint paths already shared
`blueprintToExport`.

**A resource is only comparable when both sides agree on the key *and* the field
set.** Fixing one without the other moves the noise rather than removing it.

## Guardrails

`TestLoadSnapshotFromDirectory_EveryCuratedSubDirIsRead` writes one file into
*every* `BackupResource.SubDir` and asserts each is readable under its
`FilterName`. That is deliberately a test over the table rather than over the
thirteen paths, so a nested resource added later cannot reintroduce the class.

`TestLoadSnapshotFromDirectory_EveryCuratedNameFieldIsHonoured` is the same idea
for the key: for every curated resource it writes an object carrying *only* its
declared `NameField`, so a resource added later whose name lives somewhere new
fails rather than silently falling back to the stem.

`TestLoadSnapshotFromDirectory_EveryNonStandardNameFieldIsHonoured` is that guard
for the non-standard table, iterating `nonStandardBackupFilters` on both axes so
an entry naming a directory no backup writes fails here rather than in a diff
against a live tenant.

`TestBenchmarkToExport_DiskAndLiveAgreeFieldForField` asserts an unchanged
benchmark produces no field diffs — the field-set half of the same requirement.
Note its limit: it calls `benchmarkToExport` on both sides, so it proves the
projection is single and correct, but it cannot reach the live loader's call site
(that needs a Platform SDK client). Re-inlining a literal projection there would
not fail this test.

`TestBackupObjectName_MatchesLiveNameExtraction` asserts the directory key equals
`extractName` of the corresponding list item — the two loaders are only
comparable if they agree on this, and nothing else in the codebase forces it.

`TestLoadSnapshotFromDirectory_SharedBucketCollisionMatchesLiveOrder` pins the
collision winner for `profiles` and `accounts` by reading it out of
`BackupResources`, so reordering the table moves the expectation with it.

`TestLoadSnapshotFromDirectory_UnreadableRootIsAnError` and
`TestRunDiff_UnreadableSourceDirectoryErrors` keep an unreadable source out of
the "No differences found" path, at the loader and at the command boundary.

## Watch for

- A resource whose live list name differs from anything in its detail body would
  break key parity again; `backupObjectName` can only see the file on disk.
- `NameField` in the generated backup registry accepts dotted paths
  (`computers-inventory` declares `general.name`) but neither `extractName` nor
  `backupObjectName` traverses them — the latter happens to land on the right
  value through its `general.name` branch, not because the dotted field was
  honoured. No curated backup resource relies on it today.
- `diff` still says nothing when a resource directory exists but is empty on both
  sides, which is correct but reads the same as "no such resource".
