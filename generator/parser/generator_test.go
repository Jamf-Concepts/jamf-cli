package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	names := make([]string, len(got))
	for i, op := range got {
		names[i] = op.Name
	}
	// Should keep first occurrence
	want := []string{"list", "get", "create"}
	for i, n := range want {
		if names[i] != n {
			t.Errorf("deduped[%d] = %q, want %q", i, names[i], n)
		}
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
		{"has query param (non-bool)", []*Operation{{Parameters: []*Parameter{{In: "query", Type: "string"}}}}, true},
		{"only bool query param", []*Operation{{Parameters: []*Parameter{{In: "query", Type: "boolean"}}}}, false},
		{"no special needs", []*Operation{{Method: "GET"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := needsFmt(tt.ops); got != tt.want {
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
		"Code generated by jamfpro-cli generator. DO NOT EDIT.",
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
