---
# Gateway Coverage

## The Published-Surface Contract

The gateway's published Pro/Classic API is the contract. `specs/gateway/coverage.json` is the committed manifest, derived from **jamfplatform-go-sdk's published `api/`**:

| spec | what it is |
|---|---|
| `pro_api.json` | Jamf Pro API as published on the gateway (476 paths, 700 ops, 11.31.0) |
| `classic_api_resource_documentation.json` | Classic API, likewise (270 paths, 589 ops, 11.28.0) |

Both are in `PLATFORM_SDK_COVERAGE_SPECS` and must **never** join `PLATFORM_SDK_SPECS` — they describe Jamf Pro APIs this repo already generates from `specs/*.yaml`, so handing them to the platform generator emits a second set of Pro commands built from gateway paths.

A command outside the published surface is **refused on a gateway profile before a request is sent**, because it may still work today but is expected to stop — and arrives as a bare `403 BAD_PERMISSIONS` with no hint. The escape hatch is `JAMF_CLI_ALLOW_UNPUBLISHED`.

## Verdicts

`generator/gateway` reads the manifest, stamps every Pro and Classic operation with a `jamf:gateway` annotation, and emits the runtime table `internal/gateway/coverage_gen.go`. **One level, `unserved`, and every entry refuses.** What varies is `jamf:gateway-basis`, which selects the wording:

- **`probe`** — a recorded, corroborated wire probe found it unrouted. `probedUnserved` in `generator/gateway/verdict.go`. Needs a corroborated probe — one 403 is not one. Zero entries as of the v0.20.1 ingest.
- **`unpublished`** — absent from the published spec. Message says the endpoint may still answer today, that this is transitional, and that it is refused now rather than later. 37 Pro operations plus Classic `classic-computer-configs`.

**`forceServed`** is the escape hatch asserting the published surface is *wrong*, not merely ahead of the wire. Its bar is not "the wire says this still works" — that is the transitional state, not a counter-example. Currently empty.

## Classic Three-Granularity Verdicts

Classic paths are assembled at runtime (resource + lookup), so there is no fixed set of op paths to enumerate. `classicGatewayOps` emits all three per resource:

- **`VerdictSubtree`** (`* <resource>/**`) — does the gateway carry the resource at all. Exact-path was wrong for five resources with no bare collection endpoint (`computerhistory`, `computerapplications`, etc.).
- **`VerdictSubtreeMethod`** (`GET <resource>/**`) — the method a subcommand sends is fixed at generate time even though its path is not.
- **`Verdict`** (`GET <resource>`) — `list`'s path is the one Classic path that *is* fixed.

A withdrawal inside a surviving subtree (`patchpolicies` lost `GET /patchpolicies` and kept `GET /patchpolicies/id/{}`) means the subtree-wide verdict alone is insufficient.

## 403 Vocabulary Split

A Jamf Pro instance enforces API-role privileges spelled as prose (`Read Categories`). The gateway enforces GA capability permissions (`categories:read`). Printing the wrong one sends the operator to a console where the grant it names does not exist.

Four call sites:

- **`forbiddenHint`** (`internal/client/client.go`) — answers a Pro or Classic 403 from the capability scopes of the request it actually sent, via `gateway.Scopes(method, path)`. Lives here rather than on the command because a fan-out command fans out over dozens of endpoints with one annotation.
- **`EnrichPrivilegeError`** (`internal/commands/privilege_error.go`) — answers a Platform command's 403 from its own `jamf:privileges` annotation (already the capability vocabulary). Also remaps 403 → exit code 5.
- **`hasGatewayErrorCode`** (same file) — splits the platform 403: `OWNERSHIP_FORBIDDEN` means the scope header does not match the level the credential was minted at (fix: profile, not permissions). `scopeMismatchHint` handles this; it names no *correct* level, since a gateway token is opaque.
- **`forbiddenHint`** suppresses Jamf Pro names when a platform hint already carries an answer, detected by `privileges.Marker`.

The `commands -o json` catalog carries the requirement in both vocabularies: `privileges` (Jamf Pro API-role names), `gatewayPrivileges` (capability slugs the gateway requires), `gatewayPermissions` (rendered as Jamf Account picker words). The human form matters because the picker lists names, not slugs.

`internal/privileges/catalogue.go` turns a slug into the section and permission name Jamf Account's picker shows. It is a hand transcription of Jamf's permissions-map article, checked against the committed copy `internal/privileges/permissions-map.md` by `TestCatalogueMatchesThePublishedMap` and against `TestCatalogueCoversEveryScopeThisCLISends`. Refresh: `make sync-permissions-map`.

## Exit Codes

