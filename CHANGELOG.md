# Changelog

Per-release notes — the full list of merged pull requests — are generated on
[GitHub Releases](https://github.com/Jamf-Concepts/jamf-cli/releases). This file exists for
what that list cannot say: which changes are **breaking**, what the migration is, and why.
So it records breaking changes, behaviour changes visible to a script, and removals. A
release with none of those gets no entry here.

Versions follow the `vMAJOR.MINOR.PATCH` tags in this repository, and headings match the
commit types the repo already uses (`feat!`/`build!` for a breaking change).

## Unreleased

### Breaking — `pro setup --scope standard` no longer grants privileges that change privileges or logins

The `standard` tier withheld deletes, wipes, locks and audit flushes, and
granted every other privilege. That included `Create` and `Update` on API
Roles, API Integrations and Accounts. So a `standard` client could rewrite its
own role to hold every privilege the tier withholds, attach a broader role, or
create an administrator account.

The tier now also withholds these privileges:

- `Create` and `Update` on API Roles, API Integrations, Accounts and Account
  Groups, and LDAP Servers (which also governs cloud identity providers).
- `Update SSO Settings` and `Update SMTP Server`.
- Unmanaging a computer or a mobile device (`Send Computer Unmanage Command`,
  `Unmanage Mobile Devices`), and the delete-user device command.
- `Update Retention Policy`, which alone is enough to queue a log flushing
  task (`pro log-flushing-task create`).

Their `Read` privileges stay. A `jamf-cli-standard` role that an earlier setup
created keeps its old privileges. To rewrite it, run
`pro setup --credentials create --scope standard` again. Every
`jamf-cli [<user>]` integration on the instance shares that one role, so the
re-run narrows it for all of them. The re-run changes only that role: review
the instance's API roles, API integrations and accounts for any that a
`standard` client created. To manage roles, accounts or sign-in from the CLI,
use `--scope full-admin`.

### Behaviour — Classic profile and app writes edit the element the server reads

`--mobileconfig-file`, `--custom-payload-file`, `--appconfig-file` and
`apply --name` put the operator's value into the `--from-file` document. They
used to find the target by the first matching text. So a `<payloads>` or
`<name>` inside a comment, inside CDATA, or under another parent (such as
`<general><category><name>`) took the value, and Jamf Pro stored the
document's own element. A shared or vendor document could keep its own payload,
or make `apply --name` overwrite a different existing profile.

These commands now parse the document and edit only `<general><payloads>`,
`<app_configuration><preferences>` or `<general><name>`. The rest of the
document is sent byte for byte. `apply --name` looks the record up by the name
it was given, not by a `<name>` elsewhere in the document.

Visible changes, all before any write is sent, and the same under `--dry-run`:

- Every configuration-profile `create`, `update` and `apply`, and an app write
  with `--appconfig-file`, fails on a document the XML parser cannot read, with
  the parser's error. That includes a document that declares an encoding other
  than UTF-8.
- Those writes refuse a document with more than one `<general><payloads>` or
  `<app_configuration><preferences>`, because the CLI cannot know which one the
  server reads. A profile `update`, an `apply` that replaces a profile, and
  `apply --name` also refuse a second `<general>`, and `apply --name` a second
  `<general><name>`.
- Every Classic `apply` refuses a document that carries two different names,
  root-level `<name>` or `<general><name>`, because the record lookup and the
  server could read different ones.

### Breaking — MCP `run_command` refuses the writes that change who can log in to a Jamf product

`run_command` refused `pro accounts create`, `update` and `apply`, because they
set a login password. Other commands grant a Jamf Pro login with no password
in the request, and they ran. A model could create a Classic administrator
account, bind an account group to an LDAP server it runs, point SSO at its own
identity provider, or send password-reset mail to its own SMTP server. Each
login works outside the MCP server.

These now fail over MCP, with the reason in the error:

- `create`, `update` and, where it exists, `apply` on
  `pro classic-account-users`, `classic-account-groups`,
  `classic-ldap-servers`, `cloud-ldap` and `cloud-azure`, and
  `pro cloud-ldap update-mappings`.
- `pro classic-smtp-server update` and `pro smtp-server update`.
- `pro sso-settings update`, `disable`, `cert create`, `cert update` and
  `oidc-broker-config update`, including the `sso-settings-cert` spelling.
- `platform sso-connections create` and `update`.
- `apply` on `protect users`, `groups` and `roles`, which create a Jamf
  Protect console login or change its role, and on `school users` and
  `groups`, which set a Jamf School login password or the group ACL that
  grants the teacher and parent app roles. These read their body from a file,
  so they ran only with that file inside `--input-dir`.
- `protect restore`, which applies a backup directory's roles, groups and
  users. It is refused whole, as `protect backup` already was, because
  `--resources` and `--exclude` are the model's to choose.

The reads, `delete`, history notes and connection tests on these resources
still run. Outside MCP, nothing changes.

### Behaviour — `pro blueprints import-profile` takes over the installed profile where it can

`import-profile` used to give every blueprint fresh, generated payload
identifiers, so a blueprint made from a profile already installed on devices
was a second profile beside the first, never a replacement for it. It now
carries the profile's own identifiers and UUIDs onto the blueprint whenever
that lets the blueprint adopt the installed copy (Apple's legacy-profile
declaration) without reinstalling it. Nothing needs a flag.

What a script can see change:

- The blueprint **description** is set to one short line: `Imported from
  computer profile "<name>" (ID <id>) by jamf-cli. Takeover supported.` or
  `… Takeover not supported: <first reason> (+N more).`
- **The profile is now kept as installed by default**, and `--legacy` is
  removed. Native DDM conversion (passcode, Safari, software update) and the
  unwrapping of Application & Custom Settings (MCX) payloads, which both change a
  profile's payloads and so end takeover, moved behind the new `--convert` flag.
  Scripts that passed `--legacy` must drop it; scripts that relied on native
  components must add `--convert`.
- The converted blueprint (what `--convert` produces) is also the fallback. The
  profile as installed is sent first. If the API refuses it (HTTP 400 `Failed to
  validate configuration.` on a component, and nothing else), or takeover is not
  possible for it as installed anyway (a payload whose `PayloadUUID` differs from
  its `PayloadIdentifier`), the converted profile is sent instead, with the
  payload types the API does not take standalone delivered as Custom Settings
  (MCX), and no takeover. If the API accepts a type jamf-cli lists as
  unsupported, takeover works for that profile and the command notes that
  `profileconvert.SupportedPayloadTypes` is out of date. Any other failure is
  returned without a retry. So the same profile can produce a different
  blueprint on a different server version.
- Payloads are validated against Apple's schema in both forms: a payload missing
  a required key (a screensaver with no `moduleName`) is dropped with a message,
  where it used to make the API refuse the whole blueprint.
- A line on stderr before anything is created says either `Takeover supported`
  or `Warning: takeover is not supported`, with each reason. Takeover is
  unavailable when a payload has to be wrapped as MCX, skipped, removed as
  empty or unwrapped; when payloads were promoted to native DDM components
  (`--convert`); when a payload's `PayloadUUID` differs from its
  `PayloadIdentifier` (the blueprints API rewrites one to match the other, and
  a deployment that tried it on a device reported `failed`); or when the profile
  has no identifiers.
- `--all` imports every configuration profile, computer and mobile, unless `--type`
  narrows it to one, and ends with a table of the outcome for each: created, skipped or
  failed. A skip is a profile the command was told to leave out (`--takeover-only`,
  `--skip-exclusions`, `--skip-limitations`), one with no device-group scope, or one
  whose payloads are all types blueprints disables; skips do not fail the run, a
  failure beside a success exits 7. `--deploy` deploys each blueprint after it is
  created (blueprints are created undeployed by default), except one that would reach
  devices the profile excludes, which is created and left undeployed. A blueprint that
  installs beside the Classic profile asks for confirmation first; `--all --deploy` asks
  once, after counting how many take over, how many take over only if the API accepts
  payload types jamf-cli lists as unsupported, how many install alongside and how many
  are held back (`--yes` skips the question). `-n` previews the table without creating
  anything. `import-profile --all --type computer --takeover-only --skip-exclusions
  --skip-limitations --deploy` moves only the profiles that can take over.
- `--takeover-only` imports a profile only if its blueprint can take over the
  installed profile. Otherwise no blueprint is created and the command exits with
  an error: the reasons when takeover is impossible offline, or the API's
  rejection when it refuses the profile as installed (the converted profile is
  never sent). It cannot be combined with `--convert`. Use it to sort a set of
  profiles into those that can move to blueprints in place and those that cannot.
- A profile's scope **exclusions** that name computer groups or mobile device
  groups are carried over as an activation condition on the blueprint's step,
  `NONE @property(jamf.device.groups) IN {...}`: a device in any excluded group
  keeps the components inactive. This follows how a profile's exclusion of several
  groups works. Other exclusions (individual devices, buildings, departments,
  users, network segments) and all limitations cannot be expressed. They are
  dropped with a warning, and the blueprint description says what was carried
  over and what was not. `--skip-exclusions` and `--skip-limitations` turn that
  warning into a refusal: the profile is not imported. Neither applies with
  `--computer-group` or `--mobile-device-group`, which replace the profile's
  scope. Group membership reaches the device through Jamf, so a change in
  membership activates or deactivates the components after a short delay.
- Identifiers are only carried over when takeover is supported. Carrying them
  over with a payload changed made the device reject the declaration as
  invalid, and nothing was applied at all.
- After creating the blueprint the command reads it back and warns about keys
  the API dropped because Apple's schema does not define them. Once a blueprint
  has taken over a profile those keys stop being enforced on devices.

Operating a takeover blueprint: while it is deployed it owns the profile on the
device, so editing the blueprint changes the profile. Removing or unscoping the
Classic profile leaves it in place, and Jamf Pro logs a failed "Remove
Configuration Profile" command, which is expected. Undeploying returns the
profile to the Classic definition at the next recon, or removes it from the
device if the Classic profile no longer exists.
When the blueprint carries excluded groups, remove the same exclusions from the Classic
profile: while both exclude a group, Jamf Pro removes and reinstalls the Classic profile
out-of-band and the blueprint can be left failed. With the exclusion on the blueprint
alone, a device that leaves the group gets the profile back from the blueprint within
seconds. A takeover blueprint whose device leaves its target or joins an excluded group can stay
failed once the device returns. Undeploying it and deploying it again clears that: the
undeploy removes the profile from the device and the deploy installs it from the blueprint.

### Added — generated Jamf Pro writes take a body `--from-file`

Every generated Pro `create`, `update` and body-carrying action read its body
from stdin only, so `pro categories create --from-file body.json` answered
`unknown flag`. They now take `--from-file`, as the Classic, Platform and
Security Cloud writes already did; stdin works as before. On `update`,
`--from-file` and `--set` are mutually exclusive. Not changed: `patch` (its
`--from-file` already was the body), the destructive commands whose
`--from-file` is a list of IDs or names, and the multipart uploads (`--file`).

### Deprecated — `pro bulk` group and policy subcommands move to the resource they act on

| deprecated | use instead |
|---|---|
| `pro bulk add-to-group` | `pro classic-computer-groups add-members` |
| `pro bulk remove-from-group` | `pro classic-computer-groups remove-members` |
| — | `pro classic-mobile-device-groups add-members` / `remove-members` (new) |
| `pro bulk enable-policies` | `pro classic-policies enable` |
| `pro bulk disable-policies` | `pro classic-policies disable` |
| `pro bulk send-command` | the MDM commands under `pro computer-inventory` |

The deprecated commands still run, are hidden from help, and print a
deprecation warning on stderr. They will be removed in a future release, and
`pro bulk` with them.

**`send-command` now refuses ten of its twelve commands before sending
anything.** Jamf Pro 11.32 no longer queues them through the Classic API.
Wire-checked direct and through the gateway: `BlankPush`, `DeleteUser`,
`DeviceLock` and `ScheduleOSUpdate` answer `400 No command was queued`;
`UpdateInventory`, `DeviceInformation`, `Settings` and
`RedeployJamfManagementFramework` answer 500 through the gateway and 401
direct; `UnlockUserAccount` needs a `user_name` that send-command cannot pass.
`EraseDevice` is no longer documented. The refusal exits 8 and names the
replacement where one exists. `EnableRemoteDesktop` and `DisableRemoteDesktop`
still work. `--confirm-destructive` is accepted and has no effect, and
send-command now honours `-n`.

**Six MDM commands now work through a platform gateway profile**, where the
modern MDM endpoint they use is not published:

- `pro computer-inventory enable-remote-desktop` / `disable-remote-desktop`
  send the Classic `EnableRemoteDesktop` / `DisableRemoteDesktop` command.
  That was the last thing `send-command` did that nothing else could.
- `pro computer-inventory restart` / `shutdown` and `pro mobile-devices
  restart` / `shutdown` send the Platform API device action, by management
  ID. `restart --rebuild-kernel-cache` is refused there (exit 8), because the
  Platform action takes no options.

The other modern MDM commands (`lock`, lost mode, `settings`, …) are still
refused on a gateway profile, because no Classic or Platform command does the
same.

What differs in the new commands:

- **The group is the positional `<id>` or `--name`**; the members come from
  `--computer` / `--mobile-device` (repeatable), `--from-file` or `--from-group`
  (was `--target-group` and `--group`).
- **One request per 250 members** rather than one per member, read back
  afterwards to confirm each change landed. A device already in the requested
  state is reported and not sent. Removing a computer that is not a member
  used to fail the whole request with a 409.
- **An unmanaged device is refused before anything is sent.** Jamf Pro rejects
  it as a static group member, and because the request is atomic it fails
  every managed member sent with it.
- **A single `--computer`, `<id>` or `--name` runs without `--yes`**, the way
  `update --set` does. `--from-file`, `--from-group` and the policy filters
  still preview until `--yes` is given; `-n` previews every form.
- **A change that cannot be confirmed is a failure.** When the group cannot be
  read back after the write, each change it carried is reported `unverified`
  and the command exits non-zero, rather than reporting it done.
- **A policy filter run counts a policy it could not read as a failure** (exit
  7, or 1 when nothing else succeeded). `pro bulk enable-policies` /
  `disable-policies` used to warn and exit 0, though the policy might have
  matched. A filter matching nothing now says so.
- `pro bulk enable-policies` / `disable-policies` now honour `-n`; before, `-n`
  together with `--yes` wrote the change.

### Behaviour — device actions exit 7 for a partial failure

The `computer-inventory` and `mobile-devices` actions (`lock`, `restart`,
`blank-push`, `erase`, …) exited 1 when some devices succeeded and some failed,
or when `--from-file` lines did not resolve: the same code as a total failure.
They also ignored `--allow-partial-failure`. They now exit 7 for a partial
failure, as the group, policy and `bulk` commands do, and
`--allow-partial-failure` turns that into a warning with exit 0. A total
failure keeps the exit code of the error that caused it.

### Behaviour — device `--from-file` lists take UDIDs, management IDs and names

Every `--from-file` device list (the `computer-inventory` and `mobile-devices`
actions, `bulk send-command`, and the new group commands) now resolves each
line as a numeric ID, a UUID (a UDID or a management ID), a mobile UDID, a
serial number or a name. A UUID used to be looked up as a serial number and
never matched. **A line that matches no serial number is now tried as a name**,
so an entry that was skipped before can now resolve to a device, and stderr
notes each one that did. A line matching more than one device is refused,
naming the matches. Names are looked up in batches, spaces and punctuation
included: one request per 100 names rather than one per name.

### Fixed — PATCH commands the server refused for every body

`pro platform-devices patch`, `pro platform-device-groups patch` and
`patch-members` sent `application/merge-patch+json` to endpoints that accept
only the `application/json` their spec declares, and answered 400 for every
input. `pro platform-device-groups add-members` / `remove-members` now report
how many devices actually changed (they used to echo the number of IDs given)
and preview under `-n`. An `--id` that is not a UUID (a numeric Jamf Pro ID,
say) is refused before anything is sent.

The generated Jamf Pro static-group writes failed for bodies their help
documents: `pro computer-groups-static-groups create`/`update`/`apply` without
`assignments` returned 500, as did `pro mobile-device-groups-static-groups
create`/`patch`/`apply` without `groupName`, `siteId` and `assignments`. These
fields are now filled in when left out. An `update` that names no members keeps
the current ones, because that PUT replaces the member list.

## v1.32.0

### Breaking — `scope add`/`remove` resolve `--computer` and `--mobile-device` to an ID first

`scope add --computer <UDID>` answered `409 Unable to match computer` on every
computer-scoped resource: a Mac's UUID-shaped UDID was sent as `<name>`, and a
matched UDID went out beside an empty `<name>`, which the Classic computer
matcher reads first and refuses. The `scope add` and `scope remove` commands
of the eight scopeable Classic resources now look a `--computer` or
`--mobile-device` value up in inventory — as an ID, name, UDID or serial
number — and send the device's `<id>` alone. That is the one form both Classic
matchers resolve unconditionally.

Two things a script can see change:

- **A name shared by more than one device is refused**, naming the matching
  ids. The Classic API's own name match picked one of them silently.
- **The API client needs Read Computers or Read Mobile Devices**, because the
  lookup reads inventory. Without it, a numeric ID that worked before now
  fails with a 403 (exit 5), and the hint names the privilege.

`scope remove` by serial number now works; before, it could never match,
because the scope GET carries no serial. A value that no longer names any
device still removes a member listed under that ID or name.

**Migration.** Pass the numeric ID for a device whose name is not unique, and
grant the API client Read Computers / Read Mobile Devices.

### Breaking — device secrets are read from a file, not a flag value

Three flags took a device secret as their value, where `ps`, shell history and
a CI log could read it. Each is removed and replaced by a file flag on the same
command, with no alias. The old name now fails with `unknown flag` and a hint
naming the new one.

| command | removed | use instead |
|---|---|---|
| `pro computer-inventory set-recovery-lock` | `--new-password <pw>` | `--new-password-file <path>`, or the no-echo prompt |
| `pro computer-inventory set-recovery-lock` | omitting `--new-password` to clear | `--clear` |
| `pro mobile-devices lock` | `--pin <pin>` | `--pin-file <path>` |
| `pro mobile-devices clear-passcode` | `--unlock-token <token>` | `--unlock-token-file <path>` |

Clearing a Recovery Lock is now something a caller asks for. With neither
`--new-password-file` nor `--clear`, `set-recovery-lock` prompts on a terminal,
and under `--no-input` it refuses. It used to clear the password, so a script
that relied on "no flag means clear" has to pass `--clear`. An empty file or an
empty prompt is refused too, rather than read as a clear. None of the file
flags reads stdin: `-` is refused with exit 2. A trailing line ending in any of
the files is dropped. An error reading one of the files names
the flag and not the path, so a secret typed where its path belongs is not
echoed.

### Behaviour — `-vvv`, `--dry-run` and error messages redact more credentials

The `-vvv` body log redacted passwords, secrets and the OAuth `access_token`,
and missed every other credential a body carries: `token`, `pin`,
`unlockToken`, `accessToken`, `serverToken`, `bootstrapToken`, `encodedToken`,
`identityKeystore`, `gsxKeystore.keystoreBytes`, an XML `<token>`, a form
`token=`, a FileVault institutional recovery key's `<key>` and `<data>`, and
any secret in a configuration profile's plist. Now:

- A field is redacted when its name, split on camelCase and `_ - .`, ends in
  `token`, `pin`, `passcode`, `keystore`, `keystore bytes`, `keystore file`,
  `authorization`, `authorization header`, `challenge`, `signature`,
  `credential` or `credentials`, or when it holds one of the old credential
  words anywhere. A field that only starts with one of these words is left
  alone, so `tokenUrl`, `tokenEndpointAuthMethod`, `token_type`, `pinned`,
  `keystoreFileName` and `authorizationEndpoint` still read in full.
- Every value inside an `institutional_recovery_key` element or object is
  redacted. The inventory status form,
  `<institutional_recovery_key>Not Present</institutional_recovery_key>`, is
  not.
- A plist `<key>Password</key><string>…</string>` pair is redacted by its key,
  raw, entity-escaped inside a Classic `<payloads>` element, or inside a JSON
  string.
- A string array under a credential name is redacted. A number is redacted only
  under a `pin` or `passcode` name, so `passwordMinLength` stays readable. A
  boolean never is.

`--dry-run` printed request bodies with no redaction at all, on Jamf Pro and
Classic writes, on Platform gateway writes (including the gateway-served
Security Cloud commands) and on Security Cloud Radar writes. All three previews
now go through the same redactor as `-vvv`. So do the response bodies quoted
into an HTTP error message and the JSON error envelope, and the query string of
the `-v` request line. A JCDS download that fails before a response no longer
prints the pre-signed URL's query, which is its credential. A script that read a
credential back out of any of these gets `[REDACTED]`.

### Behaviour — Classic `update --name`, `apply` and `--group` refuse an ambiguous name

Jamf Pro allows two records to share a name. `update --name` on each of the 33
Classic resources that take it asked the server's `/name/` endpoint, which
answers with one record of its own choosing, and wrote to it. Now `update
--name` resolves the name from the collection, the same way `delete --name`
and `apply` already do, and writes by ID. When two records share the name,
the command asks which one to update. With `--no-input`, or when stdin is not
a terminal, it fails, names the colliding IDs, and writes nothing. The same
refusal now applies to `delete --name`, `apply` and `pro blueprints
import-profile` when stdin is not a terminal, where they used to read the
choice from a pipe. The refusal exits 2 and its hint says to pass one of the
IDs as `<id>`. A name that reads as a number or a boolean, such as `2024`,
`true` or `1.50`, is now matched as text, so `update --name`, `delete --name`
and `apply` find that record, and two records that both have that name are
refused as a collision.

`pro classic-mac-apps apply` and `pro classic-mobile-apps apply` resolved the
name to an ID and then fetched the record to merge by name again, so they
could merge one app's settings into another. They now fetch by the resolved
ID.

`--group` on `pro classic-mobile-devices delete`, `pro computer-inventory
delete` and `pro computer-inventory attachments-delete` acted on the first
group whose name matched without regard to case. Now a group whose name
matches with the same case wins if it is the only one. Otherwise more than one
match fails, names the colliding group IDs, and acts on no device. Pass the
group's ID to choose one. A group list larger than 4 MiB is refused rather
than searched in part. The device-action `--group` commands, such as `pro
computer-inventory erase`, use a different resolver, which a separate change
covers.

### Breaking — MCP `run_command` refuses local paths and credential output unless the operator allows them

`run_command` used to compare the model's raw argument list against a short
deny-list. A command alias (`cfg`), a leading flag (`--no-color multi`) or a
flag inside the path (`pro -q backup`) got past it. Flags that name a local
file were not on the list, so the model could read, write or delete files on
the machine running `mcp serve`, and `pro diff --target <profile>` used
another profile's credentials.

The server now judges the command and the flags that cobra resolves, and the
child process checks again before it runs. These now fail over MCP:

- A flag whose value is a local file to read (`--from-file`, `--file`,
  `--script-file`, `--input` and the like), unless the path is inside the
  directory the operator passes to `mcp serve --input-dir <dir>`.
  `--password-file` is always refused.
- A flag whose value is a local path to write (`--save-to`, a command's own
  `--output`, `--report-dir`, `--dir` on `sync`). The `-o/--output` format
  flag is not affected.
- `pro diff` with a side that is neither the server's profile nor a directory
  inside `--input-dir`.
- Body logging: `-vvv`, or any `--verbose` level of 3 or more however it is
  spelled. It logs each response body to stderr before any redaction, and
  `run_command` returns stderr. Use `-vv` or less. In every mode, not only over MCP,
  the `-vv` header log now shows `Cookie` and `Set-Cookie` values as
  `[redacted]`, as it already did for `Authorization`.
- `multi`, `mcp`, `completion`, the `config` write subcommands,
  `config validate`, `doctor`, every `setup`, both `backup` commands and
  `jcds sync`.
- The commands that print an access token: `auth token` under `platform`,
  `pro` and `protect`, and `pro api-authentication token`, `oauth-token` and
  `keep-alive`.
- `pro sso-oauth-session-tokens`, which prints the session's access and ID
  tokens.
- The commands that mint a credential and print it:
  `pro api-integrations client-credentials` (a new client secret) and
  `protect api-clients apply` (a new API client's password).
- `pro cloud-distribution-point create` and `patch`, whose response
  carries the CloudFront private key that signs download URLs.
- The commands that set a Jamf Pro login password to a value the model
  chose: `pro jamf-pro-user-account-settings change-password` and
  `pro accounts create`, `update` and `apply`.
- `protect downloads csr` and `websocket-auth`, which write the tenant's
  `.p12` key material into the server's working directory.
- `protect action-configs export`, whose document carries each report
  client's header values, such as a SIEM or webhook bearer token. A redacted
  copy would overwrite the real credential when applied.

`config show` still runs over MCP, with each token, client ID and client
secret shown as `<redacted>`. `config list --status` checks only the server's
profile. These Protect commands also run over MCP with the credential shown as
`<redacted>`: `action-configs get` and `apply` (each report client's header
values, and the userinfo and query of each report-client URL),
`data-forwarding get` and `update` (the Sentinel shared key) and
`api-clients get` (the password). `pro cloud-distribution-point list` runs
over MCP with the CloudFront private key and the CDN password shown as
`<redacted>` in every output format. Every Classic `get` and `list` prints each
field that Classic `--set` refuses as a credential as `<redacted>`, in every
output format, so `-o raw` is not the wire bytes over MCP, and `pro diff`
shows those fields' old and new values as `<redacted>` while still reporting
the change. `get` and `list` on `classic-macos-config-profiles` and
`classic-mobile-config-profiles`, `pro diff` on `profiles` and `pro blueprints
components configuration-profile --id/--name` also redact profile payloads by
key name: the value of a key named `Challenge`, or ending in any case in
`password`, `secret`, `token`, `authkey`, `apikey`, `accesskey`, `privatekey`,
`secretkey`, `passcode` or `credential`, prints as `<redacted>`, and so does a
PKCS#12 certificate. Every other payload value is shown, a custom payload's
included. The payload is re-encoded, so the record is still a profile
document. A payload that does not decode is redacted whole, and the blueprint
converter refuses it. Outside MCP their output is unchanged. Secrets of the pinned tenant's devices (the LAPS
password, the recovery lock password, the FileVault personal recovery key),
the JCDS upload credentials of `pro jamf-cloud-distribution-service
renew-credentials` and `pro jamf-cloud-distribution-service-files create`,
and blueprint configuration, a secret a component carries included, are
still shown.

`mcp serve --input-dir ""` (for example `--input-dir "$DIR"` with `DIR`
unset) is now an error instead of starting with no input directory.

**Migration:** if an agent sends file bodies through `run_command` (for
example `pro scripts create --script-file …`), start the server with
`mcp serve --input-dir <dir>` and keep those files in that directory.

### Behaviour — `protect plans config-profile` refuses a plan name that is not a file name

Without `-O`, the command saves `<plan name>.mobileconfig` in the working
directory. It now refuses a plan name that contains `/` or `\`, or is empty,
`.` or `..`, because that name would put the file somewhere else. Pass
`-O <path>` for such a plan. Every other name is saved as before.

### Breaking — `--set` refuses credential fields in Pro, Platform and Security Cloud

A `--set` value is on the command line, so it lands in shell history, in `ps`
output and in CI job logs. Classic `--set` already refused a credential field.
The generated Pro `patch` and `update`, Platform and Security Cloud commands
accepted one, and the Pro `update` warning for a write-only field told you to
pass it as `--set <field>=<value>`.

`--set` now refuses a field that carries a secret: a password, client or shared
secret, token, API or access key, private key, keystore, authorization header or
device PIN, and any string the spec marks write-only. The refusal also catches a
JSON value that carries one, such as `--set deviceSyncAuth='{"clientSecret":"…"}'`.
A private-key blob is refused too: ADCS and DigiCert `clientCert.data` and the
cloud LDAP `server.keystore.fileBytes`, while the public `serverCert.data` stays
settable. A key is matched however it is spelled (`pass_word`,
`basic_auth_credentials.password`), and on an operation that carries a secret a
key with an empty segment, a `[` or surrounding whitespace is refused.
Examples are `security uem-connectors create --set deviceSyncAuth.clientSecret=…`,
`security stream update --set delivery.authorization_header=…` and
`pro computer-prestages update --set recoveryLockPassword=…`.

**Migration.** Put the secret in the request body and pass it with
`--from-file <file>` or on stdin. On Platform and Security Cloud, `--set` still
overrides the other fields of that body. A Pro `update` reads its body from
stdin only, so pipe the whole record, secret included. Pro `update --set` on a
resource with a write-only secret now always blanks that secret, and its
warning says so. Pro `--help` and shell completion no longer list credential
fields; the help names them and says where they go.

### Behaviour — `--url` and a profile's `url` no longer redirect the Security Cloud Radar login

`security` commands that use the Risk, Device Lifecycle or SSE credentials
took the Radar API host from `--url`, then `JAMFSECURITY_URL`, then the
profile's `url`. That field holds the Jamf Pro instance or, after
`platform setup`, the platform gateway, so the login sent the Radar client ID
and secret as Basic auth to that host. The Radar host is now
`api.wandera.com` unless `JAMFSECURITY_URL` is set.

Migration: a script that pointed the Radar client elsewhere with `--url` or a
profile `url` sets `JAMFSECURITY_URL` instead. `--url` still selects the
gateway for the gateway-served commands (`dns-*`, `ztna-*`, and the rest).

### Behaviour — an identifier that names more than one record is refused

These commands used to pick one record when an identifier matched several.
Some picked the last match, and some picked a record whose name equalled
another record's serial number. They now act on one record, or refuse and
name every candidate:

- `school devices get`, `erase`, `unenroll`, `trash`, `clear-activation-lock`,
  `restore`, `restart` and `refresh` match an exact UDID, then a serial
  number, then a name. The confirmation names the device it resolved: name,
  serial and UDID. Every other School command that takes a name (users,
  profiles, apps, classes, groups, device groups, locations, iBeacons)
  refuses a name that two records share.
- `protect computers get`, `delete`, `set-plan` and `update` match an exact
  UUID, then a serial number, then a hostname. A UUID was not accepted
  before. The `delete` confirmation names the computer it resolved.
- `--group` on the Jamf Pro device actions (`pro computer-inventory erase`,
  `pro mobile-devices erase`, and the `pro computers` and `pro mobile-devices`
  actions such as `lock`, `restart`, `blank-push` and `update-inventory`)
  compares the static-group name as the server sent it, so `--group 14` no
  longer finds a group named `14.2`.
  If two static groups differ only in case, the one that matches exactly is
  used. If neither matches exactly, the command refuses. A smart-group lookup
  that finds two groups is now an error. Before, the Classic static-group
  lookup ran after it and picked one of the two. The refusal names the ids.
- `pro computers flush-commands --group` and `pro mobile-devices
  flush-commands --group` list the Classic group collection and resolve the
  name to exactly one group. Before, they used the Classic `/name/` endpoint,
  which returns one group when two share a name, and the commands were
  flushed from that group. The command now refuses and names the ids. The
  prompt shown without `--yes` names the group's stored name and id.
- `pro bulk send-command --group`, `pro bulk add-to-group` and
  `pro bulk remove-from-group` (`--group` and `--target-group`) resolve a
  group name to exactly one group, by the same rule. Before, they took the
  first group in the listing whose name matched without case.
- Blueprint scoping by group name refuses a name that two groups share and
  names their ids. It accepts a group only when the server returns that exact
  name. Before, it took the first result.
- `pro computer-inventory set-auto-admin-password` refuses a `--user-name`
  that matches several LAPS accounts, preferring an exact match over a case
  variant. Without `--user-name` it uses the one MDM-created account, and
  refuses and lists the accounts when there is none or more than one. Before,
  it took the device's first account.
- `pro setup` updates an API role or integration, and rotates its client
  credentials, only when the search result carries the display name it
  searched for.
- `protect restore` refuses an insight label that two insights share.
- `pro packages upload` refuses when two packages have the local file name,
  and it accepts a match only when the server returns that exact name.
- `protect <resource> apply`, `protect analytics import` and
  `protect unified-logging-filters import` refuse a name that two records
  share and name their ids. Before, the last record in the listing was
  updated. Every other Protect command that takes a name refuses it too.
- `pro classic-<resource> scope get`, `add`, `remove` and `set` with `--name`
  list the collection and resolve the name to exactly one record, for every
  scopeable resource. Before, they used the Classic `/name/` endpoint. When
  two records share a name, that endpoint returns one of them, and the scope
  was written to that record. The command now refuses and names the ids. Pass
  one as `<id>`. Names are compared without case, and a name lookup costs one
  more request.

RSQL filter values now escape `\` as well as `"`, so a value cannot close its
quotes. A Jamf Pro lookup by serial number, name, management ID, UDID or
group name accepts a result only when its value matches the one requested,
ignoring case. A `*` in the value can make the server return other records,
and those are now refused instead of acted on.

`school devices` actions send an argument that the device listing does not
hold only when it has a UDID's form. Before, any argument went into the
request path.

`protect <resource> apply` creates a record only when the lookup completes
and finds no record with that name. A lookup that fails (a 401, a 403, a 5xx,
a GraphQL error or a timeout) now returns the error. Before, the command
created a duplicate. This applies to plans, API clients, users, groups,
roles, action configs, analytic sets, exception sets, custom prevent lists,
telemetry, removable storage control sets, unified logging filters and
unified logging filter sets.

### Behaviour — the MCP `list_commands` tool browses and searches the catalog

`list_commands` returned the whole catalog in one result. That result was
larger than the 256 KiB cap on a child's output, so the tool cut it
mid-string: the model got invalid JSON with no Protect, School or Security
Cloud commands.

The tool now takes two optional arguments, `prefix` and `query`, and returns
one JSON object per line:

- With no arguments, it lists the top level. A row with `"subcommands": N`
  has N commands under it.
- `prefix` opens one command path, for example `"pro"` or `"pro computers"`.
  A runnable command is listed with `description`, `destructive` and `flags`.
- `query` returns the commands whose path, description or aliases contain
  every word, for example `"delete policy"`.

Each result is kept under 40 KiB. Claude Code saves a text tool result
longer than 50,000 characters to a file, and gives the model only the file
path. An MCP client that parsed the old array gets NDJSON rows
now. The old result was always cut and invalid, so no client parsed it.

`jamf-cli commands` takes the same selection as `--prefix <path>`,
`--children` and `--search <words>`. With no flags it prints the whole
catalog, as before.

### Behaviour — `--all` honours a smaller `--page-size` and halves a page that times out

Fixes [#392](https://github.com/Jamf-Concepts/jamf-cli/issues/392). v1.31.1 moved
`--all` to 2000-row pages, and Jamf Pro can take longer than the gateway edge's 90
seconds to build a 2000-row page of computer inventory. The CLI gave up at 60 seconds
and sent the same request twice more, so `pro computer-inventory list --all` failed
after three full timeouts. Four changes a script can see:

- **`--all` honours a `--page-size` below the endpoint's maximum.** v1.31.1 ignored the
  flag under `--all`, which left no way to ask for a smaller page. The CLI still clamps
  a size above the maximum and says so on stderr.
- **The `--all` walk halves a page that times out** and resumes after the rows it
  holds, down to 100 rows a page. That covers the generated Jamf Pro `list --all` and
  every report and audit that fetches a whole collection. Each halving prints
  `page of 2000 timed out; continuing at --page-size 1000. Pass --page-size 1000 to
  start there.` on stderr, and `--quiet` suppresses it. Platform and Security Cloud
  lists do not shrink.
- **The CLI retries a Jamf Pro request only when re-sending it is safe.** It retries a
  request that never reached the server, whatever the method, and a `GET`, `HEAD` or
  `OPTIONS` that failed after sending. It never retries a timeout. A write that fails
  after sending carries the hint that it may have been applied. `PUT` counts as a
  write, because a smart group `PUT` recalculates membership inside the request.
- **The response-header timeout rises from 60 to 120 seconds**, above the gateway
  edge's 90, so the edge's `504` arrives first. A timeout and a `504` produce the same
  error.

Reasoning and the measurements:
`docs/solutions/logic-errors/timeout-retried-at-the-same-size-2026-09-30.md`.

## v1.31.1

### Behaviour — computer group member counts come from the collection that carries one

`pro group-tools` and `pro audit` read member counts from
`/v3/computer-groups/{smart,static}-groups` instead of `/v1/computer-groups`.
The v1 collection carries no count at all — wire-checked against Jamf Pro 11.32
on 2026-09-18, it answers `description`, `id`, `name` and `smartGroup` and
nothing else — so the `memberCount` these commands type-asserted was **always
absent and every group reported 0 members**. Three consequences a script can
see:

- **`memberCount` can now be the string `"unknown"`.** A group the collection
  listed without a readable count is reported as such rather than as empty: an
  absent count is not evidence that a group has no members. Anything doing
  arithmetic on `memberCount` needs to handle a non-numeric value, and a stderr
  warning names how many rows are affected.

- **`pro group-tools list --empty` and `analyze --unused` return fewer rows.**
  Both used to list every group, because every count read as 0. They now list
  only groups proved empty — an unknown count is excluded from both, and
  `--empty` says on stderr how many groups it left out for that reason.

- **`pro audit` gains an `Empty smart groups` finding.** It could not fire
  before: the count was always absent, so its `ok` was always false,
  `emptyCount` never incremented and the check returned no finding on every
  instance, including ones with dozens of empty smart groups.

The two v3 collections share v1's id space, and both count fields are
wire-confirmed — `membershipCount` for smart groups, `count` for static ones,
the latter checked by loading 12 computers into an empty static group and
reading 12 back. Same request cost as before: one paginated sweep per
collection.

### Behaviour — `--all` chooses its own page size, and `--page 0` means page 0

Fixes [#385](https://github.com/Jamf-Concepts/jamf-cli/issues/385). v1.32.0 reverses
the first point: `--all` honours a smaller `--page-size`. Three changes a script can
see:

- **`--all` ignores `--page-size` and requests the largest page the endpoint is
  known to honour** — 2000 on the Jamf Pro API's `{totalCount, results}`
  endpoints, 1000 on `/v1/users` and Platform `devices/v1`, 500 on AI
  Governance, 100 where neither the spec nor a wire probe says more. A
  9000-computer inventory pull is 5 requests instead of 95. `--page-size` still
  applies to a single page (`--all=false`), clamped to the same ceiling.

  Ignoring the flag is the safe reading, not the lazy one: the Jamf Pro API
  answers an oversized `page-size` by **silently clamping** it, and the walk
  read a short page as the last page — so passing `--page-size 2500` through
  would have returned 2000 of 2601 records at exit 0. Wire-checked on
  `/v1/departments` 2026-09-18.

  Both substitutions are now reported on stderr naming the page size actually
  used. Suppressed by `--quiet`; **not** suppressed by `--no-hints`, which turns
  off advisory tips, and a flag that did nothing is not a tip.

- **`--page 0` returns the first page alone.** It used to be read as "not set"
  and fell through to fetching every page, leaving page 0 reachable only as
  `--all=false` with no `--page`. Any invocation relying on `--page 0` meaning
  "all pages" now gets one page.

- **`--page` and `--page-size` have help text.** The published spec describes
  neither, so both rendered blank, with nothing saying `--page` is zero-based or
  what the page-size ceiling is.

Every fetch-everything path moved with it, not just the generated `list`
commands: `pro report *`, `pro audit`, `pro backup`, `pro group-tools`,
`pro dashboard`, the `pro overview` JCDS and VPP tiles, and Jamf Protect
deployment tasks. Platform `blueprints` and `device-groups` stay at 100 — no
declared maximum and no probe — as does the generic Platform name resolver.
Reasoning and the wire evidence:
`docs/solutions/logic-errors/all-ignored-page-size-2026-09-18.md`.

### Fixed — a YAML export or scaffold carries the same document as the JSON one

`pro blueprints export -o yaml` rendered each component's `configuration` as a sequence
of integers — the bytes of its own JSON text — and lower-cased every key
(`activationpredicate`). Both came from yaml.v3, which reads neither `json` tags nor
encoding/json's treatment of `json.RawMessage`. The same two applied to
`pro blueprints apply --scaffold -o yaml` and to the `--scaffold -o yaml` of
`pro compliance-benchmarks apply` and `pro platform-device-groups create`.

- **A YAML export and a YAML scaffold now carry the keys the JSON one carries.** A
  script keying on `activationpredicate`, or reading `configuration` as a list of
  numbers, has to read `activationPredicate` and a mapping instead. `-o json` is
  unchanged. The Jamf Protect and Jamf School exports are unchanged, their input types
  carrying no `json` tags for a YAML document to follow.
- **The YAML input path binds by `json` tag.** Every `--from-file` reading JSON or YAML
  through this path (`pro blueprints`, `pro compliance-benchmarks`, the platform device
  groups, and the Protect and School applies) now decodes YAML through JSON, so a
  key spelled either way binds — encoding/json matches a key case-insensitively.
- **A blueprint YAML export written by v1.31.0 or earlier is refused rather than sent.**
  Its byte-sequence `configuration` is a valid JSON array, so it would otherwise reach
  the gateway as the component's configuration. Re-export and apply that file.

## v1.30.0

### Breaking — Classic `scope` subcommands take `[<id>]` and `--name`

- **The positional argument is now the id, not the name.** `pro <resource> scope
  get|add|remove` took the name positionally — the only commands in the binary that
  did — so a caller holding an id from `list` had to go and find the name for it, and
  an object whose name looks like an id could not be addressed at all. They now match
  every other Classic command: an optional `[<id>]` plus `--name`, refused together.

  ```
  # before
  jamf-cli pro classic-policies scope add "Deploy Chrome" --computer-group "Lab Macs"
  # after — either of
  jamf-cli pro classic-policies scope add 1 --computer-group "Lab Macs"
  jamf-cli pro classic-policies scope add --name "Deploy Chrome" --computer-group "Lab Macs"
  ```

  A non-numeric positional is refused at exit 2 naming `--name`, rather than being sent
  as an id and answering a 404 whose hint points at `list`.

- **Each command now registers only the scope categories its own resource carries**, so
  `--help` and shell completion stop offering flags the server refuses. Validation had
  two branches — restricted software, and everything else — so a policy accepted
  `--mobile-device-group` and a mobile configuration profile accepted `--computer-group`,
  both spending a GET and a PUT to earn `409 Error: Mobile device groups cannot be
  assigned to an macOS profile`. There are five scope shapes across the eight scopeable
  resources, read off each resource's own GET and cross-checked against
  terraform-provider-jamfplatform's independently wire-probed schemas.

  A category belonging to another device family is now an unknown flag, whose hint names
  the categories this resource does have. A valid category in the wrong section is
  refused naming the section that takes it.

- **`--ibeacon` and `--class` are new.** Both were categories `scope get` listed and
  nothing could write — the worst shape for a gap, since the CLI showed a value it could
  not change. `--ibeacon` is accepted on the three resources that carry iBeacons and
  refused on the five that silently drop them; `--class` on ebooks only.

- **`--user` is now accepted as a restricted-software exclusion.** It was refused
  outright; the wire accepts it and stores it. It is the admin UI's "Directory
  Service/Local Users" exclusion.

- **A scope write sends only `<scope>`.** It used to GET the whole document, splice the
  new scope into its bytes and PUT the entire document back, so one `scope add` cost
  three GETs and re-sent sections the caller had not touched. A Classic PUT is a partial
  update at top-level-section granularity, verified on all eight resources against a
  direct instance and the platform gateway: every non-scope byte comes back identical,
  including a 19 KB configuration profile's `<payloads>`.

- **A scope name that matches more than one record is refused** instead of resolving to
  the first in document order. Only the two VPP resources reach this path, having no
  `/name/` endpoint; Classic names are not unique, and a live tenant carried two ebooks
  sharing one.

- **`-n, --dry-run` on a scope command no longer always fails.** The preview suppressed
  the PUT and the post-write check then reported that the server had not persisted the
  change, at exit 1.

### Fixed — Classic scope writes no longer silently destroy an ebook's class targets

- **Class targets are delivered in two requests, because one cannot work.** Jamf Pro
  stores `<classes>` only while the stored category is empty: a write made while it
  already holds a member clears it, and carrying the identical value, omitting the
  element and every identifier shape all clear it (5/5 each way). Since a scope PUT
  replaces `<scope>` wholesale, no single request can preserve an existing class across
  any other scope change — so `scope add --building` on an ebook holding a class
  destroyed the class and reported success. The intended change is now sent with
  `<classes>` emptied, then the same scope with the classes populated.

  The same defect is a hard `Provider produced inconsistent result after apply` in
  terraform-provider-jamfplatform, reported as
  [#428](https://github.com/jamf/terraform-provider-jamfplatform/issues/428).

- **A write is checked against the whole scope that was sent**, not just the category the
  command changed — which is why the loss above went unreported. Anything the server did
  not keep is named.

- **A Classic HTTP error renders its reason instead of an HTML page.** The Classic API
  answers a refused write with a status page whose one useful sentence was buried in
  ~400 bytes of markup and inline CSS, arriving in the JSON error envelope as a single
  escaped line. `Error: Unable to match computer group` and its siblings are now the
  message.

### Added — Jamf Pro 11.32 and jamfplatform-go-sdk v1.1.0

The SDK moved to the `jamf` GitHub org and its module path with it
(`github.com/jamf/jamfplatform-go-sdk`). That is an import change inside this repo and
nothing a CLI user sees. `v1.0.0` under the old `Jamf-Concepts` path keeps working and
gets no further releases.

- **`pro sso-settings oidc-broker-config get` and `update`** are new — `GET` and
  `PUT /v3/sso/oidc-broker-config`, added in Jamf Pro 11.32. A sub-path carrying its own
  `PUT` is an independently-writable object, so its verbs sit one token deeper, the same
  shape as its sibling `pro sso-settings cert`. Nothing moved: there is no earlier
  spelling of this command, the endpoint not having existed before.
- **`pro jamf-pro-notifications delete` gains `--all`**, which dismisses every
  dismissible notification for the user and site in one server-side call
  (`DELETE /v1/notifications`, new in 11.32). It refuses to be combined with an `<id>`
  and prompts for confirmation unless `--yes` is passed, like every other tenant-wide
  `--all`. **`delete <id> <type>` is unchanged** — same positionals, same endpoint.

  `-n, --dry-run` covers it: `delete --all -n` reports what it would send and sends
  nothing. That needed its own code — a destructive generated command declares its own
  `--dry-run`, which shadows the root persistent one, so the command's own branch is the
  only thing honouring `-n` on it, and `--all` returns before that branch is reached.

  Worth knowing why that needed saying: the new collection-level `DELETE` derives the
  same `delete` name as the existing per-notification one, and the collection addresses
  the resource root, so the plain verb went to it and the per-notification operation was
  dropped. Left alone, `pro jamf-pro-notifications delete` would have silently changed
  from removing one notification to dismissing all of them, and the per-notification
  capability would have gone. Both are now the same command.
- **Two filter fields and one preference** arrived with 11.32 and are visible in
  `--help`: `general.awaitingConfiguration` and `security.lockdownModeEnabled` on
  `pro computer-inventory list --filter`, and `showDirectoryGroupUuidColumn` on
  `pro jamf-pro-account-preferences update --set`.

### Changed — Jamf AI Governance commands are marked Preview

Upstream declares all twelve AI Governance operations preview endpoints, subject to
breaking change without warning, with general availability expected by 2027-03-03.

- Each command's `Short` now opens with `Preview - ` and its `Long` with the preview
  notice, both from the published spec. Anything parsing `platform ai-policies --help`
  or `platform ai-tools --help` text sees new wording.
- `jamf-cli commands -o json` carries `"preview": true` on those commands (thirteen: the
  spec's twelve operations plus the synthesized `apply`), which is what to key on rather
  than the help prose. Present only when true, so its absence means "nothing declared"
  rather than "GA"; it is a JSON field only, table and CSV columns coming from the first
  row.
- Help text no longer carries markdown `**bold**` or `_italic_` markers on any platform
  command; backticks, which quote a field or a value, are unchanged.

## v1.29.0

### Breaking — `pro` command names come from the spec, not from spec filenames

- **Resource names are derived from the API's own OpenAPI tags and URL paths.** They
  used to come from the filename each resource's paths were split into — upstream's
  jss module names, which appear in no published spec and in no API reference. Four
  things followed from that filename with nothing stating them: the command name, the
  endpoint-version family, whether a `-preview` tag reached a command at all, and
  whether the ingest could delete the file.

  Two things were wrong as a result. The ingest was **not reproducible** — wiping
  `specs/` and re-ingesting the same document gave 132 resources instead of 169, 73
  operations dropped and 18 `-preview` endpoints published as commands, at exit 0. And
  version consolidation keyed on a `-vN` filename suffix, which is how this CLI once
  sent `/v3/computers-inventory` while `/v4` was the served version. Both are now facts
  about the paths rather than about a filename.

  `specs/` holds one normalised document (`JamfProAPI.yaml`) instead of 165 carved-up
  ones.

- **163 resources became 135**: 38 renamed, 24 merged, 13 split. The names now match
  what the Jamf Pro API reference calls these resources. Ten were outright bugs the
  filenames had baked in — eight double plurals (`csas`, `slasas`, `oidcs`,
  `cloud-informations`, `inventory-informations`, `jamf-pro-informations`,
  `device-compliance-informations`, `jamf-remote-assist-session-histories`),
  `mac-os-managed-software-updates`, and two names that described the wrong thing
  (`patch-titles` held a single POST accepting a disclaimer; `dss-proxies` held
  `/v1/dss-declarations/{id}`).

  **`pro users` and `pro patch-policies` keep their names**, and both were briefly on
  the renamed list. A resource whose paths carry two tags has to be named by one of
  them, and the tag was being taken from whichever of its paths sorted last — which
  named the `/v1/users` CRUD after two `recalculate` actions tagged `smart-user-groups`,
  and `/v2/patch-policies` after its own logs sub-path. A merged root is named by the
  tag of its own root collection now, so `pro users list` and `pro patch-policies list`
  are the canonical names they always were. `pro user-smart-groups` and
  `pro patch-policy-logs` are the aliases, both resources having folded into the one
  they act on, and every verb from each survives under its new parent unchanged.

- **Every former resource name still works until 2027-03-09.** 100 of them resolve to
  their replacement and print a warning naming it; 3 refuse with an explanation
  because their endpoints are no longer ingested (`pro servers`,
  `pro remote-administration-configurations`,
  `pro redeploy-jamf-management-frameworks`). The warning is not silenced by `--quiet`
  or `--no-hints`. After that date the build fails until the aliases are deleted, so
  they cannot rot silently — and 60 days ahead of it a weekly scheduled build starts
  failing instead, so the removal PR gets written rather than discovered.

  `pro jcds` is the one former name that is **not** on the expiring list. It is a
  curated short alias now, along with `pro jcds-files` for the half the sub-resource
  split gave a command of its own — 37 characters is not a name anyone types.

#### 48 invocations an alias cannot cover — the operation name moved too

An alias maps one resource name to one replacement. Where a resource **split**, or
where an operation's name is derived from a path segment that now sits under a
different resource, the resource alias resolves and the subcommand then does not
exist. The endpoint is unchanged in every case, so only the command name moved.

**All 48 answer at runtime with the new invocation named**, in place of cobra's
`unknown command`, and both spellings of each reach it — the old resource name through
its alias, and the new one directly. `pro schedulers triggers` and `pro scheduler
triggers` both report that the operation moved and name `pro scheduler-jobs triggers`.
Exit code 2, the same as every other command that does not exist: what a caller is
missing here is not a classification but the pointer, and a second exit code for one
class would only make a wrapper script harder to write. Two old invocations collapse
onto `pro enrollment list`, whose endpoints went to different places, so that one names
both. `pro mobile-device-prestages delete-multiple` and
`pro mobile-device-prestage-scopes delete-multiple` collapse onto one path the same
way, and also name both. Three of the 48 are a verb whose *meaning* moved rather than
its name —
`create`, `update` and `delete` under `pro enrollment-customization-panels`, which
resolve to a live command that addresses the customization instead of the panel, so
they are refused where the other 45 would have got `unknown command`. The refusals
retire with the aliases on 2027-03-09.

The subsections after this one cover the rest: 9 endpoints that are no longer ingested
at all, 3 `apply` commands that were never expressible, and 3 paths that became command
*groups*.

| was | is now | endpoint |
|---|---|---|
| `pro access-managements list` | `pro enrollment access-management` | `GET /v4/enrollment/access-management` |
| `pro activation-codes patch` | `pro activation-code organization-name patch` | `PATCH /v1/activation-code/organization-name` |
| `pro api-roles-privileges api-role-privileges` | `pro api-role-privileges list` | `GET /v1/api-role-privileges` |
| `pro app-requests create` | `pro app-request-form-input-fields create` | `POST /v1/app-request/form-input-fields` |
| `pro app-requests delete` | `pro app-request-form-input-fields delete` | `DELETE /v1/app-request/form-input-fields/{id}` |
| `pro app-requests list` | `pro app-request-form-input-fields list` | `GET /v1/app-request/form-input-fields` |
| `pro app-requests settings` | `pro app-request get` | `GET /v1/app-request/settings` |
| `pro app-requests update-settings` | `pro app-request update` | `PUT /v1/app-request/settings` |
| `pro cloud-distribution-points cloud-distribution-point` | `pro cloud-distribution-point list` | `GET /v1/cloud-distribution-point` |
| `pro computer-inventory-collection-settings create` | `pro computer-inventory-collection-settings-custom-path create` | `POST /v2/computer-inventory-collection-settings/custom-path` |
| `pro computer-inventory-collection-settings delete` | `pro computer-inventory-collection-settings-custom-path delete` | `DELETE /v2/computer-inventory-collection-settings/custom-path/{id}` |
| `pro csas delete` | `pro csa token delete` | `DELETE /v1/csa/token` |
| `pro enrollment-languages filtered-language-codes` | `pro enrollment filtered-language-codes` | `GET /v3/enrollment/filtered-language-codes` |
| `pro enrollment-languages language-codes` | `pro enrollment language-codes` | `GET /v3/enrollment/language-codes` |
| `pro enrollment-settings create` | `pro enrollment-access-groups create` | `POST /v3/enrollment/access-groups` |
| `pro enrollment-settings delete` | `pro enrollment-access-groups delete` | `DELETE /v3/enrollment/access-groups/{id}` |
| `pro enrollment-settings enrollment` | `pro enrollment get` | `GET /v4/enrollment` |
| `pro enrollment-settings list` | `pro enrollment-access-groups list` | `GET /v3/enrollment/access-groups` |
| `pro enrollment-settings update-enrollment` | `pro enrollment update` | `PUT /v4/enrollment` |
| `pro health-checks health-check` | `pro health-check list` | `GET /v1/health-check` |
| `pro jamf-connects jamf-connect` | `pro jamf-connect list` | `GET /v1/jamf-connect` |
| `pro jamf-connects update` | `pro jamf-connect-config-profiles update` | `PUT /v1/jamf-connect/config-profiles/{id}` |
| `pro jcds delete` | `pro jamf-cloud-distribution-service-files delete` | `DELETE /v1/jcds/files/{fileName}` |
| `pro jcds files` | `pro jamf-cloud-distribution-service-files create` | `POST /v1/jcds/files` |
| `pro jcds get` | `pro jamf-cloud-distribution-service-files get` | `GET /v1/jcds/files/{fileName}` |
| `pro jcds list` | `pro jamf-cloud-distribution-service-files list` | `GET /v1/jcds/files` |
| `pro local-admin-passwords update` | `pro local-admin-password settings update` | `PUT /v2/local-admin-password/settings` |
| `pro log-flushings delete` | `pro log-flushing-task delete` | `DELETE /v1/log-flushing/task/{id}` |
| `pro log-flushings get` | `pro log-flushing-task get` | `GET /v1/log-flushing/task/{id}` |
| `pro log-flushings log-flushing` | `pro log-flushing list` | `GET /v1/log-flushing` |
| `pro log-flushings task` | `pro log-flushing-task create` | `POST /v1/log-flushing/task` |
| `pro managed-software-updates-plans abandon` | `pro managed-software-updates-plans feature-toggle abandon` | `POST /v1/managed-software-updates/plans/feature-toggle/abandon` |
| `pro managed-software-updates-plans status` | `pro managed-software-updates-plans feature-toggle status` | `GET /v1/managed-software-updates/plans/feature-toggle/status` |
| `pro managed-software-updates-plans update` | `pro managed-software-updates-plans feature-toggle update` | `PUT /v1/managed-software-updates/plans/feature-toggle` |
| `pro mdm-renewals patch` | `pro mdm-renewal-device-common-details patch` | `PATCH /v1/mdm-renewal/device-common-details` |
| `pro mobile-device-prestage-scopes delete-multiple` | `pro mobile-device-prestages scope-delete-multiple` | `POST /v2/mobile-device-prestages/{id}/scope/delete-multiple` |
| `pro mobile-device-prestages delete-multiple` | `pro mobile-device-prestages attachments-delete-multiple` | `POST /v3/mobile-device-prestages/{id}/attachments/delete-multiple` |
| `pro policy-properties policy-properties` | `pro policy-properties get` | `GET /v1/policy-properties` |
| `pro policy-properties update-policy-properties` | `pro policy-properties update` | `PUT /v1/policy-properties` |
| `pro schedulers summary` | `pro scheduler list` | `GET /v1/scheduler/summary` |
| `pro schedulers triggers` | `pro scheduler-jobs triggers` | `GET /v1/scheduler/jobs/{jobKey}/triggers` |
| `pro self-service-plus get` | `pro self-service-plus settings get` | `GET /v1/self-service-plus/settings` |
| `pro self-service-plus update` | `pro self-service-plus settings update` | `PUT /v1/self-service-plus/settings` |
| `pro sso-failovers list` | `pro sso-settings failover` | `GET /v1/sso/failover` |
| `pro sso-settings-cert cert` | `pro sso-settings cert create` | `POST /v2/sso/cert` |

#### 9 endpoints are no longer ingested

- `GET`/`POST`/`PUT`/`DELETE /v1/inventory-preload{,/{id}}` (5) — v1 is withdrawn from
  the gateway's published API *and* fully superseded: v2 moved every record operation
  under `records/`, so the CRUD lives at `pro inventory-preload-records`. Left in, the
  refused v1 paths took the plain `list`, `get` and `update` names while the served v2
  CRUD sat under a second command.
- `GET /preview/remote-administration-configurations` — the bare collection stub above
  the team-viewer family. Dropping it leaves
  `/preview/remote-administration-configurations/team-viewer/…` intact, which is the
  surface the gateway publishes.
- `POST /settings/issueTomcatSslCertificate` — unversioned legacy with no replacement
  in the versioned API.
- `POST /v1/computer-inventory/{id}/{erase,remove-mdm-profile}` (2) — the
  inventory-preload case with the noun changed: declared `deprecated` upstream,
  withdrawn from the gateway, and superseded by `/v4/computers-inventory/{id}/…`,
  which spells the collection segment differently. See the section below for what
  keeping them cost.

#### Operation names on a merged resource

Where a tag merges what were several resources, the resource's own root endpoint
keeps `get`/`list`/`update` and its siblings are named after their path segment. The
first cut renamed **both** sides of the collision, so the primary endpoint lost its
verb — `pro sso-settings sso` for `GET /v3/sso` beside `cert` for `/v2/sso/cert`, and
`pro enrollment enrollment` beside `language-codes`. 15 resources, 19 operations. A
resource with no root endpoint keeps segment naming throughout: `pro ldap` has
`groups`, `servers` and `ldap-servers` and no `list`, because there is no
`GET /v1/ldap` and a plain `list` would point at one arbitrary sub-collection with
nothing in the name saying which.

The same "the resource is one path root" assumption was in three separate places and
cost: three collection POSTs their `create`
(`pro jamf-cloud-distribution-service-files create`, `pro log-flushing-task create`,
`pro app-installers-deployments create`); two collection GETs their `list`
(`pro app-installers-titles list`, `pro app-installers-deployments list`); and two
singletons their `get` (`pro jamf-protect`, `pro cloud-distribution-point`), whose
root reads shipped as `list` because the singleton rename gave up as soon as *any*
operation on the resource carried a path parameter — which a merged resource always
has.

Where a sub-path gives up a plain verb to the resource root, the sub-path is named
after its segment: `pro sso-settings get`/`update` are `GET`/`PUT /v3/sso`. A verb
that collides with *nothing*, though, keeps the plain name even on a sub-path — see
the next section, which is what that cost and how it is fixed.

#### Independently-writable sub-paths are commands of their own

A tag can cover several path roots, and the grouping merges them into one resource.
That is right for the resource's identity and wrong for its verbs: an
independently-writable sub-path flattened into its parent produces a plain CRUD verb
that silently belongs to the sub-path, and because a lone verb collides with nothing,
no naming pass reached it.

Three examples of what that cost, all live before this change:

- **`pro sso-settings delete` sent `DELETE /v2/sso/cert`.** It deleted the SSO
  certificate, not the SSO configuration its name names. `download` and `parse`
  belonged to the certificate too.
- **`pro managed-software-updates-plans update` sent
  `PUT /v1/managed-software-updates/plans/feature-toggle`** on a resource whose
  `list`, `get` and `create` are real plan CRUD — so the one verb in the set that
  mutated pointed at a different object entirely.
- **`pro csa delete` deleted the CSA token.**

**The rule is derived from the spec.** A sub-path that carries `PUT`, `PATCH` or
`DELETE` on the sub-path *itself* is a separately-writable object and becomes a
command of its own; a `POST`-only sub-path is an append or a command submission and
stays flat. Measured over the committed document that admits **nine** sub-paths and
refuses every other one, including all 18 GET+POST `/history` pairs — by the rule,
not by a `history` special case. One further condition: a sub-path holding *every*
operation in its group does not split, because there is no sibling verb for the plain
one to be confused with (`pro app-request get` and
`pro service-discovery-enrollment get` stay as they are).

| new command | endpoint |
|---|---|
| `pro activation-code organization-name` | `/v1/activation-code/organization-name` |
| `pro app-installers global-settings` | `/v1/app-installers/global-settings` |
| `pro csa token` | `/v1/csa/token` |
| `pro enrollment adue-session-token-settings` | `/v1/adue-session-token-settings` |
| `pro local-admin-password settings` | `/v2/local-admin-password/settings` |
| `pro managed-software-updates-plans feature-toggle` | `/v1/managed-software-updates/plans/feature-toggle` |
| `pro self-service settings` | `/v1/self-service/settings` |
| `pro self-service-plus settings` | `/v1/self-service-plus/settings` |
| `pro sso-settings cert` | `/v2/sso/cert` |

Their verbs are plain again, so `pro sso-settings cert get|create|update|delete` and
`pro sso-settings cert download|parse`. Every moved invocation is in the table above.

**Four retired resource names now redirect two tokens deep.** A cobra alias is a name
on one command, so it can only resolve to a direct child of `pro`; these four are
registered as hidden second instances of the nested subtree, built from the same
generated constructor so the redirect cannot drift from what it redirects to. Each
warns and works: `pro sso-settings-cert`, `pro app-installer-global-settings`,
`pro self-service-settings`, `pro account-driven-user-enrollment-session-token-settings`.
`pro self-service-settings get` in particular resolved *correctly* before the nesting
and would have answered `unknown command "get"` after it.

**Two silent meaning changes are repaired by this, both introduced by the rename
above and neither visible to a test that only checks that a command resolves:**

- `pro sso-settings download` sent `GET /v3/sso/metadata/download` — the SAML metadata
  — before the rename and `GET /v2/sso/cert/download` after it, because the
  certificate's `download` won the collision and the metadata download was
  disambiguated away to `v-3-metadata-download`. It sends the SAML metadata again, and
  the certificate is at `pro sso-settings cert download`.
- `pro sso-settings delete` no longer exists rather than deleting the certificate.

#### 3 paths became command groups — help text at exit 0

The sharpest edge of the rename, because the failure is silent in the worst way: the
path still resolves, still exits **0**, and prints usage text where it used to return
data. A script piping one of these into `jq` gets help.

| was (returned data) | is now | the data is at |
|---|---|---|
| `pro csas token` | the `pro csa token` group | `pro csa token get` |
| `pro local-admin-passwords settings` | the `pro local-admin-password settings` group | `pro local-admin-password settings get` |
| `pro managed-software-updates-plans feature-toggle` | the `… feature-toggle` group | `pro managed-software-updates-plans feature-toggle get` |

A parent that prints help on exit 0 is this CLI's convention everywhere — `pro
categories` does it too — so a bare invocation still does exactly that; what changed is
that these three specific paths used to be leaves.

**An invocation that asked for data is refused instead.** Where `--output`, `--field`,
`--select` or `--out-file` reaches one of the three, it exits 2 naming the leaf rather
than printing help at 0 — so `pro csas token -o json | jq -r .value` fails at the CLI
with the answer in it, instead of feeding jq a usage message. A bare `pro csa token`,
and a typo beneath it, are unchanged.

#### 3 `apply` commands are gone, and none of them worked

`apply` is synthesized from a resource's `list`, `create` and `update`. Where a
resource split, those three no longer co-reside, so it is no longer generated:
`pro app-requests apply`, `pro enrollment-settings apply` and
`pro managed-software-updates-plans apply`.

The last is worth stating plainly, because it was **destructive rather than merely
absent**. `/v1/managed-software-updates/plans` publishes a collection `POST` and a
`{id}` `GET` and no update at all, so the only `PUT` the resource carried was the
feature toggle's — and `apply` composed it. Given the name of an existing plan it
resolved the name to an id and then `PUT` the plan document at
`/v1/managed-software-updates/plans/feature-toggle`, replacing the tenant's managed
software update feature toggle. There is no plan update endpoint to point a working
`apply` at; use `create`.

#### 19 write operations that were being deleted at generate time now ship

A name held by two operations was resolved by keeping one and **discarding the
other**, with a warning on stderr and exit 0. That is the wrong resolution
whichever operation loses, and on this branch the loser was twice the resource's
own root:

- **`pro enrollment-customization create`, `update` and `delete` addressed an
  LDAP panel.** The `enrollment-customization` tag covers a singular root
  carrying four panel families under `{id}` — ldap, sso, text and all — and the
  plural `/v2/enrollment-customizations` carrying the customization's own CRUD.
  Nine operations wanted three names, the panels' won, and the customization's
  POST, PUT and DELETE stopped existing. `apply` was built on top of that:
  it resolved a **customization** name to a customization id, substituted it into
  `{panel-id}` and `PUT` the document at an LDAP panel path, with `{id}` never
  substituted. Same shape as the `managed-software-updates-plans apply` defect
  above, on a different resource.
- **`pro computer-prestage-scopes create-scope` and `update-scope` were gone**,
  and the same two on `pro mobile-device-prestage-scopes`. `GET`, `POST` and `PUT`
  all sit on `/v2/computer-prestages/{id}/scope`, so the disambiguation had
  nothing left to separate them with once the collection-level `GET .../scope`
  took `scope` — and gave all three the same name.

A resource's plain verbs are its root's, so the root keeps `create`, `update` and
`delete` and a sub-path's write is qualified by the segment that owns it. Where
two methods share one path, the method separates them, as it already did for a
collision on the canonical path. Nothing is dropped in either case.

The panel writes that *did* hold the plain verbs are renamed rather than
restored: `pro enrollment-customization ldap-create`, `ldap-update` and
`all-delete` are what `create`, `update` and `delete` used to send.

The same resolution restored **twelve more operations `main` never shipped
either**, every one the loser of a collision between two sub-paths:

| command | endpoint |
|---|---|
| `pro enrollment-customization sso-create` / `sso-update` / `sso-delete` | `POST` `…/{id}/sso`, `PUT`/`DELETE` `…/{id}/sso/{panel-id}` |
| `pro enrollment-customization text-create` / `text-update` / `text-delete` | the same three on `…/{id}/text` |
| `pro enrollment-customization ldap-delete` | `DELETE …/enrollment-customization/{id}/ldap/{panel-id}` |
| `pro packages manifest-delete` | `DELETE /v1/packages/{id}/manifest` |
| `pro venafi proxy-trust-store-create` / `proxy-trust-store-delete` | `POST`/`DELETE /v1/pki/venafi/{id}/proxy-trust-store` |
| `pro patch-software-title-configurations dashboard-delete` | `DELETE /v3/patch-software-title-configurations/{id}/dashboard` |
| `pro computer-inventory attachments-delete` | `DELETE /v4/computers-inventory/{id}/attachments/{attachmentId}` |

**One invocation changes meaning and is refused rather than left to run.**
`enrollment-customizations` and `enrollment-customization-panels` both resolve
onto the one `enrollment-customization` the tag merges them into, and both
shipped a `create`, an `update` and a `delete`. Only one can keep the plain verb
and it is the resource's own root, so those three under the *panels* spelling
refuse with the panel command to use instead — `pro enrollment-customization-panels
create` names `pro enrollment-customization ldap-create`. The panel reads under
that spelling (`all`, `ldap`, `sso`, `text`, `markdown`, `parse-markdown`) are
unchanged.

The surviving 16 drops are pinned in
`generator/parser/testdata/dropped-operations.tsv`, with the reason for the two
that are not simple collisions between sub-paths. A new one fails the build; a
root operation losing its name to a sub-path's fails it outright.

#### No command name carries an API version

Three did. Two operations whose derived names collide are separated by the parts
of their paths that differ, and that included the version segment — so
`/v2/mobile-device-prestages/{id}/scope/delete-multiple` came out as
`v-2-scope-delete-multiple`. A version in a command name is the thing this
change exists to remove: it is how the CLI came to send `/v3` while `/v4` was
the served version.

A version segment names no resource and no longer enters a name. Where a
collision is between two sub-paths and **neither** addresses the resource's own
root, no operation has a claim on the plain verb and each is qualified by the
thing it acts on:

| was | now | endpoint |
|---|---|---|
| `pro mobile-device-prestages delete-multiple` | `pro mobile-device-prestages attachments-delete-multiple` | `POST /v3/mobile-device-prestages/{id}/attachments/delete-multiple` |
| `pro mobile-device-prestage-scopes delete-multiple` | `pro mobile-device-prestages scope-delete-multiple` | `POST /v2/mobile-device-prestages/{id}/scope/delete-multiple` |

Both old invocations refuse and name both replacements, since they collapse onto
one path after alias resolution and went to different places. The bare
`delete-multiple` deleting attachments on one prestage resource while its
computer-prestage sibling's removed scope is also gone with it.

#### The two v4 device actions take their own names, and the deprecated v1 pair is dropped

`POST /v1/computer-inventory/{id}/erase` and `remove-mdm-profile` are declared
`deprecated` upstream and withdrawn from the gateway; `POST
/v4/computers-inventory/{id}/…` serves both and is published. They are the same
endpoint at two path *shapes* — v4 renamed the collection segment from
`computer-inventory` to `computers-inventory` — so version consolidation, which
matches on the shape, saw two unrelated endpoints and kept both.

The deprecated pair took the plain `erase` and `remove-mdm-profile` names, which
had three consequences. The served pair came out as
`v-4-computers-inventory-erase` and `v-4-computers-inventory-remove-mdm-profile`.
`pro.go` suppresses a generated `erase`/`remove-mdm-profile` in favour of the
hand-written pair that targets by serial, name or group — so it suppressed the
*deprecated* operations and the served twins shipped beside `pro comp erase` and
`pro comp remove-mdm`, without the `--confirm-destructive` gate the hand-written
pair require for a bulk destructive operation. And those two paths are the only
ones in the document whose collection segment is singular, so they alone decided
the resource's own name.

The v1 pair is now dropped at ingest, the same way inventory-preload v1 already
was and for the same reason. `main` shipped neither (both were suppressed there
too, as whole resources), so no capability moves.

#### Also fixed by the same change

- `pro computer-groups` was registered twice — the generated registry called
  `NewComputerGroupsCmd` for two filenames naming one resource, so `pro --help`
  printed the row twice and every path beneath it resolved to the first copy. Resource
  identity comes from the paths now, so there is one subtree.
- `GET /v1/branding-images/download/{id}` was unreachable: the old per-file filter
  discarded it with a warning on every generate. It ships as `pro branding download`.
- **A stale wiring key is now a build failure rather than an extra command.** The
  three helpers that wire hand-written commands onto generated parents keyed on
  names and answered a stale key by doing nothing — so a suppression left the
  generated command in place and a replacement was added beside it. Every miss is
  recorded and a test fails on a non-empty record, which found two more dead keys
  the rename had left (`apply` and `get-by-name` on the former `jamf-protects` and
  `jamf-protect-deployment-tasks`); both are removed.

### Fixed — a destructive `x-action` no longer sends `DELETE`, or describes itself as a delete

- **`--from-file` and `--group` on a destructive action sent the wrong HTTP
  method.** The bulk block is shared by a plain `delete` and by an `x-action` that
  happens to be destructive, and it hardcoded `DELETE`. So
  `pro mobile-device-groups erase --from-file ids.txt` sent
  `DELETE /v2/mobile-device-groups/{id}/erase` — a `POST`-only endpoint — and
  reported `Deleted` for each entry. It sends the operation's own method now.
  Present in `v1.28.0` and earlier.

- **Confirmation prompts named the wrong action.** The bulk prompt said it would
  "delete N <resource>", which for an erase reads as removing inventory records
  rather than wiping devices, and the single-item prompt interpolated the raw
  operation name into the sentence. A `DELETE` still reads `This will delete …`;
  anything else quotes its own action — `This will run "erase" on mobile-device-group
  "…" (id: …)`. There is no English verb for `remove-mdm-profile`, and inventing one
  is how a prompt comes to describe a different action from the one it performs. The
  `--from-file` and `--group` help text and the `[dry-run]` lines follow the same rule.

### Fixed — the deprecation warning fires wherever a global flag is placed

- **A flag between the product token and the resource token silenced both the
  deprecation warning and the moved-verb refusal.** The resource token was read out
  of `argv` by treating anything not starting with `-` as the resource, so
  `jamf-cli pro -p ci-svc icons get 1` read the profile name as the resource. Cobra
  does not require a global flag before the subcommand and `-p` is the documented way
  to select a profile, so that is an ordinary invocation — and for it, the migration
  signal the whole 2027-03-09 window rests on was absent for all 100 retired names,
  and `pro -p … enrollment-customization-panels update 1 2` got cobra's bare arity
  error instead of the refusal naming its replacement. Whether a flag consumes the
  next argument is now asked of the command's own flag set, by cobra's rules.


### Breaking — `--file` is renamed to `--from-file` on Platform and Security Cloud writes

- **Every request-body flag is now `--from-file`.** Platform and Security Cloud commands
  took `--file` while Jamf Pro, Classic, Protect and School took `--from-file`, and
  `pro platform-device-groups` carried both — `patch` and `patch-members` took `--file`,
  `apply` took `--from-file`. 41 flag registrations were renamed, and all 361 commands that
  read a body from a path now agree on the name.

  **There is no compatibility alias.** `--file` answers `unknown flag` (exit 2) on the
  affected commands, so a script passing it fails at once instead of two spellings surviving
  in parallel. The migration is a rename:

  ```bash
  # Before
  jamf-cli security ztna-gateways create --file gateway.yaml
  # After
  jamf-cli security ztna-gateways create --from-file gateway.yaml
  ```

  Passing the old name prints `hint: did you mean --from-file?` before the error. The hint is
  looked up rather than guessed by edit distance, which would have suggested `--field`.

  Affected: every generated `platform` and `security` command that takes a body, plus
  `pro platform-device-groups patch` and `patch-members`.

  **`--file` is unchanged on the 11 commands where it names an upload payload** —
  `pro packages upload`, `pro icon upload`, `pro inventory-preload upload` and
  `csv-validate`, `pro computer-inventory upload`,
  `pro computer-extension-attributes upload`,
  `pro enrollment-customization-images upload`,
  `pro mobile-device-prestages upload`, `pro self-service upload`,
  `protect analytics import` and `protect unified-logging-filters import`. That is a
  different flag with the same name: a `--from-file` body can always arrive on a pipe
  instead, and a multipart upload cannot, because the transport needs a filename and a
  length. Renaming those too would have produced one flag name with two capabilities.

### Behaviour — Platform and Security Cloud bodies can be piped

- **`--from-file` is now optional on those commands: absent, the body is read from stdin.**
  `ReadBody` in both products was `os.ReadFile` and nothing else, where Jamf Pro and Protect
  had always accepted a pipe — so these were the only namespaces where
  `--scaffold | edit | apply` had to route through a temp file.

  ```bash
  jamf-cli security ztna-gateways apply --scaffold | vipe | jamf-cli security ztna-gateways apply --yes
  ```

  An **empty pipe is not a body**: a CI runner hands every process a stdin that is not a
  terminal and carries nothing, so that case still reads as "no body given" and a bodyless
  write is unaffected. A `--from-file` naming an **empty file** remains an error, unchanged.

### Added — `apply` on the platform gateway resources

- **Six gateway resources gained `apply`** (create-if-absent, update-if-present, keyed on the
  `name` in the body): `security dns-zones`, `security ztna-apps`, `security ztna-gateways`,
  `security ztna-grouped-gateways`, `security device-groups` and `platform ai-policies`. The
  verb existed on Jamf Pro and Classic and on no gateway resource.

  Three properties a script should rely on:

  - The name is read from the body, never a flag; a body with no `name`, or a non-string or
    empty one, is refused before any request is sent.
  - Only a genuine "not found" on the exists check takes the create branch. An auth error or
    a 5xx during the lookup aborts.
  - An existing resource is confirmed before being overwritten; `--yes` skips the prompt, and
    is required when stdin is not a terminal.

  These resources update with `PATCH`, so **fields omitted from the body keep their current
  values** — unlike Jamf Pro's and Classic's `apply`, which replace. Each command's `--help`
  states which semantics it has. `platform ai-policies` is the exception within the
  exception: its server replaces `settings` wholesale despite the merge-patch content type,
  and its help says so.

  Two things `apply` cannot do for you, both stated in each command's `--help`:

  - **The exists check and the create are separate requests**, so two runs racing on the same
    absent name — a CI retry, or concurrent jobs — can both create one. Serialise `apply` per
    resource where that matters. No spec here declares a name-uniqueness `409`, so the server
    is not a backstop.
  - **`platform ai-policies apply` writes a draft.** A draft is not enforced until it is
    published, so follow a successful apply with `platform ai-policies publish <id>`. `apply`
    does not publish: publishing is non-idempotent (nothing pending answers `409`), and
    folding it in would leave no way to write a draft alone.

- **The Jamf Security Cloud Radar commands deliberately have no `apply`.** That surface is
  singletons whose `update` is already an idempotent create-or-replace (`stream`, `status`),
  actions (`risk override`, `verification trigger`, `device-lifecycle purge`) and read-only
  documents (`well-known`, `jwks`) — there is no named collection to resolve a name against.

### Behaviour — name lookups report an ambiguity or an unaddressable match instead of guessing

- **A name repeated across a page boundary is now an error.** `--name` and `apply` decided
  matches one page at a time and returned on the first page holding exactly one, so a name
  appearing on both sides of a 100-item boundary resolved to whichever copy sorted first,
  silently. Matches accumulate across every page before the decision, so the ambiguity error
  fires wherever the copies sit.

- **A name that matches an item the list returns no ID for is no longer reported as "not
  found".** Absence and "found it, cannot address it" had the same answer, which reads as a
  typo to a person and as "create it" to `apply`. It now says the list returns no ID for the
  items it matched. Security Cloud's device groups are the live case: the implicit
  "Default Group" is returned with a name and no `id`, so
  `security device-groups apply --set "name=Default Group"` refuses rather than creating a
  second group named the same. Stored groups are unaffected — they carry an `id` and resolve
  normally.

- **The ambiguity error no longer tells the caller to "pass the positional ID".** `apply`
  takes no positional, so on the command that most needs the message it named a remedy the
  command has no argument for.

### Fixed — Platform writes

- **`apply`'s update sends the same `Content-Type` as the resource's own `patch`.** It was
  derived from the media type the spec literally declares, so `platform ai-policies apply`
  sent `application/json` to the URL `platform ai-policies patch` sends
  `application/merge-patch+json` to (the SDK's default for any bodied `PATCH`) — a
  divergence on the wire between two commands documented as having the same semantics.
- **`apply` carries the `jamf:scopes` annotation its sibling verbs carry.** Without it the
  scope-level note could not fire for `apply`, and `apply` was absent from the `scopes` field
  in `jamf-cli commands -o json`.
- **`--set` on a platform command refuses to overwrite a non-object field** rather than
  discarding it: `--set scope.owner=alice` over a body whose `scope` is a string now errors,
  matching what the Security Cloud commands already did.
- **A piped body over 10MB is refused instead of truncated.** The cap returned `io.EOF`,
  indistinguishable from real end-of-input, and a cut JSON or YAML document can still parse —
  so a long desired-state document lost its trailing fields and reported success.
- **A failure to inspect stdin is now reported.** It was collapsed into "no input", so a
  command could silently send no body where one had been piped.
- **The renamed-flag hint reaches `-o json`.** It was written straight to stderr, so it
  landed ahead of the JSON error block in combined output and the envelope's `hint` field
  was empty — on the one hint a CI job would most want to read structurally.

## v1.28.0

The Jamf Platform API reached general availability on 2026-09-03. Most of this release is
that migration; **[docs/guides/platform-api-ga.md](docs/guides/platform-api-ga.md) is the
migration guide** and carries the detail, the error messages verbatim, and the reasoning.

The gateway coverage and Platform API surface in this release come from
`jamfplatform-go-sdk` v0.22.2 (GitOps build v2082): Jamf Pro API 11.31.0 at 476 paths and
700 operations, Classic API 11.28.0 at 270 paths and 589 operations. Which endpoints the
gateway publishes decides which commands are refused, so that surface is what the numbers
below are counted against — `jamf-cli commands -o json` reports the answer for the binary
in hand.

### Breaking — Platform gateway (only affects `auth-method: platform` profiles)

- **The gateway base URL is `https://{region}.api.jamfcloud.com`.** The pre-GA
  `https://{region}.apigw.jamf.com` is retired, and the `/api` path segment it required is
  gone. A profile still naming the old host is refused **by name** before any request is
  sent, because the wire symptom is an edge-level 403 during the token exchange that names
  neither the host nor the reason. The CLI does not rewrite the URL for you.
- **Public-beta credentials were revoked at GA.** Register a replacement API integration in
  Jamf Account; a beta client cannot be migrated. `jamf-cli platform setup` writes a fresh
  profile.
- **Three scope levels, one per profile: organization, platform environment, tenant.**
  `environment-id` (new; `--environment-id`, `JAMF_ENVIRONMENT_ID`) is the level to prefer;
  `tenant-id` is the legacy one; organization scope carries no ID and is selected by the
  gateway host alone. Supplying two levels at once is refused, in the environment as well as
  in a profile. The scope now travels in an `X-Environment-Id` / `X-Tenant-Id` header
  instead of a `/tenant/{tenantId}` URL segment.
- **67 Jamf Pro and Classic commands are refused on a gateway profile**, before a request is
  sent, with exit code 8 (`Refused by policy`) — they are outside the gateway's published
  API. Several of them still answer today; that is transitional, and refusing now is cheaper
  than the eventual bare `403 BAD_PERMISSIONS`. The remedy is a second `oauth2` profile
  against the instance.
- **24 of those are MDM device actions**, which is the refusal most likely to be felt:
  `pro mobile-devices` loses `lock`, `restart`, `shutdown`, `enable-lost-mode`,
  `disable-lost-mode`, `play-lost-mode-sound`, `clear-passcode`,
  `clear-restrictions-password`, `delete-user`, `log-out-user`, `unlock-user-account`,
  `apply-redemption-code`, `refresh-cellular-plans`, `request-mirroring`, `stop-mirroring`
  and `settings`; `pro computer-inventory` loses `lock`, `restart`, `shutdown`,
  `enable-remote-desktop`, `disable-remote-desktop`, `set-recovery-lock`,
  `set-auto-admin-password` and `settings`. The gateway's published API declares GET on
  those paths but not POST, so the refusal is per method rather than per resource.
  `pro comp erase` and `pro comp remove-mdm` are hand-written and are **not** affected.
  The other 35 are `pro api-integrations` (7), `pro classic-computer-configs` (7),
  `pro api-authentication` (6), `pro api-roles` (6),
  `pro jamf-pro-initialization` (3), `pro api-role-privileges` (2), and one each of
  `pro environment-type`, `pro macos-managed-software-updates`, `pro mdm commands` and
  `pro sso-oauth-session-tokens`. That is 59 in total rather than the 67 this file
  recorded before spec-derived naming, and the gateway un-refused none of them: six were
  `pro static-computer-groups`, the withdrawn v2 command, which stops existing when every
  version of an endpoint lands in one resource, and two were `pro policy-properties`,
  whose unversioned legacy twin `/settings/obj/policyProperties` is no longer ingested at
  all while the `/v1/policy-properties` it shared a tag with is published.
  `jamf-cli commands -o json | jq -r '.[] | select(.gateway=="unserved") | .command'`
  reports the current list for the binary in hand. `JAMF_CLI_ALLOW_UNPUBLISHED=1` downgrades
  an *unpublished* refusal to a stderr warning and sends the request anyway — a stopgap for
  one job, not a mode to settle into, and the warning it substitutes cannot be silenced. The
  reverse direction is refused the same way: a Platform-only command on an instance profile
  exits 8 naming the profile, its resolved auth method and `platform setup`, rather than
  reading as a credential problem.
- **Exit code 8 is new** — `Refused by policy`, for a command that is correctly invoked but
  cannot be served by the resolved credentials. Distinct from 2, which is also every cobra
  flag error.
- **A profile's scope level is no longer attached to credentials supplied for the
  invocation.** If `JAMF_CLIENT_ID` is set for the invocation, the profile's
  `environment-id` and `tenant-id` are both ignored rather than one being used. An
  integration is created at one level in Jamf Account and its credential carries that
  choice, so a profile's level describes the profile's own integration — and an
  organization-scoped credential must send no scope header at all. Before this,
  `JAMF_URL` + `JAMF_CLIENT_ID` + `JAMF_CLIENT_SECRET` for an organization-scoped
  integration sent an `X-Tenant-Id` taken from whatever `default-profile` named, and failed
  with a level the operator never chose. Supply the level for those credentials with
  `--environment-id` / `--tenant-id` or `JAMF_ENVIRONMENT_ID` / `JAMF_TENANT_ID`; the
  resulting error names the profile whose level was passed over and both ways to set one.
  A profile holding `client-id` with only `JAMF_CLIENT_SECRET` injected is unaffected — the
  client ID names the integration, so that is still the profile's. So is a profile whose own
  `client-id` is an `env:JAMF_CLIENT_ID` reference, which is the config's documented way for
  a profile to read its client ID out of the environment: the variable is then how that
  integration supplies its own credential, not a second integration displacing it, so the
  profile keeps its level. A `file:` reference is compared the same way — it is a plain
  read — and when that read fails the error names the path and the read error rather than
  blaming `JAMF_CLIENT_ID`, since nothing later in the invocation opens that file.
  **A profile whose `client-id` is a `keychain:` reference is affected, and that is the
  shape `platform setup` writes.** Resolving one can prompt, on a path that by definition is
  not using the profile's credentials, so the comparison cannot be made and the level is
  withheld. If you run such a profile in a shell that also exports `JAMF_CLIENT_ID` for the
  same integration, either drop that variable so the profile resolves its own credential, or
  set `JAMF_ENVIRONMENT_ID` / `JAMF_TENANT_ID` beside it. The error names the profile, the
  level it holds and both remedies.
- **A platform command's 403 now exits 5, not 1.** Platform commands previously returned the
  SDK's error untouched, so the one failure with a specific remedy exited with the generic
  code.
- **`security device-groups update` prints nothing on success.** It sends
  `PUT /securitycloud/v2/groups/{groupId}`, which answers `204` with no body, where the
  deprecated v1 form answered `200` with the updated group. The published spec has now
  withdrawn v1's update and list outright, and the v2 update handler — broken since it
  appeared, answering `403` and then a `404` on a group its own list returned — was fixed on
  2026-09-04, so the CLI no longer withholds the operation. `list` and `update` send v2;
  `create`, `get` and `delete` stay on v1. Anything reading the group out of an `update` has
  to follow with a `get`.

### Breaking — everything else

- **`--out-file`, `--select`, `--compact`, `--field`, `--quiet` and `--no-hints` now take
  effect on 27 commands that parsed and discarded them.** Those commands built their own
  output formatter, which receives none of the global flags, so each flag was accepted and
  then ignored. `--out-file` exited 0 and left the file at 0 bytes while the payload went
  to stdout. Affected are the `pro report` family, `pro audit`, `pro overview`,
  `protect overview`, `school overview`, `pro group-tools`, `pro classic app-usage`,
  `multi` and `commands`.

  Two changes are visible to a script. A job that passes `--out-file` and reads stdout now
  reads nothing, because the payload goes to the file it asked for. A job that passes
  `--select` or `--compact` and parses whole rows now receives narrowed rows.

  `--select` and `--compact` narrow rows. They do not remove them, so a projection that
  matches no field in a row leaves that row present and empty. The 200+ generated commands
  have always behaved that way. The details:
  - Under `table`, `csv` and `plain`, the column set is the union across rows while either
    flag is active, because both flags make rows heterogeneous. A field only some rows
    carry used to be a column for none of them. Without a projector the first row still
    decides, as before.
  - Under `table`, `csv`, `plain` and the single-object detail view, a projection that
    matches nothing in any row now renders nothing. It used to print a row count above a
    blank header. The miss is named on stderr, and neither `--quiet` nor `--no-hints`
    silences it, because under those four formats no other output reports it.
  - The large-result hint is withheld while either flag is active. It recommended more of
    the flag that had just emptied the output.
  - `--select` bypasses the table's default-column heuristic, so a named field renders
    without `--wide`.
- **`pro audit -o raw --out-file f` and `-o xml --out-file f` now write a table, not JSON.**
  That command marshalled its rows and handed the bytes to `PrintRaw`, which passes JSON
  through unchanged for those two formats. It reached that branch only when `--out-file`
  was set. The shared formatter has no case for either format, so it renders a table. Use
  `-o json` for JSON. Without `--out-file` the output is byte-identical, and so is every
  other format. The six `pro report` multi-section reports already rendered `raw` and `xml`
  as tables. What changed for them is that `--out-file` now receives the output.
- **A section banner is no longer written for a format a parser reads.** `json`, `yaml`,
  `ndjson`, `csv` and `plain` get no `── Section ──` lines. `xml` and `raw` keep theirs,
  because they render as tables. A multi-section report still emits one block per section
  under `csv`, `ndjson` and `plain`, now with nothing between the blocks. Use `-o json` or
  `-o yaml` for a single parseable file. All seven `pro report` commands with more than one
  section say so in their `--help`, from one shared string, and so does `multi`, whose
  aggregation renders one section per merged key.
- **`pro report patch-status --scan-failures -o json` now writes one array of labelled
  sections.** It wrote three separate documents into the same file, which `jq` rejects at
  the second one. Each section carries a `fetch_error` field. An empty section and a
  section whose fetch failed used to be byte-identical, so a scheduled job read "no
  failures" from a check that never ran. An empty section is `[]`, never `null`.
- **`--field` naming a field no row carries now reports that on stderr.** `--quiet` and
  `--no-hints` do not silence it. A `--field` miss produces no output at all, so nothing
  else distinguishes a wrong field name from an empty result.
- **`--field` now extracts on `pro group-tools export` and on a `multi` aggregation.** Both
  render through their own `--format` argument rather than the global `-o`, so the flag was
  parsed, listed in Global Flags and discarded. A `multi` aggregation applies it per
  section, the way a `pro report` section does.

- **A command that documents no positional argument now refuses one**, with exit 2
  (`usage`). 736 leaf commands used to accept any positional and discard it in silence, so
  `pro categories list junkarg` ran the list and `pro backup /tmp/out` ran a whole backup
  and ignored the directory that `--output` takes. A wrapper or a CI job that passes a
  stray token now fails where it used to succeed. Remove the argument, or move it to the
  flag that takes it; the refusal names the command and the value, `--help` lists every
  flag, and any required flag that is also unset is named too — so
  `pro diff staging production` still reports `--source` and `--target` rather than
  replacing that answer with the positional complaint. That clause is conditional: a
  refusal that can suggest a subcommand instead names no flag, so
  `pro backup list-resourcez` suggests `list-resources` and stays silent about `--output`,
  which there is the root format flag rather than a directory. The value is replaced with
  `<redacted>` when the command registers a credential flag (`--new-password`, `--pin`,
  `--unlock-token`) or when the positional is itself a `key=value` pair whose key names a
  credential, such as a dropped `--set account.password=…`, because this message reaches
  stdout as JSON when output is piped and from there a CI log. The key is read
  case-insensitively and split on both separators and camelCase boundaries, so `TOKEN=`,
  `CLIENT_SECRET=`, `CLIENTSECRET=` and `clientsecret=` all redact. A positional redacts too when a
  supplied `--set` element itself carries no `=`, which is the signature
  `--set <key> <value>` leaves behind when the `=` is lost: cobra takes the key as the
  flag's element and the credential becomes the positional. The test is on the flag set
  rather than on the value, so a secret containing `=` — base64 padding, or the character
  itself — is still covered, while a mistyped filename beside a well-formed pair still
  names itself. A command that documents a
  placeholder is unchanged for an ordinary invocation. Under `--scaffold` it is now bounded
  too: 43 classic and platform leaves used to accept and discard any number of extra
  positionals with that flag set, and now enforce the declared ceiling, so
  `<resource> update <id> extra --scaffold` is refused where it exited 0 before. `multi`
  still forwards every positional to its inner command, and 22 singleton `delete` and
  `history` commands had their `--help` examples corrected, because those examples showed
  an id the command never accepted. A further 17 `--help` example lines on 12 resources are
  corrected for the same reason on the other half of the line: a `create`, `update` or
  `patch` example opened with a `get` the resource does not ship, or one with a different
  arity, so half of a documented pipe could not run.
- **An error raised before the output format is resolved now follows `default-output`.** The
  `-o` default was `json`, so a flag-parse error and an argument refusal always rendered the
  JSON envelope on stdout — even where the profile pinned another format, and even on a
  terminal — while the same profile's `RunE` errors, raised after resolution, rendered plain
  text. Two answers to one question. The default is now empty, meaning unresolved, and both
  paths answer the same way. A profile pinning `default-output` to a non-json format
  therefore gets plain text on stderr for those two error classes where it previously got
  the envelope on stdout, **piped runs included**: a wrapper parsing that envelope must key
  on the plain-text form, or pass `-o json` explicitly. With no `default-output` set a piped
  run still gets the envelope, so an unconfigured CI job is unaffected.
- **A cobra usage error now exits 2, not 1.** Four classes move: a missing required flag, a
  flag group with no member set, a flag group with mutually exclusive members set together,
  and the wrong number of positional arguments. An unknown flag and an unknown subcommand
  already exited 2, so the two halves of one mistake answered differently. `pro backup
  --nosuchflag` exited 2 and `pro backup` with no `--output` exited 1. Exit 1 is the generic
  failure code, so a script could not tell a bad invocation from a failed request. The scope
  is wide: 48 call sites declare a required flag, 118 declare a flag group, and 671 validate
  an argument count. A wrapper that treats exit 1 as a bad invocation must key on 2 instead.
- **`pro ddm-reports declaration get` and `pro ddm-reports device get` are removed.** Both
  endpoints were deprecated upstream in favour of a sibling the CLI already shipped:
  `declaration devices <id> --filter …` and `device declarations <id> --filter …`. The
  successors declare `filter` required, so there is no unfiltered read left; use
  `--filter 'active=in=(true,false)'` where you want everything.
- **`pro comp erase` and `pro comp remove-mdm` send `/v4/computers-inventory/{id}/…`**,
  where they were pinned to `/v1/computer-inventory/{id}/…`. Neither has a version fallback,
  so an instance that does not serve v4 answers 404. Flags, targeting and confirmation are
  unchanged.
- **`jamf-cli config list` no longer has a `tenant-id` column** in `table`, `csv` and
  `plain` output; it always has `environment-id` and `default` instead. `-o json` and
  `-o yaml` are unchanged and still carry `tenant-id`. Parse the JSON, not the table.
- **Building from source needs Go 1.27** (`go.mod` declares `go 1.27.0`, up from `1.26.6`).
  The default `GOTOOLCHAIN=auto` fetches it; a pinned older toolchain fails. Binary releases
  are unaffected.

### Changed

- **An empty list prints `[]`, not `null`.** Applies to Pro's `list --all` and to every
  Platform and Security Cloud list. `jq` pipelines previously failed with "Cannot iterate
  over null" on exactly the tenants where a collection was empty.
- **`pro computers-inventory` sends `/v4` instead of `/v3`**, and `get` reads the v4 detail
  endpoint. Generated subcommands retry the `/v1` path on a 404 and warn on stderr.
- **Eight Classic patch-management commands are no longer refused on a gateway profile.**
  `pro classic-patch-reports` (both subcommands), `pro classic-patch-titles` `list`, `get`,
  `update`, `delete` and `apply`, and `pro classic-patch-policies list` were refused in
  v1.28.0 because a published Classic API build had withdrawn `/patches`, `/patchreports`,
  `/patchsoftwaretitles` and two `/patchpolicies` reads. Upstream restored all of them, on
  the stated reasoning that patch management is where Classic API callers are most
  concentrated. `pro classic-patch-titles` also gained `--scaffold` and `--set`, its body
  schema having come back with the endpoints.
  `/pro/v3/computers-inventory` was restored in the same build — 13 operations, deprecated
  too close to the removal for callers to reach v4 — which changes the coverage manifest and
  no command, since the CLI sends v4.
- **`pro computer-inventory-collection-settings custom-path` sends v2, whose `scope` accepts
  only `APP`.** v1 served `[APP, FONT, PLUGIN]`, so a `FONT` or `PLUGIN` path that worked
  before now answers a 400. Nothing in `--help` says so — the Pro generator renders no
  allowed-value lists.
- **A 403 names the permission in the vocabulary of the API that answered** — capability
  permissions with Jamf Account's own section and permission names for a gateway request,
  Jamf Pro API-role privilege names for an instance request. `commands -o json` carries both
  (`privileges`, `gatewayPrivileges`, `gatewayPermissions`) plus an `api` field naming the
  serving API.
- **`commands -o json`'s `gatewaySuccessor` reports a value**, where it was documented but
  emitted on no row at all: six refused commands now name the replacement the runtime
  refusal and the `--help` caveat already named. A script reading the catalog rather than
  running `--help` was told only that a command was `unserved`.
- **A CDN/WAF refusal is reported as one** rather than as `permission denied (HTTP 403)`
  with an HTML page in the message and a hint about API roles. Known triggers: `file://`
  anywhere in a request body, `.pkg` upload content, a burst of writes. A `.pkg` upload
  through a gateway profile is currently refused; upload through an instance profile.
- **`-n, --dry-run` is honoured on Platform and Security Cloud writes**, printing method,
  resolved path and body to stderr. Hand-written platform writes with no per-command
  preview are refused under `-n` rather than executed.
- **`-v` labels retried requests** with the attempt number and the wait, so a slow call is
  distinguishable from a retry sequence.

### Added

- **Classic writes gained `--scaffold` and `--set`**, plus required-field and enum lists in
  `--help`: 114 of 117 `create`/`update`/`apply` commands, across 44 of the 54 Classic
  resources. The three without them are `pro classic-computer-configs` `create`, `update`
  and `apply`: the resource is dead, and a Jamf Pro instance 404s it too.
  `--set` builds the whole body and is mutually exclusive with `--from-file`; it refuses an
  unknown field, an out-of-enum value and a credential field, because the Classic API
  answers `201` and silently drops or defaults the first two.
- **`commands -o json` carries a `scopes` array** for every generated Platform command: the
  Jamf Platform API scope levels its spec declares a credential must be created at
  (`environment`, or `environment,tenant`). Nothing is refused on it — the specs are
  currently stricter than the gateway, and a tenant credential still reaches
  `pro platform-devices list` and `pro platform-device-groups list` despite both being
  declared environment-only — so it is reported, appended to the gateway's own scope errors,
  and used to assemble `platform setup`'s summary. Absent means the spec is silent, not that
  any level works: the three Jamf Account APIs declare nothing and are organization-scoped.
- **`--file` accepts YAML** on generated Platform and Security Cloud commands, matching
  Pro's `--from-file`.
- **`protect backup` and `protect restore`** capture and replay a whole Jamf Protect tenant.
  Each object is written to its own file in the same portable, name-referenced form the
  matching `export` produces; restore walks the tree in dependency order, resolving
  references by name against the target, and never deletes. Both take `--resources` and
  `--exclude`. Backup prunes documents an earlier run left that no longer match the tenant
  (`--no-prune` keeps them), and refuses to prune a directory another tenant has written to.
  Two files carry secrets verbatim and are written `0600` — `action-configs` and
  `data-forwarding`, because an HTTP report client's bearer token cannot be redacted without
  breaking restore. Note git records no non-exec permissions, so a clone of a backup repo
  hands them back `0644`. A mixed result exits 7.
- **`protect analytics overrides`** manages the tenant overlay on Jamf-managed analytics
  (`tenantSeverity`, `tenantActions`) — `list`, `get`, `set`, `apply`, `export`, `clear`.
  `analytics list` reports Jamf's baseline severity rather than the effective one, and
  `analytics export` omits the overlay entirely, so this is the only route to the
  customisation a tenant actually made.
- **Jamf AI Governance:** `platform ai-policies`, `platform ai-tools`.
- **Jamf Account:** `platform account-licenses`, `deal-registrations`,
  `distributor-configuration`, `distributor-purchase-orders`, `distributor-quotes`,
  `sso-connections`, `sso-domains`. Organization scope, and US-only — a non-US profile is
  refused before sending.
- **Platform audit:** `platform audit` (environment scope only). Not `pro audit`, which runs
  health checks against a Jamf Pro instance.
- **Jamf Security Cloud through the gateway:** `security dns-*`, `ztna-*`,
  `content-categories`, `device-groups`, `uem-*`, `enrollment-activation-profiles`. Every
  `security` command's `Short` says which API serves it — platform gateway or Radar — since
  the two halves take different credentials.
- **App Installers on the gateway.** The endpoints are published upstream now, so the
  commands are generated from that spec rather than a reverse-engineered one and are no
  longer refused on a gateway profile. New: `pro app-installers get`,
  `app-installer-titles versions`, `app-installer-global-settings deployment-controls`,
  `history` and `add-history-note`.
- `pro ddm-reports declaration devices` and `device declarations` gained `--page`; only the
  first page was reachable before.

### Fixed

- A YAML request body carrying a timestamp scalar or a non-string mapping key — both legal
  YAML, neither expressible in JSON — was reported as malformed input.
- `pro platform-device-groups` name lookups built a stale `/tenant/{id}/` path, which
  collapsed to `/tenant//` under environment or organization scope.
- `JAMF_ENVIRONMENT_ID` now overrides a tenant-scoped profile rather than colliding with it,
  the way every other environment variable here overrides the profile.
- **`platform setup` no longer checks any product's access or permissions.** It used to
  read `content-categories` and report a Jamf Security Cloud entitlement verdict, which
  asked the wrong question: a capability permission is granted per operation when the
  integration is created, so one read says nothing about the other 28 resources, and a
  gateway 403 already names the permission it wanted in the wording Jamf Account's picker
  uses. The verdict was also wrong in the ordinary case. Wire-checked in one organization, a
  **tenant** credential answered `BAD_PERMISSIONS` there while an **environment** credential
  in the same organization answered 200 — so the summary told a demonstrably entitled
  organization it had no entitlement, and subtracted all sixteen Security Cloud resources
  from what the profile reaches. That credential's summary now reports 16 of the 29
  reachable, and closes by saying where a permissions answer comes from.

  Setup still validates the **scope ID**, because the token exchange sends no scope header:
  credentials that authenticate say nothing about the ID just typed, so a mis-pasted one
  saved cleanly and then refused every command. The gateway resolves the scope at the edge,
  before routing and before capability, so this is not a product check either — only
  `404 ENVIRONMENT_NOT_FOUND` and `403 OWNERSHIP_FORBIDDEN` reject an ID, and everything
  else, `BAD_PERMISSIONS` included, leaves it unjudged.
- **An unknown platform environment ID produced a bare 404 with no explanation.**
  `ENVIRONMENT_NOT_FOUND` is a 404, so it reached neither the 403 privilege hint nor the
  missing-scope note. A tenant ID pasted into `environment-id` is exactly this, and it is
  now annotated with the two IDs it confuses and the profile field to check.
- **`platform setup`'s closing summary said two things that were not true.** A tenant-scoped
  profile was told it served "the Pro API and Platform API commands" when six Platform specs
  declare environment scope only, and an organization-scoped profile was told AI Governance
  was served, which answers `400 REQUEST_CONTEXT_NOT_PROVIDED` with no scope header. The
  summary is now assembled from the commands' declared scope levels, so it cannot drift from
  the specs they were generated from.
- **`platform setup` called a refused environment ID a tenant ID.** The gateway's
  `OWNERSHIP_FORBIDDEN` is reachable at either level, and the refusal was worded tenant-only
  while the closing summary named the same value an environment ID — so setup told an
  operator to use the prompt they had just used. Both now read from the level that was
  supplied.
- **A withheld scope level on a `school` profile reported missing credentials.**
  `school blueprints` and `school ddm-reports` need a platform client, and the school
  resolver requires a tenant ID before building one — so a level withheld by the rule above
  left no client and the command answered "this command requires platform gateway auth",
  with the profile, the client ID and the secret all present. It now names the profile, the
  withheld level, and `JAMF_TENANT_ID` / `--tenant-id` — the one level that resolver can
  build. This is the one path that sends no request, so the note the other two ladders get
  from the gateway's 400 had nowhere to appear.
- **`pro classic-macos-config-profiles --scaffold` named the wrong element inside
  `scope.jss_user_groups`.** It rendered `<jss_user_group>` where the wire answers
  `<user_group>` — an upstream typo in the Classic spec, confined to that one property while
  the resource's own `scope.exclusions.jss_user_groups` and all seven sibling resources
  carrying the same scope block declared it correctly. Corrected upstream and ingested with
  SDK v0.22.1. Writes were unaffected either way: the Classic API accepts both spellings and
  reads the scope back as `<user_group>`.
- **`pro classic-mobile-config-profiles` writes gained `display_in` inside
  `self_service.self_service_categories`, and it is the field that decides whether the
  category is stored at all.** The Classic spec had this one resource `$ref` the shared
  `category` schema (`{id, name, priority}`), where its five siblings declare the item
  inline with `display_in`; corrected upstream and ingested with SDK v0.22.2. Wire law, on
  Jamf Pro 11.31.1: a `<category>` carrying only `<id>`, or `<id>` plus `<name>`, is
  **silently discarded with a 201**, and `display_in=false` is a deletion gesture rather
  than a stored value. So `--scaffold` renders `<display_in>false</display_in>` — the
  boolean placeholder — and that is the one value in the template that must be changed
  rather than merely filled in. `display_in` is also **write-only on this resource alone**:
  the GET echoes `<id>` and `<name>` only, so a scaffold round trip cannot recover it. Both
  facts are carried in the field's description in `specs/classic/schemas.json`.
