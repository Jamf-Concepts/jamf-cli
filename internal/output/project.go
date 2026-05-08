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
func (p Projector) Apply(rows []map[string]any) []map[string]any {
	if p.IsZero() || len(rows) == 0 {
		return rows
	}
	flat := flattenRows(rows)
	switch {
	case len(p.Select) > 0:
		return projectSelect(flat, p.Select)
	case p.Compact:
		return projectCompact(flat)
	}
	return flat
}

// projectSelect keeps only the requested dot paths.
// A path matches a flattened key directly OR a flattened key prefixed with
// "<path>." — so --select general returns every general.* field, and
// --select general.name returns just that one. Missing paths are silently
// omitted (a row may end up empty if no path matched).
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

// projectCompact keeps only scalar values from flattened rows.
// flattenRows already lifts nested objects to dot keys, so what remains
// non-scalar here is arrays — which are the bulk of the token cost in
// Jamf Pro list responses.
func projectCompact(rows []map[string]any) []map[string]any {
	out := make([]map[string]any, len(rows))
	for i, row := range rows {
		compact := make(map[string]any, len(row))
		for k, v := range row {
			if isScalar(v) {
				compact[k] = v
			}
		}
		out[i] = compact
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
