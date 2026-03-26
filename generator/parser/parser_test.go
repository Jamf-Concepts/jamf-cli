package parser

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPluralize(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"building", "buildings"},
		{"policy", "policies"},
		{"category", "categories"},
		{"access", "accesses"},
		{"computers", "computers"},   // already plural
		{"match", "matches"},         // -ch suffix
		{"key", "keys"},              // vowel+y
		{"bus", "buss"},              // -us suffix falls through to +s
		{"class", "classes"},         // -ss suffix
		{"box", "boxes"},             // -x suffix
		{"brush", "brushes"},         // -sh suffix
		{"day", "days"},              // vowel+y
		{"deploy", "deploys"},        // vowel+y
		{"discovery", "discoveries"}, // consonant+y
		{"device", "devices"},        // -ves already plural
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := pluralize(tt.input)
			if got != tt.want {
				t.Errorf("pluralize(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestInferOperationName(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		method   string
		isAction bool
		want     string
	}{
		{"delete-multiple suffix", "/v1/buildings/delete-multiple", "POST", true, "delete-multiple"},
		{"history export", "/v1/buildings/{id}/history/export", "POST", true, "history-export"},
		{"export", "/v1/buildings/export", "POST", true, "export"},
		{"history GET", "/v1/buildings/{id}/history", "GET", false, "history"},
		{"history POST", "/v1/buildings/{id}/history", "POST", false, "add-history-note"},
		{"x-action path extraction", "/v1/computers/{id}/erase", "POST", true, "erase"},
		{"x-action lock", "/v1/computers/{id}/lock", "POST", true, "lock"},
		{"GET with id", "/v1/buildings/{id}", "GET", false, "get"},
		{"GET without id (list)", "/v1/buildings", "GET", false, "list"},
		{"POST create", "/v1/buildings", "POST", false, "create"},
		{"PUT update", "/v1/buildings/{id}", "PUT", false, "update"},
		{"PATCH", "/v1/buildings/{id}", "PATCH", false, "patch"},
		{"DELETE", "/v1/buildings/{id}", "DELETE", false, "delete"},
		{"unknown method", "/v1/buildings", "OPTIONS", false, "options"},
		{"POST with id and action", "/v1/computers/{id}", "POST", true, "action"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := inferOperationName(tt.path, tt.method, tt.isAction)
			if got != tt.want {
				t.Errorf("inferOperationName(%q, %q, %v) = %q, want %q", tt.path, tt.method, tt.isAction, got, tt.want)
			}
		})
	}
}

func TestExtractAPIVersion(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/v1/buildings", "v1"},
		{"/v2/computers", "v2"},
		{"/preview/stuff", "preview"},
		{"/JSSResource/policies", "v1"}, // fallback
		{"/v3/devices/{id}", "v3"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := extractAPIVersion(tt.path)
			if got != tt.want {
				t.Errorf("extractAPIVersion(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestIsDestructiveAction(t *testing.T) {
	tests := []struct {
		opName string
		want   bool
	}{
		{"delete", true},
		{"delete-multiple", true},
		{"erase", true},
		{"lock", true},
		{"wipe", true},
		{"remove", true},
		{"restart", true},
		{"shutdown", true},
		{"list", false},
		{"create", false},
		{"get", false},
		{"update", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.opName, func(t *testing.T) {
			got := isDestructiveAction(tt.opName)
			if got != tt.want {
				t.Errorf("isDestructiveAction(%q) = %v, want %v", tt.opName, got, tt.want)
			}
		})
	}
}

func TestParseSpec_MinimalSpec(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "Widget.yaml")

	spec := `openapi: 3.0.1
info:
  title: Widgets
  description: Manage widgets
  version: 1.0.0
paths:
  /v1/widgets:
    get:
      summary: List all widgets
      parameters:
      - name: page
        in: query
        schema:
          type: integer
      responses:
        200:
          description: OK
    post:
      summary: Create a widget
      requestBody:
        required: true
      responses:
        201:
          description: Created
  /v1/widgets/{id}:
    get:
      summary: Get a widget
      parameters:
      - name: id
        in: path
        required: true
        schema:
          type: string
      responses:
        200:
          description: OK
    delete:
      summary: Delete a widget
      parameters:
      - name: id
        in: path
        required: true
        schema:
          type: string
      responses:
        204:
          description: Deleted
`
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}

	resource, err := ParseSpec(specPath)
	if err != nil {
		t.Fatalf("ParseSpec() error = %v", err)
	}
	if resource == nil {
		t.Fatal("ParseSpec() returned nil resource")
		return
	}

	if resource.Name != "widgets" {
		t.Errorf("Name = %q, want %q", resource.Name, "widgets")
	}
	if resource.GoName != "Widgets" {
		t.Errorf("GoName = %q, want %q", resource.GoName, "Widgets")
	}
	if resource.Description != "Manage widgets" {
		t.Errorf("Description = %q, want %q", resource.Description, "Manage widgets")
	}

	if len(resource.Operations) != 4 {
		t.Fatalf("expected 4 operations, got %d", len(resource.Operations))
	}

	// Verify operations by name
	opNames := make(map[string]bool)
	for _, op := range resource.Operations {
		opNames[op.Name] = true
	}
	for _, expected := range []string{"list", "get", "create", "delete"} {
		if !opNames[expected] {
			t.Errorf("missing expected operation %q", expected)
		}
	}

	// Verify delete is marked as destructive
	for _, op := range resource.Operations {
		if op.Name == "delete" && !op.IsDestructive {
			t.Error("delete operation should be marked as destructive")
		}
	}

	// Verify GET /v1/widgets/{id} has path parameter
	for _, op := range resource.Operations {
		if op.Name == "get" {
			if len(op.Parameters) == 0 {
				t.Error("get operation should have parameters")
			}
			found := false
			for _, p := range op.Parameters {
				if p.Name == "id" && p.In == "path" {
					found = true
				}
			}
			if !found {
				t.Error("get operation should have 'id' path parameter")
			}
		}
	}
}

func TestParseSpec_SkipsLibraryFiles(t *testing.T) {
	dir := t.TempDir()

	for _, name := range []string{"DefinitionsLibrary.yaml", "CommonTypes.yaml", "SchemaLibrary.yaml"} {
		specPath := filepath.Join(dir, name)
		spec := `openapi: 3.0.1
info:
  title: Library
  version: 1.0.0
paths: {}
`
		if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
			t.Fatal(err)
		}

		resource, err := ParseSpec(specPath)
		if err != nil {
			t.Fatalf("ParseSpec(%q) error = %v", name, err)
		}
		if resource != nil {
			t.Errorf("ParseSpec(%q) should return nil for library file", name)
		}
	}
}

