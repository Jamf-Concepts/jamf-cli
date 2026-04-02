# Code Generator

Generates Go command files from OpenAPI specs (modern API) and a YAML manifest (classic API).

## How It Works

```
specs/*.yaml ──────────────► generator/parser/   ──► internal/commands/generated/*.go
                               ParseSpec()            + registry.go
                               Generator.Generate()

specs/classic/resources.yaml ► generator/classic/ ──► internal/commands/generated/classic_*.go
                               ParseManifest()        + classic_registry.go
                               Generator.Generate()
```

`generator/main.go` is the entrypoint — it runs both generators.

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
| `hasApply(ops)` | True if resource has both `create` (POST) and `update` (PUT) |
| `createPath(ops)`, `updatePath(ops)`, `updatePathParam(ops)` | Extract paths from create/update operations for apply |
| `needsFmt(ops)` | Whether generated code needs `fmt` import |
| `hasScaffold`, `opHasScaffold`, `scaffoldJSON`, `opScaffoldJSON` | JSON scaffold template generation |
| `exampleText(resource, singular, op)` | CLI example text per operation type |
| `isDestructive(op)` | Check destructive flag |
| `defaultVal(type, val)` | Format default values for flag definitions |

## Key Types

### Modern API (`parser/types.go`)

- **`Resource`** — Top-level: `Name`, `NameSingular`, `GoName`, `Description`, `Operations`, `Schemas`
- **`Operation`** — Endpoint: `Name` (list/get/create/update/delete), `Method`, `Path`, `Parameters`, `RequestBody`, `IsList`, `IsDestructive`
- **`Parameter`** — Query/path param: `Name`, `In`, `Type`, `Required`, `Default`, `IsArray`
- **`Schema`** / **`Property`** — Used for `--scaffold` JSON template generation

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

Resources with both `create` and `update` operations automatically get an `apply` subcommand (classic resources also require `name` in their lookups). Apply performs a name-based upsert:

1. Read input from `--from-file` or stdin (JSON for modern, XML for classic)
2. Extract the name/displayName from the input
3. Check if a resource with that name exists
4. Create if not found; replace (with confirmation) if found
5. Handle collisions (multiple resources with the same name) interactively

Flags: `--from-file`, `--yes` (skip confirmation), `--dry-run` (preview)
