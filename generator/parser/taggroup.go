// Copyright 2026, Jamf Software LLC

package parser

import (
	"regexp"
	"sort"
	"strings"
)

// Grouping uses the tag to decide what belongs together and the path to decide
// where the boundary falls.
//
// Neither alone is enough, and both failure modes were measured on the live
// 11.31.1 monolith (550 paths, 820 operations):
//
//   - Paths alone split a resource upstream considers whole. `/v1/computer-
//     inventory/{id}/erase`, `/vN/computers-inventory/…` and
//     `/v1/computers-inventory-detail/{id}` are three roots and one resource;
//     paths gave 19 such splits, which is the worst kind of rename because one
//     command becomes several and no alias can cover it.
//   - Tags alone are too coarse to be a command. The `computer-inventory` tag
//     carries 56 operations and `computer-groups` carries two complete CRUD
//     sets — `/v3/computer-groups/smart-groups/{id}` and
//     `…/static-groups/{id}` would both want `get`, `update` and `delete`, and
//     disambiguateSameTerminalOps cannot separate them because both terminate
//     in `{id}` with the same parameter count. Tags gave 1 split and a name
//     collision the generator has no way to resolve.
//
// So the tag constrains and the path decides: a tag never splits into unrelated
// resources, and the path root is the boundary inside one. Names come from the
// tag, which is the section heading the API reference publishes.
//
// A path root *can* merge two tags, and eight do — rootOf is computed from the
// whole document and never reads the tag, and resolveRootsWithinTag folds roots
// only inside one tag, so nothing stops two tags computing the same root
// string. That is the correct grouping (a recalculate action on `/v1/users/{id}`
// belongs with the user CRUD it acts on) and it means one of the tags has to
// name the group. groupTag decides, and it is not free: taking whichever path
// sorted last named three of the eight wrongly.
//
// Tags are a sound signal here rather than a convenient one: no path in the
// document may carry two different base tags — enforced by ParseMonolith, since
// the whole design rests on it — and `-preview` is a suffix on an otherwise
// ordinary tag rather than a family of its own.

// previewTagSuffix is stripped so a preview endpoint joins the resource it is a
// preview of, instead of naming a resource of its own.
//
// This is the rule that replaces an accident. 20 of the monolith's tags carry
// the suffix; `DroppedTags` named 3 and the other 17 stayed out of the command
// surface only because every path carrying them happened to be filed under a
// filename that named the canonical resource. Two of the 20 have a non-preview
// sibling to fold into; the rest simply stop announcing themselves as previews.
var previewTagSuffix = regexp.MustCompile(`-preview$`)

// BaseTag returns the tag a path's operations belong to, with any `-preview`
// suffix removed.
func BaseTag(tag string) string {
	return previewTagSuffix.ReplaceAllString(tag, "")
}

// TaggedPath is one document path and the base tag its operations carry.
type TaggedPath struct {
	Path string
	Tag  string
}

