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
				continue
			}
			merged[r.Name] = r
		}
	}

	names := make([]string, 0, len(merged))
	for n := range merged {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, n := range names {
		if err := checkOperationNameCollisions(merged[n]); err != nil {
			return nil, nil, err
		}
	}

	out := make([]*parser.Resource, 0, len(merged))
	for _, n := range names {
		out = append(out, merged[n])
	}
	return out, files, nil
}

// checkOperationNameCollisions rejects a resource carrying two operations of
// the same name.
//
// Operation names become both the cobra subcommand and the Go constructor
// name, so a duplicate emits two new<Resource><Op>Cmd functions into one file
// and the package stops compiling — or, worse, if the names ever diverge while
// the subcommand strings do not, registers two subcommands under one verb and
// silently serves whichever cobra matches first.
//
// This is reachable whenever two specs contribute an identically-tagged
// resource: mergeInto deduplicates by (method, path), which does nothing for a
// "list" of /groups meeting a "list" of /device-groups. Jamf Security Cloud
// introduced the first such pair. The fix is a platformResourceNameOverrides
// entry that keeps the two resources apart, so the error names that map.
func checkOperationNameCollisions(r *parser.Resource) error {
	seen := make(map[string]string, len(r.Operations))
	for _, op := range r.Operations {
		if prev, dup := seen[op.Name]; dup {
			return fmt.Errorf(
				"platform resource %q has two %q operations (%s and %s): "+
					"two specs contribute this resource name — rename one via "+
					"platformResourceNameOverrides in generator/parser/platform.go, "+
					"keyed \"{service}/%s\"",
				r.Name, op.Name, prev, op.Path, r.Name)
		}
		seen[op.Name] = op.Path
	}
	return nil
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
