// Copyright 2026, Jamf Software LLC

package parser

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// mustScaffoldJSON fails the test on a render error rather than letting an empty
// scaffold pass for a real one.
func mustScaffoldJSON(t *testing.T, s *Schema) string {
	t.Helper()
	out, err := ScaffoldJSON(s)
	if err != nil {
		t.Fatalf("ScaffoldJSON: %v", err)
	}
	return out
}

// TestScaffoldJSON_ArrayOfObjectsShowsOneElement is the case this walker exists
// for. `platform-device-groups create --scaffold` rendered "criteria": [] for a
// five-field element, so the only feedback on a wrong guess was a 400 from the
// server.
func TestScaffoldJSON_ArrayOfObjectsShowsOneElement(t *testing.T) {
	s := &Schema{
		Properties: map[string]*Property{
			"criteria": {
				Type: "array",
				Items: &Schema{Properties: map[string]*Property{
					"attributeName": {Type: "string"},
					"order":         {Type: "integer"},
					"negate":        {Type: "boolean"},
				}},
			},
		},
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(mustScaffoldJSON(t, s)), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	arr, ok := parsed["criteria"].([]any)
	if !ok {
		t.Fatalf("criteria is not an array: %#v", parsed["criteria"])
	}
	if len(arr) != 1 {
		t.Fatalf("expected exactly one example element, got %d", len(arr))
	}
	elem, ok := arr[0].(map[string]any)
	if !ok {
		t.Fatalf("element is not an object: %#v", arr[0])
	}
	for _, f := range []string{"attributeName", "order", "negate"} {
		if _, ok := elem[f]; !ok {
			t.Errorf("expected element field %q, got %#v", f, elem)
		}
	}
}

// TestScaffoldJSON_ArrayOfScalarsStaysEmpty pins the other half of the rule.
// Rendering [""] would imply an empty string is a meaningful entry, and a scalar
// element's type is already evident from the field name and its help text.
func TestScaffoldJSON_ArrayOfScalarsStaysEmpty(t *testing.T) {
	s := &Schema{
		Properties: map[string]*Property{
			"domains": {Type: "array", Items: &Schema{Type: "string"}},
			"unknown": {Type: "array"}, // spec declared no items at all
		},
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(mustScaffoldJSON(t, s)), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, f := range []string{"domains", "unknown"} {
		arr, ok := parsed[f].([]any)
		if !ok {
			t.Fatalf("%s is not an array: %#v", f, parsed[f])
		}
		if len(arr) != 0 {
			t.Errorf("expected %s to stay empty, got %#v", f, arr)
		}
	}
}

// TestScaffoldJSON_TopLevelArrayBody covers a request body that is itself an
// array — the DNS whole-list replaces, and `pro app-requests update`. Such a
// schema has no properties, which is why it used to scaffold as "[]" at best.
func TestScaffoldJSON_TopLevelArrayBody(t *testing.T) {
	s := &Schema{
		Type: "array",
		Items: &Schema{Properties: map[string]*Property{
			"hostname": {Type: "string"},
			"ztna":     {Type: "boolean"},
		}},
	}
	var parsed []map[string]any
	if err := json.Unmarshal([]byte(mustScaffoldJSON(t, s)), &parsed); err != nil {
		t.Fatalf("expected a JSON array, got error %v", err)
	}
	if len(parsed) != 1 {
		t.Fatalf("expected one example element, got %d", len(parsed))
	}
	if _, ok := parsed[0]["hostname"]; !ok {
		t.Errorf("expected the element shape, got %#v", parsed[0])
	}
}

// TestScaffoldJSON_NestedObjectsRecurse covers what the Jamf Pro builder never
// did: it emitted {} for an object property, so nested fields were invisible.
func TestScaffoldJSON_NestedObjectsRecurse(t *testing.T) {
	s := &Schema{
		Properties: map[string]*Property{
			"contact": {Type: "object", Nested: &Schema{Properties: map[string]*Property{
				"email": {Type: "string"},
				"name":  {Type: "string"},
			}}},
			"opaque": {Type: "object"}, // no resolved nested schema
		},
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(mustScaffoldJSON(t, s)), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	contact, ok := parsed["contact"].(map[string]any)
	if !ok {
		t.Fatalf("contact is not an object: %#v", parsed["contact"])
	}
	if _, ok := contact["email"]; !ok {
		t.Errorf("expected nested field email, got %#v", contact)
	}
	if m, ok := parsed["opaque"].(map[string]any); !ok || len(m) != 0 {
		t.Errorf("expected an unresolved object property to stay {}, got %#v", parsed["opaque"])
	}
}

// TestScaffoldJSON_KeepsWriteOnly guards the asymmetry between readOnly and
// writeOnly. A scaffold is a request template: read-only fields cannot be sent,
// but write-only ones — passwords, secrets — are exactly what a caller needs
// prompting for, and they never appear in a GET, so a scaffold is the only place
// they surface. See the update --set write-only postmortem.
func TestScaffoldJSON_KeepsWriteOnly(t *testing.T) {
	s := &Schema{
		Properties: map[string]*Property{
			"id":       {Type: "string", ReadOnly: true},
			"password": {Type: "string", WriteOnly: true},
			"name":     {Type: "string"},
		},
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(mustScaffoldJSON(t, s)), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := parsed["password"]; !ok {
		t.Error("writeOnly field must appear in a request scaffold — it is the one place a caller sees it")
	}
	if _, ok := parsed["id"]; ok {
		t.Error("readOnly field must not appear in a request scaffold")
	}
}

// TestScaffoldJSON_ExampleWinsForEveryType checks the example rule reaches arrays
// and objects too, not just scalars. A spec-supplied array example is a better
// answer than the synthesised one-element form.
func TestScaffoldJSON_ExampleWinsForEveryType(t *testing.T) {
	s := &Schema{
		Properties: map[string]*Property{
			"aRecords": {
				Type:    "array",
				Example: []any{"203.0.113.10"},
				Items:   &Schema{Type: "string"},
			},
		},
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(mustScaffoldJSON(t, s)), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	arr, ok := parsed["aRecords"].([]any)
	if !ok || len(arr) != 1 || arr[0] != "203.0.113.10" {
		t.Errorf("expected the spec example to win, got %#v", parsed["aRecords"])
	}
}

func TestHasScaffoldShape(t *testing.T) {
	tests := []struct {
		name string
		s    *Schema
		want bool
	}{
		{"nil", nil, false},
		{"object with properties", &Schema{Properties: map[string]*Property{"a": {Type: "string"}}}, true},
		{"empty object earns no scaffold", &Schema{}, false},
		{
			// The gap: Jamf Pro gated on properties alone, so its one
			// array-bodied endpoint shipped with no --scaffold flag.
			"array of objects",
			&Schema{Type: "array", Items: &Schema{Properties: map[string]*Property{"a": {Type: "string"}}}},
			true,
		},
		{"array of scalars", &Schema{Type: "array", Items: &Schema{Type: "string"}}, false},
		{"array with no items", &Schema{Type: "array"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasScaffoldShape(tt.s); got != tt.want {
				t.Errorf("HasScaffoldShape() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestParseSchema_RecursiveArrayTerminates is the reason maxSchemaDepth exists.
// Object nesting always terminated on its own — a property with no properties of
// its own ends the walk — but an array property can name its own parent as its
// element type, and kin-openapi resolves $ref inline, so following element
// schemas without a cap recurses until the stack dies. Without the cap this test
// does not hang — it dies in about a second with "fatal error: stack overflow",
// so the guard is better than a timeout would be.
func TestParseSchema_RecursiveArrayTerminates(t *testing.T) {
	specPath := filepath.Join(t.TempDir(), "recursive.yaml")
	if err := os.WriteFile(specPath, []byte(`
openapi: 3.0.0
info: {title: Recursive, version: v1}
paths:
  /v1/nodes:
    post:
      tags: [nodes]
      requestBody:
        content:
          application/json:
            schema: {$ref: '#/components/schemas/Node'}
      responses: {'201': {description: created}}
components:
  schemas:
    Node:
      type: object
      properties:
        name: {type: string}
        children:
          type: array
          items: {$ref: '#/components/schemas/Node'}
`), 0o644); err != nil {
		t.Fatalf("writing spec: %v", err)
	}
	resources, err := ParseSpec(specPath)
	if err != nil {
		t.Fatalf("ParseSpec: %v", err)
	}
	// Reaching here at all is the assertion. Also check the scaffold renders and
	// is bounded rather than truncated to uselessness.
	var op *Operation
	for _, r := range resources {
		for _, o := range r.Operations {
			if o.RequestBody != nil && o.RequestBody.Schema != nil {
				op = o
			}
		}
	}
	if op == nil {
		t.Fatal("expected an operation with a request body")
	}
	out := mustScaffoldJSON(t, op.RequestBody.Schema)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("recursive schema produced invalid JSON: %v\n%s", err, out)
	}
	// The element shape is the point, and it is the half that pins
	// parseSchemaDepth's Property.Items branch: a bare [] satisfies a key check,
	// so asserting only the key leaves the parser-side population untested.
	kids, ok := parsed["children"].([]any)
	if !ok || len(kids) != 1 {
		t.Fatalf("expected one example child element, got %#v (%s)", parsed["children"], out)
	}
	elem, ok := kids[0].(map[string]any)
	if !ok {
		t.Fatalf("expected the parsed element schema, got %#v", kids[0])
	}
	if _, ok := elem["name"]; !ok {
		t.Errorf("expected the element to carry Node's own properties, got %#v", elem)
	}
}

// TestParseSchema_DepthCapUnreachedByLiveSpecs backs the claim in
// maxSchemaDepth's doc comment: the cap is protection against a future spec, not
// something currently truncating output. If a spec ingest pushes past it, this
// fails and the cap gets reconsidered deliberately rather than silently
// shortening scaffolds.
func TestParseSchema_DepthCapUnreachedByLiveSpecs(t *testing.T) {
	var specs []string
	for _, pat := range []string{"../../specs/*.yaml", "../../specs/platform/*.json", "../../specs/security/*.json"} {
		m, err := filepath.Glob(pat)
		if err != nil {
			t.Fatalf("globbing %s: %v", pat, err)
		}
		specs = append(specs, m...)
	}
	if len(specs) == 0 {
		t.Skip("no specs to scan")
	}

	deepest, where := 0, ""
	var walk func(s *Schema, path string, d int)
	walk = func(s *Schema, path string, d int) {
		if s == nil {
			return
		}
		if d > deepest {
			deepest, where = d, path
		}
		if s.Items != nil {
			walk(s.Items, path+"[]", d+1)
		}
		for name, p := range s.Properties {
			if p == nil {
				continue
			}
			if p.Nested != nil {
				walk(p.Nested, path+"."+name, d+1)
			}
			if p.Items != nil {
				walk(p.Items, path+"."+name+"[]", d+1)
			}
		}
	}
	for _, f := range specs {
		resources, err := ParseSpec(f)
		if err != nil {
			continue // spec-level parse problems are other tests' business
		}
		for _, r := range resources {
			for _, op := range r.Operations {
				if op.RequestBody != nil {
					walk(op.RequestBody.Schema, filepath.Base(f)+":"+op.Name, 0)
				}
			}
		}
	}
	if deepest >= maxSchemaDepth {
		t.Errorf("a committed spec reaches schema depth %d at %s, at or past the cap of %d — scaffolds for it are being truncated; raise the cap deliberately or confirm the truncation is acceptable",
			deepest, where, maxSchemaDepth)
	}
	t.Logf("deepest committed schema depth: %d (%s); cap %d", deepest, where, maxSchemaDepth)
}

// TestParseSchema_ArrayRequestBodyEndToEnd covers the Schema.Items branch of
// parseSchemaDepth — the other half of this change's mechanism — and does it
// through the whole path a generator takes: parse a spec, ask HasScaffoldShape
// whether the flag is worth emitting, then render.
//
// Every other test in this file hands ScaffoldJSON a Schema built by hand, so
// none of them notice if the parser stops deriving Items from a spec. Without
// this the two population branches could both be deleted with the suite still
// green, and CI's only complaint would be a golden-file drift whose documented
// remedy is to commit the degraded output.
func TestParseSchema_ArrayRequestBodyEndToEnd(t *testing.T) {
	specPath := filepath.Join(t.TempDir(), "arraybody.yaml")
	if err := os.WriteFile(specPath, []byte(`
openapi: 3.0.0
info: {title: AppRequest, version: v1}
paths:
  /v1/app-request-form-settings:
    put:
      tags: [app-requests]
      requestBody:
        content:
          application/json:
            schema:
              type: array
              items:
                type: object
                properties:
                  title: {type: string, example: Quantity}
                  priority: {type: integer}
                  serverId: {type: string, readOnly: true}
      responses: {'200': {description: ok}}
`), 0o644); err != nil {
		t.Fatalf("writing spec: %v", err)
	}
	resources, err := ParseSpec(specPath)
	if err != nil {
		t.Fatalf("ParseSpec: %v", err)
	}
	var op *Operation
	for _, r := range resources {
		for _, o := range r.Operations {
			if o.RequestBody != nil && o.RequestBody.Schema != nil {
				op = o
			}
		}
	}
	if op == nil {
		t.Fatal("expected an operation with a request body")
	}
	// The emit gate: a bare-array body has no properties, so gating on
	// len(Properties) > 0 denied `pro app-requests update` the flag entirely.
	if !HasScaffoldShape(op.RequestBody.Schema) {
		t.Fatalf("bare-array request body should earn --scaffold; Items = %#v", op.RequestBody.Schema.Items)
	}
	out := mustScaffoldJSON(t, op.RequestBody.Schema)
	var parsed []map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("expected a JSON array, got %v\n%s", err, out)
	}
	if len(parsed) != 1 {
		t.Fatalf("expected one example element, got %d: %s", len(parsed), out)
	}
	if got := parsed[0]["title"]; got != "Quantity" {
		t.Errorf("expected the element's spec example, got %#v (%s)", got, out)
	}
	if _, ok := parsed[0]["priority"]; !ok {
		t.Errorf("expected the element's other properties, got %s", out)
	}
	// Read-only omission has to reach inside a synthesised element, not just the
	// top level — an element is a request template too.
	if _, ok := parsed[0]["serverId"]; ok {
		t.Errorf("read-only element field must not appear, got %s", out)
	}
}

// TestParseSchema_ArrayPropertyItemsFromSpec pins the Property.Items branch on
// its own, without the recursive-schema machinery, so a failure names the cause
// directly. This is the branch that turns "criteria": [] into a shape.
func TestParseSchema_ArrayPropertyItemsFromSpec(t *testing.T) {
	specPath := filepath.Join(t.TempDir(), "groups.yaml")
	if err := os.WriteFile(specPath, []byte(`
openapi: 3.0.0
info: {title: Groups, version: v1}
paths:
  /v1/device-groups:
    post:
      tags: [device-groups]
      requestBody:
        content:
          application/json:
            schema:
              type: object
              properties:
                name: {type: string}
                criteria:
                  type: array
                  items:
                    type: object
                    properties:
                      attributeName: {type: string, example: Device Name}
                      operator: {type: string, example: IS}
                hostnames:
                  type: array
                  items: {type: string}
      responses: {'201': {description: created}}
`), 0o644); err != nil {
		t.Fatalf("writing spec: %v", err)
	}
	resources, err := ParseSpec(specPath)
	if err != nil {
		t.Fatalf("ParseSpec: %v", err)
	}
	var op *Operation
	for _, r := range resources {
		for _, o := range r.Operations {
			if o.RequestBody != nil && o.RequestBody.Schema != nil {
				op = o
			}
		}
	}
	if op == nil {
		t.Fatal("expected an operation with a request body")
	}
	crit := op.RequestBody.Schema.Properties["criteria"]
	if crit == nil || crit.Items == nil || len(crit.Items.Properties) == 0 {
		t.Fatalf("parser did not derive the element schema for criteria: %#v", crit)
	}
	out := mustScaffoldJSON(t, op.RequestBody.Schema)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	arr, ok := parsed["criteria"].([]any)
	if !ok || len(arr) != 1 {
		t.Fatalf("expected one example criterion, got %#v (%s)", parsed["criteria"], out)
	}
	if got := arr[0].(map[string]any)["operator"]; got != "IS" {
		t.Errorf("expected the element's spec example, got %#v (%s)", got, out)
	}
	// The scalar-element half, also parsed from the spec rather than hand-built.
	if arr, ok := parsed["hostnames"].([]any); !ok || len(arr) != 0 {
		t.Errorf("expected an array of scalars to stay empty, got %#v", parsed["hostnames"])
	}
}

// TestScaffoldJSON_UntypedPropertyWithNestedShape covers a property carrying
// properties (or allOf) but no declared `type`. parseSchemaDepth resolves its
// Nested schema regardless, and matching only the literal "object" discarded
// that and rendered the whole sub-object as null.
func TestScaffoldJSON_UntypedPropertyWithNestedShape(t *testing.T) {
	s := &Schema{
		Properties: map[string]*Property{
			"settings": {Nested: &Schema{Properties: map[string]*Property{
				"enabled": {Type: "boolean"},
			}}},
			// A bare oneOf resolves to no shape at all; null is the honest answer
			// and matches what the previous default arm gave.
			"odv": {},
		},
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(mustScaffoldJSON(t, s)), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	settings, ok := parsed["settings"].(map[string]any)
	if !ok {
		t.Fatalf("an untyped property with a resolved shape must render it, got %#v", parsed["settings"])
	}
	if _, ok := settings["enabled"]; !ok {
		t.Errorf("expected the nested field, got %#v", settings)
	}
	if v, ok := parsed["odv"]; !ok || v != nil {
		t.Errorf("expected a shapeless untyped property to stay null, got %#v", v)
	}
}

// TestScaffoldJSON_ReportsRenderFailure pins the direction of the error. This
// runs at generation time, so swallowing a marshal failure into "{}" ships an
// operation with a useless scaffold while `make generate` exits 0.
func TestScaffoldJSON_ReportsRenderFailure(t *testing.T) {
	s := &Schema{
		Name: "Broken",
		Properties: map[string]*Property{
			// A spec example is passed through verbatim; a value json cannot
			// encode has to surface rather than collapse the whole scaffold.
			"weird": {Type: "string", Example: func() {}},
		},
	}
	out, err := ScaffoldJSON(s)
	if err == nil {
		t.Fatalf("expected a render error, got %q", out)
	}
	if !strings.Contains(err.Error(), "Broken") {
		t.Errorf("error should name the schema so the generator failure is locatable, got %v", err)
	}
}

// TestScaffoldRawLiteral_RejectsBacktick guards the one place Pro differs from
// the other two generators: it embeds the scaffold in a raw Go string literal, so
// a backtick anywhere in it emits uncompilable generated code. Spec examples now
// reach the scaffold from nested objects and array elements too, so the surface
// is wider than it was.
func TestScaffoldRawLiteral_RejectsBacktick(t *testing.T) {
	clean := &Schema{Properties: map[string]*Property{"name": {Type: "string", Example: "Apple Park"}}}
	if _, err := scaffoldRawLiteral(clean); err != nil {
		t.Fatalf("unexpected error for a backtick-free scaffold: %v", err)
	}
	dirty := &Schema{
		Name: "Shell",
		Properties: map[string]*Property{
			"command": {Type: "array", Items: &Schema{Properties: map[string]*Property{
				"script": {Type: "string", Example: "echo `whoami`"},
			}}},
		},
	}
	if _, err := scaffoldRawLiteral(dirty); err == nil {
		t.Error("a backtick in a nested spec example must fail generation, not emit uncompilable code")
	}
}

// A discriminated-union request body (a bare oneOf) carries no properties of its
// own, so before this it parsed to nothing at all — which cost --scaffold and
// every "Allowed values:" line with no error to notice. uem-connect's create
// became one of these when the SDK split JAMF_PRO onto its own typed contract.
func TestParseSchema_DiscriminatedUnionAdoptsTheFirstVariant(t *testing.T) {
	spec := []byte(`
openapi: 3.0.3
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    Body:
      discriminator:
        propertyName: vendor
      oneOf:
        - $ref: '#/components/schemas/Typed'
        - $ref: '#/components/schemas/Generic'
    Typed:
      type: object
      required: [vendor, url]
      properties:
        vendor: {type: string, enum: [JAMF_PRO]}
        url: {type: string, example: "https://example.jamfcloud.com"}
        authStrategy: {type: string, enum: [M2M, BASIC]}
    Generic:
      type: object
      properties:
        vendor: {type: string, enum: [INTUNE, GOOGLE]}
        isoCountry: {type: string}
        onlyOnGeneric: {type: string, enum: [A, B]}
`)
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(spec)
	if err != nil {
		t.Fatalf("loading spec: %v", err)
	}
	s := parseSchema("Body", doc.Components.Schemas["Body"].Value)

	if got := strings.Join(s.Variants, ","); got != "Typed,Generic" {
		t.Errorf("Variants = %q, want Typed,Generic", got)
	}
	if s.Discriminator != "vendor" {
		t.Errorf("Discriminator = %q, want vendor", s.Discriminator)
	}
	// The first variant's own shape, so the scaffold is a body that satisfies
	// one contract rather than a merge that satisfies none.
	if s.Properties["url"] == nil || s.Properties["url"].Example == nil {
		t.Error("first variant's properties were not adopted")
	}
	if got := strings.Join(s.Required, ","); got != "vendor,url" {
		t.Errorf("Required = %q, want vendor,url", got)
	}
	// The discriminator's values are unioned: naming only the scaffolded
	// variant's would read as "every other vendor is invalid".
	if got := strings.Join(s.Properties["vendor"].Enum, ","); got != "JAMF_PRO,INTUNE,GOOGLE" {
		t.Errorf("vendor enum = %q, want the union across variants", got)
	}

	scaffold, err := ScaffoldJSON(s)
	if err != nil {
		t.Fatalf("ScaffoldJSON: %v", err)
	}
	for _, want := range []string{`"url"`, `"vendor"`, `"authStrategy"`} {
		if !strings.Contains(scaffold, want) {
			t.Errorf("scaffold = %s, missing %s", scaffold, want)
		}
	}
	// A field only a sibling variant declares is help material, not part of the
	// body being rendered.
	if strings.Contains(scaffold, "onlyOnGeneric") {
		t.Errorf("scaffold = %s, leaked a sibling variant's field", scaffold)
	}
	if p := s.Properties["onlyOnGeneric"]; p == nil || !p.VariantOnly {
		t.Error("a sibling variant's enum field should be carried for the help, marked VariantOnly")
	}
}
