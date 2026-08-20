---
title: "One scaffold walker for all three generators; arrays show one element"
date: 2026-08-20
category: conventions
module: generator/parser
problem_type: convention
severity: medium
applies_when:
  - "Changing what --scaffold prints for any generated command"
  - "Adding a generator that emits a request-body template"
  - "A request body is a bare array, or contains an array of objects"
  - "Adding a field to parser.Schema or parser.Property that a scaffold should show"
tags:
  - generator
  - scaffold
  - schema
  - code-duplication
  - arrays
  - recursion
---

# One scaffold walker, shared by every generator

## The problem

`--scaffold` had three implementations that had drifted into three different
answers for the same schema:

| | read-only | spec examples | nested objects | arrays |
|---|---|---|---|---|
| `generator/parser/generator.go` `scaffoldJSON` (Pro) | skipped | honoured | **not recursed** — emitted `{}` | `[]` |
| `generator/platform/emitter.go` `schemaExample` | **kept** | **ignored** | recursed | `[]` |
| `generator/security/emitter.go` `schemaExample` | **kept** | **ignored** | recursed | `[]` |

The platform and security versions were byte-identical copies of each other. So
the same schema scaffolded differently depending on which API served the command,
and none of the three ever showed what an array held.

That last column was the expensive one. `pro platform-device-groups create
--scaffold` printed `"criteria": []` for a **seven-field** element, and the only
feedback on a wrong guess was a 400 from the server. Worse for a *bare-array*
request body: the Jamf Pro generator gated `--scaffold` on
`len(Schema.Properties) > 0`, and an array body has no properties, so
`pro app-requests update` — the one Pro endpoint whose body is a top-level array —
shipped with **no `--scaffold` flag at all**.

## The fix

One exported walker, `parser.ScaffoldJSON`, plus `parser.HasScaffoldShape` for the
emit gate. All three generators delegate; the duplicated trios are gone.

The parser now captures array element schemas, which it previously read only for
query parameters: `Property.Items` (array-typed property) and `Schema.Items` (a
schema that *is* an array).

Consolidated rules, and why each:

- **Read-only omitted.** It is a request template; the server rejects or ignores
  them. (Was Pro's rule. A no-op for the other two — no platform request body
  declares a read-only field — so unifying it changed no output.)
- **Write-only kept.** The asymmetry is deliberate. Passwords and secrets never
  appear in a GET, so a scaffold is the only place a caller sees them. See
  `write-only-fields-dropped-by-update-set-2026-07-22.md`.
- **A spec example beats a placeholder.** `"searchType": "is"` teaches the format;
  `""` does not.
- **An array shows one element when the element is an object**, and stays empty
  otherwise. The object shape is the part a caller cannot guess; a scalar
  element's type is evident from the field name, and `[""]` would imply an empty
  string is a meaningful entry.

## The recursion cap is load-bearing, not defensive

`maxSchemaDepth` (8) bounds `parseSchemaDepth`. Object nesting never needed it —
a property whose own `Properties` map is empty ends the walk. **Array elements
break that**: a schema may name itself as its own element type (a node with a
`children[]` of nodes), and kin-openapi resolves `$ref` inline, so following
element schemas without a cap recurses until the stack dies. Populating `Items`
therefore *introduced* a hang risk that genuinely did not exist before.

Deepest schema across every committed spec is **3**
(`ComputersInventory.yaml` → `storage.disks[].partitions[]`), so nothing is
truncated today. `TestParseSchema_DepthCapUnreachedByLiveSpecs` asserts that, so a
future ingest that pushes past the cap fails loudly instead of silently
shortening scaffolds. `TestParseSchema_RecursiveArrayTerminates` covers the cycle
— note it would *hang* rather than fail without the cap.

## Trade-off accepted

Filling a synthesised array element with spec examples can make a scaffold that
posts verbatim stop doing so. `advanced-mobile-device-searches create --scaffold`
previously emitted `"criteria": []`, which the server accepted; it now emits an
element whose example criterion name (`"Account"`) this tenant rejects with
`400 INVALID_FIELD "The criterion Account is not valid"`.

Kept anyway, because verbatim-postability was never a property worth protecting —
most creates already carried `"name": ""` and failed validation — and because
zero-valuing element fields would throw away the useful part: `"andOr": "and"` and
`"searchType": "is"` document enum-ish values that `""` cannot. `--scaffold`
prints *an example request body* to edit, not a valid document. The "can be piped
straight back into `--file`" note in the platform template is about the output
being raw JSON regardless of `-o`, not about validity.

## Rules

- **Never add a fourth scaffold builder.** Call `parser.ScaffoldJSON`. A new
  generator that hand-rolls one will drift, and the drift is invisible — the three
  above disagreed for months with nothing failing.
- **Gate `--scaffold` on `parser.HasScaffoldShape`**, not on
  `len(Properties) > 0`. The latter silently denies the flag to array bodies.
- **Any new recursion through `Property`/`Schema` must respect `maxSchemaDepth`.**
  Arrays make cycles reachable.

## Verification

Wire-verified against `pro-nmartin` on 2026-08-20: the generated `criteria`
element for `advanced-mobile-device-searches` matches the field set a real record
returns (`andOr`, `closingParen`, `name`, `openingParen`, `priority`,
`searchType`, `value`), and the scaffold posted successfully once the example
criterion was replaced with one the tenant has — created, read back faithfully,
deleted. Regenerating touched 56 files; the largest scaffold
(`computers-inventory create`) went from flat to 193 lines of real nested shape.
