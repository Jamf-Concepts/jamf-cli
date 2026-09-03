// Copyright 2026, Jamf Software LLC

package monolith

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// gatewaySource writes a miniature of the SDK's pro_api.json: one subtree with
// two families, a header parameter declared once and referenced everywhere, a
// query parameter that must survive, and the two privilege vocabularies.
func gatewaySource(t *testing.T) string {
	t.Helper()
	doc := map[string]any{
		"openapi": "3.0.1",
		"info":    map[string]any{"title": "Jamf Pro API", "version": "11.31.0"},
		"paths": map[string]any{
			"/v1/things": map[string]any{
				"get": map[string]any{
					"parameters": []any{
						map[string]any{"$ref": "#/components/parameters/X-Tenant-Id"},
						map[string]any{"$ref": "#/components/parameters/page"},
					},
					"security":                     []any{map[string]any{"oauth2": []any{"things:read"}}},
					"x-required-privileges":        []any{"things:read"},
					"x-required-privileges-legacy": []any{"Read Things"},
					"responses": map[string]any{"200": map[string]any{"content": map[string]any{
						"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/ThingList"}},
					}}},
				},
			},
			"/v1/things/{id}/reheat": map[string]any{
				"post": map[string]any{
					"parameters": []any{
						map[string]any{"in": "header", "name": "X-Inline", "schema": map[string]any{"type": "string"}},
						map[string]any{"in": "path", "name": "id", "required": true, "schema": map[string]any{"type": "string"}},
					},
					"x-required-privileges":        []any{"things:update"},
					"x-required-privileges-legacy": []any{"Update Things"},
					"responses":                    map[string]any{"204": map[string]any{"description": "done"}},
				},
			},
			"/v1/things/settings": map[string]any{
				"get": map[string]any{
					// No legacy list: the warning case.
					"x-required-privileges": []any{"things:read"},
					"responses":             map[string]any{"200": map[string]any{"description": "ok"}},
				},
			},
			"/v1/elsewhere": map[string]any{
				"get": map[string]any{"responses": map[string]any{"200": map[string]any{"description": "ok"}}},
			},
		},
		"components": map[string]any{
			"parameters": map[string]any{
				"X-Tenant-Id": map[string]any{"in": "header", "name": "X-Tenant-Id", "schema": map[string]any{"type": "string"}},
				"page":        map[string]any{"in": "query", "name": "page", "schema": map[string]any{"type": "integer"}},
			},
			"schemas": map[string]any{
				"ThingList": map[string]any{"type": "object", "properties": map[string]any{
					"results": map[string]any{"type": "array", "items": map[string]any{"$ref": "#/components/schemas/Thing"}},
				}},
				"Thing":    map[string]any{"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string"}}},
				"Unreachd": map[string]any{"type": "object"},
			},
		},
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "pro_api.json")
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func routes() []SubtreeSpec {
	return []SubtreeSpec{
		{Prefix: "/v1/things", Filename: "Thing.yaml", Title: "Jamf Pro API - Things", Description: "Things."},
		{Prefix: "/v1/things/settings", Filename: "ThingSettings.yaml", Title: "Jamf Pro API - Thing Settings", Description: "Settings."},
	}
}

func readSpec(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := yaml.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return normalizeKeys(out).(map[string]any)
}

func op(t *testing.T, spec map[string]any, path, method string) map[string]any {
	t.Helper()
	paths, _ := asMap(spec["paths"])
	item, ok := asMap(paths[path])
	if !ok {
		t.Fatalf("%s absent from the spec", path)
	}
	o, ok := asMap(item[method])
	if !ok {
		t.Fatalf("%s %s absent from the spec", method, path)
	}
	return o
}

// The longest matching prefix owns a path, so a declared parent does not
// swallow a child family — and a path outside the subtree is not touched at all.
func TestExtractSubtreeRoutesByLongestPrefix(t *testing.T) {
	dir := t.TempDir()
	written, _, err := ExtractSubtree(gatewaySource(t), dir, "/v1/things", routes())
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 2 {
		t.Fatalf("wrote %v, want two files", written)
	}
	thing := readSpec(t, filepath.Join(dir, "Thing.yaml"))
	settings := readSpec(t, filepath.Join(dir, "ThingSettings.yaml"))

	thingPaths, _ := asMap(thing["paths"])
	if _, ok := thingPaths["/v1/things/settings"]; ok {
		t.Error("the parent route swallowed the settings family")
	}
	if _, ok := thingPaths["/v1/elsewhere"]; ok {
		t.Error("a path outside the subtree was extracted")
	}
	for _, want := range []string{"/v1/things", "/v1/things/{id}/reheat"} {
		if _, ok := thingPaths[want]; !ok {
			t.Errorf("%s missing from Thing.yaml", want)
		}
	}
	settingsPaths, _ := asMap(settings["paths"])
	if len(settingsPaths) != 1 {
		t.Errorf("ThingSettings.yaml has %d paths, want 1", len(settingsPaths))
	}
}

