---
title: pro smart-group — wiki-driven smart-group template library for jamf-cli
date: 2026-05-12
status: shipped
scope: first ship — 4 commands, 23 templates across 5 categories
module: internal/commands, internal/smartgroup
tags: [smart-groups, templates, jss-criteria, mac-wiki, verify-templates]
note: brainstorming initially pivoted from a FileVault read-side helper spec (key/status/escrow-audit) to this template-library design after deciding admins get more value from creating curated smart groups than from inspecting fleet state read-only
---

# pro smart-group — wiki-driven smart-group template library

## Context

Smart groups are the most-used building block in Jamf Pro. Admins target
policies with them, scope configuration profiles, drive compliance reports,
and trigger Self Service. Every Jamf admin in every tenant builds the same
operationally-essential smart groups by hand:

- "Macs where FileVault is not encrypted"
- "Macs with an invalid recovery key"
- "Macs missing a bootstrap token"
- "Macs running OS older than X"
- "Macs that haven't checked in in N days"

These are the same ~20 smart groups, recreated per-tenant, every time. They
are operational knowledge — the kind documented in mac-wiki — but every admin
re-derives them.

This spec defines a curated library of 23 smart-group templates shipped as
first-class CLI commands. Admins go from "I need a smart group for X" to
working smart group in one command. The library lives in jamf-cli as
hardcoded Go (compile-time-safe) and POSTs to the existing
`/v2/computer-groups/smart-groups` endpoint.

This is a follow-on to the `pro filevault` read-side spec (same date) — that
spec is on pause; this one is the new primary feature.

## Audit findings (verified ground truth)

### Endpoint surface

| Endpoint | Purpose |
| --- | --- |
| `POST /v2/computer-groups/smart-groups` | Create a smart group. Required privilege: `Create Smart Computer Groups`. |
| `PUT /v2/computer-groups/smart-groups/{id}` | Update an existing smart group. Required privilege: `Update Smart Computer Groups`. |
| `GET /v2/computer-groups/smart-groups` | List/search (used for apply-pattern name lookup). |
| `POST /v1/smart-computer-groups/{id}/recalculate` | Force membership recalculation. Required privilege: `Update Smart Computer Groups`. |
| `GET /v2/computer-groups/smart-group-membership/{id}` | Read membership (member IDs). Required privilege: `Read Smart Computer Groups`. |

All five endpoints are already exposed by the generator. This spec wraps them
with workflow commands; no spec changes, no generator changes.

### Request body schema

`SmartComputerGroupV2` (from `specs/SmartComputerGroups.yaml`):

```yaml
SmartComputerGroupV2:
  name:        string  (required, minLength 1)
  description: string
  criteria:    [SmartSearchCriterion]
  siteId:      string  (nullable, default "-1")
```

`SmartSearchCriterion` (from `specs/_MonolithLibrary.yaml`):

```yaml
SmartSearchCriterion:
  andOr:         string  (required, "and" | "or")
  name:          string  (required, criterion name from JSS canonical list)
  searchType:    string  (required, e.g. "is", "is not", "more than", "less than", "like", "greater than or equal")
  value:         string  (required, display value matching the criterion's enum)
  priority:      integer
  openingParen:  boolean
  closingParen:  boolean
```

### Canonical criterion-name source

Smart-group criterion names live in the JSS server source. The canonical
file is:

```
jamf-pro-server/SmartSearch/SmartSearchApi/src/main/java/com/jamfsoftware/smartsearch/service/matcher/MatcherNameConstants.java
```

Plus per-matcher `@Component("<Criterion Name>")` annotations on classes
like `FileVault2StatusMatcher.java` and `UserApprovedMdmMatcher.java`. The
`ComputerInventoryValues.java` file in `jss9` is the parallel source for
inventory-display field names. Both were consulted to derive the verified
strings used in the template library below.

**Important:** the wiki's terminology for "Disk Encryption Recovery Key X"
is wrong. JSS uses the `FileVault 2 X` prefix consistently. The library
uses JSS-canonical names.

### Verified criterion-name registry (used by this library)

