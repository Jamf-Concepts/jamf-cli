// Copyright 2026, Jamf Software LLC

package gateway

import (
	"strings"
	"testing"
)

// fixture mirrors the shape of a real manifest: Pro paths (one declaring GET
// only), Classic paths, and scopes on some but not all operations — 44 Jamf Pro
// endpoints are unauthenticated and declare none.
func fixture() *Coverage {
	return &Coverage{
		Sources: Sources{
			Pro:     SpecSource{Title: "Jamf Pro API", Version: "11.31.0", Paths: 2},
			Classic: SpecSource{Title: "Classic API", Version: "11.28.0", Paths: 1},
		},
		Spec: map[string][]string{
			"/pro/v1/categories":         {"GET", "POST"},
			"/pro/v2/mdm/commands":       {"GET"},
			"/pro/v1/scripts":            {"GET"},
			"/pro/v1/health-check":       {"GET"},
			"/proclassic/policies":       {"GET", "POST"},
			"/proclassic/policies/id/{}": {"GET"},
		},
		Scopes: map[string]map[string][]string{
			"/pro/v1/categories":   {"GET": {"categories:read"}, "POST": {"categories:create"}},
			"/pro/v2/mdm/commands": {"GET": {"device-actions:read"}},
			"/proclassic/policies": {"GET": {"policies:read"}},
		},
	}
}

func TestVerdictServedCarriesItsScopes(t *testing.T) {
	v := fixture().Verdict("GET", "/pro/v1/categories")
	if v.Level != Served {
		t.Fatalf("declared path: got %q, want Served", v.Level)
	}
	if len(v.Scopes) != 1 || v.Scopes[0] != "categories:read" {
		t.Errorf("scopes: got %v, want [categories:read]", v.Scopes)
	}
}

// An operation with no x-required-privileges is not an absent operation: 44 Jamf
// Pro endpoints are unauthenticated (/v1/health-check, /v1/jamf-pro-version,
// /v1/locales) and declare no scope at all.
func TestNoScopeIsNotAbsence(t *testing.T) {
	v := fixture().Verdict("GET", "/pro/v1/health-check")
	if v.Level != Served {
		t.Fatalf("declared but scope-free: got %q, want Served", v.Level)
	}
	if len(v.Scopes) != 0 {
		t.Errorf("scopes: got %v, want none", v.Scopes)
	}
}

// Absence from the published artefacts refuses, and carries the unpublished
// basis so the message can say the endpoint may still answer today.
//
// The gateway does currently route several of these — api-roles returns 15
// roles, api-integrations 81 — and that is the reason to refuse rather than a
// reason not to: the route set is being narrowed onto the published surface, so
// letting them through means a workflow gets built on an endpoint that is going
// away and the failure arrives later with no explanation.
func TestDerivedAbsenceRefusesAsUnpublished(t *testing.T) {
	cov := fixture()
	for _, tc := range []struct{ method, path string }{
		{"GET", "/pro/v1/cache-settings"},  // absent from both artefacts
		{"POST", "/pro/v1/scripts"},        // path declared, method not
		{"GET", "/proclassic/nosuchthing"}, // Classic, absent
	} {
		v := cov.Verdict(tc.method, tc.path)
		if v.Level != Unserved {
			t.Errorf("%s %s: got %q, want Unserved", tc.method, tc.path, v.Level)
		}
		if v.Basis != BasisUnpublished {
			t.Errorf("%s %s: basis %q, want %q", tc.method, tc.path, v.Basis, BasisUnpublished)
		}
		if v.Detail == "" {
			t.Errorf("%s %s: refused with no evidence to quote", tc.method, tc.path)
		}
	}
}

// A probe states the fact; the derived verdict hedges. Same refusal, different
// wording, which is the only thing Basis controls.
//
// Tested through a temporary entry rather than a shipped one, because
// probedUnserved is now empty: its only entry, /pro/v1/app-installers, was
// removed when public-apis-oas#430 published that surface and the gateway
// opened it on 2026-09-03. Same reasoning as forceServed below — a table whose
// whole point is to be empty most of the time cannot be covered by whatever
// happens to be in it, and the wording it selects still has to be right for the
// next probe.
func TestProbedEntriesCarryTheProbeBasis(t *testing.T) {
	const probed = "/pro/v1/probed-example"
	probedUnserved[probed] = "wire-confirmed unserved on EU and US, 2026-08-28"
	defer delete(probedUnserved, probed)

	cov := fixture()
	// The entry is a bare path, so it covers every method beneath it.
	for _, tc := range []struct{ method, path string }{
		{"GET", probed},
		{"POST", probed + "/deployments"},
		{"GET", probed + "/{}/history"},
	} {
		v := cov.Verdict(tc.method, tc.path)
		if v.Level != Unserved {
			t.Errorf("%s %s: got %q, want Unserved", tc.method, tc.path, v.Level)
		}
		if v.Basis != BasisProbe {
			t.Errorf("%s %s: basis %q, want %q", tc.method, tc.path, v.Basis, BasisProbe)
		}
		if v.Detail == "" {
			t.Errorf("%s %s: Unserved with no probe recorded", tc.method, tc.path)
		}
	}
	// A sibling that merely starts with the same characters must not pick up the
	// probe — it is refused as unpublished instead, which is a different message.
	if v := cov.Verdict("GET", probed+"-elsewhere"); v.Basis == BasisProbe {
		t.Error("prefix match bled into a sibling path")
	}
}

