---
title: "A timed-out page was re-sent at the same size three times, and a write was re-sent after it reached the server"
date: 2026-09-30
category: logic-errors
module: internal/client
problem_type: retry-policy
severity: high
applies_when:
  - "A retry loop re-sends a request after a timeout"
  - "A retry loop re-sends a POST, PUT or PATCH on a transport error"
  - "A client timeout is at or under the timeout of an edge in front of the server"
  - "A fetch-everything walk has no way to ask for a smaller page"
tags:
  - pagination
  - retries
  - timeouts
  - platform-api
  - jamf-pro-api
issue: 392
---

## Symptom

`pro computer-inventory list --all` on a large fleet failed after about three
minutes with `request failed after 3 retries: ... http2: timeout awaiting
response headers`. The page-size change for issue 385 had moved every
`--all` walk from 100 rows a page to 2000, and a 2000-row page of computer
inventory is more than Jamf Pro can assemble within the timeouts in play.

The reporter measured it on their tenant (default sections, about 29–34KB per
computer):

| page-size | through jamf-cli | through curl, 10 min timeout |
|---|---|---|
| 2000 | timeout | **504 from CloudFront after 90s** |
| 1000 | timeout | 45s, 28.7MB |
| 750 | 28s, 25MB | |
| 500 | 13s, 17MB | |

The server time varies from run to run: the same 1000-row request took 45s
once and 14s on a repeat. So a fixed 60s is within normal variation, not a
safe margin.

## Root causes

Four problems, each of which made the others worse.

1. **The response-header timeout (60s) was under the edge's own limit
   (90s).** CloudFront in front of the platform gateway answers 504 after 90s.
   At 60s the CLI gave up on requests that were about to be answered, and never
   received the 504 that would have told it why.
2. **`doWithRetry` re-sent a timed-out request at the same size.** A timeout
   means the server is still working on the request. The identical request
   times out the same way, so the reporter waited three full timeouts for one
   error.
3. **`doWithRetry` re-sent any request on any transport error, whatever the
   method.** A POST whose connection dropped after it was sent may already have
   been applied. A PUT that timed out may still be running — a smart group
   update recalculates membership inside the request — and a second one lands
   on top of it.
4. **`--all` ignored `--page-size` altogether** (the issue 385 fix), so the
   caller had no remedy. That fix only needed to refuse sizes *above* the
   ceiling: the server clamps those silently and the walk reads the short page
   as the last one. A size below the ceiling cannot be misread that way.

## Fix

- **`httptransport.ResponseHeaderTimeout` is 120s.** It is above the edge's 90s,
  so on the gateway the 504 always arrives first, which carries a status the
  caller can act on. It is not far above it, because it is also how long a
  dead connection hangs. The Platform SDK client's whole-request timeout takes
  the same value. `TestResponseHeaderTimeout_OutlastsTheGatewayEdge` holds it
  above 90s.
- **A timeout and a 504 are one error, `registry.ErrServerTimeout`.** The client
  wraps both, and drops the edge's HTML page from the 504 message because it
  says nothing about the request.
- **Retries depend on whether the request reached the server**, which
  `httptrace.ClientTrace.WroteHeaders` reports, in HTTP/1.1 and HTTP/2 alike:

  | failure | retried |
  |---|---|
  | never sent (dial, TLS, a dead pooled connection) | yes, any method |
  | sent, then timed out | no, any method |
  | sent, then failed otherwise (connection reset) | GET, HEAD and OPTIONS only |
  | 429 | yes, as before |
  | 5xx | no, as before |

  A write that fails after it was sent, and a timed-out write, carry the hint
  that it may have been applied. PUT and DELETE are deliberately not treated as
  retry-safe, although HTTP calls them idempotent: see the smart group case
  above.
- **A timed-out page is asked for again at half the size.** Both walks — the
  generated Pro `--all` loop and `FetchAllPaginated`, which every report and
  audit uses — call `registry.ShrinkAfterTimeout` and resume after the rows
  already held with `registry.PageAt`. The walk resumes by row offset, not by
  page number, because page 3 at 2000 rows is rows 6000–7999 and page 3 at
  1000 is rows 3000–3999. When the new size does not divide the rows already
  held (halving 375 gives 187), `PageAt` also returns how many leading rows of
  the page to skip. The walk stops shrinking at 100 rows and returns the
  timeout. Each shrink prints a line on stderr naming the `--page-size` to
  start at next time, suppressed by `--quiet` only.
- **`--all` honours a `--page-size` below the ceiling**, and clamps one above it
  with `NotePageSizeClamped`, the same as a single page. `NotePageSizeIgnoredByAll`
  is gone.

## What guards it

- `TestDoWithRetry_TimeoutIsNotRetried`, `TestDoWithRetry_HTTP2TimeoutIsAServerTimeout`,
  `TestDoWithRetry_WriteIsNotResentAfterItReachedTheServer`,
  `TestDoWithRetry_UnsentWriteIsRetried` and `TestDo_GatewayTimeout504`
  (`internal/client/retry_policy_test.go`).
- `TestAllPagination_ATimedOutPageIsFetchedAgainAtHalfTheSize`,
  `TestAllPagination_AShrinkMidWalkNeitherRepeatsNorDropsARow` and
  `TestAllPagination_StopsShrinkingAtTheFloor` check the generated loop row by
  row, so a repeated or dropped row fails them. Removing the skip made the
  mid-walk test report 4502 rows of 4500.
- `TestFetchAllPaginated_ShrinksATimedOutPage` checks the hand-written walk.

## Not covered

- **The Platform and Security Cloud generated loops do not shrink.** They go
  through the SDK's own retry client, and their page sizes are already 1000 or
  lower.
- **The shrink is not wire-verified against a real timeout.** The sandbox
  tenants have too few computers to make a page outlast the edge. What was
  checked on the wire (`platform-nmjspp`, 2026-09-30): `--all --page-size 10`
  walks 45 computers in five requests of 10, `--page-size 5000` is clamped to
  2000 with the notice, and `--limit 7` asks for one page of 7.