| Go const | Display string (JSS-canonical) | Source |
| --- | --- | --- |
| `CriterionFV2Status` | `FileVault 2 Status` | `FileVault2StatusMatcher.java:@Component` |
| `CriterionFV2Enabled` | `FileVault 2 Enabled` | `MatcherNameConstants.CD.FILE_VAULT_2_ENABLED` |
| `CriterionFV2RecoveryKeyType` | `FileVault 2 Recovery Key Type` | `ComputerInventoryValues.java:103` |
| `CriterionFV2IndividualKeyValidation` | `FileVault 2 Individual Key Validation` | `ComputerInventoryValues.java:104` |
| `CriterionFV2PersonalRecoveryKey` | `FileVault 2 Personal Recovery Key` | `ComputerInventoryValues.java:106` |
| `CriterionOSVersion` | `Operating System Version` | `MatcherNameConstants.CD.OPERATING_SYSTEM_VERSION` |
| `CriterionOSBuild` | `Operating System Build` | `MatcherNameConstants.CD.OPERATING_SYSTEM_BUILD` |
| `CriterionOSRapidSecurityResponse` | `Operating System Rapid Security Response` | `MatcherNameConstants.CD.OPERATING_SYSTEM_SUPPLEMENTAL_VERSION_EXTRA` |
| `CriterionLastInventoryUpdate` | `Last Inventory Update` | `MatcherNameConstants.MDD.LAST_INVENTORY_UPDATE` |
| `CriterionBootstrapTokenEscrowed` | `Bootstrap Token Escrowed` | `MatcherNameConstants.MDD.BOOTSTRAP_TOKEN_ESCROWED` |
| `CriterionUserApprovedMDM` | `User Approved MDM` | `UserApprovedMdmMatcher.java:@Component` |
| `CriterionMDMProfileExpirationDate` | `MDM Profile Expiration Date` | `MatcherNameConstants.MDD.MDM_PROFILE_EXPIRATION_DATE` |
| `CriterionDDMEnabled` | `Declarative Device Management Enabled` | `MatcherNameConstants.CD.DECLARATIVE_DEVICE_MANAGEMENT_ENABLED` |
| `CriterionGatekeeper` | `Gatekeeper` | `ComputerInventoryValues.java:118` |
| `CriterionSIP` | `System Integrity Protection` | `ComputerInventoryValues.java:119` |
| `CriterionFirewallEnabled` | `Firewall Enabled` | `MatcherNameConstants.CD.FIREWALL_ENABLED` |
| `CriterionSupervised` | `Supervised` | `MatcherNameConstants.MDD.SUPERVISED` |
| `CriterionEnrollmentMethodPrestage` | `Enrollment Method: PreStage enrollment` | `MatcherNameConstants.E.PRESTAGE` |
| `CriterionAppleSilicon` | `Apple Silicon` | `MatcherNameConstants.CD.APPLE_SILICON` |
| `CriterionJamfBinaryVersion` | `Jamf Binary Version` | (parallel inventory criterion; verify-templates will confirm) |

All criterion strings are pulled from JSS source. Where a string is from a
`@Component` annotation it is verbatim; where from `MatcherNameConstants`
or `ComputerInventoryValues` it is verbatim from the right-hand side of
the assignment.

### Verified enum value sets (for criterion values)

| Enum class | Constants | Display strings (from getName) |
| --- | --- | --- |
| `FileVault2Status` | NOT_APPLICABLE, NOT_ENCRYPTED, BOOT_ENCRYPTED, SOME_ENCRYPTED, ALL_ENCRYPTED | `N/A`, `Not Encrypted`, `Boot Partitions Encrypted`, `Some Partitions Encrypted`, `All Partitions Encrypted` (confirmed via `FileVault2StatusMatcher.java`) |
| `GatekeeperStatus` | NOT_COLLECTED, DISABLED, APP_STORE_AND_IDENTIFIED_DEVELOPERS, APP_STORE | display strings need empirical verification — `verify-templates` will catch |
| `SipStatus` | NOT_COLLECTED, NOT_AVAILABLE, DISABLED, ENABLED | likewise |
| `FileVault2KeyType` | INDIVIDUAL, INSTITUTIONAL, BOTH | `Individual`, `Institutional`, `Both` |

### APIs deliberately not in scope

| Surface | Reason |
| --- | --- |
| Generated `pro smart-computer-groups` CRUD | Untouched. The new commands wrap the same endpoints with a workflow surface. |
| Mobile-device smart groups | Out of scope for first ship; same architectural pattern can be added in a follow-on. |
| User smart groups | Out of scope. |
| Editing criterion values per-call (admin-controlled criteria) | Templates are fixed-shape with at most one parameter; admins who need bespoke criteria use the generated CRUD command. |
| External (admin-authored) YAML templates | Locked to hardcoded Go for first ship. |

