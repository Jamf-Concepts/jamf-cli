// Copyright 2026, Jamf Software LLC

package commands

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// changelogVersionHeading matches a release heading, `## v1.32.0`.
var changelogVersionHeading = regexp.MustCompile(`^## v(\d+)\.(\d+)\.(\d+)$`)

// TestChangelogHeadingsAreUnique guards CHANGELOG.md's release headings: at
// most one `## Unreleased`, first if present; each version once; versions
// newest first.
//
// The failure it exists for merges cleanly. One branch renames `## Unreleased`
// to the version it shipped as, while another adds entries under the same
// `## Unreleased` line. Git pairs the base's heading with the second branch's,
// so the rename lands on the new entries: they are filed under a release that
// already shipped, and the version heading appears twice. Nothing conflicts,
// so nothing else says so (reproduced with git merge-file on #411 against
// #413, 2026-10-04, in both merge orders).
func TestChangelogHeadingsAreUnique(t *testing.T) {
	body, err := os.ReadFile("../../CHANGELOG.md")
	if err != nil {
		t.Fatalf("reading CHANGELOG.md: %v (if it moved, update this path rather than deleting the guard)", err)
	}
	problems, headings := changelogHeadingProblems(string(body))
	if headings == 0 {
		t.Fatal("CHANGELOG.md has no `## ` release headings — the walk found nothing to check")
	}
	for _, p := range problems {
		t.Error("CHANGELOG.md " + p)
	}
}

// The guard has to fire on the merge it was written for, not just pass on
// today's file.
func TestChangelogHeadingProblems_CatchesTheMisfiledMerge(t *testing.T) {
	misfiled := "## v1.33.0\n\n### Deprecated — new entries\n\n## v1.33.0\n\n### Breaking — shipped\n\n## v1.32.0\n"
	if problems, _ := changelogHeadingProblems(misfiled); len(problems) == 0 {
		t.Error("a repeated version heading passed")
	}
	for _, bad := range []string{
		"## Unreleased\n\n## Unreleased\n\n## v1.0.0\n",
		"## v1.0.0\n\n## Unreleased\n",
		"## v1.0.0\n\n## v1.1.0\n",
		"## 1.2.0\n",
	} {
		if problems, _ := changelogHeadingProblems(bad); len(problems) == 0 {
			t.Errorf("passed: %q", bad)
		}
	}
	if problems, _ := changelogHeadingProblems("## Unreleased\n\n## v1.10.0\n\n## v1.9.2\n\n## v0.9.0\n"); len(problems) != 0 {
		t.Errorf("a well-formed changelog failed: %v", problems)
	}
}

// changelogHeadingProblems checks every `## ` heading in text and returns one
// message per problem, and the number of headings it read.
func changelogHeadingProblems(text string) (problems []string, headings int) {
	var (
		unreleased int
		seen       = map[string]int{}
		prev       *[3]int
		prevText   string
	)
	for i, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(line, "## ") {
			continue
		}
		headings++
		lineNo := i + 1
		if line == "## Unreleased" {
			unreleased++
			if unreleased > 1 {
				problems = append(problems, fmt.Sprintf("line %d: a second `## Unreleased` heading", lineNo))
			}
			if headings != 1 {
				problems = append(problems, fmt.Sprintf("line %d: `## Unreleased` must be the first release heading", lineNo))
			}
			continue
		}
		m := changelogVersionHeading.FindStringSubmatch(line)
		if m == nil {
			problems = append(problems, fmt.Sprintf("line %d: %q is neither `## Unreleased` nor `## vMAJOR.MINOR.PATCH`", lineNo, line))
			continue
		}
		if first, dup := seen[line]; dup {
			problems = append(problems, fmt.Sprintf("line %d: %q repeats the heading on line %d — the entries under "+
				"one of them belong to another release, most likely a new `## Unreleased` a version rename landed on",
				lineNo, line, first))
			continue
		}
		seen[line] = lineNo
		var v [3]int
		for j := range v {
			v[j], _ = strconv.Atoi(m[j+1])
		}
		if prev != nil && !versionBefore(v, *prev) {
			problems = append(problems, fmt.Sprintf("line %d: %q is not older than %q above it — release headings run newest first",
				lineNo, line, prevText))
		}
		prev, prevText = &v, line
	}
	return problems, headings
}

// versionBefore reports whether a is an older version than b.
func versionBefore(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}
