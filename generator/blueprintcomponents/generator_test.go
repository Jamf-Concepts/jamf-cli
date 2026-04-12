// Copyright 2026, Jamf Software LLC

package blueprintcomponents

import (
	"encoding/json"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

func TestSchemaToExample_SimpleObject(t *testing.T) {
	schema := &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type: &openapi3.Types{"object"},
			Properties: openapi3.Schemas{
				"name": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:    &openapi3.Types{"string"},
						Example: "example-name",
					},
				},
				"count": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:    &openapi3.Types{"integer"},
						Example: float64(42),
					},
				},
			},
		},
	}

	result := schemaToExample(schema, 0)
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	if m["name"] != "example-name" {
		t.Errorf("name = %v, want example-name", m["name"])
	}
	if m["count"] != float64(42) {
		t.Errorf("count = %v, want 42", m["count"])
	}
}

func TestSchemaToExample_Enum(t *testing.T) {
	schema := &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type: &openapi3.Types{"string"},
			Enum: []any{"Allowed", "AlwaysOn", "AlwaysOff"},
		},
	}

	result := schemaToExample(schema, 0)
	if result != "Allowed" {
		t.Errorf("got %v, want Allowed", result)
	}
}

func TestSchemaToExample_BooleanDefault(t *testing.T) {
	schema := &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:    &openapi3.Types{"boolean"},
			Default: false,
		},
	}

	result := schemaToExample(schema, 0)
	if result != false {
		t.Errorf("got %v, want false", result)
	}
}

func TestSchemaToExample_BooleanNoDefault(t *testing.T) {
	schema := &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type: &openapi3.Types{"boolean"},
		},
	}

	result := schemaToExample(schema, 0)
	if result != false {
		t.Errorf("got %v, want false", result)
	}
}

func TestSchemaToExample_IntegerMinimum(t *testing.T) {
	min := float64(1)
	schema := &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type: &openapi3.Types{"integer"},
			Min:  &min,
		},
	}

	result := schemaToExample(schema, 0)
	if result != float64(1) {
		t.Errorf("got %v, want 1", result)
	}
}

func TestSchemaToExample_Array(t *testing.T) {
	schema := &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type: &openapi3.Types{"array"},
			Items: &openapi3.SchemaRef{
				Value: &openapi3.Schema{
					Type:    &openapi3.Types{"string"},
					Example: "item",
				},
			},
		},
	}

	result := schemaToExample(schema, 0)
	arr, ok := result.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", result)
	}
	if len(arr) != 1 || arr[0] != "item" {
		t.Errorf("got %v, want [item]", arr)
	}
}

func TestSchemaToExample_AllOf(t *testing.T) {
	schema := &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			AllOf: openapi3.SchemaRefs{
				{
					Value: &openapi3.Schema{
						Type: &openapi3.Types{"object"},
						Properties: openapi3.Schemas{
							"base": &openapi3.SchemaRef{
								Value: &openapi3.Schema{
									Type:    &openapi3.Types{"string"},
									Example: "base-value",
								},
							},
						},
					},
				},
				{
					Value: &openapi3.Schema{
						Type: &openapi3.Types{"object"},
						Properties: openapi3.Schemas{
							"extra": &openapi3.SchemaRef{
								Value: &openapi3.Schema{
									Type:    &openapi3.Types{"integer"},
									Example: float64(99),
								},
							},
						},
					},
				},
			},
		},
	}

	result := schemaToExample(schema, 0)
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	if m["base"] != "base-value" {
		t.Errorf("base = %v, want base-value", m["base"])
	}
	if m["extra"] != float64(99) {
		t.Errorf("extra = %v, want 99", m["extra"])
	}
}

func TestSchemaToExample_OneOfPicksNonDeprecated(t *testing.T) {
	schema := &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			OneOf: openapi3.SchemaRefs{
				{
					Value: &openapi3.Schema{
						Deprecated: true,
						Type:       &openapi3.Types{"object"},
						Properties: openapi3.Schemas{
							"version": &openapi3.SchemaRef{
								Value: &openapi3.Schema{
									Type: &openapi3.Types{"integer"},
									Enum: []any{float64(1)},
								},
							},
						},
					},
				},
				{
					Value: &openapi3.Schema{
						Type: &openapi3.Types{"object"},
						Properties: openapi3.Schemas{
							"version": &openapi3.SchemaRef{
								Value: &openapi3.Schema{
									Type: &openapi3.Types{"integer"},
									Enum: []any{float64(2)},
								},
							},
						},
					},
				},
			},
		},
	}

	result := schemaToExample(schema, 0)
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	if m["version"] != float64(2) {
		t.Errorf("version = %v, want 2 (non-deprecated)", m["version"])
	}
}

func TestSchemaToExample_AdditionalProperties(t *testing.T) {
	schema := &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type: &openapi3.Types{"object"},
			AdditionalProperties: openapi3.AdditionalProperties{
				Schema: &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type: &openapi3.Types{"object"},
						Properties: openapi3.Schemas{
							"State": &openapi3.SchemaRef{
								Value: &openapi3.Schema{
									Type: &openapi3.Types{"string"},
									Enum: []any{"Allowed", "AlwaysOn"},
								},
							},
						},
					},
				},
			},
		},
	}

	result := schemaToExample(schema, 0)
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	inner, ok := m["<key>"].(map[string]any)
	if !ok {
		t.Fatalf("expected <key> to be map, got %T", m["<key>"])
	}
	if inner["State"] != "Allowed" {
		t.Errorf("State = %v, want Allowed", inner["State"])
	}
}

