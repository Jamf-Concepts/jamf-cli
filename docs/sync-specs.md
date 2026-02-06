# Syncing OpenAPI Specs

This document describes how to update the CLI when a new Jamf Pro Server version is released.

## Prerequisites

- Access to the `jamf-pro-server` repository
- Go 1.21+

## Sync Process

### 1. Sync specs from jamf-pro-server

```bash
make sync-specs JAMF_SERVER_PATH=/path/to/jamf-pro-server
```

This command:
1. Copies OpenAPI specs from `jamf-pro-server/api/api-impl/src/main/resources/swagger_docs/uapi/`
2. Regenerates CLI commands from the specs
3. Formats the generated code

### 2. Review changes

```bash
git diff
```

Check for:
- New commands added
- Existing commands modified
- Any breaking changes to command flags or output

### 3. Test

```bash
make test
make build
```

Verify the CLI builds and tests pass.

### 4. Manual testing

```bash
# Test a few commands against a Jamf Pro instance
./bin/jamfpro-cli computers list
./bin/jamfpro-cli mobile-devices list
```

### 5. Commit and release

```bash
git add .
git commit -m "feat: sync specs with Jamf Pro Server vX.X.X"
git tag vX.X.X
git push && git push --tags
```

## Spec locations

| Source | Path |
|--------|------|
| jamf-pro-server | `api/api-impl/src/main/resources/swagger_docs/uapi/*.yaml` |
| This repo | `specs/*.yaml` |

## Generated files

The generator creates files in `internal/commands/generated/`:

- One file per API resource (e.g., `computers.go`, `scripts.go`)
- `registry.go` - registers all commands with the root command

## Troubleshooting

### Generator fails

Check that specs are valid YAML:
```bash
go run ./generator/main.go --specs ./specs --output ./internal/commands/generated
```

### New endpoints not appearing

The generator only creates commands for endpoints with:
- Valid operation IDs
- Supported HTTP methods (GET, POST, PUT, DELETE, PATCH)

Check the spec file for the missing endpoint.
