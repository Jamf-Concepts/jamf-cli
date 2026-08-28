# Glossary

Canonical terms used in jamf-cli. When user-facing ambiguity affects what
action to take, ask before acting — the same word often means different
things across Pro, Platform, Protect, and School.

## Product namespaces

| Term | Meaning |
|------|---------|
| **Jamf Pro** (`pro`) | The flagship MDM/UEM. Surfaces both the modern **UAPI** (`/api/v1/...`) and the legacy **Classic API** (`/JSSResource/...`). Most commands live here. |
| **Jamf Platform** / **Platform Gateway** (`platform`) | The cross-product API surface hosted at `<region>.api.jamfcloud.com` (the pre-GA `<region>.apigw.jamf.com` is retired). Adds Blueprints, Compliance Benchmarks, Platform Devices/Groups. Platform auth (`auth-method: platform`) also enables Pro API calls — they're proxied through the gateway at `/pro/...` and `/proclassic/...`, with the scope in an `X-Environment-Id` or `X-Tenant-Id` header. |
| **Jamf Protect** (`protect`) | The EDR/security product. GraphQL only; uses `jamfprotect-go-sdk`. No relation to Pro auth — separate credentials, separate `JAMFPROTECT_*` env vars. |
| **Jamf School** (`school`) | The K-12-focused MDM. Separate REST API; uses `jamfschool-go-sdk`. Separate `JAMFSCHOOL_*` env vars. |

## API surfaces under Pro

| Term | Meaning |
|------|---------|
| **UAPI** / **modern API** | The newer Pro JSON REST API at `/api/v1/...` (and `/v2`, `/v3`). What new feature work targets. |
| **Classic API** | The legacy XML API at `/JSSResource/...`. Still widely used; many resources have no UAPI equivalent. Routed through `/proclassic/...` under Platform gateway auth, with the scope in a request header. |
| **JCDS** | Jamf Cloud Distribution Service — file storage for installer packages. Commands: `pro jcds upload`, `pro jcds download`, `pro jcds sync`. |
| **API integration** / **API client** | An OAuth2 client-credentials pairing registered in Pro (Settings > System > API Roles & Clients). Distinct from a generic API token. |

## Authentication

| Term | Meaning |
|------|---------|
| **Token auth** (`auth-method: token`) | Pre-existing bearer token in the `Authorization: Bearer ...` header. Used for legacy basic-auth bootstraps and short-lived UAPI sessions. |
| **OAuth2** (`auth-method: oauth2`) | Client-credentials flow against the instance's `/api/oauth/token`. The standard for modern Pro automation. |
| **Platform** (`auth-method: platform`) | Client-credentials flow against the gateway's `/auth/token`. Requires `--tenant-id`. Enables both Platform API commands AND Pro API commands routed through the gateway. |
| **`keychain:` / `env:` / `file:` references** | Config-file prefixes for resolving secrets at runtime. Bare values are never stored in `config.yaml` — they're moved to keychain on profile creation. |

## Configuration delivery

These words look interchangeable but aren't. Picking the wrong one changes
which API you call and which device-side mechanism applies the config.

| Term | Meaning |
|------|---------|
| **Configuration profile** / **config profile** / **mobileconfig** | An XML `.mobileconfig` payload (legacy Apple format). Delivered via the Pro UAPI or Classic API. Traditional imperative MDM. |
| **Declarative Device Management** / **DDM** | Per Apple, an **additive** layer on top of the MDM protocol. Devices proactively and autonomously apply management settings and report state changes asynchronously via the **status channel**. Not a replacement for MDM — coexists with it on the same enrollment. |
| **Declaration** | A single DDM payload (e.g., `com.apple.configuration.passcode.settings`, `com.apple.configuration.softwareupdate.settings`). The device pulls and applies declarations on its own schedule rather than waiting for an MDM push. |
| **Status channel** | The DDM-specific communication channel devices use to proactively report state changes back to Jamf Pro/School. Some inventory attributes subscribe to this channel and update without an explicit inventory command. |
| **Blueprint** | A **Jamf Pro** AND **Jamf School** feature that bundles DDM declarations (and increasingly, configuration-profile payloads too) into a single deployable unit. Built on DDM. Scoped to smart groups or static groups. Available in both products via the Platform Gateway API surface — *the API lives in `specs/platform/`, but the user-facing concept is a Pro/School feature, not a separate "Platform product".* |
| **Blueprint component** | A single declaration or supported configuration-profile payload inside a blueprint. Newer Apple payloads (AirPrint, Restrictions, Lock Screen Message, etc.) can be added as components alongside DDM declarations. |
| **Legacy-to-DDM conversion** | `import-profile` auto-converts compatible mobileconfig payloads into native DDM components in a blueprint. `--legacy` opts out. Unsupported payloads are filtered unless `--include-unsupported` is set. |

