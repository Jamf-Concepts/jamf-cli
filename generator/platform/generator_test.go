// Copyright 2026, Jamf Software LLC

package platform

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadResources_LiveSpecs runs the orchestrator against the committed
// specs/platform/ tree and asserts the merged resource set is well-formed.
// This catches regressions in spec ingest, tenant stripping, service
// prepending, tag grouping, and collision-renaming.
func TestLoadResources_LiveSpecs(t *testing.T) {
	specsDir, err := filepath.Abs("../../specs/platform")
	if err != nil {
		t.Fatalf("resolving specs dir: %v", err)
	}

	resources, files, err := LoadResources(specsDir)
	if err != nil {
		t.Fatalf("LoadResources: %v", err)
	}
	if len(resources) == 0 {
		t.Fatal("expected resources, got 0 — is specs/platform/ populated?")
	}
	if len(files) == 0 {
		t.Fatal("LoadResources returned no consumed spec files")
	}
	for i := 1; i < len(files); i++ {
		if files[i-1] >= files[i] {
			t.Errorf("consumed files not sorted: %q >= %q", files[i-1], files[i])
		}
	}

	seenNames := make(map[string]bool, len(resources))
	for _, r := range resources {
		if r.Name == "" {
			t.Errorf("resource with empty Name (GoName=%s)", r.GoName)
		}
		if r.GoName == "" {
			t.Errorf("resource %q missing GoName", r.Name)
		}
		if len(r.Operations) == 0 {
			t.Errorf("resource %q has no operations", r.Name)
		}
		if seenNames[r.Name] {
			t.Errorf("duplicate resource name %q", r.Name)
		}
		seenNames[r.Name] = true

		for _, op := range r.Operations {
			if op.Name == "" {
				t.Errorf("%s: op with empty name (method=%s path=%s)", r.Name, op.Method, op.Path)
			}
			if op.Method == "" {
				t.Errorf("%s/%s: op with empty method", r.Name, op.Name)
			}
			// Every emitted path must include /tenant/{tenantId}/ once we
			// re-add it during template build. At parse time the prefix is
			// stripped, so the path should NOT contain "/tenant/".
			if strings.Contains(op.Path, "/tenant/{tenantId}") {
				t.Errorf("%s/%s: parser-stage path still contains tenant placeholder: %s", r.Name, op.Name, op.Path)
			}
			// Service prefix should be prepended (every path starts /api/).
			if !strings.HasPrefix(op.Path, "/api/") {
				t.Errorf("%s/%s: path missing /api/ prefix: %s", r.Name, op.Name, op.Path)
			}
		}
	}

	// Check the well-known collision rename: platform spec tag "users" must
	// map to "platform-users" so it doesn't collide with Pro's users.
	if seenNames["users"] {
		t.Errorf(`tag "users" must be renamed to "platform-users"`)
	}
	// And the renamed form must be present (assuming the spec carries that tag).
	// device-inventory-api.json defines a /users/{id}/devices endpoint with
	// tag "users", so the merged set should always include platform-users.
	if !seenNames["platform-users"] {
		t.Logf("platform-users not present — may indicate spec change, not a hard failure")
	}
}

// TestRestoreTenantSegment exercises the runtime path-template restoration.
func TestRestoreTenantSegment(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"/api/blueprints/v1/blueprints", "/api/blueprints/v1/tenant/{tenantId}/blueprints"},
		{"/api/blueprints/v1/blueprints/{id}", "/api/blueprints/v1/tenant/{tenantId}/blueprints/{id}"},
		{"/api/devices/v2/devices/{id}/applications", "/api/devices/v2/tenant/{tenantId}/devices/{id}/applications"},
		{"/no/version/here", "/no/version/here"},
	}
	for _, c := range cases {
		if got := restoreTenantSegment(c.in); got != c.want {
			t.Errorf("restoreTenantSegment(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestExtractPathParams covers placeholder extraction order.
func TestExtractPathParams(t *testing.T) {
	cases := []struct {
		path string
		want []string
	}{
		{"/v1/foo", nil},
		{"/v1/foo/{id}", []string{"id"}},
		{"/v1/foo/{id}/bar/{ruleId}", []string{"id", "ruleId"}},
		{"/api/x/v1/tenant/{tenantId}/y/{id}", []string{"tenantId", "id"}},
	}
	for _, c := range cases {
		got := extractPathParams(c.path)
		if len(got) != len(c.want) {
			t.Errorf("extractPathParams(%q) len = %d, want %d", c.path, len(got), len(c.want))
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("extractPathParams(%q)[%d] = %q, want %q", c.path, i, got[i], c.want[i])
			}
		}
	}
}
