// Copyright 2026, Jamf Software LLC

package parser

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// TestSubResourcePartitionIsPinned is the reproducibility guard for the
// sub-resource split, and it is the whole reason the rule is derived rather
// than tabulated.
//
// Which sub-paths are independently writable decides how deep a command sits,
// so a spec drop that adds a `DELETE` to a sub-path — or takes a `PUT` away —
// moves commands. Nothing else reports that: `make generate` exits 0 and the
// renamed command simply appears at a different depth, which is exactly how the
// filename-derived names went wrong for years.
//
// Both halves are asserted. The admitted set is nine, listed with the endpoint
// that qualified each one. The excluded set is every other no-param sub-path
// answering more than one method — the candidates that *look* like objects —
// because a rule is only pinned by what it refuses.
func TestSubResourcePartitionIsPinned(t *testing.T) {
	// resource -> sub-resource command path -> the methods its own root answers.
	wantAdmitted := map[string]string{
		"activation-code organization-name":             "PATCH /v1/activation-code/organization-name",
		"app-installers global-settings":                "GET,PUT /v1/app-installers/global-settings",
		"csa token":                                     "DELETE,GET /v1/csa/token",
		"enrollment adue-session-token-settings":        "GET,PUT /v1/adue-session-token-settings",
		"local-admin-password settings":                 "GET,PUT /v2/local-admin-password/settings",
		"managed-software-updates-plans feature-toggle": "GET,PUT /v1/managed-software-updates/plans/feature-toggle",
		"self-service settings":                         "GET,PUT /v1/self-service/settings",
		"self-service-plus settings":                    "GET,PUT /v1/self-service-plus/settings",
		"sso-settings cert":                             "DELETE,GET,POST,PUT /v2/sso/cert",
	}

	resources := parseCommittedSpecs(t)
	if len(resources) == 0 {
		t.Fatal("no resources parsed, so this guard would pass vacuously")
	}

	gotAdmitted := map[string]string{}
	for _, r := range resources {
		for _, sub := range r.SubResources {
			root := resourceRootPath(sub)
			var methods []string
			path := ""
			for _, op := range sub.Operations {
				if stripVersionSegments(op.Path) != root {
					continue
				}
				methods = append(methods, op.Method)
				path = op.Path
			}
			sort.Strings(methods)
			gotAdmitted[sub.QualifiedName()] = strings.Join(methods, ",") + " " + path
		}
	}
	for name, want := range wantAdmitted {
		got, ok := gotAdmitted[name]
		if !ok {
			t.Errorf("sub-resource %q is no longer split out — its verbs are back on its parent, where a plain one reads as the parent's", name)
			continue
		}
		if got != want {
			t.Errorf("sub-resource %q qualified on %q, want %q", name, got, want)
		}
	}
	for name := range gotAdmitted {
		if _, ok := wantAdmitted[name]; !ok {
			t.Errorf("sub-resource %q is new: a spec drop made a sub-path independently writable, so its "+
				"commands moved one token deeper. Add it here and to CHANGELOG.md's migration table, or "+
				"establish that the rule should not have admitted it", name)
		}
	}

	// The refusals. Restricted to a sub-path answering more than one method,
	// which is the candidate set a reader would suspect: a POST-only `/export`
	// or a GET-only `/status` is plainly an operation, and enumerating all 131
	// of those would pin the document rather than the rule.
	excluded := multiMethodExclusions(t, resources)
	if len(excluded) == 0 {
		t.Fatal("no multi-method sub-path was excluded, so the rule's refusals are unasserted")
	}

	// Composition, not just a count. 18 are GET+POST `/history` pairs, refused
	// because a POST is an append; the count is asserted separately so that a
	// `history` special case creeping in would have to move it. The other five
	// are named individually, because each refuses for a reason worth keeping.
	var histories int
	nonHistory := map[string]string{}
	for _, e := range excluded {
		if strings.HasSuffix(e.path, "/history") {
			histories++
			continue
		}
		nonHistory[e.path] = e.methods
	}
	if got, want := histories, 18; got != want {
		t.Errorf("%d of the exclusions are /history, want %d — the exclusion is by the write-method rule and "+
			"not by a `history` special case, so this count moving means the rule moved:\n  %s",
			got, want, strings.Join(exclusionLines(excluded), "\n  "))
	}
	wantNonHistory := map[string]string{
		// GET+POST: reading and submitting an access-management record.
		"/enrollment/access-management": "GET,POST",
		// GET+POST: downloading and uploading the preload CSV.
		"/inventory-preload/csv": "GET,POST",
		// GET+POST: reading the MDM command log and issuing a command. Note
		// `commands` is also the name `renameLoneNonCanonicalList` must not
		// take from the POST.
		"/mdm/commands": "GET,POST",
		// GET+PUT, so the write rule admits them — refused by the
		// non-degeneracy clause, each being the entire resource. This is the
		// pair that separates this rule from the one the design brief measured,
		// which would have split both.
		"/app-request/settings":                             "GET,PUT",
		"/service-discovery-enrollment/well-known-settings": "GET,PUT",
	}
	for path, want := range wantNonHistory {
		got, ok := nonHistory[path]
		if !ok {
			t.Errorf("%s is no longer a refused multi-method sub-path — it either split out or stopped "+
				"answering two methods", path)
			continue
		}
		if got != want {
			t.Errorf("%s answers %s, want %s", path, got, want)
		}
	}
	for path, methods := range nonHistory {
		if _, ok := wantNonHistory[path]; !ok {
			t.Errorf("%s (%s) is a new multi-method sub-path the rule refuses; record why it should stay "+
				"flat, or establish that it should split", path, methods)
		}
	}
}

