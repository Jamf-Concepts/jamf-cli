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

// TestSelectUnionsTheRenderedColumnSet asserts the RENDERED columns, which is a
// different question from whether the projector agrees with Apply.
//
// A row survives projection when it matched SOME selected path, never every
// one of them, so the survivors are heterogeneous by design. With row 0
// deciding the column set, a path row 0 does not carry was a column for no row:
// `commands -o csv --select command,api` wrote a `command`-only header while
// `-o json` returned 1375 `api` values, at exit 0 with nothing on either
// stream — a row-drop count cannot see a row that matched something.
//
// "Correct by construction" held only at ONE path, where "matched something"
// and "matched every path" coincide. The commit that claimed it measured
// `--select api`, which is that case.
func TestSelectUnionsTheRenderedColumnSet(t *testing.T) {
	// Row 0 carries only path A; a later row carries only path B.
	rows := []map[string]any{
		{"command": "agent-context"},
		{"command": "pro categories list", "api": "pro"},
	}

	for _, format := range []string{"table", "csv"} {
		t.Run(format, func(t *testing.T) {
			var buf bytes.Buffer
			f := New(format, true, false)
			f.SetWriter(&buf)
			f.SetProjector(Projector{Select: []string{"command", "api"}})
			if err := f.Print(rows); err != nil {
				t.Fatalf("Print: %v", err)
			}
			out := strings.ToUpper(buf.String())
			for _, want := range []string{"COMMAND", "API"} {
				if !strings.Contains(out, want) {
					t.Errorf("-o %s dropped the %s column, so a selected value reaches no row:\n%s", format, want, buf.String())
				}
			}
			if !strings.Contains(buf.String(), "pro") {
				t.Errorf("-o %s rendered no api value:\n%s", format, buf.String())
			}
		})
	}
}

// TestNoSelectKeepsRowZeroAsTheColumnSet pins the other half. Unioning
// unconditionally would change every table in the CLI, which is why CLAUDE.md
// rules it out; the union is gated on the projector so an unprojected table is
// byte-identical.
func TestNoSelectKeepsRowZeroAsTheColumnSet(t *testing.T) {
	rows := []map[string]any{
		{"name": "a"},
		{"name": "b", "extra": "x"},
	}
	var buf bytes.Buffer
	f := New("csv", true, false)
	f.SetWriter(&buf)
	if err := f.Print(rows); err != nil {
		t.Fatalf("Print: %v", err)
	}
	header := strings.SplitN(buf.String(), "\n", 2)[0]
	if header != "name" {
		t.Errorf("header = %q, want %q — row 0 must still decide without --select", header, "name")
	}
}

// TestIsMachineRenderedCoversEveryFormat holds the predicate to Print's own
// switch, which is the authority: a format Print has no case for reaches
// printTable through the default arm and therefore DOES take a section banner.
//
// A caller that hand-wrote this set got it wrong in both directions — xml and
// raw were called structured, suppressing the banner above the tables they
// actually render, while csv was omitted and box-drawing lines went into a CSV
// file where csv.reader yields a one-field row.
//
// Every constant is listed, so adding one to internal/output without deciding
// its answer fails here rather than silently defaulting.
func TestIsMachineRenderedCoversEveryFormat(t *testing.T) {
	want := map[Format]bool{
		FormatJSON:      true,
		FormatJSONMulti: true,
		FormatNDJSON:    true,
		FormatYAML:      true,
		FormatCSV:       true,
		FormatPlain:     true,
		// Print has no case for these two: they render as a table, so they
		// take a banner like any other table.
		FormatXML:   false,
		FormatRaw:   false,
		FormatTable: false,
	}
	for format, expected := range want {
		t.Run(string(format), func(t *testing.T) {
			if got := IsMachineRendered(format); got != expected {
				t.Errorf("IsMachineRendered(%q) = %v, want %v", format, got, expected)
			}
		})
	}

	// The count is the vacuity guard: a new Format constant must be added
	// above, with its answer decided, rather than inheriting a default.
	if len(want) != len(allFormatsForTest()) {
		t.Errorf("the table covers %d formats but internal/output declares %d — decide the new one's answer", len(want), len(allFormatsForTest()))
	}
}

// allFormatsForTest is every Format constant the package declares.
func allFormatsForTest() []Format {
	return []Format{
		FormatJSON, FormatJSONMulti, FormatNDJSON, FormatYAML,
		FormatCSV, FormatPlain, FormatXML, FormatRaw, FormatTable,
	}
}

// TestProjectionMatchingNothingRendersNothing covers the defect this whole area
// started from: a projection that matches no field in any row left every row
// empty, and printTable still wrote "RESULTS (N total)" above a blank header
// while printCSV wrote an empty header plus one empty line per row.
//
// Nothing is the honest answer, and it is a renderer decision rather than a
// caller one — which is why three rounds of caller-side guards, and a row
// filter built on top of them, kept producing new defects one arm over.
func TestProjectionMatchingNothingRendersNothing(t *testing.T) {
	rows := []map[string]any{{"id": "1"}, {"id": "2"}}
	for _, format := range []string{"table", "csv"} {
		t.Run(format, func(t *testing.T) {
			var buf bytes.Buffer
			f := New(format, true, false)
			f.SetWriter(&buf)
			f.SetProjector(Projector{Select: []string{"nosuchfield"}})
			if err := f.Print(rows); err != nil {
				t.Fatalf("Print: %v", err)
			}
			if buf.Len() != 0 {
				t.Errorf("-o %s rendered %q over no columns", format, buf.String())
			}
		})
	}
}

// TestCompactUnionsTheColumnSet is --select's sibling. projectCompact keeps a
// key in the rows that carry a value for it and drops it from the rest, so it
// makes rows heterogeneous exactly as --select does — and gating the union on
// Select alone let --compact delete a whole column from a table.
func TestCompactUnionsTheColumnSet(t *testing.T) {
	// Row 0 has no "note"; row 1 does. Compact drops empty values.
	rows := []map[string]any{
		{"id": "1", "name": "a", "note": ""},
		{"id": "2", "name": "b", "note": "seen"},
	}
	var buf bytes.Buffer
	f := New("csv", true, false)
	f.SetWriter(&buf)
	f.SetProjector(Projector{Compact: true})
	if err := f.Print(rows); err != nil {
		t.Fatalf("Print: %v", err)
	}
	if !strings.Contains(buf.String(), "note") {
		t.Errorf("--compact dropped the note column, so row 1's value reaches nothing:\n%s", buf.String())
	}
}