## Scope

### In scope (this ship)

Four handwritten subcommands under a new `pro smart-group` namespace (alias
`pro sg`):

1. **`pro smart-group templates [--category <X>]`** — list available
   templates with descriptions and parameters.
2. **`pro smart-group preview --template <slug> [params]`** — print the
   exact JSON body that would POST, no API call.
3. **`pro smart-group apply --template <slug> --name <NAME> [params] [--recalculate] [--dry-run] [--yes]`** —
   idempotent create/update, with post-apply membership check.
4. **`pro smart-group verify-templates [--category <X>] [--no-cleanup]`** —
   smoke-test every template against the live tenant; report membership
   count or error per template; cleanup temp groups.

Plus a curated library of 23 templates across 5 categories:
encryption (6), software updates (4), MDM health (5), compliance basics
(4), lifecycle hygiene (4).

### Out of scope (deferred)

- External YAML / admin-authored templates.
- Multi-param templates (current constraint: zero or one param per
  template).
- Mobile-device and user smart-group equivalents.
- `pro filevault` read-side commands (`key`, `status`, `escrow-audit`) —
  deferred to a follow-on cycle per the pivot in
  `2026-05-12-pro-filevault-design.md`.

## Template inventory (23 templates)

Format: slug · criteria · param.

### Category: encryption (6)

| Slug | Criteria | Param |
| --- | --- | --- |
| `encryption/not-encrypted` | `FileVault 2 Status` is `Not Encrypted` | — |
| `encryption/invalid-recovery-key` | `FileVault 2 Individual Key Validation` is `Not Valid` | — |
| `encryption/escrow-missing` | `FileVault 2 Recovery Key Type` is empty | — |
| `encryption/irk-only-deprecated` | `FileVault 2 Recovery Key Type` is `Institutional` | — |
| `encryption/encryption-stalled` | `FileVault 2 Status` is not `All Partitions Encrypted` AND `Last Inventory Update` more than N days | `--stalled-after <days>` (default 7) |
| `encryption/fv-ineligible` | `FileVault 2 Status` is `N/A` | — |

### Category: software updates (4)

| Slug | Criteria | Param |
| --- | --- | --- |
| `updates/os-version-below` | `Operating System Version` less than X | `--below-version <X>` (required) |
| `updates/major-version-behind` | `Operating System Version` less than `<N>.0` | `--major-below <N>` (required, integer) |
| `updates/rsr-not-applied` | `Operating System Rapid Security Response` is empty | — |
| `updates/beta-os` | `Operating System Version` like `Beta` | — |

### Category: MDM health (5)

| Slug | Criteria | Param |
| --- | --- | --- |
| `mdm/bootstrap-token-missing` | `Bootstrap Token Escrowed` is `No` | — |
| `mdm/user-approved-mdm-no` | `User Approved MDM` is `No` | — |
| `mdm/stale-checkin` | `Last Inventory Update` more than N days | `--days <N>` (default 7) |
| `mdm/mdm-cert-expiring` | `MDM Profile Expiration Date` less than N days from now | `--within-days <N>` (default 30) |
| `mdm/declarative-management-disabled` | `Declarative Device Management Enabled` is `No` | — |

### Category: compliance basics (4)

| Slug | Criteria | Param |
| --- | --- | --- |
| `compliance/gatekeeper-disabled` | `Gatekeeper` is `Disabled` | — |
| `compliance/sip-disabled` | `System Integrity Protection` is `Disabled` | — |
| `compliance/firewall-disabled` | `Firewall Enabled` is `No` | — |
| `compliance/non-compliant-baseline` | OR composite: FV2 Enabled `No`, SIP `Disabled`, Gatekeeper `Disabled`, Firewall Enabled `No` | — |

### Category: lifecycle hygiene (4)

| Slug | Criteria | Param |
| --- | --- | --- |
| `lifecycle/unsupervised` | `Supervised` is `No` | — |
| `lifecycle/ade-enrolled` | `Enrollment Method: PreStage enrollment` is `Yes` | — |
| `lifecycle/jamf-binary-outdated` | `Jamf Binary Version` less than X | `--below-version <X>` (required) |
| `lifecycle/fv-ineligible-hardware` | `FileVault 2 Status` is `N/A` AND `Apple Silicon` is `No` | — |

Total: 23 templates, 6 parameterized, 17 zero-param.

## Architecture

### File layout

