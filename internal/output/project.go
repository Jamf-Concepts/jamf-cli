// Copyright 2026, Jamf Software LLC

package output

import "strings"

// Projector applies field-level projection to flattened rows before
// format-specific rendering. Compact keeps only scalar fields after
// flattening, dropping arrays and nested remnants. Select keeps only the
// listed dot paths. When both are set, Select wins. The projector is shared
// by every output format (json, table, csv, yaml, plain) so projection
// behaves consistently.
type Projector struct {
	Compact bool
	Select  []string
}

// IsZero reports whether the projector has no rules configured.
func (p Projector) IsZero() bool {
	return !p.Compact && len(p.Select) == 0
}

// Apply returns rows projected per the configured rules.
// Always flattens nested objects to dot keys so projection sees a flat shape.
// Empty rows pass through unchanged.
//
// Select uses flattenRowsRaw (no common-prefix stripping) so user-supplied
// dot paths like "general.name" still match on single-section responses
// where stripCommonPrefix would otherwise rewrite "general.name" → "name"
// and silently produce empty projections.
func (p Projector) Apply(rows []map[string]any) []map[string]any {
	if p.IsZero() || len(rows) == 0 {
		return rows
	}
	switch {
	case len(p.Select) > 0:
		return p.selectRows(rows)
	case p.Compact:
		return projectCompact(flattenRows(rows))
	}
	return flattenRows(rows)
}

// projectSelect keeps only the requested dot paths.
// A path matches a flattened key directly OR a flattened key prefixed with
// "<path>." — so --select general returns every general.* field, and
// --select general.name returns just that one. Missing paths are silently
// omitted (a row may end up empty if no path matched).
// DropEmptySelections returns the rows whose Select projection keeps at least
// one field, and how many it dropped.
//
// It replaces an all-or-nothing guard that could only answer "did EVERY row
// come back empty", which is the wrong question for a table or a CSV. Both take
// their column set from row 0 alone — printCSV's `sortedKeys(v[0])` and
// printTable's `sortedKeys(rows[0])` — so a row 0 the select missed emptied the
// column set and discarded every matched value in every later row:
// `commands -o csv --wide --select api` wrote 1757 newlines and zero other
// characters while 1375 of those rows carried the field, at exit 0. CLAUDE.md's
// Conventions section already names that hazard; making --select live on 27
// commands with heterogeneous rows is the case it warns about.
//
// Dropping the emptied rows makes the column set correct by construction, since
// every surviving row carries at least one selected field. It also removes the
// need for any format-specific early return: zero surviving rows is an empty
// collection, which every renderer already handles.
func (p Projector) DropEmptySelections(rows []map[string]any) ([]map[string]any, int) {
	if len(p.Select) == 0 || len(rows) == 0 {
		return rows, 0
	}
	projected := p.selectRows(rows)
	kept := make([]map[string]any, 0, len(rows))
	for i, row := range projected {
		if len(row) > 0 {
			kept = append(kept, rows[i])
		}
	}
	return kept, len(rows) - len(kept)
}

// selectRows runs the Select projection, and is the only place its input
// pipeline is written down.
//
// Apply and DropEmptySelections must answer about the same rows or the guard
// suppresses output the renderer would have produced. They each spelled the
// pipeline out once, and diverged: Apply flattened first while the guard
// projected raw rows, so a nested path matched nothing, emptied every row and
// suppressed a whole report at exit 0 — the reports built as one row of nested
// sections for -o json are exactly the shape the guard was added for.
// `pro report app-status -o json --select summary.total_errors` wrote 0 bytes
// where main wrote 201, which is verbatim the signature issue #349 reports.
func (p Projector) selectRows(rows []map[string]any) []map[string]any {
	return projectSelect(flattenRowsRaw(rows), p.Select)
}

func projectSelect(rows []map[string]any, paths []string) []map[string]any {
	cleaned := make([]string, 0, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p != "" {
			cleaned = append(cleaned, p)
		}
	}
	if len(cleaned) == 0 {
		return rows
	}
	out := make([]map[string]any, len(rows))
	for i, row := range rows {
		projected := make(map[string]any, len(cleaned))
		for _, p := range cleaned {
			if v, ok := row[p]; ok {
				projected[p] = v
				continue
			}
			prefix := p + "."
			for k, v := range row {
				if strings.HasPrefix(k, prefix) {
					projected[k] = v
				}
			}
		}
		out[i] = projected
	}
	return out
}

// compactAllowKeys are leaf field names (lowercased) always kept by --compact,
// regardless of how often they appear: identity and high-signal fields.
var compactAllowKeys = map[string]bool{
	"id": true, "name": true, "displayname": true, "udid": true,
	"serialnumber": true, "serial": true, "managementid": true,
	"username": true, "email": true, "status": true, "state": true,
	"enabled": true, "managed": true, "supervised": true, "model": true,
	"osversion": true, "version": true, "type": true, "category": true,
}

// compactBlockKeys are verbose leaf fields dropped from a single-object
// --compact (where the frequency rule is meaningless at n=1).
var compactBlockKeys = map[string]bool{
	"description": true, "notes": true, "body": true, "content": true,
	"payloads": true, "payload": true, "scriptcontents": true,
	"html": true, "base64": true,
}

// leafKey returns the last dot-segment of a flattened key.
func leafKey(k string) string {
	if i := strings.LastIndex(k, "."); i >= 0 {
		return k[i+1:]
	}
	return k
}

// compactAllowed reports whether a leaf key is always kept: in the allowlist,
// or an identity field (suffix "Id").
func compactAllowed(leaf string) bool {
	if compactAllowKeys[strings.ToLower(leaf)] {
		return true
	}
	return strings.HasSuffix(leaf, "Id")
}

// projectCompact keeps high-signal scalars. flattenRows already lifts nested
// objects to dot keys and drops arrays, so this trims the remaining scalar
// noise. For a single object it keeps every scalar except a verbose blocklist.
// For a list it keeps a scalar when the leaf is allowlisted OR the key is
// present (non-null scalar) in >=80% of rows.
func projectCompact(rows []map[string]any) []map[string]any {
	if len(rows) == 0 {
		return rows
	}
	if len(rows) == 1 {
		row := rows[0]
		out := make(map[string]any, len(row))
		for k, v := range row {
			if !isScalar(v) {
				continue
			}
			if compactBlockKeys[strings.ToLower(leafKey(k))] {
				continue
			}
			out[k] = v
		}
		return []map[string]any{out}
	}

	counts := make(map[string]int)
	for _, row := range rows {
		for k, v := range row {
			if isScalar(v) {
				counts[k]++
			}
		}
	}
	out := make([]map[string]any, len(rows))
	for i, row := range rows {
		c := make(map[string]any, len(row))
		for k, v := range row {
			if !isScalar(v) {
				continue
			}
			if compactAllowed(leafKey(k)) || counts[k]*100 >= len(rows)*80 {
				c[k] = v
			}
		}
		out[i] = c
	}
	return out
}

// isScalar reports whether v is a primitive worth keeping in --compact mode.
// nil is excluded so null fields are dropped, matching flattenMap's behavior
// for nested nulls and keeping compact output free of empty values.
func isScalar(v any) bool {
	switch v.(type) {
	case bool, string, float64, float32,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64:
		return true
	}
	return false
}
