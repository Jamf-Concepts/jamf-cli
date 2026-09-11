---
# Where to Make Changes

| I want to... | Edit this file |
|---|---|
| Surface required privileges (annotation, catalog, 403 hint) | `generator/parser/generator.go` (`opAnnotations`) for the annotation; `internal/commands/root.go` for the `privileges` catalog field; `internal/commands/privilege_error.go` for the 403 hint |
| Change what the `commands` catalog says a gateway endpoint needs | `gatewayPrivilegesOf` / `gatewayPermissionsOf` (`internal/commands/gateway_coverage.go`); the annotation is stamped by `generator/parser/generator.go` (`opAnnotations`, plus `applyGatewayAnn` for the synthesized `apply`) and `generator/classic/generator.go` (`gatewayPrivAnn`, per method) |
| Change what a 403 says a permission is *called* (section + picker name) | `internal/privileges/catalogue.go` — a hand transcription of Jamf's permissions-map article, guarded by `TestCatalogueMatchesThePublishedMap` against the committed copy in `internal/privileges/permissions-map.md` (`make sync-permissions-map`) and by `TestCatalogueCoversEveryScopeThisCLISends`. Rendering is `internal/privileges/privileges.go` |
| Change the 403 hint for a Pro/Classic request **through the gateway** | `forbiddenHint` (`internal/client/client.go`), which reads `gateway.Scopes(method, path)` — the request knows the endpoint, the command annotation does not |
| Change what a platform 403 says when the *scope level* is wrong rather than the grants | `scopeMismatchHint` / `hasGatewayErrorCode` (`internal/commands/privilege_error.go`), branched on the gateway's `OWNERSHIP_FORBIDDEN` code; the declared levels it names come from `scopesOf` (`internal/commands/platform_scope.go`) |
| Change what is redacted from a `-vvv` body log | `RedactCredentialBody` (`internal/client/client.go`) — JSON, form-encoded and Classic XML; `redactBodyForLog` composes it with `RedactTokenBody` and every body log goes through it |
| Change behavior of all modern API commands | `generator/parser/generator.go` (`resourceTemplate`) |
| Change behavior of all classic API commands | `generator/classic/generator.go` (`classicResourceTemplate`) |
| Change how OpenAPI specs are parsed | `generator/parser/parser.go` |
| Change singleton detection logic | `generator/parser/parser.go` → `detectSingleton()` |
| Mark a GET-only, no-`{id}` resource as a singleton (so it gets `get`, not `list`) | `generator/parser/parser.go` → `readOnlySingletonPaths` map |
| Let an endpoint's documented non-2xx status render instead of becoming an exit-code error | `generator/parser/parser.go` → `documentedStatusResults` map; plumbing is `registry.WithAllowedStatuses` |
| Change multi-family spec splitting | `generator/parser/parser.go` → `splitByPathFamilies()` |
| Add/change alternate lookup fields (--serial, --udid) — modern API | `generator/parser/parser.go` → `resourceLookupFields` map |
| Add a CLI flag alias for a classic lookup (e.g. `--serial` → `--serialnumber`) | `generator/classic/generator.go` → `lookupFlagAliases` map |
| Fix a resource name auto-pluralization issue | `generator/parser/parser.go` → `resourceNameOverrides` map |
| Change which API version a multi-file resource family ships | nothing — `DeduplicateVersioned` ranks by the version each resource *serves* (`resourceAPIVersion`), not by its name suffix. Check `resourceGetDetailPathOverrides` and `internal/commands/pro_device_actions.go` for hand-pinned versions of the same resource |
| Fix wrong RSQL filter field for --name lookup | `generator/parser/parser.go` → `resourceNameFieldOverrides` map |
| Fix wrong ID field extracted from list response | `generator/parser/parser.go` → `resourceIDFieldOverrides` map |
| Change how classic YAML manifest is parsed | `generator/classic/parser.go` |
| Add a new resource to the classic API | `specs/classic/resources.yaml` |
| Refresh or re-route the App Installer specs | nothing by hand — `make sync-platform-specs-from-sdk` derives `specs/AppInstaller*.yaml` from the SDK's `pro_api.json`. Routes are `AppInstallerSpecs` / `AppInstallerSubtree` (`generator/monolith/overrides.go`); the walker is `generator/monolith/subtree.go` |
| Change what a classic `--scaffold` prints, or its required/enum help | `generator/parser/scaffold_xml.go` (`ScaffoldXML`) for the XML; `generator/classic/schema.go` and `generator/classic/body_render.go` for the required/optional/enum tail |
| Change how a classic resource is bound to a request-body schema | `generator/classicschema/extract.go`; refresh with `make sync-gateway-coverage-from-sdk` |
| Change classic `--set` behaviour (grammar, coercion, refusals) | `generator/classic/generator.go` → `classicRegistryTemplate` (`buildClassicXMLFromSet`, `classicSetParentKind`, `classicSetValue`) |
| Mark a classic body field as a credential `--set` must refuse | `credentialFieldNames` (`generator/classic/schema.go`) for a leaf name; `credentialFieldPaths` in the same file for one whose leaf name is too generic to substring-match (`institutional_recovery_key.key`, `.data`) |
| Add server-side subset narrowing (`--subset`) to a classic `get` | `subsets:` list in `specs/classic/resources.yaml` (drives completion; non-id lookups auto-resolve to an id first for gateway compatibility) |
| Add/modify DDM component scaffolds | `internal/blueprintcomponents/scaffolds.go` — SDK-typed components via `example*()` funcs; raw JSON fallback in `rawScaffolds` for components not yet in SDK |
| Add a new legacy-to-DDM payload converter | `internal/profileconvert/ddm_<name>.go` (new converter + register in `ddm_converter.go` init) |
| Add/remove a resource in the `backup`/`diff` commands | `internal/commands/pro_resources.go` (curated allowlist; endpoints come from generated `backup_registry.go`). `TestBackupResourcePathsAreServed` fails if a key's list/get/scope path is refused by the gateway |
| Add a new Jamf Pro handwritten command | `internal/commands/pro_*.go` (new file + wire in `pro.go`) |
| Add a new Platform API endpoint (CRUD, actions, reports) | Drop/update spec in `specs/.platform-source/`, run `make sync-platform-specs && make generate`. Don't hand-write — generator owns CRUD/actions. |
| Add a new Platform business operation (apply, import-profile, clone, etc.) | Hand-write in the relevant `pro_<resource>.go`; CRUD primitives must come from `internal/commands/platform/generated/` |
| Add or change a generated Platform API resource | `make sync-platform-specs-from-sdk` — the SDK's `api/` is the canonical source, fetched from its `main` by default (`JAMFPLATFORM_SDK_REF` for a tag or SHA, `JAMFPLATFORM_SDK_PATH` for a local checkout) |
| Change behavior of all generated Platform commands | `generator/platform/template.go` (`resourceTemplate`) |
| Change Platform spec parsing (tenant prefix, tag grouping) | `generator/parser/platform.go` |
| Change Platform name-to-ID resolution | `internal/platform/resolve.go` (hand-written, typed); `internal/platform/resolve_generic.go` (generated, untyped) |
| Add a new Jamf Protect command | `internal/commands/protect_*.go` (new file + wire in `protect.go`) |
| Add/remove a resource in `protect backup`/`restore` | `internal/commands/protect_backup.go` → `protectResources()` (Export/Restore closures + `Order`) |
| Mark an object as a tenant default that `protect restore` skips | `internal/commands/protect_backup.go` → `protectDefaultObjects` |
| Change Protect name-to-ID resolution | `internal/protect/resolve.go` |
| Change Protect YAML import/export schemas | `internal/commands/protect_analytics.go`, `protect_ulf.go` |
| Add a new Jamf Security Cloud endpoint (Risk, Device Lifecycle, SSE) | Drop the spec into `specs/.security-source/`, run `make sync-security-specs && make generate`. Don't hand-write — `generator/parser/security.go`'s `securityOpsByFile` map owns every known operation. |
| Add a Jamf Security Cloud hand-written command (business logic, not a single HTTP call — currently only `setup`) | `internal/commands/security_*.go` (new file + wire in `security.go`) |
| Change behavior of all generated Security Cloud commands | `generator/security/template.go` (`resourceTemplate`) |
| Change how Security Cloud specs are parsed / which operations map to which resource | `generator/parser/security.go` (`securityOpsByFile`) |
| Add a gateway-served Security Cloud endpoint (DNS, ZTNA, categories, device groups, UEM Connect) | Publish the spec from `jamfplatform-go-sdk`'s `api/`, drop it in `specs/.platform-source/`, `make sync-platform-specs`, then wire the constructor in `internal/commands/security.go` |
| Rename a generated platform resource (collision or a bare-noun tag) | `generator/parser/platform.go` → `platformResourceNameOverrides` (keys tried most-specific first: `{namespace}/{name}`, then `{service}/{name}`, then a bare `{name}`) |
| Add a Jamf AI Governance command | Generated — the spec is `specs/platform/ai_governance_policies_api.json`; commands are wired under `platform`, not `pro` (`internal/commands/platform.go`) |
| Add a Jamf Account (licensing/partners/SSO) command | Generated from `specs/platform/account_*.json`; wired under `platform` by `newAccountCmds` (`internal/commands/platform_account.go`), which also applies the US-only guard and help |
| Add a platform audit command | Generated from `specs/platform/audit_api.json`; wired by `newPlatformAuditCmd` (`internal/commands/platform_audit.go`), which applies the environment-scope note |
| Change what a platform command says about an opaque gateway error | `annotateDistributorScopeError` (`internal/commands/platform_account.go`) — the platform analogue of `edgeBlockedNote` |
| Record a wire probe that the gateway does not route a Pro/Classic endpoint | `generator/gateway/verdict.go` → `probedUnserved` — **needs a corroborated probe**; one 403 is not one |
| Keep a command available despite being absent from the gateway's published API | `generator/gateway/verdict.go` → `forceServed` — asserts the published surface is *wrong*, not merely ahead of the wire |
| Refresh which Pro/Classic endpoints the gateway publishes | `make sync-platform-specs-from-sdk` (does it as part of the platform sync), or `make sync-gateway-coverage-from-sdk` alone — writes `specs/gateway/coverage.json` |
| Change how a gateway-coverage refusal or hint is worded | `internal/gateway/note.go` (`Note`, `Refusal`); the `--help` caveat is `gatewayCoverageHelp` and the group-level one `gatewayGroupCoverageHelp` (`internal/commands/gateway_coverage.go`) |
| Name a working replacement for a refused command | `successors` (`internal/gateway/note.go`) — a curated table read by the runtime refusal, the `--help` caveat and the `commands -o json` catalog's `gatewaySuccessor`, so the three cannot disagree. `TestGatewaySuccessorsNameCommandsTheBinaryShips` fails when a key or its replacement stops naming a shipped command |
| Change the exit code a policy refusal returns, or the opt-out | `exitcode.Unsupported` (8) and `JAMF_CLI_ALLOW_UNPUBLISHED` (`internal/commands/gateway_coverage.go`) |
| Change when a command is refused for the wrong credentials (either direction) | `checkAPIMatch` (`internal/commands/gateway_coverage.go`), called from `PersistentPreRunE` |
| Rename a generated platform operation whose path-derived verb reads badly | `generator/parser/platform.go` → `platformOperationNameOverrides` (keyed `{METHOD} {full path}`) |
| Stop enforcing a query param the spec marks required but the server ignores | `generator/platform/emitter.go` → `platformIgnoredRequiredParams` |
| Withhold a platform operation the spec declares but the gateway does not route | `generator/parser/platform.go` → `platformUnroutedOps` (keyed `{METHOD} {normalised path}`; needs a recorded probe **and** a working operation it would displace — the table is empty) |
| Change what a command says about the scope level its API needs | `internal/commands/platform_scope.go` — `AnnotateScopeLevelError` for the runtime note, `printScopeSummary` for `platform setup`'s closing summary, both reading the `jamf:scopes` annotation stamped by `generator/platform/emitter.go` (`opAnnotations`) from `x-scope-types` |
| Let a platform endpoint's documented non-2xx render instead of becoming an exit-code error | `generator/platform/emitter.go` → `platformDocumentedStatusResults`; runtime is `internal/platform/documented_status.go` |
| Suppress a platform `--name` flag that cannot work | `generator/platform/emitter.go` → `platformNoNameLookup` (keyed `{METHOD} {path}`) |
| Point a platform `--name` lookup at a property other than name/title/displayName | `generator/platform/emitter.go` → `platformNameLookupFields` (keyed `{namespace}/{name}`); runtime is `platform.ResolveIDByNameInField` |
| Change which columns a platform list renders in table/CSV output | `generator/platform/emitter.go` → `platformTableColumns` (keyed `{namespace}/{name}` via `namespaceFromPath` — the **whole** namespace) |
| Correct a platform path version or success status the spec gets wrong | the SDK's `tools/generate/config.json`, republished as `x-jamf-tenant-path-version` / `x-jamf-expected-status` — not in this repo |
| Change Security Cloud auth (token cache, per-scope credentials, error mapping) | `internal/security/client.go` |
| Add a new cross-product command | `internal/commands/` (new file + wire in `root.go`) |
| Add a new product namespace | `internal/commands/` (e.g., `newproduct.go` + `newproduct_*.go` files); then update site (`index.html`, `style.css`, `catalog.js`) — `make verify-site` enforces |
| Modify auth behavior | `internal/auth/` |
| Change HTTP client / retry / exit codes | `internal/client/` |
| Add or change output formats | `internal/output/` |
| Add a short alias (e.g., `comp` for `computers`) | `internal/commands/aliases.go` |
| Add a command group for `--help` output | `internal/commands/groups.go` |
| Change global flags or root command behavior | `internal/commands/root.go` |
| Change config file handling | `internal/config/` |
| Change shared CLI interfaces (CLIContext, etc.) | `internal/registry/` |
| Modify the GitHub Pages showcase site | `docs/site/index.html`, `docs/site/style.css`, `docs/site/catalog.js`, `docs/site/palette.js`, `docs/site/terminal.js` |
| Change how commands.json is generated for the site | `generator/site/main.go` |
