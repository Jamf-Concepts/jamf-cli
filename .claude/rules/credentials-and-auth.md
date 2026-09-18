---
# Credentials and Authentication

## CRITICAL: Credential Input Policy

**Never accept credentials (passwords, tokens, client secrets) via CLI flags or stdin.** This prevents exposure in shell history, `ps` output, and CI/CD logs.

- **Human credentials** (username, password): Interactive prompts only (`term.ReadPassword`). No flags, no env vars, no stdin.
- **Machine credentials** (token, client-id, client-secret): Environment variables (`JAMF_*`, `JAMFPROTECT_*`, `JAMFSCHOOL_*`, `JAMFSECURITY_*`) for CI/CD. Interactive prompts for manual use. Config profiles with `keychain:` references for persistent storage. `--token-file` for file-based CI/CD.
- **Never add** `--password`, `--token`, `--client-secret`, `--token-stdin`, or `--client-secret-stdin` flags to any command.
- **Setup commands** (`pro setup`, `protect setup`, `school setup`, `security setup`, `config add-profile`) must always prompt interactively for credentials — no flag or env var bypass. `pro setup --credentials existing|create` selects which *source* a credential comes from, never the credential: both branches still read every secret through `promptClientCredentials` or `term.ReadPassword`, and `--no-input` is refused on both.

**Prove a credential before writing it.** `pro setup --credentials existing` and `config add-profile` both call `verifyProfileCredentials` (`internal/commands/credential_prompt.go`), which does one client-credentials exchange through `auth.Verify{OAuth2,Platform}Credentials`. It calls `exchangeToken` and **not** `GetToken` — the on-disk token cache is keyed on `(baseURL, clientID)`, so a cached token would report a mistyped secret as verified. `TestVerifyOAuth2Credentials_IgnoresTheTokenCache` fails if it is ever moved onto `GetToken`. Skipped for token auth, `env:`/`file:` references, and `add-profile --no-verify`.

## Auth Resolution Order

env vars (`JAMF_TOKEN`, `JAMF_CLIENT_ID`, `JAMF_CLIENT_SECRET`, `JAMF_TENANT_ID`) > config profile.

Three methods: **token** (pre-existing bearer), **oauth2** (client credentials against instance `/api/oauth/token`), **platform** (client credentials against Jamf Platform Gateway, e.g. `https://{region}.api.jamfcloud.com/auth/token`; requires `--tenant-id` or `--environment-id`; Classic paths routed through `/proclassic/`, modern through `/pro/{version}/`, with the scope in an `X-Tenant-Id` or `X-Environment-Id` header; also constructs `jamfplatform-go-sdk` client enabling Platform API commands).

Secrets in config use prefixed references: `env:VAR`, `file:/path`, `keychain:service/account`. Bare values in `config add-profile` are stored in keychain automatically.

**Both `checkAPIMatch` directions key on the resolved auth method, not on a profile**, so env-var credentials behave identically to profiles. The credential source is named in error messages from `credentialSource`, not `resolvedProfile`, because `resolveAuth`'s precedence is env-then-profile while `resolvedProfile` stays whatever `-p` or `default-profile` names.

## Auth Bypass Mechanisms

`root.go` bypasses auth by four mechanisms:

| mechanism | matches | for |
|---|---|---|
| `chainSkip` name set | `completion`, `help`, `config`, `diff`, `setup`, `multi`, `doctor`, `mcp`, `agent-context` — **anywhere** in the chain | a whole namespace that never calls an API |
| `rootOnlySkip` name set | `commands`, `version` — only as a **direct child of the root** | an ordinary English word that is also a generated operation name |
| `jamfcli/group-parent` annotation | the one command it is set on | a parent `guardUnknownSubcommands` made runnable purely to refuse a typo |
| `jamf:no-auth` annotation | the one command it is set on | a hand-written command that calls no API (`pro backup list-resources`) |

Any `--scaffold` invocation bypasses auth as well. **Prefer an annotation over a name.** A name map matches every command that shares the name wherever it sits; an annotation travels with the one command it is set on.

## Platform Gateway Scope Levels

A Jamf Platform API integration is created at one of three levels in Jamf Account. Its credential only works with that level:

| level | header | what it reaches |
|---|---|---|
| organization | *none* — resolved from the access token | Jamf Account: `account-licenses`, `deal-registrations`, `distributor-*`, `sso-connections`, `sso-domains` (US-only), plus AI Governance |
| platform environment | `X-Environment-Id` | a group of tenants — **the level to prefer**; also the only level `platform audit` accepts |
| tenant | `X-Tenant-Id` | one Jamf Pro / School / Protect / Security Cloud tenant — the legacy level |

**`platform setup` asks for the environment ID first and stops there if it gets one** — the levels are mutually exclusive, so asking for a tenant next would offer a combination no credential can use (`promptScope`). `auth.Scope` (`internal/auth/scope.go`) carries the pair and answers `Header()`, which returns `("", "")` for organization scope.

**Organization scope is entered when the gateway host itself is detected.** `isPlatformGatewayURL` (`internal/commands/platform.go`) makes the `.api.jamfcloud.com` **suffix** a request for platform auth — so `JAMF_URL` + `JAMF_CLIENT_ID` + `JAMF_CLIENT_SECRET` with no tenant/environment ID works for organization-scoped credentials.

## Withheld Profile Scope

**A profile's scope level belongs to the profile's own integration and is withheld when a different client ID is in use.** `profileScopeAppliesTo` (`internal/commands/platform_scope.go`) drops the profile's `environment-id`/`tenant-id` when `JAMF_CLIENT_ID` is set to a value different from the profile's resolved `client-id`. Keyed on the client ID rather than "any credential from the environment" — a profile naming its own client ID as `env:JAMF_CLIENT_ID` is the profile's own integration and keeps its level.

