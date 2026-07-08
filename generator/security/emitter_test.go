// Copyright 2026, Jamf Software LLC

package security

import (
	"os"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/generator/parser"
)

func pageParam(name string) *parser.Parameter {
	return &parser.Parameter{Name: name, In: "query", Type: "string"}
}

func TestHasPageParams(t *testing.T) {
	cases := []struct {
		name   string
		params []*parser.Parameter
		want   bool
	}{
		{"both present", []*parser.Parameter{pageParam("page"), pageParam("pageSize")}, true},
		{"page only", []*parser.Parameter{pageParam("page")}, false},
		{"pageSize only", []*parser.Parameter{pageParam("pageSize")}, false},
		{"neither", []*parser.Parameter{pageParam("externalId")}, false},
		{"nil entries ignored", []*parser.Parameter{nil, pageParam("page"), pageParam("pageSize")}, true},
		{"non-query ignored", []*parser.Parameter{{Name: "page", In: "path"}, {Name: "pageSize", In: "path"}}, false},
	}
	for _, c := range cases {
		if got := hasPageParams(c.params); got != c.want {
			t.Errorf("%s: hasPageParams() = %v, want %v", c.name, got, c.want)
		}
	}
}

func objSchema(props map[string]string) *parser.Schema {
	s := &parser.Schema{Type: "object", Properties: map[string]*parser.Property{}}
	for name, typ := range props {
		s.Properties[name] = &parser.Property{Name: name, Type: typ}
	}
	return s
}

func TestDetectListArrayKey(t *testing.T) {
	singleArray := &parser.Operation{
		Responses: map[string]*parser.Response{
			"200": {Schema: objSchema(map[string]string{"devices": "array", "total": "integer"})},
		},
	}
	if got := detectListArrayKey(singleArray); got != "devices" {
		t.Errorf("detectListArrayKey() = %q, want %q", got, "devices")
	}

	twoArrays := &parser.Operation{
		Responses: map[string]*parser.Response{
			"200": {Schema: objSchema(map[string]string{"devices": "array", "extra": "array"})},
		},
	}
	if got := detectListArrayKey(twoArrays); got != "" {
		t.Errorf("detectListArrayKey() with two array props = %q, want empty", got)
	}

	noArrays := &parser.Operation{
		Responses: map[string]*parser.Response{
			"200": {Schema: objSchema(map[string]string{"total": "integer"})},
		},
	}
	if got := detectListArrayKey(noArrays); got != "" {
		t.Errorf("detectListArrayKey() with no array props = %q, want empty", got)
	}

	nonObject := &parser.Operation{
		Responses: map[string]*parser.Response{
			"200": {Schema: &parser.Schema{Type: "array"}},
		},
	}
	if got := detectListArrayKey(nonObject); got != "" {
		t.Errorf("detectListArrayKey() with non-object schema = %q, want empty", got)
	}

	// SSE-style ops document both 200 and 202 — either can carry the array.
	only202 := &parser.Operation{
		Responses: map[string]*parser.Response{
			"200": {Schema: objSchema(map[string]string{"status": "string"})},
			"202": {Schema: objSchema(map[string]string{"items": "array"})},
		},
	}
	if got := detectListArrayKey(only202); got != "items" {
		t.Errorf("detectListArrayKey() checking every 2xx = %q, want %q", got, "items")
	}

	noResponses := &parser.Operation{Responses: map[string]*parser.Response{"404": {}}}
	if got := detectListArrayKey(noResponses); got != "" {
		t.Errorf("detectListArrayKey() with only non-2xx = %q, want empty", got)
	}
}

func TestBuildQueryParams(t *testing.T) {
	params := []*parser.Parameter{
		nil,
		{Name: "page", In: "query", Type: "string"},      // excluded
		{Name: "pageSize", In: "query", Type: "string"},  // excluded
		{Name: "externalId", In: "path", Type: "string"}, // excluded, not query
		{Name: "guid", In: "query", Type: "string", Description: "Device GUID"},
		{Name: "includeArchived", In: "query", Type: "boolean"},
		{Name: "limit", In: "query", Type: "integer"},
		{Name: "tags", In: "query", Type: "array"},
	}

	got := buildQueryParams(params)
	if len(got) != 4 {
		t.Fatalf("buildQueryParams() len = %d, want 4, got %#v", len(got), got)
	}

	byName := map[string]queryParam{}
	for _, q := range got {
		byName[q.Name] = q
	}

	if q := byName["guid"]; q.GoType != "string" || q.Description != "Device GUID" || q.FlagName != "guid" {
		t.Errorf("guid param = %#v", q)
	}
	if q := byName["includeArchived"]; q.GoType != "bool" || q.FlagName != "include-archived" {
		t.Errorf("includeArchived param = %#v", q)
	}
	if q := byName["limit"]; q.GoType != "int" {
		t.Errorf("limit param = %#v", q)
	}
	if q := byName["tags"]; q.GoType != "[]string" {
		t.Errorf("tags param = %#v", q)
	}
	// Description defaults from the kebab-cased name when the spec has none.
	if q := byName["includeArchived"]; q.Description != "Filter by include-archived" {
		t.Errorf("includeArchived description = %q", q.Description)
	}
}