func TestParseSpec_InvalidFile(t *testing.T) {
	_, err := ParseSpec("/nonexistent/path/spec.yaml")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestParseSpec_RealBuildingSpec(t *testing.T) {
	// Use the real Building spec — will exercise $ref resolution
	specPath := filepath.Join("..", "..", "specs", "Building.yaml")
	if _, err := os.Stat(specPath); os.IsNotExist(err) {
		t.Skip("specs/Building.yaml not found, skipping integration test")
	}

	resource, err := ParseSpec(specPath)
	if err != nil {
		t.Fatalf("ParseSpec(Building.yaml) error = %v", err)
	}
	if resource == nil {
		t.Fatal("ParseSpec(Building.yaml) returned nil")
		return
	}

	if resource.Name != "buildings" {
		t.Errorf("Name = %q, want %q", resource.Name, "buildings")
	}

	// Building.yaml has: list, get, create, update, delete, delete-multiple, history, add-history-note, history-export, export
	expectedOps := map[string]bool{
		"list":             false,
		"get":              false,
		"create":           false,
		"update":           false,
		"delete":           false,
		"delete-multiple":  false,
		"history":          false,
		"add-history-note": false,
		"history-export":   false,
		"export":           false,
	}
	for _, op := range resource.Operations {
		if _, ok := expectedOps[op.Name]; ok {
			expectedOps[op.Name] = true
		}
	}
	for name, found := range expectedOps {
		if !found {
			t.Errorf("missing expected operation %q in Building spec", name)
		}
	}

	// Verify schemas were parsed
	if _, ok := resource.Schemas["Building"]; !ok {
		t.Error("expected Building schema to be parsed")
	}

	// Verify Building schema has expected properties
	if building, ok := resource.Schemas["Building"]; ok {
		for _, prop := range []string{"name", "city", "country"} {
			if _, ok := building.Properties[prop]; !ok {
				t.Errorf("Building schema missing property %q", prop)
			}
		}
	}
}

func TestParseSpec_WithSchemas(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "Item.yaml")

	spec := `openapi: 3.0.1
info:
  title: Items
  version: 1.0.0
paths:
  /v1/items:
    get:
      summary: List items
      responses:
        200:
          description: OK
components:
  schemas:
    Item:
      type: object
      required:
      - name
      properties:
        id:
          type: string
          readOnly: true
        name:
          type: string
          description: Item name
        count:
          type: integer
          nullable: true
`
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}

	resource, err := ParseSpec(specPath)
	if err != nil {
		t.Fatalf("ParseSpec() error = %v", err)
	}

	if len(resource.Schemas) != 1 {
		t.Fatalf("expected 1 schema, got %d", len(resource.Schemas))
	}

	item := resource.Schemas["Item"]
	if item == nil {
		t.Fatal("missing Item schema")
		return
	}
	if item.Type != "object" {
		t.Errorf("schema type = %q, want %q", item.Type, "object")
	}
	if len(item.Required) != 1 || item.Required[0] != "name" {
		t.Errorf("schema required = %v, want [name]", item.Required)
	}
	if id, ok := item.Properties["id"]; !ok {
		t.Error("missing id property")
	} else if !id.ReadOnly {
		t.Error("id property should be readOnly")
	}
	if count, ok := item.Properties["count"]; !ok {
		t.Error("missing count property")
	} else if !count.Nullable {
		t.Error("count property should be nullable")
	}
}
