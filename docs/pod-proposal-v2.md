# Pod Proposal: jamfpro-cli Production Release (Updated)

> Updated 2026-02-05 to reflect completed work. Items marked with ~~strikethrough~~ are done.

## Problem Statement

Jamf Pro administrators and automated workflows lack a first-party CLI tool, forcing teams to write bespoke curl/Python scripts for every API interaction — scripts that break silently across server versions, cannot be shared between teams, and create an undocumented shadow automation layer that no one can maintain.

## Value Proposition

### For Customers (Jamf Pro Admins)

- **Eliminate scripting expertise requirement.** Admins who manage 10,000 devices should not need to understand OAuth2 token exchange, pagination cursors, or HTTP error codes to export an inventory report. `jamfpro-cli comp list -o csv --out-file fleet.csv` replaces 40 lines of Python.
- **Stable interface across server upgrades.** The CLI is auto-generated from the same OpenAPI specs that define the server API. When a customer upgrades Jamf Pro, the matching CLI version covers the new endpoints without manual script updates.
- **Multi-environment management.** Named profiles (`--profile prod`, `--profile staging`) make it trivial to compare environments, run migrations, or validate staging before production changes.
- **Scriptable with guardrails.** Structured exit codes, `--dry-run`, `--no-input`, and machine-friendly output formats (JSON, CSV, plain) make the CLI safe for both interactive use and CI/CD pipelines.
- **Five output formats.** Table (human), JSON (jq pipelines), CSV (spreadsheets), YAML (config-as-code), and plain (Unix pipes) cover every workflow without third-party tools.

### For AI-Assisted Workflows

CLIs are the natural interface for AI agents and LLM-driven automation. A well-structured CLI unlocks capabilities that GUIs and raw APIs cannot.

- **AI agents can operate Jamf Pro directly.** Tools like Claude Code, Copilot, and custom LLM agents can call `jamfpro-cli` commands as tool invocations. An IT admin can say "find all computers that haven't checked in for 30 days and export them to CSV" and an agent can compose the right `jamfpro-cli comp list -o json | jq ...` pipeline without the admin knowing the API shape.
- **Structured output is LLM-parseable.** JSON and CSV output modes give AI agents clean, deterministic data to reason over — no HTML scraping, no screenshot parsing, no brittle UI automation. An agent can ingest `jamfpro-cli comp list -o json`, analyze the fleet, and generate a summary report in seconds.
- **Exit codes enable agentic error handling.** When an AI agent runs a command and gets exit code 3 (auth error) vs. 4 (not found) vs. 6 (rate limited), it can take different corrective actions — retry with backoff, re-authenticate, or report the specific issue — instead of parsing error strings.
- **`--no-input` and `--quiet` make unattended execution safe.** AI agents cannot respond to interactive prompts. The `--no-input` flag guarantees the CLI never blocks waiting for human input, and `--quiet` suppresses spinner noise that would pollute agent output parsing.
- **Deterministic commands are auditable.** Every action an AI agent takes through the CLI is a reproducible command string that can be logged, reviewed, and replayed. Compare this to an agent clicking through a web UI — no audit trail, no reproducibility, no way to diff what changed.
- **MCP server potential.** The CLI's command structure maps directly to MCP (Model Context Protocol) tool definitions. A thin wrapper could expose every `jamfpro-cli` command as an MCP tool, making Jamf Pro a first-class data source for any MCP-compatible AI agent. Each subcommand becomes a tool; each flag becomes a parameter; JSON output becomes the tool response.
- **Prompt-friendly help text.** The `--help` output for every command, auto-generated from OpenAPI specs, serves as context that can be injected into an LLM prompt. An agent that doesn't know how to list mobile devices can call `jamfpro-cli md list --help`, read the flags, and construct the right invocation — no separate documentation lookup required.

### For Internal Teams

- **Reduce support burden.** Instead of debugging custom scripts per-customer, support can say "run `jamfpro-cli comp list -o json --verbose` and share the output." Deterministic commands with consistent error messages compress troubleshooting time.
- **Accelerate QA and testing.** Internal teams can use the CLI to rapidly validate API behavior during release testing, replacing ad-hoc Postman collections.
- **Dogfooding our own API.** Running the CLI against every release catches API regressions (missing fields, broken pagination, changed response shapes) that unit tests miss.
- **Living documentation.** The CLI's `--help` output, generated directly from OpenAPI specs, serves as always-accurate API reference without maintaining separate docs.

## Success Metric

