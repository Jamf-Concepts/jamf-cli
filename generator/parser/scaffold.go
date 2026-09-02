// Copyright 2026, Jamf Software LLC

package parser

import (
	"encoding/json"
	"fmt"

	"github.com/getkin/kin-openapi/openapi3"
)

// ScaffoldJSON returns a pretty-printed JSON template for a request body,
// derived from its parsed schema. It backs every generator's --scaffold output:
// the modern Jamf Pro generator, the Platform Gateway generator and the Jamf
// Security Cloud generator all call this, so a scaffold means the same thing
// whichever API a command belongs to.
//
// Before this was shared, the three had drifted into three different answers for
// the same schema. Pro skipped read-only fields and honoured spec examples but
// never descended into nested objects; platform and security descended into
// objects but ignored both examples and read-only. All three rendered an array
// as "[]" regardless of what it held, which is the gap this exists to close —
// `platform-device-groups create --scaffold` emitted "criteria": [] for a
// five-field element, and the only feedback on a wrong guess was a 400.
//
// The rules, and why:
//
//   - Read-only properties are omitted. This is a request template and the
//     server rejects or ignores them.
//   - Write-only properties are kept. They are the ones a caller most needs
//     prompting for — passwords, secrets, keystores — and they never appear in a
//     GET, so a scaffold is the only place they surface. See
//     docs/solutions/logic-errors/write-only-fields-dropped-by-update-set-2026-07-22.md.
//   - A spec example wins over a synthesised placeholder. A real value teaches
//     the format; "" does not.
//   - An array shows one element when the element is an object, and stays empty
//     otherwise. The object shape is the information a caller cannot guess; a
//     scalar element's type is evident from the field and its help text, and
//     rendering [""] would imply an empty string is a meaningful entry.
//
// Returns "{}" for a nil schema, so a template can embed the output
// unconditionally. A marshalling failure is returned, not swallowed: this runs at
// generation time, and the alternative is an operation that ships a scaffold of
// "{}" while `make generate` exits 0 — the silent-shortening failure mode that
// TestParseSchema_DepthCapUnreachedByLiveSpecs exists to prevent one tier up.
func ScaffoldJSON(s *Schema) (string, error) {
	if s == nil {
		return "{}", nil
	}
	data, err := json.MarshalIndent(scaffoldValue(s), "", "  ")
	if err != nil {
		return "", fmt.Errorf("rendering scaffold for schema %q: %w", s.Name, err)
	}
	return string(data), nil
}

// scaffoldValue builds the Go value for a schema. Recursion is bounded by the
// schema tree the parser built, which parseSchemaDepth already capped, so no
// separate depth guard is needed here.
func scaffoldValue(s *Schema) any {
	if s == nil {
		return map[string]any{}
	}
	switch s.Type {
	case "array":
		return scaffoldArray(s.Items)
	case "string":
		return ""
	case "boolean":
		return false
	case "integer", "number":
		return 0
	}
	// "object", "", and anything unrecognised: render the properties we have.
	// An unknown type with no properties yields {}, which is the same answer the
	// three implementations gave before and what generator tests assert.
	obj := make(map[string]any, len(s.Properties))
	for name, prop := range s.Properties {
		// VariantOnly properties belong to a sibling variant of a discriminated
		// union, not to the body being rendered — see Property.VariantOnly.
		if prop == nil || prop.ReadOnly || prop.VariantOnly {
			continue
		}
		obj[name] = propertyValue(prop)
	}
	return obj
}

// propertyValue builds the Go value for one property.
func propertyValue(p *Property) any {
	if p == nil {
		return nil
	}
	// A spec example is a better teacher than a placeholder, whatever the type.
	if p.Example != nil {
		return p.Example
	}
	switch p.Type {
	case "array":
		return scaffoldArray(p.Items)
	case "object", "":
		// An untyped property is included here because a declared type is not a
		// reliable test — scaffoldArray says the same of element schemas, and
		// scaffoldValue treats "" as an object. A property carrying properties or
		// allOf but no `type` already has its Nested schema resolved by
		// parseSchemaDepth; matching only the literal "object" discarded it and
		// rendered the whole sub-object as null.
		if p.Nested != nil {
			return scaffoldValue(p.Nested)
		}
		if p.Type == "object" {
			return map[string]any{}
		}
		// An untyped property with no resolved shape stays null, the same answer
		// the switch's default gave. Today that is only a bare oneOf (e.g.
		// compliance_benchmark_engine.json's rules[].odv).
		return nil
	case "string":
		return ""
	case "boolean":
		return false
	case "integer", "number":
		return 0
	}
	return nil
}

// scaffoldArray renders an array given its element schema: a one-element array
// when the element is an object, and an empty array otherwise.
func scaffoldArray(items *Schema) any {
	if items == nil {
		return []any{}
	}
	// An element is worth showing when it has a shape to show. Type is not a
	// reliable test on its own: plenty of spec schemas carry properties without
	// declaring "object".
	if len(items.Properties) == 0 {
		return []any{}
	}
	return []any{scaffoldValue(items)}
}

// HasScaffoldShape reports whether a request-body schema carries enough shape for
// --scaffold to be worth offering: named properties, or an array whose element
// has them.
//
// The array arm is not hypothetical padding. The Jamf Pro generator gated on
// "has properties" alone, and a bare-array request body has none — so
// `pro app-requests update`, the one Pro endpoint whose body is a top-level
// array, shipped with no --scaffold flag at all rather than with an unhelpful
// one. A schema with neither still gets no flag, because a scaffold of "{}"
// tells a caller nothing.
func HasScaffoldShape(s *Schema) bool {
	if s == nil {
		return false
	}
	if len(s.Properties) > 0 {
		return true
	}
	return s.Type == "array" && s.Items != nil && len(s.Items.Properties) > 0
}

// SchemaFromOpenAPI parses one resolved OpenAPI schema into the generator's own
// Schema tree. It is the exported door onto parseSchema, opened for the Classic
// API generator: Classic resources are declared by a YAML manifest rather than
// by a spec, so generator/classic has schemas to parse but no operation to parse
// them from.
//
// Exported rather than reimplemented because the schema walk is where the
// interesting decisions live — allOf composition, discriminated unions, the
// array-element recursion cap, enum value rendering — and a second copy of it
// would drift the way the three --scaffold builders did before
// docs/solutions/conventions/one-scaffold-walker-2026-08-20.md.
func SchemaFromOpenAPI(name string, s *openapi3.Schema) *Schema {
	if s == nil {
		return nil
	}
	return parseSchema(name, s)
}
