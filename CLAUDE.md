# CLAUDE.md

## Read first

- `docs/GLOSSARY.md` — canonical terms for Pro vs Platform vs Classic, blueprint vs config profile, smart vs static groups, scope vs target, etc. Consult before guessing.
- `docs/guides/platform-api-ga.md` — the user-facing Platform API beta→GA migration guide (base URL, scope keys, credentials, the refused-command list, the 403 vocabulary). Update it whenever any of those move; it quotes verbatim CLI output and a specific SDK ingest.
- `docs/solutions/` — categorized postmortems and design-pattern docs (e.g., `conventions/output-flag-matrix-2026-05-08.md`, `design-patterns/cobra-annotations-as-policy-2026-05-11.md`). When starting work in a package, grep `docs/solutions/` for matching `module:` or `tags:` frontmatter.
- `docs/guides/claude-context.md` — how this CLAUDE.md structure works and where to put new context. Read this before adding anything to any CLAUDE.md or `.claude/` file.


## CRITICAL: Credential Input Policy

**Never accept credentials (passwords, tokens, client secrets) via CLI flags or stdin.** This prevents exposure in shell history, `ps` output, and CI/CD logs.

- **Human credentials** (username, password): Interactive prompts only (`term.ReadPassword`). No flags, no env vars, no stdin.
- **Machine credentials** (token, client-id, client-secret): Environment variables (`JAMF_*`, `JAMFPROTECT_*`, `JAMFSCHOOL_*`, `JAMFSECURITY_*`) for CI/CD. Interactive prompts for manual use. Config profiles with `keychain:` references for persistent storage. `--token-file` for file-based CI/CD.
- **Never add** `--password`, `--token`, `--client-secret`, `--token-stdin`, or `--client-secret-stdin` flags to any command.
- **Setup commands** (`pro setup`, `protect setup`, `school setup`, `security setup`, `config add-profile`) must always prompt interactively for credentials — no flag or env var bypass. `pro setup --credentials existing|create` selects which *source* a credential comes from, never the credential: both branches still read every secret through `promptClientCredentials` or `term.ReadPassword`, and `--no-input` is refused on both. A flag naming a source is fine; a flag carrying a value is not.

- **Prove a credential before writing it.** `pro setup --credentials existing` and `config add-profile` both call `verifyProfileCredentials` (`internal/commands/credential_prompt.go`), which does one client-credentials exchange through `auth.Verify{OAuth2,Platform}Credentials`. It calls `exchangeToken` and **not** `GetToken`: the on-disk token cache is keyed on `(baseURL, clientID)` and not on the secret, so a cached token from a working pair would report a later mistyped secret as verified — the one thing verification exists to catch. `TestVerifyOAuth2Credentials_IgnoresTheTokenCache` fails if it is ever moved onto `GetToken`. The check is skipped for token auth (a bearer token is only testable by spending it), for an `env:`/`file:` reference (`config.ResolveSecret` owns those, and a reference is routinely written on a machine that cannot reach the server it names), and under `add-profile --no-verify` for offline pre-seeding. It establishes that the pair is valid and nothing about whether its privileges or scope level reach any command — a 403 answers that at the point of use, in wording setup cannot produce.


## CRITICAL: Generated Code Boundary

**Never edit files in `internal/commands/pro/generated/`** — they are overwritten by `make generate`.

**Never edit files in `internal/commands/platform/generated/`** — they are overwritten by `make generate`.

**Never edit files in `internal/commands/security/generated/`** — they are overwritten by `make generate`.

To change generated command behavior, edit the **generator templates**:
- **Modern API commands:** `generator/parser/generator.go` → `resourceTemplate` const
- **Classic API commands:** `generator/classic/generator.go` → `classicResourceTemplate` const
- **Modern registry:** `generator/parser/generator.go` → `registryTemplate` const
- **Classic registry:** `generator/classic/generator.go` → `classicRegistryTemplate` const
- **Jamf Security Cloud commands:** `generator/security/template.go` → `resourceTemplate` const

Templates are Go `const` strings embedded in the generator source — NOT separate `.tmpl` files.

After modifying a template: `make generate && make test`


## Architecture (overview)

CLI for the Jamf platform. Root command holds shared infrastructure (config, auth, completion). Each Jamf product gets its own namespace — `pro` for Jamf Pro, `protect` for Jamf Protect. Platform API commands live under `pro`.

For package layout, runtime flow, and auth wiring, see the `architecture-overview` skill.

## Where things live

This file only carries what applies to every session. Everything else has been
split out so Claude loads it only when it's actually relevant:

**Skills** (`.claude/skills/`) — loaded on demand via the `superpowers` skill system:
- `where-to-make-changes` — navigation guide for where a given change belongs
- `testing-guide` — CI guards, key tests, smoke test instructions
- `gateway-coverage` — gateway coverage manifest, verdicts, 403 vocabulary, escape hatches
- `common-workflows` — recipes for adding features, syncing specs, adding endpoints/commands

**Always-loaded rules** (`.claude/rules/`) — injected every session:
- `credentials-and-auth.md` — credential policy, auth resolution, scope levels (CRITICAL)
- `coding-style.md` — output routing, flag rules, positional contract, Go conventions
- `classic-api.md` — Classic API paths, body input, wire behavior, schema quirks

**Subdirectory CLAUDE.md files** — loaded only when Claude reads a file in that subtree:
- `internal/protect/CLAUDE.md` — Jamf Protect integration
- `internal/platform/CLAUDE.md` — Jamf Platform API integration, generator knobs, wire facts
- `internal/security/CLAUDE.md` — Security Cloud Radar + gateway-served commands, wire facts
- `internal/profileconvert/CLAUDE.md` — legacy-to-DDM payload conversion
- `generator/CLAUDE.md` — code generation pipeline and generated command features
- `docs/site/CLAUDE.md` — GitHub Pages showcase site

## Extending this file

See `docs/guides/claude-context.md` for the full decision guide. Short rule: if you're adding more than a pointer here, it belongs in `.claude/rules/`, `.claude/skills/`, a subdirectory `CLAUDE.md`, or `docs/guides/`.
