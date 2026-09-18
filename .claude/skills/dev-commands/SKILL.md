---
name: dev-commands
description: Use when building, testing, generating, syncing specs, or running the CLI — the make targets and example invocations for jamf-cli development.
---

# Build & Dev Commands

```bash
make build                  # Build binary to bin/jamf-cli
make test                   # Run all tests (-v)
make lint                   # golangci-lint (skips generated code via .golangci.yml)
make generate               # Regenerate commands from OpenAPI specs, Classic manifest, and DDM component scaffolds
make sync-specs JAMF_SERVER_PATH=/path/to/jss JAMF_PRO_VERSION=11.32.0  # Copy per-resource specs from jamf-pro-server repo checkout, then regenerate
make sync-spec JAMF_MONOLITH_SPEC=./monolith.json JAMF_PRO_VERSION=11.32.0  # Split a consolidated /api/schema/ JSON into specs/, then regenerate
make sync-platform-specs-from-sdk           # Fetch the SDK's api/ specs (its main by default; JAMFPLATFORM_SDK_REF / JAMFPLATFORM_SDK_PATH override), then regenerate
make sync-gateway-coverage-from-sdk         # Re-derive gateway Pro/Classic coverage + Classic body schemas alone (also run by the target above)
make verify-generated       # Check that generated code is up to date (CI-safe)
make verify-gateway-coverage # Check the gateway coverage manifest and table are current (CI-safe)
make verify-classic-schemas # Check specs/classic/schemas.json matches the SDK's Classic spec (CI-safe)
make sync-permissions-map   # Refresh the committed copy of Jamf's permissions-map article (drives the 403 picker names)
make verify-site            # Check that site supports all product namespaces (CI-safe)
make site                   # Build binary, generate commands.json, serve site locally at :8080
make fmt                    # go fmt + gofumpt
go test -v -run TestFoo ./internal/commands/...  # Run a single test
```

## Running the CLI

```bash
bin/jamf-cli pro setup                    # Interactive first-time config (creates Jamf Pro profile)
bin/jamf-cli protect setup                # Interactive first-time config (creates Jamf Protect profile)
JAMF_URL=https://... JAMF_TOKEN=... bin/jamf-cli pro computers list  # One-off with env vars
bin/jamf-cli -p my-protect-profile protect overview                 # Use a named protect profile
JAMF_CLI_ARGS='--quiet --no-input' bin/jamf-cli pro computers list  # Prepend default flags (CI/CD)

# Platform gateway auth (enables both Pro API and Platform API commands)
bin/jamf-cli config add-profile my-platform --url https://eu.api.jamfcloud.com --auth-method platform --tenant-id <id>
bin/jamf-cli -p my-platform pro blueprints list           # Platform API command
bin/jamf-cli -p my-platform pro computers list            # Pro API routed through gateway
```
