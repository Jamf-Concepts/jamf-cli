// Copyright 2026, Jamf Software LLC

package monolith

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The committed specs/ holds exactly one normalised Jamf Pro document plus the
// App Installer specs no monolith carries. The shipped jamf-cli skill tells
// users' models to fetch these files from main, and a sync PR runs this test,
// so a sync that deletes either one, or brings back a per-resource layout,
// fails its own CI.
func TestCommittedSpecsAreTheNormalisedLayout(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("..", "..", "specs"))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") && !strings.HasPrefix(e.Name(), ".") {
			got = append(got, e.Name())
		}
	}
	want := []string{NormalisedSpecFile}
	for _, s := range AppInstallerSpecs {
		want = append(want, s.Filename)
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("specs/*.yaml = %v, want %v", got, want)
	}
}

// The jamf-cli skill prints one block of JamfProAPI.yaml with grep and an awk
// whole-line match, so it depends on the bytes writeYAML emits: every path key
// unquoted at two spaces, every schema name at four. A change to the indent or
// to key quoting would make each user's extract print nothing and exit 0.
func TestCommittedProSpecKeepsTheShapeTheSkillExtractsRead(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "specs", NormalisedSpecFile))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")

	inPaths, pathKeys := false, 0
	for _, l := range lines {
		if l == "paths:" {
			inPaths = true
			continue
		}
		if inPaths && l != "" && l[0] != ' ' {
			break
		}
		if inPaths && strings.HasPrefix(l, "  ") && len(l) > 2 && l[2] != ' ' {
			pathKeys++
			if !strings.HasPrefix(l, "  /") || !strings.HasSuffix(l, ":") {
				t.Errorf("path key %q is not an unquoted two-space /path: line", l)
			}
		}
	}
	if pathKeys == 0 {
		t.Fatal("found no path keys under paths:, so this test would assert nothing")
	}

	// The skill's own examples.
	for _, key := range []string{"  /v1/categories:", "  /v1/categories/{id}:", "    Category:"} {
		if n := slices.Index(lines, key); n < 0 {
			t.Errorf("%q is not a whole line of %s", key, NormalisedSpecFile)
		} else if slices.Index(lines[n+1:], key) >= 0 {
			t.Errorf("%q occurs more than once, so the awk extract would print the first match only", key)
		}
	}
}