```
internal/
  commands/
    pro_smartgroup.go             ~400 LOC — namespace + 4 commands
    pro_smartgroup_test.go        unit tests + golden output tests
  smartgroup/                     new package
    types.go                      Template, ParamSpec, TemplateOpts
    library.go                    map[string]Template — all 23 by slug
    criteria.go                   Go consts for the criterion-name strings (sourced from JSS, see Verified Criterion-Name Registry)
    encryption.go                 6 encryption template builders
    updates.go                    4 software-update template builders
    mdm.go                        5 MDM-health template builders
    compliance.go                 4 compliance template builders
    lifecycle.go                  4 lifecycle template builders
    membership.go                 post-apply membership-count check
    verify.go                     verify-templates command logic
    types_test.go                 param validation
    library_test.go               golden JSON for each template's Build()
    criteria_test.go              sanity: every template references a known criterion const
internal/commands/pro.go          +1: cmd.AddCommand(newSmartGroupCmd(cliCtx))
internal/commands/groups.go       +1: "Smart Groups:" help group entry
internal/commands/aliases.go      alias: pro sg → pro smart-group
```

No generator changes. No new specs. Reuses generated `SmartComputerGroupV2`
type for the POST body.

### Type shape

```go
// internal/smartgroup/types.go

type ParamSpec struct {
    Name        string  // CLI flag name, e.g. "stalled-after"
    Type        string  // "int" | "string" | "version"
    Default     any     // nil if Required
    Description string  // for --help
    Required    bool
}

type Template struct {
    Slug        string
    Category    string
    Description string
    Params      []ParamSpec  // zero or one entry
    Build       func(opts map[string]any) (SmartComputerGroupV2, error)
}
```

Each `Build` function returns a SmartComputerGroupV2 body. Implementation
references criterion-name Go consts from `criteria.go` for compile-time
safety against refactors.

## Command specs

### `pro smart-group templates [--category <X>]`

List the library. Default `-o table` groups by category.

```bash
pro smart-group templates                       # all 23
pro smart-group templates --category encryption # 6 in encryption
pro smart-group templates -o json               # machine-readable
```

#### Sample output (table)

```
Smart Group Templates — 23 available across 5 categories

Category: encryption (6)
  encryption/not-encrypted              Macs where FileVault 2 is not enabled
  encryption/invalid-recovery-key       Macs with an INVALID escrowed recovery key
  encryption/escrow-missing             Macs without any escrowed recovery key
  encryption/irk-only-deprecated        Macs on the deprecated Institutional Recovery Key
  encryption/encryption-stalled         Macs stuck mid-encryption (params: --stalled-after)
  encryption/fv-ineligible              Macs with FV status "Not Applicable"

Category: updates (4)
  updates/os-version-below              Macs running OS older than X (params: --below-version)
  updates/major-version-behind          Macs older than macOS <N>.0 (params: --major-below)
  updates/rsr-not-applied               Macs with no Rapid Security Response applied
  updates/beta-os                       Macs running a beta OS

... etc.
```

### `pro smart-group preview --template <slug> [params]`

Print the exact JSON body that would be POSTed, no API call.

```bash
pro smart-group preview --template encryption/invalid-recovery-key
pro smart-group preview --template encryption/encryption-stalled --stalled-after 14
```

#### Sample output

```
POST /v2/computer-groups/smart-groups
{
  "name": "<--name required when running apply>",
  "description": "Auto-generated by jamf-cli (template: encryption/encryption-stalled, stalled-after=14)",
  "criteria": [
    {"andOr": "and", "priority": 0, "name": "FileVault 2 Status", "searchType": "is not", "value": "All Partitions Encrypted"},
    {"andOr": "and", "priority": 1, "name": "Last Inventory Update", "searchType": "more than x days ago", "value": "14"}
  ]
}
```

### `pro smart-group apply --template <slug> --name <NAME> [params] [--recalculate] [--dry-run] [--yes]`

Idempotent create or update by name. Follows existing jamf-cli apply-pattern.

#### Behavior

1. Validate template exists.
2. Validate required params present; type-check values.
3. Build the `SmartComputerGroupV2` body via the template's `Build()` func.
4. If `--dry-run`: print the request and return. No API call.
5. Lookup existing group by `--name` via
   `GET /v2/computer-groups/smart-groups?filter=name=="<NAME>"`.
6. If found: PUT (replace). Confirmation prompt unless `--yes`.
7. If new: POST (create).
8. If `--recalculate`: trigger
   `POST /v1/smart-computer-groups/{id}/recalculate`.
