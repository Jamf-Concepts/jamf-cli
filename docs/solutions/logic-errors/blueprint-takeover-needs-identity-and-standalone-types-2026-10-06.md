---
title: "A blueprint made from a Classic profile sat beside it instead of replacing it: takeover needs the original identity and only standalone payload types"
date: 2026-10-06
category: logic-errors
module: internal/profileconvert
problem_type: logic_error
severity: high
applies_when:
  - "Working on import-profile, ApplyTakeoverIdentity, or generatePayloadIdentifier"
  - "A blueprint deployed over an installed Classic profile reports failed:1 or shows up as a second profile"
  - "Deciding what to say to an admin about retiring a Classic profile after migrating it"
tags:
  - blueprints
  - platform-api
  - profileconvert
  - import-profile
  - takeover
  - legacy-profile
  - ddm
  - wire-probed
---

# Takeover needs the original identity and only standalone payload types

## Symptom
`import-profile` gave every payload a generated identifier (`generatePayloadIdentifier`,
a hash of type and index). A blueprint deployed to devices that already had the Classic
profile installed was therefore a second, unrelated profile. Both stayed active.

## What takeover is
Apple's legacy-profile declaration (`com.apple.configuration.legacy`) lets a DDM profile
adopt an MDM-installed one without reinstalling it. The rules, from Apple's
`LegacyProfile` documentation:

1. The DDM profile's `PayloadIdentifier` and `PayloadUUID` match the installed profile's.
2. It has the same number of payloads.
3. Each payload has the same `PayloadType`, `PayloadIdentifier` and `PayloadUUID`, in the
   same order.

A profile that fails any of that is not applied and its status is `invalid`.

## Wire facts (2026-10-06, `platform-nmjspp` + a macOS 26 VM on `nmjspp.jamfcloud.com`)

- **The API keeps the identity it is given.** Top-level `payloadIdentifier` and
  `payloadUUID` on the `com.jamf.ddm-configuration-profile` configuration are stored
  verbatim, even when they differ. Omitting them stores none.
- **A payload's `payloadUUID` is overwritten with its `payloadIdentifier`**, whatever was
  sent. Takeover therefore works only when each installed payload's UUID equals its
  identifier. Jamf Pro's own profiles satisfy this; 5 of 47 profiles on the sandbox did not
  (reverse-DNS or `type.UUID` identifiers).
- **Identity preserved with a changed payload type is worse than no identity.** "Restrictions
  Migration" had 14 payloads, 7 of a type the API rejects standalone. Wrapping those as
  `com.apple.ManagedClient.preferences` kept count and identity but changed the type; the
  blueprint report said `failed:1` and nothing was applied. Without identity the same
  blueprint would have installed alongside.
- **The success case.** "Finder Takeover" (finder + loginwindow, identity kept, nothing
  wrapped) reported `succeeded:1`; the install date on the device did not change (no
  reinstall); System Settings listed it under Device Management > Device Declarations >
  Profiles; and `ProhibitBurn` flipped by a blueprint PATCH reached the device while the
  Classic profile was unchanged.
- **Unknown keys are dropped** and stop being enforced once the blueprint owns the profile
  (`InterfaceLevel` on `com.apple.finder` vanished from the managed preferences).

## Lifecycle

| State | Owner | What happens |
|---|---|---|
| Blueprint deployed, Classic present | Blueprint | Not reinstalled; blueprint edits apply |
| Classic unscoped or deleted, blueprint deployed | Blueprint | Jamf sends Remove Configuration Profile; the device refuses (`not installed by the MDM server`); a Failed command is logged, retried once per clear |
| Blueprint undeployed, Classic present | Classic | Classic reinstalls at the next recon |
| Blueprint undeployed, Classic gone | Nobody | The profile is removed from the device entirely |

## Fallback strategies tested (2026-10-07)

"Restrictions Takeover": 14 payloads, every `PayloadUUID` equal to its `PayloadIdentifier`, 7 of
the types (`com.apple.MCX`, `coremediaio.support`, `dashboard`, `systempreferences`,
`preferences.users`, `systemuiserver`, `ShareKitHelper`) outside the standalone registry. Each
variant was created against the same device group as the installed Classic profile and deployed
to a live macOS VM.

| Variant | Identity kept | Classic installed | Classic unscoped |
|---|---|---|---|
| As-is, 14 payloads | yes | API answers 400 `Failed to validate configuration.` | n/a |
| Unsupported payloads stripped (7 left) | yes | created, `failed:1`, nothing applied, Classic untouched | deploys as a fresh install |
| Unsupported payloads MCX-wrapped (14, types changed) | yes | created, `failed:1`, nothing applied, Classic untouched | deploys as a fresh install |

Neither fallback gets takeover. Stripping breaks the payload count, wrapping breaks the payload
type, and Apple needs both to match. The device logs show no legacy-declaration lines for either
failure (`dmd` redacts its messages), so the rule is inferred from the `failed:1` report and the
Apple documentation, not read from a device error. With the Classic profile gone the same
blueprints deploy, because the identity then points at nothing and the profile installs fresh:
that is a remove-then-install, not a takeover.

So a fallback that keeps identity is strictly worse than one that drops it: while the Classic
profile is installed it fails, where a blueprint with no identity installs alongside. This is why
`import-profile` never preserves identity "where possible". It sends the profile as installed first and falls back to the MCX-wrapped form (no identity) only when the API refuses it, so the decision to give up on takeover comes from the API's answer rather than jamf-cli's type list.

An empty payload (the profile's `com.apple.desktop` here) is a second trap: the converter drops
it, which changes the payload count, and the UI will not deploy a blueprint that carries one.

## The fix
`ApplyTakeoverIdentity` judges the converted output against the original mobileconfig and
stamps identity only when every rule holds; otherwise it returns the configuration
untouched with the reasons. `import-profile` reports the verdict before creating anything,
writes it into the blueprint description, and warns about dropped keys after reading the
blueprint back. Native DDM conversion always refuses takeover; `--legacy` keeps the
payloads as legacy payloads and allows it.

## Do not
- Preserve identity "where possible" on a profile that cannot be adopted.
- Treat the failed Remove command as a fault; it is the device refusing to let MDM remove a
  profile it no longer owns.
- Tell an admin they can retire the Classic profile and then undeploy the blueprint: that
  removes the settings from devices.

## Not verified
Whether a mobile (iOS) profile behaves the same way; the sandbox VM is macOS.