func TestSchemaToExample_WriteOnlyIncluded(t *testing.T) {
	schema := &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type: &openapi3.Types{"object"},
			Properties: openapi3.Schemas{
				"Value": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type: &openapi3.Types{"string"},
						Enum: []any{"Allowed"},
					},
				},
				"Included": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:      &openapi3.Types{"boolean"},
						Default:   true,
						WriteOnly: true,
					},
				},
			},
		},
	}

	result := schemaToExample(schema, 0)
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	// writeOnly fields should still be included in scaffolds
	if m["Included"] != true {
		t.Errorf("Included = %v, want true (writeOnly should be included)", m["Included"])
	}
}

func TestSchemaToExample_EmptySchema(t *testing.T) {
	schema := &openapi3.SchemaRef{
		Value: &openapi3.Schema{},
	}

	result := schemaToExample(schema, 0)
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	if len(m) != 0 {
		t.Errorf("expected empty map, got %v", m)
	}
}

func TestSchemaToExample_ConstValue(t *testing.T) {
	schema := &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type: &openapi3.Types{"integer"},
			Enum: []any{float64(2)}, // const represented as single-element enum
		},
	}

	result := schemaToExample(schema, 0)
	if result != float64(2) {
		t.Errorf("got %v, want 2", result)
	}
}

func TestSchemaToExample_DepthGuard(t *testing.T) {
	schema := &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type: &openapi3.Types{"object"},
			Properties: openapi3.Schemas{
				"nested": nil, // nil ref
			},
		},
	}

	// Should not panic with deep recursion
	result := schemaToExample(schema, 19)
	if result == nil {
		t.Error("expected non-nil result at depth 19")
	}
}

func TestSchemaToExample_ProducesValidJSON(t *testing.T) {
	// Build a complex nested schema resembling a real component
	schema := &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type: &openapi3.Types{"object"},
			Properties: openapi3.Schemas{
				"Setting": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type: &openapi3.Types{"object"},
						Properties: openapi3.Schemas{
							"Value": &openapi3.SchemaRef{
								Value: &openapi3.Schema{
									Type: &openapi3.Types{"string"},
									Enum: []any{"Allowed", "Disallowed"},
								},
							},
							"Included": &openapi3.SchemaRef{
								Value: &openapi3.Schema{
									Type:    &openapi3.Types{"boolean"},
									Default: true,
								},
							},
						},
					},
				},
				"Items": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type: &openapi3.Types{"array"},
						Items: &openapi3.SchemaRef{
							Value: &openapi3.Schema{
								Type: &openapi3.Types{"object"},
								Properties: openapi3.Schemas{
									"Name": &openapi3.SchemaRef{
										Value: &openapi3.Schema{
											Type:    &openapi3.Types{"string"},
											Example: "test",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	result := schemaToExample(schema, 0)
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var parsed any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("produced invalid JSON: %v\nJSON: %s", err, data)
	}
}

func TestShortName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"com.jamf.ddm.software-update-settings", "software-update-settings"},
		{"com.jamf.ddm.sw-updates", "sw-updates"},
		{"com.jamf.ddm-configuration-profile", "ddm-configuration-profile"},
		{"com.jamf.ddm.app-managed", "app-managed"},
	}
	for _, tt := range tests {
		got := shortName(tt.input)
		if got != tt.want {
			t.Errorf("shortName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestApplyOverrides_PasscodeVersion(t *testing.T) {
	// The passcode-settings component has a "version" field that the API
	// requires as a string, but the OpenAPI spec defines it as an integer.
	// applyOverrides should convert it to a string.
	example := map[string]any{"version": 2, "RequirePasscode": true}
	applyOverrides("com.jamf.ddm.passcode-settings", example)

	if v, ok := example["version"].(string); !ok || v != "2" {
		t.Errorf("expected version=\"2\" (string), got %v (%T)", example["version"], example["version"])
	}
	// Other keys should be untouched
	if example["RequirePasscode"] != true {
		t.Error("RequirePasscode should be unchanged")
	}
}

func TestApplyOverrides_NonPasscode(t *testing.T) {
	// Non-passcode components should not be modified
	example := map[string]any{"version": 2}
	applyOverrides("com.jamf.ddm.software-update-settings", example)

	if _, ok := example["version"].(string); ok {
		t.Error("version should remain an int for non-passcode components")
	}
}

func TestApplyOverrides_NonMap(t *testing.T) {
	// Should not panic on non-map input
	applyOverrides("com.jamf.ddm.passcode-settings", "not a map")
}

func TestExtractSlug(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/v1/components/software-update-settings/validate", "software-update-settings"},
		{"/v1/components/sw-update/validate", "sw-update"},
		{"/v1/components/passcode/validate", "passcode"},
		{"/v1/components/free-form/validate", "free-form"},
		{"/v1/components/software-update-settings/translate", ""},
		{"/other/path", ""},
	}
	for _, tt := range tests {
		got := extractSlug(tt.path)
		if got != tt.want {
			t.Errorf("extractSlug(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}
