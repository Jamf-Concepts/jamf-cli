// Copyright 2026, Jamf Software LLC

package commands

import (
	"slices"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/commands/pro/generated"
)

func TestBackupResources_NotEmpty(t *testing.T) {
	if len(BackupResources) == 0 {
		t.Fatal("BackupResources should not be empty")
	}
}

// TestBackupResources_AllKeysRegistered is the drift-catching test: every
// curated entry must point at an endpoint that exists in the generated
// registry. A spec rename or deletion surfaces here after `make generate`
// rather than as a silent runtime failure.
func TestBackupResources_AllKeysRegistered(t *testing.T) {
	for _, r := range BackupResources {
		if r.Key == "" {
			t.Errorf("BackupResource with FilterName=%q has empty Key", r.FilterName)
			continue
		}
		ep, ok := generated.BackupEndpoints[r.Key]
		if !ok {
			t.Errorf("BackupResource %q (filter=%q) references unknown endpoint key", r.Key, r.FilterName)
			continue
		}
		if ep.ListPath == "" {
			t.Errorf("endpoint %q has empty ListPath", r.Key)
		}
		if ep.GetPath == "" && !r.ListOnly {
			t.Errorf("endpoint %q has empty GetPath but is not marked ListOnly", r.Key)
		}
		if ep.IsClassic && ep.WrapperKey == "" && ep.ListSubset == "" {
			t.Errorf("classic endpoint %q missing both WrapperKey and ListSubset", r.Key)
		}
		if r.SubDir == "" {
			t.Errorf("BackupResource %q has empty SubDir", r.Key)
		}
	}
}

func TestBackupResources_SubDirsUnique(t *testing.T) {
	seen := make(map[string]string, len(BackupResources))
	for _, r := range BackupResources {
		if prev, ok := seen[r.SubDir]; ok {
			t.Errorf("duplicate SubDir %q: %q and %q", r.SubDir, prev, r.Key)
		}
		seen[r.SubDir] = r.Key
	}
}

func TestBackupResources_PreferModernOverClassic(t *testing.T) {
	// Resources with modern equivalents that should NOT appear as classic in
	// the curated list. Keep this list in sync with the generator's modern
	// coverage — it documents the "prefer modern" policy in a checkable way.
	forbidden := map[string]string{
		"classic-computer-ext-attrs": "computer-extension-attributes",
		"classic-mobile-ext-attrs":   "mobile-device-extension-attributes",
	}
	for _, r := range BackupResources {
		if modern, ok := forbidden[r.Key]; ok {
			t.Errorf("BackupResource %q shadows modern %q — prefer modern API", r.Key, modern)
		}
	}
}

func TestResolveBackupResources_NoFilter(t *testing.T) {
	got, err := ResolveBackupResources(nil)
	if err != nil {
		t.Fatalf("ResolveBackupResources: %v", err)
	}
	if len(got) != len(BackupResources) {
		t.Errorf("expected %d resolved entries, got %d", len(BackupResources), len(got))
	}
}

func TestResolveBackupResources_WithFilter(t *testing.T) {
	got, err := ResolveBackupResources([]string{"policies", "scripts"})
	if err != nil {
		t.Fatalf("ResolveBackupResources: %v", err)
	}

	for _, r := range got {
		if r.FilterName != "policies" && r.FilterName != "scripts" {
			t.Errorf("unexpected FilterName %q in filtered results", r.FilterName)
		}
	}

	if len(got) == 0 {
		t.Error("expected at least one result for policies+scripts filter")
	}
}

func TestResolveBackupResources_NoMatch(t *testing.T) {
	got, err := ResolveBackupResources([]string{"nonexistent"})
	if err != nil {
		t.Fatalf("ResolveBackupResources: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 results for nonexistent filter, got %d", len(got))
	}
}

func TestResolveBackupResources_AccountsSplit(t *testing.T) {
	// The accounts filter must resolve to both the users + groups subsets so
	// backup doesn't silently drop half the admin config.
	got, err := ResolveBackupResources([]string{"accounts"})
	if err != nil {
		t.Fatalf("ResolveBackupResources: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 resolved entries for accounts filter, got %d", len(got))
	}
	subdirs := []string{got[0].SubDir, got[1].SubDir}
	slices.Sort(subdirs)
	want := []string{"accounts/groups", "accounts/users"}
	if !slices.Equal(subdirs, want) {
		t.Errorf("accounts subdirs = %v, want %v", subdirs, want)
	}
	for _, r := range got {
		if r.ListSubset == "" {
			t.Errorf("accounts entry %q missing ListSubset", r.Key)
		}
	}
}

func TestBackupFilterNames(t *testing.T) {
	names := BackupFilterNames()
	if len(names) == 0 {
		t.Fatal("BackupFilterNames should not be empty")
	}
	// Must contain the canonical user-facing filter tokens.
	required := []string{"policies", "profiles", "scripts", "extension-attributes", "accounts"}
	for _, r := range required {
		if !slices.Contains(names, r) {
			t.Errorf("BackupFilterNames missing %q (got %v)", r, names)
		}
	}
}
