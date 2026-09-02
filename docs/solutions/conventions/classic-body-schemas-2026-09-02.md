---
title: "Classic write commands get --scaffold, --set and field help from the SDK's Classic spec"
date: 2026-09-02
category: conventions
module: generator/classicschema
problem_type: convention
severity: medium
applies_when:
  - "Changing what --scaffold prints for a classic-* command"
  - "Adding or changing a resource in specs/classic/resources.yaml"
  - "Ingesting a new classic_api_resource_documentation.json from jamfplatform-go-sdk"
  - "Adding --set to a generator, or changing how a request body is built"
  - "A classic command's --help says the wrong thing about required or allowed values"
tags:
  - generator
  - classic-api
  - scaffold
  - patch-set
  - schema
  - xml
  - credentials
  - wire-facts
---

# Classic bodies were undocumented; the SDK's Classic spec is where the shape comes from

## The problem

`specs/classic/resources.yaml` is a hand-written manifest. It describes URLs —
path, id segment, lookups, which operations exist — and says nothing about what
goes *inside* a request body. So every Classic write command could only ever
say "Reads the XML body from --from-file or stdin", with no `--scaffold`, no
`--set`, and no help naming a single field.

That is worse on Classic than the same gap would be on a modern API, because the
Classic API is unusually forgiving in exactly the ways that hide a mistake.
Wire-checked on a live tenant, 2026-09-02:

| what you send | what happens |
|---|---|
| an element the API does not recognise (`<bogus_unknown_element>`) | **201**, silently dropped |
| an out-of-enum value (`<frequency>Twice per fortnight</frequency>`) | **201**, reads back `Once per computer` |
| an out-of-enum value in a nested array (`<and_or>maybe</and_or>`) | **201**, reads back `and` |
| a missing required field | 409, correct field named — inside an **HTML** error page, one field at a time |
| a supplied `<id>` on create | ignored; the server assigns its own |
| a body `<id>` disagreeing with the URL on PUT | ignored; the **URL** wins |
| a `<size>` count element | ignored |

So a caller who guesses a field name or an enum value gets a 201 and an object
that does the wrong thing. There is no response field that reveals it. That is
the failure this work exists to remove, and it is why the enum list is the most
valuable thing the schema carries.

## Where the shape comes from

`jamfplatform-go-sdk`'s published `classic_api_resource_documentation.json` — the
file already in `PLATFORM_SDK_COVERAGE_SPECS` for gateway coverage. It carries
161 component schemas, 145 enum constraints, 1382 examples and a `required` list
on 62 schemas. 223 of its 245 GETs reference a schema, which is how a resource is
bound to one.

`generator/classicschema` derives the committed artifact
`specs/classic/schemas.json` from it; `generator/classic.AttachSchemas` loads
that and parses each schema through `parser.SchemaFromOpenAPI`, so a Classic body
is walked by the same code that walks a Pro, Platform or Security Cloud one.

43 of the 54 manifest resources bind a schema. 111 of 117 classic
`create`/`update`/`apply` commands gained `--scaffold` and `--set`.

Four decisions in that pipeline are not obvious:

**The artifact carries `components.schemas` and no `paths`.** The Makefile is
emphatic that `classic_api_resource_documentation.json` must never join
`PLATFORM_SDK_SPECS`, because handing it to the platform generator emits a second
set of Pro commands built from gateway paths. Committing a trimmed copy into
`specs/classic/` puts that hazard next door to a file the generator *does* read,
so the artifact drops `paths` entirely: with no operations in it, nothing can
generate a command from it even if pointed at it by mistake. The
resource-to-schema mapping the paths provided is resolved at derivation time into
`x-jamf-classic-resources`.

**Only write-capable resources are bound.** The artifact describes request
bodies, and a read-only resource has none — asking for one binds the *wrong*
schema rather than nothing, because a read-only Classic resource's "detail"
endpoint can answer with a collection. `/accounts` and
`/patchavailabletitles/sourceid/{id}` resolve to the plural `accounts` and
`patch_available_titles` schemas, which are list wrappers.

**The write-shaped `*_post` schema wins where one exists.** The spec declares 15
of them and 13 are orphaned — no operation references them. They are
property-identical to their read counterparts and differ only in `xml` and
`required`, which makes swapping them in cheap and makes ignoring them a real
loss: `computer_group_post` requires `[name, is_smart]` where `computer_group`
declares no top-level `required` at all, and both are enforced on the wire. Three
of them (`ldap_server_post`, `mobile_device_invitation_post`, `user_post`) declare
no `xml.name`, so the root element falls back to the key with `_post` stripped —
without that, a body would be wrapped in `<ldap_server_post>`, an element the
Classic API has never heard of.