// forceServed is empty by policy, so the escape hatch is tested through the
// matcher both tables share rather than through a live entry. The two things
// that must hold: an entry covers its whole subtree, and it does not leak into a
// sibling whose name merely starts the same way.
func TestOverrideMatcherCoversASubtreeWithoutLeaking(t *testing.T) {
	table := map[string]string{
		"/pro/v1/thing":         "bare path covers every method beneath it",
		"POST /pro/v1/other/{}": "method-qualified, exact",
	}
	cases := []struct {
		method, path string
		want         bool
	}{
		{"GET", "/pro/v1/thing", true},
		{"DELETE", "/pro/v1/thing/5/sub", true},
		{"GET", "/pro/v1/thingamajig", false},
		{"POST", "/pro/v1/other/{}", true},
		{"GET", "/pro/v1/other/{}", false},
	}
	for _, c := range cases {
		if _, got := matchPrefix(table, c.method, c.path); got != c.want {
			t.Errorf("matchPrefix(%s %s) = %v, want %v", c.method, c.path, got, c.want)
		}
	}
}

// No manifest is the "unknown" answer, not a licence to refuse.
func TestNilCoverageStampsNothing(t *testing.T) {
	var cov *Coverage
	if v := cov.Verdict("GET", "/pro/v1/categories"); v.Level != Served {
		t.Fatalf("nil coverage: got %q, want Served", v.Level)
	}
	// A probe still applies — it does not depend on the manifest. Installed for
	// the test, probedUnserved being empty; see TestProbedEntriesCarryTheProbeBasis.
	const probed = "/pro/v1/probed-example"
	probedUnserved[probed] = "wire-confirmed unserved on EU and US, 2026-08-28"
	defer delete(probedUnserved, probed)
	if v := cov.Verdict("GET", probed+"/titles"); v.Level != Unserved {
		t.Fatalf("nil coverage, probed path: got %q, want Unserved", v.Level)
	}
}

// Five Classic resources have no bare collection endpoint (computerhistory,
// computerapplications, mobiledevicehistory, patchavailabletitles,
// patchreports), so the exact-path form reported them absent. Subtree is the
// right question for a Classic resource.
func TestVerdictSubtreeFindsAResourceWithNoCollectionPath(t *testing.T) {
	cov := &Coverage{
		Sources: Sources{Classic: SpecSource{Title: "Classic API", Version: "11.28.0", Paths: 273}},
		Spec:    map[string][]string{"/proclassic/computerhistory/id/{}": {"GET"}},
	}
	if v := cov.VerdictSubtree("/proclassic/computerhistory"); v.Level != Served {
		t.Errorf("resource with only an /id/{} endpoint: got %q, want Served", v.Level)
	}
	v := cov.VerdictSubtree("/proclassic/computerconfigurations")
	if v.Level != Unserved || v.Basis != BasisUnpublished {
		t.Errorf("genuinely absent resource: got %q/%q, want Unserved/unpublished", v.Level, v.Basis)
	}
}

func TestApplyStampsAndDeduplicates(t *testing.T) {
	cov := fixture()
	var got []string
	ops := []Op{
		{Method: "GET", GatewayPath: "/pro/v1/app-installers/titles/{id}", Set: func(v Verdict) { got = append(got, string(v.Level)) }},
		{Method: "GET", GatewayPath: "/pro/v1/app-installers/titles/{titleId}", Set: func(v Verdict) { got = append(got, string(v.Level)) }},
		{Method: "GET", GatewayPath: "/pro/v1/categories", Set: func(v Verdict) { got = append(got, string(v.Level)) }},
	}
	entries := Apply(cov, ops)
	if len(got) != 3 {
		t.Fatalf("Set called %d times, want 3", len(got))
	}
	// The two app-installer ops normalise to the same key, so one entry.
	if len(entries) != 1 {
		t.Fatalf("entries: got %d (%v), want 1", len(entries), entries)
	}
	if entries[0].Path != "/pro/v1/app-installers/titles/{}" {
		t.Errorf("path: got %q, want the normalised form", entries[0].Path)
	}
}

