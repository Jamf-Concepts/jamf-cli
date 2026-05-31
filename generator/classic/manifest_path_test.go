// Copyright 2026, Jamf Software LLC

package classic

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// isCleanPathSegment reports whether s is a well-formed single URL path segment
// for the Classic API: non-empty, no whitespace, no slashes (it is one segment,
// not a path), and no control characters. A typo'd token in resources.yaml
// (stray space, leading slash, embedded slash) produces a broken /JSSResource/
// URL at runtime that nothing else catches — this is the surface that proof guards.
func isCleanPathSegment(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r <= ' ' || r == '/' || r == '\\' {
			return false
		}
	}
	return true
}

// TestProofClassicManifestPathsWellFormed validates the hand-maintained classic
// manifest. The generated modern paths come straight from OpenAPI specs and are
// tautologically valid, but specs/classic/resources.yaml is hand-edited, so its
// path tokens are a real drift surface. We assert every Path/IDPath/GroupPath
// token is a clean single segment and that the constructed /JSSResource/{Path}
// URL is well-formed.
func TestProofClassicManifestPathsWellFormed(t *testing.T) {
	resources, err := ParseManifest("../../specs/classic/resources.yaml")
	if err != nil {
		t.Fatalf("ParseManifest(real manifest): %v", err)
	}

	// Floor sanity check: an empty parse would make "no violations" silent.
	const minResources = 10
	if len(resources) < minResources {
		t.Fatalf("parsed only %d classic resources (expected >= %d) — manifest or parser regressed", len(resources), minResources)
	}

	var violations []string
	for _, r := range resources {
		label := r.CLIName
		if label == "" {
			label = r.Name
		}

		// A clean Path token guarantees the constructed "/JSSResource/"+Path URL
		// is well-formed (no doubled slash, no whitespace), so the segment check
		// is the whole check — no separate URL assertion is needed.
		if !isCleanPathSegment(r.Path) {
			violations = append(violations, fmt.Sprintf("%s: Path token %q is not a clean URL segment", label, r.Path))
		}
		// IDPath is always populated by ParseManifest (defaults to "id"); the
		// check still validates custom id_path tokens such as "groupid".
		if !isCleanPathSegment(r.IDPath) {
			violations = append(violations, fmt.Sprintf("%s: IDPath token %q is not a clean URL segment", label, r.IDPath))
		}
		// GroupPath is only set for resources with group endpoints.
		if r.GroupPath != "" && !isCleanPathSegment(r.GroupPath) {
			violations = append(violations, fmt.Sprintf("%s: GroupPath token %q is not a clean URL segment", label, r.GroupPath))
		}
	}

	if len(violations) > 0 {
		sort.Strings(violations)
		t.Errorf("%d malformed classic-manifest path(s) in specs/classic/resources.yaml:\n  %s\n\nFix the offending entry's path/id_path/groups_path token in specs/classic/resources.yaml.",
			len(violations), strings.Join(violations, "\n  "))
	}
}

// TestIsCleanPathSegment is a permanent guard on the validator itself: the floor
// check above protects against an empty parse, but only this test catches a
// future edit that weakens isCleanPathSegment (e.g. dropping the slash check),
// which would otherwise let the manifest proof silently pass.
func TestIsCleanPathSegment(t *testing.T) {
	for _, s := range []string{"", "policies/extra", "policies ", "/policies", "back\\slash"} {
		if isCleanPathSegment(s) {
			t.Errorf("%q should be rejected", s)
		}
	}
	for _, s := range []string{"policies", "osxconfigurationprofiles", "groupid"} {
		if !isCleanPathSegment(s) {
			t.Errorf("%q should be accepted", s)
		}
	}
}
