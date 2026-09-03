---
title: "allOf-composed enums and property-reached unions were two silent hops out of the enum walk"
date: 2026-09-02
category: logic-errors
module: generator/parser
problem_type: silent-data-loss
severity: medium
applies_when:
  - "A spec authors a property as allOf: [{$ref: SomeEnum}] rather than declaring the enum inline"
  - "A oneOf/anyOf variant is itself allOf-composed from a shared base schema"
  - "A request-body property's own schema is a bare oneOf/anyOf"
  - "A generated command's --help lists no Allowed values for a field the server constrains"
tags:
  - generator
  - schema
  - enum
  - allOf
  - oneOf
  - discriminated-union
  - silent-failure
  - account-api
---

## Symptom

`platform sso-connections create --help` and `update --help` listed **no**
"Allowed values:" line at all — not for `connectionType`, not for any field of
the provider settings — while the server constrains six of them and refuses an
out-of-set value (`400 "Unsupported region: ZZZZ"`).

`make generate` exited 0. Nothing in the spec, the generated file, or the test
suite said a thing.

## Cause

Two independent gaps in `parseSchemaDepth` (`generator/parser/parser.go`), and
the account specs need both crossed to reach a single value.

**A property-level `allOf`.** All three `account_*.json` specs author every enum
as the composition idiom:

```json
"region": {
  "description": "Auth0 region.",
  "allOf": [{"$ref": "#/components/schemas/Region"}]
}
```

which is how a spec attaches a description of its own to a named scalar. The
`enum` — and the `type` — therefore sit on the composed item. The property loop
read `prop.Enum` and `prop.Type` off the property alone, so the field came out
both unconstrained and **untyped**, which also drops it out of `--set`
completion.

**An allOf-composed union variant, reached through a property.**
`ConnectionRequest.connection` is a bare `oneOf` over four provider shapes, and
each shape is `allOf[BaseConnectionSettings, {…}]`. Two separate stops there:

- The walk only recursed into `prop.Nested`, populated from `prop.Properties`.
  A bare union has none, so it stopped dead at `connection`.
- The union path itself read `branch.Value.Properties` off the adopted branch.
  A composed variant has none of its own either, so even the top-level
  bare-union handling (added for uem-connect's `ConnectorCreateRequestBody`)
  would have adopted an empty object — the same silent shape it was written to
  fix, one level of composition further in.

The failure mode is the one this repo keeps meeting: an under-declared schema
parses to *less*, never to an error.

## Fix

Three changes, each the minimum the authoring style needs:

- A property adopts its `allOf` items' `type` and `enum` when it declares none
  of its own. Precedence stays with the property; `allOf` is an intersection, so
  adopting an item's constraint is not a choice between shapes the way a `oneOf`
  branch would be.
- `composedPropSources` and `composedRequired` are extracted and shared by the
  top-level walk and the union path, so an adopted variant contributes its
  inherited fields **and** its inherited `required` — `BaseConnectionSettings`
  requires `name` and `region`, not the variant.
- A property whose own schema is a bare union gets the existing
  first-variant-plus-unioned-enums treatment as its `Nested`.

## Blast radius

`sso-connections create` and `update` are the only two of 268 generated files
the change touches. The other two account specs author their enums the same way
but only on response schemas, and no other spec in the tree authors them this
way at all — which is exactly why it went unnoticed until the Jamf Account
surface arrived.

## Tests

`TestBuildEnumChoices_ReachesAllOfComposedUnionVariants`
(`generator/platform/generator_test.go`) pins the rendered paths against the
committed specs, end to end, and fails without the fix. Three
`TestParseSchema_*` cases in `generator/parser/parser_test.go` cover the
mechanisms separately — including that a property's own enum still wins over a
composed one, the case that must *not* change.

Assert the values, not the absence of an error: absence of an error was the
symptom.