## Groups and scope

| Term | Meaning |
|------|---------|
| **Smart group** | A dynamic group whose membership is computed from criteria (e.g., "all Macs running macOS 14"). Pro has `smart-computer-groups`, `mobile-device-smart-groups`. Membership refreshes on inventory updates. |
| **Static group** | A manually-curated list of devices. Pro has `static-computer-groups`, `mobile-device-static-groups`. Add/remove by ID. |
| **Scope** | In Jamf Pro, **scope is the unified concept** for "which computers/mobile devices/users receive a remote management task." It comprises three sub-functions: **targets**, **limitations**, and **exclusions** (see below). Classic API exposes scope as `<scope>` XML; UAPI as a `scope` JSON object. Scope can be based on individual devices/users, groups, departments, buildings, directory groups, network segments, classes, or iBeacon regions — the available items vary per task type. **Scope cannot be based on personally owned devices.** |
| **Target** (scope sub-function) | The initial pool of intended recipients for a management task. **Required** to deploy any task. *Not* a separate Platform-only concept — targets are part of Pro scope. |
| **Limitation** (scope sub-function) | Optional filter that reduces the target pool to a specific subsection. Requires an initial target. |
| **Exclusion** (scope sub-function) | Optional filter that omits devices/users from the target pool. **Exclusions always override targets and limitations.** |
| **Extension Attribute** / **EA** | A custom inventory field defined by a Pro admin (e.g., "Is FileVault enabled?"). Computers and mobile devices have separate EA registries. Compliance Benchmarks auto-generate per-benchmark EAs (`[Benchmark Name] - Failed Result List`). |

## MDM mechanics

