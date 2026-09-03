// Copyright 2026, Jamf Software LLC

package gateway

import (
	"strings"
	"testing"
)

func TestScopesResolvesAConcretePath(t *testing.T) {
	// An operation can require more than one permission, and all of them have
	// to be reported: granting one of two still 403s.
	if got := strings.Join(Scopes("GET", "/pro/v1/categories"), ","); got != "categories:read,self-service:read" {
		t.Errorf("Scopes = %q, want categories:read,self-service:read", got)
	}
	// A path parameter, and a query string the table is not keyed on.
	if got := Scopes("DELETE", "/pro/v1/categories/17?force=true"); len(got) != 1 || got[0] != "categories:delete" {
		t.Errorf("Scopes = %v, want [categories:delete]", got)
	}
	if got := Scopes("GET", "/proclassic/categories/id/17"); len(got) != 1 || got[0] != "categories:read" {
		t.Errorf("Scopes = %v, want [categories:read]", got)
	}
}

func TestScopesIsEmptyForWhatTheTableDoesNotCarry(t *testing.T) {
	for _, tc := range []struct{ method, path string }{
		{"GET", "/pro/v1/no-such-endpoint"},
		{"PATCH", "/pro/v1/categories"}, // real path, method the spec does not declare
		{"GET", "/api/v1/categories"},   // instance form: never in the table
	} {
		if got := Scopes(tc.method, tc.path); got != nil {
			t.Errorf("Scopes(%s %s) = %v, want nil", tc.method, tc.path, got)
		}
	}
}

// A literal path and a parameterised sibling can both match one concrete
// request. The literal one is the more specific answer, and a single ordered
// scan would return whichever the sort happened to reach first.
func TestScopesPrefersALiteralRuleOverAParameterisedOne(t *testing.T) {
	var literal, param string
	for _, r := range scopeRules {
		if r.Method != "GET" || !strings.HasPrefix(r.Path, "/pro/") {
			continue
		}
		segs := strings.Split(strings.Trim(r.Path, "/"), "/")
		if len(segs) < 4 || strings.Contains(r.Path, "{}") {
			continue
		}
		// Look for a parameterised sibling of the same shape.
		sibling := strings.Join(append(append([]string{}, segs[:len(segs)-1]...), "{}"), "/")
		for _, other := range scopeRules {
			if other.Method == "GET" && strings.Trim(other.Path, "/") == sibling {
				literal, param = r.Path, other.Path
				break
			}
		}
		if literal != "" {
			break
		}
	}
	if literal == "" {
		t.Skip("no literal/parameterised sibling pair in the current manifest")
	}
	want := Scopes("GET", literal)
	var wantRule []string
	for _, r := range scopeRules {
		if r.Method == "GET" && r.Path == literal {
			wantRule = r.Scopes
		}
	}
	if strings.Join(want, ",") != strings.Join(wantRule, ",") {
		t.Errorf("Scopes(%s) = %v, want the literal rule's %v (parameterised sibling %s won)",
			literal, want, wantRule, param)
	}
}
