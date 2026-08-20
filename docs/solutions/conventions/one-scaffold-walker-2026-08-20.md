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
truncated today. `TestParseSchema_DepthCapUnreachedByLiveSpecs` asserts that over
`specs/`, `specs/platform/` **and** `specs/security/` — all three, because all
three are rendered by the same walker — so a future ingest that pushes past the
cap fails loudly instead of silently shortening scaffolds.
`TestParseSchema_RecursiveArrayTerminates` covers the cycle. Without the cap it
does not hang: it dies in about a second with `fatal error: stack overflow`, which
is a better guard than a timeout, and better than the "would hang" this doc first
claimed.

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
  `len(Properties) > 0` and not on "has a body". The first silently denies the
  flag to array bodies; the second offers a flag that prints `{}`. All four gates
  now use it: Pro's per-op and `apply` gates, and the Platform and Security
  templates' `HasScaffold` (which is why `--file`/`--set` and `--scaffold` are now
  separately gated there — a body can be worth sending without being worth
  scaffolding).
- **A render failure must abort generation.** `parser.ScaffoldJSON` returns an
  error; every caller propagates it. Collapsing it to `{}` ships an operation with
  a useless scaffold while `make generate` exits 0, which is the same
  silent-degradation shape as a truncated scaffold.
- **Any new recursion through `Property`/`Schema` must respect `maxSchemaDepth`.**
  Arrays make cycles reachable.
- **Do not test a scaffold rule only against a hand-built `parser.Schema`.** The
  rules live in `scaffold.go` but the *shape* comes from `parseSchemaDepth`, and
  hand-built schemas cannot notice the parser stopping populating `Items` or
  `Nested`. `TestParseSchema_ArrayRequestBodyEndToEnd` and
  `TestParseSchema_ArrayPropertyItemsFromSpec` parse a spec for exactly that
  reason: with only hand-built fixtures, both `Items`-population branches could be
  deleted with the suite green, and CI's sole complaint would be a golden-file
  drift whose documented remedy is `make generate && commit` — i.e. commit the
  degradation.

## Two places the walker is deliberately loose about `type`

Spec schemas routinely carry `properties` without declaring `type: object`, so a
declared type is not a reliable test for "has a shape".

- `scaffoldValue` and `propertyValue` treat `""` the same as `"object"` and render
  a resolved `Nested` schema. A property with neither still renders `null`, which
  today is only a bare `oneOf` (`compliance_benchmark_engine.json` →
  `rules[].odv`).
- `scaffoldArray` decides on `len(Properties)`, not on the element's declared
  type.

## Pro embeds its scaffold in a raw literal; the other two use `%q`

`generator/platform/template.go` and `generator/security/template.go` emit
`{{printf "%q" .Scaffold}}`. The Jamf Pro templates interpolate into a backtick
literal instead, so the generated scaffold stays readable and a `make generate`
diff — the review artefact for a spec ingest, which rewrites dozens of scaffolds
at once — stays line-by-line rather than becoming one escaped mega-line.

The price is that a backtick anywhere in a scaffold emits uncompilable generated
code, and spec examples now reach the scaffold from nested objects and array
elements as well as the top level, so the surface is much wider than it was. No
committed spec has one. `scaffoldRawLiteral` fails generation with the offending
scaffold in the message rather than letting the generated package fail to build
with no clue as to why.

## Verification

Wire-verified against `pro-nmartin` on 2026-08-20: the generated `criteria`
element for `advanced-mobile-device-searches` matches the field set a real record
returns (`andOr`, `closingParen`, `name`, `openingParen`, `priority`,
`searchType`, `value`), and the scaffold posted successfully once the example
criterion was replaced with one the tenant has — created, read back faithfully,
deleted. Regenerating touched 56 files; the largest scaffold
(`computers-inventory create`) went from flat to 193 lines of real nested shape.
