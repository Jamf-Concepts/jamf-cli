// Copyright 2026, Jamf Software LLC

package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestProjector_IsZero(t *testing.T) {
	if !(Projector{}).IsZero() {
		t.Error("zero-value Projector should report IsZero")
	}
	if (Projector{Compact: true}).IsZero() {
		t.Error("Compact=true should not report IsZero")
	}
	if (Projector{Select: []string{"id"}}).IsZero() {
		t.Error("Select set should not report IsZero")
	}
}

func TestProjector_Select_KeepsExactPaths(t *testing.T) {
	rows := []map[string]any{{
		"id":     1.0,
		"name":   "device-01",
		"serial": "ABC123",
		"udid":   "uuid-1",
	}}
	got := Projector{Select: []string{"id", "serial"}}.Apply(rows)
	if len(got[0]) != 2 {
		t.Errorf("expected 2 fields, got %d: %v", len(got[0]), got[0])
	}
	if got[0]["id"] != 1.0 || got[0]["serial"] != "ABC123" {
		t.Errorf("expected id and serial only, got %v", got[0])
	}
	if _, ok := got[0]["name"]; ok {
		t.Errorf("name should be dropped, got %v", got[0])
	}
}

func TestProjector_Select_PrefixMatch(t *testing.T) {
	// Multiple top-level sections so flattenRows keeps "general." prefix.
	rows := []map[string]any{{
		"id": 1.0,
		"general": map[string]any{
			"name":     "device-01",
			"platform": "Mac",
		},
		"location": map[string]any{
			"username": "alice",
		},
	}}
	got := Projector{Select: []string{"general"}}.Apply(rows)
	if got[0]["general.name"] != "device-01" {
		t.Errorf("expected general.name, got %v", got[0])
	}
	if got[0]["general.platform"] != "Mac" {
		t.Errorf("expected general.platform, got %v", got[0])
	}
	if _, ok := got[0]["location.username"]; ok {
		t.Errorf("location should be dropped, got %v", got[0])
	}
	if _, ok := got[0]["id"]; ok {
		t.Errorf("id was not selected, should be dropped, got %v", got[0])
	}
}

func TestProjector_Select_DotPath(t *testing.T) {
	rows := []map[string]any{{
		"id": 1.0,
		"general": map[string]any{
			"name":     "device-01",
			"platform": "Mac",
		},
		"location": map[string]any{
			"username": "alice",
		},
	}}
	got := Projector{Select: []string{"general.name", "id"}}.Apply(rows)
	if got[0]["general.name"] != "device-01" {
		t.Errorf("expected general.name, got %v", got[0])
	}
	if got[0]["id"] != 1.0 {
		t.Errorf("expected id, got %v", got[0])
	}
	if _, ok := got[0]["general.platform"]; ok {
		t.Errorf("general.platform was not selected, got %v", got[0])
	}
}

// Regression: a singleton GET shaped as {"general": {...}} (single
// top-level section) used to return empty rows because stripCommonPrefix
// rewrote "general.name" → "name" before projectSelect ran. The Select
// path now uses flattenRowsRaw (no stripping) so user-supplied dot paths
// like "general.name" continue to match in this shape.
func TestProjector_Select_SingleSection_KeepsDottedKeys(t *testing.T) {
	rows := []map[string]any{{
		"general": map[string]any{
			"name":     "MacBook",
			"id":       float64(123),
			"platform": "Mac",
		},
	}}
	got := Projector{Select: []string{"general.name"}}.Apply(rows)
	if got[0]["general.name"] != "MacBook" {
		t.Errorf("expected general.name=MacBook, got %v", got[0])
	}
	if _, ok := got[0]["general.id"]; ok {
		t.Errorf("general.id was not selected, got %v", got[0])
	}
	if _, ok := got[0]["name"]; ok {
		t.Errorf("Select must not strip prefix; got bare 'name': %v", got[0])
	}
}

