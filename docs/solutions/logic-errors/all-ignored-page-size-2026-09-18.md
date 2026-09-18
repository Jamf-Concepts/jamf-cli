---
title: "--all ignored --page-size and paged everything at 100, and a short page is not the end of a collection"
date: 2026-09-18
category: logic-errors
module: generator/parser
problem_type: silent-inefficiency
severity: medium
applies_when:
  - "A fetch-everything loop hard-codes a page size instead of reading the endpoint's"
  - "A caller passes --page-size alongside --all and every request still goes out at 100"
  - "A pagination loop terminates on len(results) < pageSize"
  - "A flag is accepted, validated and then not applied, with nothing said"
  - "An integer flag treats 0 as 'not set'"
tags:
  - pagination
  - generator
  - flags
  - silent-failure
  - jamf-pro-api
  - platform-api
issue: 385
---

## Symptom

`pro computer-inventory list --page-size 2000` sent `page-size=100` on every
request. A 9000-computer pull took 95 requests instead of 5, the `page_fetch`
progress events climbed in steps of 100, and **nothing said the flag had been
dropped** — which is most of why the issue was filed. The reporter went looking
for a workaround and found three more things on the way:

- `--page 0` could not ask for the first page. Zero was read as "not set", so
  the command fell through to fetching everything. The only way to get page 0
  alone was `--all=false` with no `--page` at all.
- `--page` is zero-based and had **no help text**, because the published spec
  describes neither pagination parameter. `--page-size 20000 --page 1` reads as
  "everything in one page" and returns rows 2000–3999.
- The Jamf Pro API caps page size at 2000 **without saying so**. 2000, 2500 and
  20000 all returned 2000 rows while `totalCount` reported 9000.

## Cause

Three generators and about thirty hand-written call sites each carried their own
literal `100`:

| Where | What it was |
|---|---|
| `generator/parser/generator.go` — Pro `--all` loop | `pageSize := 100`, `--page-size` filtered out of the page query on purpose |
| `generator/platform/template.go` | `const pageSize = 100` |
| `generator/security/template.go` | `const pageSize = 100` |
| `FetchAllPaginated` call sites | `100` passed at 24 of them |
| `fetchCDPFileCount`, `fetchDeploymentTasks`, the VPP tile | their own `100` |

The Pro loop's page query deliberately strips `page=`/`page-size=` from the
carried-forward query parts, so the flag was not merely unused — it was removed,
and the loop then supplied its own constant. Reading the code, that is obviously
intentional; from the command line it is indistinguishable from a bug.

## The part that is not just a constant

**A page size above the server's ceiling truncates the result and reports
success.** Wire-checked against `/v1/departments` through the platform gateway
on 2026-09-18 with 2601 records:

| requested page-size | rows returned |
|---|---|
| 1000 | 1000 |
| 2000 | 2000 |
| 2001, 2500, 5000, 20000 | 2000 |

The clamp is silent — no 400, no warning, `totalCount` still 2601 — and both
loops terminated on `len(results) < pageSize`. So `--all --page-size 2500` would
have read the first clamped page of 2000 as the last page and answered with 2000
of 2601 records, exit 0. Honouring the caller's `--page-size` under `--all` is
therefore not the safe reading of the flag; it is a way to lose records.

That is why `--all` picks the page size itself, and why the fix is a **per
endpoint ceiling** rather than one number:

| source | value | endpoints |
|---|---|---|
| spec's own `maximum` on the page-size param | whatever it declares | `/v1/users` (1000), `devices/v1` (1000), AI Governance (500) |
| wire-verified Jamf Pro cap for `{totalCount, results}` | 2000 | 122 of the 135 paginated Pro operations |
| the API default, for anything else | 100 | the two raw-array endpoints, and every Platform or Security Cloud service that declares no maximum |

`parser.MaxPageSize` is the generate-time resolver and `MaxPageSizeFor` the
runtime one for the hand-written walks; `TestFetchAllPaginatedNeverOutrunsASpecCeiling`
derives the exception set from the specs so the runtime table cannot go stale
silently.

## Guidance

**Read the ceiling off the endpoint, and treat a declared maximum differently
from an undeclared one.** An undeclared ceiling is enforced by clamping, which
costs a round trip if you guess low and loses records if you guess high. A
declared one tends to be enforced by rejecting the request, which fails the
whole call. `/v1/users` declares 1000 and honours it (wire-checked at 1170
records: `pageSize` echoes 1000 and 1000 rows come back), so it gets 1000 and
not the family's 2000.

**`jamfplatform-go-sdk` is the reference for this, not the spec alone.**
`defaultMaxPageSize` in `tools/generate/util.go` carries the 33-endpoint
verification behind 2000 and says exactly how far it extends: pro +
`totalCount` only, because `devices/v1` was probed in the same session and
behaves completely differently (hard 400 above 1000, not a silent clamp). Every
number in the table above agrees with that SDK's own page sizes except two, and
both are cases where the spec declares a maximum the SDK left conservative.

**End a walk on an EMPTY page, not a short one, wherever the ceiling is not
verified.** `generator/platform/template.go` now does, with a `maxPages`
backstop against a server that ignores `page`. It costs one extra request per
list and cannot be wrong about where the collection ends, whatever page size the
server decided to use. The Pro loop keeps its short-page test because its page
size is now the verified cap, so the clamp it would misread cannot happen.

**A dropped flag has to say so.** `--page-size` under `--all` is ignored, and
`Formatter.NotePageSizeIgnoredByAll` says which page size was used instead and
how to get a single page of the size asked for. Above the ceiling on a single
page it is clamped, and `NotePageSizeClamped` says that too. Both are suppressed
by `--quiet` and deliberately **not** by `--no-hints`: that flag turns off
advisory tips, and a notice that a flag you typed did nothing is not a tip.

**Do not read 0 as "unset" on an integer flag.** `flagAll && flagPage == 0`
made page 0 unreachable. `cmd.Flags().Changed("page")` is the question actually
being asked, and cobra answers it for free.

**A small `--limit` must stay a small request.** The `--all` loop lowers its
page size to `--limit` when the limit is smaller, so `--limit 5` asks for 5 rows
rather than pulling 2000 to return five.

## Still at 100, deliberately

- **`internal/platform/resolve_generic.go`** — the generic Platform name
  resolver walks paths from several namespaces through one loop, so there is no
  single endpoint to read a ceiling from.
- **Platform `blueprints` and `device-groups`** — no declared maximum, and the
  SDK is conservative for both. `pro backup` still spends 18 requests there. A
  wire probe on those two services is the thing that would move them, not
  another reading of the spec.
