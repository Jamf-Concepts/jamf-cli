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
		wantFiles   []string // asserted only where the function name alone cannot separate two sites
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
			// The exemption key carries the file as well, so exempting one
			// file's function does not cover a same-named function next to it.
			// root.go holds the sanctioned site, and a second site in that same
			// file wrote a 0-byte --out-file for as long as the key was the
			// function alone.
			name:       "an exemption for one file does not cover a same-named function in another",
			root:       "testdata/twofiles",
			exemptions: []exemption{{file: "testdata/twofiles/a.go", fn: "printThings", reason: "test"}},
			wantFuncs:  []string{"printThings"},
			wantFiles:  []string{"testdata/twofiles/b.go"},
		},
		{
			// A test needs a formatter to assemble a CLIContext and sets no
			// global output flag, so the walk skips _test.go outright. The skip
			// is what keeps every table-driven test in internal/commands from
			// being a finding.
			name: "a formatter in a test file is not a finding",
			root: "testdata/testfile",
		},
		{
			// new(T) builds the same working formatter as &T{} and names no
			// constructor at all.
			name:      "new(output.Formatter) is a finding",
			root:      "testdata/newexpr",
			wantFuncs: []string{"printThings"},
		},
		{
			// An element of a slice or map literal may elide its type, so the
			// node that builds the formatter carries no type for a rule to read.
			name:      "an element of a literal that elides its type is a finding",
			root:      "testdata/nestedlit",
			wantFuncs: []string{"printThings", "printThings", "printThings"},
		},
		{
			// The package exports one constructor today. An exact match on New
			// would report the tree clean the day it exports a second.
			name:      "a second constructor is still a finding",
			root:      "testdata/altctor",
			wantFuncs: []string{"printThings"},
		},
		{
			// A closure inside an exempt function is its own site: the
			// exemption records why that function cannot print through the
			// shared formatter, which says nothing about a callback in it. The
			// entry is stale as well, the function itself now building nothing.
			name:        "a closure does not inherit its function's exemption",
			root:        "testdata/closure",
			exemptions:  []exemption{{file: "testdata/closure/main.go", fn: "buildFormatter", reason: "test"}},
			wantFuncs:   []string{"buildFormatter.func1", "buildFormatter.func2"},
			wantStaleFn: []string{"buildFormatter"},
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

			var gotFuncs, gotFiles []string
			for _, f := range got.findings {
				gotFuncs = append(gotFuncs, f.fn)
				gotFiles = append(gotFiles, f.file)
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
			if tc.wantFiles != nil && !equalUnordered(gotFiles, tc.wantFiles) {
				t.Errorf("finding files: got %v, want %v", gotFiles, tc.wantFiles)
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

// A stale entry's remedy depends on which half of the key failed, and the two
// are opposite: a site that stopped building a formatter wants the entry
// deleted, while a typo'd path or a renamed function wants it corrected. One
// message for both sent the reader to delete an entry that was merely
// misspelled.
func TestStaleExemptionsSayWhichHalfOfTheKeyFailed(t *testing.T) {
	cases := []struct {
		name string
		e    exemption
		want staleReason
	}{
		{
			name: "the function is there and builds nothing",
			e:    exemption{file: "testdata/clean/main.go", fn: "printThings", reason: "test"},
			want: siteBuildsNothing,
		},
		{
			name: "the path names no file the walk reached",
			e:    exemption{file: "testdata/clean/typo.go", fn: "printThings", reason: "test"},
			want: fileNotFound,
		},
		{
			name: "the file is there and the function is not",
			e:    exemption{file: "testdata/clean/main.go", fn: "renamedAway", reason: "test"},
			want: funcNotFound,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := scan("testdata/clean", []exemption{tc.e})
			if err != nil {
				t.Fatalf("scan error: %v", err)
			}
			if len(res.stale) != 1 {
				t.Fatalf("got %d stale exemptions, want 1", len(res.stale))
			}
			if res.stale[0].reason != tc.want {
				t.Errorf("reason = %v, want %v: %q", res.stale[0].reason, tc.want, staleAdvice(res.stale[0].reason))
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
