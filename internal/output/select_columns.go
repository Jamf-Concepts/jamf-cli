// Copyright 2026, Jamf Software LLC

package output

import "strings"

// arrayJoinSep joins the elements of a scalar array in a column added by
// --select, so a small list is one cell and one record stays one row.
const arrayJoinSep = "; "

// AddSelectColumns adds a column to each summary row for every --select path
// the summary does not already carry, resolved against the full record the row
// was built from. rows[i] is the summary of items[i].
//
// A list command's table, csv and plain output is a curated summary, so a
// path outside it used to be dropped without a word and the only route to the
// field was `-o json | jq`. A path that is a summary label, or lies under one,
// is left to the projector as before.
//
// The column is headed by the requested path. It is added only when at least
// one record has the path, so a mistyped path still matches nothing and the
// formatter's projection-miss note names it. An array of scalars is joined with
// "; " into one cell; an object or an array holding objects stays compact JSON,
// which is what FormatValue renders.
func AddSelectColumns(items, rows []map[string]any, labels, paths []string) {
	for _, path := range cleanPaths(paths) {
		if coveredBySummary(path, labels) {
			continue
		}
		parts := strings.Split(path, ".")
		values := make([]any, len(items))
		found := false
		for i, item := range items {
			v, ok := walkPath(item, parts)
			if ok {
				found = true
				values[i] = joinScalarArray(v)
			}
		}
		if !found {
			continue
		}
		for i, row := range rows {
			row[path] = values[i]
		}
	}
}

// coveredBySummary reports whether path names a summary column or a field
// beneath one.
func coveredBySummary(path string, labels []string) bool {
	for _, l := range labels {
		if path == l || strings.HasPrefix(path, l+".") {
			return true
		}
	}
	return false
}

// walkPath traverses a dot path through nested objects.
func walkPath(obj map[string]any, parts []string) (any, bool) {
	var current any = obj
	for _, part := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		if current, ok = m[part]; !ok {
			return nil, false
		}
	}
	return current, true
}

// joinScalarArray renders an array of scalars as one "; "-separated string and
// returns any other value unchanged.
func joinScalarArray(v any) any {
	arr, ok := v.([]any)
	if !ok {
		return v
	}
	parts := make([]string, len(arr))
	for i, e := range arr {
		switch e.(type) {
		case map[string]any, []any:
			return v
		}
		parts[i] = FormatValue(e)
	}
	return strings.Join(parts, arrayJoinSep)
}
