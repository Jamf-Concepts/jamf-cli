---
name: common-workflows
description: Use when adding a feature, syncing Jamf Pro/Platform/Security specs, adding an endpoint or handwritten command, refreshing gateway coverage or the permissions map, or running smoke tests.
---

# Common Workflows

## Adding a feature to all generated commands

1. Edit the template `const` in `generator/parser/generator.go` (or `classic/generator.go`).
2. If new template data needed, update `parser.Resource` / `parser.Operation` in `parser/types.go`.
3. `make generate && make test && make verify-generated`.

## Syncing specs for a new Jamf Pro version

Both routes require `JAMF_PRO_VERSION` — it is written to `specs/.spec-version`, which the Makefile bakes into the binary as `specProVersion`. Full walkthrough in `docs/sync-specs.md`.

**A. Monorepo checkout:** `make sync-specs JAMF_SERVER_PATH=/path/to/jss JAMF_PRO_VERSION=11.31.0` → review `git diff --stat -- internal/commands/pro/generated/` → `make test`.

**B. Consolidated `/api/schema/` monolith:**
1. Fetch (needs auth): `curl -H "Authorization: Bearer $JAMF_TOKEN" https://<instance>/api/schema/ -o monolith.json`
2. `make sync-spec JAMF_MONOLITH_SPEC=./monolith.json JAMF_PRO_VERSION=11.31.0`
3. Review `git diff --stat -- specs/ internal/commands/pro/generated/` → `make test`.

The public monolith is a **subset** of the monorepo specs — route B legitimately drops private endpoints.

Route B writes one normalised document, `specs/JamfProAPI.yaml` (`monolith.Normalise`, `generator/monolith/document.go`), and `PruneStaleSpecs` removes the per-resource files an earlier split left behind. App Installers are the one Jamf Pro surface no monolith carries: they are derived from the SDK's `pro_api.json` through `AppInstallerSpecs` (`generator/monolith/overrides.go`), and `PruneStaleSpecs` exempts them.

After ingest, any **new tag** surfaces as a new resource command and trips `TestApplyProGroups_AllCommandsGrouped` — wire into the correct `proGroupMap` entry in `internal/commands/groups.go`.

## Adding a new Jamf Security Cloud endpoint

Unlike Platform, dropping a spec into `specs/.security-source/` isn't enough by itself — the eleven known operations are hand-mapped, so a genuinely new endpoint needs a new entry too:
1. Drop/update the spec in `specs/.security-source/`, run `make sync-security-specs` to copy it into the committed `specs/security/`.
2. Add an entry to `securityOpsByFile` in `generator/parser/security.go` (resource name, operation name, `isDestructive`/`isList` as appropriate). If it's a new spec file, also add it to `SecurityScopeForFile`.
3. `make generate && make test`.
4. Wire the new resource's `New<Resource>Cmd` into `internal/commands/security.go` if it's a new resource; add to `groups.go`'s `securityGroupMap`.

## Adding handwritten commands (Pro, Protect, School, Security, Platform, new product)

See the `where-to-make-changes` skill (`.claude/skills/where-to-make-changes/SKILL.md`) for file locations. Common pattern:
1. Create new file with appropriate prefix (`pro_`, `protect_`, `school_`, `security_`, or new product's).
2. Wire into the product's bridge (`pro.go`, `protect.go`, `school.go`, `security.go`, or `root.go`).
3. Add to `groups.go` and optionally `aliases.go`.
4. For resources needing name-to-ID lookup: add resolver method in `internal/platform/resolve.go` or `internal/protect/resolve.go`.
5. Platform commands gate `RunE` with `requirePlatformClient(cliCtx)`.
6. New product namespace: also update site (`index.html`, `style.css`, `catalog.js`) — `make verify-site` enforces.

## Syncing Platform specs from SDK

```bash
make sync-platform-specs-from-sdk                              # main
make sync-platform-specs-from-sdk JAMFPLATFORM_SDK_REF=v0.20.1 # a tag or full SHA
make sync-platform-specs-from-sdk JAMFPLATFORM_SDK_PATH=/path/to/jamfplatform-go-sdk
```

`scripts/fetch-sdk-specs.sh` does the fetching. Both routes end in the same two things — the files in `specs/.platform-source/` and one recorded revision — so only the fetch differs and the derivation is shared.

## Refreshing gateway coverage

```bash
make sync-platform-specs-from-sdk   # full sync (also refreshes coverage)
make sync-gateway-coverage-from-sdk # coverage only
```

`specs/gateway/coverage.json` is a committed artifact. `make verify-gateway-coverage` is the CI guard. A missing manifest fails the guards rather than skipping them.

## Refreshing permissions map

```bash
make sync-permissions-map   # Refresh internal/privileges/permissions-map.md from Jamf's article
```

`TestCatalogueMatchesThePublishedMap` asserts every row against this file.

## Running smoke tests

```bash
make smoke       # Full GET sweep (uses -run 'TestSmoke_Tier' — NOT TestSmoke_Seed)
make smoke-seed  # Explicitly seed test objects (creates objects on tenant)
```

Note: `make smoke` runs `-run 'TestSmoke_Tier'` — not `-run 'TestSmoke'` which would also match `TestSmoke_Seed` and create objects.
