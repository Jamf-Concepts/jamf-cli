// Copyright 2026, Jamf Software LLC

package classic

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseManifest_ValidFile(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "resources.yaml")
	data := []byte(`resources:
  - name: policies
    path: policies
    description: Deployment policies
    singular: policy
    operations: [list, get, create, update, delete]
    lookups: [id, name]
  - name: packages
    path: packages
    description: Software packages
    singular: package
    operations: [list, get]
    lookups: [id]
`)
	if err := os.WriteFile(manifest, data, 0o644); err != nil {
		t.Fatal(err)
	}

	resources, err := ParseManifest(manifest)
	if err != nil {
		t.Fatalf("ParseManifest() error = %v", err)
	}

	if len(resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(resources))
	}

	// Resources are sorted by CLIName
	pkg := resources[0]
	pol := resources[1]

	if pkg.CLIName != "classic-packages" {
		t.Errorf("first resource CLIName = %q, want %q", pkg.CLIName, "classic-packages")
	}
	if pol.CLIName != "classic-policies" {
		t.Errorf("second resource CLIName = %q, want %q", pol.CLIName, "classic-policies")
	}
}

func TestParseManifest_DefaultCLIName(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "resources.yaml")
	data := []byte(`resources:
  - name: policies
    path: policies
    description: Deployment policies
    singular: policy
    operations: [list, get]
    lookups: [id]
`)
	if err := os.WriteFile(manifest, data, 0o644); err != nil {
		t.Fatal(err)
	}

	resources, err := ParseManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}

	if resources[0].CLIName != "classic-policies" {
		t.Errorf("CLIName = %q, want %q", resources[0].CLIName, "classic-policies")
	}
	if resources[0].GoName != "ClassicPolicies" {
		t.Errorf("GoName = %q, want %q", resources[0].GoName, "ClassicPolicies")
	}
}

func TestParseManifest_CustomCLIName(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "resources.yaml")
	data := []byte(`resources:
  - name: osxconfigurationprofiles
    path: osxconfigurationprofiles
    cli_name: classic-macos-config-profiles
    description: macOS configuration profiles
    singular: os_x_configuration_profile
    operations: [list, get]
    lookups: [id, name]
`)
	if err := os.WriteFile(manifest, data, 0o644); err != nil {
		t.Fatal(err)
	}

	resources, err := ParseManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}

	r := resources[0]
	if r.CLIName != "classic-macos-config-profiles" {
		t.Errorf("CLIName = %q, want %q", r.CLIName, "classic-macos-config-profiles")
	}
	if r.GoName != "ClassicMacosConfigProfiles" {
		t.Errorf("GoName = %q, want %q", r.GoName, "ClassicMacosConfigProfiles")
	}
	if r.Singular != "os_x_configuration_profile" {
		t.Errorf("Singular = %q, want %q", r.Singular, "os_x_configuration_profile")
	}
}

func TestParseManifest_DefaultSingular(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "resources.yaml")
	data := []byte(`resources:
  - name: packages
    path: packages
    description: Software packages
    operations: [list]
    lookups: [id]
`)
	if err := os.WriteFile(manifest, data, 0o644); err != nil {
		t.Fatal(err)
	}

	resources, err := ParseManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}

	if resources[0].Singular != "package" {
		t.Errorf("Singular = %q, want %q", resources[0].Singular, "package")
	}
}

func TestParseManifest_MissingName(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "resources.yaml")
	data := []byte(`resources:
  - path: policies
    operations: [list]
    lookups: [id]
`)
	if err := os.WriteFile(manifest, data, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := ParseManifest(manifest)
	if err == nil {
		t.Fatal("expected error for missing name, got nil")
	}
}

func TestParseManifest_MissingPath(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "resources.yaml")
	data := []byte(`resources:
  - name: policies
    operations: [list]
    lookups: [id]
`)
	if err := os.WriteFile(manifest, data, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := ParseManifest(manifest)
	if err == nil {
		t.Fatal("expected error for missing path, got nil")
	}
}

func TestParseManifest_DefaultOperationsAndLookups(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "resources.yaml")
	data := []byte(`resources:
  - name: policies
    path: policies
    singular: policy
    description: Deployment policies
`)
	if err := os.WriteFile(manifest, data, 0o644); err != nil {
		t.Fatal(err)
	}

	resources, err := ParseManifest(manifest)
	if err != nil {
		t.Fatalf("ParseManifest() error = %v", err)
	}

	r := resources[0]
	wantOps := []string{"list", "get", "create", "update", "delete"}
	if len(r.Operations) != len(wantOps) {
		t.Fatalf("Operations = %v, want %v", r.Operations, wantOps)
	}
	for i, op := range r.Operations {
		if op != wantOps[i] {
			t.Errorf("Operations[%d] = %q, want %q", i, op, wantOps[i])
		}
	}

	wantLookups := []string{"id", "name"}
	if len(r.Lookups) != len(wantLookups) {
		t.Fatalf("Lookups = %v, want %v", r.Lookups, wantLookups)
	}
	for i, l := range r.Lookups {
		if l != wantLookups[i] {
			t.Errorf("Lookups[%d] = %q, want %q", i, l, wantLookups[i])
		}
	}
}

func TestParseManifest_FileNotFound(t *testing.T) {
	_, err := ParseManifest("/nonexistent/path")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestParseManifest_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "resources.yaml")
	if err := os.WriteFile(manifest, []byte("{{invalid yaml}}"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := ParseManifest(manifest)
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

func TestClassicResource_HasOperation(t *testing.T) {
	r := ClassicResource{Operations: []string{"list", "get", "delete"}}

	if !r.HasOperation("list") {
		t.Error("expected HasOperation(list) = true")
	}
	if r.HasOperation("create") {
		t.Error("expected HasOperation(create) = false")
	}
}

func TestClassicResource_HasLookup(t *testing.T) {
	r := ClassicResource{Lookups: []string{"id", "name"}}

	if !r.HasLookup("name") {
		t.Error("expected HasLookup(name) = true")
	}
	if r.HasLookup("serialnumber") {
		t.Error("expected HasLookup(serialnumber) = false")
	}
}

func TestClassicResource_ExtraLookups(t *testing.T) {
	r := ClassicResource{Lookups: []string{"id", "name", "serialnumber"}}

	extra := r.ExtraLookups()
	if len(extra) != 2 {
		t.Fatalf("expected 2 extra lookups, got %d", len(extra))
	}
	if extra[0] != "name" || extra[1] != "serialnumber" {
		t.Errorf("extra lookups = %v, want [name serialnumber]", extra)
	}
}