**Primary:** jamfpro-cli v2.0.0 is published to GitHub Releases and Homebrew, installable by any customer in under 60 seconds, covering all currently-public Jamf Pro API endpoints, with CI/CD that can sync specs from a public release within one working day.

**Secondary:**
- Homebrew install works end-to-end: `brew install ktn-jamf/tap/jamfpro-cli`
- CI pipeline runs tests, builds, and publishes on tag push with zero manual steps
- Spec sync workflow documented and tested against at least one real Jamf Pro release
- At least 3 internal team members have dogfooded the tool against a real Jamf Pro instance

---

## Progress Summary

### Completed

| Item | Files Changed | Notes |
|------|--------------|-------|
| **MIT LICENSE file** | `LICENSE` (new) | `.goreleaser.yaml` references `LICENSE*` — archives now include it. |
| **Basic auth token exchange** | `internal/auth/auth.go`, `internal/config/config.go`, `internal/commands/root.go` | `BasicProvider.GetToken()` wired to existing `BasicAuthExchange()` with token caching. Added `Password` field to config `Profile` struct. Added `--username`/`--password` flags and `JAMF_USERNAME`/`JAMF_PASSWORD` env var fallbacks. Root.go basic auth path no longer errors — creates `BasicProvider`. 4 new tests. |
| **Structured exit codes 2–6** | `internal/exitcode/exitcode.go` (new), `internal/exitcode/exitcode_test.go` (new), `cmd/jamfpro-cli/main.go`, `internal/client/client.go`, `internal/commands/root.go` | New `exitcode` package with `Error` type, `Wrap`/`New`/`CodeFrom`. `main.go` uses `exitcode.CodeFrom(err)` instead of hard-coded `1`. Client maps HTTP 401→3, 403→5, 404→4, 429→6. Root.go maps usage errors→2. Rate-limit retry exhaustion→6. 7 new tests. |
| **Error message polish** | `internal/auth/auth.go`, `internal/config/config.go`, `internal/client/client.go` | All customer-facing errors now include actionable next steps. OAuth2 401 says "invalid client credentials." Rate-limit exhaustion explains throttling. Empty token suggests checking API integration. Missing profile suggests `config add-profile` command. HTTP errors include guidance (check credentials, lacks privileges, wait and retry). |

### Remaining

| Item | Priority | Effort | Notes |
|------|----------|--------|-------|
| Create `ktn-jamf/homebrew-tap` repo | P0 | Small | Empty repo with README. GoReleaser config already references it. |
| GitHub Actions: CI workflow | P0 | Medium | `.github/workflows/ci.yaml` — `make test`, `make lint`, `make build` on PR and push to main. |
| GitHub Actions: Release workflow | P0 | Medium | `.github/workflows/release.yaml` — triggered on `v*` tag push. Runs GoReleaser. Requires `HOMEBREW_TAP_TOKEN` secret. |
| Test full release cycle | P0 | Medium | Push `v2.0.0-rc.1` tag, verify: GitHub Release with all archives, Homebrew formula pushed, `brew install` works. |
| Spec sync GitHub Action | P1 | Medium | `.github/workflows/sync-specs.yaml` with `workflow_dispatch` trigger. See Spec Sync Strategy below. |
| Create `CHANGELOG.md` | P1 | Small | Retroactively document v1.0.0 and v1.1.0 from git log. Establish Keep a Changelog format. |
| README overhaul | P1 | Small | Remove Homebrew TODO marker (line 9). Add badges (CI status, release, Go version). |
| Internal dogfooding | P1 | Medium | 3+ team members install via Homebrew, run common workflows against Jamf Pro sandbox. |
| Write `CONTRIBUTING.md` | P2 | Small | How to add aliases, how spec sync works, how to test locally, release process. |
| Security checklist re-verify | P2 | Small | Review `docs/security-review-2026-02-05.md`. Verify all items still hold with new basic auth and exit code changes. |
| Publish v2.0.0 final | P0 | Small | Tag, push, verify GitHub Release and Homebrew. Depends on all P0 items. |

---

## Updated Implementation Plan

### Phase 1: CI/CD and Release Infrastructure (Days 1–3)

**Goal:** CI/CD pipeline operational, Homebrew tap created, first release candidate tagged.

