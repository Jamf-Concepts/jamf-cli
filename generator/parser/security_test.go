// Copyright 2026, Jamf Software LLC

package parser

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSecurityScopeForFile(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"jamf-risk-api.json", "Risk"},
		{"/some/dir/jamf-risk-api.json", "Risk"},
		{"jamf-device-lifecycle-api.json", "Lifecycle"},
		{"shared-signals-events-configuration-and-management-api.json", "SSE"},
		{"unknown-spec.json", ""},
	}
	for _, c := range cases {
		if got := SecurityScopeForFile(c.path); got != c.want {
			t.Errorf("SecurityScopeForFile(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestFilterParamByInAndName(t *testing.T) {
	params := []any{
		map[string]any{"in": "header", "name": "authorization"},
		map[string]any{"in": "header", "name": "Authorization"}, // case-insensitive match
		map[string]any{"in": "query", "name": "authorization"},  // different "in", kept
		map[string]any{"in": "path", "name": "customerId"},
		"not-a-map", // passthrough for malformed entries
	}

	out := filterParamByInAndName(params, "header", "authorization")
	if len(out) != 3 {
		t.Fatalf("filterParamByInAndName() len = %d, want 3, got %#v", len(out), out)
	}
	for _, p := range out {
		m, ok := p.(map[string]any)
		if !ok {
			continue // the passthrough string is expected to survive
		}
		if m["in"] == "header" && m["name"] == "authorization" {
			t.Errorf("filterParamByInAndName() left a matching header param: %#v", m)
		}
	}
}

func TestStripCustomerIDPathParam(t *testing.T) {
	doc := map[string]any{
		"paths": map[string]any{
			"/device-lifecycle/v1/{customerId}/devices/purge/async/external": map[string]any{
				"parameters": []any{
					map[string]any{"in": "path", "name": "customerId"},
				},
				"post": map[string]any{
					"parameters": []any{
						map[string]any{"in": "path", "name": "customerId"},
						map[string]any{"in": "header", "name": "authorization"},
					},
				},
			},
		},
	}

	stripCustomerIDPathParam(doc)

	paths, ok := doc["paths"].(map[string]any)
	if !ok {
		t.Fatal("doc[paths] is not a map after stripCustomerIDPathParam")
	}
	newPath := "/device-lifecycle/v1/devices/purge/async/external"
	item, ok := paths[newPath]
	if !ok {
		t.Fatalf("expected rewritten path %q, got paths = %#v", newPath, paths)
	}
	pi := item.(map[string]any)
	if params, ok := pi["parameters"].([]any); ok && len(params) != 0 {
		t.Errorf("path-level parameters not stripped: %#v", params)
	}
	post := pi["post"].(map[string]any)
	postParams := post["parameters"].([]any)
	if len(postParams) != 1 {
		t.Fatalf("post parameters = %#v, want only the authorization header left", postParams)
	}
	if m := postParams[0].(map[string]any); m["name"] != "authorization" {
		t.Errorf("remaining post parameter = %#v, want authorization header", m)
	}
}

// TestParseSecuritySpec_RealSpecs exercises ParseSecuritySpec against the
// committed specs/security/*.json files for every entry in securityOpsByFile.
func TestParseSecuritySpec_RealSpecs(t *testing.T) {
	for file, specs := range securityOpsByFile {
		t.Run(file, func(t *testing.T) {
			specPath := filepath.Join("..", "..", "specs", "security", file)
			if _, err := os.Stat(specPath); os.IsNotExist(err) {
				t.Skipf("%s not found, skipping", specPath)
			}

			resources, err := ParseSecuritySpec(specPath)
			if err != nil {
				t.Fatalf("ParseSecuritySpec(%s) error = %v", file, err)
			}

			gotOps := map[string]bool{}
			for _, r := range resources {
				for _, op := range r.Operations {
					gotOps[r.Name+"/"+op.Name] = true
					if op.Path == "" {
						t.Errorf("%s/%s: empty Path", r.Name, op.Name)
					}
				}
			}
			for _, spec := range specs {
				key := spec.resource + "/" + spec.opName
				if !gotOps[key] {
					t.Errorf("missing expected operation %q from %s", key, file)
				}
			}
		})
	}
}

func TestParseSecuritySpec_UnknownFileReturnsNil(t *testing.T) {
	resources, err := ParseSecuritySpec("not-a-security-spec.json")
	if err != nil {
		t.Fatalf("ParseSecuritySpec() error = %v, want nil", err)
	}
	if resources != nil {
		t.Errorf("ParseSecuritySpec() = %#v, want nil", resources)
	}
}

func TestParseSecuritySpec_DeviceLifecyclePreservesCustomerIDPlaceholder(t *testing.T) {
	specPath := filepath.Join("..", "..", "specs", "security", "jamf-device-lifecycle-api.json")
	if _, err := os.Stat(specPath); os.IsNotExist(err) {
		t.Skipf("%s not found, skipping", specPath)
	}

	resources, err := ParseSecuritySpec(specPath)
	if err != nil {
		t.Fatalf("ParseSecuritySpec() error = %v", err)
	}
	for _, r := range resources {
		for _, op := range r.Operations {
			if op.Name == "purge" && op.Path != "/device-lifecycle/v1/{customerId}/devices/purge/async/external" {
				t.Errorf("purge Path = %q, want the {customerId} placeholder preserved", op.Path)
			}
		}
	}
}
