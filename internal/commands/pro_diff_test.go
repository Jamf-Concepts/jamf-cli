// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// --- isDirectoryPath ---

func TestIsDirectoryPath(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"/absolute/path", true},
		{"./relative/path", true},
		{"~/home/path", true},
		{".", true},
		{"~", true},
		{"production", false},
		{"staging", false},
		{"my-profile", false},
		{"profile123", false},
		{"", false},
		// Paths with only a single character that isn't special
		{"a", false},
		// Ensure sub-paths don't accidentally match
		{"notapath/foo", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isDirectoryPath(tt.input)
			if got != tt.want {
				t.Errorf("isDirectoryPath(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// --- loadSnapshotFromDirectory ---

func writeBackupFileForTest(t *testing.T, path string, data any, format string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("creating dirs: %v", err)
	}
	if err := writeBackupFile(path, data, format); err != nil {
		t.Fatalf("writing backup file %s: %v", path, err)
	}
}

func TestLoadSnapshotFromDirectory_Basic(t *testing.T) {
	dir := t.TempDir()

	// Write two policies as YAML backup files.
	writeBackupFileForTest(t, filepath.Join(dir, "policies", "deploy-chrome.yaml"), map[string]any{
		"name":    "Deploy Chrome",
		"enabled": true,
		"_meta":   map[string]any{"schema_version": 1},
	}, "yaml")

	writeBackupFileForTest(t, filepath.Join(dir, "policies", "install-rosetta.yaml"), map[string]any{
		"name":    "Install Rosetta",
		"enabled": false,
		"_meta":   map[string]any{"schema_version": 1},
	}, "yaml")

	snapshot, err := loadSnapshotFromDirectory(dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	policies, ok := snapshot["policies"]
	if !ok {
		t.Fatal("expected 'policies' resource bucket in snapshot")
		return
	}
	if len(policies) != 2 {
		t.Errorf("expected 2 policies, got %d", len(policies))
	}

	chrome, ok := policies["Deploy Chrome"]
	if !ok {
		t.Fatal("expected 'Deploy Chrome' object")
		return
	}
	// _meta should be stripped
	if _, hasMeta := chrome["_meta"]; hasMeta {
		t.Error("_meta should be stripped from loaded objects")
	}
	if chrome["enabled"] != true {
		t.Errorf("expected enabled=true, got %v", chrome["enabled"])
	}
}

func TestLoadSnapshotFromDirectory_JSONFormat(t *testing.T) {
	dir := t.TempDir()

	writeBackupFileForTest(t, filepath.Join(dir, "scripts", "run-updates.json"), map[string]any{
		"name":       "Run Updates",
		"scriptBody": "#!/bin/bash\n...",
	}, "json")

	snapshot, err := loadSnapshotFromDirectory(dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	scripts, ok := snapshot["scripts"]
	if !ok {
		t.Fatal("expected 'scripts' in snapshot")
		return
	}
	if _, ok := scripts["Run Updates"]; !ok {
		t.Error("expected 'Run Updates' object")
	}
}

func TestLoadSnapshotFromDirectory_ResourceFilter(t *testing.T) {
	dir := t.TempDir()

	writeBackupFileForTest(t, filepath.Join(dir, "policies", "test.yaml"), map[string]any{"name": "Test"}, "yaml")
	writeBackupFileForTest(t, filepath.Join(dir, "scripts", "my-script.yaml"), map[string]any{"name": "My Script"}, "yaml")

	snapshot, err := loadSnapshotFromDirectory(dir, []string{"policies"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := snapshot["policies"]; !ok {
		t.Error("expected policies in snapshot")
	}
	if _, ok := snapshot["scripts"]; ok {
		t.Error("scripts should not be included when filter is 'policies'")
	}
}

func TestLoadSnapshotFromDirectory_FailuresWarning(t *testing.T) {
	dir := t.TempDir()

	// Write a _failures.yaml to trigger the warning.
	if err := os.WriteFile(filepath.Join(dir, "_failures.yaml"), []byte("- resource: policies\n  error: timeout\n"), 0o644); err != nil {
		t.Fatalf("writing failures file: %v", err)
	}

	// Capture stderr.
	origStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	_, err := loadSnapshotFromDirectory(dir, nil)

	_ = w.Close()
	os.Stderr = origStderr

	var buf strings.Builder
	buf.Grow(256)
	tmp := make([]byte, 256)
	for {
		n, _ := r.Read(tmp)
		if n == 0 {
			break
		}
		buf.Write(tmp[:n])
	}

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(buf.String(), "_failures.yaml") {
		t.Error("expected WARNING about _failures.yaml in stderr")
	}
}

func TestLoadSnapshotFromDirectory_NonExistent(t *testing.T) {
	_, err := loadSnapshotFromDirectory("/nonexistent/path/xyz123", nil)
	if err == nil {
		t.Error("expected error for nonexistent directory")
	}
}

func TestLoadSnapshotFromDirectory_NotADirectory(t *testing.T) {
	f, err := os.CreateTemp("", "not-a-dir-*.txt")
	if err != nil {
		t.Fatal(err)
		return
	}
	_ = f.Close()
	defer func() { _ = os.Remove(f.Name()) }()

	_, err = loadSnapshotFromDirectory(f.Name(), nil)
	if err == nil {
		t.Errorf("expected error for non-directory path %s", f.Name())
	}
}

func TestLoadSnapshotFromDirectory_FallbackNameFromFilestem(t *testing.T) {
	dir := t.TempDir()

	// Object without a "name" field — should fall back to filename stem.
	writeBackupFileForTest(t, filepath.Join(dir, "categories", "productivity.yaml"), map[string]any{
		"priority": float64(5),
	}, "yaml")

	snapshot, err := loadSnapshotFromDirectory(dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cats := snapshot["categories"]
	if _, ok := cats["productivity"]; !ok {
		t.Error("expected fallback name 'productivity' from filename stem")
	}
}

// --- compareSnapshots ---

func makeSnapshot(resource, name string, fields map[string]any) resourceSnapshot {
	return resourceSnapshot{
		resource: {name: fields},
	}
}

func TestCompareSnapshots_Added(t *testing.T) {
	src := resourceSnapshot{}
	tgt := makeSnapshot("policies", "New Policy", map[string]any{"enabled": true})

	results := compareSnapshots(src, tgt)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Change != diffAdded {
		t.Errorf("expected change=added, got %q", r.Change)
	}
	if r.Name != "New Policy" {
		t.Errorf("expected name='New Policy', got %q", r.Name)
	}
	if r.Resource != "policies" {
		t.Errorf("expected resource='policies', got %q", r.Resource)
	}
}

func TestCompareSnapshots_Removed(t *testing.T) {
	src := makeSnapshot("scripts", "Old Script", map[string]any{"scriptBody": "echo hi"})
	tgt := resourceSnapshot{}

	results := compareSnapshots(src, tgt)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Change != diffRemoved {
		t.Errorf("expected change=removed, got %q", results[0].Change)
	}
}

func TestCompareSnapshots_Identical(t *testing.T) {
	obj := map[string]any{"enabled": true, "name": "Test Policy"}
	src := makeSnapshot("policies", "Test Policy", obj)
	tgt := makeSnapshot("policies", "Test Policy", obj)

	results := compareSnapshots(src, tgt)

	if len(results) != 0 {
		t.Errorf("expected 0 results for identical objects, got %d: %+v", len(results), results)
	}
}

func TestCompareSnapshots_Modified(t *testing.T) {
	src := makeSnapshot("policies", "Deploy Chrome", map[string]any{
		"enabled":  true,
		"category": "Productivity",
	})
	tgt := makeSnapshot("policies", "Deploy Chrome", map[string]any{
		"enabled":  false,
		"category": "Productivity",
	})

	results := compareSnapshots(src, tgt)

	if len(results) != 1 {
		t.Fatalf("expected 1 modified field, got %d: %+v", len(results), results)
	}
	r := results[0]
	if r.Change != diffModified {
		t.Errorf("expected change=modified, got %q", r.Change)
	}
	if r.Field != "enabled" {
		t.Errorf("expected field='enabled', got %q", r.Field)
	}
	if r.OldValue != "true" {
		t.Errorf("expected old_value='true', got %q", r.OldValue)
	}
	if r.NewValue != "false" {
		t.Errorf("expected new_value='false', got %q", r.NewValue)
	}
}

func TestCompareSnapshots_MultipleFieldChanges(t *testing.T) {
	src := makeSnapshot("scripts", "Patch Script", map[string]any{
		"priority":   float64(1),
		"scriptBody": "#!/bin/bash\necho old",
		"notes":      "v1",
	})
	tgt := makeSnapshot("scripts", "Patch Script", map[string]any{
		"priority":   float64(2),
		"scriptBody": "#!/bin/bash\necho new",
		"notes":      "v1",
	})

	results := compareSnapshots(src, tgt)

	if len(results) != 2 {
		t.Fatalf("expected 2 modified fields, got %d: %+v", len(results), results)
	}
	// Results are sorted by field name: priority before scriptBody.
	fields := map[string]bool{}
	for _, r := range results {
		if r.Change != diffModified {
			t.Errorf("unexpected change kind %q", r.Change)
		}
		fields[r.Field] = true
	}
	if !fields["priority"] {
		t.Error("expected 'priority' field diff")
	}
	if !fields["scriptBody"] {
		t.Error("expected 'scriptBody' field diff")
	}
}

func TestCompareSnapshots_MultipleResources(t *testing.T) {
	src := resourceSnapshot{
		"policies": {"Policy A": {"enabled": true}},
		"scripts":  {"Script X": {"priority": float64(1)}},
	}
	tgt := resourceSnapshot{
		"policies": {"Policy A": {"enabled": true}, "Policy B": {"enabled": false}},
		"scripts":  {},
	}

	results := compareSnapshots(src, tgt)

	// Policy B added, Script X removed.
	changeMap := map[string]diffChangeKind{}
	for _, r := range results {
		key := r.Resource + "/" + r.Name
		changeMap[key] = r.Change
	}

	if changeMap["policies/Policy B"] != diffAdded {
		t.Errorf("expected policies/Policy B to be added, got %q", changeMap["policies/Policy B"])
	}
	if changeMap["scripts/Script X"] != diffRemoved {
		t.Errorf("expected scripts/Script X to be removed, got %q", changeMap["scripts/Script X"])
	}
}

// --- diffObjects (field-level diff) ---

func TestDiffObjects_NoChanges(t *testing.T) {
	obj := map[string]any{
		"name":    "Test",
		"enabled": true,
	}
	diffs := diffObjects(obj, obj)
	if len(diffs) != 0 {
		t.Errorf("expected no diffs for identical objects, got %+v", diffs)
	}
}

func TestDiffObjects_FieldAdded(t *testing.T) {
	src := map[string]any{"name": "Test"}
	tgt := map[string]any{"name": "Test", "newField": "value"}

	diffs := diffObjects(src, tgt)
	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(diffs))
	}
	if diffs[0].field != "newField" {
		t.Errorf("expected field 'newField', got %q", diffs[0].field)
	}
	if diffs[0].oldVal != "<nil>" {
		t.Errorf("expected old_value '<nil>', got %q", diffs[0].oldVal)
	}
}

func TestDiffObjects_FieldRemoved(t *testing.T) {
	src := map[string]any{"name": "Test", "oldField": "gone"}
	tgt := map[string]any{"name": "Test"}

	diffs := diffObjects(src, tgt)
	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(diffs))
	}
	if diffs[0].field != "oldField" {
		t.Errorf("expected field 'oldField', got %q", diffs[0].field)
	}
	if diffs[0].newVal != "<nil>" {
		t.Errorf("expected new_value '<nil>', got %q", diffs[0].newVal)
	}
}

func TestDiffObjects_NestedObjectChanges(t *testing.T) {
	src := map[string]any{
		"scope": map[string]any{"all_computers": true},
	}
	tgt := map[string]any{
		"scope": map[string]any{"all_computers": false},
	}

	diffs := diffObjects(src, tgt)
	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff for changed nested object, got %d: %+v", len(diffs), diffs)
	}
	if diffs[0].field != "scope" {
		t.Errorf("expected field 'scope', got %q", diffs[0].field)
	}
}

// --- formatFieldValue ---

func TestFormatFieldValue(t *testing.T) {
	tests := []struct {
		input any
		want  string
	}{
		{nil, "<nil>"},
		{"hello", "hello"},
		{true, "true"},
		{false, "false"},
		{float64(42), "42"},
		{float64(3.14), "3.14"},
		{map[string]any{"k": "v"}, `{"k":"v"}`},
		{[]any{"a", "b"}, `["a","b"]`},
	}

	for _, tt := range tests {
		got := formatFieldValue(tt.input)
		if got != tt.want {
			t.Errorf("formatFieldValue(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// --- normaliseViaJSON ---

func TestNormaliseViaJSON(t *testing.T) {
	// yaml.v3 may produce map[interface{}]interface{} in older versions;
	// normaliseViaJSON should always return map[string]interface{}.
	input := map[string]any{
		"name":    "Test",
		"enabled": true,
		"count":   float64(3),
	}

	result := normaliseViaJSON(input)

	if result["name"] != "Test" {
		t.Errorf("name should be preserved, got %v", result["name"])
	}
}

// --- runDiff integration: two directories ---

func TestRunDiff_TwoDirectories_Added(t *testing.T) {
	srcDir := t.TempDir()
	tgtDir := t.TempDir()

	// Source: one policy.
	writeBackupFileForTest(t, filepath.Join(srcDir, "policies", "old.yaml"), map[string]any{
		"name":    "Old Policy",
		"enabled": true,
	}, "yaml")

	// Target: old policy plus a new one.
	writeBackupFileForTest(t, filepath.Join(tgtDir, "policies", "old.yaml"), map[string]any{
		"name":    "Old Policy",
		"enabled": true,
	}, "yaml")
	writeBackupFileForTest(t, filepath.Join(tgtDir, "policies", "new.yaml"), map[string]any{
		"name":    "New Policy",
		"enabled": false,
	}, "yaml")

	src, err := loadSnapshotFromDirectory(srcDir, nil)
	if err != nil {
		t.Fatalf("loading src: %v", err)
	}
	tgt, err := loadSnapshotFromDirectory(tgtDir, nil)
	if err != nil {
		t.Fatalf("loading tgt: %v", err)
	}

	results := compareSnapshots(src, tgt)

	if len(results) != 1 {
		t.Fatalf("expected 1 result (added), got %d: %+v", len(results), results)
	}
	if results[0].Change != diffAdded {
		t.Errorf("expected added, got %q", results[0].Change)
	}
	if results[0].Name != "New Policy" {
		t.Errorf("expected 'New Policy', got %q", results[0].Name)
	}
}

func TestRunDiff_TwoDirectories_Modified(t *testing.T) {
	srcDir := t.TempDir()
	tgtDir := t.TempDir()

	writeBackupFileForTest(t, filepath.Join(srcDir, "scripts", "deploy.yaml"), map[string]any{
		"name":     "Deploy Script",
		"priority": float64(1),
	}, "yaml")
	writeBackupFileForTest(t, filepath.Join(tgtDir, "scripts", "deploy.yaml"), map[string]any{
		"name":     "Deploy Script",
		"priority": float64(5),
	}, "yaml")

	src, _ := loadSnapshotFromDirectory(srcDir, nil)
	tgt, _ := loadSnapshotFromDirectory(tgtDir, nil)

	results := compareSnapshots(src, tgt)

	if len(results) != 1 {
		t.Fatalf("expected 1 result (modified), got %d: %+v", len(results), results)
	}
	r := results[0]
	if r.Change != diffModified {
		t.Errorf("expected modified, got %q", r.Change)
	}
	if r.Field != "priority" {
		t.Errorf("expected field 'priority', got %q", r.Field)
	}
	if r.OldValue != "1" {
		t.Errorf("expected old_value='1', got %q", r.OldValue)
	}
	if r.NewValue != "5" {
		t.Errorf("expected new_value='5', got %q", r.NewValue)
	}
}

func TestRunDiff_EmptyDirectories(t *testing.T) {
	srcDir := t.TempDir()
	tgtDir := t.TempDir()

	src, err := loadSnapshotFromDirectory(srcDir, nil)
	if err != nil {
		t.Fatalf("loading src: %v", err)
	}
	tgt, err := loadSnapshotFromDirectory(tgtDir, nil)
	if err != nil {
		t.Fatalf("loading tgt: %v", err)
	}

	results := compareSnapshots(src, tgt)
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty directories, got %d", len(results))
	}
}

// --- loadSnapshotFromProfile: auth error path ---

func TestLoadSnapshotFromProfile_BadProfile(t *testing.T) {
	// With no real config on disk, a nonexistent profile should fail gracefully.
	// We can't mock config.Load() easily without refactoring, but we can ensure
	// the function returns an error rather than panicking.
	ctx := context.Background()
	_, err := loadSnapshotFromProfile(ctx, "nonexistent-profile-xyz", nil)
	if err == nil {
		t.Error("expected error for nonexistent profile")
	}
}

// --- YAML round-trip sanity ---

func TestYAMLRoundTrip(t *testing.T) {
	// Verify that objects written by writeBackupFile and read by
	// readObjectsFromSubdir survive the round-trip intact.
	dir := t.TempDir()
	original := map[string]any{
		"name":    "Round Trip Policy",
		"enabled": true,
		"count":   float64(7),
		"tags":    []any{"a", "b"},
	}

	writeBackupFileForTest(t, filepath.Join(dir, "rt.yaml"), original, "yaml")

	data, err := os.ReadFile(filepath.Join(dir, "rt.yaml"))
	if err != nil {
		t.Fatalf("reading file: %v", err)
	}

	var parsed map[string]any
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("yaml parse: %v", err)
	}
	parsed = normaliseViaJSON(parsed)

	if parsed["name"] != "Round Trip Policy" {
		t.Errorf("name mismatch: %v", parsed["name"])
	}
	if parsed["enabled"] != true {
		t.Errorf("enabled mismatch: %v", parsed["enabled"])
	}
}

// --- JSON round-trip sanity ---

func TestJSONRoundTrip(t *testing.T) {
	dir := t.TempDir()
	original := map[string]any{
		"name":    "Round Trip Script",
		"enabled": false,
	}

	writeBackupFileForTest(t, filepath.Join(dir, "rt.json"), original, "json")

	data, err := os.ReadFile(filepath.Join(dir, "rt.json"))
	if err != nil {
		t.Fatalf("reading file: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json parse: %v", err)
	}

	if parsed["name"] != "Round Trip Script" {
		t.Errorf("name mismatch: %v", parsed["name"])
	}
}

// --- nested backup subdirectories (issue #331) ---

// TestLoadSnapshotFromDirectory_NestedSubdirs covers the reported bug directly:
// configuration profiles live two levels deep (profiles/macos, profiles/ios) and
// were invisible to the directory loader, so `diff` reported no change.
func TestLoadSnapshotFromDirectory_NestedSubdirs(t *testing.T) {
	dir := t.TempDir()

	writeBackupFileForTest(t, filepath.Join(dir, "profiles", "macos", "wifi.yaml"), map[string]any{
		"general": map[string]any{"name": "WiFi", "description": "corp wifi"},
	}, "yaml")
	writeBackupFileForTest(t, filepath.Join(dir, "profiles", "ios", "vpn.yaml"), map[string]any{
		"general": map[string]any{"name": "VPN", "description": "corp vpn"},
	}, "yaml")

	snapshot, err := loadSnapshotFromDirectory(dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	profiles, ok := snapshot["profiles"]
	if !ok {
		t.Fatalf("expected 'profiles' in snapshot, got keys %v", snapshotKeys(snapshot))
	}
	// macOS and iOS profiles share one FilterName, so they must merge into one
	// bucket — the same shape live mode produces.
	if len(profiles) != 2 {
		t.Fatalf("expected 2 merged profiles, got %d: %v", len(profiles), profiles)
	}
	for _, want := range []string{"WiFi", "VPN"} {
		if _, ok := profiles[want]; !ok {
			t.Errorf("expected profile %q in the merged bucket", want)
		}
	}
	// The intermediate directory names must never become resource keys.
	for _, unwanted := range []string{"macos", "ios"} {
		if _, ok := snapshot[unwanted]; ok {
			t.Errorf("directory %q leaked into the snapshot as a resource", unwanted)
		}
	}
}

// TestLoadSnapshotFromDirectory_EveryCuratedSubDirIsRead guards the whole bug
// class rather than the one resource that was reported: every curated resource
// must be discoverable at the path `backup` writes it to, bucketed under the
// FilterName live mode uses. A new nested resource added to BackupResources
// fails here if the loader cannot see it.
func TestLoadSnapshotFromDirectory_EveryCuratedSubDirIsRead(t *testing.T) {
	dir := t.TempDir()

	want := make(map[string][]string)
	for _, r := range BackupResources {
		objName := "obj-" + r.Key
		writeBackupFileForTest(t, filepath.Join(dir, filepath.FromSlash(r.SubDir), r.Key+".yaml"),
			map[string]any{"name": objName}, "yaml")
		want[r.FilterName] = append(want[r.FilterName], objName)
	}

	snapshot, err := loadSnapshotFromDirectory(dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for filterName, objNames := range want {
		bucket, ok := snapshot[filterName]
		if !ok {
			t.Errorf("resource %q missing from snapshot (keys: %v)", filterName, snapshotKeys(snapshot))
			continue
		}
		for _, objName := range objNames {
			if _, ok := bucket[objName]; !ok {
				t.Errorf("resource %q: object %q not read from disk", filterName, objName)
			}
		}
	}
}

// TestLoadSnapshotFromDirectory_NestedResourceFilter checks --resources still
// selects by FilterName once the files it names sit two levels down.
func TestLoadSnapshotFromDirectory_NestedResourceFilter(t *testing.T) {
	dir := t.TempDir()

	writeBackupFileForTest(t, filepath.Join(dir, "profiles", "macos", "wifi.yaml"), map[string]any{
		"general": map[string]any{"name": "WiFi"},
	}, "yaml")
	writeBackupFileForTest(t, filepath.Join(dir, "smart-groups", "computers", "all.yaml"), map[string]any{
		"name": "All Macs",
	}, "yaml")

	snapshot, err := loadSnapshotFromDirectory(dir, []string{"profiles"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := snapshot["profiles"]; !ok {
		t.Error("expected nested 'profiles' to match filter 'profiles'")
	}
	if _, ok := snapshot["smart-groups"]; ok {
		t.Error("smart-groups should be excluded when the filter is 'profiles'")
	}
}

// TestLoadSnapshotFromDirectory_IgnoresPackageFiles pins the reason a resource
// directory is not walked recursively: `backup --download-packages` writes
// package binaries under packages/files, and those are not config objects.
func TestLoadSnapshotFromDirectory_IgnoresPackageFiles(t *testing.T) {
	dir := t.TempDir()

	writeBackupFileForTest(t, filepath.Join(dir, "packages", "chrome.yaml"), map[string]any{
		"name": "Chrome.pkg",
	}, "yaml")
	filesDir := filepath.Join(dir, "packages", "files")
	if err := os.MkdirAll(filesDir, 0o755); err != nil {
		t.Fatalf("creating dirs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(filesDir, "manifest.json"), []byte(`{"name":"not a config object"}`), 0o644); err != nil {
		t.Fatalf("writing package file: %v", err)
	}

	snapshot, err := loadSnapshotFromDirectory(dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	packages := snapshot["packages"]
	if len(packages) != 1 {
		t.Fatalf("expected only the package record, got %d: %v", len(packages), packages)
	}
	if _, ok := packages["Chrome.pkg"]; !ok {
		t.Errorf("expected 'Chrome.pkg', got %v", packages)
	}
	if _, ok := snapshot["files"]; ok {
		t.Error("packages/files leaked into the snapshot as a resource")
	}
}

// TestLoadSnapshotFromDirectory_UnknownTopLevelDirStillRead keeps the old
// behaviour for directories outside the curated set — the SDK-backed resources
// (blueprints, compliance-benchmarks) and hand-assembled trees.
func TestLoadSnapshotFromDirectory_UnknownTopLevelDirStillRead(t *testing.T) {
	dir := t.TempDir()

	writeBackupFileForTest(t, filepath.Join(dir, "blueprints", "baseline.yaml"), map[string]any{
		"name": "Baseline",
	}, "yaml")

	snapshot, err := loadSnapshotFromDirectory(dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := snapshot["blueprints"]["Baseline"]; !ok {
		t.Errorf("expected blueprints/Baseline, got %v", snapshot)
	}
}

// TestRunDiff_NestedProfileModified is the end-to-end shape from the issue:
// two backup directories differing only in one configuration profile.
func TestRunDiff_NestedProfileModified(t *testing.T) {
	srcDir := t.TempDir()
	tgtDir := t.TempDir()

	writeBackupFileForTest(t, filepath.Join(srcDir, "profiles", "macos", "wifi.yaml"), map[string]any{
		"general": map[string]any{"name": "WiFi", "description": "old"},
	}, "yaml")
	writeBackupFileForTest(t, filepath.Join(tgtDir, "profiles", "macos", "wifi.yaml"), map[string]any{
		"general": map[string]any{"name": "WiFi", "description": "new"},
	}, "yaml")

	src, err := loadSnapshotFromDirectory(srcDir, nil)
	if err != nil {
		t.Fatalf("loading src: %v", err)
	}
	tgt, err := loadSnapshotFromDirectory(tgtDir, nil)
	if err != nil {
		t.Fatalf("loading tgt: %v", err)
	}

	results := compareSnapshots(src, tgt)
	if len(results) != 1 {
		t.Fatalf("expected 1 modified profile, got %d: %+v", len(results), results)
	}
	if results[0].Resource != "profiles" || results[0].Name != "WiFi" || results[0].Change != diffModified {
		t.Errorf("unexpected result: %+v", results[0])
	}
}

// --- backupObjectName ---

// TestBackupObjectName_MatchesLiveNameExtraction asserts the directory loader
// and the live loader agree on the snapshot key. They diverged for Classic
// resources, whose detail nests the name under "general": the directory side
// fell through to the filename stem (slugified, possibly de-duplicated) while
// the live side used the real name, so every Classic object in a
// directory-vs-instance diff was reported removed and added.
func TestBackupObjectName_MatchesLiveNameExtraction(t *testing.T) {
	tests := []struct {
		name     string
		detail   map[string]any // what backup wrote to disk
		listItem map[string]any // what live mode lists
		nameFld  string         // BackupEndpoint.NameField
		stem     string
	}{
		{
			name:     "modern resource with top-level name",
			detail:   map[string]any{"name": "Deploy Chrome"},
			listItem: map[string]any{"id": "3", "name": "Deploy Chrome"},
			stem:     "deploy-chrome",
		},
		{
			name:     "classic detail nests name under general",
			detail:   map[string]any{"general": map[string]any{"name": "Corp WiFi"}},
			listItem: map[string]any{"id": "7", "name": "Corp WiFi"},
			stem:     "corp-wifi",
		},
		{
			name:     "prestage uses displayName",
			detail:   map[string]any{"displayName": "Standard Mac"},
			listItem: map[string]any{"id": "1", "displayName": "Standard Mac"},
			nameFld:  "displayName",
			stem:     "standard-mac",
		},
		{
			name:     "mobile-device group uses groupName",
			detail:   map[string]any{"groupName": "Static iPads"},
			listItem: map[string]any{"groupId": "6", "groupName": "Static iPads"},
			nameFld:  "groupName",
			stem:     "static-ipads",
		},
		{
			name:     "de-duplicated slug must not become the key",
			detail:   map[string]any{"general": map[string]any{"name": "Corp WiFi"}},
			listItem: map[string]any{"id": "9", "name": "Corp WiFi"},
			stem:     "corp-wifi-2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fromDisk := backupObjectName(tt.detail, tt.nameFld, tt.stem)
			fromLive := extractName(tt.listItem, tt.nameFld, "")
			if fromDisk != fromLive {
				t.Errorf("snapshot keys disagree: directory %q vs live %q", fromDisk, fromLive)
			}
		})
	}
}

func TestBackupObjectName_FallsBackToStem(t *testing.T) {
	got := backupObjectName(map[string]any{"priority": float64(5)}, "", "productivity")
	if got != "productivity" {
		t.Errorf("expected filename stem 'productivity', got %q", got)
	}
}

func snapshotKeys(s resourceSnapshot) []string {
	keys := make([]string, 0, len(s))
	for k := range s {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// TestLoadSnapshotFromDirectory_SymlinkedResourceDir pins a capability this
// loader adds: a resource directory may be a symlink. The old loader skipped
// them (os.ReadDir reports a symlink-to-directory with IsDir() == false);
// reading the curated resources by path follows them for free, and entryIsDir
// keeps the root-level scan consistent with that.
func TestLoadSnapshotFromDirectory_SymlinkedResourceDir(t *testing.T) {
	real := t.TempDir()
	writeBackupFileForTest(t, filepath.Join(real, "shared-scripts", "deploy.yaml"), map[string]any{
		"name": "Deploy Script",
	}, "yaml")

	writeBackupFileForTest(t, filepath.Join(real, "shared-blueprints", "baseline.yaml"), map[string]any{
		"name": "Baseline",
	}, "yaml")

	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(real, "shared-scripts"), filepath.Join(dir, "scripts")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	// blueprints is outside the curated table, so it goes through the
	// root-level scan and entryIsDir rather than through a table path.
	if err := os.Symlink(filepath.Join(real, "shared-blueprints"), filepath.Join(dir, "blueprints")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	snapshot, err := loadSnapshotFromDirectory(dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := snapshot["scripts"]["Deploy Script"]; !ok {
		t.Errorf("expected symlinked scripts directory to be read, got %v", snapshot)
	}
	if _, ok := snapshot["blueprints"]["Baseline"]; !ok {
		t.Errorf("expected symlinked blueprints directory to be read, got %v", snapshot)
	}
}

// TestLoadSnapshotFromDirectory_EveryCuratedNameFieldIsHonoured guards the key
// resolver over the table rather than over the field names that happened to be
// wrong. For every curated resource, an object carrying *only* its declared
// NameField must be keyed by that field's value: mobile-device groups keep
// theirs in groupName and prestages in displayName, and a resource whose name
// is not found falls back to the filename stem — the slugified, possibly
// de-duplicated key live mode never produces, so every object is reported
// removed and added. A resource added later with a new NameField fails here.
func TestLoadSnapshotFromDirectory_EveryCuratedNameFieldIsHonoured(t *testing.T) {
	defs, err := ResolveBackupResources(nil)
	if err != nil {
		t.Fatalf("resolving curated resources: %v", err)
	}

	dir := t.TempDir()
	type expectation struct{ filterName, objName string }
	var want []expectation
	for _, def := range defs {
		field := def.NameField
		if field == "" {
			field = "name"
		}
		objName := "named-" + def.Key
		writeBackupFileForTest(t, filepath.Join(dir, filepath.FromSlash(def.SubDir), "obj.yaml"),
			map[string]any{field: objName}, "yaml")
		want = append(want, expectation{def.FilterName, objName})
	}

	snapshot, err := loadSnapshotFromDirectory(dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, w := range want {
		if _, ok := snapshot[w.filterName][w.objName]; !ok {
			t.Errorf("resource %q: object not keyed on its NameField, bucket holds %v", w.filterName, snapshot[w.filterName])
		}
	}
}

// TestLoadSnapshotFromDirectory_SharedBucketCollisionMatchesLiveOrder pins the
// merge order where two curated entries share a FilterName and the table order
// disagrees with the alphabet: profiles (macos then ios) and accounts (users
// then groups). Both loaders merge with maps.Copy in BackupResources order, so
// the entry listed last wins a name collision. A walk of the tree visits
// lexically and inverts both, which leaves the two sides keeping different
// objects under one key — every field of the survivor reported as modified,
// and the other object never compared at all.
func TestLoadSnapshotFromDirectory_SharedBucketCollisionMatchesLiveOrder(t *testing.T) {
	dir := t.TempDir()

	// Each file records the leaf directory it came from, so the surviving
	// object identifies which curated entry won.
	writeBackupFileForTest(t, filepath.Join(dir, "profiles", "macos", "wifi.yaml"), map[string]any{
		"general": map[string]any{"name": "Corporate Wi-Fi"}, "from": "macos",
	}, "yaml")
	writeBackupFileForTest(t, filepath.Join(dir, "profiles", "ios", "wifi.yaml"), map[string]any{
		"general": map[string]any{"name": "Corporate Wi-Fi"}, "from": "ios",
	}, "yaml")
	writeBackupFileForTest(t, filepath.Join(dir, "accounts", "users", "admin.yaml"), map[string]any{
		"name": "admin", "from": "users",
	}, "yaml")
	writeBackupFileForTest(t, filepath.Join(dir, "accounts", "groups", "admin.yaml"), map[string]any{
		"name": "admin", "from": "groups",
	}, "yaml")

	// lastLeaf reads the winner out of the table instead of hard-coding it, so
	// reordering BackupResources moves the expectation with it.
	lastLeaf := func(filterName string) string {
		leaf := ""
		for _, r := range BackupResources {
			if r.FilterName == filterName {
				leaf = r.SubDir[strings.LastIndex(r.SubDir, "/")+1:]
			}
		}
		return leaf
	}

	snapshot, err := loadSnapshotFromDirectory(dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, tc := range []struct{ resource, objName string }{
		{"profiles", "Corporate Wi-Fi"},
		{"accounts", "admin"},
	} {
		obj, ok := snapshot[tc.resource][tc.objName]
		if !ok {
			t.Errorf("%s: %q missing from the merged bucket, got %v", tc.resource, tc.objName, snapshot[tc.resource])
			continue
		}
		if want := lastLeaf(tc.resource); obj["from"] != want {
			t.Errorf("%s: collision kept the object from %v, want %q (the entry listed last in BackupResources, which is what live mode keeps)", tc.resource, obj["from"], want)
		}
		if len(snapshot[tc.resource]) != 1 {
			t.Errorf("%s: expected the collision to leave one object, got %d", tc.resource, len(snapshot[tc.resource]))
		}
	}
}

// TestLoadSnapshotFromDirectory_ObjectFilesInNestedParent covers a
// hand-assembled tree that drops profiles straight into profiles/ rather than
// profiles/macos: the parent directory name is already the FilterName, so its
// files merge into the same bucket the curated subdirectories fill.
func TestLoadSnapshotFromDirectory_ObjectFilesInNestedParent(t *testing.T) {
	dir := t.TempDir()

	writeBackupFileForTest(t, filepath.Join(dir, "profiles", "loose.yaml"), map[string]any{
		"general": map[string]any{"name": "Loose Profile"},
	}, "yaml")
	writeBackupFileForTest(t, filepath.Join(dir, "profiles", "macos", "wifi.yaml"), map[string]any{
		"general": map[string]any{"name": "WiFi"},
	}, "yaml")

	snapshot, err := loadSnapshotFromDirectory(dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	profiles := snapshot["profiles"]
	if len(profiles) != 2 {
		t.Fatalf("expected the parent's files and the subdirectory's to merge, got %d: %v", len(profiles), profiles)
	}
	for _, want := range []string{"Loose Profile", "WiFi"} {
		if _, ok := profiles[want]; !ok {
			t.Errorf("expected %q in the profiles bucket", want)
		}
	}
}

// TestLoadSnapshotFromDirectory_UnreadableRootIsAnError keeps an unreadable
// source out of the "no differences found" path. An empty snapshot there is not
// a partial read, it is no read at all, and reporting it as agreement with exit
// 0 is the same silent success this loader was fixed to remove.
func TestLoadSnapshotFromDirectory_UnreadableRootIsAnError(t *testing.T) {
	dir := unreadableBackupDirForTest(t)

	if _, err := loadSnapshotFromDirectory(dir, nil); err == nil {
		t.Error("expected an error for an unreadable source directory, got nil")
	}
}

// TestRunDiff_UnreadableSourceDirectoryErrors is the same check at the command
// boundary: a non-nil error is what gives the process a non-zero exit status,
// which is what a CI gate reads.
func TestRunDiff_UnreadableSourceDirectoryErrors(t *testing.T) {
	src := unreadableBackupDirForTest(t)
	tgt := t.TempDir()

	if err := runDiff(context.Background(), diffOptions{Source: src, Target: tgt}); err == nil {
		t.Error("expected runDiff to fail on an unreadable source, got nil (diff would print \"No differences found\" and exit 0)")
	}
}

// unreadableBackupDirForTest returns a backup directory that stats as a
// directory but cannot be listed: mode 0111 is searchable, not readable.
func unreadableBackupDirForTest(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX directory permissions")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}

	dir := filepath.Join(t.TempDir(), "backup")
	writeBackupFileForTest(t, filepath.Join(dir, "policies", "test.yaml"), map[string]any{"name": "Test"}, "yaml")
	if err := os.Chmod(dir, 0o111); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	return dir
}

// TestLoadSnapshotFromDirectory_ComplianceBenchmarkKeyedOnTitle pins the key
// for the one root-written resource whose name is not called "name".
// backupBenchmarks writes a benchmark's name as "title" and names the file
// SlugifyName(title), while live mode keys on bm.Title. With no declared name
// field the directory side fell through to the stem, so a slug such as
// "cis-level-1" faced a live key of "CIS Level 1" and every benchmark was
// reported removed and added. Blueprints, the other root-written resource,
// export a "name" and were never affected.
func TestLoadSnapshotFromDirectory_ComplianceBenchmarkKeyedOnTitle(t *testing.T) {
	dir := t.TempDir()

	writeBackupFileForTest(t, filepath.Join(dir, "compliance-benchmarks", "cis-level-1.yaml"), map[string]any{
		"title":           "CIS Level 1",
		"description":     "CIS macOS benchmark",
		"baselineId":      "b-1",
		"enforcementMode": "audit",
	}, "yaml")

	snapshot, err := loadSnapshotFromDirectory(dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := snapshot["compliance-benchmarks"]["CIS Level 1"]; !ok {
		t.Errorf("benchmark not keyed on its title, bucket holds %v", snapshot["compliance-benchmarks"])
	}
	if _, ok := snapshot["compliance-benchmarks"]["cis-level-1"]; ok {
		t.Error("benchmark keyed on the filename stem, which live mode never produces")
	}
}

// TestLoadSnapshotFromDirectory_EveryPlatformNameFieldIsHonoured is the
// platformNameFields analogue of the curated table guard: for every SDK-backed
// resource backup writes to the backup root, an object carrying only its
// declared name field must be keyed by that field's value. A platform resource
// added later with a new name field fails here rather than in a diff against a
// live tenant.
func TestLoadSnapshotFromDirectory_EveryPlatformNameFieldIsHonoured(t *testing.T) {
	dir := t.TempDir()

	type expectation struct{ resource, objName string }
	var want []expectation
	for resource, field := range platformNameFields {
		objName := "named-" + resource
		writeBackupFileForTest(t, filepath.Join(dir, resource, "obj.yaml"),
			map[string]any{field: objName}, "yaml")
		want = append(want, expectation{resource, objName})
	}
	if len(want) == 0 {
		t.Fatal("platformNameFields is empty; the guard would assert nothing")
	}

	snapshot, err := loadSnapshotFromDirectory(dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, w := range want {
		if _, ok := snapshot[w.resource][w.objName]; !ok {
			t.Errorf("resource %q: object not keyed on its name field, bucket holds %v", w.resource, snapshot[w.resource])
		}
	}
}
