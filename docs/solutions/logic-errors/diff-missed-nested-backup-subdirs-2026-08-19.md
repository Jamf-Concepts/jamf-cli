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
`loadSnapshotFromDirectory` now walks the tree with `filepath.WalkDir` and, per
directory:

- a path in `BackupSubDirs()` is read and then `fs.SkipDir` — a resource directory
  owns its *files*, not its subdirectories, which is what keeps
  `packages/files` (downloaded package binaries) out of the snapshot;
- a parent of a nested resource path (`profiles`) is descended through, never read
  as a resource itself;
- an unknown *top-level* directory keeps the old behaviour, keyed by its own name
  — that is what carries the SDK-backed resources written straight to the backup
  root (`blueprints`, `compliance-benchmarks`) and any hand-assembled tree;
- an unknown *nested* directory is skipped.

Buckets merge with `maps.Copy` exactly as live mode does, so macOS + iOS profiles
(and account users + groups) land in one bucket on both sides.

`backupObjectName` replaces the inline `obj["name"]` read: `name`, then
`displayName`, then `general.name`, then the stem as a last resort.

## Guardrails

`TestLoadSnapshotFromDirectory_EveryCuratedSubDirIsRead` writes one file into
*every* `BackupResource.SubDir` and asserts each is readable under its
`FilterName`. That is deliberately a test over the table rather than over the
thirteen paths, so a nested resource added later cannot reintroduce the class.

`TestBackupObjectName_MatchesLiveNameExtraction` asserts the directory key equals
`extractName` of the corresponding list item — the two loaders are only
comparable if they agree on this, and nothing else in the codebase forces it.

## Watch for

- A resource whose live list name differs from anything in its detail body would
  break key parity again; `backupObjectName` can only see the file on disk.
- `NameField` in the generated backup registry accepts dotted paths
  (`computers-inventory` declares `general.name`) but `extractName` does not
  traverse them. No curated backup resource relies on it today.
- `diff` still says nothing when a resource directory exists but is empty on both
  sides, which is correct but reads the same as "no such resource".