// Selecting a parent path on a single-section response should still match
// every child via the prefix-match branch — used to return empty.
func TestProjector_Select_SingleSection_ParentPath(t *testing.T) {
	rows := []map[string]any{{
		"general": map[string]any{
			"name":     "MacBook",
			"platform": "Mac",
		},
	}}
	got := Projector{Select: []string{"general"}}.Apply(rows)
	if got[0]["general.name"] != "MacBook" {
		t.Errorf("expected general.name=MacBook, got %v", got[0])
	}
	if got[0]["general.platform"] != "Mac" {
		t.Errorf("expected general.platform=Mac, got %v", got[0])
	}
}

func TestProjector_Select_MissingPath_OmitsSilently(t *testing.T) {
	rows := []map[string]any{{"id": 1.0, "name": "a"}}
	got := Projector{Select: []string{"id", "nonexistent"}}.Apply(rows)
	if got[0]["id"] != 1.0 {
		t.Errorf("expected id, got %v", got[0])
	}
	if _, ok := got[0]["nonexistent"]; ok {
		t.Errorf("missing path should be omitted, got %v", got[0])
	}
}

func TestProjector_Select_EmptyAfterTrim_ReturnsRows(t *testing.T) {
	rows := []map[string]any{{"id": 1.0, "name": "a"}}
	got := Projector{Select: []string{"  ", ""}}.Apply(rows)
	if len(got) != 1 || got[0]["id"] != 1.0 || got[0]["name"] != "a" {
		t.Errorf("all-whitespace select should pass through, got %v", got)
	}
}

func TestProjector_SelectAndCompact_SelectWins(t *testing.T) {
	rows := []map[string]any{{
		"id":       1.0,
		"name":     "a",
		"profiles": []any{"p1"},
	}}
	got := Projector{Compact: true, Select: []string{"profiles"}}.Apply(rows)
	if _, ok := got[0]["profiles"]; !ok {
		t.Errorf("Select should override Compact and keep profiles, got %v", got[0])
	}
	if _, ok := got[0]["id"]; ok {
		t.Errorf("Select should drop unselected fields, got %v", got[0])
	}
}

func TestProjector_Apply_NoOp(t *testing.T) {
	rows := []map[string]any{{"id": 1.0, "name": "a"}}
	got := Projector{}.Apply(rows)
	if len(got) != 1 || got[0]["id"] != 1.0 || got[0]["name"] != "a" {
		t.Errorf("zero projector should return rows unchanged, got %v", got)
	}
}

func TestProjector_Apply_Empty(t *testing.T) {
	got := Projector{Compact: true}.Apply(nil)
	if got != nil {
		t.Errorf("nil rows should pass through, got %v", got)
	}
}

func TestProjector_Compact_DropsArrays(t *testing.T) {
	rows := []map[string]any{{
		"id":       1.0,
		"name":     "device-01",
		"profiles": []any{"a", "b", "c"},
		"groups":   []any{},
	}}
	got := Projector{Compact: true}.Apply(rows)
	if _, ok := got[0]["profiles"]; ok {
		t.Errorf("compact should drop array fields, got %v", got[0])
	}
	if got[0]["id"] != 1.0 || got[0]["name"] != "device-01" {
		t.Errorf("compact should keep scalars, got %v", got[0])
	}
}

func TestProjector_Compact_FlattensNested(t *testing.T) {
	rows := []map[string]any{{
		"id": 1.0,
		"general": map[string]any{
			"name":     "device-01",
			"platform": "Mac",
		},
	}}
	got := Projector{Compact: true}.Apply(rows)
	// Single-section nested object: only "general.*" keys, so
	// stripCommonPrefix collapses to "name", "platform".
	if got[0]["name"] != "device-01" {
		t.Errorf("expected flattened name=device-01, got %v", got[0])
	}
	if got[0]["platform"] != "Mac" {
		t.Errorf("expected flattened platform=Mac, got %v", got[0])
	}
	if _, ok := got[0]["general"]; ok {
		t.Errorf("compact should not retain nested object key, got %v", got[0])
	}
}

