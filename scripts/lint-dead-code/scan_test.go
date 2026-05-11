// Copyright 2026, Jamf Software LLC

package main

import (
	"strings"
	"testing"
)

func TestScan(t *testing.T) {
	cases := []struct {
		name      string
		root      string
		wantFlags []string
		wantFuncs []string
	}{
		{
			name:      "clean",
			root:      "testdata/clean",
			wantFlags: nil,
			wantFuncs: nil,
		},
		{
			name:      "dead-flag",
			root:      "testdata/dead-flag",
			wantFlags: []string{"stale"},
			wantFuncs: nil,
		},
		{
			name:      "dead-func",
			root:      "testdata/dead-func",
			wantFlags: nil,
			wantFuncs: []string{"orphan"},
		},
		{
			name:      "allowlisted",
			root:      "testdata/allowlisted",
			wantFlags: nil,
			wantFuncs: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := scan(tc.root)
			if err != nil {
				t.Fatalf("scan(%q) error: %v", tc.root, err)
			}

			var gotFlags []string
			for _, f := range got.deadFlags {
				gotFlags = append(gotFlags, f.flagName)
			}
			if !equalUnordered(gotFlags, tc.wantFlags) {
				t.Errorf("flags: got %v, want %v", gotFlags, tc.wantFlags)
			}

			var gotFuncs []string
			for _, f := range got.deadFuncs {
				gotFuncs = append(gotFuncs, f.name)
			}
			if !equalUnordered(gotFuncs, tc.wantFuncs) {
				t.Errorf("funcs: got %v, want %v", gotFuncs, tc.wantFuncs)
			}
		})
	}
}

func TestScanFlagBackingVar(t *testing.T) {
	got, err := scan("testdata/dead-flag")
	if err != nil {
		t.Fatalf("scan error: %v", err)
	}
	if len(got.deadFlags) != 1 {
		t.Fatalf("expected 1 dead flag, got %d", len(got.deadFlags))
	}
	if got.deadFlags[0].backingVar != "unusedFlag" {
		t.Errorf("backingVar = %q, want %q", got.deadFlags[0].backingVar, "unusedFlag")
	}
	if !strings.Contains(got.deadFlags[0].file, "dead-flag") {
		t.Errorf("file = %q, want to contain 'dead-flag'", got.deadFlags[0].file)
	}
}

func equalUnordered(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]int{}
	for _, x := range a {
		seen[x]++
	}
	for _, x := range b {
		seen[x]--
	}
	for _, v := range seen {
		if v != 0 {
			return false
		}
	}
	return true
}
