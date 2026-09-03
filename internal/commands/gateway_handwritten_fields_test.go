package commands

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The path guard beside this one (TestHandWrittenPathsAreServed) checks that a
// hand-written command sends a path the gateway publishes. It says nothing
// about the *response*, and a version bump can rename a field as readily as it
// can withdraw a path — so the v3→v4 computers-inventory sweep moved thirteen
// path literals and left every reader asking for general.lastContactTime, a
// key ComputerGeneralV4 does not declare. Nothing failed: the reads answered
// "" and the reports rendered a healthy fleet. Reading it as an RSQL filter
// field was worse than silent — v4 answers 400 INVALID_FIELD — so `pro audit`
// reported a failed check on a healthy tenant.
//
// Wire-checked 2026-09-03 against nmartin.jamfcloud.com, one computer, both
// versions minutes apart: v1's general.lastContactTime and v4's
// general.lastCheckIn were the same instant, and v4's general.lastContact was
// null. So lastCheckIn is the rename; lastContact is a new field beside it,
// and substituting it is not a fix — a `<` filter on lastContact matched the
// unfiltered totalCount, which would report the whole fleet as stale.
//
// The guard is deliberately keyed on the *withdrawn* v1 names rather than on
// "every name ComputerGeneralV4 declares". `general` is a variable name shared
// by the policy, mobile-device and package sections, which carry their own
// unrelated fields (general.enabled, general.displayName, general.osVersion),
// so a completeness check over one schema reports those as defects. What is
// knowable from the specs alone, and is exactly this bug class, is the set of
// names v1 declared and v4 does not.

// generalFieldRead matches a read of a `general`-section field in either of
// the two shapes the hand-written commands use.
var generalFieldRead = regexp.MustCompile(`(?:general|gen)\[\"([A-Za-z0-9_]+)\"\]|strVal\((?:general|gen), \"([A-Za-z0-9_]+)\"\)`)

// rsqlFilterField matches an RSQL filter field inside a request-path literal.
var rsqlFilterField = regexp.MustCompile(`filter=(general\.[A-Za-z0-9_.]+)`)

// withdrawnV1GeneralReads are reads of a v1-only general field that are still
// correct, keyed "<field>@<enclosing function>" with the reason.
//
// Keyed on the site rather than on the field name, because a name-keyed
// exemption exempts the primary read as well as the fallback: with
// "lastContactTime" alone in the table, reverting pro_device.go to read it
// directly still passed. That is the same weakness the review found in
// TestHandWrittenPathsAreServed's matcher — a guard reporting "checked" for a
// case it does not check.
var withdrawnV1GeneralReads = map[string]string{
	"lastContactTime@lastCheckInOf": "the explicit v1 fallback, for an instance still answering v1",
}

// enclosingFunc names the function containing line i (0-indexed), or "" when
// the line is above the first declaration.
func enclosingFunc(lines []string, i int) string {
	for j := i; j >= 0; j-- {
		line := lines[j]
		if !strings.HasPrefix(line, "func ") {
			continue
		}
		rest := strings.TrimPrefix(line, "func ")
		if strings.HasPrefix(rest, "(") { // method: skip the receiver
			if k := strings.Index(rest, ")"); k >= 0 {
				rest = strings.TrimSpace(rest[k+1:])
			}
		}
		if k := strings.IndexAny(rest, "([{"); k >= 0 {
			rest = rest[:k]
		}
		return strings.TrimSpace(rest)
	}
	return ""
}

