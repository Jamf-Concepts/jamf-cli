// Copyright 2026, Jamf Software LLC

package output

import (
	"bytes"
	"strings"
	"testing"
)

func newTestFormatterWithStderr(format string) (*Formatter, *bytes.Buffer, *bytes.Buffer) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	f := &Formatter{
		format:  Format(format),
		writer:  stdout,
		stderr:  stderr,
		noColor: true,
	}
	return f, stdout, stderr
}

// makeRows builds n minimal rows for hint-threshold tests.
func makeRows(n int) []map[string]any {
	rows := make([]map[string]any, n)
	for i := range rows {
		rows[i] = map[string]any{"id": float64(i), "name": "row"}
	}
	return rows
}

func TestListHint_FiresAboveThresholdJSON(t *testing.T) {
	f, _, stderr := newTestFormatterWithStderr("json")
	rows := makeRows(listHintThreshold)
	if err := f.Print(rows); err != nil {
		t.Fatalf("Print failed: %v", err)
	}
	if !strings.Contains(stderr.String(), "results returned") {
		t.Errorf("expected hint on stderr, got %q", stderr.String())
	}
}

func TestListHint_SilentBelowThreshold(t *testing.T) {
	f, _, stderr := newTestFormatterWithStderr("json")
	rows := makeRows(listHintThreshold - 1)
	if err := f.Print(rows); err != nil {
		t.Fatalf("Print failed: %v", err)
	}
	if stderr.Len() != 0 {
		t.Errorf("hint should not fire under threshold, got stderr=%q", stderr.String())
	}
}

func TestListHint_SuppressedByQuiet(t *testing.T) {
	f, _, stderr := newTestFormatterWithStderr("json")
	f.SetQuiet(true)
	rows := makeRows(listHintThreshold + 50)
	if err := f.Print(rows); err != nil {
		t.Fatalf("Print failed: %v", err)
	}
	if stderr.Len() != 0 {
		t.Errorf("--quiet should suppress hint, got stderr=%q", stderr.String())
	}
}

func TestListHint_SuppressedForTable(t *testing.T) {
	f, _, stderr := newTestFormatterWithStderr("table")
	rows := makeRows(listHintThreshold + 10)
	if err := f.Print(rows); err != nil {
		t.Fatalf("Print failed: %v", err)
	}
	if stderr.Len() != 0 {
		t.Errorf("table format already shows count, hint should be skipped, got %q", stderr.String())
	}
}

func TestListHint_FiresForCSVAndYAML(t *testing.T) {
	for _, format := range []string{"csv", "yaml", "plain"} {
		t.Run(format, func(t *testing.T) {
			f, _, stderr := newTestFormatterWithStderr(format)
			rows := makeRows(listHintThreshold)
			if err := f.Print(rows); err != nil {
				t.Fatalf("Print failed for %s: %v", format, err)
			}
			if !strings.Contains(stderr.String(), "results returned") {
				t.Errorf("expected hint for %s, got %q", format, stderr.String())
			}
		})
	}
}

func TestListHint_PrintRawJSONFastPath_TopLevelArray(t *testing.T) {
	f, _, stderr := newTestFormatterWithStderr("json")
	// Top-level JSON array of 60 items, no projector → fast path triggers.
	var b bytes.Buffer
	b.WriteString("[")
	for i := 0; i < 60; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"id":0}`)
	}
	b.WriteString("]")
	if err := f.PrintRaw(b.Bytes()); err != nil {
		t.Fatalf("PrintRaw failed: %v", err)
	}
	if !strings.Contains(stderr.String(), "results returned") {
		t.Errorf("expected hint on JSON fast path for top-level array, got %q", stderr.String())
	}
}

func TestListHint_PrintRawJSONFastPath_WrappedObject_Skipped(t *testing.T) {
	f, _, stderr := newTestFormatterWithStderr("json")
	// Pro-style wrapped response — top level is an object, not an array.
	// Hint cannot reliably size embedded results without API-specific
	// knowledge, so it's skipped.
	input := []byte(`{"totalCount":1000,"results":[{"id":1}]}`)
	if err := f.PrintRaw(input); err != nil {
		t.Fatalf("PrintRaw failed: %v", err)
	}
	if stderr.Len() != 0 {
		t.Errorf("hint should be skipped for wrapped objects, got %q", stderr.String())
	}
}
