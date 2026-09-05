// Copyright 2026, Jamf Software LLC

package parser

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
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
		return
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

// TestParseSpec_XActionMisannotation verifies the parser correctly handles
// upstream specs that mis-tag a plain collection-root CRUD create
// (e.g. POST /v1/foo that returns 201) with x-action: true, while still
// honouring x-action for genuine collection-root actions that don't return 201.
func TestParseSpec_XActionMisannotation(t *testing.T) {
	spec := `openapi: 3.0.1
info:
  title: Things
  version: 1.0.0
paths:
  /v1/things:
    get:
      summary: List things
      parameters:
      - name: page
        in: query
        schema:
          type: integer
      responses:
        '200': {description: OK}
    post:
      summary: Create a thing
      x-action: true
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
      responses:
        '201': {description: Created}
  /v1/things/{id}:
    get:
      summary: Get a thing
      parameters:
      - name: id
        in: path
        required: true
        schema:
          type: string
      responses:
        '200': {description: OK}
`
	actionSpec := `openapi: 3.0.1
info:
  title: Deploy Thing
  version: 1.0.0
paths:
  /v1/deploy-thing:
    post:
      summary: Deploy a thing
      x-action: true
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
      responses:
        '200': {description: OK}
`

	t.Run("mis-annotated POST with 201 resolves to create", func(t *testing.T) {
		dir := t.TempDir()
		specPath := filepath.Join(dir, "Thing.yaml")
		if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
			t.Fatal(err)
		}
		resources, err := ParseSpec(specPath)
		if err != nil {
			t.Fatalf("ParseSpec() error = %v", err)
		}
		if len(resources) == 0 {
			t.Fatal("no resources")
		}
		var createFound bool
		for _, op := range resources[0].Operations {
			if op.Method == "POST" && op.Path == "/v1/things" {
				if op.Name != "create" {
					t.Errorf("POST /v1/things name = %q, want %q", op.Name, "create")
				}
				createFound = true
			}
		}
		if !createFound {
			t.Error("expected POST /v1/things operation")
		}
	})

	t.Run("genuine collection-root action keeps path-segment name", func(t *testing.T) {
		dir := t.TempDir()
		specPath := filepath.Join(dir, "DeployThing.yaml")
		if err := os.WriteFile(specPath, []byte(actionSpec), 0o644); err != nil {
			t.Fatal(err)
		}
		resources, err := ParseSpec(specPath)
		if err != nil {
			t.Fatalf("ParseSpec() error = %v", err)
		}
		if len(resources) == 0 {
			t.Fatal("no resources")
		}
		for _, op := range resources[0].Operations {
			if op.Method == "POST" && op.Path == "/v1/deploy-thing" {
				if op.Name != "deploy-thing" {
					t.Errorf("POST /v1/deploy-thing name = %q, want %q", op.Name, "deploy-thing")
				}
			}
		}
	})
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
			return
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
		return
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
		return
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

// makeVersionedResource is makeResource with one operation, so the resource has
// an API version to be ranked by. The version has to come off a path — a
// resource's name says which family it is in, not which version it serves.
func makeVersionedResource(name, path string) *Resource {
	return &Resource{Name: name, Operations: []*Operation{{Name: "list", Method: "GET", Path: path}}}
}

// The real shape of specs/ComputersInventory{,V2,V3}.yaml: three spec files, one
// family, and the highest version is the one whose *name* carries no version
// suffix — because that file declares /v1 and /v4 together and the within-file
// deduplication leaves it holding v4.
//
// Read by name alone, the v4 resource looks like the legacy base and is
// suppressed, so every pro computers-inventory command shipped /v3 and the two
// v4-only operations (erase, remove-mdm-profile) were never generated. Nothing
// failed: v3 answered, and the gateway published all four versions. Its 11.31.0
// drop now publishes v4 alone, which turned the silent wrong choice into a
// command refused before a request is sent.
func TestDeduplicateVersioned_BaseWinsWhenItServesTheHigherVersion(t *testing.T) {
	base := makeVersionedResource("computers-inventories", "/v4/computers-inventory")
	got := DeduplicateVersioned([]*Resource{
		base,
		makeVersionedResource("computers-inventory-v-2s", "/v2/computers-inventory"),
		makeVersionedResource("computers-inventory-v-3s", "/v3/computers-inventory"),
	})

	if len(got) != 1 {
		t.Fatalf("DeduplicateVersioned() returned %v, want the v4 resource alone", resourceNames(got))
	}
	if got[0] != base {
		t.Errorf("winner is %q serving %s, want the base resource serving /v4 — the name suffix is not the version",
			got[0].Name, got[0].Operations[0].Path)
	}
}

