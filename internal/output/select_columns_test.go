// Copyright 2026, Jamf Software LLC

package output

import (
	"strings"
	"testing"
)

func selectColumnsFixture() (items, rows []map[string]any) {
	items = []map[string]any{
		{"general": map[string]any{"name": "a", "mdmCapable": map[string]any{"capableUsers": []any{}}}},
		{"general": map[string]any{"name": "b", "mdmCapable": map[string]any{"capableUsers": []any{"x", "y"}}}},
	}
	rows = []map[string]any{{"name": "a"}, {"name": "b"}}
	return items, rows
}

func TestAddSelectColumns_ResolvesPathFromRecordAndJoinsArrays(t *testing.T) {
	items, rows := selectColumnsFixture()
	AddSelectColumns(items, rows, []string{"name"}, []string{"name", "general.mdmCapable.capableUsers"})
	if got := rows[0]["general.mdmCapable.capableUsers"]; got != "" {
		t.Errorf("empty array: want empty cell, got %#v", got)
	}
	if got := rows[1]["general.mdmCapable.capableUsers"]; got != "x; y" {
		t.Errorf("want joined array, got %#v", got)
	}
}

func TestAddSelectColumns_SkipsSummaryAndAbsentPaths(t *testing.T) {
	items, rows := selectColumnsFixture()
	AddSelectColumns(items, rows, []string{"name"}, []string{"name", "name.x", "general.nosuch"})
	for _, r := range rows {
		if len(r) != 1 {
			t.Errorf("summary and absent paths must add no column, got %v", r)
		}
	}
}

func TestAddSelectColumns_ObjectsStayJSON(t *testing.T) {
	items := []map[string]any{{"a": []any{map[string]any{"k": "v"}}}}
	rows := []map[string]any{{}}
	AddSelectColumns(items, rows, nil, []string{"a"})
	if _, ok := rows[0]["a"].([]any); !ok {
		t.Errorf("array of objects must be left for FormatValue, got %#v", rows[0]["a"])
	}
}

func TestPrint_SelectNamesPathThatMatchedNothing(t *testing.T) {
	f, _, stderr := newTestFormatterWithStderr("csv")
	f.SetProjector(Projector{Select: []string{"name", "typo"}})
	if err := f.Print([]map[string]any{{"name": "a"}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "--select typo matched no field") {
		t.Errorf("want a note naming the mistyped path, got %q", stderr.String())
	}
}
