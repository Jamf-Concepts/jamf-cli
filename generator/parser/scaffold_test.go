// Copyright 2026, Jamf Software LLC

package parser

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

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
	if err := json.Unmarshal([]byte(ScaffoldJSON(s)), &parsed); err != nil {
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
	if err := json.Unmarshal([]byte(ScaffoldJSON(s)), &parsed); err != nil {
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
	if err := json.Unmarshal([]byte(ScaffoldJSON(s)), &parsed); err != nil {
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
	if err := json.Unmarshal([]byte(ScaffoldJSON(s)), &parsed); err != nil {
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
	if err := json.Unmarshal([]byte(ScaffoldJSON(s)), &parsed); err != nil {
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
	if err := json.Unmarshal([]byte(ScaffoldJSON(s)), &parsed); err != nil {
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
// schemas without a cap recurses until the stack dies. This test would hang, not
// fail, without the cap.
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
	out := ScaffoldJSON(op.RequestBody.Schema)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("recursive schema produced invalid JSON: %v\n%s", err, out)
	}
	if _, ok := parsed["children"]; !ok {
		t.Errorf("expected children rendered, got %s", out)
	}
}

// TestParseSchema_DepthCapUnreachedByLiveSpecs backs the claim in
// maxSchemaDepth's doc comment: the cap is protection against a future spec, not
// something currently truncating output. If a spec ingest pushes past it, this
// fails and the cap gets reconsidered deliberately rather than silently
// shortening scaffolds.
func TestParseSchema_DepthCapUnreachedByLiveSpecs(t *testing.T) {
	var specs []string
	for _, pat := range []string{"../../specs/*.yaml", "../../specs/platform/*.json"} {
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
