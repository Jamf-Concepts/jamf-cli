# Jamf Concepts Compliance Audit Report
## jamfpro-cli

**Audit Date:** 2026-03-25
**Audit Branch:** `concepts-audit-jamfpro-cli-20260325-2132`
**Audited By:** Claude Code (`concepts-audit` skill)
**Repository:** `https://github.com/Jamf-Concepts/jamfpro-cli`
**Visibility:** PRIVATE ✅
**Distribution:** GitHub Releases (binary) + Homebrew tap (`Jamf-Concepts/homebrew-tap`)
**License Intent:** Not yet determined

---

## Executive Summary

`jamfpro-cli` is a well-structured Go CLI tool for Jamf Pro Server API automation. The codebase is large (231 files total, ~196 generated from OpenAPI specs; 66 handwritten files / ~22K lines) with a mature CI/CD pipeline using GoReleaser for binary distribution. The project is in good shape overall — auth handling is solid, destructive operations are gated appropriately, and no real secrets were found. The primary gaps are in repository metadata, dependency automation, and a few security hygiene items addressed by this audit.

**7 findings total: 1 high, 3 medium, 3 low.** All auto-fixable items have been applied on this audit branch.

---

## Findings

### 🔴 HIGH

#### H1 — License file is incorrect for a Concepts project
**File:** `LICENSE`
**Status:** ⚠️ Requires manual action — license intent TBD
**Detail:** The repository contains an MIT license (`Copyright (c) 2025 jamf`). For a Jamf Concepts project where source code remains private, the correct license is either the **Jamf Concepts Use Agreement** or the **Jamf Source Available License** — not MIT. Since license intent was marked as "not yet determined," this cannot be auto-corrected. Once the distribution decision is made, update the LICENSE file accordingly.

**Action:** Replace `LICENSE` with the appropriate Jamf Concepts license before any public-facing distribution. Consult your Jamf Legal contact or the Concepts publishing guide.

---

### 🟡 MEDIUM

#### M1 — `catalog-info.yaml` had invalid Backstage fields
**File:** `catalog-info.yaml`
**Status:** ✅ Fixed on this branch
**Detail:** `spec.owner` was `jamf-pro` (not a valid Crews team) and `spec.system` was `jamf-pro-server` (not a valid Backstage system entity). Both were corrected:
- `spec.owner: jamf-pro` → `spec.owner: concepts`
- `spec.system: jamf-pro-server` → `spec.system: jamf-concept-projects`

#### M2 — No Dependabot configuration
**File:** `.github/dependabot.yml` (new)
**Status:** ✅ Fixed on this branch
**Detail:** No automated dependency update configuration existed. Created `dependabot.yml` covering both `gomod` (Go module dependencies) and `github-actions` (workflow action pins), both on weekly cadence.

#### M3 — Missing `User-Agent` header on outbound API requests
**File:** `internal/client/client.go`
**Status:** ✅ Fixed on this branch
**Detail:** The central HTTP client made all requests without a `User-Agent` header. This means requests are indistinguishable from raw `curl` calls in Jamf Pro server logs, complicating incident investigation and support. Added:
```
User-Agent: jamfpro-cli/1.0 (+https://github.com/Jamf-Concepts/jamfpro-cli)
```

---

### 🔵 LOW

#### L1 — README missing Homebrew installation instructions
**File:** `README.md`
**Status:** ✅ Fixed on this branch
**Detail:** The installation section showed only binary releases and `go install`. Since the project distributes via `Jamf-Concepts/homebrew-tap` (confirmed in `.goreleaser.yaml`), a Homebrew install block was added.

#### L2 — README missing License section and copyright line
**File:** `README.md`
**Status:** ✅ Fixed on this branch
**Detail:** No license notice or copyright statement was present in the README. Added a License section referencing the LICENSE file and including a copyright line.

#### L3 — Audit artifact file patterns missing from `.gitignore`
**File:** `.gitignore`
**Status:** ✅ Fixed on this branch
**Detail:** Running this audit generates `codeql-results-*.sarif` and `gitleaks-results-*.txt` files in the repository root for local reference. These patterns were added to `.gitignore` to prevent them from being accidentally committed.