| Term | Meaning |
|------|---------|
| **MDM command** | An imperative push to a device (e.g., "Send blank push", "Update inventory", "Wipe computer", "Enable/Disable Bluetooth", "OS update — download & install"). Pro tracks pass/fail state per command; flush stuck or failed ones with `pro computers flush-commands`. |
| **DDM** (vs MDM command) | Additive layer — devices pull declarations and report state autonomously via the status channel. Same enrollment runs both; pick the right tool: imperative one-shot action → MDM command; continuously-enforced configuration state → declaration. |
| **PreStage enrollment** | (Capital S — that's the Apple/Jamf canonical spelling.) Configuration applied to devices going through Automated Device Enrollment, configured *before* the device activates. `computer-prestages` for macOS, `mobile-device-prestages` for iOS/iPadOS. Settings sync with Apple every two minutes. |
| **Automated Device Enrollment** / **ADE** | Apple's zero-touch enrollment program. **Formerly DEP.** Jamf docs use "Automated Device Enrollment" or "ADE"; DEP is explicitly the old name. Devices purchased through Apple Business Manager / Apple School Manager auto-enroll without user action. |
| **Account-driven Device Enrollment** | (macOS 14+) Alternative to PreStage where users sign in with a Managed Apple Account in System Settings to initiate enrollment. No URL link required. |

## Compliance

| Term | Meaning |
|------|---------|
| **Compliance Benchmarks** | A **Jamf Pro** feature built on the **macOS Security Compliance Project (mSCP)** framework. Supports industry standards (CIS macOS Benchmarks, etc.) for aligning Mac fleet security posture with recognized practices. |
| **Benchmark** | A specific compliance configuration created from a template (e.g., CIS Level 1). Each benchmark has its own name and gets a Jamf-Pro-auto-generated smart group `[Name] - Compliant` + extension attribute `[Name] - Failed Result List`. |
| **Benchmark template** | The mSCP-derived starting point used when creating a benchmark — e.g., CIS L1, CIS L2, BIO2 (Government Information Security Baseline 2), NLMAPGOV. |
| **Rule** | One individual check within a benchmark (e.g., "FileVault must be enabled"). Vendor-defined; updates flow from mSCP. Some rules accept **ODVs** (Organization-Defined Values) for customization. |
| **Enforcement type** | Per-benchmark setting controlling whether failures auto-remediate. Jamf recommends starting with **Monitor only** to observe before enforcing. |
| **Baseline** *(Platform API term)* | The CLI subcommand and SDK term for the customer-editable YAML config that selects rules within a benchmark version. The Jamf Pro UI doesn't use "baseline" — it talks about "rules" and "ODVs" directly. This is a naming-gap between the underlying Platform API (`compliance-benchmarks baselines`) and the user-facing UI. |

## Commands and aliases

| Term | Meaning |
|------|---------|
| **Generated command** | A command emitted by `make generate` into `internal/commands/{pro,platform}/generated/`. **Never hand-edit these.** Fix the template in `generator/parser/generator.go` instead. |
| **Hand-written command** | A command in `internal/commands/pro_*.go`, `protect_*.go`, `school_*.go`. Owns business logic (upsert, apply, clone, etc.) — wraps generated commands. |
| **`apply`** | Upsert (create-or-update) by name. Hand-written; available on most resources. |
| **`overview`** | A product-level dashboard command. `pro overview` makes ~37 parallel API calls; `protect overview` makes ~14. |
| **Alias** | A short alternate command name, registered in `aliases.go`. E.g., `comp` → `computers`, `bp` → `blueprints`. |

## Implementation conventions

| Term | Meaning |
|------|---------|
| **`pro` / `protect` / `school` / `platform`** as command-tree parents | Each product has a bridge file (`pro.go`, `protect.go`, etc.) that wires its subcommands. New commands land under the right bridge. |
| **`cliCtx` / `registry.CLIContext`** | The shared infrastructure handle passed to every command — HTTPClient, AuthProvider, Output formatter, optional PlatformSDKClient/Protect/School clients. |
| **Cobra annotations (`lint:*`, `jamf:*`, `mcp:*`)** | Policy metadata attached to commands. `lint:keep-flag` suppresses the dead-code linter; future namespaces will drive MCP exposure, exit-code policy, destructive-command gating. Read the cobra `Annotations` map, not magic strings in code. |
| **Provenance** | The spec file paths + SHA256 hashes baked into the generated package at `make generate` time. Surfaced via `jamf-cli version -v`. Lets users diagnose "which spec version is this CLI generated from?" without git archaeology. |

## When to ask

- **"scope"** — could mean (a) the full Pro scope object (targets + limitations + exclusions), (b) just the "targets" sub-function casually called "scope", or (c) Platform's scope field on a blueprint. Different field names in code.
- **"target"** — almost always means a Pro scope sub-function. Don't assume it's a separate Platform concept.
- **"profile"** — could mean (a) a config-file profile in `~/.config/jamf-cli/`, (b) a mobileconfig configuration profile, (c) a Jamf-managed enrollment profile (MDM profile installed at enrollment), (d) a PreStage enrollment payload. Ask which.
- **"group"** — smart vs static, computer vs mobile — four combinations. Ask if unclear.
- **"policy"** — Pro has policies (`/policies` Classic), Protect has plans (sometimes informally called "policies"), Platform doesn't have either. Confirm the product.
- **"command"** — could mean a CLI subcommand (`jamf-cli pro computers list`) or an MDM command (a `DeviceLock` push). Context usually disambiguates, but if not, ask.
- **"benchmark" vs "baseline"** — in the Jamf Pro UI, the unit is a *benchmark*, with *rules* and *ODVs*. In the CLI/Platform API, *baseline* shows up as a subcommand because that's the API resource name. They refer to overlapping but not identical things — ask whether the user means the UI concept or the API resource.

## Sources

The product-terminology entries above were cross-checked against Jamf Product
Documentation (`learn.jamf.com`) via Ask Jamf — particularly the Jamf Pro
Documentation, Jamf Pro Blueprints Configuration Guide, Compliance Benchmarks
Configuration Guide, and Jamf 100 Course Lesson 17 (Device Scope). When
documentation and CLI behavior diverge (e.g., the `baseline` naming gap),
both are noted.

If you find an entry that contradicts current `learn.jamf.com` content, fix
the entry — these are point-in-time captures of vocabulary that can drift
with product renames (DEP → ADE, Azure ID → Microsoft Entra ID, etc.).