// withdrawnGeneralFields returns the general-section field names v1 declared
// and ComputerGeneralV4 does not.
func withdrawnGeneralFields(t *testing.T) map[string]bool {
	t.Helper()

	props := func(path, schema string) map[string]yaml.Node {
		raw, err := os.ReadFile(filepath.Join("..", "..", "specs", path))
		if err != nil {
			t.Fatalf("reading specs/%s: %v\n"+
				"This guard reads the two general-section schemas; if a file has moved, "+
				"update the path rather than deleting the guard.", path, err)
		}
		var doc struct {
			Components struct {
				Schemas map[string]struct {
					Properties map[string]yaml.Node `yaml:"properties"`
				} `yaml:"schemas"`
			} `yaml:"components"`
		}
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("parsing specs/%s: %v", path, err)
		}
		s, ok := doc.Components.Schemas[schema]
		if !ok || len(s.Properties) == 0 {
			t.Fatalf("%s carries no properties in specs/%s — the spec shape has changed, "+
				"so this guard would pass vacuously", schema, path)
		}
		return s.Properties
	}

	v1 := props("_MonolithLibrary.yaml", "ComputerGeneral")
	v4 := props("ComputersInventory.yaml", "ComputerGeneralV4")

	withdrawn := map[string]bool{}
	for name := range v1 {
		if _, kept := v4[name]; !kept {
			withdrawn[name] = true
		}
	}
	if len(withdrawn) == 0 {
		t.Fatal("v4 withdrew no general field, so this guard has nothing to check. " +
			"If a later version really is a pure superset, retarget the guard at that " +
			"version pair rather than removing it.")
	}
	return withdrawn
}

// TestHandWrittenReadsDoNotNameAWithdrawnGeneralField fails when a
// hand-written command reads or filters on a computers-inventory general field
// that v4 withdrew, unless the read is a declared fallback.
func TestHandWrittenReadsDoNotNameAWithdrawnGeneralField(t *testing.T) {
	withdrawn := withdrawnGeneralFields(t)

	var files []string
	for _, dir := range handWrittenPathDirs {
		found, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, found...)
	}
	sort.Strings(files)
	if len(files) == 0 {
		t.Fatal("no Go files to scan — the scan directories have moved")
	}

	reads, filters := 0, 0
	seenFallback := map[string]bool{}

	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(string(src), "\n")
		for i, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}

			for _, m := range generalFieldRead.FindAllStringSubmatch(line, -1) {
				name := m[1]
				if name == "" {
					name = m[2]
				}
				reads++
				if !withdrawn[name] {
					continue
				}
				key := name + "@" + enclosingFunc(lines, i)
				if reason, ok := withdrawnV1GeneralReads[key]; ok {
					seenFallback[key] = true
					t.Logf("declared fallback %s:%d %s (%s)", file, i+1, key, reason)
					continue
				}
				t.Errorf("%s:%d reads general.%s, which v4 withdrew.\n"+
					"computers-inventory serves v4, so this read answers \"\" on every record "+
					"and the caller cannot tell a missing value from a healthy one. Use the v4 "+
					"name, or add a \"%s\" entry to withdrawnV1GeneralReads if this site is a "+
					"declared fallback.", file, i+1, name, key)
			}

			for _, m := range rsqlFilterField.FindAllStringSubmatch(line, -1) {
				filters++
				field := strings.TrimPrefix(m[1], "general.")
				if !withdrawn[field] {
					continue
				}
				t.Errorf("%s:%d filters on %s, which v4 withdrew.\n"+
					"Wire-checked: v4 answers 400 INVALID_FIELD for an undeclared filter field, "+
					"so this check fails on a healthy tenant. A filter field has no fallback — "+
					"there is nothing to fall back to once the request is refused.", file, i+1, m[1])
			}
		}
	}

	if reads == 0 {
		t.Fatal("matched no general-section reads — generalFieldRead no longer matches the " +
			"code, so this guard is vacuous")
	}
	if filters == 0 {
		t.Fatal("matched no RSQL filter fields — rsqlFilterField no longer matches the code, " +
			"so half this guard is vacuous")
	}
	for key, reason := range withdrawnV1GeneralReads {
		if !seenFallback[key] {
			t.Errorf("withdrawnV1GeneralReads has a stale entry: %s (%s) no longer names a "+
				"live read site. Remove it.", key, reason)
		}
	}
}
