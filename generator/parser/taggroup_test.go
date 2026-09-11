// Copyright 2026, Jamf Software LLC

package parser

import (
	"sort"
	"strings"
	"testing"
)

// A root that holds paths from two tags is named by the tag of its own root
// collection, not by whichever of its tags sorted last.
//
// This is the guard for the defect the tag-derived naming shipped with. Eight
// groups in the 11.31.1 monolith hold more than one tag, which is the correct
// grouping — a `recalculate` action on `/v1/users/{id}` belongs with the user
// CRUD it acts on — but the group's Tag was assigned in a loop over sorted
// paths, so the last write won. Three of the eight took the wrong name and two
// shipped a name that contradicted every string the commands under it printed:
// `/v1/users` CRUD as `pro smart-user-groups`, whose `apply` deleted and
// recreated a user record, and `/v2/patch-policies` as `pro patch-policy-logs`.
//
// Asserted against the live document rather than a fixture, because the
// property that matters is about the document upstream ships: the next drop can
// introduce a merge, and sort order is a plausible-looking answer for it.
func TestMergedTagGroupsAreNamedByTheirRootCollection(t *testing.T) {
	tagOf := liveTagOfPath(t)
	groups := groupLive(t)

	merged := 0
	for _, g := range groups {
		tags := map[string]bool{}
		for _, p := range g.Paths {
			tags[tagOf[p]] = true
		}
		if len(tags) < 2 {
			continue
		}
		merged++

		// The root collection is the group's own path with no parameter and no
		// deeper segment. Where the group has one, its tag decides.
		rootKey := strings.Join(g.Root, "/")
		atRoot := map[string]bool{}
		for _, p := range g.Paths {
			if _, segs := splitVersionSegment(p); strings.Join(segs, "/") == rootKey {
				atRoot[tagOf[p]] = true
			}
		}
		if len(atRoot) == 0 {
			// No root collection: the most frequent tag speaks for the group.
			// Assert that rather than skipping, so the fallback is covered too.
			want := mostFrequentTag(tagCounts(g.Paths, tagOf))
			if g.Tag != want {
				t.Errorf("group %q (root %v) holds tags %v and no root collection; Tag = %q, want the most frequent %q",
					g.Name, g.Root, sortedSet(tags), g.Tag, want)
			}
			continue
		}
		if !atRoot[g.Tag] {
			t.Errorf("group %q (root %v) holds tags %v; Tag = %q, but its root collection is tagged %v — a merged root must be named by its own collection, not by sort order",
				g.Name, g.Root, sortedSet(tags), g.Tag, sortedSet(atRoot))
		}
	}
	if merged == 0 {
		t.Fatal("no group in the committed specs holds more than one tag, so this test proves nothing; if upstream really removed every merge, delete it and say so")
	}
	t.Logf("%d of %d groups hold more than one tag", merged, len(groups))
}

// The two names the defect got wrong, pinned by name against the endpoints they
// serve. A rule test can be satisfied by a rule that is right about the wrong
// group; these two say which commands the CLI ships.
func TestUserAndPatchPolicyResourcesAreNamedAfterWhatTheyServe(t *testing.T) {
	byName := map[string]*PathGroup{}
	for _, g := range groupLive(t) {
		byName[g.Name] = g
	}
	for name, wantPath := range map[string]string{
		"users":          "/v1/users",
		"patch-policies": "/v2/patch-policies",
	} {
		g, ok := byName[name]
		if !ok {
			t.Errorf("no group named %q; the CRUD at %s has to be reachable under the name of the thing it acts on", name, wantPath)
			continue
		}
		if !containsString(g.Paths, wantPath) {
			t.Errorf("group %q does not serve %s; it holds %v", name, wantPath, g.Paths)
		}
	}
	for _, gone := range []string{"smart-user-groups", "patch-policy-logs"} {
		if g, ok := byName[gone]; ok {
			t.Errorf("group %q is back, holding %v; it is a sub-path tag naming a resource whose CRUD is something else", gone, g.Paths)
		}
	}
}

// A path whose methods declare different base tags is refused, because the tag
// decides which resource a path joins and nothing downstream can express a path
// belonging to two.
func TestSoleBaseTagOfRefusesAPathCarryingTwoTags(t *testing.T) {
	if _, err := soleBaseTagOf("/v3/foo", []string{"foo", "foo-management"}); err == nil {
		t.Fatal("a path tagged both foo and foo-management was accepted; one tag would win by sorted-method order and the command tree would reorganise silently")
	} else {
		for _, want := range []string{"/v3/foo", "foo", "foo-management"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not name %q; the reader has to be told which path and which tags", err, want)
			}
		}
	}

	// A `-preview` sibling is the same tag, not a disagreement: folding a
	// preview endpoint into the resource it previews is what BaseTag is for.
	if got, err := soleBaseTagOf("/v3/foo", []string{"foo-preview", "foo"}); err != nil || got != "foo" {
		t.Errorf("soleBaseTagOf(foo-preview, foo) = %q, %v; want \"foo\", nil", got, err)
	}
	if got, err := soleBaseTagOf("/v3/foo", nil); err != nil || got != "" {
		t.Errorf("an untagged path = %q, %v; want \"\", nil — it groups by its own root", got, err)
	}
}

// Every committed path declares one base tag. The refusal above is a guard
// against a drop; this is the statement that the guard costs nothing today.
func TestEveryLivePathDeclaresOneBaseTag(t *testing.T) {
	tagsOf := livePathTags(t)
	for _, p := range keysOfSlices(tagsOf) {
		if _, err := soleBaseTagOf(p, tagsOf[p]); err != nil {
			t.Errorf("%v", err)
		}
	}
}

// liveTagOfPath maps each committed path this generator ingests to its one base
// tag.
func liveTagOfPath(t *testing.T) map[string]string {
	t.Helper()
	tagsOf := livePathTags(t)
	out := make(map[string]string, len(tagsOf))
	for _, p := range keysOfSlices(tagsOf) {
		if !KeepPath(p, tagsOf[p]) {
			continue
		}
		tag, err := soleBaseTagOf(p, tagsOf[p])
		if err != nil {
			t.Fatalf("%v", err)
		}
		out[p] = tag
	}
	return out
}

func tagCounts(paths []string, tagOf map[string]string) map[string]int {
	out := map[string]int{}
	for _, p := range paths {
		out[tagOf[p]]++
	}
	return out
}

func sortedSet(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func containsString(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
