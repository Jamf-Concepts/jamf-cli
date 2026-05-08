// Copyright 2026, Jamf Software LLC

package output

// Projector applies field-level projection to flattened rows before
// format-specific rendering. Compact keeps only scalar fields after
// flattening, dropping arrays and nested remnants. The projector is shared
// by every output format (json, table, csv, yaml, plain) so --compact
// behaves consistently.
type Projector struct {
	Compact bool
}

// IsZero reports whether the projector has no rules configured.
func (p Projector) IsZero() bool {
	return !p.Compact
}

// Apply returns rows projected per the configured rules.
// Always flattens nested objects to dot keys so projection sees a flat shape.
// Empty rows pass through unchanged.
func (p Projector) Apply(rows []map[string]any) []map[string]any {
	if p.IsZero() || len(rows) == 0 {
		return rows
	}
	flat := flattenRows(rows)
	if p.Compact {
		return projectCompact(flat)
	}
	return flat
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
