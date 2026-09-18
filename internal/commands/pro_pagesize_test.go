// Copyright 2026, Jamf Software LLC

package commands

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/generator/parser"
)

// The generated commands read their page size straight off the spec at generate
// time (parser.MaxPageSize). The hand-written fetch-everything helpers cannot:
// they assemble a path at runtime from a literal, sometimes from a registry
// entry, so the ceiling has to be a runtime lookup — MaxPageSizeFor — and a
// runtime lookup is a second copy of a fact the specs already state.
//
// These two tests are what keeps the copy honest. Without them the table goes
// stale the way every hand-maintained path table in this repo has gone stale:
// silently, and only on the customer's tenant.

// TestFetchAllPaginatedNeverOutrunsASpecCeiling requires proPageSizeCeilings to
// name exactly the Jamf Pro endpoints whose spec declares a page-size maximum
// below the verified cap, so no hand-written walk asks for a bigger page than
// the endpoint says it will serve.
//
// The direction matters. Asking for LESS than the ceiling costs extra requests
// and nothing else — which is why an endpoint whose response is a raw array
// rather than a page is not in scope here: parser.MaxPageSize leaves those at
// the API default because a page size means nothing to them, and
// FetchAllPaginated detects the array and returns it whole on the first
// response whatever it asked for.
//
// Asking for MORE than a DECLARED maximum is the case this guards. That is the
// declaration the server tends to enforce by rejecting the request rather than
// by clamping it, so getting it wrong fails the whole call rather than costing
// a round trip. Only /v1/users declares one today; the set is derived from the
// specs so the next one to appear fails here rather than on a tenant.
func TestFetchAllPaginatedNeverOutrunsASpecCeiling(t *testing.T) {
	specs, err := filepath.Glob(filepath.Join("..", "..", "specs", "*.yaml"))
	if err != nil {
		t.Fatalf("globbing specs: %v", err)
	}
	if len(specs) == 0 {
		t.Fatal("no specs found — this test would pass vacuously")
	}

	declared := map[string]int{}
	checked := 0
	for _, spec := range specs {
		resources, err := parser.ParseSpec(spec)
		if err != nil {
			t.Fatalf("parsing %s: %v", spec, err)
		}
		for _, r := range parser.FlattenResources(resources) {
			for _, op := range r.AllOperations() {
				if !op.IsPaginated {
					continue
				}
				checked++
				declaredMax := parser.DeclaredPageSizeMax(op.Parameters)
				if declaredMax == 0 || declaredMax >= ProMaxPageSize {
					continue
				}
				declared[op.Path] = declaredMax
			}
		}
	}
	if checked == 0 {
		t.Fatal("no paginated operations found — the walk did not exercise anything")
	}

	for path, declaredMax := range declared {
		if strings.Contains(path, "{") {
			t.Errorf("%s declares a page-size maximum of %d but its path carries a parameter, so proPageSizeCeilings cannot express it by exact match — MaxPageSizeFor needs a pattern before this endpoint is safe to walk",
				path, declaredMax)
			continue
		}
		got, ok := proPageSizeCeilings[path]
		switch {
		case !ok:
			t.Errorf("%s declares a page-size maximum of %d and is missing from proPageSizeCeilings; a walk of it would ask for %d",
				path, declaredMax, ProMaxPageSize)
		case got != declaredMax:
			t.Errorf("proPageSizeCeilings[%q] = %d, but the spec declares %d", path, got, declaredMax)
		}
	}

	for path := range proPageSizeCeilings {
		if _, ok := declared[path]; !ok {
			t.Errorf("proPageSizeCeilings names %q, which no spec declares a lower maximum for — a stale entry throttles a walk for no reason", path)
		}
	}

	t.Logf("checked %d paginated operations, %d declare a lower ceiling", checked, len(declared))
}

// TestProMaxPageSizeMatchesTheGenerator holds the runtime constant and the
// generate-time one to the same number. They are two spellings of one
// wire-verified fact, and a divergence would mean `pro departments list --all`
// and `pro report inventory` walked the same collection at different page
// sizes for no stated reason.
func TestProMaxPageSizeMatchesTheGenerator(t *testing.T) {
	if ProMaxPageSize != parser.ProPageSizeCap {
		t.Errorf("ProMaxPageSize = %d but parser.ProPageSizeCap = %d; the generated commands and the hand-written walks must agree",
			ProMaxPageSize, parser.ProPageSizeCap)
	}
}

// TestMaxPageSizeForReadsThroughAQueryString covers the shape most call sites
// actually pass: a path with the sections or filter already appended.
func TestMaxPageSizeForReadsThroughAQueryString(t *testing.T) {
	tests := []struct {
		path string
		want int
	}{
		{"/v1/departments", ProMaxPageSize},
		{"/v4/computers-inventory?section=GENERAL&section=HARDWARE", ProMaxPageSize},
		{"/v1/users", 1000},
		{"/v1/users?filter=" + "username==\"a*\"", 1000},
		// A sibling under the same prefix is a different endpoint and must not
		// inherit the exception.
		{"/v1/users-preload", ProMaxPageSize},
	}
	for _, tt := range tests {
		if got := MaxPageSizeFor(tt.path); got != tt.want {
			t.Errorf("MaxPageSizeFor(%q) = %d, want %d", tt.path, got, tt.want)
		}
	}
}