| Task | Status | Details |
|------|--------|---------|
| ~~Add LICENSE file~~ | **Done** | MIT license at repo root. |
| Create `ktn-jamf/homebrew-tap` repo | TODO | Empty repo with README. GoReleaser config already references it. |
| GitHub Actions: CI workflow | TODO | `.github/workflows/ci.yaml` — `make test`, `make lint`, `make build` on PR and push to main. Matrix: Go 1.24, ubuntu-latest + macos-latest. |
| GitHub Actions: Release workflow | TODO | `.github/workflows/release.yaml` — triggered on `v*` tag push. Runs GoReleaser. Requires `HOMEBREW_TAP_TOKEN` secret. |
| Test full release cycle | TODO | Push `v2.0.0-rc.1` tag, verify: GitHub Release with all 6 archives, Homebrew formula pushed, `brew install` works. |

**Phase 1 Checkpoint:** Working CI/CD that produces a release candidate. Homebrew install tested.

### Phase 2: Spec Sync, CHANGELOG, README (Days 4–7)

**Goal:** Working spec sync workflow. Documentation production-ready.

| Task | Status | Details |
|------|--------|---------|
| ~~Basic auth token exchange~~ | **Done** | `BasicProvider.GetToken()` connected to `BasicAuthExchange()`. `--username`/`--password` flags, env vars, profile support. |
| ~~Structured exit codes~~ | **Done** | New `exitcode` package. Codes 2–6 wired into client HTTP errors, auth errors, and usage validation. `main.go` extracts code. |
| ~~Error message review~~ | **Done** | All non-generated error messages include actionable next steps. Rate-limit exhaustion explained. |
| Spec sync GitHub Action | TODO | `.github/workflows/sync-specs.yaml` with `workflow_dispatch` trigger. See Spec Sync Strategy below. |
| Create `CHANGELOG.md` | TODO | Retroactively document v1.0.0 and v1.1.0 from git log. Establish format. |
| README overhaul | TODO | Remove Homebrew TODO marker. Add badges. |

**Phase 2 Checkpoint:** Spec sync workflow runs. CHANGELOG exists. README is customer-facing quality.

### Phase 3: Polish, Dogfooding, Release (Days 8–15)

| Task | Status | Details |
|------|--------|---------|
| Internal dogfooding | TODO | 3+ team members install via Homebrew, run common workflows against Jamf Pro sandbox. Collect bugs and UX issues. Fix blockers. |
| Write `CONTRIBUTING.md` | TODO | How to add aliases, how spec sync works, how to test locally, release process. |
| Security checklist | TODO | Re-verify `docs/security-review-2026-02-05.md` with new basic auth and exit code changes. |
| Publish `v2.0.0-rc.2` | TODO | Incorporate dogfooding fixes. Full release cycle test. |
| Cut `v2.0.0` final | TODO | Tag, push, verify GitHub Release and Homebrew. |
| Ownership handoff | TODO | Document who owns the repo, approves releases, monitors issues. |

---

## Spec Sync Strategy

### Recommended: Manual Trigger with Version Gate

```yaml
# .github/workflows/sync-specs.yaml
on:
  workflow_dispatch:
    inputs:
      jamf_pro_version:
        description: 'Jamf Pro version to sync from (e.g., 11.15.0)'
        required: true
      server_repo_ref:
        description: 'Git ref in jamf-pro-server repo (tag or commit)'
        required: true
      dry_run:
        description: 'Preview changes without creating PR'
        type: boolean
        default: false
```

**Why manual, not automatic:**
1. A tag in the server repo could fire before the release is actually public.
2. Manual trigger gives a human the final say: "Jamf Pro vX.Y.Z is publicly available, sync the specs."
3. Cost of being 24h late syncing is negligible. Cost of leaking unreleased endpoints is significant.

**Workflow steps:**
1. Checkout jamfpro-cli repo
2. Checkout jamf-pro-server at specified `server_repo_ref` (requires cross-repo PAT)
3. Run `make sync-specs JAMF_SERVER_PATH=./jamf-pro-server`
4. Run `make test` to verify generated code compiles
5. If `dry_run` — output diff summary and stop
6. Otherwise, create PR: `feat: sync specs with Jamf Pro vX.Y.Z`
7. PR requires manual review and merge
8. After merge, manually tag and release the CLI

**Safety rails:** PR-based review (never direct push), explicit `server_repo_ref` input (no "latest main"), `dry_run` mode for pre-release validation.

---

## Risks

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| Homebrew tap token misconfigured | Medium | Medium | Test with rc.1 tag before real release |
| Spec sync ships unreleased endpoints | Low | High | Manual trigger, PR review, dry-run mode |
| Generated commands incorrect for edge-case APIs | Medium | Medium | Dogfooding against real instance; file issues for future fixes |
| GoReleaser v2 breaks in CI (works locally) | Medium | Low | Test with `release-snapshot` first; pin GoReleaser version |
| No integration tests = regressions slip through | Medium | Medium | Mitigate with dogfooding; follow-up pod for test framework |
| Cross-repo PAT for spec sync hard to manage | Medium | Low | Fine-grained PAT scoped read-only; document rotation process |