A gateway refusal exits **8** (`exitcode.Unsupported`), not 2. Exit 2 is cobra flag errors, missing required flags, wrong arg counts, unknown subcommands, missing URL, missing credentials, retired host, scope conflict. A pipeline can distinguish "refused by policy" from "invoked wrong".

Both directions of `checkAPIMatch` return exit 8: a Pro/Classic command on a gateway profile and a Platform-only command on an instance profile.

## Escape Hatch

`JAMF_CLI_ALLOW_UNPUBLISHED` — value-parsed with `strconv.ParseBool`. Downgrades the refusal to a stderr warning per invocation. **Honoured only for `BasisUnpublished`** — a probed-unrouted endpoint has no route to reach. Neither `--quiet` nor `--no-hints` silences the warning.

## `checkAPIMatch`

Called from `PersistentPreRunE`. Refuses:
- A Pro/Classic command on a gateway profile (annotation `jamf:api` + `isGatewayProvider` type assertion)
- A Platform-only command on an instance profile (annotation `jamf:api: platform-gateway` against a non-platform provider)

Keys on the resolved auth method, not on a profile — so env-var credentials behave identically to profiles. Error names `credentialSource` (the env vars, a `--token-file`, or the profile name), not `resolvedProfile`.

## Response-Side: `gatewayUnservedNote`

`internal/client/client.go`. Appended (not substituted) to a 403/404 response, gated on the body carrying `BAD_PERMISSIONS` or `404 page not found`. Needed for fan-out commands (`pro overview` makes ~41 calls) that carry one annotation for the whole batch — only the request knows which endpoint was refused.

## Successors Table

`successors` in `internal/gateway/note.go` — a curated table read by the runtime refusal, `gatewayCoverageHelp` in `--help`, and the `commands -o json` catalog's `gatewaySuccessor` field. The three cannot answer differently. `TestGatewaySuccessorsNameCommandsTheBinaryShips` fails when a key or its replacement stops naming a shipped command, or when nothing under the key is refused any more.

`gatewayGroupCoverageHelp` propagates the caveat onto a parent command whose every runnable leaf is refused.

## Gateway Scopes per Operation

The manifest stores per-method gateway scopes from `x-required-privileges` (`categories:read`, `device-actions:execute`, …). `Coverage.Verdict` returns them, emitted into the runtime table as `scopeRules`, and answered by `gateway.Scopes(method, path)`.

An operation with no scope is not an absent operation — 44 Jamf Pro endpoints are unauthenticated (`/v1/health-check`, `/v1/jamf-pro-version`, `/v1/locales`) and declare none.

The whole manifest is emitted, not just operations generated commands send. The consumer is a concrete failing request, and hand-written commands assemble paths the generator never enumerates.

## Manifest Provenance

`make verify-gateway-coverage` is the CI guard. A **missing manifest** fails the guards rather than skipping them — `specs/gateway/coverage.json` is a required committed artifact. `make sync-platform-specs-from-sdk` refreshes it (does it as part of platform sync); `make sync-gateway-coverage-from-sdk` does coverage alone.

The manifest records the SDK revision it came from — resolved at fetch time and passed down as `JAMFPLATFORM_SDK_REV`, or read off a checkout's `HEAD` when one was given. `gateway.CarryForwardProvenance` keeps a recorded revision a later run was not told.

## What a 403 Cannot Establish

An unrouted path and an under-privileged one are byte-identical. A bare Tyk `404 page not found` distinguishes an unknown namespace and nothing finer. One 403 is not a probe; a `probedUnserved` entry needs the same credential reaching the rest of the namespace in the same run, across regions.

## `apply` Carries Its Own Gateway Verdict

`applyGatewayVerdict` reads the **first refused operation in send order** (`list`, then `create` or `update` — `applyGatewayOpNames`). A refused `apply` emits **no** gateway privileges, since a resource whose `list` is withdrawn can still declare scopes on the `create` it kept, and a refused command must not advertise a grant that cannot make it work.

A refused Classic command also names no permission (`gatewayPrivAnn` returns nothing), for the same reason.

## Hand-Written Commands and Gateway Marking

`markGatewayCoverage` (`internal/commands/pro_device_actions.go`) stamps `jamf:api`/`jamf:gateway`/`-basis`/`-detail` for a method and path the command assembles itself, so `checkAPIMatch` refuses it pre-flight and `--help` carries the caveat. Used because `POST /v2/mdm/commands` is unpublished. Derived from the runtime table so the refusal disappears automatically if the gateway publishes the method.

Do **not** reach for it on a fan-out command where only one of many endpoints is refused — the response-side `gatewayUnservedNote` already covers that case.
