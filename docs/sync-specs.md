# Syncing OpenAPI Specs

This document describes how to update the CLI when a new Jamf Pro Server version is released.

## Prerequisites

- Access to the `jamf/jss` repository
- Go 1.21+

## Sync Process

### Option A: Automated (GitHub Actions)

```bash
gh workflow run sync-specs.yaml \
  -f jamf_pro_version=11.25.2 \
  -f server_repo_ref=11.25.2-t1772925731845
```

### Option B: Manual (local)

#### 1. Clone and extract specs

```bash
git clone --no-checkout --depth 1 --filter=blob:none \
  --branch <tag> https://github.com/jamf/jss.git /tmp/jss-sparse
cd /tmp/jss-sparse
git sparse-checkout set --no-cone '**/swagger_docs/uapi/*.yaml'
git checkout
```

Specs are scattered across subdirectories in the jss repo (not a single folder). The sparse-checkout pattern `**/swagger_docs/uapi/*.yaml` collects them all.

#### 2. Copy specs and update version

```bash
find /tmp/jss-sparse -path '*/swagger_docs/uapi/*.yaml' -exec cp {} specs/ \;
echo "11.25.2" > specs/VERSION
```

#### 3. Regenerate and test

```bash
make generate
make test
make lint
make build
```

#### 4. Add new commands to groups

New commands must be added to `commandGroupMap` in `internal/commands/groups.go`. The `TestApplyGroups_AllCommandsGrouped` test will fail and list any ungrouped commands.

#### 5. Manual testing

```bash
./bin/jamf-cli computers list
./bin/jamf-cli mobile-devices list
```

#### 6. Commit

```bash
git add specs/ internal/commands/generated/ internal/commands/groups.go
git commit -m "feat: sync specs with Jamf Pro v11.25.2"
```

## Spec locations

| Source | Path |
|--------|------|
| jamf/jss | `**/swagger_docs/uapi/*.yaml` (scattered across subdirectories) |
| This repo | `specs/*.yaml` |

## Version tracking

`specs/VERSION` contains the Jamf Pro version the specs were synced from. Updated on every sync.

## Generated files

The generator creates files in `internal/commands/generated/`:

- One file per API resource (e.g., `computers.go`, `scripts.go`)
- `registry.go` — registers all modern API commands
- `classic_registry.go` — registers all Classic API commands

## Troubleshooting

### Generator fails

Check that specs are valid YAML:
```bash
go run ./generator/main.go --specs ./specs --output ./internal/commands/generated
```

Some specs fail to parse due to missing `$ref` targets or schema issues — these are skipped with an error message. This is expected for specs that reference shared definition libraries not included in the uapi directory.

### New endpoints not appearing

The generator only creates commands for endpoints with:
- Valid operation IDs
- Supported HTTP methods (GET, POST, PUT, DELETE, PATCH)

Check the spec file for the missing endpoint.

### Ungrouped commands

After adding new specs, run `make test`. The `TestApplyGroups_AllCommandsGrouped` test will fail and list every command that needs a group assignment in `groups.go`.