9. **Always: post-apply membership check.** Call
   `GET /v2/computer-groups/smart-group-membership/{id}` and log the
   member count.
10. If membership is zero and the template has known-fragile criteria:
    warn — "This template matched 0 devices. Run
    `pro sg verify-templates` to check criterion compatibility with your
    tenant."

#### Sample output

```
Created smart group 'FV Invalid Recovery Keys' (ID: 287)
Recalculated. Membership: 34 devices.
```

#### Errors and exit codes

- Missing required param → exit 2 (usage), message identifies the param.
- Unknown template slug → exit 2 with fuzzy-match suggestion.
- 403 on POST/PUT → exit 5 with the missing privilege named.
- 409 / name collision without `--yes` → prompt; abort if declined.
- Recalculate timeout (60s default) → exit 0; the group was created;
  warn that recalc may still be running.

### `pro smart-group verify-templates [--category <X>] [--no-cleanup]`

Smoke-test the library against the live tenant. Creates temporary groups
prefixed `__verify_<slug>`, recalculates, reads membership, deletes
(unless `--no-cleanup`).

```bash
pro smart-group verify-templates                       # full library
pro smart-group verify-templates --category encryption # one category
pro smart-group verify-templates --no-cleanup          # leave temp groups for inspection
```

#### Behavior

For each template in scope:

1. Build the body with default param values.
2. POST with name `__verify_<slug>_<random-suffix>`.
3. Recalculate.
4. Read membership count.
5. Categorize as: OK (non-zero match), zero-match warning, or error
   (4xx/5xx response).
6. Delete the temp group unless `--no-cleanup`.

Aggregate report at the end.

#### Sample output

```
Verifying 23 templates against tenant prod-1...

✓ encryption/not-encrypted              — 12 devices match
✓ encryption/invalid-recovery-key       — 34 devices match
✓ encryption/escrow-missing             — 127 devices match
✓ encryption/irk-only-deprecated        — 0 devices match (expected — IRK is rare)
✓ encryption/encryption-stalled         — 2 devices match
✓ encryption/fv-ineligible              — 4 devices match
✓ updates/os-version-below              — 451 devices match (default --below-version=15.0)
...
⚠ compliance/firewall-disabled          — 0 devices match
    Possible criterion-name or value mismatch. Inspect via:
    pro sg preview --template compliance/firewall-disabled

Summary: 22 OK, 1 zero-match warning, 0 errors.
Cleaning up 23 temporary groups...
```

#### Privileges required

- `Create Smart Computer Groups`, `Update Smart Computer Groups`,
  `Delete Smart Computer Groups`, `Read Smart Computer Groups`.

## Wiki use policy

The mac-wiki was the *author-time* source for the operational concepts
this library encodes (which smart groups admins actually need). It is
**not** a runtime dependency:

- No wiki fetching at runtime.
- No wiki content shipped in the binary.
- User-facing strings stand on their own as plain English.
- Internal Go comments reference wiki slugs where relevant (`// concept
  from wiki: security/filevault`) — for maintainer traceability only,
  never printed.

The mac-wiki was also wrong in places — notably the "Disk Encryption
Recovery Key X" terminology. The canonical source for criterion names
is the JSS server source (`MatcherNameConstants.java`, `@Component`
annotations on each Matcher class, `ComputerInventoryValues.java`),
verified during the audit.

## Output and exit-code conventions

- Default output is `table` for all four commands.
- `templates` supports `-o {table, json, csv, yaml, plain}`.
- `preview` is single-record; supports `-o {table, json, plain}`.
- `apply` is single-result; supports `-o {table, json, plain}`.
- `verify-templates` supports `-o {table, json}`.
- Exit codes:
  - `0` success
  - `1` general error
  - `2` invalid usage (missing/conflicting flags, unknown slug)
  - `3` auth error
  - `4` not found
  - `5` permission denied (privilege missing)
  - `6` rate limited

## Testing

### Unit tests

- `internal/smartgroup/library_test.go` — for each of the 23 templates,
  call `Build()` with defaults and assert the resulting JSON matches a
  golden fixture. 23 fixtures.
- `internal/smartgroup/types_test.go` — param validation: required
  missing → error, type mismatch → error, out-of-range int → error.
- `internal/smartgroup/criteria_test.go` — every `Build()` output's
  criteria must reference at least one criterion-name const from
  `criteria.go`. Catches refactor typos at test time.