---

## Security Scan Results

### Secrets (Gitleaks)
**Result:** 10 findings — all false positives
**Details:**
| Finding | Location | Classification |
|---------|----------|----------------|
| Generic API key | OpenAPI spec example values (7 findings) | False positive — example placeholder values in spec YAML |
| Bearer token | Generated scaffold values (2 findings) | False positive — `"your-token-here"` style placeholders |
| Credentials | `docs/plans/2026-02-05-config-setup.md` (deleted) | False positive — `admin:jamf123456` in a deleted planning doc |

**No real credentials found in the repository.**

A `.gitleaksignore` file should be created to suppress these findings in future scans. The patterns from the deleted planning doc can be ignored — the file no longer exists in the working tree.

### Static Analysis (CodeQL for Go)
**Tool:** CodeQL 2.24.2
**Result:** 165 warnings — all in generated code, none in handwritten code
**Details:** All findings are `go/unreachable-statement` warnings in `internal/commands/generated/` files. This is a well-known pattern in generated CLI scaffolding code where placeholder `return nil` statements follow generated error returns. Zero findings in any handwritten Go file.

**No action required** — generated code is exempt from manual review per project policy.

### Code Style (gofmt)
**Result:** ✅ Clean — all Go files pass `gofmt`

---

## Repository Configuration

### Branch Protection
**Status:** ✅ Org-level rulesets apply — verify manually via GitHub UI
GitHub's branch protection API returns 404 when org-level rulesets are in use. Manual verification recommended at: `https://github.com/Jamf-Concepts/jamfpro-cli/settings/rules`

**Required checks to verify:**
- Pull request required before merging (1+ approver)
- Status checks required (CI must pass)
- Enforce for administrators

### Repository Visibility
**Status:** ✅ PRIVATE — correct for a Concepts project with undecided license intent

### Dependabot Vulnerability Alerts
**Status:** Verify manually — API check was inconclusive
Enable at: `https://github.com/Jamf-Concepts/jamfpro-cli/settings/security_analysis`

---

## Code Quality Notes

These are not blocking findings but may be worth addressing in future iterations:

- **`--token`, `--client-secret`, `--client-id` flags accept secrets as CLI arguments** — these appear in process listings (`ps aux`) and shell history. The existing `--token-stdin` and `--client-secret-stdin` alternatives are the safer path. A warning in the help text or README would help guide users away from the insecure flag form. The `setup` command's `--password` flag has the same issue.
- **`overview` command makes ~37 unbounded parallel API calls** — no concurrency cap. On a slow Jamf Pro instance or with rate limiting enabled, this could trigger HTTP 429s. A semaphore or configurable `-concurrency` flag (like `backup` uses) would make it more robust.
- **CI `make test` does not use `-race` flag** — race detection is valuable for concurrent code (overview, backup's parallel fetch). Adding `-race` to the test runner would catch data races in development.

---

## Fixes Applied on This Branch

| Fix | File | Type |
|-----|------|------|
| Corrected `spec.owner` and `spec.system` | `catalog-info.yaml` | Auto-fix |
| Created Dependabot config | `.github/dependabot.yml` | Auto-fix |
| Added `User-Agent` header to HTTP client | `internal/client/client.go` | Auto-fix |
| Added Homebrew install instructions | `README.md` | Auto-fix |
| Added License section + copyright | `README.md` | Auto-fix |
| Added audit artifact patterns to gitignore | `.gitignore` | Auto-fix |

## Manual Actions Required

| Action | Priority | Owner |
|--------|----------|-------|
| Decide on license and replace `LICENSE` file | 🔴 High | Project owner + Legal |
| Enable Dependabot vulnerability alerts in GitHub | 🟡 Medium | Admin |
| Verify branch protection / rulesets are configured | 🟡 Medium | Admin |
| Create `.gitleaksignore` for false-positive suppressions | 🔵 Low | Developer |

---

*Report generated by the `concepts-audit` Claude Code skill on 2026-03-25.*
