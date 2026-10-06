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

One route: an instance's consolidated `/api/schema/` document. `JAMF_PRO_VERSION` is required — it is written to `specs/.spec-version`, which the Makefile bakes into the binary as `specProVersion`. Full walkthrough in `docs/sync-specs.md`.

1. Fetch (needs auth): `curl -H "Authorization: Bearer $JAMF_TOKEN" https://<instance>/api/schema/ -o monolith.json`
2. `make sync-spec JAMF_MONOLITH_SPEC=./monolith.json JAMF_PRO_VERSION=11.32.0`
3. Review `git diff --stat -- specs/ internal/commands/pro/generated/` → `make test`.

`monolith.Normalise` writes the document to `specs/JamfProAPI.yaml` as sorted YAML. `specs/AppInstallers.yaml` is the one other spec: App Installers sits under `hiddenapi/` in jamf/jss, so no `/api/schema/` carries it, and `make sync-platform-specs-from-sdk` derives it from the gateway's Pro API spec (`monolith.ExtractSubtree`, routed by `monolith.AppInstallerSpecs`). `PruneStaleSpecs` exempts every `AppInstallerSpecs` filename, and `TestCommittedSpecsAreTheNormalisedLayout` fails if `specs/*.yaml` is anything other than those two files.

**The jamf/jss route (`make sync-specs`) is retired** and the target refuses. It ran `rm -f specs/*.yaml`, so it deleted both files and every `pro app-installers*` command, and the shipped `jamf-cli` skill fetches `specs/JamfProAPI.yaml` from main. It carried no endpoint `/api/schema/` lacks (checked at 11.32.0-t1787580540993). Do not bring back a per-resource layout for an endpoint the instance does not serve — derive it from the gateway spec the way App Installers is.

Resource identity comes from URL paths (`parser.ParseMonolith`), not filenames. Naming knobs are `resourceNameOverrides` and `readOnlySingletonPaths` in `generator/parser/parser.go`.

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