type subPathExclusion struct {
	resource string
	path     string
	methods  string
}

func exclusionLines(xs []subPathExclusion) []string {
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		out = append(out, fmt.Sprintf("%-40s %-8s %s", x.resource, x.methods, x.path))
	}
	return out
}

// multiMethodExclusions collects the no-param sub-paths that answer more than
// one method and were nonetheless left flat.
func multiMethodExclusions(t *testing.T, resources []*Resource) []subPathExclusion {
	t.Helper()
	var out []subPathExclusion
	// Flattened, because two of the /history pairs sit *inside* a sub-resource
	// (`/app-installers/global-settings/history` and
	// `/self-service/settings/history`) and reading only the parents reported
	// 16 of them rather than 18.
	for _, r := range FlattenResources(resources) {
		roots := map[string]bool{}
		for _, sub := range r.SubResources {
			roots[resourceRootPath(sub)] = true
		}
		methodsAt := map[string]map[string]bool{}
		for _, op := range r.Operations {
			p := stripVersionSegments(op.Path)
			if strings.Contains(p, "{") {
				continue
			}
			if methodsAt[p] == nil {
				methodsAt[p] = map[string]bool{}
			}
			methodsAt[p][op.Method] = true
		}
		rootPath := resourceRootPath(r)
		for _, p := range sortedKeys(methodsAt) {
			if p == rootPath || roots[p] || len(methodsAt[p]) < 2 {
				continue
			}
			var ms []string
			for m := range methodsAt[p] {
				ms = append(ms, m)
			}
			sort.Strings(ms)
			out = append(out, subPathExclusion{r.QualifiedName(), p, strings.Join(ms, ",")})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].resource+out[i].path < out[j].resource+out[j].path })
	return out
}

// resourceRootPath reads the root off the resource.
//
// An earlier version of this guard derived it as "the shortest no-param path
// the resource serves", and that is wrong in both directions — it answered
// `/inventory-preload/csv` for a resource whose declared root is dropped and
// `/mdm/commands` for `pro mdm`, so this test called two genuine exclusions
// roots and reported five fewer refusals than the rule makes. Resource.Root is
// carried for exactly that reason.
func resourceRootPath(r *Resource) string {
	return "/" + strings.Join(r.Root, "/")
}

