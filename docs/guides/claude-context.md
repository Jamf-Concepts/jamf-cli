# Claude Context — Contributing Guide

This project uses Claude Code for development. AI sessions and agents load context
from several places. This guide explains the structure so human contributors and AI
sessions both know where to put new context and where to look for it.

## How context loads

| File / directory | When it loads | Purpose |
|---|---|---|
| `CLAUDE.md` (root) | Every session | Critical cross-cutting policies only: credential rules, generated code boundary, architecture overview, pointers to everything below |
| `.claude/rules/*.md` | Every session (injected by the superpowers harness) | Always-relevant conventions and wire facts: coding style, Classic API behavior, auth and credential wiring |
| `.claude/skills/*.md` | On demand, when the task matches | Recipes and navigation guides: how to add a feature, sync specs, find where a change belongs |
| `<package>/CLAUDE.md` | When Claude reads a file in that subtree | Package-specific quirks, invariants, and gotchas |
| `docs/guides/` | When explicitly referenced or searched | Human-readable reference docs; Claude is pointed at key guides in the root CLAUDE.md "Read first" section |

## Where to put new context

**Adding a critical policy that must hold in every session** (e.g. a new credential security rule, a new generated code boundary):
→ Add to the relevant section in the root `CLAUDE.md`.

**Adding a convention or wire fact that applies broadly but not every session** (e.g. a new API quirk, a coding convention):
→ Create or update `.claude/rules/<topic>.md`, and add a pointer in the "Always-loaded rules" list in root `CLAUDE.md`.

**Adding a workflow recipe or navigation guide** (e.g. how to add a new product namespace, how to sync a new spec source):
→ Create or update `.claude/skills/<topic>.md`, and add a pointer in the "Skills" list in root `CLAUDE.md`.

**Adding context specific to one package** (e.g. a subtle invariant in the generator, a wire fact about one API family):
→ Add to or create `<package>/CLAUDE.md` in that directory. No root pointer needed — Claude loads it automatically.

**Adding human-readable reference documentation** (e.g. a migration guide, a design pattern doc, a postmortem):
→ `docs/guides/` for guides, `docs/solutions/` for postmortems and design patterns. Add a pointer to the root CLAUDE.md "Read first" section if it's something every session should know about.

## What not to put in the root CLAUDE.md

The root file loads on every session regardless of what is being worked on. Adding content there that only matters for a fraction of tasks degrades every session. If you find yourself writing more than a pointer in the root file, it almost certainly belongs somewhere else in this structure.

## The `.claude/` directory

`.claude/rules/` and `.claude/skills/` are committed and tracked (see `.gitignore`). They are loaded by the [superpowers](https://github.com/anthropics/claude-code) skill system. When working with Claude Code, these files are your primary mechanism for persistent, session-specific behavioral guidance beyond what the root CLAUDE.md carries.

`.claude/settings.json` is also committed — it holds project-level Claude Code settings (hooks, permissions).

Personal or machine-local preferences that should not be shared with the team belong in `.claude.local.md` (gitignored).
