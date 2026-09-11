// Copyright 2026, Jamf Software LLC

package parser

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// groupUntagged runs the shipped grouping over paths that declare no tag.
//
// The six rules below are about the *path* half of the hybrid — which run of
// literal segments is a root — and an untagged path exercises exactly that: it
// is bounded by its own root, so no tag folds anything and the answer is the
// path rule alone. They used to call GroupPathsByCollection, a path-only
// entry point nothing in the pipeline invoked, so they read as coverage of the
// shipped rule and were coverage of a retired one. That function is gone.
func groupUntagged(paths []string) []*PathGroup {
	tagged := make([]TaggedPath, 0, len(paths))
	for _, p := range paths {
		tagged = append(tagged, TaggedPath{Path: p})
	}
	return GroupPathsByTagAndCollection(tagged)
}

// groupLive runs the shipped grouping over every committed path, with its tag.
func groupLive(t *testing.T) []*PathGroup {
	t.Helper()
	tagsOf := livePathTags(t)
	tagged := make([]TaggedPath, 0, len(tagsOf))
	for _, p := range keysOfSlices(tagsOf) {
		if !KeepPath(p, tagsOf[p]) {
			continue
		}
		tag, err := soleBaseTagOf(p, tagsOf[p])
		if err != nil {
			t.Fatalf("%v", err)
		}
		tagged = append(tagged, TaggedPath{Path: p, Tag: tag})
	}
	return GroupPathsByTagAndCollection(tagged)
}