// TestSubResourceRoots covers the rule directly, including each disqualifier.
//
// Unit cases rather than only the live partition, because the live document
// exercises the non-degeneracy clause exactly twice and the nesting exclusion
// not at all — a rule with untested branches is a rule that moves silently on
// the next spec drop.
func TestSubResourceRoots(t *testing.T) {
	op := func(method, path string) *Operation {
		return &Operation{Method: method, Path: path, Name: "x"}
	}
	for _, tc := range []struct {
		name    string
		root    []string
		ops     []*Operation
		want    []string
		because string
	}{
		{
			name: "a writable sub-path splits",
			root: []string{"sso"},
			ops: []*Operation{
				op("GET", "/v3/sso"), op("PUT", "/v3/sso"),
				op("GET", "/v2/sso/cert"), op("PUT", "/v2/sso/cert"),
			},
			want: []string{"/sso/cert"},
		},
		{
			name: "a GET+POST sub-path does not",
			root: []string{"sso"},
			ops: []*Operation{
				op("GET", "/v3/sso"), op("PUT", "/v3/sso"),
				op("GET", "/v3/sso/history"), op("POST", "/v3/sso/history"),
			},
			want:    nil,
			because: "a POST is an append or a command submission, not ownership — 18 /history pairs depend on this",
		},
		{
			name: "the group's own root never splits",
			root: []string{"sso"},
			ops:  []*Operation{op("GET", "/v3/sso"), op("PUT", "/v3/sso")},
			want: nil,
		},
		{
			name: "a sub-path outside the root's subtree still splits",
			root: []string{"enrollment"},
			ops: []*Operation{
				op("GET", "/v4/enrollment"), op("PUT", "/v4/enrollment"),
				op("GET", "/v1/adue-session-token-settings"), op("PUT", "/v1/adue-session-token-settings"),
			},
			want:    []string{"/adue-session-token-settings"},
			because: "the grouping merges roots a tag covers, and this one is nowhere near /v4/enrollment",
		},
		{
			name: "a candidate the root sits inside is refused",
			root: []string{"a", "b"},
			ops: []*Operation{
				op("GET", "/v1/a/b"), op("PUT", "/v1/a"),
			},
			want:    nil,
			because: "splitting /a would take the resource's own endpoint with it",
		},
		{
			name: "only the outermost of two nested candidates",
			root: []string{"x"},
			ops: []*Operation{
				op("GET", "/v1/x"),
				op("PUT", "/v1/x/a"),
				op("PUT", "/v1/x/a/b"),
			},
			want:    []string{"/x/a"},
			because: "a second level would put a CRUD verb three tokens deep",
		},
		{
			name: "a sub-path holding every operation is refused",
			root: []string{"app-request"},
			ops: []*Operation{
				op("GET", "/v1/app-request/settings"), op("PUT", "/v1/app-request/settings"),
			},
			want: nil,
			because: "the non-degeneracy clause: there is no sibling verb for the plain one to be " +
				"confused with, so `pro app-request settings get` is a token of stutter that resolves nothing",
		},
		{
			name: "no root supplied",
			root: nil,
			ops:  []*Operation{op("GET", "/v1/x"), op("PUT", "/v1/x/y")},
			want: nil,
			because: "the per-file ParseSpec path and the platform parser pass none, and must keep the " +
				"naming they had",
		},
	} {
		got := subResourceRoots(tc.root, tc.ops)
		if strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Errorf("%s: subResourceRoots(%v) = %v, want %v — %s", tc.name, tc.root, got, tc.want, tc.because)
		}
	}
}

// TestSubResourceName covers the token a sub-path takes beneath its parent.
func TestSubResourceName(t *testing.T) {
	for _, tc := range []struct {
		root []string
		sub  string
		want string
	}{
		{[]string{"sso"}, "/sso/cert", "cert"},
		{[]string{"app-installers"}, "/app-installers/global-settings", "global-settings"},
		{[]string{"managed-software-updates", "plans"}, "/managed-software-updates/plans/feature-toggle", "feature-toggle"},
		// Outside the root's subtree, so there is no shared prefix to trim and
		// the whole path is the name — which is the name it already shipped
		// under, one level deeper.
		{[]string{"enrollment"}, "/adue-session-token-settings", "adue-session-token-settings"},
	} {
		if got := subResourceName(tc.root, tc.sub); got != tc.want {
			t.Errorf("subResourceName(%v, %q) = %q, want %q", tc.root, tc.sub, got, tc.want)
		}
	}
}

