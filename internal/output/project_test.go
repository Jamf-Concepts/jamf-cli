// Copyright 2026, Jamf Software LLC

package output

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestProjector_IsZero(t *testing.T) {
	if !(Projector{}).IsZero() {
		t.Error("zero-value Projector should report IsZero")
	}
	if (Projector{Compact: true}).IsZero() {
		t.Error("Compact=true should not report IsZero")
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