// GroupPathsByTagAndCollection assigns every path to a resource, using the tag
// to bound what may be grouped together and the path structure to decide the
// boundary inside a tag.
func GroupPathsByTagAndCollection(paths []TaggedPath) []*PathGroup {
	all := make([]string, 0, len(paths))
	tagOf := make(map[string]string, len(paths))
	for _, tp := range paths {
		all = append(all, tp.Path)
		tagOf[tp.Path] = tp.Tag
	}

	exact, withParamChild := classifyPrefixes(all)
	isRoot := func(prefix []string) bool {
		if len(prefix) == 0 {
			return false
		}
		if len(prefix) == 1 {
			return true
		}
		key := strings.Join(prefix, "/")
		return exact[key] && withParamChild[key]
	}

	// Root per path, then the roots each bound holds.
	rootOf := make(map[string]string, len(all))
	boundOf := make(map[string]string, len(all))
	rootsByBound := map[string]map[string]bool{}
	for _, p := range all {
		_, segs := splitVersionSegment(p)
		root := strings.Join(longestRoot(segs, isRoot), "/")
		rootOf[p] = root
		bound := boundKey(tagOf[p], root)
		boundOf[p] = bound
		if rootsByBound[bound] == nil {
			rootsByBound[bound] = map[string]bool{}
		}
		rootsByBound[bound][root] = true
	}

	// Within a bound, decide which roots survive as resources and where the
	// rest go.
	target := map[string]string{} // bound+"\x00"+root -> surviving root
	for bound, roots := range rootsByBound {
		for from, to := range resolveRootsWithinBound(roots, exact, withParamChild, rootOf, all, boundOf, bound) {
			target[bound+"\x00"+from] = to
		}
	}

	byRoot := map[string]*PathGroup{}
	for _, p := range all {
		root := rootOf[p]
		if to, ok := target[boundOf[p]+"\x00"+root]; ok {
			root = to
		}
		version, _ := splitVersionSegment(p)
		g := byRoot[root]
		if g == nil {
			g = &PathGroup{Root: splitPathSegments(root)}
			byRoot[root] = g
		}
		g.Paths = append(g.Paths, p)
		if !containsInt(g.Versions, version) {
			g.Versions = append(g.Versions, version)
		}
	}

	mergeRoots(byRoot)
	// After the merge, so a group's tag is decided from every path it finally
	// holds. mergeRoots moves paths between groups and cannot re-derive a tag
	// assigned before it ran.
	for _, g := range byRoot {
		g.Tag = groupTag(g.Root, g.Paths, tagOf)
	}
	nameFromTags(byRoot)

	out := make([]*PathGroup, 0, len(byRoot))
	for _, g := range byRoot {
		sort.Strings(g.Paths)
		sort.Ints(g.Versions)
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// boundKey returns the key that bounds which roots may be folded together: the
// tag, or the root itself when the path declares no tag.
//
// Untagged paths must not share one bound. The tag is what says two roots are
// the same resource, and "no tag" says nothing — so pooling every untagged path
// under the empty string hands resolveRootsWithinBound a set of unrelated roots
// and its no-survivor fallback folds them all into the largest one.
// `/v1/health-check` and `/v1/health-status` would come out as a single
// `health-check` resource holding both, which is the finding-1 failure again in
// a different key: a shared key merging things nothing declared to be together.
//
// No path in the 11.31.1 monolith is untagged, so this is a guard against a
// drop rather than a fix for today. It is written as a bound rather than a
// refusal because an untagged path is a perfectly ingestible endpoint — it just
// groups by its own root, which is the answer with no tag to improve on it.
func boundKey(tag, root string) string {
	if tag != "" {
		return "tag:" + tag
	}
	return "root:" + root
}

// groupTag picks the tag that names a group, for the case where the group holds
// paths from more than one.
//
// The group's own root collection decides. That path *is* the resource, where
// every other path in the group is a sub-path of it or an action on it — so
// `/v1/users`, tagged `users`, names the resource holding user CRUD, and the
// two `recalculate` actions tagged `smart-user-groups` do not. Failing a root
// collection, the most frequent tag: the sub-path majority speaking for a group
// no path answers as the collection of.
//
// This replaces last-write-wins over a sorted path list, which named a group
// after whichever of its tags sorted last. Three of the eight merged groups in
// the 11.31.1 monolith took a wrong name that way, and two of the three shipped
// a name that contradicted every string the commands under it printed:
//
//   - `/v1/users` CRUD shipped as `pro smart-user-groups`, on the strength of
//     two recalculate actions, with an `apply` that resolved a name against the
//     user collection and deleted and recreated a **user record**.
//   - `/v2/patch-policies` shipped as `pro patch-policy-logs`, whose own `list`
//     is "Retrieve Patch Policies".
//
// A merge is not itself a defect, so this is a naming rule and not a refusal —
// see the note above. What would be a defect is a merge nobody chose deciding
// the name by sort order, and TestMergedTagGroupsAreNamedByTheirRootCollection
// is the guard against that returning.
func groupTag(root, paths []string, tagOf map[string]string) string {
	rootKey := strings.Join(root, "/")

	all := map[string]int{}
	atRoot := map[string]int{}
	for _, p := range paths {
		tag := tagOf[p]
		all[tag]++
		if _, segs := splitVersionSegment(p); strings.Join(segs, "/") == rootKey {
			atRoot[tag]++
		}
	}
	if tag := mostFrequentTag(atRoot); tag != "" {
		return tag
	}
	return mostFrequentTag(all)
}

// mostFrequentTag returns the most frequent non-empty tag, the
// lexicographically first of them when several tie.
//
// Ties are broken by name rather than left to map order, because the result
// names a command: an unstable answer would move a resource's name between two
// runs of `make generate` with no change to the document.
//
// The empty tag is skipped rather than counted. A path declaring no tag groups
// by its root alone and has no name to contribute, so counting it could only
// suppress a real tag that a group's other paths do carry.
func mostFrequentTag(count map[string]int) string {
	best, bestN := "", 0
	for _, tag := range sortedKeys(count) {
		if tag == "" {
			continue
		}
		if count[tag] > bestN {
			best, bestN = tag, count[tag]
		}
	}
	return best
}

// resolveRootsWithinBound returns the roots of one bound that must be folded
// into another, as from → to. A root absent from the result survives as its own
// resource.
//
// A root survives when it is CRUD-shaped — it answers as a collection *and* has
// a `{param}` child — or when it is a path prefix of another root in the bound,
// which makes it the parent collection rather than a stray. Everything else
// folds into the surviving root it shares the longest path prefix with, or into
// the bound's largest survivor when it shares none.
//
// The two conditions are what keep both failure modes out. Requiring CRUD shape
// is what stops `/v1/computer-inventory/{id}/erase` becoming a resource beside
// the computer inventory it acts on. Exempting a prefix is what stops
// `GET /v1/computer-groups` — the listing of *all* groups — being swallowed by
// whichever of smart-groups or static-groups happened to be largest.
func resolveRootsWithinBound(
	roots map[string]bool,
	exact, hasParamChild map[string]bool,
	rootOf map[string]string,
	all []string,
	boundOf map[string]string,
	bound string,
) map[string]string {
	opCount := map[string]int{}
	for _, p := range all {
		if boundOf[p] == bound {
			opCount[rootOf[p]]++
		}
	}

	names := make([]string, 0, len(roots))
	for r := range roots {
		names = append(names, r)
	}
	sort.Strings(names)

	isPrefixOfAnother := func(r string) bool {
		for _, other := range names {
			if other != r && strings.HasPrefix(other, r+"/") {
				return true
			}
		}
		return false
	}

	var survivors []string
	for _, r := range names {
		if (exact[r] && hasParamChild[r]) || isPrefixOfAnother(r) {
			survivors = append(survivors, r)
		}
	}
	// A bound whose every root is a bare action or lookup still has to produce a
	// resource; the largest root is it.
	if len(survivors) == 0 {
		best := names[0]
		for _, r := range names {
			if opCount[r] > opCount[best] || (opCount[r] == opCount[best] && r < best) {
				best = r
			}
		}
		survivors = []string{best}
	}

	survives := map[string]bool{}
	for _, r := range survivors {
		survives[r] = true
	}

	folds := map[string]string{}
	for _, r := range names {
		if survives[r] {
			continue
		}
		best, bestShared, bestOps := "", -1, -1
		for _, s := range survivors {
			shared := sharedPathSegments(r, s)
			if shared > bestShared || (shared == bestShared && opCount[s] > bestOps) {
				best, bestShared, bestOps = s, shared, opCount[s]
			}
		}
		if best != "" && best != r {
			folds[r] = best
		}
	}
	return folds
}

// sharedPathSegments counts the leading path segments two roots have in common.
func sharedPathSegments(a, b string) int {
	as, bs := strings.Split(a, "/"), strings.Split(b, "/")
	n := 0
	for n < len(as) && n < len(bs) && as[n] == bs[n] {
		n++
	}
	return n
}

// nameFromTags names every group after its OpenAPI tag.
//
// The tag is the section heading the API reference publishes, so a command named
// after it is one a reader can find in the docs. The path is a worse authority
// on the noun than it looks: upstream serves the same resource at
// `/v1/computer-inventory/{id}/erase` and `/v4/computers-inventory`, so the
// singular/plural is an accident of whichever path a reader lands on, where the
// tag is a deliberate choice.
//
// A tag covering several groups cannot name them all, and 14 of them do. The
// primary group — the one whose root prefixes the others, or failing that the
// one with the most paths — takes the bare tag, and each sibling appends the
// path segments that distinguish it. That reproduces the names those siblings
// already have (`computer-groups-smart-groups`, `enrollment-languages`) while
// letting the primary move onto the reference's name.
//
// A group whose tag is empty, or whose derived name would collide anyway, keeps
// its path-derived name.
func nameFromTags(byRoot map[string]*PathGroup) {
	groupsByTag := map[string][]string{}
	for root, g := range byRoot {
		if g.Tag != "" {
			groupsByTag[g.Tag] = append(groupsByTag[g.Tag], root)
		}
	}

	proposed := map[string]string{} // root -> name
	for tag, roots := range groupsByTag {
		sort.Strings(roots)
		if len(roots) == 1 {
			proposed[roots[0]] = tag
			continue
		}
		primary := primaryRoot(roots, byRoot)
		shared := commonRootPrefix(roots)
		for _, root := range roots {
			if root == primary {
				proposed[root] = tag
				continue
			}
			segs := splitPathSegments(root)
			tail := segs[min(shared, len(segs)):]
			if len(tail) == 0 {
				tail = segs[len(segs)-1:]
			}
			proposed[root] = tag + "-" + strings.Join(tail, "-")
		}
	}

	// A proposed name that two groups want, or that an untagged group already
	// holds, is not usable — fall back to the path for those.
	wanted := map[string][]string{}
	for root, name := range proposed {
		wanted[name] = append(wanted[name], root)
	}
	for root, g := range byRoot {
		name, ok := proposed[root]
		if !ok || len(wanted[name]) > 1 {
			g.Name = groupName(g.Root)
			continue
		}
		g.Name = applyNameOverride(name)
	}
}

// primaryRoot picks the group that takes the bare tag name: the root that is a
// path prefix of the others, or the one with the most paths.
func primaryRoot(roots []string, byRoot map[string]*PathGroup) string {
	for _, candidate := range roots {
		prefixesAll := true
		for _, other := range roots {
			if other != candidate && !strings.HasPrefix(other, candidate+"/") {
				prefixesAll = false
				break
			}
		}
		if prefixesAll {
			return candidate
		}
	}
	best := roots[0]
	for _, r := range roots {
		switch {
		case len(byRoot[r].Paths) > len(byRoot[best].Paths):
			best = r
		case len(byRoot[r].Paths) == len(byRoot[best].Paths) && r < best:
			best = r
		}
	}
	return best
}

// commonRootPrefix returns the number of leading path segments every root
// shares.
func commonRootPrefix(roots []string) int {
	if len(roots) == 0 {
		return 0
	}
	shared := len(splitPathSegments(roots[0]))
	for _, r := range roots[1:] {
		if n := sharedPathSegments(roots[0], r); n < shared {
			shared = n
		}
	}
	return shared
}
