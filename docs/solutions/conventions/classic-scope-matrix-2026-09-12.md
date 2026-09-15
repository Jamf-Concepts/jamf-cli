---
title: "Each scopeable Classic resource has its own scope shape; there are five, not two"
date: 2026-09-12
category: conventions
module: internal/scope
problem_type: wire-contract
severity: high
applies_when:
  - "Adding a scopeable Classic resource, or changing which flags a scope command takes"
  - "Deciding whether a scope category applies to a resource type"
tags:
  - classic-api
  - scope
  - validation
---

# Each scopeable Classic resource has its own scope shape; there are five, not two

## Context

`ValidateScopeCombination` had two branches — restricted software, and
everything else — so a policy accepted `--mobile-device-group` and a mobile
configuration profile accepted `--computer-group`. Both went out as a GET and a
PUT and came back as a page of HTML. Two categories the CLI could *read* had no
flag able to write them (`scope get` listed iBeacon limitations and an ebook's
target classes, and nothing could change either), and one legitimate category
was refused outright.

## The matrix

Read off the wire on Jamf Pro 11.31.1 (2026-09-12) by listing the child
elements each resource's own GET returns per section, then checked against
terraform-provider-jamfplatform's independently wire-probed schemas
(`internal/common/scope`), which agree category-for-category.

Flags are CLI spellings. `--user`/`--user-group` are the **directory** (LDAP/IdP)
categories, wire `<users>`/`<user_groups>`; `--jss-user`/`--jss-user-group` are
the **Jamf Pro** ones, wire `<jss_users>`/`<jss_user_groups>`.

| resource (`SingularKey`) | targets | limitations | exclusions |
|---|---|---|---|
| `policy`, `os_x_configuration_profile` | computer, computer-group, building, department, jss-user, jss-user-group | network-segment, user, user-group, **ibeacon** | the targets, plus network-segment, user, user-group, **ibeacon** |
| `mac_application` | same as above | network-segment, user, user-group | as above, **no ibeacon** |
| `configuration_profile` (mobile) | mobile-device, mobile-device-group, building, department, jss-user, jss-user-group | network-segment, user, user-group, **ibeacon** | the targets, plus network-segment, user, user-group, **ibeacon** |
| `mobile_device_application` | same as above | network-segment, user, user-group | as above, **no ibeacon** |
| `ebook` | computer, computer-group, mobile-device, mobile-device-group, building, department, jss-user, jss-user-group, **class** | network-segment, user, user-group | everything but class, plus network-segment, user, user-group |
| `restricted_software` | computer, computer-group, building, department | **none** | computer, computer-group, building, department, **user** |
| `vpp_assignment`, `vpp_invitation` | jss-user, jss-user-group | user-group | jss-user, jss-user-group, user-group |

Four things in that table are not derivable from the resource name, and each
was wrong in the CLI before:

- **iBeacons are per-resource, not per-family.** A policy and a macOS profile
  carry them; the equally computer-scoped `mac_application` does not, and
  neither does `mobile_device_application` or `ebook`. A write carrying them on
  one of those answers 2xx and reads back with no `<ibeacons>` element.
- **`<classes>` is an ebook target and nothing else's.** A policy carrying one
  answers 409.
- **Restricted software has no limitations tab at all**, and its one narrowing
  category is a `--user` **exclusion** — the admin UI's "Directory
  Service/Local Users", free text rather than a Jamf Pro object. The CLI
  refused that flag; the wire accepts it and persists it.
- **A resource's GET is not always the authority.** `macapplications` returns an
  empty `<mobile_device_groups>` in both its targets and its exclusions, and
  sending a member in it answers `409 Error: Mobile device groups cannot be
  assigned to an macOS profile`. The element is a server artefact. Where a GET
  and a write probe disagree, the write probe wins — which is also why this
  matrix follows the provider on that one row.

## Guidance

`shapes` in `internal/scope/matrix.go` holds it, keyed on `Resource.SingularKey`
(already unique per scopeable resource, and already stamped into every generated
`scope.Resource`, so a new resource needs no generator change to be covered).

Each scope command **registers only its own resource's categories**, so `--help`
and shell completion are honest: a mobile configuration profile does not offer
`--computer-group`, and restricted software does not offer a `--section
limitation` it has no tab for. The cost is that a cross-family category arrives
as cobra's `unknown flag` before `ValidateScopeCombination` can explain it, so
each mutating leaf carries a `jamf:scope-categories` annotation and the root's
flag-error handler renders it as the hint. A genuine typo still gets
`suggestFlag`'s near-match, which takes precedence.

