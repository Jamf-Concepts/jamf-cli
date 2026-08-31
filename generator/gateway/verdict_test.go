// Copyright 2026, Jamf Software LLC

package gateway

import "testing"

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
func TestProbedEntriesCarryTheProbeBasis(t *testing.T) {
	cov := fixture()
	// The entry is a bare path, so it covers every method beneath it.
	for _, tc := range []struct{ method, path string }{
		{"GET", "/pro/v1/app-installers/titles"},
		{"POST", "/pro/v1/app-installers/deployments"},
		{"GET", "/pro/v1/app-installers"},
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
	if v := cov.Verdict("GET", "/pro/v1/app-installers-elsewhere"); v.Basis == BasisProbe {
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
	// A probe still applies — it does not depend on the manifest.
	if v := cov.Verdict("GET", "/pro/v1/app-installers/titles"); v.Level != Unserved {
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
		{Method: "GET", GatewayPath: "/pro/v1/app-installers/titles/{id}", Set: func(l, b, d string) { got = append(got, l) }},
		{Method: "GET", GatewayPath: "/pro/v1/app-installers/titles/{titleId}", Set: func(l, b, d string) { got = append(got, l) }},
		{Method: "GET", GatewayPath: "/pro/v1/categories", Set: func(l, b, d string) { got = append(got, l) }},
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
		{Method: "GET", GatewayPath: "/proclassic/computerhistory", Wildcard: true, Set: func(string, string, string) {}},
		{Method: "GET", GatewayPath: "/proclassic/computerconfigurations", Wildcard: true, Set: func(string, string, string) {}},
	})
	if len(entries) != 1 {
		t.Fatalf("entries: got %d (%v), want 1", len(entries), entries)
	}
	if entries[0].Method != AnyMethod || entries[0].Path != "/proclassic/computerconfigurations/"+AnySubpath {
		t.Errorf("wildcard entry: got %s %s", entries[0].Method, entries[0].Path)
	}
}