Every non-prompting reference form is resolved and compared; `keychain:` alone is not (resolving one would prompt on a system that asks). Note: `keychain:<profile>/client-id` is exactly what `platform setup` writes, so this case is real for a CI environment that also exports `JAMF_CLIENT_ID`.

`withheldProfileScope` feeds `withheldScopeNote` — without it the request carries no scope header, the gateway answers a bare 400, and there is no hint naming the profile and its level.

## Scope Conflict Check

`checkScopeConflict` refuses `--tenant-id X --environment-id Y` together, on both the `pro`/`platform` and `security` product paths. The flag vars are read explicitly from `clientIDFromInvocation` for the `security` product (which returns from `PersistentPreRunE` before `resolveAuth` folds the env vars), so both paths are covered.

## Retired Gateway URL

`refuseRetiredGatewayURL` inside `newPlatformSDKClient` refuses a profile still naming `{region}.apigw.jamf.com` before any request. Deliberately not beside its callers — lives on the one constructor every platform path calls, so it cannot be forgotten by the next caller. The GA host is `https://{region}.api.jamfcloud.com` (no `/api` segment).

## Scope ID Probe in Platform Setup

`reportScopeIDProbe` (`internal/commands/platform_gateway_setup.go`) probes `/pro/v1/jamf-pro-version` (a no-capability-privilege endpoint) to validate the scope ID:

| scope header | answer |
|---|---|
| correct owned ID | 200 |
| unknown environment UUID or tenant pasted at env prompt | 404 `ENVIRONMENT_NOT_FOUND` |
| unknown tenant, foreign tenant, or environment ID pasted at tenant prompt | 403 `OWNERSHIP_FORBIDDEN` |
| none (organization scope) | skipped entirely |

Everything else leaves the ID unjudged. `BAD_PERMISSIONS` means the header resolved; reading it as a verdict would produce a false entitlement claim. The probe is skipped for organization scope (no header to validate).

## Setup Command Merging

Both `platform setup` and `security setup` **merge into the profile rather than replacing it** — assigning a fresh literal would zero every field the other command owns. `mergePlatformProfile` (`platform.go`) and `mergeSecurityProfileBase` (`security_setup.go`) are the merges, tested in both orders. `security setup` fills `product` and `auth-method` only when unset, so running it second does not demote a gateway profile.

## Where the Scope Resolution Lives

`resolveScope` (`internal/auth/scope.go` plus its caller in `root.go`) reads the
`--environment-id` / `--tenant-id` **flag vars first**, then
`JAMF_ENVIRONMENT_ID` / `JAMF_TENANT_ID`, then the profile's own keys. It reads
`JAMF_CLIENT_ID` from the environment **directly** (`clientIDFromInvocation`),
because `PersistentPreRunE` returns for the `security` product before
`resolveAuth` folds the env vars into the package var — so a rule reading only
the folded var would silently not apply on the path serving the 52
gateway-served Security Cloud commands. `ResolveAuthForProfile` reads
`params.ClientID` only, because `resolveAuth` has already folded by the time it
is called and params is the complete answer there; reading the mutable package
var instead made the function's result depend on global state its params exist
to isolate.

`scopeFromParams` is the term that makes an explicitly supplied level
**replace** the profile's rather than joining it: `JAMF_ENVIRONMENT_ID` against
a tenant profile has to mean "use this environment", and merging the two instead
reported a mutual-exclusion error for a perfectly sensible override.

`scopeLevelNote` hands the withheld-scope remedy to `withheldScopeNote` rather
than rendering a second one — an earlier version said "no scope header was
sent" twice and then advised setting an ID on the very profile whose ID had just
been ignored. Both ladders use the **resolved** profile name from
`config.GetProfile`, not the requested one: passing the empty string on made the
rule read as "there is no profile" and dropped the scope of every default-profile
user.

`platformGatewayRegions` (`internal/commands/platform.go`) holds the GA hosts,
and `refuseRetiredGatewayURL` refuses a profile still naming the retired
`{region}.apigw.jamf.com` before any request is sent. **The refusal lives inside
`newPlatformSDKClient`, not beside its callers**: `ResolveAuthForProfile`
checked it while the `security` and `school` resolvers did not, because
`PersistentPreRunE` returns early for both products — so a stale profile reached
the gateway-served Security Cloud commands and `school blueprints` and failed
inside the token exchange, which is the useless symptom the refusal exists to
replace. A guard on the one constructor every path must call cannot be forgotten
by the next caller.

`resolveSchoolClient` requires a tenant ID before it constructs a platform
client, so a withheld level leaves the client nil and **no request is sent** —
the `REQUEST_CONTEXT_NOT_PROVIDED` every other arm of
`AnnotateScopeLevelError` keys on never arrives.
`platform.ErrNoPlatformClient` is the sentinel both client gates wrap, so that
error can carry the withheld note. Deliberately not a stderr warning at
resolution time: `resolveSchoolClient` runs for every `school` command,
including the ones that never touch the Platform API.

`printScopeSummary` / `printPermissionsNote` (`internal/commands/platform_scope.go`)
assemble `platform setup`'s closing summary from the `jamf:scopes` annotation.
It **reports the level and nothing about entitlements**: an earlier version
subtracted the sixteen Security Cloud groups on one `BAD_PERMISSIONS`, which
emptied the list for the ordinary Jamf Pro tenant and did so on a code that
cannot separate "no entitlement" from one missing grant.