Two guards:

- `TestEveryScopeableResourceHasAShape` fails when a resource ships without an
  entry, or when a stale entry outlives its resource. Without a shape,
  validation refuses every flag and the command can address nothing — so the
  refusal for an unmapped resource says "this is a jamf-cli bug" rather than
  listing valid categories.
- `TestEveryShapeCategoryIsAddressable` fails when a shape admits a flag no
  accessor reaches — the silent half of a typo in the table, where the flag is
  registered, accepted by validation, and then writes to nothing.

**Adding a scopeable resource:** read its own GET, list the child elements per
section, then confirm any category you are unsure of with a write probe rather
than trusting the GET. Add the row to `shapes` and to the test's list.

## An ebook's class targets need two writes

`<classes>` is stored only while the **stored** category is empty. A write made
while it already holds a member clears it, and the escape routes all fail:
carrying the identical value clears it, omitting the element clears it, and the
child's identifier shape makes no difference (`<class><id>N</id></class>`,
`<class><name>X</name></class>` and both together behave identically).
Wire-checked 2026-09-12, 5/5 each way. Since a scope PUT replaces `<scope>`
wholesale, **no single request can preserve an existing class across any other
scope change** — so `scope add --building` on an ebook holding a class used to
destroy the class and report success.

`PutScope` delivers such a scope in two requests instead: the first carries
every intended change with `<classes>` emptied, leaving the category empty; the
second carries the same scope with the classes populated, which the server now
accepts. Verified 5/5, and end to end through the CLI — `scope add --building`,
`scope add --computer-group` and `scope remove --department` on an ebook holding
a class all leave the class in place.

Three properties make that safe rather than clever, and are the reason it is
not conditional on more than the category being non-empty:

- **It is never worse than one request.** The first PUT already carries the
  caller's real change, so an interruption between the two leaves exactly what
  a single PUT left before: the change applied, the classes gone.
- **It is keyed on the category, not the resource.** Only ebooks carry
  `<classes>` today, but the rule is a property of the element, so a resource
  that gains one needs no edit. A scope with no class members takes the
  single-request path, so the ordinary case is untouched.
- **It needs no new state.** Both requests are built from the same merged
  desired scope; the first is the second with one field zeroed.

The same defect exists in terraform-provider-jamfplatform, where it surfaces
as a hard `Provider produced inconsistent result after apply` on every other
apply rather than as silent loss — filed as
[jamf/terraform-provider-jamfplatform#428](https://github.com/jamf/terraform-provider-jamfplatform/issues/428)
with the clear-then-set remedy. Its acceptance suite cannot catch it: there is
no `jamfplatform_pro_class` resource to mint a class id, so `class_ids` is the
one ID-keyed ebook category with no live fixture.

## Collateral loss is checked, not assumed

Because `<scope>` is replaced wholesale, any write can lose a category the
server decides not to store, and a check scoped to the item the command touched
cannot see it — which is exactly how the class loss above went unreported.
`VerifyScopeWrite` therefore compares the **whole** scope that was sent against
what comes back and names anything missing. Matching is by name, ID or UDID
rather than element equality, because the server augments what it was sent (a
member sent by name returns with an ID, a network segment gains a `uid`).

With the two-request delivery in place this guard should stay quiet on ebooks,
so a class drop reaching it means the server rule has moved — and it says so,
rather than restating the limitation as expected behaviour.

## All-flags are refused client-side

`all_computers` / `all_mobile_devices` / `all_jss_users` make the specific
targets they cover unreachable, and the server enforces that by **accepting the
write with 200 and dropping the member** — wire-checked: `all_computers=true`
plus a `computer_groups` target reads back with the group gone. So
`CheckAllFlagConflict` refuses it before the write, naming the categories the
flag covers and pointing at the exclusions section, which narrows an all-flag
scope rather than conflicting with it. Which targets each flag covers is
per-shape: an ebook's `all_computers` covers only its computer categories,
where a policy's also covers buildings and departments.

## Related

- `docs/solutions/conventions/classic-scope-put-is-scope-only-2026-09-12.md` —
  the write shape, and the element-order trap.
