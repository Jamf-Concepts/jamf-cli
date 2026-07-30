---
title: "Classic API profile payloads must have every & pre-escaped once inside the CDATA block (PI-827 workaround)"
date: 2026-07-30
category: conventions
module: generator/classic
problem_type: server-quirk
severity: high
applies_when:
  - "Sending <payloads> content to POST/PUT /JSSResource/osxconfigurationprofiles or mobiledeviceconfigurationprofiles"
  - "A profile upload fails with HTTP 409 'Unable to update the database'"
  - "Adding a new xml-cdata file field to the classic generator"
tags:
  - classic-api
  - config-profiles
  - cdata
  - xml-escaping
  - PI-827
---

## Problem

`pro classic-macos-config-profiles apply --mobileconfig-file X.mobileconfig`
failed with HTTP 409 "Unable to update the database" whenever the
mobileconfig contained any XML entity in a value — e.g. `<string>R&amp;D</string>`.
Since plists must encode `&` and `<` in values, any profile with `&` or `<`
in a string value failed to upload, even though the CLI's CDATA-wrapped
request body is spec-correct XML.

## Root cause

Jamf Pro's Classic API entity-decodes `<payloads>` content one extra time on
ingest — including CDATA content, which a compliant parser must treat as raw
text. `&amp;` arrives at the database as a bare `&`, breaking the server's
own re-parse of the plist. Known server bugs, all unfixed as of 11.30:
[PI-827](https://jamf.atlassian.net/browse/PI-827) (the exact issue,
Triaged/To Do since 2024), [PI-777](https://jamf.atlassian.net/browse/PI-777)
(same family), [PI-690](https://jamf.atlassian.net/browse/PI-690) (closed
Not Going To Do — Jamf treats entity-encoded plists as working as intended).
Don't expect a server-side fix; double-encoding is the de-facto contract.

## Solution

The full server model (all empirically verified): the Classic API entity-decodes
`<payloads>` content **once for CDATA** (spec says zero) and **twice for
text-form** (spec says once — the parser's decode plus the same rogue pass).
So text-form bodies — exactly what a GET/backup response contains — also fail
on any `&`-bearing profile when piped back in.

`normalizeClassicProfilePayloadsForSend` (classic registry template in
`generator/classic/generator.go`) funnels every input form into the one wire
format the server stores correctly, applied as the **final** body
transformation before `Client.Do` in create/update/apply (gated on
`.IsConfigProfile`):

1. Recover the true plist: CDATA content is already true form; text-form
   content is entity-decoded once (`encoding/xml` chardata parse — handles
   named, decimal, and hex refs).
2. Guard `]]>` → `]]&gt;` (backstop; see wrap-time handling below).
3. Escape every `&` once and wrap in CDATA. This is exactly invertible by
   the server's single decode pass regardless of which entities the payload
   contains (`&amp;` → `&`, `&amp;lt;` → `&lt;`, `&amp;#65;` → `&#65;`).

Ordering matters: the normalize must run *after* `injectClassicProfilePayloadUUIDs`,
which plist-parses the CDATA content and needs it in true (un-double-escaped)
form. Everything internal operates on the true plist; only the wire form is
escaped.

Wrap-time (`injectClassicFileFields`): binary plists (`bplist0` prefix) are
converted to XML via `profileconvert.NormalizeXML`; files containing `]]>`
get embedded CDATA sections rewritten as escaped character data by
`stripCDATASections` — a **textual** rewrite, deliberately not a plist
parse/re-serialise round trip, because `howett.net/plist` is not byte-faithful
for CDATA text (drops trailing whitespace). Any stray `]]>` remaining from
malformed input is entity-guarded so the emitted document stays well-formed
(XML 1.0 §2.4/§2.7). `replaceClassicProfilePayload` carries the same guard.

Verified live (create + update paths) with an entity-torture profile
(`&`, `<`, `>`, `"`, `'`, literal `&amp;` text, numeric refs, unicode):
stored values round-tripped byte-identical, confirmed via GET.

Also verified: *raw* `>` and `'` in plist text (legal XML, common in
hand-written mobileconfigs, e.g. CEL expressions like
`target.signing_time >= timestamp('...')`) pass through unchanged — the
server's decode pass only affects entities, so `&` is the only character
that needs pre-escaping. The server model this implies: Jamf extracts
payloads content as raw text (CDATA markers stripped, no parser-level
entity resolution) and entity-decodes exactly once — for text-form bodies
that coincides with spec-correct XML (cf. terraform-provider-jamfpro
PR #1103, whose single-`encoding/xml`-escape fix is correct under both
interpretations), for CDATA it's the PI-827 bug.

## Verification

All live-verified against Jamf Pro 11.x via the platform gateway: every
reserved character (`&` `<` `>` `"` `'`) in every legal representation (raw
where the spec allows, named entity, decimal ref, hex ref), literal entity
text (`&amp;amp;`), embedded CDATA sections, unicode, and a Santa CEL
expression (`target.signing_time >= timestamp('...') && ...`) — byte-exact
storage through create, update, and the GET→apply text-form round trip.

## Scope notes

- GET responses are *not* double-encoded — the server escapes correctly on
  output. The quirk is ingest-only, so no decode is needed when reading.
- The server trims leading/trailing whitespace in plist string values on
  ingest (verified with entity-free values — it is the server's own plist
  re-serialisation, unrelated to escaping, and no client can prevent it).
  Internal whitespace survives.
- `macapplications`/`mobiledeviceapplications` `--appconfig-file` also embeds
  xml-cdata content (`app_configuration/preferences`) and may have the same
  server quirk — untested, not covered by the send-time normalizer yet
  (the wrap-time `]]>`/binary handling does apply to it). Test before
  extending `normalizeClassicProfilePayloadsForSend` to it.
- Related external context: terraform-provider-jamfpro PR #1103 fixed the
  provider's own double-escaping of text-form payloads; its device-verified
  evidence corroborates the decode arithmetic above.
