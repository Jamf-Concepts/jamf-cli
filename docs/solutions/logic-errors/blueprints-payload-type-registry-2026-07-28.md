---
title: "import-profile failed with an opaque 400: the blueprints configuration-profile component takes a fixed payload-type registry, not any Apple payload"
date: 2026-07-28
category: logic-errors
module: internal/profileconvert
problem_type: bug
severity: high
applies_when:
  - "Working on import-profile, ConvertMobileconfig, ConvertPlist, or the MCX (Custom Settings) split"
  - "A blueprint create/update returns VALIDATION_FAILURE on steps[N].components[M].configuration"
  - "Deciding whether a legacy Apple payload can be sent as a standalone com.jamf.ddm-configuration-profile payload"
tags:
  - blueprints
  - platform-api
  - profileconvert
  - import-profile
  - mcx
  - custom-settings
  - payload-types
  - wire-probed
---

# The blueprints configuration-profile component has a payload-type registry

## Symptom

`pro blueprints import-profile` failed for many real Jamf Pro profiles with a
400 naming only the whole component:

```json
{"httpStatus":400,"errors":[{"code":"VALIDATION_FAILURE",
  "field":"steps[0].components[2].configuration",
  "description":"Failed to validate configuration."}]}
```

No payload, key, or reason. A stock Jamf Pro "Restrictions" profile (14 payloads)
and a stock "Login Window" profile both failed outright — nothing was created.

## Root cause

The code (and `specs/platform/blueprints-api.json`, and therefore the command
help) assumed the API "accepts **any** Apple MDM payload and validates it against
Apple's published schema", with only `com.apple.font` and
`com.apple.webClip.managed` excluded. That is wrong in both directions.

Wire-probed against a live tenant (one bare `{payloadType, payloadIdentifier}`
payload per POST, 136 payload types — every type Apple publishes in
`apple/device-management/mdm/profiles` plus Jamf's `com.apple.MCX.<Name>` family
and Jamf's own spellings). Four distinct outcomes:

| Outcome | Count | API response |
|---|---|---|
| Accepted standalone | 74 | 201 |
| Accepted, needs its required keys | (of the 74) | names the missing field, e.g. `payloadContent[0].moduleName: must not be empty` |
| Explicitly blocked | 22 | `payloadType: Payload disabled: <type>` |
| Not in the registry | 40 | `configuration: Failed to validate configuration.` |

Two findings that drive the design:

1. **The opaque message means "unknown payload type".** It is byte-for-byte what
   an invented type (`com.zzz.invented`) returns. Keys are barely checked by
   comparison — `com.apple.finder` accepts a made-up key, a string where the
   schema wants a boolean, and no keys at all. So a failing import is almost
   always carrying a payload *type* outside the registry, not a bad key. (A
   wrong-typed value on a strict type can also produce it, so probe types bare to
   classify them.)
2. **Well-documented Apple payloads are in the unknown bucket** —
   `com.apple.MCX`, `com.apple.Safari`, `com.apple.SoftwareUpdate`,
   `com.apple.Terminal`, `com.apple.TimeMachine`, `com.apple.systemuiserver`,
   `com.apple.ShareKitHelper`, `com.apple.coremediaio.support`,
   `com.apple.mail.managed`, `.GlobalPreferences`, `com.apple.mcxloginscripts`, …
   Apple having a schema says nothing about the registry.

That second point is exactly what `splitMCXPayloads` keyed on: it unwrapped an MCX
inner domain to a standalone payload when Apple published a schema for it
(`isRealApplePayload`, a live GitHub fetch). For `com.apple.Safari` and friends
that turned an importable profile into a rejected one.

Two smaller causes in the same failure:

- **Jamf Pro writes a non-canonical spelling.** Its User Preferences payload is
  `com.apple.preferences.users`, which is Apple's *filename*; Apple's declared
  `payloadtype` — and the only spelling the registry knows — is
  `com.apple.preference.users` (singular). One character, whole-profile failure.
- `DisabledPayloadTypes` (22 entries) already matched the probed "Payload
  disabled" set exactly, so only the unknown-type bucket was unhandled.

## Fix

`internal/profileconvert/convert.go`:

- `SupportedPayloadTypes` — the probed 74-type registry.
- `canonicalPayloadTypes` / `CanonicalPayloadType` — Jamf → Apple spelling rewrite.
- `wrapAsManagedPreferences` — package a domain's settings as
  `com.apple.ManagedClient.preferences` (`Forced` → `mcx_preference_settings`).

A type outside the registry is **wrapped, not dropped**: probing confirmed the MCX
wrapper is accepted for every domain tried, including third-party ones
(`com.microsoft.Word`), and it is also the correct legacy delivery for these
domains — they are managed preferences, not standalone MDM payloads. Applied in
`ConvertMobileconfig`, `ConvertPlist`, and `ConvertToDDMComponents`, plus
`buildLeftoverEntry` (which generalises the old one-entry
`mcxWrapLeftoverTypes = {com.apple.SoftwareUpdate}` special case).

`splitMCXPayloads` now unwraps only when a converter will consume the domain or
`SupportedPayloadTypes` has it (`unwrappableDomain`). That also removes the live
GitHub fetch from the default import path — classification is deterministic and
offline.

Result on the two failing profiles: Restrictions 400 → created (4 payloads wrapped,
1 spelling rewritten, settings preserved), Login Window 400 → created (3 wrapped).

## Gotchas

- **The API reference is wrong here.** `specs/platform/blueprints-api.json`
  still documents "any Apple MDM payload" and names only two exclusions. Don't
  re-derive behaviour from it; it is vendored, so fix the CLI's own docs instead.
- **`PayloadContent` keeps Apple's capitalisation** inside a wrapped payload. The
  API accepts `payloadContent` too and stores whichever it is given verbatim, so
  the capitalised Apple spelling is the one to emit.
- **Missing a supported type is benign, the reverse is not.** An unknown-to-us
  type that the API actually supports gets MCX-wrapped — still delivered, just as
  managed preferences. So prefer omission over guessing a type into the list.
- **Re-deriving the registry:** probe each candidate type *bare*. Bare removes
  key-validity as a confounder, and a supported-but-needs-keys type identifies
  itself by naming the missing field instead of returning the opaque message.
  Batching many types per POST and bisecting failures does not work reliably —
  duplicate blueprint names across batches fail for an unrelated reason and get
  mis-attributed to a payload type.
- `com.apple.MCX.Accounts`, `.EnergySaver`, `.MobileAccounts`, `.TimeMachine`,
  `.TimeServer` are supported even though Apple declares all of those files as
  payloadtype `com.apple.MCX` (which is *not* supported). Jamf's per-panel split
  is what the registry keys on. `com.apple.MCX.FileVault2` is disabled, and
  `.Dock`, `.Printing`, `.WiFi`, `.Mobility` are unknown.
