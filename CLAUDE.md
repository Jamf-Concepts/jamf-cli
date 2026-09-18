# CLAUDE.md

## Read first

- `docs/GLOSSARY.md` — canonical terms for Pro vs Platform vs Classic, blueprint vs config profile, smart vs static groups, scope vs target, etc. Consult before guessing.
- `docs/guides/platform-api-ga.md` — the user-facing Platform API beta→GA migration guide (base URL, scope keys, credentials, the refused-command list, the 403 vocabulary). Update it whenever any of those move; it quotes verbatim CLI output and a specific SDK ingest.
- `docs/solutions/` — categorized postmortems and design-pattern docs (e.g., `conventions/output-flag-matrix-2026-05-08.md`, `design-patterns/cobra-annotations-as-policy-2026-05-11.md`). When starting work in a package, grep `docs/solutions/` for matching `module:` or `tags:` frontmatter.
- `docs/superpowers/` — plans and specs for work in flight. Notes rather than a contract: nothing there is loaded automatically or checked by CI, and nothing there overrides a rule. `docs/superpowers/README.md` says where a document belongs once the work lands.

## CRITICAL: Credential Input Policy

**Never accept credentials (passwords, tokens, client secrets) via CLI flags or stdin.**

The policy lives in one file, `.claude/rules/credentials-and-auth.md`, which is
`@`-imported below so every session loads it. It is deliberately not duplicated
here: five lines of it were byte-identical in both files, so the one policy
flagged as never-optional had two copies that could drift — and did, the
injected copy losing its section heading and leaving a table with no header.

## CRITICAL: Generated Code Boundary

**Never edit files in `internal/commands/pro/generated/`, `internal/commands/platform/generated/`, or `internal/commands/security/generated/`** — they are overwritten by `make generate`.

To change generated command behavior, edit the **generator templates** — see `generator/CLAUDE.md` for the pipeline and template locations. After modifying a template: `make generate && make test`.

## Build & Dev Commands

See the `dev-commands` skill (`.claude/skills/dev-commands/SKILL.md`).

## Architecture (overview)

CLI for the Jamf platform. Root command holds shared infrastructure (config, auth, completion). Each Jamf product gets its own namespace — `pro` for Jamf Pro, `protect` for Jamf Protect. Platform API commands live under `pro`.

For package layout, code generation, and product integrations, see the pointers below.

## Where things live

This file only carries what applies to every session. Everything else has been
split out so Claude loads it only when it's actually relevant:

**Skills** (`.claude/skills/`) — loaded on demand:
- `where-to-make-changes` — navigation guide for where a given change belongs
- `testing-guide` — CI guards, key tests, smoke test instructions
- `gateway-coverage` — gateway coverage manifest, verdicts, 403 vocabulary, escape hatches
- `common-workflows` — recipes for adding features, syncing specs, adding endpoints/commands
- `dev-commands` — build/run/dev commands

**Always-loaded rules** (`.claude/rules/`) — imported below so every session
loads them regardless of harness:
- `credentials-and-auth.md` — credential policy, auth resolution, scope levels (CRITICAL)
- `coding-style.md` — output routing, flag rules, positional contract, Go conventions
- `classic-api.md` — Classic API paths, body input, wire behavior, schema quirks

@.claude/rules/credentials-and-auth.md
@.claude/rules/coding-style.md
@.claude/rules/classic-api.md

**Subdirectory CLAUDE.md files** — loaded only when Claude reads a file in that subtree:
- `internal/protect/CLAUDE.md` — Jamf Protect integration
- `internal/platform/CLAUDE.md` — Jamf Platform API integration, generator knobs, wire facts
- `internal/security/CLAUDE.md` — Security Cloud Radar + gateway-served commands, wire facts
- `internal/profileconvert/CLAUDE.md` — legacy-to-DDM payload conversion
- `generator/CLAUDE.md` — code generation pipeline and generated command features
- `docs/site/CLAUDE.md` — GitHub Pages showcase site

## Extending this file

See `docs/guides/claude-context.md` for the full decision guide. Short rule: if
you're adding more than a pointer here, it belongs in `.claude/rules/`,
`.claude/skills/`, a subdirectory `CLAUDE.md`, or `docs/guides/`.
