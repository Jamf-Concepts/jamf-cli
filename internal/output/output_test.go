// Copyright 2026, Jamf Software LLC

package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func newTestFormatter(format string) (*Formatter, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	f := &Formatter{
		format:  Format(format),
		writer:  buf,
		noColor: true,
		wide:    false, // default to compact mode for tests
	}
	return f, buf
}

// --- JSON tests ---

func TestPrintRaw_JSON_SingleObject(t *testing.T) {
	f, buf := newTestFormatter("json")
	input := `{"id":1,"name":"Test"}`
	err := f.PrintRaw([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should be pretty-printed
	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nOutput: %s", err, buf.String())
	}
	if result["id"] != float64(1) {
		t.Errorf("expected id=1, got %v", result["id"])
	}
	if result["name"] != "Test" {
		t.Errorf("expected name=Test, got %v", result["name"])
	}
	// Verify it's indented (pretty-printed)
	if !strings.Contains(buf.String(), "\n") {
		t.Error("expected pretty-printed JSON with newlines")
	}
}

func TestPrintRaw_JSON_Array(t *testing.T) {
	f, buf := newTestFormatter("json")
	input := `[{"id":1,"name":"A"},{"id":2,"name":"B"}]`
	err := f.PrintRaw([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON array: %v\nOutput: %s", err, buf.String())
	}
	if len(result) != 2 {
		t.Errorf("expected 2 items, got %d", len(result))
	}
}

// --- YAML tests ---

func TestPrintRaw_YAML_SingleObject(t *testing.T) {
	f, buf := newTestFormatter("yaml")
	input := `{"id":1,"name":"Test Device"}`
	err := f.PrintRaw([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "id:") {
		t.Errorf("expected 'id:' in YAML output, got:\n%s", out)
	}
	if !strings.Contains(out, "name:") {
		t.Errorf("expected 'name:' in YAML output, got:\n%s", out)
	}
}

func TestPrintRaw_YAML_Array(t *testing.T) {
	f, buf := newTestFormatter("yaml")
	input := `[{"id":1,"name":"A"},{"id":2,"name":"B"}]`
	err := f.PrintRaw([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	// YAML array items start with "- "
	if !strings.Contains(out, "- id:") {
		t.Errorf("expected YAML array format with '- id:', got:\n%s", out)
	}
}

// --- Table tests ---

func TestPrintRaw_Table_Array(t *testing.T) {
	f, buf := newTestFormatter("table")
	input := `[{"id":1,"name":"Computer A"},{"id":2,"name":"Computer B"}]`
	err := f.PrintRaw([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	// New format: summary + blank + header + separator + data rows
	if !strings.Contains(out, "(2 total)") {
		t.Errorf("expected summary header with count, got:\n%s", out)
	}
	if !strings.Contains(out, "ID") {
		t.Errorf("expected 'ID' in header, got:\n%s", out)
	}
	if !strings.Contains(out, "NAME") {
		t.Errorf("expected 'NAME' in header, got:\n%s", out)
	}
	if !strings.Contains(out, "─") {
		t.Errorf("expected box-drawing separator, got:\n%s", out)
	}
	if !strings.Contains(out, "Computer A") {
		t.Errorf("expected 'Computer A' in output, got:\n%s", out)
	}
}

func TestPrintRaw_Table_SingleObject(t *testing.T) {
	f, buf := newTestFormatter("table")
	input := `{"id":42,"name":"Single Device"}`
	err := f.PrintRaw([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	// A single object renders as a vertical detail view, not a 1-row table.
	if !strings.Contains(out, "DETAILS") {
		t.Errorf("expected DETAILS detail-view header, got:\n%s", out)
	}
	if strings.Contains(out, "total)") {
		t.Errorf("single object should not render the list-table summary, got:\n%s", out)
	}
	if !strings.Contains(out, "42") {
		t.Errorf("expected '42' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Single Device") {
		t.Errorf("expected 'Single Device' in output, got:\n%s", out)
	}
}

func TestPrintRaw_Table_EmptyArray(t *testing.T) {
	f, buf := newTestFormatter("table")
	input := `[]`
	err := f.PrintRaw([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if strings.TrimSpace(out) != "" {
		t.Errorf("expected empty output for empty array, got: %s", out)
	}
}

func TestPrintRaw_Table_DeterministicColumnOrder(t *testing.T) {
	f, buf := newTestFormatter("table")
	f.wide = true // Use wide mode to see all columns
	// id and name should always come first, rest alphabetical
	input := `[{"zebra":"z","id":1,"alpha":"a","name":"test"}]`
	err := f.PrintRaw([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	// Find the header line (after summary and blank line)
	lines := strings.Split(out, "\n")
	var header string
	for _, line := range lines {
		if strings.Contains(line, "ID") && strings.Contains(line, "NAME") {
			header = line
			break
		}
	}
	if header == "" {
		t.Fatalf("could not find header line in:\n%s", out)
	}

	idIdx := strings.Index(header, "ID")
	nameIdx := strings.Index(header, "NAME")
	alphaIdx := strings.Index(header, "ALPHA")
	zebraIdx := strings.Index(header, "ZEBRA")

	if idIdx < 0 || nameIdx < 0 || alphaIdx < 0 || zebraIdx < 0 {
		t.Fatalf("missing headers in: %s", header)
	}

	// id first, then name, then alpha, then zebra
	if idIdx >= nameIdx {
		t.Errorf("ID should come before NAME: %s", header)
	}
	if nameIdx >= alphaIdx {
		t.Errorf("NAME should come before ALPHA: %s", header)
	}
	if alphaIdx >= zebraIdx {
		t.Errorf("ALPHA should come before ZEBRA: %s", header)
	}
}

func TestPrintRaw_Table_NestedObject(t *testing.T) {
	f, buf := newTestFormatter("table")
	f.wide = true // Use wide mode to see all columns
	input := `[{"id":1,"details":{"cpu":"M1","ram":"16GB"}}]`
	err := f.PrintRaw([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	// Nested object flattened; common prefix "details." stripped since all dotted keys share it
	if !strings.Contains(out, "CPU") {
		t.Errorf("expected CPU column header, got:\n%s", out)
	}
	if !strings.Contains(out, "M1") {
		t.Errorf("expected M1 value, got:\n%s", out)
	}
	if !strings.Contains(out, "RAM") {
		t.Errorf("expected RAM column header, got:\n%s", out)
	}
	if !strings.Contains(out, "16GB") {
		t.Errorf("expected 16GB value, got:\n%s", out)
	}
}

func TestPrintRaw_Table_NestedFlattening_ComputersInventory(t *testing.T) {
	f, buf := newTestFormatter("table")
	// Simulated computers-inventory response: useful data nested, empty sections dropped
	input := `[{"id":"5","udid":"ABC-123","general":{"name":"MacBook Pro","platform":"Mac","supervised":true},"applications":[],"hardware":{}}]`
	err := f.PrintRaw([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	// Common prefix "general." should be stripped — column shows NAME not GENERAL.NAME
	if !strings.Contains(out, "NAME") {
		t.Errorf("expected NAME column, got:\n%s", out)
	}
	if strings.Contains(out, "GENERAL.NAME") {
		t.Errorf("common prefix should be stripped — expected NAME not GENERAL.NAME, got:\n%s", out)
	}
	if !strings.Contains(out, "MacBook Pro") {
		t.Errorf("expected 'MacBook Pro' value, got:\n%s", out)
	}
	// Empty arrays/objects should be dropped
	if strings.Contains(out, "APPLICATIONS") {
		t.Errorf("empty array 'applications' should be dropped, got:\n%s", out)
	}
	if strings.Contains(out, "HARDWARE") {
		t.Errorf("empty object 'hardware' should be dropped, got:\n%s", out)
	}
}

func TestPrintRaw_Table_MixedPrefixesNotStripped(t *testing.T) {
	f, buf := newTestFormatter("table")
	f.wide = true
	// Two different section prefixes — should NOT strip
	input := `[{"id":"5","general":{"name":"Mac"},"hardware":{"serial":"ABC"}}]`
	err := f.PrintRaw([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "GENERAL.NAME") {
		t.Errorf("mixed prefixes should NOT be stripped, expected GENERAL.NAME, got:\n%s", out)
	}
	if !strings.Contains(out, "HARDWARE.SERIAL") {
		t.Errorf("mixed prefixes should NOT be stripped, expected HARDWARE.SERIAL, got:\n%s", out)
	}
}

func TestPrintRaw_Table_NoFlatteningForFlatData(t *testing.T) {
	f, buf := newTestFormatter("table")
	// Flat data should pass through unchanged (no nested objects)
	input := `[{"id":1,"name":"Test","serial":"ABC"}]`
	err := f.PrintRaw([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "NAME") {
		t.Errorf("expected NAME column for flat data, got:\n%s", out)
	}
	if !strings.Contains(out, "SERIAL") {
		t.Errorf("expected SERIAL column for flat data, got:\n%s", out)
	}
}

// --- CSV tests ---

func TestPrintRaw_CSV_Array(t *testing.T) {
	f, buf := newTestFormatter("csv")
	input := `[{"id":1,"name":"A"},{"id":2,"name":"B"}]`
	err := f.PrintRaw([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines (header + 2 rows), got %d:\n%s", len(lines), out)
	}
	// Headers should be present (not uppercase for CSV)
	header := lines[0]
	if !strings.Contains(header, "id") {
		t.Errorf("expected 'id' in CSV header, got: %s", header)
	}
}

func TestPrintRaw_CSV_SingleObject(t *testing.T) {
	f, buf := newTestFormatter("csv")
	input := `{"id":1,"name":"Test"}`
	err := f.PrintRaw([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines (header + 1 row), got %d:\n%s", len(lines), out)
	}
}

func TestPrintRaw_CSV_ValuesWithCommas(t *testing.T) {
	f, buf := newTestFormatter("csv")
	input := `[{"id":1,"name":"Doe, John"}]`
	err := f.PrintRaw([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	// Values with commas should be quoted
	if !strings.Contains(out, `"Doe, John"`) {
		t.Errorf("expected quoted value for comma-containing string, got:\n%s", out)
	}
}

func TestPrintRaw_CSV_DeterministicColumnOrder(t *testing.T) {
	f, buf := newTestFormatter("csv")
	input := `[{"zebra":"z","id":1,"name":"test","alpha":"a"}]`
	err := f.PrintRaw([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	header := lines[0]

	idIdx := strings.Index(header, "id")
	nameIdx := strings.Index(header, "name")
	alphaIdx := strings.Index(header, "alpha")
	zebraIdx := strings.Index(header, "zebra")

	if idIdx >= nameIdx {
		t.Errorf("id should come before name in CSV: %s", header)
	}
	if nameIdx >= alphaIdx {
		t.Errorf("name should come before alpha in CSV: %s", header)
	}
	if alphaIdx >= zebraIdx {
		t.Errorf("alpha should come before zebra in CSV: %s", header)
	}
}

// --- Plain tests ---

func TestPrintRaw_Plain_Array(t *testing.T) {
	f, buf := newTestFormatter("plain")
	input := `[{"id":1,"name":"A"},{"id":2,"name":"B"}]`
	err := f.PrintRaw([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	// Plain has no header, just data rows
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines (no header), got %d:\n%s", len(lines), out)
	}
	// Tab-separated
	if !strings.Contains(lines[0], "\t") {
		t.Errorf("expected tab-separated values, got: %s", lines[0])
	}
}

func TestPrintRaw_Plain_SingleObject(t *testing.T) {
	f, buf := newTestFormatter("plain")
	input := `{"id":1,"name":"Test"}`
	err := f.PrintRaw([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 line for single object, got %d:\n%s", len(lines), out)
	}
	if !strings.Contains(lines[0], "\t") {
		t.Errorf("expected tab-separated values, got: %s", lines[0])
	}
}

func TestPrintRaw_Plain_ScalarValue(t *testing.T) {
	f, buf := newTestFormatter("plain")
	input := `"hello world"`
	err := f.PrintRaw([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := strings.TrimSpace(buf.String())
	if out != "hello world" {
		t.Errorf("expected 'hello world', got: %s", out)
	}
}

func TestPrintRaw_Plain_DeterministicColumnOrder(t *testing.T) {
	f, buf := newTestFormatter("plain")
	input := `[{"zebra":"z","id":1,"name":"test","alpha":"a"}]`
	err := f.PrintRaw([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := strings.TrimSpace(buf.String())
	// Values should be in order: id, name, alpha, zebra => 1, test, a, z
	parts := strings.Split(out, "\t")
	if len(parts) != 4 {
		t.Fatalf("expected 4 tab-separated values, got %d: %v", len(parts), parts)
	}
	if parts[0] != "1" {
		t.Errorf("first column should be id=1, got: %s", parts[0])
	}
	if parts[1] != "test" {
		t.Errorf("second column should be name=test, got: %s", parts[1])
	}
	if parts[2] != "a" {
		t.Errorf("third column should be alpha=a, got: %s", parts[2])
	}
	if parts[3] != "z" {
		t.Errorf("fourth column should be zebra=z, got: %s", parts[3])
	}
}

// --- FormatValue tests ---

func TestFormatValue(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
	}{
		{"nil", nil, ""},
		{"string", "hello", "hello"},
		{"integer float", float64(42), "42"},
		{"decimal float", float64(3.14), "3.14"},
		{"bool true", true, "true"},
		{"bool false", false, "false"},
		{"nested map", map[string]any{"a": "b"}, `{"a":"b"}`},
		{"nested slice", []any{"a", "b"}, `["a","b"]`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := FormatValue(tc.input)
			if result != tc.expected {
				t.Errorf("FormatValue(%v) = %q, want %q", tc.input, result, tc.expected)
			}
		})
	}
}

// --- sortedKeys tests ---

func TestSortedKeys(t *testing.T) {
	m := map[string]any{
		"zebra": "z",
		"id":    1,
		"name":  "test",
		"alpha": "a",
	}
	keys := sortedKeys(m)
	expected := []string{"id", "name", "alpha", "zebra"}
	if len(keys) != len(expected) {
		t.Fatalf("expected %d keys, got %d: %v", len(expected), len(keys), keys)
	}
	for i, k := range expected {
		if keys[i] != k {
			t.Errorf("key[%d] = %q, want %q (full: %v)", i, keys[i], k, keys)
		}
	}
}

func TestSortedKeys_DottedNames(t *testing.T) {
	m := map[string]any{
		"id":               "5",
		"udid":             "ABC",
		"general.name":     "MacBook",
		"general.platform": "Mac",
	}
	keys := sortedKeys(m)
	// id first, general.name second (dotted name priority), rest alphabetical
	if keys[0] != "id" {
		t.Errorf("expected id first, got %q", keys[0])
	}
	if keys[1] != "general.name" {
		t.Errorf("expected general.name second, got %q", keys[1])
	}
}

func TestKeyPriority_DottedNames(t *testing.T) {
	tests := []struct {
		key      string
		expected int
	}{
		{"id", 0},
		{"name", 1},
		{"general.name", 1},
		{"general.platform", 2},
		{"general.site.name", 2}, // two dots — not promoted
		{"udid", 2},
	}
	for _, tc := range tests {
		t.Run(tc.key, func(t *testing.T) {
			got := keyPriority(tc.key)
			if got != tc.expected {
				t.Errorf("keyPriority(%q) = %d, want %d", tc.key, got, tc.expected)
			}
		})
	}
}

func TestFlattenRows_FlatData(t *testing.T) {
	rows := []map[string]any{
		{"id": float64(1), "name": "Test"},
	}
	result := flattenRows(rows)
	// Should return original slice (no allocation)
	if len(result) != 1 || result[0]["name"] != "Test" {
		t.Errorf("flat data should pass through unchanged, got: %v", result)
	}
}

func TestFlattenRows_NestedData(t *testing.T) {
	rows := []map[string]any{
		{
			"id":          "5",
			"general":     map[string]any{"name": "Mac", "platform": "Mac"},
			"hardware":    map[string]any{}, // empty — dropped
			"apps":        []any{},          // empty array — dropped
			"tags":        []any{"a", "b"},  // non-empty array — kept
			"description": nil,              // nil — dropped
		},
	}
	result := flattenRows(rows)
	if len(result) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result))
	}
	flat := result[0]
	if flat["id"] != "5" {
		t.Errorf("id should be preserved, got %v", flat["id"])
	}
	// Common prefix "general." stripped since all dotted keys share it
	if flat["name"] != "Mac" {
		t.Errorf("general.name should be flattened and prefix-stripped to 'name', got %v", flat["name"])
	}
	if flat["platform"] != "Mac" {
		t.Errorf("general.platform should be flattened and prefix-stripped to 'platform', got %v", flat["platform"])
	}
	if _, ok := flat["hardware"]; ok {
		t.Error("empty object 'hardware' should be dropped")
	}
	if _, ok := flat["apps"]; ok {
		t.Error("empty array 'apps' should be dropped")
	}
	if _, ok := flat["tags"]; !ok {
		t.Error("non-empty array 'tags' should be kept")
	}
	if _, ok := flat["description"]; ok {
		t.Error("nil 'description' should be dropped")
	}
}

func TestSortedKeys_NoIdNoName(t *testing.T) {
	m := map[string]any{
		"zebra": "z",
		"alpha": "a",
		"beta":  "b",
	}
	keys := sortedKeys(m)
	expected := []string{"alpha", "beta", "zebra"}
	for i, k := range expected {
		if keys[i] != k {
			t.Errorf("key[%d] = %q, want %q", i, keys[i], k)
		}
	}
}

// --- normalizeForTabular tests ---

func TestNormalizeForTabular_SliceOfMaps(t *testing.T) {
	input := []any{
		map[string]any{"id": float64(1)},
		map[string]any{"id": float64(2)},
	}
	result := normalizeForTabular(input)
	slice, ok := result.([]map[string]any)
	if !ok {
		t.Fatalf("expected []map[string]interface{}, got %T", result)
	}
	if len(slice) != 2 {
		t.Errorf("expected 2 items, got %d", len(slice))
	}
}

func TestNormalizeForTabular_SingleMap(t *testing.T) {
	input := map[string]any{"id": float64(1), "name": "test"}
	result := normalizeForTabular(input)
	slice, ok := result.([]map[string]any)
	if !ok {
		t.Fatalf("expected []map[string]interface{}, got %T", result)
	}
	if len(slice) != 1 {
		t.Errorf("expected 1 item, got %d", len(slice))
	}
}

func TestNormalizeForTabular_ScalarString(t *testing.T) {
	input := "hello"
	result := normalizeForTabular(input)
	s, ok := result.(string)
	if !ok {
		t.Fatalf("expected string, got %T", result)
	}
	if s != "hello" {
		t.Errorf("expected 'hello', got %q", s)
	}
}

func TestNormalizeForTabular_MixedArray(t *testing.T) {
	// Mixed array should return raw data, not drop non-map items
	input := []any{
		map[string]any{"id": "1"},
		"stray string",
		map[string]any{"id": "2"},
	}
	result := normalizeForTabular(input)
	// Should return the original data unchanged (not []map[string]interface{})
	arr, ok := result.([]any)
	if !ok {
		t.Fatalf("expected []interface{}, got %T", result)
	}
	if len(arr) != 3 {
		t.Errorf("expected 3 items, got %d", len(arr))
	}
}

func TestNormalizeForTabular_EmptySlice(t *testing.T) {
	input := []any{}
	result := normalizeForTabular(input)
	slice, ok := result.([]map[string]any)
	if !ok {
		t.Fatalf("expected []map[string]interface{}, got %T", result)
	}
	if len(slice) != 0 {
		t.Errorf("expected 0 items, got %d", len(slice))
	}
}

func TestPrintRaw_Table_NilValues(t *testing.T) {
	f, buf := newTestFormatter("table")
	input := `[{"id":1,"name":null}]`
	err := f.PrintRaw([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	// Should contain the ID value and handle null gracefully
	if !strings.Contains(out, "1") {
		t.Errorf("expected '1' in output, got:\n%s", out)
	}
}

func TestPrintRaw_Table_BooleanValues(t *testing.T) {
	f, buf := newTestFormatter("table")
	input := `[{"id":1,"managed":true}]`
	err := f.PrintRaw([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "true") {
		t.Errorf("expected 'true' in output, got:\n%s", out)
	}
}

func TestPrintRaw_Table_NumericValues(t *testing.T) {
	f, buf := newTestFormatter("table")
	f.wide = true // Use wide mode to see all columns
	input := `[{"id":1,"count":42}]`
	err := f.PrintRaw([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	// Should display as "42" not "42.000000" or "4.200000e+01"
	if !strings.Contains(out, "42") {
		t.Errorf("expected '42' in output, got:\n%s", out)
	}
	if strings.Contains(out, "42.") {
		t.Errorf("integer should not have decimal point, got:\n%s", out)
	}
}

// --- Enhanced table output tests ---

func TestPrintRaw_Table_BoxDrawing(t *testing.T) {
	f, buf := newTestFormatter("table")
	input := `[{"id":1,"name":"Test"}]`
	err := f.PrintRaw([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "─") {
		t.Errorf("expected box-drawing character in output, got:\n%s", out)
	}
}

func TestPrintRaw_Table_SummaryHeader(t *testing.T) {
	f, buf := newTestFormatter("table")
	input := `[{"id":1},{"id":2},{"id":3}]`
	err := f.PrintRaw([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "(3 total)") {
		t.Errorf("expected '(3 total)' in output, got:\n%s", out)
	}
}

func TestPrintRaw_Table_StatusColors(t *testing.T) {
	f, buf := newTestFormatter("table")
	f.noColor = false // Enable colors
	input := `[{"id":1,"status":"Active"}]`
	err := f.PrintRaw([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	// Should contain status symbol
	if !strings.Contains(out, "●") {
		t.Errorf("expected status symbol ● for Active status, got:\n%s", out)
	}
	// Should contain green color code
	if !strings.Contains(out, "\033[32m") {
		t.Errorf("expected green color code for Active status, got:\n%s", out)
	}
}

func TestPrintRaw_Table_NoColorFlag(t *testing.T) {
	f, buf := newTestFormatter("table")
	f.noColor = true
	input := `[{"id":1,"status":"Active"}]`
	err := f.PrintRaw([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	// Should NOT contain ANSI color codes when noColor is true
	if strings.Contains(out, "\033[") {
		t.Errorf("expected no ANSI codes with noColor=true, got:\n%s", out)
	}
}

func TestFormatStatusValue(t *testing.T) {
	f := &Formatter{noColor: false, writer: nil, format: FormatTable}
	tests := []struct {
		input  string
		symbol string
	}{
		{"Active", "●"},
		{"Inactive", "○"},
		{"Pending", "◐"},
		{"Failed", "●"},
		{"enabled", "●"},
		{"disabled", "○"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := f.formatStatusValue(tc.input)
			if !strings.Contains(result, tc.symbol) {
				t.Errorf("formatStatusValue(%q) missing symbol %q, got %q", tc.input, tc.symbol, result)
			}
		})
	}
}

func TestIsStatusColumn(t *testing.T) {
	tests := []struct {
		name   string
		expect bool
	}{
		{"status", true},
		{"Status", true},
		{"enrollmentStatus", true},
		{"managed", true},
		{"isManaged", true},
		{"userApprovedMdm", true},
		{"mdmCapable", false},    // suffix matching: doesn't end with "mdm"
		{"remoteDesktop", false}, // suffix matching: doesn't end with "remote"
		{"supervised", true},
		{"name", false},
		{"id", false},
		{"description", false},
		{"mdmAccessRights", false}, // doesn't end with any status suffix
		{"stateProvince", false},   // doesn't end with "state"
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := isStatusColumn(tc.name)
			if result != tc.expect {
				t.Errorf("isStatusColumn(%q) = %v, want %v", tc.name, result, tc.expect)
			}
		})
	}
}

func TestColorize(t *testing.T) {
	// With colors enabled
	f := &Formatter{noColor: false}
	result := f.colorize("test", colorGreen)
	if result != "\033[32mtest\033[0m" {
		t.Errorf("colorize with colors enabled: got %q", result)
	}

	// With colors disabled
	f.noColor = true
	result = f.colorize("test", colorGreen)
	if result != "test" {
		t.Errorf("colorize with colors disabled: got %q, want %q", result, "test")
	}
}

// --- Column filtering tests ---

func TestPrintRaw_Table_DefaultColumns(t *testing.T) {
	f, buf := newTestFormatter("table")
	f.wide = false
	input := `[{"id":1,"name":"Test","serial":"ABC123","extra":"data","status":"Active"}]`
	err := f.PrintRaw([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	// 5 columns <= 8 limit, so all should show
	for _, col := range []string{"ID", "NAME", "STATUS", "SERIAL", "EXTRA"} {
		if !strings.Contains(out, col) {
			t.Errorf("expected %s column, got:\n%s", col, out)
		}
	}
}

func TestPrintRaw_Table_WideMode(t *testing.T) {
	f, buf := newTestFormatter("table")
	f.wide = true
	input := `[{"id":1,"name":"Test","serial":"ABC123","extra":"data"}]`
	err := f.PrintRaw([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	// Wide mode should show all columns
	if !strings.Contains(out, "SERIAL") {
		t.Error("expected SERIAL column in wide mode")
	}
	if !strings.Contains(out, "EXTRA") {
		t.Error("expected EXTRA column in wide mode")
	}
}

func TestDefaultColumns(t *testing.T) {
	allKeys := []string{"zebra", "id", "serial", "name", "status", "extra"}
	result := defaultColumns(allKeys, nil)

	// 6 keys <= 8 limit, so all are returned
	if len(result) != 6 {
		t.Fatalf("expected 6 columns (all keys <= limit), got %d: %v", len(result), result)
	}
}

func TestDefaultColumns_NoStatusColumns(t *testing.T) {
	allKeys := []string{"id", "name", "serial", "extra"}
	result := defaultColumns(allKeys, nil)

	// 4 keys <= 8 limit, so all are returned
	if len(result) != 4 {
		t.Fatalf("expected 4 columns (all keys <= limit), got %d: %v", len(result), result)
	}
}

func TestDefaultColumns_NoDefaultColumns(t *testing.T) {
	allKeys := []string{"serial", "extra", "zebra"}
	result := defaultColumns(allKeys, nil)

	// 3 keys <= 8 limit, so all are returned
	if len(result) != 3 {
		t.Fatalf("expected 3 columns (all keys <= limit), got %d: %v", len(result), result)
	}
}

func TestDefaultColumns_OverLimit(t *testing.T) {
	allKeys := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}
	row := map[string]any{"a": "1", "b": "2", "c": "3", "d": "4", "e": "5", "f": "6", "g": "7", "h": "8", "i": "9", "j": "10"}
	result := defaultColumns(allKeys, row)

	// 10 keys > 8 limit, so only first 8 returned
	if len(result) != 8 {
		t.Fatalf("expected 8 columns (truncated to limit), got %d: %v", len(result), result)
	}
}

func TestDefaultColumns_ArraysDeprioritized(t *testing.T) {
	// 10 keys: 8 scalar + 2 array. Arrays should be pushed after scalars.
	allKeys := []string{"id", "name", "bigArray", "c", "d", "e", "f", "otherArray", "g", "h"}
	row := map[string]any{
		"id": "1", "name": "Test", "c": "c", "d": "d", "e": "e", "f": "f", "g": "g", "h": "h",
		"bigArray":   []any{"x", "y"},
		"otherArray": []any{"a"},
	}
	result := defaultColumns(allKeys, row)

	if len(result) != 8 {
		t.Fatalf("expected 8 columns, got %d: %v", len(result), result)
	}
	// Array columns should not be in the first 8 (there are 8 scalar columns)
	for _, k := range result {
		if k == "bigArray" || k == "otherArray" {
			t.Errorf("array column %q should be deprioritized out of default 8, got: %v", k, result)
		}
	}
}

func TestPrintRaw_Table_FallbackToAllColumns(t *testing.T) {
	f, buf := newTestFormatter("table")
	f.wide = false
	// Data with no id, name, or status columns
	input := `[{"serial":"ABC123","extra":"data","zebra":"z"}]`
	err := f.PrintRaw([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	// Should fallback to showing all columns
	if !strings.Contains(out, "SERIAL") {
		t.Error("expected SERIAL column in fallback mode")
	}
	if !strings.Contains(out, "EXTRA") {
		t.Error("expected EXTRA column in fallback mode")
	}
	if !strings.Contains(out, "ZEBRA") {
		t.Error("expected ZEBRA column in fallback mode")
	}
}

func TestDefaultColumns_WithImportantColumns(t *testing.T) {
	allKeys := []string{"id", "name", "serialNumber", "lastContactDate", "udid", "extra", "status"}
	result := defaultColumns(allKeys, nil)

	// 7 keys <= 8 limit, so all are returned
	if len(result) != 7 {
		t.Fatalf("expected 7 columns (all keys <= limit), got %d: %v", len(result), result)
	}
}

func TestPrintRaw_Table_ComputerColumns(t *testing.T) {
	f, buf := newTestFormatter("table")
	f.wide = false
	// Simulated computer list response — 8 keys = limit, all shown
	input := `[{"id":"1","name":"MacBook Pro","serialNumber":"C02X1234","lastContactDate":"2026-02-05","lastReportDate":"2026-02-04","udid":"ABC-123","macAddress":"AA:BB:CC","isManaged":true}]`
	err := f.PrintRaw([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()

	// 8 keys = limit, all columns shown
	for _, col := range []string{"ID", "NAME", "SERIALNUMBER", "LASTCONTACTDATE", "LASTREPORTDATE", "ISMANAGED", "UDID", "MACADDRESS"} {
		if !strings.Contains(out, col) {
			t.Errorf("expected %s column in default computer output, got:\n%s", col, out)
		}
	}
}

func TestPrintRaw_Table_MobileDeviceColumns(t *testing.T) {
	f, buf := newTestFormatter("table")
	f.wide = false
	// Simulated mobile device list response — 8 keys = limit, all shown
	input := `[{"id":"1","name":"iPad Pro","serialNumber":"DMQVGC0DHLA0","type":"ios","model":"iPad Pro 11-inch","wifiMacAddress":"C4:84:66:92:78:00","udid":"0dad565fb40b010a9e490440188063a378721069","username":"admin"}]`
	err := f.PrintRaw([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()

	// 8 keys = limit, all columns shown
	for _, col := range []string{"ID", "NAME", "SERIALNUMBER", "TYPE", "MODEL", "WIFIMACADDRESS", "UDID", "USERNAME"} {
		if !strings.Contains(out, col) {
			t.Errorf("expected %s column in default mobile device output, got:\n%s", col, out)
		}
	}
}

func TestPrintRaw_Table_MobileDeviceColumns_WideMode(t *testing.T) {
	f, buf := newTestFormatter("table")
	f.wide = true
	input := `[{"id":"1","name":"iPad Pro","serialNumber":"DMQVGC0DHLA0","type":"ios","model":"iPad Pro","wifiMacAddress":"C4:84:66:92:78:00","udid":"0dad565fb40b010a9e490440188063a378721069"}]`
	err := f.PrintRaw([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()

	// Wide mode should show ALL columns
	for _, col := range []string{"ID", "NAME", "SERIALNUMBER", "TYPE", "MODEL", "WIFIMACADDRESS", "UDID"} {
		if !strings.Contains(out, col) {
			t.Errorf("expected %s column in wide mode, got:\n%s", col, out)
		}
	}
}

func TestPrintRaw_Table_GenericResourceWithType(t *testing.T) {
	f, buf := newTestFormatter("table")
	f.wide = false
	// Generic resource — 5 keys <= 8 limit, all shown
	input := `[{"id":"1","name":"Install Chrome","type":"policy","description":"Installs Chrome browser","category":"Software"}]`
	err := f.PrintRaw([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()

	for _, col := range []string{"ID", "NAME", "TYPE", "DESCRIPTION", "CATEGORY"} {
		if !strings.Contains(out, col) {
			t.Errorf("expected %s column, got:\n%s", col, out)
		}
	}
}

func TestPrintRaw_Table_BuildingsResource(t *testing.T) {
	f, buf := newTestFormatter("table")
	f.wide = false
	// Buildings resource — 6 keys <= 8 limit, all shown
	input := `[{"id":"1","name":"HQ Building","streetAddress1":"123 Main St","city":"Minneapolis","stateProvince":"MN","zipPostalCode":"55401"}]`
	err := f.PrintRaw([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()

	for _, col := range []string{"ID", "NAME", "STREETADDRESS1", "CITY", "STATEPROVINCE", "ZIPPOSTALCODE"} {
		if !strings.Contains(out, col) {
			t.Errorf("expected %s column, got:\n%s", col, out)
		}
	}
}

func TestPrintRaw_Table_OnlyIdName(t *testing.T) {
	f, buf := newTestFormatter("table")
	f.wide = false
	// Resource with only id and name - should still work
	input := `[{"id":"1","name":"Test Resource"}]`
	err := f.PrintRaw([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, "ID") || !strings.Contains(out, "NAME") {
		t.Errorf("expected ID and NAME columns, got:\n%s", out)
	}
}

func TestPrintRaw_Table_NoIdOrName(t *testing.T) {
	f, buf := newTestFormatter("table")
	f.wide = false
	// Resource without id or name - should fallback to all columns
	input := `[{"key":"abc123","value":"some data","timestamp":"2026-02-05"}]`
	err := f.PrintRaw([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()

	// Should show all columns as fallback
	for _, col := range []string{"KEY", "VALUE", "TIMESTAMP"} {
		if !strings.Contains(out, col) {
			t.Errorf("expected %s column in fallback mode, got:\n%s", col, out)
		}
	}
}

func TestPrintRaw_Table_StatusColumnExclusion(t *testing.T) {
	f, buf := newTestFormatter("table")
	f.wide = false
	// 4 keys <= 8 limit, all shown
	input := `[{"id":"1","name":"Test","mdmAccessRights":3,"userApprovedMdm":true}]`
	err := f.PrintRaw([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()

	for _, col := range []string{"ID", "NAME", "USERAPPROVEDMDM", "MDMACCESSRIGHTS"} {
		if !strings.Contains(out, col) {
			t.Errorf("expected %s column, got:\n%s", col, out)
		}
	}
}

func TestPrintRaw_Table_MultipleStatusColumns(t *testing.T) {
	f, buf := newTestFormatter("table")
	f.wide = false
	// 7 keys <= 8 limit, all shown
	input := `[{"id":"1","name":"Device","isManaged":true,"supervised":true,"enrollmentStatus":"complete","userApprovedMdm":true,"extra":"data"}]`
	err := f.PrintRaw([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()

	for _, col := range []string{"ID", "NAME", "ISMANAGED", "SUPERVISED", "ENROLLMENTSTATUS", "USERAPPROVEDMDM", "EXTRA"} {
		if !strings.Contains(out, col) {
			t.Errorf("expected %s column, got:\n%s", col, out)
		}
	}
}

// --- Date formatting tests ---

func TestIsDateColumn(t *testing.T) {
	tests := []struct {
		name   string
		expect bool
	}{
		{"lastContactDate", true},
		{"lastReportDate", true},
		{"lastEnrolledDate", true},
		{"createdAt", true},
		{"updatedAt", true},
		{"modifiedTime", true},
		{"timestamp", true},
		{"enrollmentTimestamp", true},
		{"name", false},
		{"status", false},
		{"serialNumber", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := isDateColumn(tc.name)
			if result != tc.expect {
				t.Errorf("isDateColumn(%q) = %v, want %v", tc.name, result, tc.expect)
			}
		})
	}
}

func TestFormatDateValue_Absolute(t *testing.T) {
	// Test absolute formatting (wide mode always uses absolute)
	tests := []struct {
		input    string
		expected string
	}{
		// ISO 8601 with milliseconds (12-hour format)
		{"2025-09-27T07:26:37.424Z", "Sep 27, 2025 7:26 AM"},
		// ISO 8601 without milliseconds
		{"2025-09-27T14:30:00Z", "Sep 27, 2025 2:30 PM"},
		// RFC3339
		{"2025-09-27T19:45:37+00:00", "Sep 27, 2025 7:45 PM"},
		// Date only (midnight shows date only)
		{"2025-09-27T00:00:00Z", "Sep 27, 2025"},
		// Just date
		{"2025-09-27", "Sep 27, 2025"},
		// Empty string
		{"", ""},
		// Non-date string (returned as-is)
		{"not-a-date", "not-a-date"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result, _ := formatDateValue(tc.input, true)
			if result != tc.expected {
				t.Errorf("formatDateValue(%q, wide) = %q, want %q", tc.input, result, tc.expected)
			}
		})
	}
}

func TestFormatDateValue_RelativeTimestamps(t *testing.T) {
	// Fix nowFunc for deterministic tests
	fixedNow := time.Date(2026, 2, 5, 12, 0, 0, 0, time.UTC)
	origNow := nowFunc
	nowFunc = func() time.Time { return fixedNow }
	defer func() { nowFunc = origNow }()

	tests := []struct {
		name     string
		offset   time.Duration
		expected string
	}{
		{"just now (30s)", 30 * time.Second, "just now"},
		{"minutes ago", 15 * time.Minute, "15m ago"},
		{"hours ago", 5 * time.Hour, "5h ago"},
		{"days ago", 3 * 24 * time.Hour, "3d ago"},
		{"weeks ago", 14 * 24 * time.Hour, "2w ago"},
		{"old date (absolute)", 60 * 24 * time.Hour, "Dec 07, 2025 12:00 PM"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ts := fixedNow.Add(-tc.offset).Format(time.RFC3339)
			result, _ := formatDateValue(ts, false)
			if result != tc.expected {
				t.Errorf("formatDateValue(%q, false) = %q, want %q", ts, result, tc.expected)
			}
		})
	}
}

func TestFormatDateValue_WideAlwaysAbsolute(t *testing.T) {
	fixedNow := time.Date(2026, 2, 5, 12, 0, 0, 0, time.UTC)
	origNow := nowFunc
	nowFunc = func() time.Time { return fixedNow }
	defer func() { nowFunc = origNow }()

	// A recent time (5 minutes ago) should still show absolute in wide mode
	ts := fixedNow.Add(-5 * time.Minute).Format(time.RFC3339)
	result, isRecent := formatDateValue(ts, true)
	if !isRecent {
		t.Error("expected recent time to be flagged as recent")
	}
	// Should be absolute, not "5m ago"
	if strings.Contains(result, "ago") || result == "just now" {
		t.Errorf("wide mode should show absolute date, got %q", result)
	}
	if !strings.Contains(result, "2026") {
		t.Errorf("expected absolute date with year, got %q", result)
	}
}

func TestFormatDateValue_RecentDetection(t *testing.T) {
	// Test that recent dates are detected
	now := time.Now().UTC()
	recentTime := now.Add(-1 * time.Hour).Format(time.RFC3339)
	oldTime := now.Add(-48 * time.Hour).Format(time.RFC3339)

	_, isRecent := formatDateValue(recentTime, false)
	if !isRecent {
		t.Errorf("expected time from 1 hour ago to be recent")
	}

	_, isRecent = formatDateValue(oldTime, false)
	if isRecent {
		t.Errorf("expected time from 48 hours ago to not be recent")
	}

	// Empty and invalid should not be recent
	_, isRecent = formatDateValue("", false)
	if isRecent {
		t.Errorf("expected empty string to not be recent")
	}

	_, isRecent = formatDateValue("not-a-date", false)
	if isRecent {
		t.Errorf("expected invalid date to not be recent")
	}
}

func TestPrintRaw_Table_DateFormatting(t *testing.T) {
	f, buf := newTestFormatter("table")
	f.wide = true
	input := `[{"id":"1","name":"Test","lastContactDate":"2025-09-27T07:26:37.424Z","createdAt":"2025-01-15T00:00:00Z"}]`
	err := f.PrintRaw([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()

	// Should show human-readable dates
	if !strings.Contains(out, "Sep 27, 2025") {
		t.Errorf("expected formatted date 'Sep 27, 2025', got:\n%s", out)
	}
	if !strings.Contains(out, "Jan 15, 2025") {
		t.Errorf("expected formatted date 'Jan 15, 2025', got:\n%s", out)
	}
	// Should NOT show ISO format
	if strings.Contains(out, "2025-09-27T") {
		t.Errorf("expected ISO date to be formatted, got:\n%s", out)
	}
}

// --- New constructor tests ---

func TestNew(t *testing.T) {
	f := New("json", true, false)
	if f.format != FormatJSON {
		t.Errorf("format = %q, want %q", f.format, FormatJSON)
	}
	if !f.noColor {
		t.Error("expected noColor=true")
	}
	if f.wide {
		t.Error("expected wide=false")
	}
	if f.writer == nil {
		t.Error("writer should default to non-nil (os.Stdout)")
	}
}

func TestNew_Wide(t *testing.T) {
	f := New("table", false, true)
	if f.format != FormatTable {
		t.Errorf("format = %q, want %q", f.format, FormatTable)
	}
	if f.noColor {
		t.Error("expected noColor=false")
	}
	if !f.wide {
		t.Error("expected wide=true")
	}
}

// --- SetWriter tests ---

func TestSetWriter(t *testing.T) {
	f := New("json", true, false)
	buf := &bytes.Buffer{}
	f.SetWriter(buf)

	data := []map[string]any{{"id": float64(1), "name": "test"}}
	if err := f.Print(data); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("expected output written to custom writer")
	}
	if !strings.Contains(buf.String(), "test") {
		t.Error("expected 'test' in output")
	}
}

// --- Print dispatcher tests ---

func TestPrint_JSON(t *testing.T) {
	f, buf := newTestFormatter("json")
	data := []map[string]any{{"id": float64(1)}}
	if err := f.Print(data); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should be valid JSON
	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
}

func TestPrint_YAML(t *testing.T) {
	f, buf := newTestFormatter("yaml")
	data := []map[string]any{{"id": float64(1), "name": "test"}}
	if err := f.Print(data); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "name: test") {
		t.Errorf("expected YAML output, got:\n%s", buf.String())
	}
}

func TestPrint_CSV(t *testing.T) {
	f, buf := newTestFormatter("csv")
	data := []map[string]any{{"id": float64(1), "name": "test"}}
	if err := f.Print(data); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected header + 1 row, got %d lines", len(lines))
	}
}

func TestPrint_CSV_Empty(t *testing.T) {
	f, buf := newTestFormatter("csv")
	data := []map[string]any{}
	if err := f.Print(data); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected empty output for empty data, got: %s", buf.String())
	}
}

func TestPrint_CSV_UnsupportedType(t *testing.T) {
	f, _ := newTestFormatter("csv")
	err := f.Print("not a slice of maps")
	if err == nil {
		t.Fatal("expected error for unsupported CSV type")
		return
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Errorf("expected 'not supported' in error, got: %v", err)
	}
}

func TestPrint_Plain_String(t *testing.T) {
	f, buf := newTestFormatter("plain")
	if err := f.Print("hello world"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(buf.String()) != "hello world" {
		t.Errorf("expected 'hello world', got: %s", buf.String())
	}
}

func TestPrint_Table_NonSlice(t *testing.T) {
	f, buf := newTestFormatter("table")
	if err := f.Print("simple string"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "simple string") {
		t.Errorf("expected fallback output, got: %s", buf.String())
	}
}

func TestPrint_Table_Empty(t *testing.T) {
	f, buf := newTestFormatter("table")
	data := []map[string]any{}
	if err := f.Print(data); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected empty output for empty table, got: %s", buf.String())
	}
}

// --- PrintError tests ---

func TestPrintError_JSON(t *testing.T) {
	f, buf := newTestFormatter("json")
	testErr := fmt.Errorf("something went wrong")
	details := map[string]any{"field": "name"}
	f.PrintError(testErr, "validation_error", details)

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nOutput: %s", err, buf.String())
	}
	if result["error"] != "validation_error" {
		t.Errorf("error = %v, want %q", result["error"], "validation_error")
	}
	if result["message"] != "something went wrong" {
		t.Errorf("message = %v, want %q", result["message"], "something went wrong")
	}
	if result["field"] != "name" {
		t.Errorf("field = %v, want %q", result["field"], "name")
	}
}

func TestPrintError_NonJSON(t *testing.T) {
	// PrintError in non-JSON mode writes to os.Stderr — just verify it doesn't panic
	f, _ := newTestFormatter("table")
	testErr := fmt.Errorf("something went wrong")
	f.PrintError(testErr, "general", nil)
	// No assertion needed — we're just ensuring it doesn't panic
}

// --- parseDate and relativeDate tests ---

func TestParseDate(t *testing.T) {
	tests := []struct {
		input string
		ok    bool
	}{
		{"2025-09-27T07:26:37.424Z", true},
		{"2025-09-27T14:30:00Z", true},
		{"2025-09-27T19:45:37+00:00", true},
		{"2025-09-27", true},
		{"not-a-date", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			_, ok := parseDate(tt.input)
			if ok != tt.ok {
				t.Errorf("parseDate(%q) ok = %v, want %v", tt.input, ok, tt.ok)
			}
		})
	}
}

func TestAbsoluteDate_MidnightOmitsTime(t *testing.T) {
	ts := time.Date(2025, 9, 27, 0, 0, 0, 0, time.UTC)
	got := absoluteDate(ts)
	if strings.Contains(got, "AM") || strings.Contains(got, "PM") {
		t.Errorf("midnight should omit time, got: %q", got)
	}
	if !strings.Contains(got, "Sep 27, 2025") {
		t.Errorf("expected 'Sep 27, 2025', got: %q", got)
	}
}

func TestAbsoluteDate_WithTime(t *testing.T) {
	ts := time.Date(2025, 9, 27, 14, 30, 0, 0, time.UTC)
	got := absoluteDate(ts)
	if !strings.Contains(got, "2:30 PM") {
		t.Errorf("expected '2:30 PM' in output, got: %q", got)
	}
}

func TestDateColumnsNotStatusColumns(t *testing.T) {
	// Date columns should not be considered status columns
	// even if they contain status-like words
	dateColumns := []string{
		"lastEnrolledDate",   // contains "enrolled"
		"activatedTimestamp", // contains "active"
		"connectedDate",      // contains "connected"
	}
	for _, col := range dateColumns {
		if isStatusColumn(col) {
			t.Errorf("date column %q should not be a status column", col)
		}
	}
}

// --- XML / PrintBytes / Format tests ---

func TestFormat_ReturnsConfiguredFormat(t *testing.T) {
	for _, fmt := range []string{"json", "xml", "raw", "table", "csv", "yaml"} {
		f, _ := newTestFormatter(fmt)
		if got := f.Format(); got != fmt {
			t.Errorf("Format() = %q, want %q", got, fmt)
		}
	}
}

func TestPrintBytes_PrettyPrintsXML(t *testing.T) {
	f, buf := newTestFormatter("json")
	input := []byte(`<account><id>1</id><name>admin</name></account>`)
	if err := f.PrintBytes(input); err != nil {
		t.Fatalf("PrintBytes() error: %v", err)
	}
	out := buf.String()
	// Should be indented
	if !strings.Contains(out, "  <id>") {
		t.Errorf("expected indented XML, got:\n%s", out)
	}
	// Should end with newline
	if !strings.HasSuffix(out, "\n") {
		t.Errorf("expected trailing newline, got %q", out)
	}
}

func TestPrintBytes_Raw_SkipsPrettyPrint(t *testing.T) {
	f, buf := newTestFormatter("raw")
	input := []byte(`<account><id>1</id></account>`)
	if err := f.PrintBytes(input); err != nil {
		t.Fatalf("PrintBytes() error: %v", err)
	}
	// Raw: exact bytes + newline (PrintBytes adds \n if missing)
	if !strings.Contains(buf.String(), `<account><id>1</id></account>`) {
		t.Errorf("raw mode should not reformat XML, got:\n%s", buf.String())
	}
	if strings.Contains(buf.String(), "  <id>") {
		t.Errorf("raw mode must not indent XML")
	}
}

func TestPrintBytes_NonXML_PassThrough(t *testing.T) {
	f, buf := newTestFormatter("json")
	input := []byte(`not xml at all`)
	if err := f.PrintBytes(input); err != nil {
		t.Fatalf("PrintBytes() error: %v", err)
	}
	if !strings.Contains(buf.String(), "not xml at all") {
		t.Errorf("non-XML should pass through unchanged, got: %s", buf.String())
	}
}

func TestPrintBytes_AddsTrailingNewline(t *testing.T) {
	f, buf := newTestFormatter("xml")
	// prettyXML already adds \n, but test the branch where data has no trailing \n
	input := []byte("plain text")
	if err := f.PrintBytes(input); err != nil {
		t.Fatalf("PrintBytes() error: %v", err)
	}
	if !strings.HasSuffix(buf.String(), "\n") {
		t.Errorf("PrintBytes should append newline when missing")
	}
}

func TestPrintRaw_XMLFormat_PrettyPrintsXML(t *testing.T) {
	f, buf := newTestFormatter("xml")
	input := []byte(`<policy><id>5</id></policy>`)
	if err := f.PrintRaw(input); err != nil {
		t.Fatalf("PrintRaw() error: %v", err)
	}
	if !strings.Contains(buf.String(), "  <id>") {
		t.Errorf("FormatXML should pretty-print XML via PrintRaw, got:\n%s", buf.String())
	}
}

func TestPrintRaw_RawFormat_ExactBytes(t *testing.T) {
	f, buf := newTestFormatter("raw")
	input := []byte(`{"id":1}`)
	if err := f.PrintRaw(input); err != nil {
		t.Fatalf("PrintRaw() error: %v", err)
	}
	if buf.String() != `{"id":1}` {
		t.Errorf("FormatRaw should write exact bytes, got: %q", buf.String())
	}
}

func TestResolveFormat(t *testing.T) {
	cases := []struct {
		name              string
		flagChanged       bool
		current           string
		configDefault     string
		isTTY, hasOutFile bool
		want              string
	}{
		{"explicit flag wins", true, "csv", "yaml", true, false, "csv"},
		{"config default when unset", false, "json", "yaml", true, false, "yaml"},
		{"tty -> table", false, "json", "", true, false, "table"},
		{"piped -> json", false, "json", "", false, false, "json"},
		{"out-file -> json even on tty", false, "json", "", true, true, "json"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ResolveFormat(c.flagChanged, c.current, c.configDefault, c.isTTY, c.hasOutFile)
			if got != c.want {
				t.Fatalf("ResolveFormat = %q, want %q", got, c.want)
			}
		})
	}
}

// --- NDJSON tests ---

func TestPrintNDJSON_List(t *testing.T) {
	var buf bytes.Buffer
	f := New("ndjson", true, false)
	f.SetWriter(&buf)
	rows := []map[string]any{{"id": "1", "name": "a"}, {"id": "2", "name": "b"}}
	if err := f.Print(rows); err != nil {
		t.Fatalf("Print: %v", err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), buf.String())
	}
	for _, ln := range lines {
		if strings.Contains(ln, "[") || strings.Contains(ln, "\n") {
			t.Errorf("line is not a bare compact object: %q", ln)
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(ln), &obj); err != nil {
			t.Errorf("line not valid JSON object: %q (%v)", ln, err)
		}
	}
}

func TestPrintNDJSON_SingleObject(t *testing.T) {
	var buf bytes.Buffer
	f := New("ndjson", true, false)
	f.SetWriter(&buf)
	if err := f.Print(map[string]any{"id": "1"}); err != nil {
		t.Fatalf("Print: %v", err)
	}
	if got := strings.Count(buf.String(), "\n"); got != 1 {
		t.Errorf("single object should be one line, got %d newlines: %q", got, buf.String())
	}
}

func TestPrintNDJSON_Empty(t *testing.T) {
	var buf bytes.Buffer
	f := New("ndjson", true, false)
	f.SetWriter(&buf)
	if err := f.Print([]map[string]any{}); err != nil {
		t.Fatalf("Print: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("empty list should produce no output, got %q", buf.String())
	}
}

func TestPrintNDJSON_NullNoOutput(t *testing.T) {
	// An empty paginated --all result marshals its nil accumulator to "null";
	// ndjson must emit nothing, not a literal "null" line.
	var buf bytes.Buffer
	f := New("ndjson", true, false)
	f.SetWriter(&buf)
	if err := f.PrintRaw([]byte("null")); err != nil {
		t.Fatalf("PrintRaw: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("top-level null should produce no output, got %q", buf.String())
	}
}

func TestPaginationProgress_QuietIsSilent(t *testing.T) {
	f, _, stderr := newTestFormatterWithStderr("json")
	f.SetQuiet(true)
	p := f.PaginationProgress()
	p.Update(10, 100)
	p.Stop()
	// quiet => Silent reporter => no stderr writes
	if stderr.Len() != 0 {
		t.Errorf("quiet should produce no progress output, got %q", stderr.String())
	}
}

func TestPaginationProgress_NilStderrNoPanic(t *testing.T) {
	orig := isStderrTTY
	isStderrTTY = func() bool { return false } // force Events mode
	defer func() { isStderrTTY = orig }()

	f := New("json", false, false) // stderr is nil — production path
	p := f.PaginationProgress()
	// must not panic writing to a nil underlying writer
	p.Update(1, 2)
	p.Stop()
}

// TestPaginationProgress_PipedStdoutStillInteractive guards the regression where
// piping stdout (which auto-sets noColor=true) wrongly forced Events mode even
// when stderr was an interactive terminal.
func TestPaginationProgress_PipedStdoutStillInteractive(t *testing.T) {
	orig := isStderrTTY
	isStderrTTY = func() bool { return true } // stderr is a terminal
	defer func() { isStderrTTY = orig }()

	f, _, stderr := newTestFormatterWithStderr("ndjson")
	// Simulate the piped-stdout state: noColor auto-set to true, but the user
	// never explicitly requested --no-color or NO_COLOR.
	f.noColor = true
	f.SetExplicitNoColor(false)

	p := f.PaginationProgress()
	p.Update(1, 2)
	p.Stop()

	out := stderr.String()
	if !strings.Contains(out, "Fetched 1 / 2") {
		t.Errorf("piped stdout + stderr TTY should use in-place counter; got %q", out)
	}
	if strings.Contains(out, `"event":"page_fetch"`) {
		t.Errorf("piped stdout + stderr TTY must not emit NDJSON events; got %q", out)
	}
}

// TestPaginationProgress_ExplicitNoColorUsesEvents ensures that --no-color or
// NO_COLOR explicitly set by the user switches progress to Events mode.
func TestPaginationProgress_ExplicitNoColorUsesEvents(t *testing.T) {
	orig := isStderrTTY
	isStderrTTY = func() bool { return true } // stderr is a terminal
	defer func() { isStderrTTY = orig }()

	f, _, stderr := newTestFormatterWithStderr("ndjson")
	f.noColor = true
	f.SetExplicitNoColor(true) // user explicitly passed --no-color / set NO_COLOR

	p := f.PaginationProgress()
	p.Update(1, 2)
	p.Stop()

	out := stderr.String()
	if !strings.Contains(out, `"event":"page_fetch"`) {
		t.Errorf("explicit --no-color should use Events mode; got %q", out)
	}
	if strings.Contains(out, "Fetched") {
		t.Errorf("explicit --no-color must not emit in-place counter; got %q", out)
	}
}

func TestPrintNDJSON_PrintRawArray(t *testing.T) {
	// The --all production path calls PrintRaw with a JSON array assembled from
	// all paginated pages. Each element must become a bare compact JSON line;
	// no outer "[" or "]" wrapper should appear.
	var buf bytes.Buffer
	f := New("ndjson", true, false)
	f.SetWriter(&buf)
	if err := f.PrintRaw([]byte(`[{"id":"1"},{"id":"2"},{"id":"3"}]`)); err != nil {
		t.Fatalf("PrintRaw: %v", err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d:\n%s", len(lines), buf.String())
	}
	for i, ln := range lines {
		if strings.Contains(ln, "[") || strings.Contains(ln, "]") {
			t.Errorf("line %d contains array bracket: %q", i, ln)
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(ln), &obj); err != nil {
			t.Errorf("line %d is not a valid JSON object: %q (%v)", i, ln, err)
		}
	}
}

func TestPrintNDJSON_SelectProjection(t *testing.T) {
	// ndjson must honour --select per record: projection runs in Print before
	// format dispatch, so each NDJSON line should contain only selected fields.
	var buf bytes.Buffer
	f := New("ndjson", true, false)
	f.SetWriter(&buf)
	f.SetProjector(Projector{Select: []string{"id"}})
	if err := f.PrintRaw([]byte(`[{"id":"1","name":"a"},{"id":"2","name":"b"}]`)); err != nil {
		t.Fatalf("PrintRaw: %v", err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d:\n%s", len(lines), buf.String())
	}
	for i, ln := range lines {
		var obj map[string]any
		if err := json.Unmarshal([]byte(ln), &obj); err != nil {
			t.Errorf("line %d not valid JSON: %q (%v)", i, ln, err)
			continue
		}
		if _, ok := obj["id"]; !ok {
			t.Errorf("line %d missing selected field 'id': %q", i, ln)
		}
		if _, ok := obj["name"]; ok {
			t.Errorf("line %d contains unselected field 'name': %q", i, ln)
		}
	}
}
