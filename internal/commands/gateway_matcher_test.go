package commands

import "testing"

// TestManifestPathMatchesDoesNotLetASubstitutionMatchALiteral pins the two
// matcher properties the review found reporting "checked" for cases the guard
// did not check.
func TestManifestPathMatchesDoesNotLetASubstitutionMatchALiteral(t *testing.T) {
	m := coverageManifest{Spec: map[string][]string{
		"/pro/v1/packages/{}":                {"DELETE", "GET", "PUT"},
		"/pro/v1/packages/delete-multiple":   {"POST"},
		"/pro/v1/packages/export":            {"POST"},
		"/pro/v1/computers-inventory/detail": {"GET"},
		"/pro/v1/computers-inventory/{}":     {"GET", "PATCH"},
	}}

	// A code "{}" addresses a parameter, so it must not pick up the methods of
	// the literal siblings at the same arity. This is the reported case: POST
	// was reported as published on /pro/v1/packages/{}.
	got := m.methodsFor("/pro/v1/packages/{}")
	for _, meth := range got {
		if meth == "POST" {
			t.Errorf("methodsFor(/pro/v1/packages/{}) = %v; POST comes from the literal "+
				"delete-multiple/export siblings and the gateway declares none on the "+
				"parameterised path", got)
		}
	}

	// A literal still matches its own pattern in preference to the parameterised
	// one — that is what internal/gateway's two-pass lookup does.
	if got := m.methodsFor("/pro/v1/computers-inventory/detail"); len(got) != 1 || got[0] != "GET" {
		t.Errorf("methodsFor(.../detail) = %v, want [GET] only — the literal pattern must "+
			"win over the {} one", got)
	}

	// And a literal with no pattern of its own still falls back to {}.
	if got := m.methodsFor("/pro/v1/computers-inventory/42"); len(got) != 2 {
		t.Errorf("methodsFor(.../42) = %v, want the {} pattern's methods", got)
	}
}
