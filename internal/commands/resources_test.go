package commands

import (
	"testing"
)

func TestBackupResources_NotEmpty(t *testing.T) {
	if len(BackupResources) == 0 {
		t.Fatal("BackupResources should not be empty")
	}
}

func TestBackupResources_AllHavePaths(t *testing.T) {
	for _, r := range BackupResources {
		if r.ListPath == "" {
			t.Errorf("resource %q has empty ListPath", r.Name)
		}
		if r.GetPath == "" {
			t.Errorf("resource %q has empty GetPath", r.Name)
		}
		if r.SubDir == "" {
			t.Errorf("resource %q has empty SubDir", r.Name)
		}
	}
}

func TestBackupResources_ClassicHaveWrapperKey(t *testing.T) {
	for _, r := range BackupResources {
		if r.IsClassic && r.WrapperKey == "" {
			t.Errorf("classic resource %q (%s) missing WrapperKey", r.Name, r.ListPath)
		}
	}
}

func TestFilterResources(t *testing.T) {
	filtered := FilterResources(BackupResources, []string{"policies", "scripts"})

	for _, r := range filtered {
		if r.Name != "policies" && r.Name != "scripts" {
			t.Errorf("unexpected resource %q in filtered results", r.Name)
		}
	}

	if len(filtered) == 0 {
		t.Error("expected at least one result for policies+scripts filter")
	}
}

func TestFilterResources_Empty(t *testing.T) {
	// Empty filter returns all resources
	filtered := FilterResources(BackupResources, nil)
	if len(filtered) != len(BackupResources) {
		t.Errorf("empty filter should return all %d resources, got %d", len(BackupResources), len(filtered))
	}
}

func TestFilterResources_NoMatch(t *testing.T) {
	filtered := FilterResources(BackupResources, []string{"nonexistent"})
	if len(filtered) != 0 {
		t.Errorf("expected 0 results for nonexistent filter, got %d", len(filtered))
	}
}
