// Copyright 2026, Jamf Software LLC

package platform

import (
	"encoding/json"
	"strings"
)

// TableColumn defines a column selection for list table output.
type TableColumn struct {
	Field string // dot-notation JSON path (e.g. "deploymentState.state")
	Label string // output key/header
}

// SelectTableColumns transforms a JSON array to include only the configured
// columns. For JSON/YAML/raw output formats, the input is returned unchanged
// so full fidelity is preserved.
func SelectTableColumns(data []byte, columns []TableColumn, format string) []byte {
	if format == "json" || format == "yaml" || format == "xml" || format == "raw" {
		return data
	}
	var items []json.RawMessage
	if err := json.Unmarshal(data, &items); err != nil {
		return data
	}
	result := make([]map[string]any, 0, len(items))
	for _, raw := range items {
		var obj map[string]any
		if err := json.Unmarshal(raw, &obj); err != nil {
			continue
		}
		row := make(map[string]any, len(columns))
		for _, col := range columns {
			row[col.Label] = resolveFieldPath(obj, col.Field)
		}
		result = append(result, row)
	}
	out, err := json.Marshal(result)
	if err != nil {
		return data
	}
	return out
}

// resolveFieldPath extracts a value from a map using a dot-notation field path.
// Returns nil if any segment in the path is missing or not an object.
func resolveFieldPath(obj map[string]any, field string) any {
	if !strings.Contains(field, ".") {
		return obj[field]
	}
	parts := strings.Split(field, ".")
	var current any = obj
	for _, part := range parts[:len(parts)-1] {
		m, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = m[part]
	}
	m, ok := current.(map[string]any)
	if !ok {
		return nil
	}
	return m[parts[len(parts)-1]]
}