// TestSubResourceIdentityIsSelfConsistent pins the three derived identifiers,
// because each has a distinct failure mode and none of them is observable from
// a command name.
//
//   - Parent has to name a resource that exists, or QualifiedName answers for a
//     resource nothing ships and every table keyed on it silently misses.
//   - GoName has to be unique across the generated package: three sub-resources
//     are called `settings`, so a name-derived identifier would have emitted
//     `NewSettingsCmd` three times and failed to compile — which is the benign
//     version. `SelfServiceSettings` colliding with a future top-level
//     `self-service-settings` resource is the malign one.
//   - FileBase has to be unique for the same reason one file per resource is
//     what the stale-file prune assumes.
func TestSubResourceIdentityIsSelfConsistent(t *testing.T) {
	resources := parseCommittedSpecs(t)
	ApplyNameOverrides(resources)

	top := map[string]bool{}
	for _, r := range resources {
		top[r.Name] = true
	}
	goNames := map[string]string{}
	fileBases := map[string]string{}
	subs := 0
	for _, r := range FlattenResources(resources) {
		if r.Parent != "" {
			subs++
			if !top[r.Parent] {
				t.Errorf("%q names parent %q, which is not a resource this generator produces", r.QualifiedName(), r.Parent)
			}
		}
		if prev, dup := goNames[r.GoName]; dup {
			t.Errorf("GoName %q is used by both %q and %q", r.GoName, prev, r.QualifiedName())
		}
		goNames[r.GoName] = r.QualifiedName()
		if prev, dup := fileBases[r.FileBase()]; dup {
			t.Errorf("FileBase %q is used by both %q and %q", r.FileBase(), prev, r.QualifiedName())
		}
		fileBases[r.FileBase()] = r.QualifiedName()
		if want := goNameOf(r); r.GoName != want {
			t.Errorf("%q has GoName %q, want %q derived from its qualified name", r.QualifiedName(), r.GoName, want)
		}
	}
	if subs == 0 {
		t.Fatal("no sub-resource was produced, so this guard covers nothing")
	}
}

// TestNoPlainVerbLeavesItsResource is the guard for the defect the split exists
// to remove, and it is written against the whole shipped surface rather than
// against the nine resources that happened to be affected.
//
// A plain CRUD verb makes a claim: `pro sso-settings delete` says it deletes
// the SSO settings. It sent `DELETE /v2/sso/cert` and deleted the certificate.
// `pro managed-software-updates-plans update` sent
// `PUT /v1/managed-software-updates/plans/feature-toggle` on a resource whose
// `list`, `get` and `create` are real plan CRUD, so the one verb in the set
// that mutated pointed at a different object entirely — and
// `pro managed-software-updates-plans apply` was built out of it, listing plans,
// resolving a name to an id and then PUTting the document at the feature
// toggle.
//
// So: a plain verb must address the resource's own root, or a path beneath it.
// Anything else is a verb whose noun is wrong, and no other pass can see it —
// resolveNoParamConflicts renames a verb that *collides*, and every one of
// these collided with nothing.
//
// `apply` is not in the set because it is synthesized rather than parsed; it is
// covered transitively, since it is composed from `list`, `create` and `update`
// and cannot be generated when one of those is absent.
func TestNoPlainVerbLeavesItsResource(t *testing.T) {
	plainVerbs := map[string]bool{
		"list": true, "get": true, "create": true,
		"update": true, "patch": true, "delete": true,
	}
	resources := parseCommittedSpecs(t)
	checked, allowed := 0, 0
	for _, r := range FlattenResources(resources) {
		// Both sides through stripVersionSegments, because Resource.Root comes
		// from splitVersionSegment — which strips a leading `vN` and nothing
		// else — while stripVersionSegments also strips `preview`. Comparing
		// the two spellings reported every `pro team-viewer-remote-
		// administration` verb as leaving its own root.
		root := stripVersionSegments(resourceRootPath(r))
		// Post-dedupe, because that is the surface that ships: the
		// enrollment-customization panel families declare create/update/delete
		// three times over and Generate keeps one of each.
		for _, op := range dedupeOperations(r.Operations) {
			if !plainVerbs[op.Name] {
				continue
			}
			checked++
			p := stripVersionSegments(op.Path)
			if p == root || strings.HasPrefix(p, root+"/") {
				continue
			}
			if reason, ok := plainVerbsOutsideTheirRoot[op.Method+" "+op.Path]; ok {
				if reason == "" {
					t.Errorf("%s: %q sends %s %s and its allowlist entry carries no reason", r.QualifiedName(), op.Name, op.Method, op.Path)
				}
				allowed++
				continue
			}
			t.Errorf("%s: %q sends %s %s, which is outside the resource's root %s — "+
				"a plain verb whose endpoint belongs to something else is the defect the sub-resource "+
				"split exists to remove",
				r.QualifiedName(), op.Name, op.Method, op.Path, root)
		}
	}
	if checked < 300 {
		t.Fatalf("only %d plain verbs examined — the walk is not reaching the shipped surface", checked)
	}
	if allowed != len(plainVerbsOutsideTheirRoot) {
		t.Errorf("%d of the %d allowlisted endpoints were reached; a stale entry hides a real finding "+
			"the day the same shape comes back", allowed, len(plainVerbsOutsideTheirRoot))
	}
	t.Logf("%d plain verbs checked against their resource's root (%d allowlisted)", checked, allowed)
}