func TestBuildTemplateResource_PaginatedOpDetectsArrayKey(t *testing.T) {
	op := &parser.Operation{
		Name:       "list",
		Method:     "GET",
		Path:       "/risk/v2/devices",
		IsList:     true,
		Parameters: []*parser.Parameter{pageParam("page"), pageParam("pageSize")},
		Responses: map[string]*parser.Response{
			"200": {Schema: objSchema(map[string]string{"devices": "array"})},
		},
	}
	r := &parser.Resource{Name: "risk", GoName: "Risk", Operations: []*parser.Operation{op}}

	tr, err := buildTemplateResource(r, "Risk")
	if err != nil {
		t.Fatalf("buildTemplateResource() error = %v", err)
	}
	if len(tr.Operations) != 1 || !tr.Operations[0].Paginate {
		t.Fatalf("expected op to be Paginate, got %#v", tr.Operations)
	}
	if tr.Operations[0].UnwrapArrayKey != "devices" {
		t.Errorf("UnwrapArrayKey = %q, want %q", tr.Operations[0].UnwrapArrayKey, "devices")
	}
}

// TestBuildTemplateResource_AmbiguousArrayKeyErrors is the regression test
// for the silent pagination/flag-drop bug: an op shaped like a paginated
// list (has page+pageSize params, IsList) whose response schema can't be
// resolved to a single array property must fail generation loudly instead
// of silently downgrading to a plain, unfiltered GET.
func TestBuildTemplateResource_AmbiguousArrayKeyErrors(t *testing.T) {
	op := &parser.Operation{
		Name:       "list",
		Method:     "GET",
		Path:       "/risk/v2/devices",
		IsList:     true,
		Parameters: []*parser.Parameter{pageParam("page"), pageParam("pageSize"), {Name: "guid", In: "query", Type: "string"}},
		Responses: map[string]*parser.Response{
			// Two array properties -> detectListArrayKey can't pick one.
			"200": {Schema: objSchema(map[string]string{"devices": "array", "extra": "array"})},
		},
	}
	r := &parser.Resource{Name: "risk", GoName: "Risk", Operations: []*parser.Operation{op}}

	_, err := buildTemplateResource(r, "Risk")
	if err == nil {
		t.Fatal("buildTemplateResource() error = nil, want error for ambiguous array key")
	}
	if !strings.Contains(err.Error(), "risk") || !strings.Contains(err.Error(), "list") {
		t.Errorf("error %q should name the resource/op", err.Error())
	}
}

func TestGenerate_NonPaginatedOpRendersQueryFlags(t *testing.T) {
	// A hypothetical non-list GET with a filter query param must still get
	// its flag declared, registered, and applied to the request — even
	// though it's not a paginated op.
	op := &parser.Operation{
		Name:       "get",
		Method:     "GET",
		Path:       "/risk/v1/device",
		Parameters: []*parser.Parameter{{Name: "guid", In: "query", Type: "string", Description: "Device GUID"}},
		Responses:  map[string]*parser.Response{"200": {Schema: objSchema(map[string]string{"guid": "string"})}},
	}
	r := &parser.Resource{Name: "risk-device", GoName: "RiskDevice", Operations: []*parser.Operation{op}}

	dir := t.TempDir()
	files, err := Generate([]*parser.Resource{r}, map[string]string{"risk-device": "Risk"}, dir)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("Generate() produced %d files, want 1", len(files))
	}

	raw, readErr := os.ReadFile(files[0])
	if readErr != nil {
		t.Fatalf("reading generated file: %v", readErr)
	}
	src := string(raw)
	for _, want := range []string{`var guid string`, `"guid"`, `q.Set("guid", guid)`, `cmd.Flags().StringVar(&guid,`} {
		if !strings.Contains(src, want) {
			t.Errorf("generated file missing %q:\n%s", want, src)
		}
	}
}
