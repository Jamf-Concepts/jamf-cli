# Syncing OpenAPI Specs

This document describes how to update the CLI when a new Jamf Pro Server version is released.

The Jamf Pro API spec comes from one source: the consolidated `/api/schema/`
document that an instance on the target version serves. `make sync-spec`
ingests it and regenerates the commands.

The committed layout under `specs/` is two files:

| File | Source | Written by |
|---|---|---|
| `specs/JamfProAPI.yaml` | the instance's `/api/schema/` document | `make sync-spec` |
| `specs/AppInstallers.yaml` | the gateway's published Pro API spec | `make sync-platform-specs-from-sdk` |

App Installers sits under `hiddenapi/` in Jamf Pro's source, so no
`/api/schema/` document carries it. `make sync-spec` does not write
`AppInstallers.yaml` and does not delete it. `TestCommittedSpecsAreTheNormalisedLayout`
(`generator/monolith/layout_test.go`) fails if `specs/` holds any other
top-level `*.yaml` file, or if either file is missing.

### Why there is no `jamf/jss` route

An earlier `make sync-specs` target copied the per-resource specs out of a
`jamf/jss` checkout. It is retired, and the target now refuses with a pointer
to `make sync-spec`. The route deleted `specs/JamfProAPI.yaml` and
`specs/AppInstallers.yaml`, and it carried nothing that `/api/schema/` lacks.
At build `11.32.0-t1787580540993`, every operation in the checkout's
`swagger_docs/uapi/` tree was also in the instance's document. The route also
could not run at that tag, because two modules ship a file named `User.yaml`.

Do not restore a per-resource layout to get an endpoint that `/api/schema/`
does not serve. Derive it from the gateway's spec the way App Installers is
derived (`monolith.ExtractSubtree`, `monolith.AppInstallerSpecs`).

## Prerequisites

- Go toolchain matching `go.mod`
- Credentials for an instance on the target version

## Ingest the consolidated schema

```bash
# 1. Fetch (needs auth)
TOKEN=$(bin/jamf-cli -p <profile> pro auth token --field token --quiet)
curl -s -H "Authorization: Bearer $TOKEN" \
  https://<instance>/api/schema/ -o /tmp/monolith.json

# 2. Confirm the version you are ingesting
curl -s -H "Authorization: Bearer $TOKEN" \
  https://<instance>/api/v1/jamf-pro-version

# 3. Normalise and regenerate
make sync-spec JAMF_MONOLITH_SPEC=/tmp/monolith.json JAMF_PRO_VERSION=11.32.0
```

`JAMF_MONOLITH_SPEC` also accepts an `http(s)://` URL directly.

`JAMF_PRO_VERSION` is mandatory. It is what `specs/.spec-version` (and therefore
`jamf-cli version`) reports. The target checks the format and stops before it
changes anything if the value does not match. Accepted values are three
dot-separated numbers, with an optional build suffix:

- `11.32.0`
- `11.32.0-t1787580540993` — the form `/api/v1/jamf-pro-version` returns on a
  non-GA instance

Anything else is rejected, including a value that carries whitespace.

`monolith.Normalise` (`generator/monolith/document.go`) writes the document as
sorted, deterministic YAML, so an ingest produces a diff that a reviewer can
read. It also coerces each `example` back to its declared `type`, because the
JSON document turns `example: "3"` into a number. `monolith.PruneStaleSpecs`
then removes any other top-level `*.yaml` in `specs/`, except the App
Installer specs, and reports each removal. The subdirectories of `specs/` are
not touched.

## After the sync

```bash
make test
make lint
make build
```

### 1. Group any new commands

A brand new tag becomes a brand new resource command, which trips
`TestApplyProGroups_AllCommandsGrouped`. Add it to `proGroupMap` in
`internal/commands/groups.go`; the test failure names the command.

### 2. Sanity-check auto-derived resource names

Resource names come from each resource's collection path and are
auto-pluralized. When that reads wrong — a collective noun or a double `s` — add an entry to
`resourceNameOverrides` in `generator/parser/parser.go` and regenerate. Getting
this right before the command ships is much cheaper than renaming it later.

A single-valued endpoint needs the *verb* fixed too, not just the noun: a
GET-only settings-style path with no `{id}` has no PUT for `detectSingleton` to
match, so it generates `list` (plus a meaningless `--field id` example) for an
endpoint that returns one object. Add its path to `readOnlySingletonPaths` in
the same file — that makes it a singleton, which also drops the pluralization,
so no `resourceNameOverrides` entry is needed.

### 3. Check for privilege-name changes

Upstream sometimes renames a privilege (11.31.0 turned `Read Activation Code`
into `Read License Information`). Those strings are surfaced verbatim in
`commands -o json` as `privileges` and appended to the 403 hint at runtime, so a
downstream consumer can break on an otherwise routine sync:

```bash
git diff -- specs/ | grep -E '^[-+].*x-required-privileges' -A 3
```

Call out anything that moved in the PR body.

### 4. Check whether any new endpoint documents a non-2xx as a *result*

Almost every operation lists a 403 as a boilerplate error, but a few
check-style endpoints (e.g. DigiCert's `privilege-check`) return 403 *as the
answer*, with the body holding the detail the user asked for. Left alone, the
client maps that to `permission_denied` with a hint blaming the caller's own API
role, and the payload only ever appears inside an error string. Add the
operation to `documentedStatusResults` in `generator/parser/parser.go` and
regenerate — the command then renders the body and picks its own exit code.

### 5. Manual testing

Exercise the new and changed endpoints against a live instance on the target
version, not just `--help`:

```bash
bin/jamf-cli -p <profile> pro computers list
bin/jamf-cli -p <profile> pro mobile-devices list
```

### 6. Commit

```bash
git add specs/ internal/commands/pro/generated/ internal/commands/groups.go
git commit -m "feat(pro): ingest the Jamf Pro 11.32 spec"
```

## Version tracking

`specs/.spec-version` contains the Jamf Pro version the specs were synced from.
The Makefile reads it into the binary as `main.specProVersion`, surfaced by
`jamf-cli version`. `make sync-spec` writes it.

## Generated files

The generator creates files in `internal/commands/pro/generated/`:

- One file per API resource (e.g., `computers.go`, `scripts.go`)
- `registry.go` — registers all modern API commands
- `classic_registry.go` — registers all Classic API commands
- `smoke_registry.go` — every GET, for smoke tests
- `backup_registry.go` — list+get pairs consumed by `backup`/`diff`
- `provenance.go` — SHA256 of every source spec

`make verify-generated` deletes and regenerates the package, then fails if the
result differs from what is committed. It compares against `HEAD`, so run it on
a clean tree (or after committing).

## Troubleshooting

### Generator fails

Check that specs are valid YAML:

```bash
go run ./generator --specs ./specs --output ./internal/commands/pro/generated
```

A spec file that fails to load is reported as a `note:` line and skipped. If
`specs/JamfProAPI.yaml` is the file that fails, the generator then has no Jamf
Pro document at all, so fix the ingest rather than the generated code.

### New endpoints not appearing

The generator only creates commands for endpoints with supported HTTP methods
(GET, POST, PUT, DELETE, PATCH) on a path that belongs to the spec file's
canonical prefix family. Paths outside that family are reported as
`Warning: ... not in canonical prefix family — skipped`. Check the generator
output before assuming the spec is at fault.

### Ungrouped commands

After adding new specs, run `make test`. `TestApplyProGroups_AllCommandsGrouped` will fail and list every command that needs a group assignment in `groups.go`.