---

## Things Still Worth Noting

These items from the original proposal are **not yet addressed** and should be tracked:

1. **`config setup` browser open is macOS-only.** Uses `open` command. Linux users get no browser opened. Worth a note in docs or a `xdg-open` fallback.

2. **Pagination fetches ALL pages by default.** `--all=true` is the default in generated list commands. For large fleets (50,000+ devices), this could be very slow. Document prominently and/or add progress indicator.

3. **No version compatibility matrix.** CLI v2.0.0 generated from specific specs may not work against older Jamf Pro versions. No version check, no warning, no documentation of which CLI maps to which server version.

4. **`--dry-run` flag is declared but not implemented.** The global flag exists in `root.go` but no generated command reads it. Customers using `--dry-run` before destructive operations get false safety — the operation executes.

5. **Plaintext secrets in config.** A customer writing `client-secret: my-secret` in config.yaml has credentials in plaintext on disk. Docs should recommend `env:` or `file:` prefixes and warn about plaintext.

6. **No `version --check` or update command.** Binary download users have no way to check for newer versions.

7. **Shell completions not tested in CI.** GoReleaser generates bash/zsh/fish/powershell completions but nothing verifies they're valid syntax.

8. **Awkward generated command names.** Names like `computer-prestages-v-3s`, `mobile-device-prestage-scope-v-2s`, and `slasas` come from OpenAPI spec naming. Consider aliases for common ones.

---

## Critical Files

| File | Relevance |
|------|-----------|
| `.goreleaser.yaml` | Homebrew tap config (lines 63–80), release artifacts, needs matching CI workflow |
| ~~`internal/auth/auth.go:63`~~ | ~~Basic auth TODO~~ — **Implemented.** `BasicProvider.GetToken()` calls `BasicAuthExchange()` with token caching. |
| `internal/commands/root.go` | Global flags, exit code handling. Now includes `--username`/`--password`, `exitcode` integration. |
| `cmd/jamfpro-cli/main.go` | Entry point. Now uses `exitcode.CodeFrom(err)` for structured exit codes. |
| `internal/exitcode/exitcode.go` | **New.** Exit code constants, `Error` type, `CodeFrom()` extractor. |
| `internal/client/client.go` | HTTP client. Now maps HTTP status codes to exit codes and returns actionable error messages. |
| `internal/config/config.go` | Config management. Added `Password` field to `Profile` struct. |
| `Makefile` (lines 43–59) | `sync-specs` target the GitHub Action will invoke |
| `docs/security-review-2026-02-05.md` | Existing security review to re-verify with new basic auth changes |
| `LICENSE` | **New.** MIT license, now included in release archives. |

---

## Day 15 Ownership

| Responsibility | Owner | Cadence |
|---------------|-------|---------|
| Merge PRs, cut releases | Pod lead | Per Jamf Pro release |
| Run spec sync workflow | Pod lead | After each public Jamf Pro release |
| Bug triage | Pod lead | As reported; route to API team if server-side |
| CI/CD maintenance | Pod lead | As needed |
| Go dependency updates | Pod lead | Monthly (`go mod tidy`) |

---

## Readiness Assessment (Updated)

| # | Criterion | Score | Notes |
|---|-----------|-------|-------|
| 1 | Problem Framing | 5/5 | Unchanged. |
| 2 | 80/20 Check | 5/5 | Hard code changes (auth, exit codes, errors) are done. Remaining work is infrastructure (CI/CD, Homebrew tap) and docs. |
| 3 | Success Metric | 5/5 | Unchanged. |
| 4 | Day 15 Ownership | 3/5 | Unchanged. Single point of failure risk remains. |
| 5 | Orchestration Trajectory | 5/5 | Code-level risks eliminated. Remaining work is well-understood infrastructure. |
| 6 | Structure Resistance | 5/5 | All code changes leverage existing patterns (auth provider interface, Cobra flags, error wrapping). |
| 7 | Staffing | 4/5 | Improved — only infrastructure tasks remain, lower skill floor required. |
| 8 | Tools Readiness | 5/5 | Unchanged. |

**Overall: 37/40** (up from 33/40) — Code risks resolved. Remaining work is CI/CD plumbing and docs.
