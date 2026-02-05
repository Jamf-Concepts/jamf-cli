package output

import (
	"bytes"
	"encoding/json"
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
	var result map[string]interface{}
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
	var result []map[string]interface{}
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
	// New format: summary + blank + header + separator + data row
	if !strings.Contains(out, "(1 total)") {
		t.Errorf("expected summary header with count, got:\n%s", out)
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
	// Nested objects should be rendered as compact JSON
	if !strings.Contains(out, `{"cpu":"M1","ram":"16GB"}`) {
		t.Errorf("expected compact JSON for nested object, got:\n%s", out)
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

// --- formatValue tests ---

func TestFormatValue(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected string
	}{
		{"nil", nil, ""},
		{"string", "hello", "hello"},
		{"integer float", float64(42), "42"},
		{"decimal float", float64(3.14), "3.14"},
		{"bool true", true, "true"},
		{"bool false", false, "false"},
		{"nested map", map[string]interface{}{"a": "b"}, `{"a":"b"}`},
		{"nested slice", []interface{}{"a", "b"}, `["a","b"]`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := formatValue(tc.input)
			if result != tc.expected {
				t.Errorf("formatValue(%v) = %q, want %q", tc.input, result, tc.expected)
			}
		})
	}
}

// --- sortedKeys tests ---

func TestSortedKeys(t *testing.T) {
	m := map[string]interface{}{
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

func TestSortedKeys_NoIdNoName(t *testing.T) {
	m := map[string]interface{}{
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

// --- normalizeJSON tests ---

func TestNormalizeJSON_SliceOfMaps(t *testing.T) {
	input := []interface{}{
		map[string]interface{}{"id": float64(1)},
		map[string]interface{}{"id": float64(2)},
	}
	result := normalizeJSON(input)
	slice, ok := result.([]map[string]interface{})
	if !ok {
		t.Fatalf("expected []map[string]interface{}, got %T", result)
	}
	if len(slice) != 2 {
		t.Errorf("expected 2 items, got %d", len(slice))
	}
}

func TestNormalizeJSON_SingleMap(t *testing.T) {
	input := map[string]interface{}{"id": float64(1), "name": "test"}
	result := normalizeJSON(input)
	slice, ok := result.([]map[string]interface{})
	if !ok {
		t.Fatalf("expected []map[string]interface{}, got %T", result)
	}
	if len(slice) != 1 {
		t.Errorf("expected 1 item, got %d", len(slice))
	}
}

func TestNormalizeJSON_ScalarString(t *testing.T) {
	input := "hello"
	result := normalizeJSON(input)
	s, ok := result.(string)
	if !ok {
		t.Fatalf("expected string, got %T", result)
	}
	if s != "hello" {
		t.Errorf("expected 'hello', got %q", s)
	}
}

func TestNormalizeJSON_MixedArray(t *testing.T) {
	// Mixed array should return raw data, not drop non-map items
	input := []interface{}{
		map[string]interface{}{"id": "1"},
		"stray string",
		map[string]interface{}{"id": "2"},
	}
	result := normalizeJSON(input)
	// Should return the original data unchanged (not []map[string]interface{})
	arr, ok := result.([]interface{})
	if !ok {
		t.Fatalf("expected []interface{}, got %T", result)
	}
	if len(arr) != 3 {
		t.Errorf("expected 3 items, got %d", len(arr))
	}
}

func TestNormalizeJSON_EmptySlice(t *testing.T) {
	input := []interface{}{}
	result := normalizeJSON(input)
	slice, ok := result.([]map[string]interface{})
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
		{"mdmCapable", true},
		{"remoteDesktop", true},
		{"supervised", true},
		{"name", false},
		{"id", false},
		{"description", false},
		{"mdmAccessRights", false}, // excluded - numeric value, not a status
		{"stateProvince", false},   // excluded - address field, not a status
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
	// Should show id, name, status (default columns)
	if !strings.Contains(out, "ID") {
		t.Error("expected ID column")
	}
	if !strings.Contains(out, "NAME") {
		t.Error("expected NAME column")
	}
	if !strings.Contains(out, "STATUS") {
		t.Error("expected STATUS column")
	}
	// Should NOT show serial or extra
	if strings.Contains(out, "SERIAL") {
		t.Error("expected SERIAL column to be hidden in default mode")
	}
	if strings.Contains(out, "EXTRA") {
		t.Error("expected EXTRA column to be hidden in default mode")
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
	result := defaultColumns(allKeys)

	// Should contain id, name, status only
	expected := []string{"id", "name", "status"}
	if len(result) != len(expected) {
		t.Fatalf("expected %d columns, got %d: %v", len(expected), len(result), result)
	}
	for i, exp := range expected {
		if result[i] != exp {
			t.Errorf("column[%d] = %q, want %q", i, result[i], exp)
		}
	}
}

func TestDefaultColumns_NoStatusColumns(t *testing.T) {
	allKeys := []string{"id", "name", "serial", "extra"}
	result := defaultColumns(allKeys)

	// Should contain only id and name
	if len(result) != 2 {
		t.Fatalf("expected 2 columns, got %d: %v", len(result), result)
	}
	if result[0] != "id" || result[1] != "name" {
		t.Errorf("expected [id, name], got %v", result)
	}
}

func TestDefaultColumns_NoDefaultColumns(t *testing.T) {
	allKeys := []string{"serial", "extra", "zebra"}
	result := defaultColumns(allKeys)

	// When no id/name/status columns exist, result should be empty
	// printTable will fallback to showing all columns in this case
	if len(result) != 0 {
		t.Fatalf("expected 0 columns when no default columns exist, got %d: %v", len(result), result)
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

func TestIsImportantColumn(t *testing.T) {
	tests := []struct {
		name   string
		expect bool
	}{
		// Identifiers
		{"serialNumber", true},
		{"SerialNumber", true},
		{"SERIALNUMBER", true},
		// Computer fields
		{"lastContactDate", true},
		{"lastReportDate", true},
		{"operatingSystemVersion", true},
		{"jamfBinaryVersion", true},
		{"JamfBinaryVersion", true},
		// Mobile device fields
		{"lastInventoryUpdateTimestamp", true},
		{"osVersion", true},
		{"type", true},
		{"model", true},
		// Generic
		{"version", true},
		// Should NOT match
		{"name", false},
		{"id", false},
		{"description", false},
		{"serial", false}, // must be exact match
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := isImportantColumn(tc.name)
			if result != tc.expect {
				t.Errorf("isImportantColumn(%q) = %v, want %v", tc.name, result, tc.expect)
			}
		})
	}
}

func TestDefaultColumns_WithImportantColumns(t *testing.T) {
	allKeys := []string{"id", "name", "serialNumber", "lastContactDate", "udid", "extra", "status"}
	result := defaultColumns(allKeys)

	// Should contain id, name, serialNumber, lastContactDate, status
	// (udid and extra should be filtered out)
	expected := map[string]bool{
		"id":              true,
		"name":            true,
		"serialNumber":    true,
		"lastContactDate": true,
		"status":          true,
	}

	if len(result) != len(expected) {
		t.Fatalf("expected %d columns, got %d: %v", len(expected), len(result), result)
	}

	for _, col := range result {
		if !expected[col] {
			t.Errorf("unexpected column %q in result: %v", col, result)
		}
	}
}

func TestPrintRaw_Table_ComputerColumns(t *testing.T) {
	f, buf := newTestFormatter("table")
	f.wide = false
	// Simulated computer list response
	input := `[{"id":"1","name":"MacBook Pro","serialNumber":"C02X1234","lastContactDate":"2026-02-05","lastReportDate":"2026-02-04","udid":"ABC-123","macAddress":"AA:BB:CC","isManaged":true}]`
	err := f.PrintRaw([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()

	// Should show important computer columns
	for _, col := range []string{"ID", "NAME", "SERIALNUMBER", "LASTCONTACTDATE", "LASTREPORTDATE", "ISMANAGED"} {
		if !strings.Contains(out, col) {
			t.Errorf("expected %s column in default computer output, got:\n%s", col, out)
		}
	}

	// Should NOT show non-important columns
	for _, col := range []string{"UDID", "MACADDRESS"} {
		if strings.Contains(out, col) {
			t.Errorf("expected %s column to be hidden in default mode, got:\n%s", col, out)
		}
	}
}

func TestPrintRaw_Table_MobileDeviceColumns(t *testing.T) {
	f, buf := newTestFormatter("table")
	f.wide = false
	// Simulated mobile device list response (based on MobileDeviceV2 schema)
	input := `[{"id":"1","name":"iPad Pro","serialNumber":"DMQVGC0DHLA0","type":"ios","model":"iPad Pro 11-inch","wifiMacAddress":"C4:84:66:92:78:00","udid":"0dad565fb40b010a9e490440188063a378721069","username":"admin"}]`
	err := f.PrintRaw([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()

	// Should show important mobile device columns
	for _, col := range []string{"ID", "NAME", "SERIALNUMBER", "TYPE", "MODEL"} {
		if !strings.Contains(out, col) {
			t.Errorf("expected %s column in default mobile device output, got:\n%s", col, out)
		}
	}

	// Should NOT show non-important columns
	for _, col := range []string{"WIFIMACADDRESS", "UDID", "USERNAME"} {
		if strings.Contains(out, col) {
			t.Errorf("expected %s column to be hidden in default mode, got:\n%s", col, out)
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
	// Generic resource that happens to have a "type" field (e.g., policies, scripts)
	input := `[{"id":"1","name":"Install Chrome","type":"policy","description":"Installs Chrome browser","category":"Software"}]`
	err := f.PrintRaw([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()

	// Type should show since it's an important column
	if !strings.Contains(out, "TYPE") {
		t.Errorf("expected TYPE column for generic resource, got:\n%s", out)
	}

	// Description and category should be hidden
	for _, col := range []string{"DESCRIPTION", "CATEGORY"} {
		if strings.Contains(out, col) {
			t.Errorf("expected %s column to be hidden in default mode, got:\n%s", col, out)
		}
	}
}

func TestPrintRaw_Table_BuildingsResource(t *testing.T) {
	f, buf := newTestFormatter("table")
	f.wide = false
	// Buildings resource - verify it doesn't break with different column set
	input := `[{"id":"1","name":"HQ Building","streetAddress1":"123 Main St","city":"Minneapolis","stateProvince":"MN","zipPostalCode":"55401"}]`
	err := f.PrintRaw([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()

	// Should show id and name
	if !strings.Contains(out, "ID") || !strings.Contains(out, "NAME") {
		t.Errorf("expected ID and NAME columns, got:\n%s", out)
	}

	// Address details should be hidden in default mode
	for _, col := range []string{"STREETADDRESS1", "CITY", "STATEPROVINCE", "ZIPPOSTALCODE"} {
		if strings.Contains(out, col) {
			t.Errorf("expected %s column to be hidden in default mode, got:\n%s", col, out)
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
	// Verify mdmAccessRights is excluded despite matching "mdm" pattern
	input := `[{"id":"1","name":"Test","mdmAccessRights":3,"userApprovedMdm":true}]`
	err := f.PrintRaw([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()

	// userApprovedMdm should show (status column)
	if !strings.Contains(out, "USERAPPROVEDMDM") {
		t.Errorf("expected USERAPPROVEDMDM column, got:\n%s", out)
	}

	// mdmAccessRights should be hidden (excluded)
	if strings.Contains(out, "MDMACCESSRIGHTS") {
		t.Errorf("expected MDMACCESSRIGHTS to be hidden, got:\n%s", out)
	}
}

func TestPrintRaw_Table_MultipleStatusColumns(t *testing.T) {
	f, buf := newTestFormatter("table")
	f.wide = false
	// Device with multiple status columns
	input := `[{"id":"1","name":"Device","isManaged":true,"supervised":true,"enrollmentStatus":"complete","userApprovedMdm":true,"extra":"data"}]`
	err := f.PrintRaw([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()

	// All status columns should show
	for _, col := range []string{"ISMANAGED", "SUPERVISED", "ENROLLMENTSTATUS", "USERAPPROVEDMDM"} {
		if !strings.Contains(out, col) {
			t.Errorf("expected %s status column, got:\n%s", col, out)
		}
	}

	// Extra should be hidden
	if strings.Contains(out, "EXTRA") {
		t.Errorf("expected EXTRA to be hidden, got:\n%s", out)
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

func TestFormatDateValue(t *testing.T) {
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
			result, _ := formatDateValue(tc.input)
			if result != tc.expected {
				t.Errorf("formatDateValue(%q) = %q, want %q", tc.input, result, tc.expected)
			}
		})
	}
}

func TestFormatDateValue_RecentDetection(t *testing.T) {
	// Test that recent dates are detected
	now := time.Now().UTC()
	recentTime := now.Add(-1 * time.Hour).Format(time.RFC3339)
	oldTime := now.Add(-48 * time.Hour).Format(time.RFC3339)

	_, isRecent := formatDateValue(recentTime)
	if !isRecent {
		t.Errorf("expected time from 1 hour ago to be recent")
	}

	_, isRecent = formatDateValue(oldTime)
	if isRecent {
		t.Errorf("expected time from 48 hours ago to not be recent")
	}

	// Empty and invalid should not be recent
	_, isRecent = formatDateValue("")
	if isRecent {
		t.Errorf("expected empty string to not be recent")
	}

	_, isRecent = formatDateValue("not-a-date")
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