// keysOfSlices returns a map's keys, sorted.
func keysOfSlices(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// namesOf renders a grouping as "name: path path path" lines for comparison.
func namesOf(groups []*PathGroup) map[string]string {
	out := map[string]string{}
	for _, g := range groups {
		out[g.Name] = strings.Join(g.Paths, " ")
	}
	return out
}

// The rule that separates a sub-collection from an action taking an id. Without
// the "answers as a collection itself" test, `download` becomes a resource
// because `/v1/icon/download/{id}` gives it a parameter child — and five thin
// resources appear for what are plainly operations.
func TestPathRootRule_ActionWithAnIDIsNotAResource(t *testing.T) {
	got := namesOf(groupUntagged([]string{
		"/v1/icon/{id}",
		"/v1/icon/download/{id}",
		"/v1/icon",
	}))
	if len(got) != 1 || got["icon"] == "" {
		t.Fatalf("want one group named icon, got %v", got)
	}
}

// A sub-collection that answers as a collection *and* has items of its own is a
// resource in its own right. This is the computer-groups case: three resources
// under one path prefix, where today three separate spec filenames decide it.
func TestPathRootRule_SubCollectionIsAResource(t *testing.T) {
	got := namesOf(groupUntagged([]string{
		"/v1/computer-groups",
		"/v2/computer-groups/static-groups",
		"/v2/computer-groups/static-groups/{id}",
		"/v3/computer-groups/smart-groups",
		"/v3/computer-groups/smart-groups/{id}",
		"/v3/computer-groups/static-groups",
		"/v3/computer-groups/static-groups/{id}",
	}))
	for _, want := range []string{"computer-groups", "computer-groups-smart-groups", "computer-groups-static-groups"} {
		if _, ok := got[want]; !ok {
			t.Errorf("missing group %q; got %v", want, keysOf(got))
		}
	}
	if len(got) != 3 {
		t.Errorf("want 3 groups, got %d: %v", len(got), keysOf(got))
	}
	// v2 and v3 static-groups are the same resource, not two. Which version
	// wins is deduplicateVersionedOps' decision, per version-stripped path.
	if got := len(groupUntagged([]string{
		"/v2/computer-groups/static-groups",
		"/v3/computer-groups/static-groups",
		"/v2/computer-groups/static-groups/{id}",
		"/v3/computer-groups/static-groups/{id}",
	})); got != 1 {
		t.Errorf("v2 and v3 of one collection made %d groups, want 1", got)
	}
}

// A no-parameter sub-path is an operation, not a resource.
func TestPathRootRule_UnparameterisedSubPathIsAnOperation(t *testing.T) {
	got := namesOf(groupUntagged([]string{
		"/v1/computers-inventory",
		"/v1/computers-inventory/filevault",
		"/v1/computers-inventory/{id}",
		"/v1/computers-inventory/{id}/view-recovery-lock-password",
	}))
	if len(got) != 1 {
		t.Fatalf("want one group, got %v", keysOf(got))
	}
}

// Every version of a path lands in one group, and the group records them all.
// This is the property that makes the endpoint-version family a fact about the
// paths rather than about a filename's `-vN` suffix — the mis-keying that cost
// the CLI its v4 computer-inventory endpoints cannot be expressed here.
func TestPathRootRule_OneGroupSpansEveryVersion(t *testing.T) {
	groups := groupUntagged([]string{
		"/v1/computers-inventory",
		"/v2/computers-inventory",
		"/v3/computers-inventory",
		"/v4/computers-inventory",
	})
	if len(groups) != 1 {
		t.Fatalf("want 1 group, got %d", len(groups))
	}
	if want := []int{1, 2, 3, 4}; !equalInts(groups[0].Versions, want) {
		t.Errorf("versions = %v, want %v", groups[0].Versions, want)
	}
}

// A top-level collection is its own resource even with nothing beneath it, so
// two unrelated depth-1 paths do not get folded together by a shared filename.
//
// This is also boundKey's guard. Neither root is CRUD-shaped and neither
// prefixes the other, so resolveRootsWithinBound's no-survivor fallback would
// fold both into the largest — and with untagged paths pooled under one empty
// tag they share a bound, so `health-status` would come out as part of a
// `health-check` resource. Bounding an untagged path by its own root is what
// keeps them two.
func TestPathRootRule_TopLevelCollectionIsAlwaysARoot(t *testing.T) {
	got := namesOf(groupUntagged([]string{"/v1/health-check", "/v1/health-status"}))
	if len(got) != 2 {
		t.Errorf("want 2 groups, got %v", keysOf(got))
	}
}

// A path with no version segment still groups.
func TestPathRootRule_UnversionedPath(t *testing.T) {
	got := namesOf(groupUntagged([]string{"/ldap/groups", "/ldap/servers"}))
	if _, ok := got["ldap"]; !ok {
		t.Errorf("want ldap, got %v", keysOf(got))
	}
}

// An override replaces a derived name that cannot stand as a command name, and
// every entry has to still match something the live specs produce — otherwise it
// is a rule nobody applies, describing a path or a tag that moved.
//
// Checked against both derivations, because either can produce the name an
// override replaces: nameFromTags builds one from the tag, and groupName from
// the path when the tag is shared or absent.
func TestPathGroupNameOverridesAllMatchALiveGroup(t *testing.T) {
	derived := map[string]bool{}
	for _, g := range groupLive(t) {
		derived[strings.Join(g.Root, "-")] = true
	}
	tagsOf := livePathTags(t)
	for p, tags := range tagsOf {
		_ = p
		for _, tag := range tags {
			derived[BaseTag(tag)] = true
		}
	}
	for key, replacement := range pathGroupNameOverrides {
		if !derived[key] {
			t.Errorf("pathGroupNameOverrides[%q] matches no group the live specs produce; the path moved or the entry is stale", key)
		}
		if replacement == "" || replacement == key {
			t.Errorf("pathGroupNameOverrides[%q] = %q, which overrides nothing", key, replacement)
		}
	}
}

// Grouping is total and disjoint: every path reaches exactly one resource. The
// failure this rules out is the one the retired file-based layout produced when
// it was pointed at an incomplete tree — 73 operations silently unreachable,
// with the generator exiting 0.
func TestPathRootRule_CoversEveryPathExactlyOnce(t *testing.T) {
	tagsOf := livePathTags(t)
	var paths []string
	for _, p := range keysOfSlices(tagsOf) {
		if KeepPath(p, tagsOf[p]) {
			paths = append(paths, p)
		}
	}
	if len(paths) == 0 {
		t.Fatal("every committed path was dropped; this test cannot pass vacuously")
	}
	seen := map[string]int{}
	for _, g := range groupLive(t) {
		if g.Name == "" {
			t.Errorf("group with root %v derived an empty name for %v", g.Root, g.Paths)
		}
		for _, p := range g.Paths {
			seen[p]++
		}
	}
	for _, p := range paths {
		switch seen[p] {
		case 1:
		case 0:
			t.Errorf("%s reaches no resource", p)
		default:
			t.Errorf("%s reaches %d resources", p, seen[p])
		}
	}
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// livePathTags returns every path the committed specs declare, mapped to the
// tags its operations carry.
func livePathTags(t *testing.T) map[string][]string {
	t.Helper()
	specs, err := filepath.Glob("../../specs/*.yaml")
	if err != nil {
		t.Fatalf("globbing specs: %v", err)
	}
	out := map[string][]string{}
	for _, s := range specs {
		if strings.HasPrefix(filepath.Base(s), ".") {
			continue
		}
		loader := openapi3.NewLoader()
		loader.IsExternalRefsAllowed = true
		doc, err := loader.LoadFromFile(s)
		if err != nil || doc.Paths == nil {
			continue
		}
		for p, item := range doc.Paths.Map() {
			if item == nil {
				continue
			}
			seen := map[string]bool{}
			for _, op := range item.Operations() {
				for _, tag := range op.Tags {
					if !seen[tag] {
						seen[tag] = true
						out[p] = append(out[p], tag)
					}
				}
			}
			if _, ok := out[p]; !ok {
				out[p] = nil
			}
		}
	}
	if len(out) == 0 {
		t.Fatal("no paths found in ../../specs; this test cannot pass vacuously")
	}
	return out
}

// The four legacy unversioned stubs must not reach the command surface.
func TestKeepPath_DropsTheLegacyUnversionedStubs(t *testing.T) {
	tagsOf := livePathTags(t)
	for _, p := range []string{
		"/preview/computers",
		"/preview/remote-administration-configurations",
		"/settings/issueTomcatSslCertificate",
		"/settings/obj/policyProperties",
		"/v1/devices/{id}/groups",
	} {
		tags, declared := tagsOf[p]
		if !declared {
			t.Errorf("%s is no longer declared by the specs; its drop entry is stale", p)
			continue
		}
		if KeepPath(p, tags) {
			t.Errorf("%s survived the drop (tags %v)", p, tags)
		}
	}

	// The team-viewer family sits *under* the dropped bare collection and is
	// the surface the gateway publishes, so dropping the stub must not take it.
	for p, tags := range tagsOf {
		if strings.Contains(p, "/team-viewer") && !KeepPath(p, tags) {
			t.Errorf("%s was dropped; only the bare collection stub above it should be", p)
		}
	}
}

// A tag is not always a safe unit to drop, and assuming it was would have
// deleted a live command: `policies-preview` tags the legacy
// `/settings/obj/policyProperties` *and* `/v1/policy-properties`, the real
// versioned resource. Dropping that tag removes `pro policy-properties`
// silently, because a resource that stops being generated reports nothing.
//
// So: no dropped tag may touch a versioned path. This fails when an ingest adds
// one to a tag already on the list, which is the only warning there would be.
func TestDroppedTagsDoNotTakeAVersionedPathWithThem(t *testing.T) {
	for p, tags := range livePathTags(t) {
		version, _ := splitVersionSegment(p)
		if version == 0 {
			continue
		}
		for _, tag := range tags {
			if droppedTags[tag] {
				t.Errorf("dropped tag %q also covers the versioned path %s, whose resource would stop being generated with no error; drop the legacy path by name in droppedPaths instead", tag, p)
			}
		}
	}
}

// Both drop tables have to keep matching the specs. A stale entry is a rule
// nobody applies, describing an endpoint that moved.
func TestDropTablesStillMatchTheSpecs(t *testing.T) {
	tagsOf := livePathTags(t)
	liveTags := map[string]bool{}
	for _, tags := range tagsOf {
		for _, tag := range tags {
			liveTags[tag] = true
		}
	}
	for tag := range droppedTags {
		if !liveTags[tag] {
			t.Errorf("droppedTags[%q] matches no tag the specs declare; the endpoint moved, or the entry is stale", tag)
		}
	}
	for p := range droppedPaths {
		if _, ok := tagsOf[p]; !ok {
			t.Errorf("droppedPaths[%q] matches no path the specs declare", p)
		}
	}
}

// Whatever is dropped, the great majority has to survive — a drop table that
// silently swallowed the surface would pass every test above.
func TestKeepPath_DropsOnlyAHandful(t *testing.T) {
	tagsOf := livePathTags(t)
	var dropped []string
	for p, tags := range tagsOf {
		if !KeepPath(p, tags) {
			dropped = append(dropped, p)
		}
	}
	sort.Strings(dropped)
	// A ceiling rather than an exact count, because the table grows one
	// deliberate entry at a time. What it rules out is the drop tables becoming
	// a mechanism that swallows the surface: 16 of 563 is a legacy tail, and a
	// number several times that is a bug in KeepPath rather than a decision
	// anyone made.
	if len(dropped) > 25 {
		t.Errorf("KeepPath drops %d of %d paths, which is more than a legacy tail:\n  %s",
			len(dropped), len(tagsOf), strings.Join(dropped, "\n  "))
	}
	if len(dropped) == 0 {
		t.Error("KeepPath drops nothing; the drop tables are not being consulted")
	}
	t.Logf("dropped %d of %d paths:\n  %s", len(dropped), len(tagsOf), strings.Join(dropped, "\n  "))
}
