# Changelog

Per-release notes — the full list of merged pull requests — are generated on
[GitHub Releases](https://github.com/Jamf-Concepts/jamf-cli/releases). This file exists for
what that list cannot say: which changes are **breaking**, what the migration is, and why.
So it records breaking changes, behaviour changes visible to a script, and removals. A
release with none of those gets no entry here.

Versions follow the `vMAJOR.MINOR.PATCH` tags in this repository, and headings match the
commit types the repo already uses (`feat!`/`build!` for a breaking change).

## v1.28.0

The Jamf Platform API reached general availability on 2026-09-03. Most of this release is
that migration; **[docs/guides/platform-api-ga.md](docs/guides/platform-api-ga.md) is the
migration guide** and carries the detail, the error messages verbatim, and the reasoning.

### Breaking — Platform gateway (only affects `auth-method: platform` profiles)

- **The gateway base URL is `https://{region}.api.jamfcloud.com`.** The pre-GA
  `https://{region}.apigw.jamf.com` is retired, and the `/api` path segment it required is
  gone. A profile still naming the old host is refused **by name** before any request is
  sent, because the wire symptom is an edge-level 403 during the token exchange that names
  neither the host nor the reason. The CLI does not rewrite the URL for you.
- **Public-beta credentials were revoked at GA.** Register a replacement API integration in
  Jamf Account; a beta client cannot be migrated. `jamf-cli platform setup` writes a fresh
  profile.
- **Three scope levels, one per profile: organization, platform environment, tenant.**
  `environment-id` (new; `--environment-id`, `JAMF_ENVIRONMENT_ID`) is the level to prefer;
  `tenant-id` is the legacy one; organization scope carries no ID and is selected by the
  gateway host alone. Supplying two levels at once is refused, in the environment as well as
  in a profile. The scope now travels in an `X-Environment-Id` / `X-Tenant-Id` header
  instead of a `/tenant/{tenantId}` URL segment.
- **75 Jamf Pro and Classic commands are refused on a gateway profile**, before a request is
  sent, with exit code 8 (`Refused by policy`) — they are outside the gateway's published
  API. Several of them still answer today; that is transitional, and refusing now is cheaper
  than the eventual bare `403 BAD_PERMISSIONS`. The remedy is a second `oauth2` profile
  against the instance.
- **24 of those are MDM device actions**, which is the refusal most likely to be felt:
  `pro mobile-devices` loses `lock`, `restart`, `shutdown`, `enable-lost-mode`,
  `disable-lost-mode`, `play-lost-mode-sound`, `clear-passcode`,
  `clear-restrictions-password`, `delete-user`, `log-out-user`, `unlock-user-account`,
  `apply-redemption-code`, `refresh-cellular-plans`, `request-mirroring`, `stop-mirroring`
  and `settings`; `pro computers-inventory` loses `lock`, `restart`, `shutdown`,
  `enable-remote-desktop`, `disable-remote-desktop`, `set-recovery-lock`,
  `set-auto-admin-password` and `settings`. The gateway's published API declares GET on
  those paths but not POST, so the refusal is per method rather than per resource.
  `pro comp erase` and `pro comp remove-mdm` are hand-written and are **not** affected.
  The other 51 are `pro api-integrations` (7), `pro classic-computer-configs` (7),
  `pro api-roles` (6), `pro authentications` (6),
  `pro static-computer-groups` (6, use `pro computer-groups-static-groups`),
  five `pro classic-patch-titles` subcommands, `pro api-roles-privileges` (2),
  `pro classic-patch-reports` (2), `pro policy-properties` (2), `pro systems` (2),
  and one each of `pro classic-patch-policies list`, `pro database-connections`,
  `pro environment-type`, `pro mac-os-managed-software-updates`,
  `pro mdm-commands commands` and `pro oauth-token-sessions`.
  `jamf-cli commands -o json | jq -r '.[] | select(.gateway=="unserved") | .command'`
  reports the current list for the binary in hand. `JAMF_CLI_ALLOW_UNPUBLISHED=1` downgrades
  an *unpublished* refusal to a stderr warning and sends the request anyway — a stopgap for
  one job, not a mode to settle into, and the warning it substitutes cannot be silenced. The
  reverse direction is refused the same way: a Platform-only command on an instance profile
  exits 8 naming the profile, its resolved auth method and `platform setup`, rather than
  reading as a credential problem.
- **Exit code 8 is new** — `Refused by policy`, for a command that is correctly invoked but
  cannot be served by the resolved credentials. Distinct from 2, which is also every cobra
  flag error.
- **A platform command's 403 now exits 5, not 1.** Platform commands previously returned the
  SDK's error untouched, so the one failure with a specific remedy exited with the generic
  code.

### Breaking — everything else

- **`--out-file`, `--select`, `--compact`, `--field`, `--quiet` and `--no-hints` now take
  effect on 27 commands that parsed and discarded them.** Those commands built their own
  output formatter, which receives none of the global flags, so the flag was accepted and
  then ignored: `--out-file` exited 0 and left the file at 0 bytes while the payload went to
  stdout. Two script-visible consequences. A job passing `--out-file` and reading **stdout**
  now reads nothing, because the payload goes to the file it asked for. And a job passing
  `--select` or `--compact` and parsing whole rows now receives narrowed rows. Affected are
  the `pro report` family, `pro audit`, `pro overview`, `protect overview`,
  `school overview`, `pro group-tools`, `pro classic app-usage`, `multi` and `commands`.
  A `--select` naming a field a row does not carry drops that row and says so on stderr;
  `--quiet` and `--no-hints` silence the note, not the drop.
- **`pro audit -o raw` and `-o xml` render a table rather than JSON.** Both used to marshal
  the rows and hand the bytes to `PrintRaw`, which passes JSON through unchanged for those
  two formats; the shared formatter's own dispatch has no case for either and renders a
  table. Every other format is byte-identical. Use `-o json` for JSON.

- **`pro backup` and `pro diff` refuse a stray positional argument**, with exit 2
  (`usage`). Both take their whole input as flags, and cobra supplies no default validator,
  so a positional was discarded and the command ran anyway: `pro backup somegarbage
  --output DIR` started a full backup from a typo. A wrapper or a CI job that passes a stray
  token now fails where it used to succeed. Remove the argument, or move it to the flag that
  takes it. The refusal names the command and the value, and adds any required flag that is
  also unset, so `pro diff staging production` still reports `--source` and `--target`
  rather than replacing that answer with the positional complaint. That clause is
  conditional: a refusal that can suggest a subcommand instead names no flag, so
  `pro backup list-resourcez` suggests `list-resources` and stays silent about `--output`,
  which there is the root format flag rather than a directory.
- **An error raised before the output format is resolved now follows `default-output`.** The
  `-o` default was `json`, so a flag-parse error and an argument refusal always rendered the
  JSON envelope on stdout — even where the profile pinned another format, and even on a
  terminal — while the same profile's `RunE` errors, raised after resolution, rendered plain
  text. Two answers to one question. The default is now empty, meaning unresolved, and both
  paths answer the same way. A profile pinning `default-output` to a non-json format
  therefore gets plain text on stderr for those two error classes where it previously got
  the envelope on stdout, **piped runs included**: a wrapper parsing that envelope must key
  on the plain-text form, or pass `-o json` explicitly. With no `default-output` set a piped
  run still gets the envelope, so an unconfigured CI job is unaffected.
- **A cobra usage error now exits 2, not 1.** Four classes move: a missing required flag, a
  flag group with no member set, a flag group with mutually exclusive members set together,
  and the wrong number of positional arguments. An unknown flag and an unknown subcommand
  already exited 2, so the two halves of one mistake answered differently. `pro backup
  --nosuchflag` exited 2 and `pro backup` with no `--output` exited 1. Exit 1 is the generic
  failure code, so a script could not tell a bad invocation from a failed request. The scope
  is wide: 48 call sites declare a required flag, 118 declare a flag group, and 671 validate
  an argument count. A wrapper that treats exit 1 as a bad invocation must key on 2 instead.
- **`pro ddm-reports declaration get` and `pro ddm-reports device get` are removed.** Both
  endpoints were deprecated upstream in favour of a sibling the CLI already shipped:
  `declaration devices <id> --filter …` and `device declarations <id> --filter …`. The
  successors declare `filter` required, so there is no unfiltered read left; use
  `--filter 'active=in=(true,false)'` where you want everything.
- **`pro comp erase` and `pro comp remove-mdm` send `/v4/computers-inventory/{id}/…`**,
  where they were pinned to `/v1/computer-inventory/{id}/…`. Neither has a version fallback,
  so an instance that does not serve v4 answers 404. Flags, targeting and confirmation are
  unchanged.
- **`jamf-cli config list` no longer has a `tenant-id` column** in `table`, `csv` and
  `plain` output; it always has `environment-id` and `default` instead. `-o json` and
  `-o yaml` are unchanged and still carry `tenant-id`. Parse the JSON, not the table.
- **Building from source needs Go 1.27** (`go.mod` declares `go 1.27.0`, up from `1.26.6`).
  The default `GOTOOLCHAIN=auto` fetches it; a pinned older toolchain fails. Binary releases
  are unaffected.

### Changed

- **An empty list prints `[]`, not `null`.** Applies to Pro's `list --all` and to every
  Platform and Security Cloud list. `jq` pipelines previously failed with "Cannot iterate
  over null" on exactly the tenants where a collection was empty.
- **`pro computers-inventory` sends `/v4` instead of `/v3`**, and `get` reads the v4 detail
  endpoint. Generated subcommands retry the `/v1` path on a 404 and warn on stderr.
- **`pro computer-inventory-collection-settings custom-path` sends v2, whose `scope` accepts
  only `APP`.** v1 served `[APP, FONT, PLUGIN]`, so a `FONT` or `PLUGIN` path that worked
  before now answers a 400. Nothing in `--help` says so — the Pro generator renders no
  allowed-value lists.
- **A 403 names the permission in the vocabulary of the API that answered** — capability
  permissions with Jamf Account's own section and permission names for a gateway request,
  Jamf Pro API-role privilege names for an instance request. `commands -o json` carries both
  (`privileges`, `gatewayPrivileges`, `gatewayPermissions`) plus an `api` field naming the
  serving API.
- **`commands -o json`'s `gatewaySuccessor` reports a value**, where it was documented but
  emitted on no row at all: six refused commands now name the replacement the runtime
  refusal and the `--help` caveat already named. A script reading the catalog rather than
  running `--help` was told only that a command was `unserved`.
- **A CDN/WAF refusal is reported as one** rather than as `permission denied (HTTP 403)`
  with an HTML page in the message and a hint about API roles. Known triggers: `file://`
  anywhere in a request body, `.pkg` upload content, a burst of writes. A `.pkg` upload
  through a gateway profile is currently refused; upload through an instance profile.
- **`-n, --dry-run` is honoured on Platform and Security Cloud writes**, printing method,
  resolved path and body to stderr. Hand-written platform writes with no per-command
  preview are refused under `-n` rather than executed.
- **`-v` labels retried requests** with the attempt number and the wait, so a slow call is
  distinguishable from a retry sequence.

### Added

- **Classic writes gained `--scaffold` and `--set`**, plus required-field and enum lists in
  `--help`: 111 of 117 `create`/`update`/`apply` commands, across 43 of the 54 Classic
  resources.
  `--set` builds the whole body and is mutually exclusive with `--from-file`; it refuses an
  unknown field, an out-of-enum value and a credential field, because the Classic API
  answers `201` and silently drops or defaults the first two.
- **`--file` accepts YAML** on generated Platform and Security Cloud commands, matching
  Pro's `--from-file`.
- **`protect backup` and `protect restore`** capture and replay a whole Jamf Protect tenant.
  Each object is written to its own file in the same portable, name-referenced form the
  matching `export` produces; restore walks the tree in dependency order, resolving
  references by name against the target, and never deletes. Both take `--resources` and
  `--exclude`. Backup prunes documents an earlier run left that no longer match the tenant
  (`--no-prune` keeps them), and refuses to prune a directory another tenant has written to.
  Two files carry secrets verbatim and are written `0600` — `action-configs` and
  `data-forwarding`, because an HTTP report client's bearer token cannot be redacted without
  breaking restore. Note git records no non-exec permissions, so a clone of a backup repo
  hands them back `0644`. A mixed result exits 7.
- **`protect analytics overrides`** manages the tenant overlay on Jamf-managed analytics
  (`tenantSeverity`, `tenantActions`) — `list`, `get`, `set`, `apply`, `export`, `clear`.
  `analytics list` reports Jamf's baseline severity rather than the effective one, and
  `analytics export` omits the overlay entirely, so this is the only route to the
  customisation a tenant actually made.
- **Jamf AI Governance:** `platform ai-policies`, `platform ai-tools`.
- **Jamf Account:** `platform account-licenses`, `deal-registrations`,
  `distributor-configuration`, `distributor-purchase-orders`, `distributor-quotes`,
  `sso-connections`, `sso-domains`. Organization scope, and US-only — a non-US profile is
  refused before sending.
- **Platform audit:** `platform audit` (environment scope only). Not `pro audit`, which runs
  health checks against a Jamf Pro instance.
- **Jamf Security Cloud through the gateway:** `security dns-*`, `ztna-*`,
  `content-categories`, `device-groups`, `uem-*`, `enrollment-activation-profiles`. Every
  `security` command's `Short` says which API serves it — platform gateway or Radar — since
  the two halves take different credentials.
- **App Installers on the gateway.** The endpoints are published upstream now, so the
  commands are generated from that spec rather than a reverse-engineered one and are no
  longer refused on a gateway profile. New: `pro app-installers get`,
  `app-installer-titles versions`, `app-installer-global-settings deployment-controls`,
  `history` and `add-history-note`.
- `pro ddm-reports declaration devices` and `device declarations` gained `--page`; only the
  first page was reachable before.

### Fixed

- A YAML request body carrying a timestamp scalar or a non-string mapping key — both legal
  YAML, neither expressible in JSON — was reported as malformed input.
- `pro platform-device-groups` name lookups built a stale `/tenant/{id}/` path, which
  collapsed to `/tenant//` under environment or organization scope.
- `JAMF_ENVIRONMENT_ID` now overrides a tenant-scoped profile rather than colliding with it,
  the way every other environment variable here overrides the profile.
