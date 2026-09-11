// Copyright 2026, Jamf Software LLC

package parser

import "testing"

// The name a plain verb takes belongs to the resource's own root, and the loser
// of that collision is renamed rather than deleted.
//
// Both halves are asserted, because the defect had two: `pro
// enrollment-customization create` addressed an LDAP panel *and* the
// customization's own POST stopped existing, so a test checking only that the
// root won would pass on a build that still dropped it.
func TestQualifyDuplicateVerbsOutsideTheRoot(t *testing.T) {
	root := []string{"enrollment-customizations"}
	ops := []*Operation{
		{Name: "create", Method: "POST", Path: "/v1/enrollment-customization/{id}/ldap"},
		{Name: "update", Method: "PUT", Path: "/v1/enrollment-customization/{id}/ldap/{panel-id}"},
		{Name: "delete", Method: "DELETE", Path: "/v1/enrollment-customization/{id}/all/{panel-id}"},
		{Name: "create", Method: "POST", Path: "/v2/enrollment-customizations"},
		{Name: "update", Method: "PUT", Path: "/v2/enrollment-customizations/{id}"},
		{Name: "delete", Method: "DELETE", Path: "/v2/enrollment-customizations/{id}"},
	}
	qualifyDuplicateVerbsOutsideTheRoot(ops, root)

	want := map[string]string{
		"POST /v2/enrollment-customizations":                      "create",
		"PUT /v2/enrollment-customizations/{id}":                  "update",
		"DELETE /v2/enrollment-customizations/{id}":               "delete",
		"POST /v1/enrollment-customization/{id}/ldap":             "ldap-create",
		"PUT /v1/enrollment-customization/{id}/ldap/{panel-id}":   "ldap-update",
		"DELETE /v1/enrollment-customization/{id}/all/{panel-id}": "all-delete",
	}
	for _, op := range ops {
		key := op.Method + " " + op.Path
		if got := op.Name; got != want[key] {
			t.Errorf("%s = %q, want %q", key, got, want[key])
		}
	}
	// Nothing may share a name, or dedupeOperations drops whichever it reaches
	// second — which is the whole defect.
	seen := map[string]string{}
	for _, op := range ops {
		if prev, dup := seen[op.Name]; dup {
			t.Errorf("%q is held by both %s and %s %s", op.Name, prev, op.Method, op.Path)
		}
		seen[op.Name] = op.Method + " " + op.Path
	}
}

// A collision with no root operation in it is left alone: there is no
// precedence to apply and no answer this rule can supply.
func TestQualifyDuplicateVerbsLeavesACollisionWithNoRootOperation(t *testing.T) {
	ops := []*Operation{
		{Name: "upload", Method: "POST", Path: "/v1/packages/{id}/manifest"},
		{Name: "upload", Method: "POST", Path: "/v1/packages/{id}/upload"},
	}
	qualifyDuplicateVerbsOutsideTheRoot(ops, []string{"packages"})
	for _, op := range ops {
		if op.Name != "upload" {
			t.Errorf("%s %s = %q, want it left as \"upload\"", op.Method, op.Path, op.Name)
		}
	}
}

// A GET is left to dedupeOperations, whose collection-path preference is what
// resourceGetDetailPathOverrides depends on: renaming the loser there splits one
// `get` into two commands and takes the override's default path with it.
func TestQualifyDuplicateVerbsLeavesGETsToDedupe(t *testing.T) {
	ops := []*Operation{
		{Name: "get", Method: "GET", Path: "/v4/computers-inventory-detail/{id}"},
		{Name: "get", Method: "GET", Path: "/v4/computers-inventory/{id}"},
	}
	qualifyDuplicateVerbsOutsideTheRoot(ops, []string{"computers-inventory"})
	for _, op := range ops {
		if op.Name != "get" {
			t.Errorf("%s %s = %q, want it left as \"get\"", op.Method, op.Path, op.Name)
		}
	}
}

// Three methods on one sub-path against a collection-level sibling on the same
// terminal segment: the path cannot separate them, so the method does.
//
// Without the fallback the second and third both keep `scope-by-id` and
// dedupeOperations drops them — POST and PUT on both prestage scopes, four
// writes.
func TestDisambiguateSameTerminalOpsSeparatesThreeMethodsOnOnePath(t *testing.T) {
	ops := []*Operation{
		{Name: "scope", Method: "GET", Path: "/v2/computer-prestages/scope"},
		{Name: "scope-by-id", Method: "GET", Path: "/v2/computer-prestages/{id}/scope"},
		{Name: "scope-by-id", Method: "POST", Path: "/v2/computer-prestages/{id}/scope"},
		{Name: "scope-by-id", Method: "PUT", Path: "/v2/computer-prestages/{id}/scope"},
	}
	// The pass keys on the derived name, so start them all from the collision
	// the parser produces.
	for _, op := range ops[1:] {
		op.Name = "scope"
	}
	disambiguateSameTerminalOps(ops, nil)

	want := map[string]string{
		"GET /v2/computer-prestages/scope":       "scope",
		"GET /v2/computer-prestages/{id}/scope":  "scope-by-id",
		"POST /v2/computer-prestages/{id}/scope": "create-scope",
		"PUT /v2/computer-prestages/{id}/scope":  "update-scope",
	}
	for _, op := range ops {
		key := op.Method + " " + op.Path
		if op.Name != want[key] {
			t.Errorf("%s = %q, want %q", key, op.Name, want[key])
		}
	}
}
