// Copyright 2026, Jamf Software LLC

package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
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
		{"download path", "/v1/things/{id}/download/{fileId}", "GET", false, "download"},
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

	resources, err := ParseSpec(specPath)
	if err != nil {
		t.Fatalf("ParseSpec() error = %v", err)
	}
	if len(resources) == 0 {
		t.Fatal("ParseSpec() returned no resources")
		return
	}
	resource := resources[0]

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

		resources, err := ParseSpec(specPath)
		if err != nil {
			t.Fatalf("ParseSpec(%q) error = %v", name, err)
		}
		if len(resources) != 0 {
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

	resources, err := ParseSpec(specPath)
	if err != nil {
		t.Fatalf("ParseSpec(Building.yaml) error = %v", err)
	}
	if len(resources) == 0 {
		t.Fatal("ParseSpec(Building.yaml) returned nil")
		return
	}
	resource := resources[0]

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

	resources, err := ParseSpec(specPath)
	if err != nil {
		t.Fatalf("ParseSpec() error = %v", err)
	}
	if len(resources) == 0 {
		t.Fatal("ParseSpec() returned no resources")
	}
	resource := resources[0]

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

func makeResource(name string) *Resource {
	return &Resource{Name: name}
}

func resourceNames(rs []*Resource) []string {
	names := make([]string, len(rs))
	for i, r := range rs {
		names[i] = r.Name
	}
	return names
}

func TestDeduplicateVersioned(t *testing.T) {
	tests := []struct {
		name      string
		input     []*Resource
		wantNames []string
	}{
		{
			name:      "no versioned resources — no-op",
			input:     []*Resource{makeResource("buildings"), makeResource("computers"), makeResource("policies")},
			wantNames: []string{"buildings", "computers", "policies"},
		},
		{
			name:      "single versioned resource — renamed to canonical",
			input:     []*Resource{makeResource("buildings"), makeResource("inventory-preload-v-2s")},
			wantNames: []string{"buildings", "inventory-preloads"},
		},
		{
			name: "multiple versions — highest wins, lower dropped",
			input: []*Resource{
				makeResource("mobile-device-prestages-v-2s"),
				makeResource("mobile-device-prestages-v-3s"),
				makeResource("computers"),
			},
			wantNames: []string{"mobile-device-prestages", "computers"},
		},
		{
			name: "base resource suppressed when versioned family exists",
			input: []*Resource{
				makeResource("inventory-preloads"),
				makeResource("inventory-preload-v-2s"),
				makeResource("buildings"),
			},
			wantNames: []string{"inventory-preloads", "buildings"},
		},
		{
			name: "multiple independent version families",
			input: []*Resource{
				makeResource("computer-prestages-v-2s"),
				makeResource("computer-prestages-v-3s"),
				makeResource("inventory-preload-v-2s"),
				makeResource("unrelated"),
			},
			wantNames: []string{"computer-prestages", "inventory-preloads", "unrelated"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DeduplicateVersioned(tt.input)
			gotNames := resourceNames(got)

			if len(gotNames) != len(tt.wantNames) {
				t.Fatalf("DeduplicateVersioned() returned %v, want %v", gotNames, tt.wantNames)
			}
			for i, name := range gotNames {
				if name != tt.wantNames[i] {
					t.Errorf("DeduplicateVersioned()[%d] = %q, want %q", i, name, tt.wantNames[i])
				}
			}

			// Winner must have correct derived fields set.
			for _, r := range got {
				if versionedName.MatchString(r.Name) {
					t.Errorf("output still contains versioned name %q — winner not renamed", r.Name)
				}
			}
		})
	}
}

func TestDeduplicateVersioned_WinnerFieldsRenamed(t *testing.T) {
	r := makeResource("mobile-device-prestages-v-3s")
	result := DeduplicateVersioned([]*Resource{r})
	if len(result) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(result))
	}
	if result[0].Name != "mobile-device-prestages" {
		t.Errorf("Name = %q, want %q", result[0].Name, "mobile-device-prestages")
	}
	if result[0].NameSingular != "mobile-device-prestage" {
		t.Errorf("NameSingular = %q, want %q", result[0].NameSingular, "mobile-device-prestage")
	}
	if result[0].GoName != "MobileDevicePrestages" {
		t.Errorf("GoName = %q, want %q", result[0].GoName, "MobileDevicePrestages")
	}
}

