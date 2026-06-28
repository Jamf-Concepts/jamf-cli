---
title: "Cobra annotations as policy contract for cross-cutting concerns"
date: 2026-05-11
category: design-patterns
module: internal/commands
problem_type: design_pattern
severity: low
applies_when:
  - "Adding a structural verifier (lint, audit, scorecard) that needs per-command metadata"
  - "Designing MCP exposure rules (read-only, destructive, hidden) for commands"
  - "Marking commands that intentionally use non-zero exit codes for success-by-policy"
  - "Adding a destructive-command confirmation gate that needs an allowlist"
tags:
  - cobra
  - annotations
  - policy
  - lint
  - mcp
  - destructive-commands
  - structural-verification
---

# Cobra annotations as policy contract for cross-cutting concerns

## Context

When jamf-cli's dead-code lint (PR #198) needed an allowlist mechanism for
intentional flag retentions, the choice was between:

- Comment-based markers (`//lint:keep` above declarations)
- A separate `.linterignore` file with patterns
- Cobra command annotations (`Annotations: map[string]string{"lint:keep-flag": "name1,name2"}`)

We picked Cobra annotations — and the deciding factor wasn't this single
linter. It was the recognition that every future structural verifier
(anti-reimplementation, MCP-surface-parity, naming-consistency, destructive-
command gating, exit-code policy) will need similar per-command metadata. A
single annotation namespace serves them all.

The cli-printing-press project ([reference](https://github.com/mvanhorn/cli-printing-press))
uses the same convention for `pp:endpoint`, `mcp:hidden`, `mcp:read-only`,
`pp:typed-exit-codes`, `pp:novel-static-reference`. Each annotation drives one
or more verifiers without that verifier having to maintain its own allowlist.

## Guidance

When you need per-command metadata that a tool (lint, MCP exposure, exit-code
verifier, gate) consumes, **prefer Cobra annotations over string-matching
files, comment magic, or separate registries**.

### Namespace conventions

- `lint:*` — for the structural lint suite. Examples:
  - `lint:keep-flag: "name1,name2"` — suppress dead-flag finding for these flags
  - `lint:keep-func` (future) — suppress dead-func finding for the command's RunE
- `mcp:*` — for the MCP server (when added). Examples:
  - `mcp:hidden: "true"` — don't expose this command as an MCP tool
  - `mcp:read-only: "true"` — annotate as readOnlyHint when exposed
- `jamf:*` — for jamf-cli policy. Examples:
  - `jamf:destructive: "true"` — flag for confirmation gating; also drives MCP destructiveHint
  - `jamf:privileges: "Read Computers,Read Mobile Devices"` — comma-joined privilege names from `x-required-privileges` (the human-readable Jamf API privilege names, e.g. `Read Computers`); emitted by the Pro and Platform generators via `opAnnotations`; surfaced in the `commands` catalog as a `privileges` array and, for Pro only, appended to the 403 `permission_denied` hint at runtime (the Platform 403 hint is not wired)
  - `jamf:typed-exit-codes: "0,3"` — declares intentional non-zero success codes

### Reading annotations

Annotations are `map[string]string` on `*cobra.Command`. Values are bare strings;
encode lists as comma-separated. Readers must handle missing keys (zero value =
no policy applied):

```go
keep, ok := cmd.Annotations["lint:keep-flag"]
if !ok {
    return nil // no opt-outs declared
}
allowed := strings.Split(keep, ",")
```

### Writing annotations

Declare them inline at command construction so the policy is visible next to
the command definition, not in a registry elsewhere:

```go
cmd := &cobra.Command{
    Use:   "delete",
    Short: "Delete a computer by ID",
    Annotations: map[string]string{
        "jamf:destructive":      "true",
        "jamf:privileges":       "Delete Computers",
        "jamf:typed-exit-codes": "0,4",  // 4 = not found, treated as success
    },
    RunE: func(cmd *cobra.Command, args []string) error { ... },
}
```

### When the static-analysis tool can't read the annotation

AST readers must handle the case where `Annotations` is set dynamically
(loops, conditionals, function returns). The dead-code lint walks
`*ast.CompositeLit` for the literal `Annotations: map[string]string{...}`
form. If the annotation is set via `cmd.Annotations[...] = ...` after
construction, or returned from a helper, the linter falls back to no
allowlist for that command.

Prefer the inline literal form so the policy is statically visible.

## Why this beats the alternative

**Vs. `//lint:keep` comments:** Comments are positional and brittle — a
refactor that moves a flag binding away from its comment silently disables
the keep. Annotations bind to the command, which is the right granularity.

**Vs. `.linterignore` file:** A central file becomes write-only history. No
reviewer remembers to check it. The annotation lives next to the command it
governs, so reviewers see it in the diff.

**Vs. per-tool allowlists in each verifier:** Three verifiers with three
allowlists is three places to forget to update when a command moves. One
annotation namespace, all verifiers read it.

**Vs. magic strings:** A verifier that string-matches `// HACK keep this` is
indistinguishable from "this comment text accidentally matched." Annotations
are structured data with a defined schema.

## Related

- `scripts/lint-dead-code/scan.go` — first consumer of the `lint:*` namespace
- Future structural verifiers should add their own namespace under `lint:*`,
  `mcp:*`, or `jamf:*` rather than introducing a new mechanism