**The spec is right about XML roots and the manifest is not, twice.** The
manifest's `singular:` is what the CLI already sends as the XML root and uses as
the JSON unwrap key, so a disagreement is two sources of one fact conflicting and
is reported as a warning rather than silently resolved. Both live disagreements
were settled on the wire: an account group's root is `<group>` and an account
user's is `<account>`, against the manifest's invented `account_group` and
`account_user`. **This is a live inconsistency in `get`** — `classic-account-groups
get 8 -o json` returns `{"group": {...}}` while every other classic `get` returns
the object flat, because the unwrap key does not match. Not changed here; it
changes the output shape of two commands.

## The XML rendering rules

`parser.ScaffoldXML` is the XML sibling of `parser.ScaffoldJSON` and shares its
rules, per
[one-scaffold-walker-2026-08-20](one-scaffold-walker-2026-08-20.md): read-only
omitted, write-only kept, a spec example preferred over a placeholder, one
element shown for an array of objects.

It is a separate renderer rather than a re-marshal of the JSON, because the JSON
is not the wire format and three of the Classic shapes have no faithful JSON
round trip.

**The repeated-element wrapper is collapsed.** Classic renders
`<criteria><criterion>…</criterion></criteria>` as an array of single-key
objects:

```
criteria: {type: array, items: {properties: {criterion: {…}, size: {$ref: size}}}}
```

The element name lives one level below the array, and it is **never derivable
from the array's own name** — 97 of the 373 repeated elements in the spec are not
a naive singularisation (`criteria`→`criterion`, `computers`→`computer`,
`smart_groups`→`group`). Read it off the schema.

**The same spec uses a second modelling for the same XML.** `policy.scripts` is
an *object* holding a `script` array plus `size`, where `policy.criteria` is an
*array of wrappers*. Both render to `<scripts><script>…</script></scripts>`, and
a renderer handling only the first shape emits the second one a level short.

**`size` is overloaded and must not be dropped by name.** Of its 104
occurrences, 102 are the server-computed collection counter, but
`computer_post`'s `hardware.storage[].device.size` (example 512287) and the
`partition.size` beneath it are physical capacities in MB.
`parser.ClassicIsCountElement` discriminates on a repeated sibling — a counter
counts something — and `partition`, whose twelve siblings are all scalars, is
correctly kept. `device` is *not* correctly handled by that test, which is why
`TestNoBoundResourceCarriesASemanticSizeField` exists: neither field is reachable
today, because no manifest resource binds `computer_post`, and the test fails if
an ingest ever binds one. The honest fix then is to read the spec again, not to
guess harder now.

**Every `id` is kept, including a resource's own.** A body `id` is inert (both
wire rows above), so there is nothing to protect a caller from — and the
alternative is worse: most `id` elements in a Classic body are foreign keys the
caller is supposed to supply (a policy's category, site, dock item and directory
binding all reference one), so a rule that stripped them would have to
distinguish identity from reference, and getting that wrong silently removes the
field that binds a policy to its category.

**Escaping is `&`, `<`, `>` and nothing else.** `encoding/xml`'s `EscapeText` is
the obvious choice and is wrong here: it also escapes quotes, and the spec's own
policy examples are shell commands, so it rendered
`echo "foobar"` as `echo &#34;foobar&#34;` — a template a caller has to
un-escape before editing. `&` still matters most, because PI-827 records that the
Classic API extra-decodes some element bodies.

## `--set` on an XML body

Four differences from the three existing `--set` implementations, each forced by
something specific to Classic.

**It builds the whole body and is mutually exclusive with `--from-file`**, where
the Platform and Security Cloud `--set` overlay onto a `--file` body. Overlaying
would mean parsing the caller's XML, merging and re-marshalling it — and a
Classic config-profile body carries a mobileconfig inside a CDATA section that
PI-827 says the server extra-decodes, so a round trip through a generic XML map
is exactly how a payload gets mangled. It is also unnecessary: **a Classic PUT is
a partial update.** Wire-checked 2026-09-02 — a body of
`<network_segment><name>x</name></network_segment>` renamed the segment and left
its address range, its override flags, and a computer group's whole `criteria`
array intact. So there is no fetch-merge-PUT cycle of the kind the modern Pro
generator's `update --set` needs.

Piped stdin is a **warning**, not a refusal. Refusing looked tidier and was
wrong: the test is "stdin is not a character device", which is true of a pipe
carrying a body *and* of the empty stdin a CI runner hands every process — so it
failed `create --set name=x` in exactly the automated case `--set` exists for,
with a message about a body nobody sent.

