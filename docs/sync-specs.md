# Syncing OpenAPI Specs

This document describes how to update the CLI when a new Jamf Pro Server version is released.

There are two independent sources for the Jamf Pro API specs, and two matching
sync routes:

| Source | Route | When to use |
|---|---|---|
| `jamf/jss` monorepo (`**/swagger_docs/uapi/*.yaml`) | `make sync-specs` | You have a server repo checkout at the release tag |
| A live instance's consolidated `/api/schema/` document | `make sync-spec` | You only have an instance running the target version |

Both routes end in `make generate` and both write the version to
`specs/.spec-version`.

## Prerequisites

- Go toolchain matching `go.mod`
- For `sync-specs`: a `jamf/jss` checkout at the release tag
- For `sync-spec`: credentials for an instance on the target version

## Option A: from the jamf/jss monorepo

### Automated (GitHub Actions)

```bash
gh workflow run sync-specs.yaml \
  -f jamf_pro_version=11.31.0 \
  -f server_repo_ref=11.31.0-t1785774933693
```

### Manual (local)

```bash
mkdir -p /tmp/jss-sparse && cd /tmp/jss-sparse
git clone --no-checkout --depth 1 --filter=blob:none \
  --branch <tag> https://github.com/jamf/jss.git jamf-pro-server
cd jamf-pro-server
git sparse-checkout set --no-cone '**/swagger_docs/uapi/*.yaml'
git checkout
```

Specs are scattered across subdirectories in the jss repo (not a single folder). The sparse-checkout pattern `**/swagger_docs/uapi/*.yaml` collects them all.

Then, from this repo:

```bash
make sync-specs JAMF_SERVER_PATH=/tmp/jss-sparse JAMF_PRO_VERSION=11.31.0
```

`JAMF_SERVER_PATH` is the directory *containing* the checkout — the target reads
specs from `$JAMF_SERVER_PATH/jamf-pro-server/**/swagger_docs/uapi/`, which is
why the clone above lands in a `jamf-pro-server` subdirectory.

`JAMF_PRO_VERSION` is mandatory — it is what `specs/.spec-version` (and therefore
`jamf-cli version`) reports.

## Option B: from a live instance's consolidated schema

The public `/api/schema/` endpoint serves one consolidated OpenAPI document.
`make sync-spec` splits it back into the per-resource layout under `specs/`.

```bash
# 1. Fetch (needs auth)
TOKEN=$(bin/jamf-cli -p <profile> pro auth token --field token --quiet)
curl -s -H "Authorization: Bearer $TOKEN" \
  https://<instance>/api/schema/ -o /tmp/monolith.json

# 2. Confirm the version you are ingesting
curl -s -H "Authorization: Bearer $TOKEN" \
  https://<instance>/api/v1/jamf-pro-version

# 3. Split and regenerate
make sync-spec JAMF_MONOLITH_SPEC=/tmp/monolith.json JAMF_PRO_VERSION=11.31.0
```

`JAMF_MONOLITH_SPEC` also accepts an `http(s)://` URL directly.

Splitter behaviour and its knobs live in `generator/monolith/`:

- **Routing** — each path goes to the spec file that already owns it. Genuinely
  new paths fall through to `TagFilenameOverrides[tag]` → the file that already
  owns other paths with the same tag → `PascalSingular(tag).yaml`. Every
  fall-through is reported as a `Warning:` line, so read the generator output.
- **`DroppedTags`** — tags never emitted (legacy preview endpoints that shadow a
  canonical resource).
- **`PreservedSpecs`** — spec files maintained outside the public monolith
  (private endpoints, e.g. the App Installer specs). The splitter leaves these
  files alone and treats their paths as invisible, so the monolith cannot
  clobber them. Library files they `$ref` are auto-preserved.
- **Components** — a schema used by one spec file is inlined into it; a schema
  shared by two or more is emitted to `specs/_MonolithLibrary.yaml` and
  referenced by external `$ref`.

The public monolith is a subset of the monorepo specs, so Option B legitimately
produces fewer paths than Option A. Do not "fix" a missing private endpoint by
hand-editing a generated spec — add it to `PreservedSpecs` instead.

## After either route

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

Resource names come from the spec filename and are auto-pluralized. When that
reads wrong — a collective noun or a double `s` — add an entry to
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
git commit -m "feat: sync specs with Jamf Pro v11.31.0"
```

## Version tracking

`specs/.spec-version` contains the Jamf Pro version the specs were synced from.
The Makefile reads it into the binary as `main.specProVersion`, surfaced by
`jamf-cli version`. Both sync routes and the GitHub Action write it.

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

Some specs fail to parse due to missing `$ref` targets or schema issues — these are skipped with an error message. This is expected for specs that reference shared definition libraries not included in the uapi directory.

### New endpoints not appearing

The generator only creates commands for endpoints with supported HTTP methods
(GET, POST, PUT, DELETE, PATCH) on a path that belongs to the spec file's
canonical prefix family. Paths outside that family are reported as
`Warning: ... not in canonical prefix family — skipped`. Check the generator
output before assuming the spec is at fault.

### Ungrouped commands

After adding new specs, run `make test`. `TestApplyProGroups_AllCommandsGrouped` will fail and list every command that needs a group assignment in `groups.go`.
