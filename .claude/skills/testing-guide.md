---
# Testing — Key Tests and CI Guards

## CI Verification Targets

```bash
make verify-generated       # Generated code matches specs (safe in CI — no SDK needed)
make verify-gateway-coverage # Coverage manifest and table current (CI-safe)
make verify-classic-schemas  # specs/classic/schemas.json matches SDK's Classic spec (CI-safe)
make verify-site             # Site supports all product namespaces (CI-safe)
```

`make verify-gateway-coverage` uses `git status --porcelain`, not `git diff` — git diff cannot see an untracked file; the first run after adding `specs/gateway/` regenerated the manifest with different provenance and still reported clean.

A **missing manifest** (`specs/gateway/coverage.json`) fails the guards rather than skipping them — it is a required committed artifact. With the file absent, `TestEveryRefusalCarriesItsEvidence`, `TestAlmostEveryRequestResolvesAsServed`, `TestAppInstallersResolveAsServed` and `TestCatalogueCoversEveryScopeThisCLISends` all skipped, and both packages reported `ok`. All three are `t.Fatalf` now, each naming the artifact and the sync target that produces it.

## Smoke Tests

```bash
make smoke       # Full GET sweep — runs -run 'TestSmoke_Tier', NOT -run 'TestSmoke'
make smoke-seed  # Explicitly seed test objects (creates objects on tenant)
```

`make smoke` runs `-run 'TestSmoke_Tier'` — not `-run 'TestSmoke'`, which also matches `TestSmoke_Seed` and **creates objects on the tenant**. Seeding is `make smoke-seed`, explicitly.

`smokeClient` must enable gateway mode — without it, every request goes out instance-shaped and 404s, missing the shape that was just changed.

## Gateway Coverage Tests

**`TestHandWrittenPathsAreServed`** (`internal/commands/gateway_handwritten_paths_test.go`) — greps `internal/commands` and `internal/resolve` for `/vN/` and `/JSSResource/` literals and checks each against the coverage manifest.

Critical distinction: **it reads `specs/gateway/coverage.json`'s `spec` section rather than calling `gateway.Lookup`**. The `unserved` runtime table is the intersection of "the gateway omits it" AND "a generated command sends it", so a withdrawn version no generated command sends any more resolves as *Served* — a `Lookup`-based test would have passed on all 13 stale files.

`unservedHandWrittenPaths` is its allowlist, each entry carrying why no successor exists. A stale entry fails the test. Grep for hard-coded versions whenever a resource's winning version changes — the test covers command files, but cannot see a stale literal in an override table.

**`TestAlmostEveryRequestResolvesAsServed`** — catches a manifest that is present but not matching the live spec state. Its `gatewayOps` helper must replay the generator's consolidation passes, not just `ParseSpec`: parsing alone counts all three `ComputersInventory{,V2,V3}.yaml` resources when only one becomes a command, and a superseded version is what a spec drop withdraws. Only `DeduplicateVersioned` and the two passes that rewrite a path are replayed (not the passes that set names, columns, lookup fields — no verdict reads those).

**`TestEveryRefusalCarriesItsEvidence`** — asserts every refused command has an entry in the coverage table with a recorded basis. Fails on a missing manifest.