// The suppression rule still has to hold the other way round, which is the case
// it was written for: inventory-preload's base file declares v1 (plus one
// unversioned path with no v2 equivalent) and InventoryPreloadV2.yaml declares
// v2, so the versioned sibling is genuinely the newer one and the base goes.
func TestDeduplicateVersioned_BaseStillLosesWhenItIsOlder(t *testing.T) {
	base := &Resource{Name: "inventory-preloads", Operations: []*Operation{
		{Name: "list", Method: "GET", Path: "/v1/inventory-preload"},
		{Name: "notes", Method: "POST", Path: "/inventory-preload/history/notes"},
	}}
	winner := makeVersionedResource("inventory-preload-v-2s", "/v2/inventory-preload")

	got := DeduplicateVersioned([]*Resource{base, winner})
	if len(got) != 1 {
		t.Fatalf("DeduplicateVersioned() returned %v, want the v2 resource alone", resourceNames(got))
	}
	if got[0] != winner {
		t.Errorf("winner serves %s, want /v2 — an unversioned path alongside v1 must not out-rank v2",
			got[0].Operations[0].Path)
	}
	if got[0].Name != "inventory-preloads" {
		t.Errorf("Name = %q, want the canonical inventory-preloads", got[0].Name)
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
			// An allowlisted GET-only path is a singleton despite having no PUT,
			// so it generates `get` rather than `list` for a single-object response.
			name: "read-only singleton — allowlisted GET-only path",
			ops: []*Operation{
				{Name: "list", Method: "GET", Path: "/v2/environment-type", IsList: false},
			},
			want: true,
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

func TestDetectVersionLock(t *testing.T) {
	versionLockSchema := &Schema{
		Properties: map[string]*Property{
			"displayName": {Name: "displayName", Type: "string"},
			"versionLock": {Name: "versionLock", Type: "integer"},
		},
	}
	noVersionLockSchema := &Schema{
		Properties: map[string]*Property{
			"name": {Name: "name", Type: "string"},
		},
	}

	tests := []struct {
		name string
		ops  []*Operation
		want bool
	}{
		{
			name: "PUT with versionLock in request body",
			ops: []*Operation{
				{Name: "list", Method: "GET", Path: "/v3/computer-prestages"},
				{
					Name: "update", Method: "PUT", Path: "/v3/computer-prestages/{id}",
					RequestBody: &RequestBody{Schema: versionLockSchema},
				},
			},
			want: true,
		},
		{
			name: "POST with versionLock in request body",
			ops: []*Operation{
				{
					Name: "create-scope", Method: "POST", Path: "/v2/computer-prestages/{id}/scope",
					RequestBody: &RequestBody{Schema: versionLockSchema},
				},
			},
			want: true,
		},
		{
			name: "no versionLock in any request body",
			ops: []*Operation{
				{Name: "list", Method: "GET", Path: "/v1/buildings"},
				{
					Name: "create", Method: "POST", Path: "/v1/buildings",
					RequestBody: &RequestBody{Schema: noVersionLockSchema},
				},
				{
					Name: "update", Method: "PUT", Path: "/v1/buildings/{id}",
					RequestBody: &RequestBody{Schema: noVersionLockSchema},
				},
			},
			want: false,
		},
		{
			name: "GET-only operations (no request body)",
			ops: []*Operation{
				{Name: "list", Method: "GET", Path: "/v1/things"},
				{Name: "get", Method: "GET", Path: "/v1/things/{id}"},
			},
			want: false,
		},
		{
			name: "nil request body on PUT",
			ops: []*Operation{
				{Name: "update", Method: "PUT", Path: "/v1/things/{id}"},
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
			got := detectVersionLock(tt.ops)
			if got != tt.want {
				t.Errorf("detectVersionLock() = %v, want %v", got, tt.want)
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
		return
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
		return
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
		return
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
		return
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
		return
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
		return
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
			name: "path param ends in Id, bare prefix+Code suffix matches schema property",
			schemas: map[string]*Schema{
				"Language": {Properties: map[string]*Property{"languageCode": {}, "name": {}}},
			},
			ops:  []*Operation{{Name: "get", Method: "GET", Path: "/v3/languages/{languageId}"}},
			want: "languageCode", // languageId → bare "language" → "language"+"Code" = "languageCode"
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

// ── Override functions ───────────────────────────────────────────────────────

func TestApplyNameOverrides(t *testing.T) {
	resources := []*Resource{
		{Name: "computers-inventories", NameSingular: "computers-inventory", GoName: "ComputersInventories"},
		{Name: "buildings", NameSingular: "building", GoName: "Buildings"},
	}
	ApplyNameOverrides(resources)

	r := resources[0]
	if r.Name != "computers-inventory" {
		t.Errorf("Name = %q, want computers-inventory", r.Name)
	}
	// "computers-inventory" ends in 'y', not 's', so singularize leaves it unchanged
	if r.NameSingular != "computers-inventory" {
		t.Errorf("NameSingular = %q, want computers-inventory", r.NameSingular)
	}
	if r.GoName != "ComputersInventory" {
		t.Errorf("GoName = %q, want ComputersInventory", r.GoName)
	}
	// unaffected resource unchanged
	if resources[1].Name != "buildings" {
		t.Errorf("buildings name changed unexpectedly to %q", resources[1].Name)
	}
}

func TestApplyNameFieldOverrides(t *testing.T) {
	resources := []*Resource{
		{Name: "computers-inventory", NameField: "name", IDField: "id"},
		{Name: "groups", NameField: "name", IDField: "id"},
		{Name: "buildings", NameField: "name", IDField: "id"},
	}
	ApplyNameFieldOverrides(resources)

	tests := []struct {
		name     string
		wantName string
		wantID   string
	}{
		{"computers-inventory", "general.name", "id"},
		{"groups", "groupName", "groupPlatformId"},
		{"buildings", "name", "id"}, // unaffected
	}
	for i, tt := range tests {
		r := resources[i]
		if r.NameField != tt.wantName {
			t.Errorf("%s NameField = %q, want %q", tt.name, r.NameField, tt.wantName)
		}
		if r.IDField != tt.wantID {
			t.Errorf("%s IDField = %q, want %q", tt.name, r.IDField, tt.wantID)
		}
	}
}

func TestApplyCreateOpOverrides(t *testing.T) {
	// Resource matching the override map: sub-path POST should be renamed to "create".
	target := resourceCreateOpOverrides["device-enrollment-instances"]
	if target.Path == "" {
		t.Fatal("resourceCreateOpOverrides missing device-enrollment-instances entry")
	}

	r := &Resource{
		Name: "device-enrollment-instances",
		Operations: []*Operation{
			{Name: "list", Method: "GET", Path: "/v1/device-enrollments"},
			{Name: "upload-token", Method: target.Method, Path: target.Path},
			{Name: "update", Method: "PUT", Path: "/v1/device-enrollments/{id}"},
		},
	}
	// Unaffected resource: keeps its op names unchanged.
	other := &Resource{
		Name: "buildings",
		Operations: []*Operation{
			{Name: "list", Method: "GET", Path: "/v1/buildings"},
			{Name: "create", Method: "POST", Path: "/v1/buildings"},
		},
	}
	ApplyCreateOpOverrides([]*Resource{r, other})

	// Target op should now be named "create".
	var got *Operation
	for _, op := range r.Operations {
		if op.Path == target.Path && op.Method == target.Method {
			got = op
			break
		}
	}
	if got == nil {
		t.Fatal("target op not found after ApplyCreateOpOverrides")
	} else if got.Name != "create" {
		t.Errorf("op name = %q, want %q", got.Name, "create")
	}
	// Untouched ops keep their names.
	for _, op := range other.Operations {
		if op.Name != "list" && op.Name != "create" {
			t.Errorf("buildings op renamed unexpectedly to %q", op.Name)
		}
	}
}

func TestApplyUpdateTokenOpOverrides(t *testing.T) {
	target := resourceUpdateTokenOpOverrides["device-enrollment-instances"]
	if target.Path == "" {
		t.Fatal("resourceUpdateTokenOpOverrides missing device-enrollment-instances entry")
	}

	tokenOp := &Operation{Name: "upload-token-by-id", Method: target.Method, Path: target.Path}
	updateOp := &Operation{Name: "update", Method: "PUT", Path: "/v1/device-enrollments/{id}"}
	r := &Resource{
		Name: "device-enrollment-instances",
		Operations: []*Operation{
			{Name: "list", Method: "GET", Path: "/v1/device-enrollments"},
			updateOp,
			tokenOp,
		},
	}
	ApplyUpdateTokenOpOverrides([]*Resource{r})

	if r.UpdateTokenOp == nil {
		t.Fatal("UpdateTokenOp not set")
	}
	if r.UpdateTokenOp != tokenOp {
		t.Errorf("UpdateTokenOp = %+v, want the token op", r.UpdateTokenOp)
	}
	// Token op must be removed from Operations so no standalone subcommand is emitted.
	for _, op := range r.Operations {
		if op.Path == target.Path && op.Method == target.Method {
			t.Errorf("token op still present in Operations: %+v", op)
		}
	}
	// Other ops remain.
	names := map[string]bool{}
	for _, op := range r.Operations {
		names[op.Name] = true
	}
	if !names["list"] || !names["update"] {
		t.Errorf("unexpected Operations after detach: %v", names)
	}
}

func TestApplyUpdateTokenOpOverrides_NoMatch(t *testing.T) {
	// Resources not in the override map are untouched.
	r := &Resource{
		Name: "buildings",
		Operations: []*Operation{
			{Name: "update", Method: "PUT", Path: "/v1/buildings/{id}"},
		},
	}
	ApplyUpdateTokenOpOverrides([]*Resource{r})
	if r.UpdateTokenOp != nil {
		t.Errorf("UpdateTokenOp set on unrelated resource: %+v", r.UpdateTokenOp)
	}
	if len(r.Operations) != 1 {
		t.Errorf("unrelated resource Operations mutated: %d ops", len(r.Operations))
	}
}

func TestParseSchema_AllOfFlattening(t *testing.T) {
	t.Run("merges properties from allOf items", func(t *testing.T) {
		schema := &openapi3.Schema{
			AllOf: openapi3.SchemaRefs{
				{Value: &openapi3.Schema{
					Properties: openapi3.Schemas{
						"name":        {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
						"displayName": {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
					},
				}},
				{Value: &openapi3.Schema{
					Properties: openapi3.Schemas{
						"extra": {Value: &openapi3.Schema{Type: &openapi3.Types{"boolean"}}},
					},
				}},
			},
		}
		s := parseSchema("TestAllOf", schema)
		if len(s.Properties) != 3 {
			t.Fatalf("expected 3 properties, got %d: %v", len(s.Properties), propKeys(s.Properties))
		}
		for _, name := range []string{"name", "displayName", "extra"} {
			if _, ok := s.Properties[name]; !ok {
				t.Errorf("missing property %q", name)
			}
		}
	})

	t.Run("merges nested allOf (two levels)", func(t *testing.T) {
		schema := &openapi3.Schema{
			AllOf: openapi3.SchemaRefs{
				{Value: &openapi3.Schema{
					AllOf: openapi3.SchemaRefs{
						{Value: &openapi3.Schema{
							Properties: openapi3.Schemas{
								"baseField": {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
							},
						}},
					},
					Properties: openapi3.Schemas{
						"midField": {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
					},
				}},
				{Value: &openapi3.Schema{
					Properties: openapi3.Schemas{
						"topField": {Value: &openapi3.Schema{Type: &openapi3.Types{"boolean"}}},
					},
				}},
			},
		}
		s := parseSchema("TestNestedAllOf", schema)
		if len(s.Properties) != 3 {
			t.Fatalf("expected 3 properties, got %d: %v", len(s.Properties), propKeys(s.Properties))
		}
		for _, name := range []string{"baseField", "midField", "topField"} {
			if _, ok := s.Properties[name]; !ok {
				t.Errorf("missing property %q", name)
			}
		}
	})

	t.Run("direct properties still work", func(t *testing.T) {
		schema := &openapi3.Schema{
			Properties: openapi3.Schemas{
				"name": {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
			},
		}
		s := parseSchema("TestDirect", schema)
		if len(s.Properties) != 1 {
			t.Fatalf("expected 1 property, got %d", len(s.Properties))
		}
		if _, ok := s.Properties["name"]; !ok {
			t.Error("missing property name")
		}
	})
}

func propKeys(m map[string]*Property) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func containsOp(ops []*Operation, target *Operation) bool {
	return slices.Contains(ops, target)
}

func TestPairCollectionBulkActions(t *testing.T) {
	t.Run("pairs per-id action with bulk sibling and drops the bulk op", func(t *testing.T) {
		list := &Operation{Name: "list", Method: "GET", Path: "/v1/deployments", IsList: true}
		perID := &Operation{Name: "installation-retry", Method: "POST", Path: "/v1/deployments/{id}/computers/installation-retry", IsAction: true}
		bulk := &Operation{Name: "installation-retry", Method: "POST", Path: "/v1/deployments/computers/installation-retry", IsAction: true}

		got := pairCollectionBulkActions([]*Operation{list, perID, bulk})

		if containsOp(got, bulk) {
			t.Error("bulk op should have been removed")
		}
		if !containsOp(got, perID) {
			t.Error("per-{id} op should be retained")
		}
		if perID.BulkActionPath != bulk.Path {
			t.Errorf("perID.BulkActionPath = %q, want %q", perID.BulkActionPath, bulk.Path)
		}
	})

	t.Run("leaves a lone collection action (no per-id sibling) untouched", func(t *testing.T) {
		export := &Operation{Name: "export", Method: "POST", Path: "/v1/deployments/export", IsAction: true}
		list := &Operation{Name: "list", Method: "GET", Path: "/v1/deployments", IsList: true}

		got := pairCollectionBulkActions([]*Operation{list, export})

		if !containsOp(got, export) {
			t.Error("lone collection action should be retained as its own command")
		}
		if export.BulkActionPath != "" {
			t.Errorf("export should not get a BulkActionPath, got %q", export.BulkActionPath)
		}
	})

	t.Run("does not pair a mis-tagged CRUD op (PUT /{id}) with a collection create", func(t *testing.T) {
		// Some specs mis-tag PUT /accounts/{id} (update) and POST /accounts (create)
		// with x-action: true. Their stripped paths both reduce to /v1/accounts, but
		// they differ in method and the PUT ends in the {id} param — must NOT pair.
		create := &Operation{Name: "create", Method: "POST", Path: "/v1/accounts", IsAction: true}
		update := &Operation{Name: "update", Method: "PUT", Path: "/v1/accounts/{id}", IsAction: true}

		got := pairCollectionBulkActions([]*Operation{create, update})

		if len(got) != 2 {
			t.Errorf("expected both ops retained (no false pairing), got %d", len(got))
		}
		if update.BulkActionPath != "" {
			t.Errorf("update must not be paired, got BulkActionPath %q", update.BulkActionPath)
		}
	})

	t.Run("does not pair when methods differ", func(t *testing.T) {
		// A per-{id} POST action must not pair with a same-path GET collection action.
		perID := &Operation{Name: "reindex", Method: "POST", Path: "/v1/things/{id}/reindex", IsAction: true}
		getBulk := &Operation{Name: "reindex", Method: "GET", Path: "/v1/things/reindex", IsAction: true}

		got := pairCollectionBulkActions([]*Operation{perID, getBulk})

		if len(got) != 2 || perID.BulkActionPath != "" {
			t.Errorf("cross-method ops must not pair; got %d ops, BulkActionPath=%q", len(got), perID.BulkActionPath)
		}
	})

	t.Run("no pairing when a per-id action has no matching bulk sibling", func(t *testing.T) {
		perID := &Operation{Name: "erase", Method: "POST", Path: "/v1/computers/{id}/erase", IsAction: true}
		list := &Operation{Name: "list", Method: "GET", Path: "/v1/computers", IsList: true}

		got := pairCollectionBulkActions([]*Operation{list, perID})

		if len(got) != 2 {
			t.Errorf("expected both ops retained, got %d", len(got))
		}
		if perID.BulkActionPath != "" {
			t.Errorf("erase should not get a BulkActionPath, got %q", perID.BulkActionPath)
		}
	})

	// Isolates the `strings.HasSuffix(op.Path, "}")` guard. The "mis-tagged CRUD"
	// case above is saved by a method mismatch (PUT vs POST); here both ops share
	// the POST method and strip to the same collection path, so ONLY the
	// trailing-} guard prevents a false pairing. Removing that guard makes this
	// test fail.
	t.Run("does not pair a same-method {id}-terminal op with a collection sibling", func(t *testing.T) {
		create := &Operation{Name: "create", Method: "POST", Path: "/v1/accounts", IsAction: true}
		update := &Operation{Name: "update", Method: "POST", Path: "/v1/accounts/{id}", IsAction: true}

		got := pairCollectionBulkActions([]*Operation{create, update})

		if len(got) != 2 {
			t.Errorf("expected both ops retained (trailing-} guard must fire), got %d", len(got))
		}
		if update.BulkActionPath != "" {
			t.Errorf("{id}-terminal op must not pair, got BulkActionPath %q", update.BulkActionPath)
		}
	})

	// Isolates the `strings.Count(op.Path, "{") != 1` guard. The two-param action
	// strips to the same collection path as the bulk sibling AND shares its POST
	// method, so ONLY the exactly-one-param guard stops it from wrongly claiming
	// --all (the single-param sibling is the legitimate owner of that pairing).
	// Removing that guard makes this test fail.
	t.Run("does not pair a multi-param action with a collection sibling", func(t *testing.T) {
		bulk := &Operation{Name: "installation-retry", Method: "POST", Path: "/v1/deployments/computers/installation-retry", IsAction: true}
		perComputer := &Operation{Name: "installation-retry", Method: "POST", Path: "/v1/deployments/{id}/computers/{computerId}/installation-retry", IsAction: true}

		got := pairCollectionBulkActions([]*Operation{bulk, perComputer})

		if len(got) != 2 {
			t.Errorf("expected both ops retained (param-count guard must fire), got %d", len(got))
		}
		if perComputer.BulkActionPath != "" {
			t.Errorf("multi-param action must not pair, got BulkActionPath %q", perComputer.BulkActionPath)
		}
	})
}

func TestStripParamSegments(t *testing.T) {
	cases := map[string]string{
		"/v1/deployments/{id}/computers/installation-retry": "/v1/deployments/computers/installation-retry",
		"/v1/deployments/export":                            "/v1/deployments/export",
		"/v1/foo/{id}/bars/{barId}/baz":                     "/v1/foo/bars/baz",
	}
	for in, want := range cases {
		if got := stripParamSegments(in); got != want {
			t.Errorf("stripParamSegments(%q) = %q, want %q", in, got, want)
		}
	}
}

// applyDocumentedStatusResults must attach the allowlisted statuses (with the
// spec's own wording) only to the operations named in documentedStatusResults —
// every other operation's 403 stays an ordinary permission error.
func TestApplyDocumentedStatusResults(t *testing.T) {
	const (
		privilegeCheckPath = "/v1/pki/digicert/trust-lifecycle-manager/{id}/privilege-check"
		missingDesc        = "DigiCert account is missing one or more required permissions."
		presentDesc        = "DigiCert account has all required permissions for certificate deployment."
	)

	t.Run("allowlisted operation", func(t *testing.T) {
		op := &Operation{
			Method: "GET",
			Path:   privilegeCheckPath,
			Responses: map[string]*Response{
				"204": {StatusCode: "204", Description: presentDesc},
				"403": {StatusCode: "403", Description: missingDesc},
				"404": {StatusCode: "404", Description: "Settings not found."},
			},
		}
		applyDocumentedStatusResults(op)

		if len(op.StatusResults) != 1 {
			t.Fatalf("StatusResults = %+v, want exactly the 403", op.StatusResults)
		}
		if op.StatusResults[0].Code != 403 {
			t.Errorf("status = %d, want 403", op.StatusResults[0].Code)
		}
		if op.StatusResults[0].Description != missingDesc {
			t.Errorf("description = %q, want the spec's 403 wording", op.StatusResults[0].Description)
		}
		if op.NoContentDescription != presentDesc {
			t.Errorf("NoContentDescription = %q, want the spec's 204 wording", op.NoContentDescription)
		}
	})

	t.Run("same path, different method is not allowlisted", func(t *testing.T) {
		op := &Operation{
			Method:    "POST",
			Path:      privilegeCheckPath,
			Responses: map[string]*Response{"403": {StatusCode: "403", Description: missingDesc}},
		}
		applyDocumentedStatusResults(op)
		if len(op.StatusResults) != 0 {
			t.Errorf("StatusResults = %+v, want none", op.StatusResults)
		}
	})

	t.Run("ordinary operation keeps its 403 as an error", func(t *testing.T) {
		op := &Operation{
			Method:    "GET",
			Path:      "/v1/buildings",
			Responses: map[string]*Response{"403": {StatusCode: "403", Description: "Forbidden"}},
		}
		applyDocumentedStatusResults(op)
		if len(op.StatusResults) != 0 || op.NoContentDescription != "" {
			t.Errorf("StatusResults = %+v, NoContentDescription = %q; want both empty",
				op.StatusResults, op.NoContentDescription)
		}
	})
}

func TestStripVersionSegments(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{"leading version, as Jamf Pro paths carry it", "/v1/computers-inventory", "/computers-inventory"},
		{"preview counts as a version", "/preview/x", "/x"},
		{"no version is left alone", "/no-version/foo", "/no-version/foo"},
		{
			// The shape stripVersionPrefix could not see: the gateway puts the
			// version after the service namespace.
			"version after the service namespace",
			"/securitycloud/v1/groups",
			"/securitycloud/groups",
		},
		{
			"the v2 sibling collapses onto the same key",
			"/securitycloud/v2/groups",
			"/securitycloud/groups",
		},
		{
			// UEM Connect nests a service version deeper still.
			"version deep in the path",
			"/securitycloud/uem-connect/v1/connectors",
			"/securitycloud/uem-connect/connectors",
		},
		{
			"two version segments are both removed",
			"/securitycloud/v1/uem-connect/v2/connectors",
			"/securitycloud/uem-connect/connectors",
		},
		{
			// A segment merely starting with "v" is not a version. The old
			// leading-segment check accepted any such segment.
			"a segment that only starts with v is kept",
			"/securitycloud/venafi/groups",
			"/securitycloud/venafi/groups",
		},
		{"trailing version segment", "/securitycloud/groups/v2", "/securitycloud/groups"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stripVersionSegments(tt.path); got != tt.want {
				t.Errorf("stripVersionSegments(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestAPIVersionRank(t *testing.T) {
	tests := []struct {
		path string
		want int
	}{
		{"/v1/foo", 1},
		{"/v3/foo", 3},
		{"/v10/foo", 10},
		{"/preview/foo", -1},
		{"/foo/bar", 0},
		// Ranked by the version wherever it sits. Reading only the leading
		// segment scored both of these 0, which made "prefer the higher
		// version" a tie decided by map iteration order.
		{"/securitycloud/v1/groups", 1},
		{"/securitycloud/v2/groups", 2},
		// The outermost version wins when a path carries two: it is the one
		// that distinguishes siblings.
		{"/securitycloud/v1/uem-connect/v2/connectors", 1},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := apiVersionRank(tt.path); got != tt.want {
				t.Errorf("apiVersionRank(%q) = %d, want %d", tt.path, got, tt.want)
			}
		})
	}
}

// TestDeduplicateVersionedOps_GatewayPathShape covers the case that shipped two
// commands for one endpoint: Jamf Security Cloud published a v2 device-groups
// list beside the deprecated v1, and because the version sits after the service
// namespace the dedup key never collided, so both survived — the deprecated one
// holding the plain "list" name.
func TestDeduplicateVersionedOps_GatewayPathShape(t *testing.T) {
	v1 := &Operation{Name: "list", Method: "GET", Path: "/securitycloud/v1/groups"}
	v2 := &Operation{Name: "groups", Method: "GET", Path: "/securitycloud/v2/groups"}

	got := deduplicateVersionedOps([]*Operation{v1, v2})

	if len(got) != 1 {
		t.Fatalf("expected the two versions to collapse to one op, got %d: %v", len(got), got)
	}
	if got[0] != v2 {
		t.Errorf("expected the v2 op to win, got %s", got[0].Path)
	}
	if len(got[0].FallbackPaths) != 1 || got[0].FallbackPaths[0] != v1.Path {
		t.Errorf("expected v1's path recorded as a fallback, got %v", got[0].FallbackPaths)
	}
}

// TestDeduplicateVersionedOps_DistinctGatewayServicesSurvive guards the other
// direction: stripping every version segment must not merge endpoints that
// merely share a terminal segment across different services.
func TestDeduplicateVersionedOps_DistinctGatewayServicesSurvive(t *testing.T) {
	ops := []*Operation{
		{Name: "list", Method: "GET", Path: "/securitycloud/v1/groups"},
		{Name: "list", Method: "GET", Path: "/device-groups/v1/device-groups"},
		{Name: "list", Method: "GET", Path: "/securitycloud/uem-connect/v1/connectors"},
	}
	if got := deduplicateVersionedOps(ops); len(got) != 3 {
		t.Errorf("expected all 3 distinct endpoints to survive, got %d", len(got))
	}
}

// TestNormalisePlatformPathsDropsTheTenant pins the scope leaving the URL. The
// specs disagree with each other: Security Cloud dropped /tenant/{tenantId} in
// GitOps build v1495, while blueprints, benchmarks, devices, pro and classic
// still declare it. Both have to come out as /{service}[/{version}]{path},
// because the tenant now travels as an X-Tenant-Id header and a tenant segment
// left in a generated path would be sent as a literal.
func TestNormalisePlatformPathsDropsTheTenant(t *testing.T) {
	cases := []struct {
		name    string
		service string
		version string
		in      string
		want    string
	}{
		{
			name:    "declared tenant segment is removed",
			service: "blueprints",
			in:      "/v1/tenant/{tenantId}/blueprints",
			want:    "/blueprints/v1/blueprints",
		},
		{
			name:    "header-scoped spec is left alone",
			service: "securitycloud",
			in:      "/v1/ztna/apps",
			want:    "/securitycloud/v1/ztna/apps",
		},
		{
			name:    "tenant ahead of a sub-namespace version",
			service: "securitycloud",
			in:      "/tenant/{tenantId}/uem-connect/v1/connectors",
			want:    "/securitycloud/uem-connect/v1/connectors",
		},
		{
			name:    "version supplied by the extension",
			service: "securitycloud",
			version: "v1",
			in:      "/tenant/{tenantId}/categories",
			want:    "/securitycloud/v1/categories",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc := map[string]any{"paths": map[string]any{c.in: map[string]any{}}}
			normalisePlatformPaths(doc, c.service, c.version)
			paths := doc["paths"].(map[string]any)
			if _, ok := paths[c.want]; !ok {
				got := make([]string, 0, len(paths))
				for p := range paths {
					got = append(got, p)
				}
				t.Errorf("paths = %v, want %q", got, c.want)
			}
			for p := range paths {
				if strings.Contains(p, "tenant") {
					t.Errorf("path %q still carries a tenant segment", p)
				}
			}
		})
	}
}

// TestParseSchema_NonStringEnumsAreCaptured pins the enum values generated help
// prints for a numeric enum. Security Cloud's recoveryDelayInSec is an enum of
// five integers, required on create, and 0 — the value a caller gets by
// forgetting the field — is rejected; dropping non-strings left the help with
// nothing to list for exactly the field that most needed it.
func TestParseSchema_NonStringEnumsAreCaptured(t *testing.T) {
	schema := &openapi3.Schema{
		Type: &openapi3.Types{"object"},
		Properties: openapi3.Schemas{
			"recoveryDelayInSec": &openapi3.SchemaRef{Value: &openapi3.Schema{
				Type: &openapi3.Types{"integer"},
				// JSON numbers decode to float64, which is what the parser sees.
				Enum: []any{float64(300), float64(1800), float64(28800)},
			}},
			"routingStrategy": &openapi3.SchemaRef{Value: &openapi3.Schema{
				Type: &openapi3.Types{"string"},
				Enum: []any{"RANDOM", "NEAREST"},
			}},
			"enabled": &openapi3.SchemaRef{Value: &openapi3.Schema{
				Type: &openapi3.Types{"boolean"},
				Enum: []any{true},
			}},
			// A value with no useful literal form is dropped rather than
			// printed as Go's default formatting of a composite.
			"weird": &openapi3.SchemaRef{Value: &openapi3.Schema{
				Type: &openapi3.Types{"object"},
				Enum: []any{map[string]any{"a": 1}, nil},
			}},
		},
	}

	got := parseSchema("GroupedGatewayCreate", schema)
	want := map[string][]string{
		"recoveryDelayInSec": {"300", "1800", "28800"},
		"routingStrategy":    {"RANDOM", "NEAREST"},
		"enabled":            {"true"},
		"weird":              nil,
	}
	for name, wantEnum := range want {
		prop := got.Properties[name]
		if prop == nil {
			t.Fatalf("property %q missing from parsed schema", name)
		}
		if !slices.Equal(prop.Enum, wantEnum) {
			t.Errorf("%s enum = %v, want %v", name, prop.Enum, wantEnum)
		}
	}
}

// TestServiceSegment pins the namespace read off servers[0].url, including the
// two shapes one spec drop legitimately mixes.
//
// The GA gateway mounts each namespace at the root, and GitOps build v1807
// dropped /api from the published specs — but the Security Cloud four are
// generated from a different upstream tree that still declares it, so both
// forms arrive together and both have to yield the same namespace.
//
// The "host is not the namespace" case is why this is matched on the URL's path
// rather than on an "/api/" marker: the old implementation cut the URL on
// "/api/", which "{region}.api.jamfcloud.com" does not contain (the host's api
// is dot-delimited, not slash-delimited). It therefore returned "" for every
// v1807 spec, and an empty service silently drops the namespace from every
// generated path — no error, no warning, every command 404s.
func TestServiceSegment(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want string
	}{
		{"GA shape", "https://{region}.api.jamfcloud.com/blueprints", "blueprints"},
		{"multi-segment namespace", "https://{region}.api.jamfcloud.com/ddm/report", "ddm/report"},
		{"pre-v1807 /api still yields the namespace", "https://{region}.apigw.jamf.com/api/blueprints", "blueprints"},
		{"Security Cloud's stage host keeps /api", "https://{region}.api.stage.platform.jamflabs.com/api/securitycloud", "securitycloud"},
		{"trailing slash", "https://{region}.api.jamfcloud.com/devices/", "devices"},
		{"host only", "https://{region}.api.jamfcloud.com", ""},
		{"host with bare /api", "https://{region}.apigw.jamf.com/api", ""},
		{"no servers url", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc := map[string]any{"servers": []any{map[string]any{"url": c.url}}}
			if got := serviceSegment(doc); got != c.want {
				t.Errorf("serviceSegment(%q) = %q, want %q", c.url, got, c.want)
			}
		})
	}

	t.Run("no servers block", func(t *testing.T) {
		if got := serviceSegment(map[string]any{}); got != "" {
			t.Errorf("serviceSegment(no servers) = %q, want \"\"", got)
		}
	})
}

// TestDropUnroutedPlatformOps pins the withholding of an operation a published
// spec declares but the gateway does not route.
//
// platformUnroutedOps is empty — the one entry it ever held is gone, Security
// Cloud's v2 device-group PUT having been fixed on 2026-09-04 — so the entry
// here is a test-local one. The mechanism has to stay pinned while the table is
// empty: the next drop is added under pressure, and a generator pass that
// silently stopped dropping would ship the operation the entry was written to
// withhold. Same reasoning as gateway.TestProbedEntriesCarryTheProbeBasis,
// which pins the probe wording against a temporary entry for want of a live
// one.
func TestDropUnroutedPlatformOps(t *testing.T) {
	const unrouted = "PUT /securitycloud/v2/groups/{groupId}"
	restore := platformUnroutedOps
	platformUnroutedOps = map[string]bool{unrouted: true}
	t.Cleanup(func() { platformUnroutedOps = restore })

	ops := []*Operation{
		{Name: "update", Method: "PUT", Path: "/securitycloud/v1/groups/{groupId}"},
		{Name: "update", Method: "PUT", Path: "/securitycloud/v2/groups/{groupId}"},
		{Name: "list", Method: "GET", Path: "/securitycloud/v2/groups"},
	}

	kept := dropUnroutedPlatformOps(ops)

	if len(kept) != 2 {
		t.Fatalf("kept %d ops, want 2: %+v", len(kept), kept)
	}
	for _, op := range kept {
		if op.Path == "/securitycloud/v2/groups/{groupId}" {
			t.Errorf("unrouted %s %s survived the drop", op.Method, op.Path)
		}
	}
	// The v1 PUT and the routed v2 list both survive — the drop is
	// per-operation, not per-version.
	var sawV1Put, sawV2List bool
	for _, op := range kept {
		switch {
		case op.Method == "PUT" && op.Path == "/securitycloud/v1/groups/{groupId}":
			sawV1Put = true
		case op.Method == "GET" && op.Path == "/securitycloud/v2/groups":
			sawV2List = true
		}
	}
	if !sawV1Put {
		t.Error("v1 PUT was dropped; only the paths named in platformUnroutedOps may be")
	}
	if !sawV2List {
		t.Error("v2 list was dropped; the drop is keyed on method+path, not on version")
	}
}

// TestPlatformUnroutedOpsIsEmptyOrEvidenced fails when platformUnroutedOps
// gains an entry, so that adding one is a deliberate edit to this test rather
// than a quiet table append.
//
// A drop is the CLI asserting the published spec is wrong about what the
// gateway carries, and it costs a command. The bar is a recorded probe plus a
// working operation the declared one would displace; both live in the table's
// own comment. This does not judge the evidence — nothing in code can — it only
// makes the addition visible.
func TestPlatformUnroutedOpsIsEmptyOrEvidenced(t *testing.T) {
	for key := range platformUnroutedOps {
		t.Errorf("platformUnroutedOps names %q — a drop needs a recorded probe in the "+
			"table's comment and a working operation it would otherwise displace; "+
			"state both there, then add the key here", key)
	}
}

// TestPlatformUnroutedOpsAreDeclared fails when an entry in
// platformUnroutedOps no longer matches any operation in the shipped specs.
//
// An entry is removed when the gateway starts routing the endpoint, which no
// spec signal announces — that takes a probe. This covers the other way an
// entry goes stale: upstream withdrawing the path, after which the entry
// silently guards nothing and the next reader takes it as current wire
// knowledge.
func TestPlatformUnroutedOpsAreDeclared(t *testing.T) {
	specsDir, err := filepath.Abs("../../specs/platform")
	if err != nil {
		t.Fatalf("resolving specs dir: %v", err)
	}
	specFiles, err := filepath.Glob(filepath.Join(specsDir, "*.json"))
	if err != nil {
		t.Fatalf("globbing specs: %v", err)
	}
	if len(specFiles) == 0 {
		t.Fatal("no specs in specs/platform/ — nothing to check the table against")
	}

	declared := make(map[string]bool)
	for _, path := range specFiles {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("decoding %s: %v", path, err)
		}
		service := serviceSegment(doc)
		normalisePlatformPaths(doc, service, tenantPathVersion(doc))
		paths, _ := doc["paths"].(map[string]any)
		for p, item := range paths {
			pi, _ := item.(map[string]any)
			for _, method := range []string{"get", "post", "put", "patch", "delete"} {
				if _, ok := pi[method]; ok {
					declared[strings.ToUpper(method)+" "+p] = true
				}
			}
		}
	}

	for key := range platformUnroutedOps {
		if !declared[key] {
			t.Errorf("platformUnroutedOps names %q, which no shipped spec declares — "+
				"remove the entry, or fix its key if a path was renamed", key)
		}
	}
}

// TestPlatformOperationNameOverridesWinOverDerivation pins that an override is
// the final word on an operation's name, and that every entry still matches a
// shipped operation.
//
// Overrides used to be applied before the name-derivation passes, where a pass
// could quietly undo one. Audit is the case that exposed it: two no-param GETs
// under the audit tag (/audit and /audit/sources) both derived "list", so
// resolveNoParamConflicts renamed *both* to their terminal segment — and the
// override naming /audit/v1/audit "list" was lost, shipping the stutter
// `platform audit audit`. Nothing failed; the command was simply misnamed,
// which is why this asserts the resulting name rather than the absence of an
// error.
// TestTwoSpecsSharingATagGetDistinctResourceNames pins the namespace key in
// platformResourceNameOverrides, and asserts the resulting *names* rather than
// the absence of an error, because absence of an error was the symptom.
//
// Two Security Cloud specs tag a resource "activation-profiles": uem-connect,
// which only deploys a profile to a UEM, and the enrollment API, which mints,
// reads, pauses, resumes and deletes them. Both declare the service
// "securitycloud", so a single service-keyed override renamed both to
// uem-activation-profiles and generator/platform's mergeInto folded them into
// one resource — seven operations under one command, six of them enrollment's,
// filed under UEM Connect. checkOperationNameCollisions could not catch it:
// deploy-to-uem collides with none of the six, so `make generate` exited 0 and
// the only visible sign was a command tree that had quietly moved.
func TestTwoSpecsSharingATagGetDistinctResourceNames(t *testing.T) {
	specFiles, err := filepath.Glob(filepath.Join("..", "..", "specs", "platform", "*.json"))
	if err != nil {
		t.Fatalf("globbing specs: %v", err)
	}
	if len(specFiles) == 0 {
		t.Fatal("no specs in specs/platform/ — nothing to check the table against")
	}

	// resource name → the paths it was built from, across every spec.
	paths := make(map[string][]string)
	for _, path := range specFiles {
		resources, err := ParsePlatformSpec(path)
		if err != nil {
			t.Fatalf("ParsePlatformSpec(%s): %v", path, err)
		}
		for _, r := range resources {
			for _, op := range r.Operations {
				paths[r.Name] = append(paths[r.Name], op.Path)
			}
		}
	}

	for name, want := range map[string]string{
		"uem-activation-profiles":        "/securitycloud/uem-connect/v1/",
		"enrollment-activation-profiles": "/securitycloud/v1/",
	} {
		got, ok := paths[name]
		if !ok {
			t.Errorf("no resource named %q — the two activation-profiles tags have merged again, "+
				"or one was renamed; check platformResourceNameOverrides", name)
			continue
		}
		for _, p := range got {
			if !strings.HasPrefix(p, want) {
				t.Errorf("resource %q carries %s, which is not under %s — two specs' operations "+
					"have merged into one resource", name, p, want)
			}
		}
	}

	// Every override value has to name a resource that ships, or the entry is
	// keyed at a level nothing matches — the way the uem-connect entries would
	// be if their namespace changed. A stale key renames nothing and reports
	// nothing.
	for key, value := range platformResourceNameOverrides {
		if _, ok := paths[value]; !ok {
			t.Errorf("platformResourceNameOverrides[%q] = %q, but no shipped resource carries that name — "+
				"the key no longer matches any spec's namespace or service", key, value)
		}
	}
}

func TestPlatformOperationNameOverridesWinOverDerivation(t *testing.T) {
	specsDir, err := filepath.Abs("../../specs/platform")
	if err != nil {
		t.Fatalf("resolving specs dir: %v", err)
	}
	specFiles, err := filepath.Glob(filepath.Join(specsDir, "*.json"))
	if err != nil {
		t.Fatalf("globbing specs: %v", err)
	}
	if len(specFiles) == 0 {
		t.Fatal("no specs in specs/platform/ — nothing to check the table against")
	}

	got := make(map[string]string) // "METHOD path" → operation name
	for _, path := range specFiles {
		resources, err := ParsePlatformSpec(path)
		if err != nil {
			t.Fatalf("ParsePlatformSpec(%s): %v", path, err)
		}
		for _, r := range resources {
			for _, op := range r.Operations {
				got[strings.ToUpper(op.Method)+" "+op.Path] = op.Name
			}
		}
	}

	for key, want := range platformOperationNameOverrides {
		name, ok := got[key]
		if !ok {
			t.Errorf("platformOperationNameOverrides names %q, which no shipped spec declares — "+
				"remove the entry, or fix its key if a path was renamed", key)
			continue
		}
		if name != want {
			t.Errorf("%s: operation name = %q, want the override %q — a derivation pass overwrote it", key, name, want)
		}
	}
}

// TestParseSchema_PropertyAllOfCarriesTypeAndEnum covers the property-level
// composition idiom: `allOf: [{$ref: Enum}]` beside a description of the
// property's own. The constraint and the type sit on the composed item, so a
// property that reads them off itself alone comes out untyped and unconstrained
// — which is how every enum in the three account specs was invisible in help.
func TestParseSchema_PropertyAllOfCarriesTypeAndEnum(t *testing.T) {
	schema := &openapi3.Schema{
		Properties: openapi3.Schemas{
			"region": {Value: &openapi3.Schema{
				Description: "Auth0 region.",
				AllOf: openapi3.SchemaRefs{
					{Value: &openapi3.Schema{
						Type: &openapi3.Types{"string"},
						Enum: []any{"US", "EU", "RAMP"},
					}},
				},
			}},
		},
	}
	s := parseSchema("ConnectionSettings", schema)
	p := s.Properties["region"]
	if p == nil {
		t.Fatal("region property missing")
	}
	if p.Type != "string" {
		t.Errorf("Type = %q, want string — an untyped property drops out of --set completion", p.Type)
	}
	if len(p.Enum) != 3 {
		t.Fatalf("Enum = %v, want the three composed values", p.Enum)
	}
	// The property's own description must survive: it is the reason the spec
	// wraps the ref in an allOf at all.
	if p.Description != "Auth0 region." {
		t.Errorf("Description = %q, want the property's own", p.Description)
	}
}

// TestParseSchema_PropertyAllOfDoesNotOverrideItsOwn pins the precedence: an
// enum or type declared on the property wins over a composed one, so the
// composition is a fallback rather than a rewrite.
func TestParseSchema_PropertyAllOfDoesNotOverrideItsOwn(t *testing.T) {
	schema := &openapi3.Schema{
		Properties: openapi3.Schemas{
			"mode": {Value: &openapi3.Schema{
				Type: &openapi3.Types{"string"},
				Enum: []any{"OWN"},
				AllOf: openapi3.SchemaRefs{
					{Value: &openapi3.Schema{Type: &openapi3.Types{"integer"}, Enum: []any{"COMPOSED"}}},
				},
			}},
		},
	}
	s := parseSchema("T", schema)
	p := s.Properties["mode"]
	if p.Type != "string" {
		t.Errorf("Type = %q, want the property's own", p.Type)
	}
	if len(p.Enum) != 1 || p.Enum[0] != "OWN" {
		t.Errorf("Enum = %v, want only the property's own", p.Enum)
	}
}

// TestParseSchema_UnionVariantsAreAllOfComposed covers a bare oneOf whose
// variants carry no properties of their own — every one of account_sso's four
// connection shapes is allOf[BaseConnectionSettings, {…}]. Reading .Properties
// off the adopted branch got nothing, so the union parsed to an empty object:
// no scaffold fields, no enum lines, and make generate exiting 0.
func TestParseSchema_UnionVariantsAreAllOfComposed(t *testing.T) {
	base := &openapi3.Schema{
		Properties: openapi3.Schemas{
			"name":   {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
			"region": {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}, Enum: []any{"US"}}},
		},
		Required: []string{"name", "region"},
	}
	composed := func(own openapi3.Schemas, required []string) *openapi3.Schema {
		return &openapi3.Schema{
			AllOf: openapi3.SchemaRefs{
				{Value: base},
				{Value: &openapi3.Schema{Properties: own, Required: required}},
			},
		}
	}
	schema := &openapi3.Schema{
		OneOf: openapi3.SchemaRefs{
			{Ref: "#/components/schemas/OidcConnectionSettings", Value: composed(openapi3.Schemas{
				"issuer": {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
			}, []string{"issuer"})},
			{Ref: "#/components/schemas/EntraConnectionSettings", Value: composed(openapi3.Schemas{
				"identityApi": {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}, Enum: []any{"AZURE_ACTIVE_DIRECTORY_V1"}}},
			}, nil)},
		},
	}

	s := parseSchema("ConnectionRequest", schema)
	for _, name := range []string{"name", "region", "issuer"} {
		if _, ok := s.Properties[name]; !ok {
			t.Errorf("missing property %q from the adopted variant; got %v", name, propKeys(s.Properties))
		}
	}
	// Inherited required fields belong to the variant as much as its own do.
	for _, want := range []string{"name", "region", "issuer"} {
		if !slices.Contains(s.Required, want) {
			t.Errorf("Required = %v, missing %q", s.Required, want)
		}
	}
	// A sibling variant's enum is still named, and marked as belonging to no
	// scaffolded field.
	p := s.Properties["identityApi"]
	if p == nil {
		t.Fatalf("sibling variant's property missing; got %v", propKeys(s.Properties))
	}
	if !p.VariantOnly {
		t.Error("sibling-only property not marked VariantOnly — it would be scaffolded into a body that does not accept it")
	}
	if len(p.Enum) != 1 {
		t.Errorf("Enum = %v, want the sibling variant's composed value", p.Enum)
	}
}

// TestParseSchema_PropertyReachedUnion covers a union one level in: a property
// whose own schema is a bare oneOf. Without it the walk stops dead at the
// property, which is where account_sso keeps its whole connection shape.
func TestParseSchema_PropertyReachedUnion(t *testing.T) {
	schema := &openapi3.Schema{
		Properties: openapi3.Schemas{
			"connection": {Value: &openapi3.Schema{
				OneOf: openapi3.SchemaRefs{
					{Value: &openapi3.Schema{Properties: openapi3.Schemas{
						"issuer": {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
					}}},
					{Value: &openapi3.Schema{Properties: openapi3.Schemas{
						"tenantDomain": {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
					}}},
				},
			}},
		},
	}
	s := parseSchema("ConnectionRequest", schema)
	p := s.Properties["connection"]
	if p == nil {
		t.Fatal("connection property missing")
	}
	if p.Type != "object" {
		t.Errorf("Type = %q, want object", p.Type)
	}
	if p.Nested == nil {
		t.Fatal("Nested not populated — the union's fields are unreachable")
	}
	if _, ok := p.Nested.Properties["issuer"]; !ok {
		t.Errorf("first variant's fields not adopted; got %v", propKeys(p.Nested.Properties))
	}
}