func TestProjector_Compact_DropsNil(t *testing.T) {
	rows := []map[string]any{{
		"id":   1.0,
		"name": "device-01",
		// Top-level nil — without the fix this survived compact, while
		// nested nils were dropped by flattenMap. Now both paths agree.
		"description": nil,
	}}
	got := Projector{Compact: true}.Apply(rows)
	if _, ok := got[0]["description"]; ok {
		t.Errorf("compact should drop nil fields, got %v", got[0])
	}
	if got[0]["id"] != 1.0 || got[0]["name"] != "device-01" {
		t.Errorf("compact should keep non-nil scalars, got %v", got[0])
	}
}

func TestProjector_Compact_PreservesScalarTypes(t *testing.T) {
	rows := []map[string]any{{
		"id":      1.0,
		"managed": true,
		"version": "10.15",
	}}
	got := Projector{Compact: true}.Apply(rows)
	if got[0]["managed"] != true {
		t.Errorf("expected managed=true, got %v", got[0]["managed"])
	}
	if got[0]["id"] != 1.0 {
		t.Errorf("expected id=1.0, got %v", got[0]["id"])
	}
	if got[0]["version"] != "10.15" {
		t.Errorf("expected version=10.15, got %v", got[0]["version"])
	}
}

// Formatter integration: PrintRaw with --compact on JSON should drop arrays
// in the actual output, not just in the parsed projection.
func TestFormatter_PrintRaw_JSON_Compact(t *testing.T) {
	buf := &bytes.Buffer{}
	f := &Formatter{
		format:    FormatJSON,
		writer:    buf,
		projector: Projector{Compact: true},
	}

	input := `{"id":1,"name":"d1","profiles":["a","b"]}`
	if err := f.PrintRaw([]byte(input)); err != nil {
		t.Fatalf("PrintRaw failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("output not valid JSON: %v\nOutput: %s", err, buf.String())
	}
	if _, ok := out["profiles"]; ok {
		t.Errorf("compact should drop profiles array, got %v", out)
	}
	if out["id"] != 1.0 || out["name"] != "d1" {
		t.Errorf("compact should keep id/name, got %v", out)
	}
}

func TestFormatter_PrintRaw_JSON_NoProjector_Unchanged(t *testing.T) {
	buf := &bytes.Buffer{}
	f := &Formatter{
		format: FormatJSON,
		writer: buf,
	}
	input := `{"id":1,"profiles":["a"]}`
	if err := f.PrintRaw([]byte(input)); err != nil {
		t.Fatalf("PrintRaw failed: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("output not valid JSON: %v", err)
	}
	if _, ok := out["profiles"]; !ok {
		t.Errorf("without projector, profiles should be retained, got %v", out)
	}
}

// Array input with --compact should drop arrays from each row.
func TestFormatter_PrintRaw_JSON_Compact_Array(t *testing.T) {
	buf := &bytes.Buffer{}
	f := &Formatter{
		format:    FormatJSON,
		writer:    buf,
		projector: Projector{Compact: true},
	}

	input := `[{"id":1,"profiles":["a"]},{"id":2,"profiles":["b","c"]}]`
	if err := f.PrintRaw([]byte(input)); err != nil {
		t.Fatalf("PrintRaw failed: %v", err)
	}

	var out []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("output not valid JSON array: %v\nOutput: %s", err, buf.String())
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(out))
	}
	for i, row := range out {
		if _, ok := row["profiles"]; ok {
			t.Errorf("row %d should drop profiles, got %v", i, row)
		}
		if _, ok := row["id"]; !ok {
			t.Errorf("row %d should keep id, got %v", i, row)
		}
	}
}

