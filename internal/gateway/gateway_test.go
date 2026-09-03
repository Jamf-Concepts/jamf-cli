// Copyright 2026, Jamf Software LLC

package gateway

import (
	"strings"
	"testing"
)

func TestMatchPathSegmentWise(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		{"/pro/v1/categories", "/pro/v1/categories", true},
		{"/pro/v1/categories", "/pro/v1/categories/7", false},
		{"/pro/v1/app-installers/titles/{}", "/pro/v1/app-installers/titles/7", true},
		{"/pro/v1/app-installers/titles/{}", "/pro/v1/app-installers/titles", false},
		{"/pro/v1/app-installers/titles/{}", "/pro/v1/app-installers/titles/7/extra", false},

		// A pattern must not bleed into a sibling whose name merely starts the
		// same way — the reason this is segment-wise rather than a prefix test.
		{"/pro/v1/auth", "/pro/v1/authentication-settings", false},
		{"/pro/v1/auth", "/pro/v1/auth", true},

		// Classic entries are whole-subtree, because a Classic path is built at
		// runtime from the resource plus whichever lookup is in play.
		{"/proclassic/computerconfigurations/**", "/proclassic/computerconfigurations", true},
		{"/proclassic/computerconfigurations/**", "/proclassic/computerconfigurations/id/3", true},
		{"/proclassic/computerconfigurations/**", "/proclassic/computerconfigurations/name/Foo", true},
		{"/proclassic/computerconfigurations/**", "/proclassic/computergroups", false},
	}
	for _, c := range cases {
		if got := matchPath(c.pattern, c.path); got != c.want {
			t.Errorf("matchPath(%q, %q) = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
}

func TestLookupIgnoresAQueryString(t *testing.T) {
	// api-roles is refused as unpublished and is a paginated list, so it is a
	// request the CLI really sends with a query string attached. It replaced
	// app-installers here, which was the stable example until the gateway
	// started serving it on 2026-09-03.
	if _, ok := Lookup("GET", "/pro/v1/api-roles?page=0&page-size=100"); !ok {
		t.Error("a query string defeated the lookup")
	}
}

// The surface that used to be this package's canonical refusal. Nothing about
// the lookup changed; the manifest did.
func TestAppInstallersAreNoLongerRefused(t *testing.T) {
	for _, path := range []string{
		"/pro/v1/app-installers",
		"/pro/v1/app-installers/titles?page=0&page-size=100",
		"/pro/v1/app-installers/deployments/7/history",
	} {
		if f, ok := Lookup("GET", path); ok {
			t.Errorf("%s is refused (%s: %s); the gateway serves App Installers as of 2026-09-03", path, f.Basis, f.Detail)
		}
	}
}

func TestLookupHonoursTheMethod(t *testing.T) {
	if _, ok := Lookup("GET", "/pro/v1/categories"); ok {
		t.Error("a served path was reported as omitted")
	}
}

func TestLookupWildcardMethodMatchesAnything(t *testing.T) {
	// The Classic entries carry Method "*".
	f, ok := Lookup("DELETE", "/proclassic/computerconfigurations/id/3")
	if !ok {
		t.Fatal("wildcard-method entry did not match DELETE")
	}
	if f.Level != Unserved || f.Basis != BasisUnpublished {
		t.Errorf("got %q/%q, want Unserved/unpublished", f.Level, f.Basis)
	}
}

// Every entry is refused, so every entry has to carry the evidence its message
// will quote — and a basis, which is the only thing that selects the wording.
func TestEveryEntryCarriesEvidenceAndABasis(t *testing.T) {
	if len(unserved) == 0 {
		t.Skip("no coverage table generated")
	}
	for _, f := range unserved {
		if f.Level != Unserved {
			t.Errorf("%s %s: level %q — the table holds refusals only", f.Method, f.Path, f.Level)
		}
		if f.Detail == "" {
			t.Errorf("%s %s: refused with no evidence to quote", f.Method, f.Path)
		}
		if f.Basis != BasisProbe && f.Basis != BasisUnpublished {
			t.Errorf("%s %s: basis %q selects no wording", f.Method, f.Path, f.Basis)
		}
	}
}

// A probe states the fact. An unpublished endpoint may well answer today, so its
// message says the routing is transitional instead of claiming it is gone —
// getting these the wrong way round is how an operator concludes the CLI is
// wrong when their command demonstrably works.
func TestNoteWordsTheTwoBasesDifferently(t *testing.T) {
	if got := Note(Served, BasisUnpublished, "anything"); got != "" {
		t.Errorf("Served: got %q, want no note", got)
	}
	probe := Note(Unserved, BasisProbe, "wire-confirmed unserved 2026-08-28")
	if !strings.Contains(probe, "does not serve") {
		t.Errorf("probe note does not state the fact: %q", probe)
	}
	if strings.Contains(probe, "may answer today") {
		t.Errorf("probe note hedges about an endpoint that was probed: %q", probe)
	}
	unpub := Note(Unserved, BasisUnpublished, "not declared by the gateway's Jamf Pro API 11.31.0")
	for _, want := range []string{"not part of the Jamf Platform gateway's published API", "may answer today", "transitional"} {
		if !strings.Contains(unpub, want) {
			t.Errorf("unpublished note is missing %q: %q", want, unpub)
		}
	}
}

func TestRefusalNamesTheCommandAndTheRemedy(t *testing.T) {
	// No shipped entry carries the probe basis — probedUnserved is empty since
	// App Installers moved onto the gateway — so the command name here is
	// hypothetical. The wording it selects still has to be right for the next
	// probe, which is why it is pinned rather than deleted.
	probe := Refusal("pro example-resource list", BasisProbe, "wire-confirmed unserved on EU and US, 2026-08-28")
	for _, want := range []string{
		"pro example-resource list",
		"is not served by the Jamf Platform gateway",
		"Wire-confirmed unserved",
		"auth-method is oauth2 or token",
	} {
		if !strings.Contains(probe, want) {
			t.Errorf("probe refusal is missing %q:\n%s", want, probe)
		}
	}

	// The unpublished refusal has the harder job: the command may work today, so
	// it has to say why it is being stopped anyway or it reads as a CLI bug.
	unpub := Refusal("pro api-roles list", BasisUnpublished, "not declared by the gateway's Jamf Pro API 11.31.0")
	for _, want := range []string{
		"pro api-roles list",
		"published API",
		"may answer today",
		"transitional",
		"auth-method is oauth2 or token",
	} {
		if !strings.Contains(unpub, want) {
			t.Errorf("unpublished refusal is missing %q:\n%s", want, unpub)
		}
	}
}