// plainVerbsOutsideTheirRoot are the plain verbs that address an endpoint
// outside their resource's own root and are *not* fixed by the sub-resource
// split, each with why.
//
// Every one is a tag merging two sibling path roots, which is a different shape
// from the one subResourceRoots addresses: that rule considers no-param
// sub-paths, and each of these is either a second collection root of its own or
// sits under a `{id}`. Splitting them would need a rule about parameterised
// sub-paths and a judgement about which root is the resource, and neither is
// this change.
//
// Listed so the guard covers the class rather than the nine resources the split
// touched: a *new* verb of this shape fails, and an entry that stops being
// reached fails too.
var plainVerbsOutsideTheirRoot = map[string]string{
	// The `computer-inventory` tag covers /vN/computers-inventory,
	// /v1/computer-inventory/{id}/… and /vN/computers-inventory-detail/{id} —
	// three roots and one resource, which is the case tag grouping exists for.
	// The detail endpoint is the same computer, so the verb's noun is right.
	// Only the PATCH: dedupeOperations keeps the collection's own
	// /v4/computers-inventory/{id} for `get`.
	"PATCH /v4/computers-inventory-detail/{id}": "the same computer at its detail endpoint; three path roots, one resource",

	// /v1/dss-declarations/{declarationId} is tagged with the DDM surface. It
	// is a genuinely different object from anything under /v1/ddm, and the
	// handover records the old `dss-proxies` name as simply wrong for it. A
	// judgement about which of the two roots owns `get`, not a method question.
	"GET /v1/dss-declarations/{declarationId}": "a DSS declaration under the ddm tag; needs a name decision, not a split",

	// The enrollment-customization panel families used to be here — three
	// entries reading "a panel under {id}; the singular and plural roots are one
	// tag", which is why they are worth recording as removed rather than just
	// deleted. The reason was true of the *name* and said nothing about what
	// happened to the operation the panel displaced: the customization's own
	// POST, PUT and DELETE collided with the panel's on `create`, `update` and
	// `delete`, and dedupeOperations dropped them. An allowlist entry that
	// explains why a verb may keep a foreign noun cannot be satisfied while the
	// operation it displaced is being deleted, which is what
	// TestNoPreviouslyShippedWriteIsDropped now asserts separately.
	//
	// qualifyDuplicateVerbsOutsideTheRoot resolves that collision in favour of
	// the root, so the panel writes are `ldap-create`, `sso-update`,
	// `all-delete` and so on — not plain verbs, and nothing for this guard to
	// allow.
}
