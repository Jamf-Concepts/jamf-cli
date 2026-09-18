---
# Classic API

## Paths and Gateway Routing

Classic API paths start with `/JSSResource/` and bypass the `/api` prefix added by `client.Do()`. In platform gateway mode, rewritten to `/proclassic/` with the scope in the `X-Tenant-Id`/`X-Environment-Id` header.

## Body Input — `--from-file`, Stdin, `--scaffold`, `--set`

**Every classic write takes `--from-file`, not just `apply`.** Classic `create` and `update` read their XML body from `--from-file` or, absent the flag, stdin (`readClassicBody` in `classic_registry.go`). Stdin alone was unreachable for the callers that most need these commands (AutoPkg's `JamfCLIRunner` passes a body as `--from-file` or as a JSON dict it serialises). The body may hold credentials — SMTP or LDAP account password — so it belongs in a file, never a flag value or `argv`.

**Classic writes carry `--scaffold`, `--set` and field help, from `specs/classic/schemas.json`** — derived by `generator/classicschema` from the SDK's `classic_api_resource_documentation.json`. 44 of 54 manifest resources bind a schema. Refresh with `make sync-gateway-coverage-from-sdk`. `make verify-classic-schemas` is the CI guard.

**`--set` builds the whole body and is mutually exclusive with `--from-file`**, unlike Platform/Security Cloud `--set` which overlays onto a `--from-file` body. A Classic PUT is a partial update (wire-checked — a body of just `<name>` renamed a network segment and left its address range intact), so `--set` alone is a valid update with no fetch-merge cycle. Piped stdin is a *warning* rather than a refusal, because "stdin is not a character device" is true of the empty stdin a CI runner hands every process.

## Wire Behavior — The API Hides Mistakes

Wire-checked 2026-09-02: an unrecognised element answers **201 and is silently dropped**; an out-of-enum value answers **201 and reads back the default** (`frequency: "Twice per fortnight"` → `Once per computer`; a criterion's `and_or: "maybe"` → `and`); a missing required field answers 409 with the right field named *inside an HTML error page*, one field at a time.

**`--set` refuses an unknown field and an out-of-enum value** — the only validation the CLI does that the server does not. `--help` renders the enum list, which is the part the wire will not teach you.

**`--set` refuses an empty value for an enum field.** The guard read `if values, ok := spec.Enums[key]; ok && raw != ""`, and `""` is out of range for every enum. So `--set general.frequency="$FREQ"` with `FREQ` unset sent `<frequency></frequency>`, the Classic API answered 200, and the policy's execution frequency was silently changed to `Once per computer`. The refusal names the legal set and says to omit the flag instead. A **non-enum** field still takes `key=` and renders an empty element, because clearing a field is a legitimate edit.

## Schema: Repeated-Element Wrapper

**The repeated-element wrapper is collapsed, and the element name is never derivable from the array's name.** Classic models `<criteria><criterion>…</criterion></criteria>` as an array of single-key objects, and 97 of the 373 repeated elements are not a naive singularisation (`criteria`→`criterion`, `smart_groups`→`group`). Read it off the schema (`parser.ClassicRepeatedElement`).

**The spec is not always right about it.** `os_x_configuration_profile.scope.jss_user_groups` declared `<jss_user_group>` where the wire answers `<user_group>` — an upstream typo. Corrected in the SDK's `propertyRenames` and ingested at v0.22.1. The read side is where this bites: a generated Classic type leaves `XMLName xml.Name` untagged, so a mismatched child decodes to nothing and the call reports success — the scoped user group was silently dropped on every decode. A repeated-element name is worth spot-checking against a sibling carrying the same block, and against the resource's own exclusions half.

## Schema: Two Modellings for the Same XML

The same spec uses two modellings: `policy.scripts` is an *object* holding a `script` array plus `size`, where `policy.criteria` is an *array of wrappers*. A renderer handling only the first emits the second a level short.

## Schema: `size` Overloaded

`size` is overloaded: 102 of its 104 occurrences are the server-computed collection counter, but `computer_post`'s `hardware.storage[].device.size` is a capacity in MB. `parser.ClassicIsCountElement` discriminates on a repeated sibling. `TestNoBoundResourceCarriesASemanticSizeField` fails if an ingest ever binds the resource where that test is wrong.

## Schema: Every `id` Is Kept

**Every `id` is kept.** A body `id` is inert — a create sending `<id>99999</id>` was assigned 226, and a PUT to `/id/228` carrying `<id>229</id>` updated 228. Most `id` elements in a Classic body are foreign keys the caller supplies, so a rule stripping them would have to tell identity from reference.

## Schema: `*_post` Write-Shaped Schemas

**The write-shaped `*_post` schema wins where the spec declares one**, and 13 of the 15 are orphaned — no operation references them. They differ from read counterparts only in `xml` and `required`, which is exactly what is needed: `computer_group_post` requires `[name, is_smart]` where `computer_group` declares no top-level `required` at all. Three declare no `xml.name`, so the root falls back to the key with `_post` stripped.

## Credential Field Refusals

**Classic `--set` refuses a credential field, and it is the only one of the four `--set` implementations that enforces the credential policy.** Distribution points, SMTP servers, LDAP servers, directory bindings, VPP accounts and disk-encryption configurations all carry one.

Matched on the field name **and** a string type, because a distribution point declares `username_password_required`, a boolean switch whose name contains "password"; refusing that would block a legitimate setting.

**A credential field is matched on its leaf name *or* its full dotted path.** `credentialFieldNames` alone missed three string-typed secrets: `json_web_token_configuration.encryption_key`, and `disk_encryption_configuration.institutional_recovery_key.{key,data}`. All three were offered by shell completion. The leaf names `key` and `data` are far too broad to substring-match, hence `credentialFieldPaths`, matched on a path suffix. `TestEverySecretBearingFieldIsRefusedForSet` sweeps `specs/classic/schemas.json` for any string-typed field whose name or path names a key, token or secret and requires each to be refused, with four exemptions each carrying a reason. A stale exemption fails, and so does a vacuous walk.

Credential fields are kept out of shell completion.

## `--scaffold` Args Validation

**`classicScaffoldArgs` relaxes the validator only when the `--scaffold` flag is set.** Cobra validates `Args` before `RunE`, so a classic `update` on an id-only resource refused `update --scaffold` with "accepts 1 arg(s), received 0" and never reached the template. The modern Pro `update` escapes it and the rest of that generator did not — `patch` and `x-action` under a path parameter with no name lookup got a bare `ExactArgs(N)` and their `--scaffold` was unreachable on 26 leaves. `resourceTemplate` now emits the same floor-only relaxation inline. `TestScaffoldKeepsTheDeclaredPositionalCeiling` now fails at both ends.

## Self Service Categories

**A Self Service category is stored only if the body carries `display_in`, and the wire reports nothing when it does not.** Wire-checked on Jamf Pro 11.31.1 (2026-09-07): a `<category>` holding only `<id>`, or `<id>` plus `<name>`, or `<feature_in>` without `<display_in>`, is accepted with 201 and silently discarded. `display_in=true` persists; `display_in=false` is a *deletion* gesture. `mobile_device_configuration_profile` was the one resource whose spec `$ref`'d the shared `category` schema without `display_in` — corrected in the SDK's `schemaPatches` at v0.22.2.

**`display_in` is write-only on `mobile_device_configuration_profile` alone** — its GET echoes `<id>` and `<name>` only, where the other five echo `display_in`. `feature_in` is a per-resource capability: four macOS/ebook resources store and echo it defaulted to `false`; both mobile resources store none.

## Policy Scaffold

**A policy scaffold cannot be sent unedited.** `general.category.id`'s spec example is `0`, which answers `409 No match found for category 0`. `scope` and `account_maintenance` answer **500** because their specimen references point at objects that do not exist on the target. The rendering is correct and showing one specimen per optional section is the shared scaffold rule, so the generated help says to delete the sections you do not need.

## Account Group and User XML Root Names

An account group's wire root is `<group>` and an account user's is `<account>`, against the manifest's invented `account_group` and `account_user`. Since `Singular` is also the JSON unwrap key, `classic-account-groups get 8 -o json` returns `{"group": {...}}` while every other classic `get` returns the object flat. Correcting it changes the output shape of two commands, so the derivation reports it as a warning and leaves it alone.

## Dead and Restored Resources

**`classic-computer-configs`** — dead resource. The gateway declares no `/computerconfigurations` and the instance 404s it. Refused whole-resource. `TestARefusedClassicCommandNamesNoPermission` covers it.

**`classic-patch-titles`** — restored at v2082 after being refused since v1993. Now has a bound body schema with `--scaffold`, `--set` and enum help. Confirmed through the CLI on EU environment credential 2026-09-05.

## Classic Gateway Verdicts — Three Granularities

Classic paths are assembled at runtime, so `classicGatewayOps` emits all three per resource:

- **`VerdictSubtree`** (`* <resource>/**`) — does the gateway carry the resource at all. Five resources have no bare collection endpoint (`computerhistory`, `computerapplications`, etc.) so exact-path was wrong for them.
- **`VerdictSubtreeMethod`** (`GET <resource>/**`) — the method a subcommand sends is fixed at generate time even though its path is not.
- **`Verdict`** (`GET <resource>`) — `list`'s path is the one Classic path that *is* fixed.

A withdrawal inside a surviving subtree (`patchpolicies` lost `GET /patchpolicies` and kept `GET /patchpolicies/id/{}`) means the subtree-wide verdict alone is insufficient. `gatewayPrivAnn` returns nothing for a refused Classic command — a refused command must not advertise a grant that cannot make it work.

## Classic Scope — The Field-Order Trap and the Category Matrix

Breaking changes of 2026-09-12; full reasoning in
`docs/solutions/conventions/classic-scope-put-is-scope-only-2026-09-12.md` (the
write) and `classic-scope-matrix-2026-09-12.md` (the categories).

**The write is `<root><scope>…</scope></root>` to the top-level endpoint, one
request.** A Classic PUT is a partial update at top-level-section granularity —
verified on all eight scopeable resources by diffing each whole document with
`<scope>` elided, a 19 KB profile's `<payloads>` included — while `<scope>`
itself is replaced wholesale, so the block has to be sent entire and an empty
category element is what clears one. `PutScope` (`internal/scope/scope.go`)
used to GET the document purely to splice the scope into its bytes, so a
`scope add` cost three GETs; it costs one now, and `replaceScopeInXML` is gone.

**`ScopeXML`'s field order is load-bearing and is not any resource's GET
order.** The Classic XML binding is sequence-ordered: it reads scope children in
schema order and **silently ignores whatever arrives out of it, answering 200
either way**. That is the trap in this area. Echoing a resource's own bytes back
— the obvious way to build a scope-only body — applies cleanly to policies and
profiles and is a silent no-op on `macapplications` and
`mobiledeviceapplications`, whose GET returns `<exclusions>` as buildings,
departments, mobile_device_groups, …; it reads exactly like "those two need more
than `<scope>` in the body", and they do not.
**Do not re-order `ScopeXML`'s fields**, and never assemble a Classic body by
splicing server bytes. `TestMarshalScopeBody_FieldOrderIsSchemaOrder` is the
guard; the marshaller is `marshalScopeBody`.

**There are five scope shapes across the eight resources, not two.** Validation
had one branch for restricted software and one for everything else, so a policy
accepted `--mobile-device-group` and a mobile profile accepted
`--computer-group` — both a GET and a PUT spent to earn `409 Error: Mobile
device groups cannot be assigned to an macOS profile`. `shapes`
(`internal/scope/matrix.go`) holds the matrix, keyed on `Resource.SingularKey`,
one entry per scopeable resource. Read the resource's own GET for the category
set, then confirm anything doubtful with a write probe. Four rows are not
derivable from the resource name:

- iBeacons are per-resource rather than per-family (a policy and a macOS profile
  carry them; the equally computer-scoped `mac_application` does not).
- `<classes>` is an ebook target and nothing else's.
- Restricted software has no limitations tab, and its one narrowing category is
  a `--user` **exclusion** the CLI used to refuse outright.
- `macapplications`' GET advertises a `<mobile_device_groups>` that 409s on
  write.

**An ebook's class targets are delivered in two PUTs, and that is the only way
they work.** Jamf Pro stores `<classes>` only while the **stored** category is
empty: a write made while it already holds a member clears it, and every escape
route fails — carrying the identical value clears it, omitting the element
clears it, and the child's identifier shape makes no difference. Wire-checked
5/5 each way. Since a scope PUT replaces `<scope>` wholesale, no single request
can preserve an existing class across any other scope change, so
`pro classic-ebooks scope add --building` destroyed the class and reported
success. `PutScope` now sends the intended change with `<classes>` emptied, then
the same scope with the classes populated — keyed on the category being
non-empty rather than on the resource, so a classless scope still takes one
request, and the first request carries the real change so an interruption is no
worse than the single request it replaced. The same defect is a hard
`inconsistent result after apply` in terraform-provider-jamfplatform
([#428](https://github.com/jamf/terraform-provider-jamfplatform/issues/428)).

**Collateral loss is checked rather than assumed.** `VerifyScopeWrite` compares
the **whole** scope sent against what came back, not just the item the command
touched — which is how the class loss above stayed silent. Matching is by name,
ID or UDID, because the server augments what it was sent (a member sent by name
returns with an ID, a network segment gains a `uid`), so element equality would
report every successful write as a loss.

**A command registers only its own resource's categories**, which is what makes
`--help` and completion honest, and costs the explanatory refusal: cobra rejects
an unregistered flag before `RunE`. Hence the `jamf:scope-categories` annotation
on each mutating leaf and the hint in `SetFlagErrorFunc`; a genuine typo still
gets `suggestFlag`'s near-match first.

**All-flags are refused client-side**, because the server enforces them by
accepting the write with 200 and dropping the member (wire-checked:
`all_computers=true` plus a `computer_groups` target reads back with the group
gone). `CheckAllFlagConflict` names the categories the flag covers and points at
exclusions, which narrow an all-flag scope rather than conflicting with it.

**`<limit_to_users>` is no longer modelled**, and that deleted a struct, a slice
type, three functions and a branch in five others. The server denormalises
`<limitations><user_groups>` into it on every write and back on every read, so
the two wire paths always carry identical values — verified in both directions,
including that an empty `limitations.user_groups` with `limit_to_users` omitted
clears both.

**A name is resolved to at most one record.** `resolveNameToID` (the two VPP
resources, which have no `/name/` endpoint) used to take the first
case-insensitive match in document order; Classic names are not unique — a live
tenant carried two ebooks sharing one — so it now refuses, naming the colliding
ids and telling the caller to pass one as `<id>`.

## Classic Refusals Have a Readable Reason

The Classic API answers a refused write with an HTML status page, and
`classicHTMLErrorReason` (`internal/client/client.go`) lifts the one actionable
sentence out of it. Passing the page through verbatim buried the answer in ~400
bytes of markup and inline CSS, which inside the JSON error envelope is one
unreadable escaped line. It is gated on the page's `<title>Status page</title>`
and falls back to the raw body, so a JSON envelope, a gateway refusal or a WAF
block is not swallowed.

Four reasons worth knowing, each previously invisible:

- `Error: Unable to match computer group` — the identifier named no record.
- `Error: Unable to match excluded computer group` — a **dangling** reference the
  GET still reports, to an object since deleted. Such a scope is not re-saveable
  by any means.
- `Error: Duplicate name` — two records share one, so neither saves.
- `Unable to update the database` — also seen transiently on objects that saved
  fine on retry, so do not read a single 409 as a property of the request.

## PI-827 Is Not Mitigated by a Smaller Body

A configuration profile whose `<payloads>` carries an escaped entity loses one
escaping level on **any** successful save, whether or not the body carries
`<payloads>`: the server re-serialises the stored payload on write. After that
decay the payload is no longer re-saveable and every further write answers 409.
Wire-checked 2026-09-12 with a scope-only PUT, which decays it identically to a
full-document one. So the scope-only write is smaller and touches fewer fields,
and buys no payload-integrity guarantee.

## Name Resolution

`apply`, `--name`, `--serial` and `--udid` all work by GETting the resource's collection and RSQL-filtering it, so they are generated only when `nameResolutionPath` is non-empty. Resources whose modern API is POST-collection + GET-`{id}` only (dock-items, venafis, cloud-azure, cloud-ldaps) ship ID-only CRUD: no `--name`, no `apply`. Add a `resourceNameLookupPathOverrides` entry if a sibling endpoint can serve the lookup.

## Classic Schema Refresh

`specs/classic/schemas.json` is derived by `generator/classicschema` from the SDK's `classic_api_resource_documentation.json` — the same file gateway coverage reads. Refresh: `make sync-gateway-coverage-from-sdk` (one target derives both artifacts). `make verify-classic-schemas` is the CI guard. Never edit `specs/classic/schemas.json` by hand.
