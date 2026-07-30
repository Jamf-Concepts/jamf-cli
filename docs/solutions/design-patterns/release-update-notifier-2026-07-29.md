---
title: "Release update notifier: module proxy over GitHub API, notify-only, gate-first"
date: 2026-07-29
category: design-patterns
module: internal/commands
problem_type: design_pattern
severity: low
applies_when:
  - "Adding any background network probe that runs on ordinary command invocations"
  - "Deciding where a CLI should read its own latest published version from"
  - "Adding a self-update or upgrade command"
  - "Adding advisory stderr output that must not pollute machine-readable stdout"
tags:
  - update-notifier
  - version
  - go-module-proxy
  - rate-limits
  - cobra
  - mcp
  - ci
---

# Release update notifier

## Context

jamf-cli ships weekly-ish and installs through four channels (Homebrew tap,
`.pkg`, release tarball, `go install`). None of them tells a user their CLI is
stale, so admins hit "that command doesn't exist" on endpoints a newer build
already generates. `checkTenantVersion` (root.go) already warned about the
*mirror* case — tenant older than the baked-in spec — so the notifier follows
its conventions rather than inventing new ones.

Three decisions are worth remembering.

## 1. Read the latest version from the Go module proxy, not the GitHub API

`api.github.com/repos/.../releases/latest` is capped at **60 requests per hour
per IP, unauthenticated** — a budget every client behind one corporate NAT
shares. For a tool used by admin teams inside a single egress IP, that fails
silently and unpredictably.

`https://proxy.golang.org/github.com/!jamf-!concepts/jamf-cli/@latest` returns
`{"Version":"v1.25.2","Time":"..."}` — CDN-backed, no credentials, no per-IP
budget, and already the canonical source for `go install`. Module paths escape
capitals as `!lowercase`; use `module.EscapePath`, don't hand-write it.

Fallback is `github.com/<repo>/releases/latest`, whose 302 carries the tag. It
is a plain web request rather than an API call, so it isn't rate-limited
either. **Do not add the GitHub API as a source.**

Version comparison uses `golang.org/x/mod/semver` — already in the module
graph, no third-party dependency. `compareProVersions` in root.go is *not*
reusable here: it truncates to three ints and ignores prerelease ordering.

## 2. Notify, never self-update

A Homebrew-managed or `.pkg`-deployed binary lives in a path the invoking user
often cannot write, and swapping a notarized binary out from under Homebrew's
manifest breaks `brew doctor`. So the notice is install-source aware
(`detectInstallKind`) and prints the command that actually works for *that*
install: `brew upgrade jamf-cli`, `go install ...@latest`, or the releases URL.
Detection is path-based on purpose — shelling out to `brew --prefix` would cost
more than the whole check.

Homebrew installs get a 24h grace period after a release: the tap is bumped
after the GitHub release, so an immediate nag points at a version
`brew upgrade` cannot resolve yet.

## 3. Put every suppression rule in one gate, and print in PostRun

`updateCheckGate` holds the policy as data (version shape, opt-outs, quiet,
no-hints, TTY on *both* stdout and stderr, CI markers, skipped commands) so it
is testable without a terminal, a network, or a command tree. Vetoes worth
keeping in mind:

- **`mcp` must be skipped** — it speaks JSON-RPC over stdio.
- **Both stdout and stderr must be TTYs.** Piped stdout means a script is
  consuming output; a redirected stderr means someone is capturing logs.
- **Non-release versions never notify** — `dev`, a `git describe` suffix, a
  `-dirty` tree, and prereleases have no defined upgrade. This is also why
  `main.resolveVersion` falls back to `debug.ReadBuildInfo`: without it every
  `go install` build reports `dev` and silently opts out.

The notice prints in `PersistentPostRunE` (success path only), never in
PreRun: it can then neither interleave with nor delay the command's own output,
and a user dealing with an error doesn't also get told about a release.

The probe runs in a goroutine with a 2s context, at most once per 24h (1h after
a failure — the failure is cached deliberately, so an offline machine stops
paying the timeout on every command). Cache is `.update-cache.json` beside the
config file, 0600, temp-file-then-rename, mirroring `writeVersionCache`.

## Guidance

Adding another background probe to ordinary invocations? Copy this shape:
gate as data → cached with a TTL → background with a hard deadline → advisory
on stderr in PostRun → three opt-outs (flag, env, config key) → every failure
silent.
