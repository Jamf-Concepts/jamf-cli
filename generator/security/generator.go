// Copyright 2026, Jamf Software LLC

// Package security orchestrates parsing of the three Jamf Security Cloud
// OpenAPI specs (Risk, Device Lifecycle, Shared Signals & Events) and code
// generation for their commands. Generated commands live under
// internal/commands/security/generated/, wired under the hand-written
// "security" product command (internal/commands/security.go) alongside the
// hand-written `security setup`.
package security

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/Jamf-Concepts/jamf-cli/generator/parser"
)

// LoadResources walks specsDir for *.json Security Cloud spec files, parses
// each via parser.ParseSecuritySpec, and returns the combined resources plus
// a resource-name → scope ("Risk"/"Lifecycle"/"SSE") map for the emitter,
// alongside the sorted list of spec files actually consumed (so the
// provenance writer doesn't have to re-glob and risk drifting from what was
// parsed).
func LoadResources(specsDir string) ([]*parser.Resource, map[string]string, []string, error) {
	files, err := filepath.Glob(filepath.Join(specsDir, "*.json"))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("globbing security specs: %w", err)
	}
	sort.Strings(files)

	var resources []*parser.Resource
	var used []string
	scopeOf := make(map[string]string)
	for _, f := range files {
		scope := parser.SecurityScopeForFile(f)
		if scope == "" {
			continue // not a recognized Security Cloud spec
		}
		parsed, err := parser.ParseSecuritySpec(f)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("parsing %s: %w", filepath.Base(f), err)
		}
		if len(parsed) == 0 {
			continue
		}
		used = append(used, f)
		for _, r := range parsed {
			scopeOf[r.Name] = scope
		}
		resources = append(resources, parsed...)
	}
	return resources, scopeOf, used, nil
}
