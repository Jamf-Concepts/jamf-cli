# Solutions

Documented solutions to past problems — bugs, design patterns, conventions —
that future contributors (human or AI) should learn from rather than rediscover.

Inspired by [cli-printing-press](https://github.com/mvanhorn/cli-printing-press)'s
`docs/solutions/` archive. The shape is intentionally close to theirs so the
authoring habit transfers cleanly.

## When to write a solution doc

Write one when **any** of these apply:

- You fixed a class of bug, not a single instance. The fix is generalizable, but
  the path to discovering it is non-obvious.
- You introduced (or refactored to) a pattern that other parts of the codebase
  should adopt. Documenting it now beats explaining it in five future code
  reviews.
- A convention was decided that isn't enforceable by lint or tests. The doc
  is the enforcement mechanism — reviewers point to it.
- A debugging session took >30 minutes and the next person hitting the same
  symptom should be able to short-circuit it.

Don't write one for:

- One-off bugs in a single file with no broader lesson. The commit message is
  enough.
- Conventions already covered in `CLAUDE.md` or `docs/GLOSSARY.md`.
- Style preferences without a load-bearing reason behind them.

## Directories

Group by problem type:

- `best-practices/` — how to do something well, when more than one way exists
- `conventions/` — rules the codebase follows that aren't enforceable by tooling
- `design-patterns/` — reusable structural patterns with named application sites
- `logic-errors/` — bug classes worth remembering (root cause + correct fix)
- `security-issues/` — anything where the wrong fix has a security cost

## File format

Each solution is a single Markdown file. Filename pattern:
`<short-kebab-title>-<YYYY-MM-DD>.md`. The date is the resolution date, not the
discovery date.

Required YAML frontmatter:

```yaml
---
title: "One-line title describing the rule, not the bug"
date: 2026-05-08
category: conventions          # one of the directory names above
module: <package-or-area>      # e.g., "internal/commands", "generator", "internal/output"
problem_type: convention       # design_pattern | convention | bug | best_practice | security
severity: medium               # low | medium | high
applies_when:
  - "concrete situation 1"
  - "concrete situation 2"
tags: [keyword1, keyword2, keyword3]
---
```

Body sections:

```markdown
## Context

What happened, what was confusing, what didn't work. Link to PRs, issues,
or commits.

## Guidance

The rule itself — actionable, in imperative voice. If there's a code shape,
show the shape. If there's a checklist, list it.

## Why this beats the alternative (optional)

Only when "just do X" isn't obvious and the cost of the wrong path is real.
```

## Reading these

Future contributors (human and AI) should `grep` this directory by tag or
keyword when implementing or debugging in a documented area. The frontmatter
is structured so it's easy to search across.

Future AI sessions: when starting work in a package, scan
`docs/solutions/*/*.md` for matching `module:` or `tags:`. If a solution
covers the area you're touching, follow its guidance rather than rediscovering
the same bug.