**An unknown field is refused.** The Classic API discards an element it does not
recognise, so an unrecognised `--set` would otherwise be accepted by the CLI and
by the server and change nothing. The refusal names the schema and suggests the
settable siblings within the same prefix, because the schema is the only place a
field list exists.

**An out-of-enum value is refused**, for the reason in the table above. This is
the one validation the CLI does that the server does not.

**A credential field is refused, and this is new.** None of the three existing
`--set` implementations enforces the repo's credential policy — the modern Pro
generator's only related mechanism is a data-loss *warning* that actively
recommends `--set password=<value>` as the remedy. On the Classic surface that
gap is wide: a distribution point, an SMTP server, an LDAP server, a directory
binding, a VPP account and a disk-encryption configuration all carry one, and
`create`/`update`/`apply` are exactly the commands a caller reaches for. A flag
value lands in shell history, in `ps` output and in CI logs. Matched on the field
name *and* a string type — a distribution point declares
`username_password_required`, a boolean switch whose name contains "password" and
whose value is not one, and refusing that blocks a legitimate setting. The
refusal is also kept out of shell completion, since suggesting
`read_write_password=` at a prompt walks a caller into the thing the policy
forbids.

Arrays are not indexable, matching the modern Pro generator: `--set` cannot say
which member of a repeated element it means, and says so by name rather than
reporting the leaf as an unknown field. `criteria.name` and the `criteria[].and_or`
spelling that `--help` itself uses both land on that message — without the
parent-kind check the caller is told the field does not exist and goes to check
their spelling.

## Two traps found by building this

**Cobra validates `Args` before `RunE`.** A classic `update` on a resource with
no name lookup is `ExactArgs(1)`, so `update --scaffold` was refused with
"accepts 1 arg(s), received 0" and never reached the early return that prints the
template. `--scaffold` describes a body: it needs no identifier, makes no request
and skips auth. `classicScaffoldArgs` relaxes the validator only when the flag is
set. The modern Pro generator does not hit this because its update has a `--name`
alternative and is therefore `MaximumNArgs(1)` anyway.

**The scaffold must bypass the output formatter.** Classic commands default to
pretty-printed XML, so routing an already-indented template through
`ctx.Output.PrintBytes` re-indented it and returned it double-spaced. Straight to
stdout, which is what the modern generator's `printScaffoldOutput` and the
platform commands' `printScaffold` both do.

## What a scaffold does not promise

A policy scaffold cannot be sent unedited, and the failures are informative:

- `general.category.id`'s spec example is `0`, which the server answers
  `409 No match found for category 0`. Same class as the ztna `categoryName: ""`
  note in CLAUDE.md — the rendered value is *guaranteed* invalid rather than
  merely incomplete.
- `general.date_time_limitations`' date examples answer 409.
- `scope` and `account_maintenance` answer **500**, because their specimen
  references (`<building><id>1</id>`, `<network_segment><id>0</id>`) point at
  objects that do not exist on the target.

The rendering is correct — it matches the wire shape element for element — and
showing one specimen entry per optional section is the shared scaffold rule. But
a dangling reference earning a 500 rather than a validation error is worth
knowing, so the generated help says the template populates every optional section
with a specimen and that the sections you do not need should be deleted.

## Refreshing

One flag, not two: `--gateway-source` derives both `specs/gateway/coverage.json`
and `specs/classic/schemas.json` in one generator run, from the same drop
directory and the same source spec. A second flag that has to carry the same
value is a code path nothing exercises on its own — the way the gateway URL-shape
bug survived weeks.

```bash
make sync-platform-specs-from-sdk JAMFPLATFORM_SDK_PATH=/path/to/jamfplatform-go-sdk
# or the coverage half alone:
make sync-gateway-coverage-from-sdk JAMFPLATFORM_SDK_PATH=…
make verify-classic-schemas   # CI-safe; a no-op pass with no SDK checkout
```

The artifact is committed so `make generate` and CI stay hermetic, and it is
byte-stable across runs so `verify-classic-schemas` stays meaningful.
`CarryForwardProvenance` keeps a recorded SDK revision a later run was not told,
for the reason `gateway.CarryForwardProvenance` exists: without it, re-deriving
from unchanged specs blanks the field and the verify target then reports a stale
artifact differing only in what it just erased.

## What to check at the next ingest

- Warnings from `make sync-gateway-coverage`: an unresolved resource, or a new
  `singular` disagreement.
- Whether a newly bound resource carries a semantic `size`
  (`TestNoBoundResourceCarriesASemanticSizeField` will say).
- Whether a new `*_post` schema arrived without an `xml.name`.
- Whether the Classic spec's version moved. It is 11.28.0 against the Pro spec's
  11.31.0, so a Classic resource added since 11.28 is invisible here.
