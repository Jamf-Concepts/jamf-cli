# Glossary

Canonical terms used in jamf-cli. When user-facing ambiguity affects what
action to take, ask before acting — the same word often means different
things across Pro, Platform, Protect, and School.

## Product namespaces

| Term | Meaning |
|------|---------|
| **Jamf Pro** (`pro`) | The flagship MDM/UEM. Surfaces both the modern **UAPI** (`/api/v1/...`) and the legacy **Classic API** (`/JSSResource/...`). Most commands live here. |
| **Jamf Platform** / **Platform Gateway** (`platform`) | The cross-product API surface hosted at `<region>.apigw.jamf.com`. Adds Blueprints, Compliance Benchmarks, Platform Devices/Groups. Platform auth (`auth-method: platform`) also enables Pro API calls — they're proxied through the gateway at `/api/pro/tenant/<id>/...` and `/api/proclassic/tenant/<id>/...`. |
| **Jamf Protect** (`protect`) | The EDR/security product. GraphQL only; uses `jamfprotect-go-sdk`. No relation to Pro auth — separate credentials, separate `JAMFPROTECT_*` env vars. |
| **Jamf School** (`school`) | The K-12-focused MDM. Separate REST API; uses `jamfschool-go-sdk`. Separate `JAMFSCHOOL_*` env vars. |

## API surfaces under Pro

| Term | Meaning |
|------|---------|
| **UAPI** / **modern API** | The newer Pro JSON REST API at `/api/v1/...` (and `/v2`, `/v3`). What new feature work targets. |
| **Classic API** | The legacy XML API at `/JSSResource/...`. Still widely used; many resources have no UAPI equivalent. Routed through `/api/proclassic/tenant/{id}/...` under Platform gateway auth. |
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
| **Configuration profile** / **config profile** / **mobileconfig** | An XML `.mobileconfig` payload (legacy Apple format). Delivered via the Pro UAPI or Classic API. Imperative MDM-style. |
| **Declaration** / **DDM** | Declarative Device Management — Apple's newer pull-based config model. Delivered as JSON to the device. |
| **Blueprint** | The Platform API's DDM container. A blueprint bundles multiple **components** (Passcode, Software Update, etc.) into one deployable unit. Lives in Platform, not Pro. |
| **DDM component** | A single declaration inside a blueprint (e.g., `com.apple.passcode`, `software-update-settings`). Each maps to one Apple declaration type. |
| **Legacy-to-DDM conversion** | `import-profile` auto-converts compatible payloads from a mobileconfig into native DDM components. `--legacy` opts out. Unsupported payloads are filtered unless `--include-unsupported` is set. |

## Groups and scope

| Term | Meaning |
|------|---------|
| **Smart group** | A dynamic group whose membership is computed from criteria (e.g., "all Macs running macOS 14"). Pro has `smart-computer-groups`, `mobile-device-smart-groups`. Membership refreshes on inventory updates. |
| **Static group** | A manually-curated list of devices. Pro has `static-computer-groups`, `mobile-device-static-groups`. Add/remove by ID. |
| **Scope** | In Pro: which devices/users/groups a policy, profile, or app applies to. The Classic API exposes this as `<scope>` XML; UAPI as a `scope` JSON object. |
| **Target** | In Platform: similar concept to Pro's scope, but the API uses "target" (e.g., a blueprint targets device groups). Not interchangeable in code — different field names, different APIs. |
| **Extension Attribute** / **EA** | A custom inventory field defined by a Pro admin (e.g., "Is FileVault enabled?"). Computers and mobile devices have separate EA registries. |

## MDM mechanics

| Term | Meaning |
|------|---------|
| **MDM command** | An imperative push to a device (`DeviceLock`, `EraseDevice`, `ClearPasscode`, `UpdateInventory`). Sent via Pro's command API; flushable with `pro computers flush-commands`. |
| **Declaration** (vs MDM command) | Pull-based config. The device fetches its current declaration set on a schedule; no per-command tracking. |
| **Prestage** | Enrollment-prep configuration sent at activation. `computer-prestages` for macOS (ADE/DEP), `mobile-device-prestages` for iOS/iPadOS. |
| **ADE / DEP / Automated Device Enrollment** | Apple's zero-touch enrollment program. "DEP" was the old name; current docs use "ADE" or "Automated Device Enrollment". Both refer to the same flow. |

## Compliance

| Term | Meaning |
|------|---------|
| **Compliance benchmark** | A Platform compliance framework (e.g., CIS macOS Benchmark). Has versions; each version contains rules and baselines. |
| **Baseline** | A YAML config within a benchmark version that selects which rules apply. The customer-editable layer. |
| **Rule** | One individual check within a benchmark (e.g., "FileVault must be enabled"). Read-only — vendor-defined. |
| **Compliance report** | The per-device evaluation of a baseline. `pro compliance-benchmarks device-results` returns the latest. |

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

- **"scope" vs "target"** — if you can't tell from context whether the user means the Pro field or the Platform field, ask.
- **"profile"** — could mean (a) a config-file profile in `~/.config/jamf-cli/`, (b) a mobileconfig configuration profile, (c) a DDM declaration profile. Ask which.
- **"group"** — smart vs static, computer vs mobile — four combinations. Ask if unclear.
- **"policy"** — Pro has policies (`/policies` Classic), Protect has plans (sometimes called "policies"), Platform doesn't have either. Confirm the product.
- **"command"** — could mean a CLI subcommand (`jamf-cli pro computers list`) or an MDM command (a `DeviceLock` push). Context usually disambiguates, but if it doesn't, ask.
