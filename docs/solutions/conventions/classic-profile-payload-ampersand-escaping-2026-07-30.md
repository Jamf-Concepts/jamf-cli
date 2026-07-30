---
title: "Classic API profile payloads: escape every & once inside CDATA, then verify — the server's entity handling is per-payload-type (PI-827)"
date: 2026-07-30
category: conventions
module: generator/classic
problem_type: server-quirk
severity: high
applies_when:
  - "Sending <payloads> content to POST/PUT /JSSResource/osxconfigurationprofiles or mobiledeviceconfigurationprofiles"
  - "A profile upload fails with HTTP 409 'Unable to update the database'"
  - "A stored profile value shows &amp; / &gt; as literal text on a device"
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
failed with an opaque HTTP 409 "Unable to update the database" whenever the
mobileconfig contained any XML entity in a value — e.g.
`<string>R&amp;D</string>`. Since plists must encode `&` and `<`, any
profile with `&` or `<` in a string value could not be uploaded. Backup
round-trips (GET XML piped back into `apply`) failed identically.

## The server model (every clause wire-verified 2026-07-30, EU + US tenants)

1. **Validation**: the server entity-decodes the submitted payload content
   once and rejects the write with 409 when the result contains a bare `&`
   or `<`. Consequence: spec-correct raw CDATA is rejected for ANY plist
   containing `&amp;`/`&lt;`, regardless of payload types. The escaped form
   (every `&` escaped once more) is the only submittable form for such
   profiles.
2. **Storage is per-payload-type** (proven with a mixed profile — one wire
   body, two different treatments):
   - Fragments of payload types the server re-renders —
     `com.apple.ManagedClient.preferences` custom settings (values AND dict
     keys), `com.apple.notificationsettings` — are entity-decoded once.
     The escape stores them **byte-exact**.
   - Fragments of every other payload type (TCC, direct
     `com.apple.loginwindow`, and all payloads on
     `mobiledeviceconfigurationprofiles`) are stored **verbatim**, keeping
     one extra entity layer. Values with `&`/`<` in those types cannot be
     stored faithfully by any client — the device sees `&amp;` where `&`
     was meant. This matches PI-827's history: Jamf's own UI-created
     exports cannot be re-uploaded intact.
3. GET responses are escaped correctly (ingest-only quirk); the server also
   canonicalizes representation (entities for `& < >`, literals for `" '`,
   sorted keys) and trims leading/trailing whitespace inside string values.

Do not re-derive this from theory: a raw-first/retry-on-409 design was
implemented and reverted in this branch's history because probes showed the
409 is validation (fires for all payload types), not a per-type signal —
raw never succeeds for entity-bearing profiles, so retrying costs a round
trip and buys nothing.

## Solution

`normalizeClassicProfilePayloadsForSend` (classic registry template in
`generator/classic/generator.go`), applied as the final body transform in
create/update/apply for both profile resources:

1. Recover the true plist: CDATA content is already true form; text-form
   content (GET/backup XML piped back in) is entity-decoded once.
2. Minimise source escaping (`minimizeClassicPlistSourceEscaping`): decode
   every character reference except those encoding `&`/`<`. Verbatim-stored
   payload types preserve the wire representation with zero decodes, so
   avoidable references (`&#34;` for `"`, `&#xA;` for newlines — exactly
   what howett.net/plist's encoding/xml backend emits when the update path
   re-serialises for UUID injection) would surface as literal text in
   stored values. This also shrinks the unavoidable corruption set to
   genuine `&`/`<` characters.
3. Guard `]]>` → `]]&gt;` (after minimising — decoding `&gt;` can resurrect
   the terminator), then escape every `&` once and wrap in CDATA.

`verifyClassicProfileStored` then GETs the stored payload after every
successful write and compares it against the submitted plist
(`profileconvert.DiffPayloadValues` — parse-based, masks the
`Payload*` metadata keys the server rewrites, trims string edges). When the
server stored values differently (case 2 above), the exact dotted paths are
printed as a stderr warning. Silent corruption is never acceptable;
a warning that names `PayloadContent[3].LoginwindowText` is.

Wrap-time (`injectClassicFileFields`): CMS-signed mobileconfigs are
detected (`profileconvert.IsSignedProfile`) and their inner plist extracted
(`ExtractSignedProfile`, smallstep/pkcs7) with a stderr note — the
signature cannot survive this API anyway. Binary plists (`bplist0`) are
converted to XML (`NormalizeXML`). Embedded CDATA sections are rewritten as
escaped character data by `stripCDATASections` — a **textual** rewrite,
deliberately not a plist re-serialise, because `howett.net/plist` drops
trailing whitespace in CDATA text.

## Verification

Live matrix on both tenants: all five reserved characters in every legal
representation (raw/named/decimal/hex), literal entity text, embedded CDATA
sections, dict keys with entities, CEL expressions, signed profiles, binary
plists, GET→apply round-trips — byte-exact storage for MCX-family content
through create/update/apply, with deterministic warnings for the
server-unfixable combinations (mobile profiles and typed macOS payloads
carrying `&`/`<` in values).

## Scope notes

- `macapplications`/`mobiledeviceapplications` `--appconfig-file` also
  embeds xml-cdata content — the wrap-time handling applies, but the
  escape/verify path does not (untested against the server; probe before
  extending).
- Related: terraform-provider-jamfpro PR #1103 and jamfplatform-go-sdk's
  `PayloadsXMLText` — the SDK needs the same wire form; its acceptance
  matrix in `acc_proclassic_profile_payloads_test.go` encodes the same
  server model.
- Jira: PI-827 (Triaged since 2024), PI-777, PI-690 (closed Not Going To
  Do). The per-payload-type evidence here is a ready-made escalation kit.