// The gateway declares X-Tenant-Id on every operation. It is the scope this
// CLI's own client stamps on a request, never a flag a user supplies, and the
// parser turns any declared parameter into one — so both the $ref'd and the
// inline form have to go, and the query parameter beside them has to stay.
func TestExtractSubtreeDropsHeaderParams(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := ExtractSubtree(gatewaySource(t), dir, "/v1/things", routes()); err != nil {
		t.Fatal(err)
	}
	spec := readSpec(t, filepath.Join(dir, "Thing.yaml"))

	list := op(t, spec, "/v1/things", "get")
	params, _ := list["parameters"].([]any)
	if len(params) != 1 {
		t.Fatalf("list parameters = %v, want only the query one", params)
	}
	if ref, _ := asMap(params[0]); ref["$ref"] != "#/components/parameters/page" {
		t.Errorf("kept %v, want the page parameter", params[0])
	}
	reheat := op(t, spec, "/v1/things/{id}/reheat", "post")
	for _, p := range reheat["parameters"].([]any) {
		if pm, _ := asMap(p); pm["in"] == "header" {
			t.Errorf("an inline header parameter survived: %v", pm)
		}
	}
	// Dropping the ref must also keep the header out of the inlined components,
	// or the parser sees it declared and the file carries a dead definition.
	comps, _ := asMap(spec["components"])
	declared, _ := asMap(comps["parameters"])
	if _, ok := declared["X-Tenant-Id"]; ok {
		t.Error("X-Tenant-Id was inlined into components")
	}
	if _, ok := declared["page"]; !ok {
		t.Error("the page parameter was not inlined, so its $ref cannot resolve")
	}
}

// A Pro command's jamf:privileges annotation speaks Jamf Pro API-role prose;
// the gateway spec's x-required-privileges is the GA capability vocabulary,
// which reaches the same command through specs/gateway/coverage.json instead.
// Publishing the slug as the Pro privilege would name a grant that does not
// exist in Jamf Pro's own picker.
func TestExtractSubtreePromotesTheLegacyPrivileges(t *testing.T) {
	dir := t.TempDir()
	_, warnings, err := ExtractSubtree(gatewaySource(t), dir, "/v1/things", routes())
	if err != nil {
		t.Fatal(err)
	}
	spec := readSpec(t, filepath.Join(dir, "Thing.yaml"))
	list := op(t, spec, "/v1/things", "get")
	privs, _ := list["x-required-privileges"].([]any)
	if len(privs) != 1 || privs[0] != "Read Things" {
		t.Errorf("x-required-privileges = %v, want [Read Things]", privs)
	}
	if _, ok := list["x-required-privileges-legacy"]; ok {
		t.Error("the legacy key was left behind for the parser to ignore")
	}
	if _, ok := list["security"]; ok {
		t.Error("the gateway's oauth2 scopes were carried into a Pro spec")
	}

	// An operation with no legacy list is reported, not silently published with
	// the capability slug in the Jamf Pro field.
	settings := readSpec(t, filepath.Join(dir, "ThingSettings.yaml"))
	if _, ok := op(t, settings, "/v1/things/settings", "get")["x-required-privileges"]; ok {
		t.Error("a capability slug was published as a Jamf Pro privilege")
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "/v1/things/settings") {
		t.Errorf("warnings = %v, want one naming the operation with no legacy privileges", warnings)
	}
}

// jss carries x-action and the gateway publication drops it. Without it, POST
// .../reheat infers "create" and collides with the collection create, and a
// GET on an {id} sub-path collides with the canonical get.
func TestExtractSubtreeStampsXActionOnSubPathOperations(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := ExtractSubtree(gatewaySource(t), dir, "/v1/things", routes()); err != nil {
		t.Fatal(err)
	}
	spec := readSpec(t, filepath.Join(dir, "Thing.yaml"))
	if got := op(t, spec, "/v1/things/{id}/reheat", "post")["x-action"]; got != true {
		t.Errorf("sub-path POST x-action = %v, want true", got)
	}
	// The collection root is not an action: it is the list and the create.
	if _, ok := op(t, spec, "/v1/things", "get")["x-action"]; ok {
		t.Error("the collection root was stamped as an action")
	}
	// Nor is a bare {id} path — that is get/update/delete.
	if isActionPath("/v1/things/{id}", "/v1/things") {
		t.Error("a bare {id} path was classified as an action")
	}
	// A family's own root is not an action either, even though it is deeper
	// than the subtree — /v1/things/settings owns itself.
	if _, ok := op(t, readSpec(t, filepath.Join(dir, "ThingSettings.yaml")), "/v1/things/settings", "get")["x-action"]; ok {
		t.Error("a family root was stamped as an action")
	}
}

// A new family under the subtree is a spec file whose name is a judgement call.
// Dropping it would lose the endpoints with nothing to notice, which is how the
// reverse-engineered specs went stale in the first place.
func TestExtractSubtreeRefusesAnUnroutedPath(t *testing.T) {
	only := []SubtreeSpec{{Prefix: "/v1/things/settings", Filename: "ThingSettings.yaml", Title: "t", Description: "d"}}
	_, _, err := ExtractSubtree(gatewaySource(t), t.TempDir(), "/v1/things", only)
	if err == nil {
		t.Fatal("an unrouted path was accepted")
	}
	if !strings.Contains(err.Error(), "/v1/things") {
		t.Errorf("error does not name the unrouted path: %v", err)
	}
}

// Each file is self-contained: its own transitive closure inlined, nothing
// shared, and nothing it does not reference.
func TestExtractSubtreeInlinesOnlyTheClosure(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := ExtractSubtree(gatewaySource(t), dir, "/v1/things", routes()); err != nil {
		t.Fatal(err)
	}
	spec := readSpec(t, filepath.Join(dir, "Thing.yaml"))
	comps, _ := asMap(spec["components"])
	schemas, _ := asMap(comps["schemas"])
	for _, want := range []string{"ThingList", "Thing"} {
		if _, ok := schemas[want]; !ok {
			t.Errorf("%s not inlined, so its $ref cannot resolve", want)
		}
	}
	if _, ok := schemas["Unreachd"]; ok {
		t.Error("a schema nothing references was inlined")
	}
	if _, err := os.Stat(filepath.Join(dir, LibraryFilename)); err == nil {
		t.Error("a shared library was written; these files must be self-contained")
	}
}