func TestDetectNameField(t *testing.T) {
	tests := []struct {
		name    string
		schemas map[string]*Schema
		want    string
	}{
		{
			name:    "empty schemas",
			schemas: map[string]*Schema{},
			want:    "name",
		},
		{
			name: "name only",
			schemas: map[string]*Schema{
				"Foo": {Properties: map[string]*Property{"name": {}}},
			},
			want: "name",
		},
		{
			name: "displayName only",
			schemas: map[string]*Schema{
				"Foo": {Properties: map[string]*Property{"displayName": {}}},
			},
			want: "displayName",
		},
		{
			name: "mixed schemas - displayName wins",
			schemas: map[string]*Schema{
				"Foo": {Properties: map[string]*Property{"displayName": {}}},
				"Bar": {Properties: map[string]*Property{"name": {}}},
			},
			want: "displayName",
		},
		{
			name: "both in same schema - displayName wins (non-readonly)",
			schemas: map[string]*Schema{
				"Foo": {Properties: map[string]*Property{"name": {}, "displayName": {}}},
			},
			want: "displayName",
		},
		{
			// readOnly displayName is skipped; writable name is used instead
			name: "readOnly displayName skipped - falls through to name",
			schemas: map[string]*Schema{
				"Foo": {Properties: map[string]*Property{
					"displayName": {ReadOnly: true},
					"name":        {},
				}},
			},
			want: "name",
		},
		{
			// readOnly name is skipped; typed candidate is used instead
			name: "readOnly name skipped - falls through to typed candidate",
			schemas: map[string]*Schema{
				"Package": {Properties: map[string]*Property{
					"name":        {ReadOnly: true},
					"packageName": {},
				}},
			},
			want: "packageName",
		},
		{
			// short prefix (<3 chars) must not match to avoid false positives
			name: "short prefix ignored - fall back to name",
			schemas: map[string]*Schema{
				"Certificate": {Properties: map[string]*Property{"caName": {}}},
			},
			want: "name",
		},
		{
			name: "no name fields at all",
			schemas: map[string]*Schema{
				"Foo": {Properties: map[string]*Property{"id": {}, "description": {}}},
			},
			want: "name",
		},
		{
			// Schema "Package" + field "packageName": prefix "package" is in schema name → typed candidate
			name: "packageName matches schema name - used as name field",
			schemas: map[string]*Schema{
				"Package": {Properties: map[string]*Property{"packageName": {}, "fileName": {}, "manifestFileName": {}}},
			},
			want: "packageName",
		},
		{
			// username prefix "user" is in schema name "User"
			name: "username matches User schema",
			schemas: map[string]*Schema{
				"User": {Properties: map[string]*Property{"username": {}, "realname": {}}},
			},
			want: "username",
		},
		{
			// multiple typed candidates from different schemas → ambiguous → fall back to "name"
			name: "multiple typed candidates - fall back to name",
			schemas: map[string]*Schema{
				"Package":  {Properties: map[string]*Property{"packageName": {}}},
				"Category": {Properties: map[string]*Property{"categoryName": {}}},
			},
			want: "name",
		},
		{
			// field prefix not in schema name → not a typed candidate → fall back
			name: "packageName in unrelated schema - fall back to name",
			schemas: map[string]*Schema{
				"Foo": {Properties: map[string]*Property{"packageName": {}, "fileName": {}}},
			},
			want: "name",
		},
		{
			// name takes priority over typed candidates
			name: "name present alongside typed candidate - name wins",
			schemas: map[string]*Schema{
				"Package": {Properties: map[string]*Property{"packageName": {}, "name": {}}},
			},
			want: "name",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectNameField(tt.schemas)
			if got != tt.want {
				t.Errorf("detectNameField() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDetectSingleton(t *testing.T) {
	tests := []struct {
		name string
		ops  []*Operation
		want bool
	}{
		{
			name: "regular collection — GET+POST with {id}",
			ops: []*Operation{
				{Name: "list", Method: "GET", Path: "/v1/widgets", IsList: true},
				{Name: "get", Method: "GET", Path: "/v1/widgets/{id}"},
				{Name: "create", Method: "POST", Path: "/v1/widgets"},
				{Name: "update", Method: "PUT", Path: "/v1/widgets/{id}"},
				{Name: "delete", Method: "DELETE", Path: "/v1/widgets/{id}"},
			},
			want: false,
		},
		{
			name: "singleton settings — GET+PUT on same path, no path params",
			ops: []*Operation{
				{Name: "list", Method: "GET", Path: "/v1/cache-settings", IsList: false},
				{Name: "update", Method: "PUT", Path: "/v1/cache-settings"},
			},
			want: true,
		},
		{
			name: "singleton with history pagination — still a singleton",
			ops: []*Operation{
				{Name: "list", Method: "GET", Path: "/v1/jamf-protect", IsList: false},
				{Name: "update", Method: "PUT", Path: "/v1/jamf-protect"},
				{Name: "delete", Method: "DELETE", Path: "/v1/jamf-protect"},
				{Name: "create", Method: "POST", Path: "/v1/jamf-protect/register"},
				{Name: "history", Method: "GET", Path: "/v1/jamf-protect/history", IsList: true},
				{Name: "add-history-note", Method: "POST", Path: "/v1/jamf-protect/history"},
			},
			want: true,
		},
		{
			name: "read-only collection — GET only, no PUT",
			ops: []*Operation{
				{Name: "list", Method: "GET", Path: "/v1/computer-groups", IsList: false},
			},
			want: false,
		},
		{
			name: "paginated list — not a singleton even with no {id}",
			ops: []*Operation{
				{Name: "list", Method: "GET", Path: "/v1/things", IsList: true},
				{Name: "update", Method: "PUT", Path: "/v1/things"},
			},
			want: false,
		},
		{
			name: "mixed resource — has {id} in a sub-path",
			ops: []*Operation{
				{Name: "list", Method: "GET", Path: "/v1/jamf-connect", IsList: false},
				{Name: "update", Method: "PUT", Path: "/v1/jamf-connect/config-profiles/{id}"},
			},
			want: false,
		},
		{
			name: "empty operations",
			ops:  []*Operation{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectSingleton(tt.ops)
			if got != tt.want {
				t.Errorf("detectSingleton() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseSpec_SingletonSpec(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "CacheSettings.yaml")

	spec := `openapi: 3.0.1
info:
  title: Cache Settings
  description: Manage cache settings
  version: 1.0.0
paths:
  /v1/cache-settings:
    get:
      summary: Get cache settings
      responses:
        200:
          description: OK
    put:
      summary: Update cache settings
      requestBody:
        required: true
      responses:
        200:
          description: Updated
`
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}

	resources, err := ParseSpec(specPath)
	if err != nil {
		t.Fatalf("ParseSpec() error = %v", err)
	}
	if len(resources) == 0 {
		t.Fatal("ParseSpec() returned no resources")
	}
	resource := resources[0]

	if !resource.IsSingleton {
		t.Error("expected IsSingleton = true for GET+PUT settings resource")
	}
	if resource.Name != "cache-settings" {
		t.Errorf("Name = %q, want %q (no pluralization for singletons)", resource.Name, "cache-settings")
	}
	if resource.NameSingular != "cache-settings" {
		t.Errorf("NameSingular = %q, want %q", resource.NameSingular, "cache-settings")
	}
	if resource.GoName != "CacheSettings" {
		t.Errorf("GoName = %q, want %q", resource.GoName, "CacheSettings")
	}

	// Verify "list" was renamed to "get"
	opNames := make(map[string]bool)
	for _, op := range resource.Operations {
		opNames[op.Name] = true
	}
	if opNames["list"] {
		t.Error("singleton resource should not have a 'list' operation (should be renamed to 'get')")
	}
	if !opNames["get"] {
		t.Error("singleton resource should have a 'get' operation (renamed from 'list')")
	}
	if !opNames["update"] {
		t.Error("singleton resource should have an 'update' operation")
	}
}

func TestParseSpec_NonSingleton_CollectionUnchanged(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "Widget.yaml")

	spec := `openapi: 3.0.1
info:
  title: Widgets
  version: 1.0.0
paths:
  /v1/widgets:
    get:
      summary: List widgets
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
      responses:
        201:
          description: Created
  /v1/widgets/{id}:
    get:
      summary: Get a widget
      responses:
        200:
          description: OK
    put:
      summary: Update a widget
      responses:
        200:
          description: OK
    delete:
      summary: Delete a widget
      responses:
        204:
          description: Deleted
`
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}

	resources, err := ParseSpec(specPath)
	if err != nil {
		t.Fatalf("ParseSpec() error = %v", err)
	}
	if len(resources) == 0 {
		t.Fatal("ParseSpec() returned no resources")
	}
	resource := resources[0]

	if resource.IsSingleton {
		t.Error("expected IsSingleton = false for collection resource with {id} paths")
	}
	if resource.Name != "widgets" {
		t.Errorf("Name = %q, want %q", resource.Name, "widgets")
	}

	opNames := make(map[string]bool)
	for _, op := range resource.Operations {
		opNames[op.Name] = true
	}
	if !opNames["list"] {
		t.Error("non-singleton should keep 'list' operation")
	}
}

func TestParseSpec_RealJamfProtectSpec(t *testing.T) {
	specPath := filepath.Join("..", "..", "specs", "JamfProtect.yaml")
	if _, err := os.Stat(specPath); os.IsNotExist(err) {
		t.Skip("specs/JamfProtect.yaml not found, skipping integration test")
	}

	resources, err := ParseSpec(specPath)
	if err != nil {
		t.Fatalf("ParseSpec(JamfProtect.yaml) error = %v", err)
	}
	if len(resources) == 0 {
		t.Fatal("ParseSpec(JamfProtect.yaml) returned no resources")
	}
	resource := resources[0]

	if !resource.IsSingleton {
		t.Error("JamfProtect should be detected as a singleton")
	}
	if resource.Name != "jamf-protect" {
		t.Errorf("Name = %q, want %q", resource.Name, "jamf-protect")
	}
	if resource.GoName != "JamfProtect" {
		t.Errorf("GoName = %q, want %q", resource.GoName, "JamfProtect")
	}

	opNames := make(map[string]bool)
	for _, op := range resource.Operations {
		opNames[op.Name] = true
	}
	if opNames["list"] {
		t.Error("jamf-protect should not have 'list' (should be 'get')")
	}
	if !opNames["get"] {
		t.Error("jamf-protect should have 'get' operation")
	}
	if !opNames["update"] {
		t.Error("jamf-protect should have 'update' operation")
	}
	if !opNames["delete"] {
		t.Error("jamf-protect should have 'delete' operation")
	}
	if !opNames["history"] {
		t.Error("jamf-protect should have 'history' operation")
	}
}

func TestParseSpec_ReadOnlyEndpoint_NotSingleton(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "ComputerGroups.yaml")

	spec := `openapi: 3.0.1
info:
  title: Computer Groups
  version: 1.0.0
paths:
  /v1/computer-groups:
    get:
      summary: Returns all computer groups
      responses:
        200:
          description: OK
`
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}

	resources, err := ParseSpec(specPath)
	if err != nil {
		t.Fatalf("ParseSpec() error = %v", err)
	}
	if len(resources) == 0 {
		t.Fatal("ParseSpec() returned no resources")
	}
	resource := resources[0]

	if resource.IsSingleton {
		t.Error("read-only GET-only endpoint should not be a singleton (no PUT)")
	}
	if resource.Name != "computer-groups" {
		t.Errorf("Name = %q, want %q", resource.Name, "computer-groups")
	}
}

func TestParseSpec_MultiFamily_Synthetic(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "SelfServiceBranding.yaml")

	spec := `openapi: 3.0.1
info:
  title: Self Service Branding
  version: 1.0.0
paths:
  /v1/self-service/branding/macos:
    get:
      summary: List macOS branding configurations
      parameters:
      - name: page
        in: query
        schema:
          type: integer
      responses:
        200:
          description: OK
    post:
      summary: Create a macOS branding configuration
      responses:
        201:
          description: Created
  /v1/self-service/branding/macos/{id}:
    get:
      summary: Get a macOS branding configuration
      parameters:
      - name: id
        in: path
        required: true
        schema:
          type: string
      responses:
        200:
          description: OK
    put:
      summary: Update a macOS branding configuration
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
      summary: Delete a macOS branding configuration
      parameters:
      - name: id
        in: path
        required: true
        schema:
          type: string
      responses:
        204:
          description: Deleted
  /v1/self-service/branding/ios:
    get:
      summary: List iOS branding configurations
      parameters:
      - name: page
        in: query
        schema:
          type: integer
      responses:
        200:
          description: OK
    post:
      summary: Create an iOS branding configuration
      responses:
        201:
          description: Created
  /v1/self-service/branding/ios/{id}:
    get:
      summary: Get an iOS branding configuration
      parameters:
      - name: id
        in: path
        required: true
        schema:
          type: string
      responses:
        200:
          description: OK
    put:
      summary: Update an iOS branding configuration
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
      summary: Delete an iOS branding configuration
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

	resources, err := ParseSpec(specPath)
	if err != nil {
		t.Fatalf("ParseSpec() error = %v", err)
	}
	if len(resources) != 2 {
		t.Fatalf("ParseSpec() returned %d resources, want 2 (one per branding family)", len(resources))
	}

	names := make(map[string]*Resource)
	for _, r := range resources {
		names[r.Name] = r
	}

	macos, ok := names["self-service-branding-macos"]
	if !ok {
		t.Errorf("expected resource named %q, got %v", "self-service-branding-macos", resourceNames(resources))
	} else {
		if macos.NameSingular != "self-service-branding-macos" {
			t.Errorf("macos NameSingular = %q, want %q", macos.NameSingular, "self-service-branding-macos")
		}
		if macos.GoName != "SelfServiceBrandingMacos" {
			t.Errorf("macos GoName = %q, want %q", macos.GoName, "SelfServiceBrandingMacos")
		}
		if macos.IsSingleton {
			t.Error("macos branding should not be a singleton (it has {id} paths)")
		}
		opNames := make(map[string]bool)
		for _, op := range macos.Operations {
			opNames[op.Name] = true
		}
		if !opNames["list"] {
			t.Error("macos branding should have a 'list' operation")
		}
	}

	ios, ok := names["self-service-branding-ios"]
	if !ok {
		t.Errorf("expected resource named %q, got %v", "self-service-branding-ios", resourceNames(resources))
	} else {
		if ios.NameSingular != "self-service-branding-ios" {
			t.Errorf("ios NameSingular = %q, want %q", ios.NameSingular, "self-service-branding-ios")
		}
		if ios.IsSingleton {
			t.Error("ios branding should not be a singleton (it has {id} paths)")
		}
	}
}

func TestParseSpec_MultiFamily_CreatesParentForOrphans(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "Groups.yaml")

	// Spec with two sibling CRUD families AND cross-cutting parent-level endpoints.
	// /v1/groups/smart and /v1/groups/static are the sibling families.
	// GET /v1/groups (list all) and POST /v1/groups/{id}/erase are orphaned parent ops.
	spec := `openapi: 3.0.1
info:
  title: Groups
  version: 1.0.0
paths:
  /v1/groups:
    get:
      summary: List all groups
      parameters:
      - name: page
        in: query
        schema:
          type: integer
      responses:
        200:
          description: OK
  /v1/groups/{id}/erase:
    post:
      summary: Erase all devices in group
      x-action: true
      parameters:
      - name: id
        in: path
        required: true
        schema:
          type: string
      responses:
        204:
          description: Erased
  /v1/groups/smart:
    get:
      summary: List smart groups
      parameters:
      - name: page
        in: query
        schema:
          type: integer
      responses:
        200:
          description: OK
    post:
      summary: Create a smart group
      responses:
        201:
          description: Created
  /v1/groups/smart/{id}:
    get:
      summary: Get a smart group
      parameters:
      - name: id
        in: path
        required: true
        schema:
          type: string
      responses:
        200:
          description: OK
    put:
      summary: Update a smart group
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
      summary: Delete a smart group
      parameters:
      - name: id
        in: path
        required: true
        schema:
          type: string
      responses:
        204:
          description: Deleted
  /v1/groups/static:
    get:
      summary: List static groups
      parameters:
      - name: page
        in: query
        schema:
          type: integer
      responses:
        200:
          description: OK
    post:
      summary: Create a static group
      responses:
        201:
          description: Created
  /v1/groups/static/{id}:
    get:
      summary: Get a static group
      parameters:
      - name: id
        in: path
        required: true
        schema:
          type: string
      responses:
        200:
          description: OK
    put:
      summary: Update a static group
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
      summary: Delete a static group
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

	resources, err := ParseSpec(specPath)
	if err != nil {
		t.Fatalf("ParseSpec() error = %v", err)
	}

	// Expect 3 resources: groups/smart, groups/static, and the parent groups.
	if len(resources) != 3 {
		t.Fatalf("ParseSpec() returned %d resources, want 3 (smart + static + parent)", len(resources))
	}

	byName := make(map[string]*Resource)
	for _, r := range resources {
		byName[r.Name] = r
	}

	if _, ok := byName["groups-smart"]; !ok {
		t.Errorf("missing groups-smart, got %v", resourceNames(resources))
	}
	if _, ok := byName["groups-static"]; !ok {
		t.Errorf("missing groups-static, got %v", resourceNames(resources))
	}

	parent, ok := byName["groups"]
	if !ok {
		t.Fatalf("missing parent resource 'groups' for orphaned ops, got %v", resourceNames(resources))
	}

	parentOpNames := make(map[string]bool)
	for _, op := range parent.Operations {
		parentOpNames[op.Name] = true
	}
	if !parentOpNames["list"] {
		t.Error("parent resource should have 'list' operation (GET /v1/groups)")
	}
	if !parentOpNames["erase"] {
		t.Error("parent resource should have 'erase' operation (POST /v1/groups/{id}/erase)")
	}
	if parent.IsSingleton {
		t.Error("parent resource should not be a singleton")
	}
}

func TestParseSpec_MultiFamily_RealSpec(t *testing.T) {
	specPath := filepath.Join("..", "..", "specs", "SelfServiceBranding.yaml")
	if _, err := os.Stat(specPath); os.IsNotExist(err) {
		t.Skip("specs/SelfServiceBranding.yaml not found, skipping integration test")
	}

	resources, err := ParseSpec(specPath)
	if err != nil {
		t.Fatalf("ParseSpec(SelfServiceBranding.yaml) error = %v", err)
	}
	// 2 sibling families (macos + ios) + 1 parent resource holding the version-stripped
	// orphan POST /self-service/branding/images.
	if len(resources) != 3 {
		t.Fatalf("ParseSpec() returned %d resources, want 3 (macos + ios + parent), got %v",
			len(resources), resourceNames(resources))
	}

	names := make(map[string]bool)
	for _, r := range resources {
		names[r.Name] = true
	}
	if !names["self-service-branding-macos"] {
		t.Errorf("missing self-service-branding-macos, got %v", resourceNames(resources))
	}
	if !names["self-service-branding-ios"] {
		t.Errorf("missing self-service-branding-ios, got %v", resourceNames(resources))
	}
	if !names["self-service-branding-images"] {
		t.Errorf("missing self-service-branding-images (parent for version-stripped upload op), got %v", resourceNames(resources))
	}
}

func TestStripVersionPrefix(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/v1/foo", "/foo"},
		{"/v2/a/b", "/a/b"},
		{"/preview/x", "/x"},
		{"/v10/long", "/long"},
		{"/no-version/foo", "/no-version/foo"},
		{"/v1/self-service/branding", "/self-service/branding"},
		{"/v2/inventory-preload/records", "/inventory-preload/records"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := stripVersionPrefix(tt.path)
			if got != tt.want {
				t.Errorf("stripVersionPrefix(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestOpHasBinaryResponse(t *testing.T) {
	tests := []struct {
		name string
		op   *Operation
		want bool
	}{
		{
			name: "200 with IsBinary true",
			op: &Operation{
				Responses: map[string]*Response{
					"200": {StatusCode: "200", IsBinary: true},
				},
			},
			want: true,
		},
		{
			name: "201 with IsBinary true",
			op: &Operation{
				Responses: map[string]*Response{
					"201": {StatusCode: "201", IsBinary: true},
				},
			},
			want: true,
		},
		{
			name: "200 with IsBinary false",
			op: &Operation{
				Responses: map[string]*Response{
					"200": {StatusCode: "200", IsBinary: false},
				},
			},
			want: false,
		},
		{
			name: "404 with IsBinary true but no 200/201",
			op: &Operation{
				Responses: map[string]*Response{
					"404": {StatusCode: "404", IsBinary: true},
				},
			},
			want: false,
		},
		{
			name: "no responses",
			op:   &Operation{},
			want: false,
		},
		{
			name: "mixed responses — only 200 is binary",
			op: &Operation{
				Responses: map[string]*Response{
					"200": {StatusCode: "200", IsBinary: true},
					"404": {StatusCode: "404", IsBinary: false},
				},
			},
			want: true,
		},
		{
			name: "200 not binary, 201 binary",
			op: &Operation{
				Responses: map[string]*Response{
					"200": {StatusCode: "200", IsBinary: false},
					"201": {StatusCode: "201", IsBinary: true},
				},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := opHasBinaryResponse(tt.op)
			if got != tt.want {
				t.Errorf("opHasBinaryResponse() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFilterToCanonicalPrefix(t *testing.T) {
	// Helper to build an Operation with only Path and Method set.
	op := func(method, path string) *Operation {
		return &Operation{Method: method, Path: path}
	}

	tests := []struct {
		name      string
		ops       []*Operation
		wantPaths []string
	}{
		{
			name: "single collection path — excludes unrelated family",
			ops: []*Operation{
				op("GET", "/v1/icon"),
				op("POST", "/v1/icon"),
				op("GET", "/v1/icon/{id}"),
				op("DELETE", "/v1/icon/{id}"),
				op("GET", "/v1/icon/download/{id}"),
				op("GET", "/v1/branding-images/download/{id}"),
			},
			wantPaths: []string{
				"/v1/icon",
				"/v1/icon/{id}",
				"/v1/icon/download/{id}",
			},
		},
		{
			name: "multi-depth canonical path — includes sibling paths sharing base",
			ops: []*Operation{
				op("GET", "/v2/inventory-preload/records"),
				op("POST", "/v2/inventory-preload/records"),
				op("GET", "/v2/inventory-preload/records/{id}"),
				op("PUT", "/v2/inventory-preload/records/{id}"),
				op("GET", "/v2/inventory-preload/csv"),
				op("POST", "/v2/inventory-preload/csv"),
			},
			wantPaths: []string{
				"/v2/inventory-preload/records",
				"/v2/inventory-preload/records/{id}",
				"/v2/inventory-preload/csv",
			},
		},
		{
			name: "action-only spec — no collection path, all ops returned unchanged",
			ops: []*Operation{
				op("POST", "/v1/computers/{id}/erase"),
				op("POST", "/v1/computers/{id}/lock"),
			},
			wantPaths: []string{
				"/v1/computers/{id}/erase",
				"/v1/computers/{id}/lock",
			},
		},
		{
			name: "two collection paths — multi-family, all ops returned unchanged",
			ops: []*Operation{
				op("GET", "/v1/branding/macos"),
				op("GET", "/v1/branding/macos/{id}"),
				op("GET", "/v1/branding/ios"),
				op("GET", "/v1/branding/ios/{id}"),
			},
			wantPaths: []string{
				"/v1/branding/macos",
				"/v1/branding/macos/{id}",
				"/v1/branding/ios",
				"/v1/branding/ios/{id}",
			},
		},
		{
			name: "single-depth canonical path includes own parameterized child",
			ops: []*Operation{
				op("GET", "/v1/widgets"),
				op("POST", "/v1/widgets"),
				op("GET", "/v1/widgets/{id}"),
				op("DELETE", "/v1/widgets/{id}"),
			},
			wantPaths: []string{
				"/v1/widgets",
				"/v1/widgets/{id}",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterToCanonicalPrefix(tt.ops)

			gotPaths := make(map[string]bool, len(got))
			for _, op := range got {
				gotPaths[op.Path] = true
			}
			wantPaths := make(map[string]bool, len(tt.wantPaths))
			for _, p := range tt.wantPaths {
				wantPaths[p] = true
			}

			for p := range wantPaths {
				if !gotPaths[p] {
					t.Errorf("filterToCanonicalPrefix() missing expected path %q; got paths: %v", p, pathKeys(gotPaths))
				}
			}
			for p := range gotPaths {
				if !wantPaths[p] {
					t.Errorf("filterToCanonicalPrefix() returned unexpected path %q; want paths: %v", p, tt.wantPaths)
				}
			}
		})
	}
}

func TestParseOperation_BinaryGetBecomesDownload(t *testing.T) {
	// GET /images/{id} returning image/* should be renamed from "get" to "download"
	op := buildOpenAPI3Operation("Download an image", "image/*")
	result := parseOperation("/v2/enrollment-customizations/images/{id}", "get", op)
	if result.Name != "download" {
		t.Errorf("name = %q, want %q", result.Name, "download")
	}
}

func TestParseOperation_SubResourceGetNamed(t *testing.T) {
	// GET /{id}/prestages should be named "prestages" not "get"
	op := buildOpenAPI3Operation("Retrieve prestages", "application/json")
	result := parseOperation("/v2/enrollment-customizations/{id}/prestages", "get", op)
	if result.Name != "prestages" {
		t.Errorf("name = %q, want %q", result.Name, "prestages")
	}
}

func TestParseOperation_StandardGetUnchanged(t *testing.T) {
	// GET /{id} should remain "get"
	op := buildOpenAPI3Operation("Get a widget", "application/json")
	result := parseOperation("/v1/widgets/{id}", "get", op)
	if result.Name != "get" {
		t.Errorf("name = %q, want %q", result.Name, "get")
	}
}

func TestParseOperation_SubResourceAfterTwoParams(t *testing.T) {
	// GET /{id}/account/{username}/audit should be named "audit"
	op := buildOpenAPI3Operation("Get audit history", "application/json")
	result := parseOperation("/v2/laps/{clientManagementId}/account/{username}/audit", "get", op)
	if result.Name != "audit" {
		t.Errorf("name = %q, want %q", result.Name, "audit")
	}
}

func TestParseOperation_GetWithTrailingParamNamed(t *testing.T) {
	// GET /{id}/ldap/{panel-id} → "ldap" (named segment before terminal param)
	op := buildOpenAPI3Operation("Get LDAP panel", "application/json")
	result := parseOperation("/v1/enrollment-customization/{id}/ldap/{panel-id}", "get", op)
	if result.Name != "ldap" {
		t.Errorf("name = %q, want %q", result.Name, "ldap")
	}
}

func TestParseOperation_PutSubResourceNamed(t *testing.T) {
	// PUT /{id}/set-password → "set-password"
	op := buildOpenAPI3Operation("Set the LAPS password", "application/json")
	result := parseOperation("/v2/local-admin-password/{clientManagementId}/set-password", "put", op)
	if result.Name != "set-password" {
		t.Errorf("name = %q, want %q", result.Name, "set-password")
	}
}

func TestParseOperation_PutStandardNotRenamed(t *testing.T) {
	// PUT /{id} stays "update" — only non-param terminal gets renamed
	op := buildOpenAPI3Operation("Update a widget", "application/json")
	result := parseOperation("/v1/widgets/{id}", "put", op)
	if result.Name != "update" {
		t.Errorf("name = %q, want %q", result.Name, "update")
	}
}

func TestDeduplicateVersionedOps(t *testing.T) {
	t.Run("prefers higher version", func(t *testing.T) {
		ops := []*Operation{
			{Name: "list", Method: "GET", Path: "/v2/account-preferences", APIVersion: "v2"},
			{Name: "list", Method: "GET", Path: "/v3/account-preferences", APIVersion: "v3"},
		}
		got := deduplicateVersionedOps(ops)
		if len(got) != 1 {
			t.Fatalf("expected 1 op, got %d", len(got))
		}
		if got[0].Path != "/v3/account-preferences" {
			t.Errorf("path = %q, want /v3/account-preferences", got[0].Path)
		}
	})

	t.Run("prefers explicitly versioned over unversioned", func(t *testing.T) {
		ops := []*Operation{
			{Name: "list", Method: "GET", Path: "/ldap/servers", APIVersion: "v1"},
			{Name: "list", Method: "GET", Path: "/v1/ldap/servers", APIVersion: "v1"},
		}
		got := deduplicateVersionedOps(ops)
		if len(got) != 1 {
			t.Fatalf("expected 1 op, got %d", len(got))
		}
		if got[0].Path != "/v1/ldap/servers" {
			t.Errorf("path = %q, want /v1/ldap/servers", got[0].Path)
		}
	})

	t.Run("different paths not deduplicated", func(t *testing.T) {
		ops := []*Operation{
			{Name: "list", Method: "GET", Path: "/v3/enrollment/access-groups", APIVersion: "v3"},
			{Name: "list", Method: "GET", Path: "/v4/enrollment", APIVersion: "v4"},
		}
		got := deduplicateVersionedOps(ops)
		if len(got) != 2 {
			t.Fatalf("expected 2 ops, got %d", len(got))
		}
	})
}

func TestResolveNoParamConflicts(t *testing.T) {
	t.Run("GET settings renamed when competing with list", func(t *testing.T) {
		ops := []*Operation{
			{Name: "list", Method: "GET", Path: "/v2/resource/settings"},
			{Name: "list", Method: "GET", Path: "/v2/resource/pending-rotations"},
		}
		resolveNoParamConflicts(ops)
		names := map[string]bool{}
		for _, op := range ops {
			names[op.Name] = true
		}
		if !names["settings"] || !names["pending-rotations"] {
			t.Errorf("expected names {settings, pending-rotations}, got %v", names)
		}
	})

	t.Run("canonical list with param child is untouched", func(t *testing.T) {
		ops := []*Operation{
			{Name: "list", Method: "GET", Path: "/v2/resource"},
			{Name: "get", Method: "GET", Path: "/v2/resource/{id}"},
			{Name: "list", Method: "GET", Path: "/v2/resource/sub-path"},
		}
		resolveNoParamConflicts(ops)
		// /v2/resource has a /{id} child → canonical → stays "list"
		// /v2/resource/sub-path → renamed to "sub-path"
		var canonical, renamed string
		for _, op := range ops {
			if op.Path == "/v2/resource" {
				canonical = op.Name
			}
			if op.Path == "/v2/resource/sub-path" {
				renamed = op.Name
			}
		}
		if canonical != "list" {
			t.Errorf("canonical list = %q, want %q", canonical, "list")
		}
		if renamed != "sub-path" {
			t.Errorf("sub-path name = %q, want %q", renamed, "sub-path")
		}
	})
}

func TestDisambiguateSameTerminalOps(t *testing.T) {
	t.Run("audit vs audit-by-guid", func(t *testing.T) {
		ops := []*Operation{
			{Name: "audit", Method: "GET", Path: "/v2/laps/{id}/account/{username}/audit"},
			{Name: "audit", Method: "GET", Path: "/v2/laps/{id}/account/{username}/{guid}/audit"},
		}
		disambiguateSameTerminalOps(ops)
		if ops[0].Name != "audit" {
			t.Errorf("shorter path name = %q, want audit", ops[0].Name)
		}
		if ops[1].Name != "audit-by-guid" {
			t.Errorf("longer path name = %q, want audit-by-guid", ops[1].Name)
		}
	})

	t.Run("three history variants", func(t *testing.T) {
		ops := []*Operation{
			{Name: "history", Method: "GET", Path: "/v2/laps/{id}/history"},
			{Name: "history", Method: "GET", Path: "/v2/laps/{id}/account/{username}/history"},
			{Name: "history", Method: "GET", Path: "/v2/laps/{id}/account/{username}/{guid}/history"},
		}
		disambiguateSameTerminalOps(ops)
		names := map[string]string{}
		for _, op := range ops {
			names[op.Path] = op.Name
		}
		if names["/v2/laps/{id}/history"] != "history" {
			t.Errorf("device history = %q, want history", names["/v2/laps/{id}/history"])
		}
		if names["/v2/laps/{id}/account/{username}/history"] != "account-history" {
			t.Errorf("account history = %q, want account-history", names["/v2/laps/{id}/account/{username}/history"])
		}
		if names["/v2/laps/{id}/account/{username}/{guid}/history"] != "account-history-by-guid" {
			t.Errorf("guid history = %q, want account-history-by-guid", names["/v2/laps/{id}/account/{username}/{guid}/history"])
		}
	})

	t.Run("same-path GET and PUT disambiguated by method", func(t *testing.T) {
		ops := []*Operation{
			{Name: "mappings", Method: "GET", Path: "/v2/resource/{id}/mappings"},
			{Name: "mappings", Method: "PUT", Path: "/v2/resource/{id}/mappings"},
		}
		disambiguateSameTerminalOps(ops)
		if ops[0].Name != "mappings" {
			t.Errorf("GET name = %q, want mappings", ops[0].Name)
		}
		if ops[1].Name != "update-mappings" {
			t.Errorf("PUT name = %q, want update-mappings", ops[1].Name)
		}
	})
}

// buildOpenAPI3Operation creates a minimal openapi3.Operation with a 200 response
// of the given content type for use in parseOperation tests.
func buildOpenAPI3Operation(summary, contentType string) *openapi3.Operation {
	desc := "OK"
	resp := openapi3.NewResponses(
		openapi3.WithStatus(200, &openapi3.ResponseRef{
			Value: &openapi3.Response{
				Description: &desc,
				Content: openapi3.Content{
					contentType: &openapi3.MediaType{},
				},
			},
		}),
	)
	return &openapi3.Operation{
		Summary:   summary,
		Responses: resp,
	}
}

func pathKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestDetectIDField(t *testing.T) {
	tests := []struct {
		name    string
		schemas map[string]*Schema
		ops     []*Operation
		want    string
	}{
		{
			name:    "no get operation - defaults to id",
			schemas: map[string]*Schema{},
			ops:     []*Operation{{Name: "list", Method: "GET", Path: "/v1/things"}},
			want:    "id",
		},
		{
			name: "standard {id} path param with id in schema",
			schemas: map[string]*Schema{
				"Building": {Properties: map[string]*Property{"id": {}, "name": {}}},
			},
			ops:  []*Operation{{Name: "get", Method: "GET", Path: "/v1/buildings/{id}"}},
			want: "id",
		},
		{
			name: "non-standard path param with exact schema match",
			schemas: map[string]*Schema{
				"Device": {Properties: map[string]*Property{"clientManagementId": {}, "name": {}}},
			},
			ops:  []*Operation{{Name: "get", Method: "GET", Path: "/v1/mdm/{clientManagementId}"}},
			want: "clientManagementId",
		},
		{
			name: "non-standard path param with exact schema match - fileName",
			schemas: map[string]*Schema{
				"FileData": {Properties: map[string]*Property{"fileName": {}, "length": {}}},
			},
			ops:  []*Operation{{Name: "get", Method: "GET", Path: "/v1/jcds/files/{fileName}"}},
			want: "fileName",
		},
		{
			name: "path param ending in Id - strip Id to find bare property",
			schemas: map[string]*Schema{
				"Settings": {Properties: map[string]*Property{"key": {}, "username": {}}},
			},
			ops:  []*Operation{{Name: "get", Method: "GET", Path: "/v1/preferences/{keyId}"}},
			want: "key",
		},
		{
			name: "generic {id} not in schema - unique ID-like property used",
			schemas: map[string]*Schema{
				"Template": {Properties: map[string]*Property{"templateId": {}, "name": {}}},
			},
			ops:  []*Operation{{Name: "get", Method: "GET", Path: "/v1/templates/{id}"}},
			want: "templateId",
		},
		{
			name: "generic {id} not in schema - unique uuid property used",
			schemas: map[string]*Schema{
				"Plan": {Properties: map[string]*Property{"planUuid": {}, "name": {}}},
			},
			ops:  []*Operation{{Name: "get", Method: "GET", Path: "/v1/plans/{id}"}},
			want: "planUuid",
		},
		{
			name: "generic {id} not in schema - ambiguous multiple ID-like properties",
			schemas: map[string]*Schema{
				"Group": {Properties: map[string]*Property{"groupId": {}, "platformId": {}, "name": {}}},
			},
			ops:  []*Operation{{Name: "get", Method: "GET", Path: "/v1/groups/{id}"}},
			want: "id", // ambiguous — fall back
		},
		{
			name: "path param has no match in schema - falls back to path param name",
			schemas: map[string]*Schema{
				"Language": {Properties: map[string]*Property{"languageCode": {}, "name": {}}},
			},
			ops:  []*Operation{{Name: "get", Method: "GET", Path: "/v3/languages/{languageId}"}},
			want: "languageId", // can't derive languageCode from languageId automatically
		},
		{
			name: "prefers exact match of non-standard path param over id property",
			schemas: map[string]*Schema{
				"Renewal": {Properties: map[string]*Property{"id": {}, "clientManagementId": {}, "name": {}}},
			},
			ops:  []*Operation{{Name: "get", Method: "GET", Path: "/v1/renewals/{clientManagementId}"}},
			want: "clientManagementId",
		},
		{
			name: "get operation without path param - defaults to id",
			schemas: map[string]*Schema{
				"Settings": {Properties: map[string]*Property{"enabled": {}}},
			},
			ops:  []*Operation{{Name: "get", Method: "GET", Path: "/v1/settings"}},
			want: "id",
		},
		{
			name: "generic {id} not in schema - single groupId found",
			schemas: map[string]*Schema{
				"SmartGroup": {Properties: map[string]*Property{"groupId": {}, "name": {}}},
			},
			ops:  []*Operation{{Name: "get", Method: "GET", Path: "/v1/groups/{id}"}},
			want: "groupId",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectIDField(tt.schemas, tt.ops)
			if got != tt.want {
				t.Errorf("detectIDField() = %q, want %q", got, tt.want)
			}
		})
	}
}