**`TestAppInstallersResolveAsServed`** — pins App Installers as the *served* case (the surface `gatewayUnservedNote`'s mechanism was built around). Fails on a missing manifest.

**`TestEveryOverrideStillMatchesACommandThisCLISends`** — fails when an override stops matching any shipped path. Checks that override tables (in generator knobs like `platformResourceNameOverrides`) don't go stale after a resource name changes.

**`TestGatewaySuccessorsNameCommandsTheBinaryShips`** — fails when a key in `successors` (`internal/gateway/note.go`) or its replacement stops naming a shipped command, or when nothing under the key is refused any more. Both ways an entry goes stale.

**`TestGatewayTenantTravelsInHeader`** and **`TestLoadResourcesDropsTheTenantFromEveryPath`** — fail if a tenant segment reappears in generated paths after the header-migration.

**`TestProbedEntriesCarryTheProbeBasis`** — pins the `probe`-basis wording against a test-local entry (since the live `probedUnserved` table is currently empty). Same pattern as `TestPlatformUnroutedOpsIsEmptyOrEvidenced` for the unrouted table.

**`TestApplyCarriesTheSameVerdictAsItsSiblings`** — replays the whole parse → dedupe → `gateway.Apply` pipeline over the live specs and fails if no wholly-refused apply-shipping resource is left to exercise it.

## Classic Gateway Tests

**`TestVerdictSubtreeMethodCatchesAMethodWithdrawnFromAServedResource`** — pins the per-method-across-subtree verdict mechanism. Moved to a synthetic `generator/gateway` entry after v2082 restored the patch family (no live refusal exercises it any more).

**`TestExactVerdictSeparatesAWithdrawnCollectionGetFromASurvivingDetailGet`** — pins the exact-path verdict for a collection GET that was withdrawn while the `{id}` path survived (`patchpolicies` shape).

**`TestApplyEmitsASubtreeMethodEntryUnderThatMethod`** — pins the apply synthesised verb's subtree-method entry.

**`TestARefusedClassicCommandNamesNoPermission`** — a refused command must not advertise a grant that cannot make it work. Covers `classic-computer-configs` (the one Classic resource still refused whole-resource). Fails rather than passing vacuously if every Classic resource becomes served.

**`TestClassicPatchFamilyIsServedAndKeepsItsPermissions`** — fails if a refusal comes back for the eight patch-family commands (restored at v2082). The failure to watch for now: a refusal reinstated by an ingest.

## Positional Contract Tests

Five tests hold the positional contract tree — each covers a surface the others cannot see:

**`TestEveryLeafRefusesAnUndocumentedPositional`** — every leaf against its own `Use`, plus the refusal's wording and hint. Also asserts that no zero-arity leaf's `Args` is `refuseStrayPositionals` by pointer identity — if it is, the `classifyArgsErrors` wrap did not reach it. **`guardStrayPositionals` must run before `classifyArgsErrors` in `NewRootCmd`** — the guard installs validators and the classifier wraps only what it finds; swapping them leaves 731 of 736 refusals unwrapped.

**`TestScaffoldKeepsTheDeclaredPositionalCeiling`** — the validator a `--scaffold` flag swaps in at runtime (not readable with no flag set).

**`TestNoExampleDocumentsAnUndeclaredPositional`** — each leaf's own `Example`. The generator once rendered `delete 1` and `history 1` on 22 singletons whose `Use` takes no id, so `--help` taught a form the guard refused.

**`TestEveryExampleInvocationNamesACommandThatExists`** — every `jamf-cli` on an `Example` line. The previous filter hid the **head** of every pipe; 13 lines on 8 resources opened with `<resource> get` against a resource shipping no `get`.

**`TestNoCommandLiteralReadsAnUndeclaredPositional`** — an AST scan requiring any `cobra.Command` literal that mentions `args` to declare `Args`. Two documented blind spots: it sees **composite literals only** (a `cmd.RunE = func(…)` assigned afterwards is invisible — as in `platform_account.go` and `platform_audit.go`); and it keys on the **spelling** `args` (a closure written `func(cmd *cobra.Command, positional []string)` that reads `positional[0]` passes the scan).

## Output and Formatting Tests

**`TestNoFileBuildsItsOwnOutputFormatter`** — refuses a formatter built outside the three sanctioned functions (`printRows`, `formatterFor`, `writerFor`). It resolves the **import** rather than matching construction syntax, so a file that cannot name `internal/output` cannot trip it in any form.

**`TestRequestBodyFlagIsUniformlyFromFile`** — walks the assembled tree and refuses a `--file` flag that is not on the named upload list (9 multipart uploads + 2 YAML imports = 11 leaves).

**`TestSuggestFlag_RenameIsScopedToCommandsThatHaveTheReplacement`** — asserts the `renamedFlags` entries fire only on commands that actually have the destination flag. No command declares both `--file` and `--from-file`.

## Platform Generator Tests

**`TestPlatformTableColumnKeys`** — fails on any key in `platformTableColumns` that matches no live list op. Was keyed on `serviceFromPath` (first segment only); three-segment namespaces produced wrong keys and emitted lists with no columns silently.

**`TestPlatformOperationNameOverridesWinOverDerivation`** — asserts the resulting *name* rather than absence of an error (absence of an error was the old symptom). Overrides are applied *last*, after all disambiguation passes.

**`TestTwoSpecsSharingATagGetDistinctResourceNames`** — asserts resulting names and paths. Also fails when an override value stops naming a shipped resource (how a key goes stale after a namespace moves).

**`TestSecurityCommandsDeclareTheirAPI`** — fails if a generated Security Cloud command is emitted without both `jamf:api` annotation and the `Short` suffix naming the API.

**`TestSecurityCloudSpecParity`** — pairs each subcommand with a spec endpoint. Must account for synthesised verbs (`apply`); a stale `platformUnroutedOps` entry shows up as 51 subcommands vs 52 declared operations.

**`TestServiceSegment`** — pins both URL shapes (v1807 platform specs with no `/api/` in `servers[0].url`, and Security Cloud specs still declaring it). Both must coexist in one drop.

**`TestDropUnroutedPlatformOps`** — pins the mechanism against a test-local entry while `platformUnroutedOps` is empty.

**`TestPlatformUnroutedOpsIsEmptyOrEvidenced`** — fails on any addition to `platformUnroutedOps`, so the next drop is a deliberate edit to this test rather than a quiet table append.

**`TestLivePagingFlagsAreNotSilentlyDropped`** — walks live specs to ensure `--page-size` (and `--page`) are not filtered out for cursor-paged and non-auto-paginating ops. Catches the bug where `buildQueryParams` filtered `page`/`page-size` unconditionally. Fails if no op is left to cover.

**`TestDeduplicateVersioned_BaseWinsWhenItServesTheHigherVersion`** — pins the computers-inventory shape (base file declares v1 AND v4 together, so base file wins). `..._BaseStillLosesWhenItIsOlder` pins the inventory-preload shape. Both must hold — the fix must not flip the case the old rule was written for.

**`TestBuildEnumChoices_ReachesAllOfComposedUnionVariants`** — pins the `allOf`-composed-union enum extraction end-to-end for account specs. Without the fix, SSO connection type and setting enums were invisible in `--help`.

## Auth and Credentials Tests

**`TestVerifyOAuth2Credentials_IgnoresTheTokenCache`** — fails if `verifyProfileCredentials` is ever moved onto `GetToken`. The token cache is keyed on `(baseURL, clientID)` not the secret, so a cached token from a working pair would report a mistyped secret as verified — the one thing verification exists to catch.

**`TestCredentialSourceNamesTheEnvironmentNotTheProfile`** — asserts that when env-var credentials are in use, error messages name the env vars as the source, not the `resolvedProfile` (which stays whatever `-p` or `default-profile` names).

**`TestResolveAuthScopePrecedence`** — covers the `profileScopeAppliesTo` withheld-scope rule. Previously failed when it read a mutable package var instead of `params.ClientID`, making the result depend on global state.

**`TestMessagesReadCorrectlyForAPluralCredentialSource`** — pins that the credential-source phrase reads grammatically correct for env-var credentials (plural) vs a profile name (singular).

## Classic Schema Tests

**`TestNoBoundResourceCarriesASemanticSizeField`** — fails if an ingest ever binds the one resource where `hardware.storage[].device.size` is a capacity in MB (not a server-computed counter). `parser.ClassicIsCountElement` discriminates by repeated sibling.

**`TestEverySecretBearingFieldIsRefusedForSet`** — sweeps `specs/classic/schemas.json` for any string-typed field whose name or path names a key, token or secret, and requires each to be refused by `--set`. Four named exemptions, each with a reason. A stale exemption fails; a vacuous walk also fails.

## Backup and Resource Tests

**`TestBackupResourcePathsAreServed`** — fails if a key in `pro_resources.go`'s curated allowlist has a list/get/scope path refused by the gateway.

## Site and Group Tests

**`TestApplyProGroups_AllCommandsGrouped`** — trips when a new tag surfaces as a new resource command after a spec ingest. Wire into the correct `proGroupMap` entry in `groups.go`.

**`TestCommandsCatalogIncludesANestedCommandNamedCommands`** — asserts a generated operation named `commands` (nested under a resource) appears in `commands -o json`. Previously `collectCommands` skipped every command named `commands` at any depth rather than only the root's catalog command.

**`TestChainSkip_RootOnlyNamesDoNotSkipNestedCommands`** — asserts the *auth-resolution* error (not absence of an error) for `platform ai-policies version`, which previously bypassed auth via `chainSkip`. Also pins the reverse case.

## Permissions Map Tests

**`TestCatalogueMatchesThePublishedMap`** — asserts every row of `internal/privileges/catalogue.go` against the committed `internal/privileges/permissions-map.md`. Found four wrong names that used phrases the Jamf Account picker does not contain. Refresh: `make sync-permissions-map`.

**`TestCatalogueCoversEveryScopeThisCLISends`** — a spec requiring a capability with no row in the catalogue fails this test.

## Scope and Platform Infra Tests

**`TestScopeFromParams`** / **`TestCheckScopeConflict`** — cover the mutual-exclusion of `--tenant-id` and `--environment-id` when both are supplied. Must fire on both the `pro`/`platform` and `security` product paths.

**`TestGatewayUnservedNote`** — pins every direction of the response-side `gatewayUnservedNote` mechanism, including App Installers as the **served** case.

**`TestEveryExampleInvocationNamesACommandThatExists`** — `TestEveryLeafRefusesAnUndocumentedPositional` reads only the leaf the `Example` sits on; this test reads every `jamf-cli` invocation on every `Example` line across all commands.
