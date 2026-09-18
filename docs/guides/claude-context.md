# Claude Context — Contributing Guide

This project uses Claude Code for development. AI sessions and agents load context
from several places. This guide explains the structure so human contributors and AI
sessions both know where to put new context and where to look for it.

## How context loads

| File / directory | When it loads | Purpose |
|---|---|---|
| `CLAUDE.md` (root) | Every session | Critical cross-cutting policies only: credential rules, generated code boundary, architecture overview, pointers to everything below |
| `.claude/rules/*.md` | Every session (imported by `@`-path lines in root `CLAUDE.md`) | Always-relevant conventions and wire facts: coding style, Classic API behavior, auth and credential wiring |
| `.claude/skills/*/SKILL.md` | On demand, when the task matches | Recipes and navigation guides: how to add a feature, sync specs, find where a change belongs |
| `<package>/CLAUDE.md` | When Claude reads a file in that subtree | Package-specific quirks, invariants, and gotchas |
| `docs/guides/` | When explicitly referenced or searched | Human-readable reference docs; Claude is pointed at key guides in the root CLAUDE.md "Read first" section |

## Where to put new context

**Adding a critical policy that must hold in every session** (e.g. a new credential security rule, a new generated code boundary):
→ Add to the relevant section in the root `CLAUDE.md`.

**Adding a convention or wire fact that applies broadly but not every session** (e.g. a new API quirk, a coding convention):
→ Create or update `.claude/rules/<topic>.md`, and add both a pointer in the "Always-loaded rules" list and an `@.claude/rules/<topic>.md` import line in root `CLAUDE.md` (the `@`-import is what actually loads it every session).

**Adding a workflow recipe or navigation guide** (e.g. how to add a new product namespace, how to sync a new spec source):
→ Create or update `.claude/skills/<topic>/SKILL.md`, and add a pointer in the "Skills" list in root `CLAUDE.md`.

**Adding context specific to one package** (e.g. a subtle invariant in the generator, a wire fact about one API family):
→ Add to or create `<package>/CLAUDE.md` in that directory. No root pointer needed — Claude loads it automatically.

**Adding human-readable reference documentation** (e.g. a migration guide, a design pattern doc, a postmortem):
→ `docs/guides/` for guides, `docs/solutions/` for postmortems and design patterns. Add a pointer to the root CLAUDE.md "Read first" section if it's something every session should know about.

## What not to put in the root CLAUDE.md

The root file loads on every session regardless of what is being worked on. Adding content there that only matters for a fraction of tasks degrades every session. If you find yourself writing more than a pointer in the root file, it almost certainly belongs somewhere else in this structure.

## The `.claude/` directory

`.claude/rules/` and `.claude/skills/` are committed and tracked (see `.gitignore`). Rules load every session via `@`-path import lines in the root `CLAUDE.md`; skills are surfaced on demand by Claude Code's skill system when a task matches. These files are the primary mechanism for persistent behavioral guidance beyond what the root CLAUDE.md carries.

`.claude/settings.json` is also committed — it holds project-level Claude Code settings (hooks, permissions).

Personal or machine-local preferences that should not be shared with the team belong in `.claude.local.md` (gitignored).

## What the Restructure Deliberately Dropped

The pre-restructure root `CLAUDE.md` was 46,838 words and always loaded. The
split is a condensation, not a relocation: roughly a third of it was wire-probe
vocabulary that belongs in a git commit or an upstream issue rather than in
every session's context. This section records what was dropped on purpose, so a
future reader can tell a deliberate cut from an accidental one.

**Kept, and moved to a new home.** Every mechanism, override table, guard test
and trap whose absence would change what a session *does*. If you find a symbol
in Go source that the prompt surface does not name and it is none of the
categories below, that is a gap — restore its paragraph to the file that owns
its subject.

**Dropped on purpose:**

| Category | Examples | Why |
|---|---|---|
| Upstream commit SHAs and build numbers | `adb8d7b`, `c91fce8`, `sdkCommit` | They identify one moment in another repo's history. The fact they attached to is kept; the SHA is recoverable from `specs/gateway/coverage.json`'s recorded revision or the SDK's own log. |
| Probe tenant names | `wisconsam` | Names a sandbox, not a rule. |
| Raw JSON field and enum vocabulary | `categoryId`, `siteId`, `connectionType`, `hostnames`, `isoCountry`, `tenantIds`, `APP`, `CUSTOM`, `TENANT_NOT_FOUND`, `use1`, `writeOnly`, `eulaAccepted`, `jssUrl` | Reachable from the spec in `specs/`, which is the authority. Kept only where the CLI's behaviour turns on the exact spelling (`baseline-id` vs `baselineId`, `ScopeXML`'s field order, `display_in`). |
| SDK-internal symbols | `Logger`, `ErrorHandler`, `PassthroughErrorHandler`, `RequestLogHook`, `isRetryableWriteStatus`, `WithTenantID`, `WithEnvironmentID` | They are another repo's API. The consequences this repo depends on are kept (never hand the platform SDK a retry client; prefer `Client.Scope()` over `TenantID()`). |
| Per-spec filenames | `ComputerPrestageScopeV2`, `PatchSoftwareTitleConfigurations`, `account_sso` | Command identity comes from paths and tags now, not filenames, so a spec filename no longer decides anything. |
| Retired mechanisms | `annotateAuditScopeError`, `DeduplicateVersioned`, `replaceScopeInXML`, `limit_to_users` | Deleted from the code. The *reason* each was deleted is kept where a future change might re-introduce it; the symbol itself would only be looked up and not found. |
| Classic resources the CLI does not ship | `mobiledevicehistory`, `patchavailabletitles`, `patchreports` | Named only as examples of the five resources with no bare collection endpoint, which is a rule the gateway-coverage skill states without the list. |

**Not a category, and therefore a gap if you find one:** an override table, a
generator pass, a guard test, a wire fact the CLI's own behaviour depends on, or
any trap whose symptom is a silent wrong answer.
