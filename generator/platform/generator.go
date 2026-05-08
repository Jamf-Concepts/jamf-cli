// Copyright 2026, Jamf Software LLC

// Package platform orchestrates parsing of all Platform Gateway OpenAPI specs
// and merges resources by tag-name across specs. The output is a list of
// canonical Resources ready for code emission. Generated commands live under
// internal/commands/platform/generated/ so any product (pro, school, etc.) can
// wire selected resources into its command tree.
package platform

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/Jamf-Concepts/jamf-cli/generator/parser"
)

// LoadResources walks specsDir for *.json platform spec files, parses each,
// and merges resources sharing the same name (e.g. "devices" appears in
// device-groups-api.json and device-inventory-api.json — operations from both
// merge into a single Resource).
//
// Returns the merged resources plus the sorted list of spec files actually
// consumed, so callers (notably the provenance writer) don't have to re-glob
// and risk drifting from what was parsed.
func LoadResources(specsDir string) ([]*parser.Resource, []string, error) {
	files, err := filepath.Glob(filepath.Join(specsDir, "*.json"))
	if err != nil {
		return nil, nil, fmt.Errorf("globbing platform specs: %w", err)
	}
	sort.Strings(files)

	merged := make(map[string]*parser.Resource)
	for _, f := range files {
		resources, err := parser.ParsePlatformSpec(f)
		if err != nil {
			return nil, nil, fmt.Errorf("parsing %s: %w", filepath.Base(f), err)
		}
		for _, r := range resources {
			if existing, ok := merged[r.Name]; ok {
				mergeInto(existing, r)
			} else {
				merged[r.Name] = r
			}
		}
	}

	names := make([]string, 0, len(merged))
	for n := range merged {
		names = append(names, n)
	}
	sort.Strings(names)

	out := make([]*parser.Resource, 0, len(merged))
	for _, n := range names {
		out = append(out, merged[n])
	}
	return out, files, nil
}

// mergeInto folds src's operations and schemas into dst. Operations are
// deduplicated by (method, path); when both sides define the same operation,
// dst wins. Schemas are unioned; dst wins on collision.
func mergeInto(dst, src *parser.Resource) {
	type opKey struct{ method, path string }
	have := make(map[opKey]bool, len(dst.Operations))
	for _, op := range dst.Operations {
		have[opKey{op.Method, op.Path}] = true
	}
	for _, op := range src.Operations {
		k := opKey{op.Method, op.Path}
		if have[k] {
			continue
		}
		dst.Operations = append(dst.Operations, op)
		have[k] = true
	}
	if dst.Schemas == nil {
		dst.Schemas = make(map[string]*parser.Schema)
	}
	for name, s := range src.Schemas {
		if _, exists := dst.Schemas[name]; !exists {
			dst.Schemas[name] = s
		}
	}
}
