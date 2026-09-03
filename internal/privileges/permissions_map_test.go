// Copyright 2026, Jamf Software LLC

package privileges

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// catalogue.go says of itself: "No test asserts what an entry SAYS.
// TestCatalogueCoversEveryScopeThisCLISends checks only that a required
// capability HAS a row, so a wrong section or a wrong permission name is
// invisible to it." This is that test.
//
// It became possible because jamfplatform-go-sdk v0.21.0 committed a snapshot
// of the published permissions map and parses it as a privilege oracle, which
// established that the article's markdown rendering is machine-readable. The
// SDK reads only the Capability column — its parser takes cells[1] and
// discards both the Permission name and the "###" headings — so the two
// dimensions this file needs are the two nobody upstream consumes. That is
// worth knowing about what this test does and does not prove: it removes drift
// between our transcription and the article, and it does NOT verify that the
// article matches what Jamf Account's picker actually prints. Nothing
// reachable from here can do the latter.
//
// permissions-map.md beside this file is a verbatim copy fetched from
// permissionsMapURL. Refresh it with `make sync-permissions-map` and read the
// diff: a name or section moving is a real change to what an operator is told
// to look for, not housekeeping.

// mapCapabilityRow matches a row of the article's capability tables:
// | Permission name | `slug:{actions}` | Endpoints |
var mapCapabilityRow = regexp.MustCompile("^\\|\\s*(.+?)\\s*\\|\\s*`([a-z0-9-]+):\\{([a-z,]+)\\}`\\s*\\|")

// The section of the article this file reads. Bounded at both ends, because
// the page also carries an "Endpoints with no permission" list whose rows
// would otherwise parse as capabilities with no actions.
const (
	mapSectionStart = "## Find the capability for an endpoint you already call"
	mapSectionEnd   = "## Endpoints with no permission"
)

// sectionSpellingExceptions record a heading this file deliberately spells
// differently from the article, with the reason. Self-expiring: an entry that
// no longer disagrees fails the test.
var sectionSpellingExceptions = map[string]string{
	// The article's first heading is "Organization management scope"; the
	// trailing word describes the API scope level rather than naming the
	// picker's section, and terraform-provider-jamfplatform's copy spells it
	// without. Cosmetic for finding a section.
	"Organization management scope": "Organization management",
}

// publishedRow is one parsed article row.
type publishedRow struct {
	section string
	name    string
}

func parsePublishedMap(t *testing.T) map[string]publishedRow {
	t.Helper()

	raw, err := os.ReadFile("permissions-map.md")
	if err != nil {
		t.Fatalf("reading permissions-map.md: %v\n"+
			"This is the committed copy of %s. Refresh it with "+
			"`make sync-permissions-map`; do not delete this guard.", err, permissionsMapURL)
	}
	body := string(raw)

	_, after, found := strings.Cut(body, mapSectionStart)
	if !found {
		t.Fatalf("permissions-map.md has no %q heading — the published article has been "+
			"restructured, so this parse cannot be trusted. Re-read it before adjusting "+
			"the bounds.", mapSectionStart)
	}
	before, _, found := strings.Cut(after, mapSectionEnd)
	if !found {
		t.Fatalf("permissions-map.md has no %q heading — see above.", mapSectionEnd)
	}

	rows := map[string]publishedRow{}
	var section string
	for line := range strings.SplitSeq(before, "\n") {
		if strings.HasPrefix(line, "### ") {
			section = strings.TrimSpace(strings.TrimPrefix(line, "### "))
			continue
		}
		m := mapCapabilityRow.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue // heading row, alignment row, or prose
		}
		if section == "" {
			t.Errorf("row %q appears before any ### heading, so it has no section", m[2])
			continue
		}
		// A capability is declared across as many rows as its resources need
		// (compliance-benchmarks has two, "Compliance Benchmarks" and
		// "Compliance Benchmarks baseline rules"). The first wins, which is
		// what the catalogue transcribed.
		if _, seen := rows[m[2]]; !seen {
			rows[m[2]] = publishedRow{section: section, name: m[1]}
		}
	}

	// A parse that silently finds nothing reports perfect agreement, which is
	// the one failure mode this test must not have.
	if len(rows) < 100 {
		t.Fatalf("parsed only %d capabilities from permissions-map.md — the row shape has "+
			"changed and this test would otherwise pass vacuously", len(rows))
	}
	return rows
}

// TestCatalogueMatchesThePublishedMap asserts every catalogue row's section and
// permission name against the committed copy of the article it transcribes.
func TestCatalogueMatchesThePublishedMap(t *testing.T) {
	published := parsePublishedMap(t)

	var onlyHere, onlyThere []string
	for slug := range catalogue {
		if _, ok := published[slug]; !ok {
			onlyHere = append(onlyHere, slug)
		}
	}
	for slug := range published {
		if _, ok := catalogue[slug]; !ok {
			onlyThere = append(onlyThere, slug)
		}
	}
	sort.Strings(onlyHere)
	sort.Strings(onlyThere)
	if len(onlyHere) > 0 {
		t.Errorf("catalogue.go carries %d capabilities the article does not declare: %v\n"+
			"Either the article dropped them — in which case a rendered hint now names a "+
			"permission Jamf Account no longer has — or the slug is misspelled here.",
			len(onlyHere), onlyHere)
	}
	if len(onlyThere) > 0 {
		t.Errorf("the article declares %d capabilities catalogue.go has no row for: %v\n"+
			"Add them; the file is deliberately a complete copy of the article, so that a "+
			"future revision stays diffable against it.", len(onlyThere), onlyThere)
	}

	usedException := map[string]bool{}
	for slug, want := range published {
		got, ok := catalogue[slug]
		if !ok {
			continue // already reported above
		}

		wantSection := want.section
		if replacement, isException := sectionSpellingExceptions[wantSection]; isException {
			usedException[wantSection] = true
			wantSection = replacement
		}
		if got.category != wantSection {
			t.Errorf("%s section = %q, article says %q\n"+
				"Jamf Account's picker groups by section, so a wrong one sends an operator "+
				"to the wrong part of the page.", slug, got.category, wantSection)
		}

		if got.name != want.name {
			t.Errorf("%s name = %q, article says %q\n"+
				"The picker is searched BY NAME, so a name we expand or abbreviate is a name "+
				"that finds nothing in the box.", slug, got.name, want.name)
		}
	}

	for heading, replacement := range sectionSpellingExceptions {
		if !usedException[heading] {
			t.Errorf("sectionSpellingExceptions has a stale entry: the article no longer has "+
				"a %q heading (spelled %q here). Remove the entry and take the article's "+
				"spelling.", heading, replacement)
		}
	}
}
