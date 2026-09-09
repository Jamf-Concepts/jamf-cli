// Copyright 2026, Jamf Software LLC

package parser

import "testing"

// TestNoParamRootKeepsThePlainVerb pins which side of a no-param GET collision
// keeps `get`/`list`.
//
// resolveNoParamConflicts renames a colliding no-param GET to its terminal path
// segment. It exempted a path with a /{param} child, which was enough while one
// spec file meant one path root — /v3/sso, /v2/sso/cert and /v1/sso/failover
// were three resources with a clean get/update each. Tag grouping merges them
// into one, so the rename fired on both sides and the resource's primary
// endpoint lost its verb: `pro sso-settings sso` beside `cert`, and
// `pro enrollment enrollment` beside `language-codes`. 15 resources, 19
// operations.
//
// The three cases below are the rule's whole shape, and each was got wrong by an
// earlier attempt:
//
//   - sso-settings: a root with children, and the fix.
//   - enrollment: a root whose collision group holds a sibling from outside its
//     subtree (/v1/adue-session-token-settings). Requiring the root to prefix
//     every colliding path found no root here; judging candidacy against every
//     no-param GET on the resource rather than the group's own members is what
//     fixed it.
//   - ldap: three peer sub-collections with no GET /v1/ldap between them.
//     "Shortest path wins" handed `list` to /v1/ldap/groups, so `pro ldap list`
//     returned LDAP groups — a plain verb pointing at one arbitrary
//     sub-collection, which is worse than the stutter, because nothing in the
//     name says which one. A resource with no root keeps terminal-segment
//     naming for all of it.
func TestNoParamRootKeepsThePlainVerb(t *testing.T) {
	want := map[string]map[string]string{
		// resource -> endpoint -> operation name
		"sso-settings": {
			"GET /v3/sso":          "get",
			"PUT /v3/sso":          "update",
			"GET /v1/sso/failover": "failover",
			// Was `v-3-metadata-download`, disambiguated against the
			// certificate's own `download`. Taking the certificate out of the
			// resource removes the collision, so the better name falls out.
			"GET /v3/sso/metadata/download": "download",
		},
		// `/v2/sso/cert` is independently writable, so it is a sub-resource of
		// its own now and its verbs are plain — see subresource.go. Asserted
		// here rather than moved out, because the property this test pins is
		// the same one: the *resource's* root keeps the plain verb, and the
		// resource a path belongs to is what the split changed.
		"sso-settings cert": {
			"GET /v2/sso/cert":          "get",
			"PUT /v2/sso/cert":          "update",
			"DELETE /v2/sso/cert":       "delete",
			"POST /v2/sso/cert":         "create",
			"GET /v2/sso/cert/download": "download",
			"POST /v2/sso/cert/parse":   "parse",
		},
		"enrollment": {
			"GET /v4/enrollment":                "get",
			"PUT /v4/enrollment":                "update",
			"GET /v3/enrollment/language-codes": "language-codes",
		},
		// The sub-resource outside its parent's root subtree, which is the
		// shape a root-relative test cannot see.
		"enrollment adue-session-token-settings": {
			"GET /v1/adue-session-token-settings": "get",
			"PUT /v1/adue-session-token-settings": "update",
		},
		"ldap": {
			"GET /v1/ldap/groups":       "groups",
			"GET /v1/ldap/servers":      "servers",
			"GET /v1/ldap/ldap-servers": "ldap-servers",
		},
	}
	got := map[string]map[string]string{}
	for _, r := range FlattenResources(parseCommittedSpecs(t)) {
		key := r.QualifiedName()
		if _, interesting := want[key]; !interesting {
			continue
		}
		got[key] = map[string]string{}
		for _, op := range r.Operations {
			got[key][op.Method+" "+op.Path] = op.Name
		}
	}
	for res, eps := range want {
		if got[res] == nil {
			t.Errorf("resource %q was not parsed, so its operation names are unasserted", res)
			continue
		}
		for ep, name := range eps {
			if got[res][ep] != name {
				t.Errorf("%s: %s is named %q, want %q", res, ep, got[res][ep], name)
			}
		}
	}
}

// TestIsResourceRootPath covers the rule the exemption is built on, including
// the three shapes an earlier collision-derived version got wrong.
func TestIsResourceRootPath(t *testing.T) {
	for _, tc := range []struct {
		name    string
		path    string
		root    []string
		want    bool
		because string
	}{
		{"the resource's own root", "/sso", []string{"sso"}, true, "GET /v3/sso is sso-settings' root"},
		{"a multi-segment root", "/pki/certificate-authority", []string{"pki", "certificate-authority"}, true, ""},
		{
			"a selector below the root", "/pki/certificate-authority/active",
			[]string{"pki", "certificate-authority"},
			false,
			"there is no bare GET /v1/pki/certificate-authority, so /active is the shallowest served path and is still not the root — treating it as one named it `list` for an endpoint returning the one active CA",
		},
		{
			"a peer sub-collection", "/ldap/groups",
			[]string{"ldap"},
			false,
			"pro ldap has three peers and no GET /v1/ldap; a plain `list` would point at one of them with nothing in the name saying which",
		},
		{"a sibling outside the root's subtree", "/adue-session-token-settings", []string{"enrollment"}, false, ""},
		{"a deeper path under the root", "/enrollment/language-codes", []string{"enrollment"}, false, ""},
		{
			"no root supplied", "/anything", nil, false,
			"the per-file ParseSpec path and the platform parser pass none, and must keep the naming they had",
		},
	} {
		if got := isResourceRootPath(tc.path, tc.root); got != tc.want {
			t.Errorf("%s: isResourceRootPath(%q, %v) = %v, want %v — %s",
				tc.name, tc.path, tc.root, got, tc.want, tc.because)
		}
	}
}
