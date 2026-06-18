# Code Generator

Generates Go command files from OpenAPI specs (modern API) and a YAML manifest (classic API).

## How It Works

```
specs/*.yaml ──────────────► generator/parser/   ──► internal/commands/pro/generated/*.go
                               ParseSpec()            + registry.go
                               Generator.Generate()

specs/classic/resources.yaml ► generator/classic/ ──► internal/commands/pro/generated/classic_*.go
                               ParseManifest()        + classic_registry.go
                               Generator.Generate()
```

`generator/main.go` is the entrypoint — it runs both generators.

## Spec → Resource mapping

Most spec files map to a single resource. Two special cases apply before code generation:

### Singleton detection

A resource is a **singleton** if no path in the spec contains a `{param}` path parameter **and** there is a non-paginated GET and a PUT sharing the same path. This identifies settings-style objects (single instance, no collection).

Singletons differ from collection resources in three ways:
- The `list` operation is renamed to `get` (no ID argument needed)
- The CLI name is not pluralised (e.g. `cache`, not `caches`)
- `apply` and `delete-by-name` are not generated

Examples: `cache`, `client-check-in`, `jamf-protect`, `self-service-settings`.

### Multi-family spec splitting

Some spec files contain multiple independent CRUD families sharing a parent path prefix (e.g. `/v1/self-service/branding/macos` and `/v1/self-service/branding/ios`, each with their own `/{id}` child paths). `ParseSpec` detects this pattern and returns one `Resource` per family rather than collapsing them.

`ParseSpec` returns `[]*Resource` (not `*Resource`) to support this case.

Examples: `SelfServiceBranding.yaml` → `self-service-branding-macos` + `self-service-branding-ios`.

### Filename safety

Generated filenames are sanitised by `safeFilename()` to avoid Go build constraints. Files ending in `_<GOOS>.go` or `_<GOARCH>.go` are silently excluded from builds on other platforms — `self_service_branding_ios.go` would never compile on macOS. Such files get a `_resource` suffix (e.g. `self_service_branding_ios_resource.go`).

## Templates

Templates are **Go `const` strings** embedded in the generator source — NOT separate `.tmpl` files.

| Template | Location | Generates |
|----------|----------|-----------|
| `resourceTemplate` | `parser/generator.go` | Per-resource command file (modern API) |
| `registryTemplate` | `parser/generator.go` | `registry.go` (wires all modern commands) |
| `classicResourceTemplate` | `classic/generator.go` | Per-resource command file (classic API) |
| `classicRegistryTemplate` | `classic/generator.go` | `classic_registry.go` (wires all classic commands) |

## Template FuncMap (Modern)

Functions available in `resourceTemplate`:

| Function | Purpose |
|----------|---------|
| `toCamel`, `toLowerCamel`, `toSnake`, `toKebab`, `toScreamingSnake` | Case conversion (via `strcase`) |
| `hasPathParam(path)` | True if path contains `{param}` |
| `pathParams(params)`, `queryParams(params)` | Filter parameters by location |
| `goType(type)`, `flagType(type)` | Map OpenAPI types to Go types / cobra flag methods |
| `sortOps(ops)` | Canonical order: list, get, create, update, delete, ... |
| `dedupeOps(ops)` | Remove duplicate operations by name |
| `escapeQuotes(s)` | Escape for Go string literals |
| `hasPostOrPut`, `hasDelete`, `hasDestructive`, `hasList` | Conditional import/feature guards |
| `shouldGenerateApply(r)` | True if resource should have an `apply` command (has create+update AND is not a singleton) |
| `createPath(ops)`, `updatePath(ops)`, `updatePathParam(ops)` | Extract paths from create/update operations for apply |
| `needsFmt(r)` | Whether generated code needs `fmt` import |
| `needsURL(r)` | Whether generated code needs `net/url` import |
| `hasScaffold`, `opHasScaffold`, `scaffoldJSON`, `opScaffoldJSON` | JSON scaffold template generation |
| `exampleText(resource, singular, op)` | CLI example text per operation type (singleton-aware) |
| `isDestructive(op)` | Check destructive flag |
| `defaultVal(type, val)` | Format default values for flag definitions |

## Key Types

### Modern API (`parser/types.go`)

- **`Resource`** — Top-level: `Name`, `NameSingular`, `GoName`, `Description`, `Operations`, `Schemas`, `IsSingleton`
- **`Operation`** — Endpoint: `Name` (list/get/create/update/delete), `Method`, `Path`, `Parameters`, `RequestBody`, `IsList`, `IsPaginated`, `IsDestructive`. `IsList` is true for list/history ops and drives list-only semantics (default sections, output array key, singleton detection); `IsPaginated` is broader (any GET exposing `page`/`page-size`) and gates `--all`/`--limit` auto-pagination, so report/action GETs like `patch-report` paginate too.
- **`Parameter`** — Query/path param: `Name`, `In`, `Type`, `Required`, `Default`, `IsArray`
- **`Schema`** / **`Property`** — Used for `--scaffold` JSON template generation

`IsSingleton` is set by `detectSingleton()` in `parser.go` and controls CLI name pluralisation, operation naming, and whether `apply`/`delete-by-name` are generated.

### Classic API (`classic/types.go`)

- **`ClassicResource`** — `Name`, `Path`, `CLIName`, `GoName`, `Singular`, `Description`, `Operations` (string slice), `Lookups`, `HasScope`

## Testing

```bash
# Test the generators
go test ./generator/parser/... ./generator/classic/...

# Full cycle: regenerate + test everything
make generate && make test
```

## How Registry Files Work

`registry.go` defines the `HTTPClient` and `OutputFormatter` interfaces that all generated commands depend on, plus `RegisterCommands()` which wires every modern resource into cobra. It also contains shared apply helpers: `readApplyInput`, `extractJSONField`, `resolveNameToIDForApply`, and `extractIDString`.

`classic_registry.go` has the parallel `RegisterClassicCommands()` for classic resources, plus classic-specific apply helpers: `extractClassicName` and `resolveClassicNameToIDForApply`.

Both are called from `internal/commands/root.go`.

## Apply (Upsert) Commands

Resources with both `create` and `update` operations automatically get an `apply` subcommand, **unless they are singletons** (singletons have no named collection to search against, so upsert semantics don't apply). Classic API resources also require `name` in their lookups. Apply performs a name-based upsert:

1. Read input from `--from-file` or stdin (JSON for modern, XML for classic)
2. Extract the name/displayName from the input
3. Check if a resource with that name exists
4. Create if not found; replace (with confirmation) if found
5. Handle collisions (multiple resources with the same name) interactively

Flags: `--from-file`, `--yes` (skip confirmation), `--dry-run` (preview)