// Every output format runs through the same applyProjection hook in Print,
// so --compact should drop arrays from rendered bytes regardless of format.
func TestFormatter_Print_Compact_AllFormats(t *testing.T) {
	rows := []map[string]any{{
		"id":       1.0,
		"name":     "device-01",
		"profiles": []any{"a", "b"},
	}}

	cases := []struct {
		format Format
		// "name" is the column to confirm a row rendered; absence of
		// "profiles" confirms the array was dropped.
		mustContain    string
		mustNotContain string
	}{
		{FormatTable, "device-01", "profiles"},
		{FormatCSV, "device-01", "profiles"},
		{FormatYAML, "device-01", "profiles"},
		{FormatPlain, "device-01", "profiles"},
	}

	for _, tc := range cases {
		t.Run(string(tc.format), func(t *testing.T) {
			buf := &bytes.Buffer{}
			f := &Formatter{
				format:    tc.format,
				writer:    buf,
				noColor:   true,
				projector: Projector{Compact: true},
			}
			if err := f.Print(rows); err != nil {
				t.Fatalf("Print failed: %v", err)
			}
			out := buf.String()
			if !strings.Contains(out, tc.mustContain) {
				t.Errorf("%s: expected output to contain %q, got %q", tc.format, tc.mustContain, out)
			}
			if strings.Contains(out, tc.mustNotContain) {
				t.Errorf("%s: compact should drop %q, got %q", tc.format, tc.mustNotContain, out)
			}
		})
	}
}

// TestDropEmptySelectionsAgreesWithApply holds the two halves of the Select
// projection to one answer. They each stated the input pipeline once and
// diverged: Apply flattened the rows first, the guard did not, so a nested dot
// path matched nothing in the guard and everything in the renderer. The guard
// won, and a whole report was suppressed at exit 0.
//
// The some-rows-match row is the one that matters most, and the all-or-nothing
// guard this replaced could not express it: a table and a CSV take their column
// set from row 0, so keeping a row the select emptied discarded every matched
// value in every later row.
func TestDropEmptySelectionsAgreesWithApply(t *testing.T) {
	nested := []map[string]any{{
		"summary": map[string]any{"total_errors": 3, "total_ok": 9},
	}}
	// Heterogeneous, and row 0 is the one that misses — the shape that lost
	// 1375 values on `commands -o csv --select api`.
	mixed := []map[string]any{
		{"command": "agent-context"},
		{"command": "pro categories list", "api": "pro"},
		{"command": "pro classic-sites list", "api": "pro-classic"},
	}

	for _, tc := range []struct {
		name     string
		rows     []map[string]any
		sel      []string
		wantKept int
		wantDrop int
	}{
		{"nested path that exists", nested, []string{"summary.total_errors"}, 1, 0},
		{"nested parent that exists", nested, []string{"summary"}, 1, 0},
		{"nested path that does not", nested, []string{"summary.nosuch"}, 0, 1},
		{"top-level path that does not", nested, []string{"nosuch"}, 0, 1},
		{"flat path that exists", []map[string]any{{"id": "1"}}, []string{"id"}, 1, 0},
		{"some rows match, row 0 does not", mixed, []string{"api"}, 2, 1},
		{"every row matches", mixed, []string{"command"}, 3, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := Projector{Select: tc.sel}
			kept, dropped := p.DropEmptySelections(tc.rows)
			if len(kept) != tc.wantKept || dropped != tc.wantDrop {
				t.Errorf("DropEmptySelections = (%d kept, %d dropped), want (%d, %d)", len(kept), dropped, tc.wantKept, tc.wantDrop)
			}

			// The invariant: a row survives exactly when Apply gives it fields.
			// Anything else drops data the renderer would have shown, or keeps
			// a row that empties a table's column set.
			for _, row := range p.Apply(kept) {
				if len(row) == 0 {
					t.Errorf("a surviving row projects to nothing, so it can still empty a table's column set: %v", kept)
				}
			}
			if dropped+len(kept) != len(tc.rows) {
				t.Errorf("%d kept + %d dropped != %d input rows", len(kept), dropped, len(tc.rows))
			}
		})
	}
}
