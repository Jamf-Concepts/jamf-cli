package commands

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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
	}
	if len(policies) != 2 {
		t.Errorf("expected 2 policies, got %d", len(policies))
	}

	chrome, ok := policies["Deploy Chrome"]
	if !ok {
		t.Fatal("expected 'Deploy Chrome' object")
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
