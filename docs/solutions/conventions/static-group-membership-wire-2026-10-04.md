---
title: "Static group membership: which endpoint to write through, and what each one does on the wire"
date: 2026-10-04
category: conventions
module: internal/commands
problem_type: wire-behaviour
severity: medium
applies_when:
  - "Adding or changing a command that adds or removes static group members"
  - "Touching pro classic-computer-groups / classic-mobile-device-groups add-members or remove-members"
  - "A generated computer-groups-static-groups or mobile-device-groups-static-groups write answers 500"
  - "A Platform PATCH answers 400 'malformed or the content type is not supported'"
tags:
  - classic
  - static-groups
  - membership
  - platform
  - content-type
---

# Static group membership on the wire

I probed this on 2026-10-04 on Jamf Pro 11.32, both directly (`pro-nmartin`) and through the EU gateway with an environment-scoped credential (`platform-nmjspp`). All probes used throwaway groups.

## The endpoints

| endpoint | many devices per request | identifiers | one unknown entry | already a member / not a member |
|---|---|---|---|---|
| Classic `PUT computergroups/id/{}` with `<computer_additions>` / `<computer_deletions>` | yes; additions and deletions can go in one PUT | `id`, `serial_number`, `udid`, `name` | 409 `Unable to match computer`; whole request rejected, entry not named | re-add: idempotent. Remove a non-member: **409**, request rejected |
| Classic `PUT mobiledevicegroups/id/{}` with `<mobile_device_additions>` / `<mobile_device_deletions>` | yes | same four | 409; whole request rejected | re-add: idempotent. Remove a non-member: silently ignored |
| `PATCH /v2/mobile-device-groups/static-groups/{id}` with `assignments[{mobileDeviceId, selected}]` | yes, incremental | numeric ID only | 400 `INVALID_DEVICE` | ignored silently |
| `PUT /v3/computer-groups/static-groups/{id}` with `assignments[]` | **replaces the whole member list** | numeric ID only | 400 `INVALID_DEVICE` | — |
| Platform `PATCH /device-groups/v1/device-groups/{id}/members` with `{added, removed}` | yes | Pro `managementId` | 400 `INVALID_DEVICE`; also returned for a device of the other type | ignored silently, 204 |

**The commands write through Classic.** It is the only endpoint that takes add/remove deltas for both families in one request. The v3 computer PUT replaces the member list, so an add or remove through it is a read-modify-write that races. Its GET also returns no members, so you cannot even read the list back from v3.

**Every Classic request is atomic.** The server doesn't name the entry that failed, so every entry is resolved before it is sent. Membership is read first, so a device already in the requested state is never sent; removing a non-member would otherwise reject the whole computer request. The group is read back after the write to confirm each change.

**Unmanaged devices are refused.** `Error: The computers with the following IDs are unmanaged and cannot be added to a computer group: 284, 287` (the mobile family says "The devices with …"). The whole request fails, including the managed members in it. Inventory reports managed state (`general.remoteManagement.managed` for computers, `general.managed` on mobile `/detail`), so these devices are held back before sending. If the managed state is absent, the server's list of IDs is parsed and the rest of the chunk is sent once more.

## Generated static-group writes need fields their spec doesn't require

| request | without | answer |
|---|---|---|
| `POST /v3/computer-groups/static-groups` | `assignments` | 500, `errors: []` |
| `PUT /v3/computer-groups/static-groups/{id}` | `assignments` | 500, `errors: []` |
| `POST /v2/mobile-device-groups/static-groups` | `assignments` | 500 |
| `POST /v2/mobile-device-groups/static-groups` | `siteId` | 403 `INVALID_PRIVILEGE` |
| `PATCH /v2/mobile-device-groups/static-groups/{id}` | `groupName`, `siteId` or `assignments` | 500, or 400 "Cannot parse null string" |

On the v2 PATCH, `assignments: []` leaves the members unchanged. `pro_static_group_bodies.go` fills in any of these fields the caller left out, on the six generated leaves. Remove it once the jamf-pro-server spec declares the fields required, or the server stops needing them. The same gap in the SDK's typed methods is jamf/jamfplatform-go-sdk#86.

## Platform PATCH content type

`Transport().DoExpect` sends a PATCH that has no explicit content type as `application/merge-patch+json`. `PATCH /v1/devices/{id}`, `/v1/device-groups/{id}` and `/v1/device-groups/{id}/members` declare `application/json`, and they answer merge-patch with 400 "The request is malformed or the content type is not supported" for every body. The generator now sends each PATCH as its declared content type (`sendsMergePatch`). The hand-written device-group PATCHes name it too (`pdgPatchContentType`). ai-policies stays on merge-patch, because that is the wire-verified form (`platformPatchMergeOnWire`). The transport default is jamf/jamfplatform-go-sdk#85.

## Also seen

- Platform device-group IDs and Pro group IDs refer to the same group. A Platform PATCH shows up in the Classic GET immediately.
- The Classic policy PUT `<policy><general><enabled>false</enabled></general></policy>` changes only `enabled`; a whole-document diff confirmed nothing else moved.
- Through the gateway, modern `POST /v2/mdm/commands` is unserved, while Classic `computercommands/command/{}/id/{}` is served. However, Jamf Pro 11.32 queues only `EnableRemoteDesktop`/`DisableRemoteDesktop` through that Classic route. `BlankPush`, `DeleteUser`, `DeviceLock` and `ScheduleOSUpdate` answer 400 "No command was queued". `UpdateInventory`, `DeviceInformation`, `Settings` and `RedeployJamfManagementFramework` answer 500 through the gateway and 401 direct. So the remote-desktop pair is the only gateway fallback the computer MDM commands can have (`sendClassicComputerCommand`), and `send-command` refuses the rest (`deadClassicComputerCommands`).
