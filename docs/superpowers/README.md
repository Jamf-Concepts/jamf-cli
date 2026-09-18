# `docs/superpowers/`

Working documents for changes in flight: a plan before the code, a spec while
the shape is still being argued about.

They are notes, not a contract. Nothing here is loaded into a session's context
automatically, nothing is checked by CI, and nothing here overrides
`.claude/rules/`, a subdirectory `CLAUDE.md`, or `docs/solutions/`.

**Where a document belongs once the work lands:**

| If the document is... | It belongs in... |
|---|---|
| A rule every session must follow | `.claude/rules/` |
| A recipe for a task, loaded on demand | `.claude/skills/<name>/SKILL.md` |
| Knowledge scoped to one package | that package's `CLAUDE.md` |
| A postmortem, or a design pattern with a date | `docs/solutions/<category>/` |
| An operator-facing guide | `docs/guides/` |

A plan that has shipped and taught nothing durable can simply be deleted — the
code and its tests are the record. A plan that *did* teach something durable
should have that part moved to one of the homes above before the plan is
removed, or it goes with it.

`docs/guides/claude-context.md` is the full decision guide for the Claude-facing
surface.
