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
// resources, and a path root never merges two tags. Names come from the path,
// which keeps the biggest resources on the names they already ship under.
//
// Tags are a sound signal here rather than a convenient one: no path in the
// document carries two different tags, so the grouping is unambiguous, and
// `-preview` is a suffix on an otherwise ordinary tag rather than a family of
// its own.

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

	// Root per path, then the roots each tag holds.
	rootOf := make(map[string]string, len(all))
	rootsByTag := map[string]map[string]bool{}
	for _, p := range all {
		_, segs := splitVersionSegment(p)
		root := strings.Join(longestRoot(segs, isRoot), "/")
		rootOf[p] = root
		if rootsByTag[tagOf[p]] == nil {
			rootsByTag[tagOf[p]] = map[string]bool{}
		}
		rootsByTag[tagOf[p]][root] = true
	}

	// Within a tag, decide which roots survive as resources and where the rest go.
	target := map[string]string{} // tag+"\x00"+root -> surviving root
	for tag, roots := range rootsByTag {
		for from, to := range resolveRootsWithinTag(roots, exact, withParamChild, rootOf, all, tagOf, tag) {
			target[tag+"\x00"+from] = to
		}
	}

	byRoot := map[string]*PathGroup{}
	for _, p := range all {
		root := rootOf[p]
		if to, ok := target[tagOf[p]+"\x00"+root]; ok {
			root = to
		}
		version, _ := splitVersionSegment(p)
		g := byRoot[root]
		if g == nil {
			g = &PathGroup{Root: splitPathSegments(root)}
			byRoot[root] = g
		}
		g.Paths = append(g.Paths, p)
		g.Tag = tagOf[p]
		if !containsInt(g.Versions, version) {
			g.Versions = append(g.Versions, version)
		}
	}

	mergeRoots(byRoot)
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

// resolveRootsWithinTag returns the roots of one tag that must be folded into
// another, as from → to. A root absent from the result survives as its own
// resource.
//
// A root survives when it is CRUD-shaped — it answers as a collection *and* has
// a `{param}` child — or when it is a path prefix of another root in the tag,
// which makes it the parent collection rather than a stray. Everything else
// folds into the surviving root it shares the longest path prefix with, or into
// the tag's largest survivor when it shares none.
//
// The two conditions are what keep both failure modes out. Requiring CRUD shape
// is what stops `/v1/computer-inventory/{id}/erase` becoming a resource beside
// the computer inventory it acts on. Exempting a prefix is what stops
// `GET /v1/computer-groups` — the listing of *all* groups — being swallowed by
// whichever of smart-groups or static-groups happened to be largest.
func resolveRootsWithinTag(
	roots map[string]bool,
	exact, withParamChild map[string]bool,
	rootOf map[string]string,
	all []string,
	tagOf map[string]string,
	tag string,
) map[string]string {
	opCount := map[string]int{}
	for _, p := range all {
		if tagOf[p] == tag {
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
		if (exact[r] && withParamChild[r]) || isPrefixOfAnother(r) {
			survivors = append(survivors, r)
		}
	}
	// A tag whose every root is a bare action or lookup still has to produce a
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
