// Copyright 2026, Jamf Software LLC

package parser

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/generator/classic"
)

func TestHasPathParam(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/v1/buildings/{id}", true},
		{"/v1/buildings", false},
		{"/v1/{type}/items", true},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := hasPathParam(tt.path); got != tt.want {
				t.Errorf("hasPathParam(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestPathParams(t *testing.T) {
	params := []*Parameter{
		{Name: "id", In: "path"},
		{Name: "page", In: "query"},
		{Name: "type", In: "path"},
		{Name: "filter", In: "query"},
	}
	got := pathParams(params)
	if len(got) != 2 {
		t.Fatalf("expected 2 path params, got %d", len(got))
	}
	if got[0].Name != "id" || got[1].Name != "type" {
		t.Errorf("path params = [%s, %s], want [id, type]", got[0].Name, got[1].Name)
	}
}

func TestQueryParams(t *testing.T) {
	params := []*Parameter{
		{Name: "id", In: "path"},
		{Name: "page", In: "query"},
		{Name: "filter", In: "query"},
	}
	got := queryParams(params)
	if len(got) != 2 {
		t.Fatalf("expected 2 query params, got %d", len(got))
	}
	if got[0].Name != "page" || got[1].Name != "filter" {
		t.Errorf("query params = [%s, %s], want [page, filter]", got[0].Name, got[1].Name)
	}
}

func TestGoType(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"integer", "int"},
		{"boolean", "bool"},
		{"number", "float64"},
		{"string", "string"},
		{"unknown", "string"},
		{"", "string"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := goType(tt.input); got != tt.want {
				t.Errorf("goType(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFlagType(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"integer", "Int"},
		{"boolean", "Bool"},
		{"number", "Float64"},
		{"string", "String"},
		{"unknown", "String"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := flagType(tt.input); got != tt.want {
				t.Errorf("flagType(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestEscapeQuotes(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"backslash", `a\b`, `a\\b`},
		{"quote", `say "hello"`, `say \"hello\"`},
		{"newline", "line1\nline2", "line1 line2"},
		{"carriage return", "line1\rline2", "line1line2"},
		{"backtick", "use `code`", "use 'code'"},
		{"combined", "a \"b\"\nc", `a \"b\" c`},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := escapeQuotes(tt.input); got != tt.want {
				t.Errorf("escapeQuotes(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestDedupeOperations(t *testing.T) {
	t.Run("keeps first occurrence", func(t *testing.T) {
		ops := []*Operation{
			{Name: "list"},
			{Name: "get"},
			{Name: "list"}, // duplicate
			{Name: "create"},
			{Name: "get"}, // duplicate
		}
		got := dedupeOperations(ops)
		if len(got) != 3 {
			t.Fatalf("expected 3 unique ops, got %d", len(got))
		}
		want := []string{"list", "get", "create"}
		for i, n := range want {
			if got[i].Name != n {
				t.Errorf("deduped[%d] = %q, want %q", i, got[i].Name, n)
			}
		}
	})

	t.Run("prefers get matching collection path", func(t *testing.T) {
		// Simulates SmartComputerGroups: membership/{id} sorts before smart-groups/{id}
		// but smart-groups/{id} matches the collection path → should win.
		ops := []*Operation{
			{Name: "list", Method: "GET", Path: "/v2/computer-groups/smart-groups"},
			{Name: "get", Method: "GET", Path: "/v2/computer-groups/smart-group-membership/{id}"},
			{Name: "get", Method: "GET", Path: "/v2/computer-groups/smart-groups/{id}"},
			{Name: "create", Method: "POST", Path: "/v2/computer-groups/smart-groups"},
		}
		got := dedupeOperations(ops)
		getOp := findOp(got, "get")
		if getOp == nil {
			t.Fatal("no get operation after dedup")
		} else if getOp.Path != "/v2/computer-groups/smart-groups/{id}" {
			t.Errorf("get path = %q, want /v2/computer-groups/smart-groups/{id}", getOp.Path)
		}
	})

	t.Run("no replacement when both get ops miss collection path", func(t *testing.T) {
		ops := []*Operation{
			{Name: "get", Method: "GET", Path: "/v1/foo/bar/{id}"},
			{Name: "get", Method: "GET", Path: "/v1/foo/baz/{id}"},
		}
		got := dedupeOperations(ops)
		// Should keep first (bar) since neither matches a collection path
		getOp := findOp(got, "get")
		if getOp == nil {
			t.Fatal("no get operation after dedup")
		} else if getOp.Path != "/v1/foo/bar/{id}" {
			t.Errorf("get path = %q, want /v1/foo/bar/{id}", getOp.Path)
		}
	})
}

func TestCollectionPath(t *testing.T) {
	tests := []struct {
		name string
		ops  []*Operation
		want string
	}{
		{
			name: "list GET with child takes priority",
			ops: []*Operation{
				{Name: "list", Method: "GET", Path: "/v2/computer-groups/smart-groups"},
				{Name: "get", Method: "GET", Path: "/v2/computer-groups/smart-groups/{id}"},
				{Name: "get", Method: "GET", Path: "/v2/computer-groups/smart-group-membership/{id}"},
			},
			want: "/v2/computer-groups/smart-groups",
		},
		{
			name: "list GET without child is skipped",
			ops: []*Operation{
				{Name: "list", Method: "GET", Path: "/v1/device-compliance/feature-toggle"},
				{Name: "get", Method: "GET", Path: "/v1/device-compliance/computer/{id}"},
			},
			want: "/v1/device-compliance/computer",
		},
		{
			name: "create POST with child",
			ops: []*Operation{
				{Name: "create", Method: "POST", Path: "/v2/enrollment-customizations"},
				{Name: "get", Method: "GET", Path: "/v2/enrollment-customizations/{id}"},
			},
			want: "/v2/enrollment-customizations",
		},
		{
			name: "create POST without child is skipped",
			ops: []*Operation{
				{Name: "create", Method: "POST", Path: "/v1/enrollment-customization/parse-markdown"},
				{Name: "get", Method: "GET", Path: "/v1/enrollment-customization/{id}/all/{panel-id}"},
			},
			want: "",
		},
		{
			name: "fallback to get stripped",
			ops: []*Operation{
				{Name: "get", Method: "GET", Path: "/v1/buildings/{id}"},
			},
			want: "/v1/buildings",
		},
		{
			name: "stripped path with remaining params is skipped",
			ops: []*Operation{
				{Name: "update", Method: "PUT", Path: "/v1/foo/{id}/bar/{subId}"},
				{Name: "get", Method: "GET", Path: "/v1/foo/{id}/bar/{subId}"},
			},
			want: "",
		},
		{
			name: "empty ops",
			ops:  []*Operation{},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := collectionPath(tt.ops)
			if got != tt.want {
				t.Errorf("collectionPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIndexedPathParams(t *testing.T) {
	params := []*Parameter{
		{Name: "filter", In: "query"},
		{Name: "id", In: "path"},
		{Name: "page", In: "query"},
		{Name: "username", In: "path"},
	}
	got := indexedPathParams(params)
	if len(got) != 2 {
		t.Fatalf("expected 2 indexed path params, got %d", len(got))
	}
	if got[0].Index != 0 || got[0].Param.Name != "id" {
		t.Errorf("indexed[0] = {%d, %q}, want {0, id}", got[0].Index, got[0].Param.Name)
	}
	if got[1].Index != 1 || got[1].Param.Name != "username" {
		t.Errorf("indexed[1] = {%d, %q}, want {1, username}", got[1].Index, got[1].Param.Name)
	}
}

func TestSortOperations(t *testing.T) {
	ops := []*Operation{
		{Name: "delete"},
		{Name: "create"},
		{Name: "history"},
		{Name: "list"},
		{Name: "get"},
		{Name: "update"},
		{Name: "export"},
		{Name: "custom-action"},
	}
	sorted := sortOperations(ops)

	// Expected canonical order: list, get, create, update, delete, history, export, custom-action
	expected := []string{"list", "get", "create", "update", "delete", "history", "export", "custom-action"}
	if len(sorted) != len(expected) {
		t.Fatalf("expected %d sorted ops, got %d", len(expected), len(sorted))
	}
	for i, want := range expected {
		if sorted[i].Name != want {
			t.Errorf("sorted[%d] = %q, want %q", i, sorted[i].Name, want)
		}
	}
}

func TestHasPostOrPut(t *testing.T) {
	tests := []struct {
		name string
		ops  []*Operation
		want bool
	}{
		{"with POST", []*Operation{{Method: "GET"}, {Method: "POST"}}, true},
		{"with PUT", []*Operation{{Method: "PUT"}}, true},
		{"with PATCH", []*Operation{{Method: "PATCH"}}, true},
		{"GET only", []*Operation{{Method: "GET"}}, false},
		{"DELETE only", []*Operation{{Method: "DELETE"}}, false},
		{"empty", []*Operation{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasPostOrPut(tt.ops); got != tt.want {
				t.Errorf("hasPostOrPut() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasDelete(t *testing.T) {
	tests := []struct {
		name string
		ops  []*Operation
		want bool
	}{
		{"with DELETE", []*Operation{{Method: "DELETE"}}, true},
		{"without DELETE", []*Operation{{Method: "GET"}, {Method: "POST"}}, false},
		{"empty", []*Operation{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasDelete(tt.ops); got != tt.want {
				t.Errorf("hasDelete() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasDestructive(t *testing.T) {
	tests := []struct {
		name string
		ops  []*Operation
		want bool
	}{
		{"with destructive", []*Operation{{IsDestructive: true}}, true},
		{"without destructive", []*Operation{{IsDestructive: false}}, false},
		{"empty", []*Operation{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasDestructive(tt.ops); got != tt.want {
				t.Errorf("hasDestructive() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNeedsFmt(t *testing.T) {
	tests := []struct {
		name string
		ops  []*Operation
		want bool
	}{
		{"destructive", []*Operation{{IsDestructive: true}}, true},
		{"has delete", []*Operation{{Method: "DELETE"}}, true},
		{"has integer query param", []*Operation{{Parameters: []*Parameter{{In: "query", Type: "integer"}}}}, true},
		{"has array query param", []*Operation{{Parameters: []*Parameter{{In: "query", Type: "string", IsArray: true}}}}, true},
		{"string query param (uses url pkg, not fmt)", []*Operation{{Parameters: []*Parameter{{In: "query", Type: "string"}}}}, false},
		{"only bool query param", []*Operation{{Parameters: []*Parameter{{In: "query", Type: "boolean"}}}}, false},
		{"no special needs", []*Operation{{Method: "GET"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Resource{Operations: tt.ops}
			if got := needsFmt(r); got != tt.want {
				t.Errorf("needsFmt() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGenerate_ProducesValidGoFile(t *testing.T) {
	dir := t.TempDir()
	gen := NewGenerator(dir)

	resource := &Resource{
		Name:         "widgets",
		NameSingular: "widget",
		GoName:       "Widgets",
		Description:  "Test widgets",
		Operations: []*Operation{
			{
				Name:       "list",
				Method:     "GET",
				Path:       "/v1/widgets",
				Summary:    "List widgets",
				APIVersion: "v1",
				Parameters: []*Parameter{
					{Name: "page", In: "query", Type: "integer"},
					{Name: "filter", In: "query", Type: "string"},
				},
			},
			{
				Name:       "get",
				Method:     "GET",
				Path:       "/v1/widgets/{id}",
				Summary:    "Get a widget",
				APIVersion: "v1",
				Parameters: []*Parameter{
					{Name: "id", In: "path", Type: "string", Required: true},
				},
			},
			{
				Name:        "create",
				Method:      "POST",
				Path:        "/v1/widgets",
				Summary:     "Create a widget",
				APIVersion:  "v1",
				RequestBody: &RequestBody{Required: true},
			},
			{
				Name:          "delete",
				Method:        "DELETE",
				Path:          "/v1/widgets/{id}",
				Summary:       "Delete a widget",
				APIVersion:    "v1",
				IsDestructive: true,
				Parameters: []*Parameter{
					{Name: "id", In: "path", Type: "string", Required: true},
				},
			},
		},
		Schemas: make(map[string]*Schema),
	}

	outPath, err := gen.Generate(resource)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	code := string(content)

	checks := []string{
		"package generated",
		CodegenHeader,
		"NewWidgetsCmd",
		`Use:   "widgets"`,
		"List widgets",
		"Get a widget",
		"Create a widget",
		"Delete a widget",
	}
	for _, check := range checks {
		if !strings.Contains(code, check) {
			t.Errorf("generated code missing %q", check)
		}
	}

	// Destructive ops should have --yes flag and confirmation prompt
	if !strings.Contains(code, `"yes"`) {
		t.Error("destructive op should generate --yes flag")
	}
	if !strings.Contains(code, "destructive operation requires --yes") {
		t.Error("destructive op should generate --no-input guard")
	}
}

func TestGenerate_ListOnly(t *testing.T) {
	dir := t.TempDir()
	gen := NewGenerator(dir)

	resource := &Resource{
		Name:         "reports",
		NameSingular: "report",
		GoName:       "Reports",
		Operations: []*Operation{
			{
				Name:       "list",
				Method:     "GET",
				Path:       "/v1/reports",
				Summary:    "List reports",
				APIVersion: "v1",
			},
		},
		Schemas: make(map[string]*Schema),
	}

	outPath, err := gen.Generate(resource)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	code := string(content)

	if !strings.Contains(code, "ListCmd") || !strings.Contains(code, "newReportsList") {
		t.Error("expected list command in output")
	}
	if strings.Contains(code, "CreateCmd") || strings.Contains(code, "newReportsCreate") {
		t.Error("unexpected create command for list-only resource")
	}
	if strings.Contains(code, "DeleteCmd") || strings.Contains(code, "newReportsDelete") {
		t.Error("unexpected delete command for list-only resource")
	}
}

func TestGenerate_DestructiveOps(t *testing.T) {
	dir := t.TempDir()
	gen := NewGenerator(dir)

	resource := &Resource{
		Name:         "devices",
		NameSingular: "device",
		GoName:       "Devices",
		Operations: []*Operation{
			{
				Name:          "erase",
				Method:        "POST",
				Path:          "/v1/devices/{id}/erase",
				Summary:       "Erase a device",
				APIVersion:    "v1",
				IsAction:      true,
				IsDestructive: true,
				Parameters: []*Parameter{
					{Name: "id", In: "path", Type: "string", Required: true},
				},
			},
		},
		Schemas: make(map[string]*Schema),
	}

	outPath, err := gen.Generate(resource)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	code := string(content)

	// Verify --yes flag, --dry-run flag, confirmation prompt
	if !strings.Contains(code, "--yes") {
		t.Error("destructive op should have --yes flag definition")
	}
	if !strings.Contains(code, "dry-run") {
		t.Error("destructive op should have --dry-run flag")
	}
	if !strings.Contains(code, "Type 'yes' to confirm") {
		t.Error("destructive op should have confirmation prompt")
	}
	if !strings.Contains(code, "os.Stderr") {
		t.Error("destructive op should write to stderr")
	}
}

func TestGenerateRegistry(t *testing.T) {
	dir := t.TempDir()
	gen := NewGenerator(dir)

	resources := []*Resource{
		{Name: "widgets", GoName: "Widgets"},
		{Name: "gadgets", GoName: "Gadgets"},
	}

	outPath, err := gen.GenerateRegistry(resources)
	if err != nil {
		t.Fatalf("GenerateRegistry() error = %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	code := string(content)

	if !strings.Contains(code, "RegisterCommands") {
		t.Error("expected RegisterCommands function")
	}
	if !strings.Contains(code, "NewGadgetsCmd") {
		t.Error("expected NewGadgetsCmd registration")
	}
	if !strings.Contains(code, "NewWidgetsCmd") {
		t.Error("expected NewWidgetsCmd registration")
	}

	// Verify sorted order: gadgets before widgets
	gadgetIdx := strings.Index(code, "NewGadgetsCmd")
	widgetIdx := strings.Index(code, "NewWidgetsCmd")
	if gadgetIdx > widgetIdx {
		t.Error("expected gadgets before widgets (sorted by name)")
	}
}

// --- scaffoldJSON tests ---

func TestScaffoldJSON_BasicProperties(t *testing.T) {
	s := &Schema{
		Properties: map[string]*Property{
			"name":    {Type: "string"},
			"count":   {Type: "integer"},
			"enabled": {Type: "boolean"},
		},
	}
	got := scaffoldJSON(s)
	// Verify valid JSON
	var parsed map[string]any
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("scaffoldJSON produced invalid JSON: %v\n%s", err, got)
	}
	if parsed["name"] != "" {
		t.Errorf("name = %v, want empty string", parsed["name"])
	}
	if parsed["count"] != float64(0) {
		t.Errorf("count = %v, want 0", parsed["count"])
	}
	if parsed["enabled"] != false {
		t.Errorf("enabled = %v, want false", parsed["enabled"])
	}
}

func TestScaffoldJSON_SkipsReadOnly(t *testing.T) {
	s := &Schema{
		Properties: map[string]*Property{
			"id":   {Type: "string", ReadOnly: true},
			"name": {Type: "string"},
		},
	}
	got := scaffoldJSON(s)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := parsed["id"]; ok {
		t.Error("readOnly field 'id' should be skipped")
	}
	if _, ok := parsed["name"]; !ok {
		t.Error("writable field 'name' should be present")
	}
}

func TestScaffoldJSON_UsesExamples(t *testing.T) {
	s := &Schema{
		Properties: map[string]*Property{
			"name": {Type: "string", Example: "Apple Park"},
		},
	}
	got := scaffoldJSON(s)
	if !strings.Contains(got, "Apple Park") {
		t.Errorf("expected example value 'Apple Park' in output, got: %s", got)
	}
}

func TestScaffoldJSON_EmptySchema(t *testing.T) {
	if got := scaffoldJSON(nil); got != "{}" {
		t.Errorf("nil schema: got %q, want %q", got, "{}")
	}
	if got := scaffoldJSON(&Schema{}); got != "{}" {
		t.Errorf("empty schema: got %q, want %q", got, "{}")
	}
}

func TestScaffoldJSON_DeterministicOrder(t *testing.T) {
	s := &Schema{
		Properties: map[string]*Property{
			"zebra": {Type: "string"},
			"alpha": {Type: "string"},
			"mike":  {Type: "string"},
		},
	}
	// Run multiple times to check determinism
	first := scaffoldJSON(s)
	for i := range 10 {
		if got := scaffoldJSON(s); got != first {
			t.Fatalf("non-deterministic output on iteration %d:\n%s\nvs\n%s", i, first, got)
		}
	}
	// Verify alphabetical order
	alphaIdx := strings.Index(first, "alpha")
	mikeIdx := strings.Index(first, "mike")
	zebraIdx := strings.Index(first, "zebra")
	if alphaIdx >= mikeIdx || mikeIdx >= zebraIdx {
		t.Errorf("expected alphabetical order (alpha < mike < zebra), got: %s", first)
	}
}

func TestScaffoldJSON_AllTypes(t *testing.T) {
	s := &Schema{
		Properties: map[string]*Property{
			"arr":  {Type: "array"},
			"obj":  {Type: "object"},
			"str":  {Type: "string"},
			"num":  {Type: "integer"},
			"flag": {Type: "boolean"},
		},
	}
	got := scaffoldJSON(s)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// array → []
	if arr, ok := parsed["arr"].([]any); !ok || len(arr) != 0 {
		t.Errorf("arr = %v, want empty array", parsed["arr"])
	}
	// object → {}
	if obj, ok := parsed["obj"].(map[string]any); !ok || len(obj) != 0 {
		t.Errorf("obj = %v, want empty object", parsed["obj"])
	}
	if parsed["str"] != "" {
		t.Errorf("str = %v, want empty string", parsed["str"])
	}
	if parsed["num"] != float64(0) {
		t.Errorf("num = %v, want 0", parsed["num"])
	}
	if parsed["flag"] != false {
		t.Errorf("flag = %v, want false", parsed["flag"])
	}
}

// --- hasScaffold tests ---

func TestHasScaffold_PostWithSchema(t *testing.T) {
	ops := []*Operation{{
		Method:      "POST",
		RequestBody: &RequestBody{Schema: &Schema{Properties: map[string]*Property{"name": {Type: "string"}}}},
	}}
	if !hasScaffold(ops) {
		t.Error("POST with schema should return true")
	}
}

func TestHasScaffold_PutWithSchema(t *testing.T) {
	ops := []*Operation{{
		Method:      "PUT",
		RequestBody: &RequestBody{Schema: &Schema{Properties: map[string]*Property{"name": {Type: "string"}}}},
	}}
	if !hasScaffold(ops) {
		t.Error("PUT with schema should return true")
	}
}

func TestHasScaffold_GetOnly(t *testing.T) {
	ops := []*Operation{{Method: "GET"}}
	if hasScaffold(ops) {
		t.Error("GET-only should return false")
	}
}

func TestHasScaffold_PostNoSchema(t *testing.T) {
	ops := []*Operation{{Method: "POST", RequestBody: nil}}
	if hasScaffold(ops) {
		t.Error("POST with nil RequestBody should return false")
	}
	ops2 := []*Operation{{Method: "POST", RequestBody: &RequestBody{Schema: nil}}}
	if hasScaffold(ops2) {
		t.Error("POST with nil schema should return false")
	}
}

func TestGenerate_ScaffoldFlag(t *testing.T) {
	dir := t.TempDir()
	gen := NewGenerator(dir)

	resource := &Resource{
		Name:         "buildings",
		NameSingular: "building",
		GoName:       "Buildings",
		Operations: []*Operation{
			{
				Name:    "create",
				Method:  "POST",
				Path:    "/v1/buildings",
				Summary: "Create a building",
				RequestBody: &RequestBody{
					Schema: &Schema{
						Properties: map[string]*Property{
							"name": {Type: "string", Example: "Main Office"},
						},
					},
				},
			},
		},
		Schemas: make(map[string]*Schema),
	}

	outPath, err := gen.Generate(resource)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	code := string(content)

	if !strings.Contains(code, "--scaffold") {
		t.Error("expected --scaffold flag in generated code")
	}
	if !strings.Contains(code, "Main Office") {
		t.Error("expected scaffold JSON containing example value 'Main Office'")
	}
}

func TestGenerate_ExampleText(t *testing.T) {
	dir := t.TempDir()
	gen := NewGenerator(dir)

	resource := &Resource{
		Name:         "buildings",
		NameSingular: "building",
		GoName:       "Buildings",
		Operations: []*Operation{
			{Name: "list", Method: "GET", Path: "/v1/buildings", Summary: "List buildings", IsList: true},
			{
				Name: "get", Method: "GET", Path: "/v1/buildings/{id}", Summary: "Get building",
				Parameters: []*Parameter{{Name: "id", In: "path", Type: "string"}},
			},
			{
				Name: "create", Method: "POST", Path: "/v1/buildings", Summary: "Create building",
				RequestBody: &RequestBody{Schema: &Schema{Properties: map[string]*Property{"name": {Type: "string"}}}},
			},
			{
				Name: "update", Method: "PUT", Path: "/v1/buildings/{id}", Summary: "Update building",
				Parameters:  []*Parameter{{Name: "id", In: "path", Type: "string"}},
				RequestBody: &RequestBody{Schema: &Schema{Properties: map[string]*Property{"name": {Type: "string"}}}},
			},
			{
				Name: "delete", Method: "DELETE", Path: "/v1/buildings/{id}", Summary: "Delete building",
				IsDestructive: true, Parameters: []*Parameter{{Name: "id", In: "path", Type: "string"}},
			},
		},
		Schemas: make(map[string]*Schema),
	}

	outPath, err := gen.Generate(resource)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	code := string(content)

	// Each operation should have an Example field
	if !strings.Contains(code, "Example:") {
		t.Error("expected Example: field in generated code")
	}
	// Verify list example contains --field
	if !strings.Contains(code, "--field id") {
		t.Error("expected list example to show --field usage")
	}
	// Verify get example shows -o yaml
	if !strings.Contains(code, "-o yaml") {
		t.Error("expected get example to show -o yaml usage")
	}
	// Verify create example shows --scaffold
	if !strings.Contains(code, "--scaffold") {
		t.Error("expected create example to show --scaffold usage")
	}
}

// --- Error path tests ---

func TestGenerate_BadOutputDir(t *testing.T) {
	gen := NewGenerator("/nonexistent/path/that/does/not/exist")

	resource := &Resource{
		Name:         "widgets",
		NameSingular: "widget",
		GoName:       "Widgets",
		Operations: []*Operation{
			{Name: "list", Method: "GET", Path: "/v1/widgets", Summary: "List widgets"},
		},
		Schemas: make(map[string]*Schema),
	}

	_, err := gen.Generate(resource)
	if err == nil {
		t.Fatal("expected error for nonexistent output dir")
	}
	if !strings.Contains(err.Error(), "creating file") {
		t.Errorf("error = %q, want to contain 'creating file'", err.Error())
	}
}

func TestGenerateRegistry_BadOutputDir(t *testing.T) {
	gen := NewGenerator("/nonexistent/path/that/does/not/exist")

	resources := []*Resource{
		{Name: "widgets", GoName: "Widgets"},
	}

	_, err := gen.GenerateRegistry(resources)
	if err == nil {
		t.Fatal("expected error for nonexistent output dir")
	}
	if !strings.Contains(err.Error(), "creating file") {
		t.Errorf("error = %q, want to contain 'creating file'", err.Error())
	}
}

func TestGenerate_IsList(t *testing.T) {
	dir := t.TempDir()
	gen := NewGenerator(dir)

	resource := &Resource{
		Name:         "items",
		NameSingular: "item",
		GoName:       "Items",
		Operations: []*Operation{
			{
				Name:       "list",
				Method:     "GET",
				Path:       "/v1/items",
				Summary:    "List items",
				IsList:     true,
				APIVersion: "v1",
				Parameters: []*Parameter{
					{Name: "page", In: "query", Type: "integer"},
				},
			},
		},
		Schemas: make(map[string]*Schema),
	}

	outPath, err := gen.Generate(resource)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	code := string(content)

	// IsList should generate auto-pagination with --all and --limit flags
	if !strings.Contains(code, "flagAll") {
		t.Error("expected --all flag for list operation")
	}
	if !strings.Contains(code, "flagLimit") {
		t.Error("expected --limit flag for list operation")
	}
	if !strings.Contains(code, "allResults") {
		t.Error("expected auto-pagination logic")
	}
}

func TestGenerate_DeleteMultipleOp(t *testing.T) {
	dir := t.TempDir()
	gen := NewGenerator(dir)

	resource := &Resource{
		Name:         "items",
		NameSingular: "item",
		GoName:       "Items",
		Operations: []*Operation{
			{
				Name:          "delete-multiple",
				Method:        "POST",
				Path:          "/v1/items/delete-multiple",
				Summary:       "Delete multiple items",
				APIVersion:    "v1",
				IsDestructive: true,
			},
		},
		Schemas: make(map[string]*Schema),
	}

	outPath, err := gen.Generate(resource)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	code := string(content)

	if !strings.Contains(code, "flagIds") {
		t.Error("expected --ids flag for delete-multiple")
	}
	if !strings.Contains(code, "delete-multiple") {
		t.Error("expected delete-multiple in example text")
	}
}

func TestGenerate_ArrayQueryParam(t *testing.T) {
	dir := t.TempDir()
	gen := NewGenerator(dir)

	resource := &Resource{
		Name:         "reports",
		NameSingular: "report",
		GoName:       "Reports",
		Operations: []*Operation{
			{
				Name:       "list",
				Method:     "GET",
				Path:       "/v1/reports",
				Summary:    "List reports",
				APIVersion: "v1",
				Parameters: []*Parameter{
					{Name: "filter", In: "query", Type: "string", IsArray: true},
				},
			},
		},
		Schemas: make(map[string]*Schema),
	}

	outPath, err := gen.Generate(resource)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	code := string(content)

	if !strings.Contains(code, "StringSliceVar") {
		t.Error("expected StringSliceVar for array query param")
	}
}

func TestGenerate_Filename(t *testing.T) {
	dir := t.TempDir()
	gen := NewGenerator(dir)

	resource := &Resource{
		Name:         "mobile-devices",
		NameSingular: "mobile-device",
		GoName:       "MobileDevices",
		Operations: []*Operation{
			{
				Name:       "list",
				Method:     "GET",
				Path:       "/v1/mobile-devices",
				Summary:    "List mobile devices",
				APIVersion: "v1",
			},
		},
		Schemas: make(map[string]*Schema),
	}

	outPath, err := gen.Generate(resource)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	expectedFile := filepath.Join(dir, "mobile_devices.go")
	if outPath != expectedFile {
		t.Errorf("output path = %q, want %q", outPath, expectedFile)
	}
}

// --- opHasNameLookup tests ---

func TestOpHasNameLookup(t *testing.T) {
	listOp := &Operation{Name: "list", Method: "GET", Path: "/v1/things", IsList: true}
	tests := []struct {
		name string
		op   *Operation
		r    *Resource
		want bool
	}{
		{
			"get with path param on listable resource — lookup generated",
			&Operation{Name: "get", Method: "GET", Path: "/v1/things/{id}"},
			&Resource{Operations: []*Operation{listOp, {Name: "get", Method: "GET", Path: "/v1/things/{id}"}}, IDField: "id"},
			true,
		},
		{
			"delete with path param on listable resource — lookup generated",
			&Operation{Name: "delete", Method: "DELETE", Path: "/v1/things/{id}"},
			&Resource{Operations: []*Operation{listOp, {Name: "delete", Method: "DELETE", Path: "/v1/things/{id}"}}, IDField: "id"},
			true,
		},
		{
			"op without path param — no lookup",
			&Operation{Name: "get", Method: "GET", Path: "/v1/things"},
			&Resource{Operations: []*Operation{listOp, {Name: "get", Method: "GET", Path: "/v1/things"}}, IDField: "id"},
			false,
		},
		{
			"no ops at all — no collection path — no lookup",
			&Operation{Name: "get", Method: "GET", Path: "/v1/things/{id}"},
			&Resource{Operations: []*Operation{}, IDField: "id"},
			false,
		},
		{
			"singleton — no lookup",
			&Operation{Name: "get", Method: "GET", Path: "/v1/things/{id}"},
			&Resource{IsSingleton: true, Operations: []*Operation{listOp, {Name: "get", Method: "GET", Path: "/v1/things/{id}"}}, IDField: "id"},
			false,
		},
		{
			"sub-resource with multi-param path — lookup suppressed",
			&Operation{Name: "get", Method: "GET", Path: "/v1/foo/{id}/bars/{barId}"},
			&Resource{Operations: []*Operation{listOp, {Name: "get", Method: "GET", Path: "/v1/foo/{id}/bars/{barId}"}}, IDField: "id"},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := opHasNameLookup(tt.op, tt.r); got != tt.want {
				t.Errorf("opHasNameLookup() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGenerate_DeleteByNameCommand(t *testing.T) {
	dir := t.TempDir()
	gen := NewGenerator(dir)

	resource := &Resource{
		Name:         "buildings",
		NameSingular: "building",
		GoName:       "Buildings",
		NameField:    "name",
		IDField:      "id",
		Operations: []*Operation{
			{Name: "list", Method: "GET", Path: "/v1/buildings", Summary: "List", IsList: true},
			{
				Name: "get", Method: "GET", Path: "/v1/buildings/{id}", Summary: "Get",
				Parameters: []*Parameter{{Name: "id", In: "path", Type: "string"}},
			},
			{
				Name: "delete", Method: "DELETE", Path: "/v1/buildings/{id}", Summary: "Delete",
				IsDestructive: true, Parameters: []*Parameter{{Name: "id", In: "path", Type: "string"}},
			},
		},
		Schemas: make(map[string]*Schema),
	}

	outPath, err := gen.Generate(resource)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	code := string(content)

	// delete command should have --name flag and inline name resolution (not a separate subcommand)
	checks := []string{
		`flagName`,
		`resolveNameToID`,
		`"name", "id", flagName`,
		"/v1/buildings/{id}",
		`"yes"`,
		"dry-run",
		`StringVar(&flagName, "name"`,
	}

	for _, check := range checks {
		if !strings.Contains(code, check) {
			t.Errorf("generated delete --name code missing %q", check)
		}
	}

	// should NOT generate a separate delete-by-name subcommand
	if strings.Contains(code, "DeleteByNameCmd") {
		t.Error("should not generate a separate DeleteByNameCmd")
	}
}

func TestGenerate_NoDeleteByName_WithoutDelete(t *testing.T) {
	dir := t.TempDir()
	gen := NewGenerator(dir)

	resource := &Resource{
		Name:         "reports",
		NameSingular: "report",
		GoName:       "Reports",
		Operations: []*Operation{
			{Name: "list", Method: "GET", Path: "/v1/reports", Summary: "List"},
			{
				Name: "get", Method: "GET", Path: "/v1/reports/{id}", Summary: "Get",
				Parameters: []*Parameter{{Name: "id", In: "path", Type: "string"}},
			},
		},
		Schemas: make(map[string]*Schema),
	}

	outPath, err := gen.Generate(resource)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	code := string(content)

	if strings.Contains(code, "DeleteByNameCmd") {
		t.Error("unexpected delete-by-name for resource without delete operation")
	}
}

// --- hasApply tests ---

func TestHasApply(t *testing.T) {
	tests := []struct {
		name string
		ops  []*Operation
		want bool
	}{
		{
			"create and update",
			[]*Operation{
				{Name: "create", Method: "POST"},
				{Name: "update", Method: "PUT"},
			},
			true,
		},
		{
			"create only",
			[]*Operation{
				{Name: "create", Method: "POST"},
			},
			false,
		},
		{
			"update only",
			[]*Operation{
				{Name: "update", Method: "PUT"},
			},
			false,
		},
		{
			"list and get only",
			[]*Operation{
				{Name: "list", Method: "GET"},
				{Name: "get", Method: "GET"},
			},
			false,
		},
		{
			"create POST but update is PATCH not PUT",
			[]*Operation{
				{Name: "create", Method: "POST"},
				{Name: "update", Method: "PATCH"},
			},
			false,
		},
		{"empty", []*Operation{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasApply(tt.ops); got != tt.want {
				t.Errorf("hasApply() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGenerate_ApplyCommand(t *testing.T) {
	dir := t.TempDir()
	gen := NewGenerator(dir)

	resource := &Resource{
		Name:         "buildings",
		NameSingular: "building",
		GoName:       "Buildings",
		NameField:    "name",
		Operations: []*Operation{
			{Name: "list", Method: "GET", Path: "/v1/buildings", Summary: "List buildings", IsList: true},
			{
				Name: "get", Method: "GET", Path: "/v1/buildings/{id}", Summary: "Get building",
				Parameters: []*Parameter{{Name: "id", In: "path", Type: "string"}},
			},
			{
				Name: "create", Method: "POST", Path: "/v1/buildings", Summary: "Create building",
				RequestBody: &RequestBody{Schema: &Schema{Properties: map[string]*Property{"name": {Type: "string"}}}},
			},
			{
				Name: "update", Method: "PUT", Path: "/v1/buildings/{id}", Summary: "Update building",
				Parameters:  []*Parameter{{Name: "id", In: "path", Type: "string"}},
				RequestBody: &RequestBody{Schema: &Schema{Properties: map[string]*Property{"name": {Type: "string"}}}},
			},
		},
		Schemas: make(map[string]*Schema),
	}

	outPath, err := gen.Generate(resource)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	code := string(content)

	// Apply command should be present
	checks := []string{
		"newBuildingsApplyCmd",
		`"apply"`,
		"Create or replace a building by name",
		"--from-file",
		`"yes"`,
		"dry-run",
		"readApplyInput",
		"extractJSONField",
		"resolveNameToIDForApply",
		`"name"`,             // nameField
		"/v1/buildings",      // createPath
		"/v1/buildings/{id}", // updatePath
		"bytes.NewReader",
		`Created building`,
		`Replaced building`,
		`[dry-run] Would create building`,
		`[dry-run] Would replace building`,
	}

	for _, check := range checks {
		if !strings.Contains(code, check) {
			t.Errorf("generated apply code missing %q", check)
		}
	}

	// Imports should include "bytes"
	if !strings.Contains(code, `"bytes"`) {
		t.Error("expected bytes import for apply command")
	}
}

func TestGenerate_ApplyWithDisplayName(t *testing.T) {
	dir := t.TempDir()
	gen := NewGenerator(dir)

	resource := &Resource{
		Name:         "api-roles",
		NameSingular: "API role",
		GoName:       "ApiRoles",
		NameField:    "displayName",
		Operations: []*Operation{
			{Name: "create", Method: "POST", Path: "/v1/api-roles", Summary: "Create"},
			{
				Name: "update", Method: "PUT", Path: "/v1/api-roles/{id}", Summary: "Update",
				Parameters: []*Parameter{{Name: "id", In: "path", Type: "string"}},
			},
		},
		Schemas: make(map[string]*Schema),
	}

	outPath, err := gen.Generate(resource)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	code := string(content)

	// Should use displayName instead of name
	if !strings.Contains(code, `"displayName"`) {
		t.Error("expected displayName as the name field in apply command")
	}
}

func TestGenerate_NoApply_WithoutUpdate(t *testing.T) {
	dir := t.TempDir()
	gen := NewGenerator(dir)

	resource := &Resource{
		Name:         "reports",
		NameSingular: "report",
		GoName:       "Reports",
		Operations: []*Operation{
			{Name: "list", Method: "GET", Path: "/v1/reports", Summary: "List"},
			{Name: "create", Method: "POST", Path: "/v1/reports", Summary: "Create"},
		},
		Schemas: make(map[string]*Schema),
	}

	outPath, err := gen.Generate(resource)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	code := string(content)

	if strings.Contains(code, "ApplyCmd") {
		t.Error("unexpected apply command for resource without update operation")
	}
}

func TestNeedsFmt_WithApply(t *testing.T) {
	r := &Resource{
		Operations: []*Operation{
			{Name: "create", Method: "POST", Path: "/v1/widgets"},
			{Name: "update", Method: "PUT", Path: "/v1/widgets/{id}"},
		},
	}
	if !needsFmt(r) {
		t.Error("needsFmt should return true when hasApply is true")
	}
}

func TestNeedsURL_WithApply(t *testing.T) {
	r := &Resource{
		Operations: []*Operation{
			{Name: "create", Method: "POST", Path: "/v1/widgets"},
			{Name: "update", Method: "PUT", Path: "/v1/widgets/{id}"},
		},
	}
	if !needsURL(r) {
		t.Error("needsURL should return true when hasApply is true")
	}
}

func TestGeneratedFiles_HaveCodegenHeader(t *testing.T) {
	generatedDir := filepath.Join("..", "..", "internal", "commands", "pro", "generated")

	entries, err := os.ReadDir(generatedDir)
	if err != nil {
		t.Skipf("generated directory not found (run from repo root): %v", err)
	}

	modernHeader := CodegenHeader
	classicHeader := classic.CodegenHeader

	var checked int
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}

		path := filepath.Join(generatedDir, entry.Name())
		content, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("failed to read %s: %v", entry.Name(), err)
			continue
		}

		lines := strings.SplitN(string(content), "\n", 3)
		hasCopyright := lines[0] == "// Copyright 2026, Jamf Software LLC"
		headerLine := lines[0]
		if hasCopyright && len(lines) > 1 {
			headerLine = lines[1]
		}
		if headerLine != modernHeader && headerLine != classicHeader {
			t.Errorf("%s: missing code generation header\n  got:  %q\n  want: %q or %q",
				entry.Name(), headerLine, modernHeader, classicHeader)
		}
		if !hasCopyright {
			t.Errorf("%s: missing copyright header on first line", entry.Name())
		}
		checked++
	}

	if checked == 0 {
		t.Skip("no .go files found in generated directory")
	}
	t.Logf("verified %d generated files have correct headers", checked)
}

// --- shouldGenerateApply tests ---

func TestShouldGenerateApply(t *testing.T) {
	tests := []struct {
		name        string
		isSingleton bool
		ops         []*Operation
		want        bool
	}{
		{
			name:        "non-singleton with create+update — apply generated",
			isSingleton: false,
			ops: []*Operation{
				{Name: "create", Method: "POST", Path: "/v1/widgets"},
				{Name: "update", Method: "PUT", Path: "/v1/widgets/{id}"},
			},
			want: true,
		},
		{
			name:        "singleton with create+update — apply suppressed",
			isSingleton: true,
			ops: []*Operation{
				{Name: "get", Method: "GET", Path: "/v1/settings"},
				{Name: "create", Method: "POST", Path: "/v1/settings/register"},
				{Name: "update", Method: "PUT", Path: "/v1/settings"},
			},
			want: false,
		},
		{
			name:        "non-singleton with only update — no apply",
			isSingleton: false,
			ops: []*Operation{
				{Name: "update", Method: "PUT", Path: "/v1/widgets/{id}"},
			},
			want: false,
		},
		{
			name:        "non-singleton with only create — no apply",
			isSingleton: false,
			ops: []*Operation{
				{Name: "create", Method: "POST", Path: "/v1/widgets"},
			},
			want: false,
		},
		{
			name:        "sub-resource with multi-param create+update — apply suppressed",
			isSingleton: false,
			ops: []*Operation{
				{Name: "create", Method: "POST", Path: "/v1/foo/{id}/bars"},
				{Name: "update", Method: "PUT", Path: "/v1/foo/{id}/bars/{barId}"},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Resource{IsSingleton: tt.isSingleton, Operations: tt.ops}
			got := shouldGenerateApply(r)
			if got != tt.want {
				t.Errorf("shouldGenerateApply() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGenerate_GetNameFlag(t *testing.T) {
	// Verify that get/update/delete commands gain --name flag when the resource
	// is non-singleton, has a list op, and the operation has a single path param.
	dir := t.TempDir()
	gen := NewGenerator(dir)

	resource := &Resource{
		Name:         "buildings",
		NameSingular: "building",
		GoName:       "Buildings",
		NameField:    "name",
		Operations: []*Operation{
			{Name: "list", Method: "GET", Path: "/v1/buildings", Summary: "List", IsList: true},
			{
				Name: "get", Method: "GET", Path: "/v1/buildings/{id}", Summary: "Get",
				Parameters: []*Parameter{{Name: "id", In: "path", Type: "string"}},
			},
			{
				Name: "update", Method: "PUT", Path: "/v1/buildings/{id}", Summary: "Update",
				Parameters:  []*Parameter{{Name: "id", In: "path", Type: "string"}},
				RequestBody: &RequestBody{Schema: &Schema{Properties: map[string]*Property{"name": {Name: "name", Type: "string"}}}},
			},
		},
		Schemas: make(map[string]*Schema),
	}

	outPath, err := gen.Generate(resource)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	code := string(content)

	// get command should have --name flag for name-based lookup
	if !strings.Contains(code, `StringVar(&flagName, "name"`) {
		t.Error("expected --name flag on get command for listable resource")
	}
	if !strings.Contains(code, `resolveNameToID`) {
		t.Error("expected resolveNameToID call for --name lookup")
	}
	// should NOT generate a separate get-by-name subcommand
	if strings.Contains(code, "GetByNameCmd") {
		t.Error("should not generate a separate GetByNameCmd")
	}
	// update command should accept optional ID (MaximumNArgs(1)) and --name flag
	if !strings.Contains(code, `Use:   "update [<id>]"`) {
		t.Error("expected update command to have optional ID arg (MaximumNArgs(1))")
	}
	if !strings.Contains(code, `MaximumNArgs(1)`) {
		t.Error("expected MaximumNArgs(1) on update command")
	}
}

func TestGenerate_SingletonResource(t *testing.T) {
	dir := t.TempDir()

	r := &Resource{
		Name:         "cache-settings",
		NameSingular: "cache-settings",
		GoName:       "CacheSettings",
		IsSingleton:  true,
		Operations: []*Operation{
			{Name: "get", Method: "GET", Path: "/v1/cache-settings"},
			{
				Name: "update", Method: "PUT", Path: "/v1/cache-settings",
				RequestBody: &RequestBody{Schema: &Schema{
					Properties: map[string]*Property{
						"name": {Name: "name", Type: "string"},
					},
				}},
			},
		},
		Schemas:   map[string]*Schema{},
		NameField: "name",
	}

	g := NewGenerator(dir)
	outPath, err := g.Generate(r)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading output file: %v", err)
	}
	code := string(content)

	// Should have get command, not list
	if !strings.Contains(code, `Use:   "get"`) {
		t.Error("singleton should generate 'get' command, not 'list'")
	}
	if strings.Contains(code, `Use:   "list"`) {
		t.Error("singleton should not generate 'list' command")
	}

	// Should NOT generate apply command
	if strings.Contains(code, "ApplyCmd") {
		t.Error("singleton should not generate apply command")
	}
	if strings.Contains(code, `Use:   "apply"`) {
		t.Error("singleton should not have apply subcommand")
	}

	// Should NOT import "bytes" (only used by apply)
	if strings.Contains(code, `"bytes"`) {
		t.Error("singleton without apply should not import 'bytes'")
	}

	// Should still have update command
	if !strings.Contains(code, `Use:   "update"`) {
		t.Error("singleton should still generate 'update' command")
	}

	// Command group should use the singleton name
	if !strings.Contains(code, `Use:   "cache-settings"`) {
		t.Error("singleton command group should use unsuffixed name 'cache-settings'")
	}
}

func TestGenerate_SingletonResource_NoGetByName(t *testing.T) {
	dir := t.TempDir()

	r := &Resource{
		Name:         "jamf-protect",
		NameSingular: "jamf-protect",
		GoName:       "JamfProtect",
		IsSingleton:  true,
		Operations: []*Operation{
			{Name: "get", Method: "GET", Path: "/v1/jamf-protect"},
			{Name: "update", Method: "PUT", Path: "/v1/jamf-protect"},
			{Name: "delete", Method: "DELETE", Path: "/v1/jamf-protect"},
			{Name: "create", Method: "POST", Path: "/v1/jamf-protect/register"},
		},
		Schemas:   map[string]*Schema{},
		NameField: "name",
	}

	g := NewGenerator(dir)
	outPath, err := g.Generate(r)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading output file: %v", err)
	}
	code := string(content)

	// Should not have get-by-name (no ID path param)
	if strings.Contains(code, "GetByNameCmd") {
		t.Error("singleton should not generate get-by-name command")
	}
	// Should not have delete-by-name (no ID path param on get)
	if strings.Contains(code, "DeleteByNameCmd") {
		t.Error("singleton should not generate delete-by-name command")
	}
	// Should not have apply (IsSingleton)
	if strings.Contains(code, "ApplyCmd") {
		t.Error("singleton should not generate apply command")
	}
}

func TestNeedsURL_SingletonSkipsApply(t *testing.T) {
	// A singleton resource with create+update: needsURL should return false
	// because shouldGenerateApply is false (no apply = no url.PathEscape in apply code)
	// and there are no path params or string query params.
	r := &Resource{
		IsSingleton: true,
		Operations: []*Operation{
			{Name: "get", Method: "GET", Path: "/v1/settings"},
			{Name: "update", Method: "PUT", Path: "/v1/settings"},
			{Name: "create", Method: "POST", Path: "/v1/settings/register"},
		},
	}
	if needsURL(r) {
		t.Error("singleton with no path params/query params should not need net/url import")
	}

	// A non-singleton with create+update: needsURL should return true (apply uses url.PathEscape)
	rNormal := &Resource{
		IsSingleton: false,
		Operations: []*Operation{
			{Name: "list", Method: "GET", Path: "/v1/things", IsList: true},
			{Name: "create", Method: "POST", Path: "/v1/things"},
			{Name: "update", Method: "PUT", Path: "/v1/things/{id}"},
		},
	}
	if !needsURL(rNormal) {
		t.Error("non-singleton with apply should need net/url import")
	}
}

func TestSafeFilename(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		// GOOS collisions — must get _resource suffix
		{"self-service-branding-ios", "self_service_branding_ios_resource.go"},
		{"self-service-branding-darwin", "self_service_branding_darwin_resource.go"},
		{"self-service-branding-windows", "self_service_branding_windows_resource.go"},
		{"self-service-branding-linux", "self_service_branding_linux_resource.go"},
		{"self-service-branding-android", "self_service_branding_android_resource.go"},
		// GOARCH collisions — must get _resource suffix
		{"device-arm64", "device_arm64_resource.go"},
		{"device-amd64", "device_amd64_resource.go"},
		{"device-wasm", "device_wasm_resource.go"},
		// Normal names — no suffix added
		{"buildings", "buildings.go"},
		{"self-service-settings", "self_service_settings.go"},
		{"mobile-device-prestages", "mobile_device_prestages.go"},
		{"cache", "cache.go"},
		// Names that merely contain an OS word mid-name — no suffix
		{"ios-apps", "ios_apps.go"},
		{"linux-agents", "linux_agents.go"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := safeFilename(tt.name)
			if got != tt.want {
				t.Errorf("safeFilename(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestGenerate_MultiParamCommand(t *testing.T) {
	dir := t.TempDir()
	gen := NewGenerator(dir)

	resource := &Resource{
		Name:         "laps",
		NameSingular: "lap",
		GoName:       "Laps",
		Description:  "LAPS passwords",
		NameField:    "username",
		Operations: []*Operation{
			{
				Name:       "list",
				Method:     "GET",
				Path:       "/v2/laps/pending-rotations",
				Summary:    "List pending rotations",
				APIVersion: "v2",
			},
			{
				Name:       "accounts",
				Method:     "GET",
				Path:       "/v2/laps/{clientManagementId}/accounts",
				Summary:    "Get LAPS accounts",
				APIVersion: "v2",
				Parameters: []*Parameter{
					{Name: "clientManagementId", In: "path", Type: "string", Required: true},
				},
			},
			{
				Name:       "audit",
				Method:     "GET",
				Path:       "/v2/laps/{clientManagementId}/account/{username}/audit",
				Summary:    "Get audit history",
				APIVersion: "v2",
				Parameters: []*Parameter{
					{Name: "clientManagementId", In: "path", Type: "string", Required: true},
					{Name: "username", In: "path", Type: "string", Required: true},
				},
			},
		},
		Schemas: make(map[string]*Schema),
	}

	outPath, err := gen.Generate(resource)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	code := string(content)

	// Single-param command should use <id>
	if !strings.Contains(code, `"accounts <id>"`) {
		t.Error("single-param command should use <id> in usage")
	}

	// Multi-param command should use named params
	if !strings.Contains(code, `"audit <clientManagementId> <username>"`) {
		t.Error("multi-param command should use named params in usage")
	}

	// Multi-param command should use ExactArgs(2)
	if !strings.Contains(code, "cobra.ExactArgs(2)") {
		t.Error("multi-param command should use ExactArgs(2)")
	}

	// Path replacement should use args[0] and args[1]
	if !strings.Contains(code, `url.PathEscape(args[0])`) {
		t.Error("should use args[0] for first path param")
	}
	if !strings.Contains(code, `url.PathEscape(args[1])`) {
		t.Error("should use args[1] for second path param")
	}

	// Single-param command should still use ExactArgs(1)
	if !strings.Contains(code, "cobra.ExactArgs(1)") {
		t.Error("single-param command should use ExactArgs(1)")
	}
}

func findOp(ops []*Operation, name string) *Operation {
	for _, op := range ops {
		if op.Name == name {
			return op
		}
	}
	return nil
}

func TestGenerate_IDFieldInNameResolution(t *testing.T) {
	dir := t.TempDir()
	gen := NewGenerator(dir)

	resource := &Resource{
		Name:         "renewals",
		NameSingular: "renewal",
		GoName:       "Renewals",
		Description:  "MDM renewals",
		NameField:    "name",
		IDField:      "clientManagementId",
		Operations: []*Operation{
			{
				Name:       "list",
				Method:     "GET",
				Path:       "/v1/mdm-renewal/device-common-details",
				Summary:    "List MDM renewals",
				IsList:     true,
				APIVersion: "v1",
			},
			{
				Name:       "get",
				Method:     "GET",
				Path:       "/v1/mdm-renewal/device-common-details/{clientManagementId}",
				Summary:    "Get a renewal",
				APIVersion: "v1",
				Parameters: []*Parameter{
					{Name: "clientManagementId", In: "path", Type: "string", Required: true},
				},
			},
			{
				Name:   "create",
				Method: "POST",
				Path:   "/v1/mdm-renewal/device-common-details",
				RequestBody: &RequestBody{
					Schema: &Schema{
						Properties: map[string]*Property{"name": {}},
					},
				},
				APIVersion: "v1",
			},
			{
				Name:   "update",
				Method: "PUT",
				Path:   "/v1/mdm-renewal/device-common-details/{clientManagementId}",
				RequestBody: &RequestBody{
					Schema: &Schema{
						Properties: map[string]*Property{"name": {}},
					},
				},
				APIVersion: "v1",
				Parameters: []*Parameter{
					{Name: "clientManagementId", In: "path", Type: "string", Required: true},
				},
			},
		},
		Schemas: map[string]*Schema{
			"Renewal": {
				Properties: map[string]*Property{
					"id":                 {},
					"clientManagementId": {},
					"name":               {},
				},
			},
		},
	}

	outPath, err := gen.Generate(resource)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	code := string(content)

	// get (with --name flag) should use the custom ID field
	if !strings.Contains(code, `"name", "clientManagementId"`) {
		t.Error("get --name should use clientManagementId as ID field")
	}

	// apply should use the custom ID field
	if !strings.Contains(code, `"name", "clientManagementId", name, noInput`) {
		t.Error("apply should use clientManagementId as ID field")
	}

	// Should NOT contain the old hardcoded pattern (id field as 3rd positional arg = "id")
	// when the resource has a non-standard ID field.
	// We check that the resolve call doesn't use "id" for this resource.
	if strings.Contains(code, `"name", "id", args[0]`) {
		t.Error("should not use hardcoded 'id' when IDField is clientManagementId")
	}
}

// ── Patch helpers ────────────────────────────────────────────────────────────

func TestFlattenSchemaToScalarFields(t *testing.T) {
	schemas := map[string]*Schema{
		"GeneralUpdate": {
			Properties: map[string]*Property{
				"name":     {Type: "string"},
				"managed":  {Type: "boolean"},
				"readOnly": {Type: "string", ReadOnly: true},
				"ids":      {Type: "array"},
			},
		},
	}

	root := &Schema{
		Properties: map[string]*Property{
			"udid": {Type: "string"},
			// object with inline Nested (cross-file $ref pattern)
			"general": {
				Type: "object",
				Nested: &Schema{
					Properties: map[string]*Property{
						"name":    {Type: "string"},
						"managed": {Type: "boolean"},
						"ids":     {Type: "array"}, // excluded — array
					},
				},
			},
			// object resolved via SchemaRef (same-file component)
			"purchasing": {
				Type:      "object",
				SchemaRef: "GeneralUpdate",
			},
			// array at top level — excluded
			"extensionAttributes": {Type: "array"},
		},
	}

	fields := flattenSchemaToScalarFields(root, schemas)

	byPath := make(map[string]string, len(fields))
	for _, f := range fields {
		byPath[f.Path] = f.Type
	}

	// top-level scalar included
	if byPath["udid"] != "string" {
		t.Errorf("expected udid=string, got %q", byPath["udid"])
	}
	// nested via Nested
	if byPath["general.name"] != "string" {
		t.Errorf("expected general.name=string, got %q", byPath["general.name"])
	}
	if byPath["general.managed"] != "boolean" {
		t.Errorf("expected general.managed=boolean, got %q", byPath["general.managed"])
	}
	// nested via SchemaRef
	if byPath["purchasing.name"] != "string" {
		t.Errorf("expected purchasing.name=string, got %q", byPath["purchasing.name"])
	}
	// read-only excluded
	if _, ok := byPath["purchasing.readOnly"]; ok {
		t.Error("read-only field should be excluded")
	}
	// arrays excluded
	if _, ok := byPath["general.ids"]; ok {
		t.Error("array field inside nested schema should be excluded")
	}
	if _, ok := byPath["extensionAttributes"]; ok {
		t.Error("top-level array should be excluded")
	}
}

func TestPatchHasLookup(t *testing.T) {
	patchOp := &Operation{
		Name:   "patch",
		Method: "PATCH",
		Path:   "/v1/things/{id}",
		RequestBody: &RequestBody{
			Schema: &Schema{Properties: map[string]*Property{"name": {Type: "string"}}},
		},
	}
	listOp := &Operation{Name: "list", Method: "GET", Path: "/v1/things"}

	tests := []struct {
		name string
		r    *Resource
		want bool
	}{
		{
			name: "singleton has no lookup",
			r:    &Resource{IsSingleton: true, Operations: []*Operation{patchOp}},
			want: false,
		},
		{
			name: "no list path — no lookup",
			r:    &Resource{Operations: []*Operation{patchOp}, IDField: "id"},
			want: false,
		},
		{
			name: "patch without path param — no lookup",
			r: &Resource{
				Operations: []*Operation{
					listOp,
					{
						Name: "patch", Method: "PATCH", Path: "/v1/things",
						RequestBody: &RequestBody{Schema: &Schema{}},
					},
				},
				IDField: "id",
			},
			want: false,
		},
		{
			name: "full conditions met",
			r: &Resource{
				Operations: []*Operation{listOp, patchOp},
				IDField:    "id",
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := patchHasLookup(tt.r); got != tt.want {
				t.Errorf("patchHasLookup() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGeneratePatchCommand(t *testing.T) {
	dir := t.TempDir()
	resource := &Resource{
		Name:         "widgets",
		NameSingular: "widget",
		GoName:       "Widgets",
		IDField:      "id",
		NameField:    "name",
		Operations: []*Operation{
			{Name: "list", Method: "GET", Path: "/v1/widgets", IsList: true},
			{
				Name:   "patch",
				Method: "PATCH",
				Path:   "/v1/widgets/{id}",
				RequestBody: &RequestBody{
					Schema: &Schema{
						Properties: map[string]*Property{
							"name":  {Type: "string"},
							"color": {Type: "string"},
						},
					},
				},
			},
		},
		Schemas: map[string]*Schema{},
	}

	gen := NewGenerator(dir)
	outPath, err := gen.Generate(resource)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	code := string(content)

	// patch command function exists
	if !strings.Contains(code, "func newWidgetsPatchCmd(") {
		t.Error("expected newWidgetsPatchCmd function")
	}
	// uses MaximumNArgs(1) because patchHasLookup is true
	if !strings.Contains(code, "cobra.MaximumNArgs(1)") {
		t.Error("expected MaximumNArgs(1) for patch command with lookup")
	}
	// --name flag registered
	if !strings.Contains(code, `"name", "", "Look up widget by name"`) {
		t.Error("expected --name flag for patch command with lookup")
	}
	// merge-patch content type set
	if !strings.Contains(code, "application/merge-patch+json") {
		t.Error("expected merge-patch content type")
	}
	// --set flag registered
	if !strings.Contains(code, `"set", nil`) {
		t.Error("expected --set flag")
	}
	// --from-file flag registered
	if !strings.Contains(code, `"from-file"`) {
		t.Error("expected --from-file flag")
	}
	// no patch-by-name function generated
	if strings.Contains(code, "PatchByNameCmd") {
		t.Error("patch-by-name should not be generated as a separate command")
	}
}
