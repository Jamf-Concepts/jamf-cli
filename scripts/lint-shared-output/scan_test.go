// Copyright 2026, Jamf Software LLC

package main

import (
	"strings"
	"testing"
)

func TestScan(t *testing.T) {
	cases := []struct {
		name        string
		root        string
		exemptions  []exemption
		wantFuncs   []string
		wantStaleFn []string
	}{
		{
			name: "printing through the shared formatter is clean",
			root: "testdata/clean",
		},
		{
			name:      "a second formatter is a finding",
			root:      "testdata/offender",
			wantFuncs: []string{"printThings"},
		},
		{
			name:       "an exempt function is not a finding",
			root:       "testdata/exempt",
			exemptions: []exemption{{file: "testdata/exempt/main.go", fn: "buildFormatter", reason: "test"}},
		},
		{
			// An import alias must not be a way past the lint.
			name:      "an aliased import is still a finding",
			root:      "testdata/aliased",
			wantFuncs: []string{"printThings"},
		},
		{
			// Nor a dot-import, where the call carries no package qualifier.
			name:      "a dot-imported New is still a finding",
			root:      "testdata/dotimport",
			wantFuncs: []string{"printThings"},
		},
		{
			// Some other package's New is not this rule's business. Without the
			// import check the identifier alone would convict it.
			name: "an unrelated package named output is left alone",
			root: "testdata/decoy",
		},
		{
			// The constructor is not the only way in. Every field of
			// output.Formatter is unexported, but the type and its setters are
			// not, so a literal plus SetWriter builds a working second
			// formatter that never names New.
			name:      "a Formatter literal is a finding and a type reference is not",
			root:      "testdata/literal",
			wantFuncs: []string{"printThings"},
		},
		{
			// Matching the selector rather than the call keeps the constructor
			// from being smuggled through a function value.
			name:      "taking New as a value is still a finding",
			root:      "testdata/funcval",
			wantFuncs: []string{"printThings"},
		},
		{
			// The exemption key carries the receiver, so exempting one method
			// does not silently exempt its same-named sibling.
			name:       "an exemption for one method does not cover a same-named method on another type",
			root:       "testdata/method",
			exemptions: []exemption{{file: "testdata/method/main.go", fn: "preview.render", reason: "test"}},
			wantFuncs:  []string{"report.render"},
		},
		{
			// The other direction: a site that stops building its own formatter
			// must not leave its excuse behind.
			name:        "an exemption matching nothing is stale",
			root:        "testdata/clean",
			exemptions:  []exemption{{file: "testdata/clean/main.go", fn: "printThings", reason: "test"}},
			wantStaleFn: []string{"printThings"},
		},
		{
			// A stale entry is reported only for the tree that was scanned, so
			// scanning one package does not condemn every entry outside it.
			name:       "an exemption outside the scanned root is not stale",
			root:       "testdata/clean",
			exemptions: []exemption{{file: "testdata/offender/main.go", fn: "printThings", reason: "test"}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := scan(tc.root, tc.exemptions)
			if err != nil {
				t.Fatalf("scan(%q) error: %v", tc.root, err)
			}

			var gotFuncs []string
			for _, f := range got.findings {
				gotFuncs = append(gotFuncs, f.fn)
				if f.line == 0 {
					t.Errorf("finding in %s carries no line number", f.file)
				}
				if !strings.HasPrefix(f.file, tc.root) {
					t.Errorf("finding file = %q, want it under %q", f.file, tc.root)
				}
			}
			if !equalUnordered(gotFuncs, tc.wantFuncs) {
				t.Errorf("findings: got %v, want %v", gotFuncs, tc.wantFuncs)
			}

			var gotStale []string
			for _, e := range got.stale {
				gotStale = append(gotStale, e.fn)
			}
			if !equalUnordered(gotStale, tc.wantStaleFn) {
				t.Errorf("stale exemptions: got %v, want %v", gotStale, tc.wantStaleFn)
			}

			if want := len(tc.wantFuncs) == 0 && len(tc.wantStaleFn) == 0; got.clean() != want {
				t.Errorf("clean() = %v, want %v", got.clean(), want)
			}
		})
	}
}

// The exemptions ship in the binary, so a wrong path or a renamed function
// silently stops covering its site — and then the lint fails on a site the
// author believed was accounted for. Every entry must match something in the
// tree the lint actually runs against.
func TestDefaultExemptionsAllMatchALiveCallSite(t *testing.T) {
	// The entries are written relative to the repository root, which is also
	// where the Makefile target runs the lint from.
	t.Chdir("../..")

	res, err := scan("internal", defaultExemptions)
	if err != nil {
		t.Fatalf("scan error: %v", err)
	}
	for _, e := range res.stale {
		t.Errorf("exemption %s %s matches no output.New call — delete it", e.file, e.fn)
	}
	for _, e := range defaultExemptions {
		if e.reason == "" {
			t.Errorf("exemption %s %s carries no reason", e.file, e.fn)
		}
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