func TestApplyWildcardJudgesTheSubtree(t *testing.T) {
	cov := &Coverage{
		Sources: Sources{Classic: SpecSource{Title: "Classic API", Version: "11.28.0", Paths: 273}},
		Spec:    map[string][]string{"/proclassic/computerhistory/id/{}": {"GET"}},
	}
	entries := Apply(cov, []Op{
		{Method: "GET", GatewayPath: "/proclassic/computerhistory", Scope: ScopeSubtree, Set: func(Verdict) {}},
		{Method: "GET", GatewayPath: "/proclassic/computerconfigurations", Scope: ScopeSubtree, Set: func(Verdict) {}},
	})
	if len(entries) != 1 {
		t.Fatalf("entries: got %d (%v), want 1", len(entries), entries)
	}
	if entries[0].Method != AnyMethod || entries[0].Path != "/proclassic/computerconfigurations/"+AnySubpath {
		t.Errorf("wildcard entry: got %s %s", entries[0].Method, entries[0].Path)
	}
}

// A Classic resource can keep one method and lose the rest, which the
// whole-resource verdict reports as fine. Classic 11.28.0 did exactly that to
// patchsoftwaretitles: POST /patchsoftwaretitles/id/{} survived and every read
// and write on it was withdrawn.
func TestVerdictSubtreeMethodCatchesAMethodWithdrawnFromAServedResource(t *testing.T) {
	cov := &Coverage{
		Sources: Sources{Classic: SpecSource{Title: "Classic API", Version: "11.28.0"}},
		Spec:    map[string][]string{"/proclassic/patchsoftwaretitles/id/{}": {"POST"}},
	}

	if v := cov.VerdictSubtree("/proclassic/patchsoftwaretitles"); v.Level != Served {
		t.Fatalf("the resource is still carried: got %q", v.Level)
	}
	if v := cov.VerdictSubtreeMethod("/proclassic/patchsoftwaretitles", "POST"); v.Level != Served {
		t.Errorf("POST survives the withdrawal: got %q %q", v.Level, v.Detail)
	}
	for _, m := range []string{"GET", "PUT", "DELETE"} {
		v := cov.VerdictSubtreeMethod("/proclassic/patchsoftwaretitles", m)
		if v.Level != Unserved || v.Basis != BasisUnpublished {
			t.Errorf("%s: got %q/%q, want unserved/unpublished", m, v.Level, v.Basis)
		}
		if !strings.Contains(v.Detail, "no "+m) {
			t.Errorf("%s: the detail does not name the method: %q", m, v.Detail)
		}
	}
}

// The collection GET is judged exactly, not across the subtree: Classic 11.28.0
// withdrew GET /patchpolicies while keeping GET /patchpolicies/id/{}, so `list`
// is dead and `get` is not. A subtree-method verdict cannot separate those.
func TestExactVerdictSeparatesAWithdrawnCollectionGetFromASurvivingDetailGet(t *testing.T) {
	cov := &Coverage{
		Sources: Sources{Classic: SpecSource{Title: "Classic API", Version: "11.28.0"}},
		Spec:    map[string][]string{"/proclassic/patchpolicies/id/{}": {"GET", "PUT", "DELETE"}},
	}
	if v := cov.VerdictSubtreeMethod("/proclassic/patchpolicies", "GET"); v.Level != Served {
		t.Errorf("a detail GET survives, so the subtree carries GET: got %q", v.Level)
	}
	if v := cov.Verdict("GET", "/proclassic/patchpolicies"); v.Level != Unserved {
		t.Errorf("the collection GET is withdrawn: got %q", v.Level)
	}
}

// A subtree-method entry has to reach the runtime keyed by its own method, or
// the refusal is either absent or applied to every method on the resource.
func TestApplyEmitsASubtreeMethodEntryUnderThatMethod(t *testing.T) {
	cov := &Coverage{
		Sources: Sources{Classic: SpecSource{Title: "Classic API", Version: "11.28.0"}},
		Spec:    map[string][]string{"/proclassic/patchsoftwaretitles/id/{}": {"POST"}},
	}
	entries := Apply(cov, []Op{
		{Method: "GET", GatewayPath: "/proclassic/patchsoftwaretitles", Scope: ScopeSubtreeMethod, Set: func(Verdict) {}},
		{Method: "POST", GatewayPath: "/proclassic/patchsoftwaretitles", Scope: ScopeSubtreeMethod, Set: func(Verdict) {}},
	})
	if len(entries) != 1 {
		t.Fatalf("entries: got %d (%v), want 1 (POST is served)", len(entries), entries)
	}
	if entries[0].Method != "GET" || entries[0].Path != "/proclassic/patchsoftwaretitles/"+AnySubpath {
		t.Errorf("got %s %s, want GET /proclassic/patchsoftwaretitles/**", entries[0].Method, entries[0].Path)
	}
}
