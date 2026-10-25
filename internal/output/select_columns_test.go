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

func TestColumnKeys_FollowSelectOrder(t *testing.T) {
	rows := []map[string]any{{"name": "a", "serial": "s", "general.users": "u", "general.site": "x"}}
	for _, format := range []string{"csv", "plain"} {
		f, out := newTestFormatter(format)
		f.SetProjector(Projector{Select: []string{"name", "serial", "general.users"}})
		if err := f.Print(rows); err != nil {
			t.Fatal(err)
		}
		got := out.String()
		if format == "csv" && !strings.HasPrefix(got, "name,serial,general.users\n") {
			t.Errorf("csv header must follow --select order, got %q", got)
		}
		if format == "plain" && strings.TrimSpace(got) != "a\ts\tu" {
			t.Errorf("plain columns must follow --select order, got %q", got)
		}
	}
}

func TestOrderBySelect_ObjectPathKeepsItsKeysTogetherAndDedupes(t *testing.T) {
	got := orderBySelect([]string{"a", "g.x", "g.y", "z"}, []string{"z", "g", "z"})
	want := []string{"z", "g.x", "g.y", "a"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v, want %v", got, want)
	}
}