- `internal/commands/pro_smartgroup_test.go` — table-driven, mock HTTP,
  golden outputs per `-o` format per command.

### Output-flag matrix

Per the project memory note (`feedback_output_flag_matrix.md`), every
command must be exercised against `-o {table, json, csv, yaml, plain}`,
`--quiet`, `--no-color`, `--out-file`, and `--field` where applicable.

### No live-tenant tests

`verify-templates` is the manual smoke check against a real tenant; not
wired into automated CI.

## Open questions

1. **Display values vs enum constants.** JSS enum constants are
   SCREAMING_SNAKE_CASE; display values are spaced strings (e.g.
   `BOOT_ENCRYPTED` → `Boot Partitions Encrypted`). The smart-group
   criterion `value` field expects display values. We have the canonical
   strings for `FileVault2Status` (confirmed via `FileVault2StatusMatcher`)
   but display strings for `GatekeeperStatus` and `SipStatus` need
   empirical verification. `verify-templates` will catch any mismatches
   on first run against a real tenant.
2. **`searchType` value strings.** Some are obvious (`is`, `is not`,
   `like`). Date-relative ones (`more than x days ago`) and version-
   compare ones (`less than`, `greater than or equal`) are confirmed from
   `specs/Groups.yaml` examples but the full set of legal `searchType`
   values per criterion type isn't enumerated in the OpenAPI spec.
   `verify-templates` will surface any rejected combinations.
3. **`Apple Silicon` criterion value.** Used by
   `lifecycle/fv-ineligible-hardware`. The criterion exists
   (`MatcherNameConstants.CD.APPLE_SILICON`) but the value strings need
   confirmation. Likely `Yes` / `No`.
4. **Empty-value semantics.** Templates like `encryption/escrow-missing`
   ("Recovery Key Type is empty") and `updates/rsr-not-applied` ("RSR is
   empty") rely on a specific searchType+value combination for "is
   empty." The exact form (likely `searchType: "is"` with `value: ""`,
   or possibly a dedicated `"is null"` searchType) needs empirical
   verification. The implementation plan should pin this down with a
   single test against a live tenant before the templates ship.
5. **Verifying required-param templates.** `verify-templates` needs a
   value to test parameterized templates. Required-param templates
   (`updates/os-version-below`, `updates/major-version-behind`,
   `lifecycle/jamf-binary-outdated`) have no defaults. Decision: the
   verify runner uses sensible test values it hardcodes per template
   (e.g., `--below-version=15.0`, `--major-below=15`,
   `--below-version=11.0.0`). These are inline in `verify.go`, not part
   of the public template API.
6. **`encryption/encryption-stalled` precision.** Current criteria
   match anything not `All Partitions Encrypted` — which includes
   `Not Encrypted` and `N/A` devices, not just stalled ones. The
   implementation plan should consider tightening to a specific value
   list (`Boot Partitions Encrypted` OR `Some Partitions Encrypted`)
   via two criteria joined with `andOr: "or"`. Trade-off: tighter
   matching but a multi-criterion build path.

## Success criteria

- `pro smart-group templates` prints the full curated library grouped by
  category, with parameter signatures shown for parameterized templates.
- `pro smart-group preview --template <slug>` produces a JSON body
  identical to what `apply` would POST, byte-for-byte (other than the
  name field placeholder).
- `pro smart-group apply --template <slug> --name <NAME>` creates a
  smart group whose membership count is reported to the user.
  Idempotent across re-runs with the same name.
- `pro smart-group verify-templates` runs the full library against a
  live tenant, reports OK/warning/error per template, and cleans up
  temp groups by default.
- All criterion-name strings used by templates are sourced from JSS
  canonical files and documented with file:line citations in
  `criteria.go` comments.
- No changes to `generator/`, `specs/`, or any generated file.

## Follow-on (deferred to a future cycle)

- External (admin-authored) YAML templates loaded from
  `~/.config/jamf-cli/smart-group-templates/`. Architecture leaves
  space for this — the `Library` map can be extended by a loader at
  startup.
- Mobile-device smart-group templates and user smart-group templates.
- Multi-parameter templates if a real need emerges that can't be split
  into discrete templates.
- The `pro filevault` read-side commands (`key`, `status`,
  `escrow-audit`) from the precursor spec.
- Generalization across verticals: when a template library grows to
  cover MSU / PSSO / etc., consider factoring shared infrastructure
  into a generic `pro template` namespace.
