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
