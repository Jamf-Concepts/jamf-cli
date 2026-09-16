// Copyright 2026, Jamf Software LLC

package platform

import (
	"path/filepath"
	"strings"
	"testing"
)

// SDK v1.1.0 prefixed every AI Governance description with two preview banners,
// and taking the first paragraph left each of the twelve Longs as the banner
// alone — `platform ai-policies list --help` said the endpoint may change and no
// longer said what it lists. Reading past a banner to find the description is a
// property of the shape rather than of that spec.
func TestFirstParagraphReadsPastALeadingAdmonition(t *testing.T) {
	const desc = "**Preview:** _This endpoint is currently in a preview state._\n\n" +
		"**Preview endpoint.** Expected to reach general availability by 2027-03-03,\n" +
		"pending feedback.\n\n" +
		"Returns a paginated list of active AI governance policies for the\n" +
		"authenticated tenant.\n\n\n" +
		"**Required Permissions:** `ai-policies:read`"

	got := firstParagraph(desc)
	for _, want := range []string{
		"Returns a paginated list of active AI governance policies for the authenticated tenant.",
		"Preview: This endpoint is currently in a preview state.",
		"by 2027-03-03",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Long is missing %q\ngot: %s", want, got)
		}
	}
	// The grant is the jamf:privileges annotation and the 403 hint; repeating
	// it here is what a Long saying nothing but a grant name looks like.
	if strings.Contains(got, "Required Permissions") {
		t.Errorf("Long should not carry the permissions admonition\ngot: %s", got)
	}
	// Everything after the substantive paragraph still goes.
	if strings.Contains(got, "ai-policies:read") {
		t.Errorf("Long should stop at the substantive paragraph\ngot: %s", got)
	}
}

func TestFirstParagraph(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{
			name: "no admonition keeps the old behaviour",
			in:   "Deletes the thing.\n\nA second paragraph that is dropped.",
			want: "Deletes the thing.",
		},
		{
			name: "a wrapped paragraph reads as one line",
			in:   "Deletes\nthe   thing.",
			want: "Deletes the thing.",
		},
		{
			name: "bold mid-sentence is not an admonition",
			in:   "Returns `404` if the gateway does not exist **or belongs to another tenant**.",
			want: "Returns `404` if the gateway does not exist or belongs to another tenant.",
		},
		{
			name: "an admonition with nothing substantive after it is all there is to say",
			in:   "**Deprecated:** use the v2 endpoint.",
			want: "Deprecated: use the v2 endpoint.",
		},
		{
			name: "a permissions-only description renders empty rather than as a grant name",
			in:   "**Required Permissions:** `ai-policies:read`",
			want: "",
		},
		{name: "empty", in: "", want: ""},
		{name: "whitespace only", in: "   \n\n  ", want: ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := firstParagraph(c.in); got != c.want {
				t.Errorf("firstParagraph:\n got %q\nwant %q", got, c.want)
			}
		})
	}
}

func TestAdmonitionLabel(t *testing.T) {
	cases := []struct {
		in        string
		wantLabel string
		wantOK    bool
	}{
		{"**Preview:** it may change.", "Preview", true},
		{"**Preview endpoint.** Expected to graduate.", "Preview endpoint", true},
		{"**Required Permissions:** `x:read`", "Required Permissions", true},
		// A bolded word opening a sentence is not a callout.
		{"**Deletes** the thing.", "", false},
		{"Plain prose.", "", false},
		{"**unterminated bold", "", false},
		{"****", "", false},
	}
	for _, c := range cases {
		label, ok := admonitionLabel(c.in)
		if label != c.wantLabel || ok != c.wantOK {
			t.Errorf("admonitionLabel(%q) = (%q, %v), want (%q, %v)", c.in, label, ok, c.wantLabel, c.wantOK)
		}
	}
}

// Cobra dumps Long verbatim to a terminal, so a markdown marker is punctuation
// the reader has to look past. Backticks stay — they quote a field or a value —
// and a snake_case identifier has to keep both of its own underscores.
func TestStripMarkdownEmphasis(t *testing.T) {
	cases := []struct{ in, want string }{
		{"**Preview:** _not stable_ yet.", "Preview: not stable yet."},
		{"a `declarationIdentifier` field", "a `declarationIdentifier` field"},
		{"filter on smart_group_id or user_name", "filter on smart_group_id or user_name"},
		{"wildcard `declarationIdentifier==Blueprint_*` matches", "wildcard `declarationIdentifier==Blueprint_*` matches"},
		{"(_parenthesised_) and end _italic_", "(parenthesised) and end italic"},
		{"no emphasis here", "no emphasis here"},
		{"an_unpaired_ underscore run", "an_unpaired_ underscore run"},
	}
	for _, c := range cases {
		if got := stripMarkdownEmphasis(c.in); got != c.want {
			t.Errorf("stripMarkdownEmphasis(%q):\n got %q\nwant %q", c.in, got, c.want)
		}
	}
}

// x-preview is per operation as of SDK v1.1.0, and the annotation is what a
// consumer should key on rather than the "Preview - " prefix upstream renders
// into the summary — prose nobody here controls, on the surface most likely to
// move. Asserted against the live specs so it cannot pass vacuously.
func TestPreviewOperationsAreStampedFromTheSpec(t *testing.T) {
	specsDir, err := filepath.Abs("../../specs/platform")
	if err != nil {
		t.Fatalf("resolving specs dir: %v", err)
	}
	resources, _, err := LoadResources(specsDir)
	if err != nil {
		t.Fatalf("LoadResources: %v", err)
	}

	var preview, total int
	for _, r := range resources {
		for _, op := range r.AllOperations() {
			total++
			if op.Preview {
				preview++
			}
		}
	}
	if total == 0 {
		t.Fatal("no platform operations parsed")
	}
	// AI Governance declares all twelve operations preview; if upstream
	// graduates them this drops to zero and the read stops being exercised.
	if preview == 0 {
		t.Errorf("no operation carries x-preview — either the specs graduated (update this test) or the read broke")
	}
	if preview == total {
		t.Errorf("every one of %d operations is preview, which no drop has ever been — suspect the read", total)
	}
}
