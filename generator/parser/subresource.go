// Copyright 2026, Jamf Software LLC

package parser

import (
	"sort"
	"strings"

	"github.com/iancoleman/strcase"
)

// A tag can cover several path roots, and the grouping merges them into one
// resource. That is right for the resource's identity and wrong for its verbs:
// an independently-writable sub-path flattened into its parent produces a plain
// CRUD verb that silently belongs to the sub-path.
//
// `pro sso-settings delete` was `DELETE /v2/sso/cert` — it deleted the
// certificate, not the SSO configuration — and `download` and `parse` belonged
// to the certificate too. `pro managed-software-updates-plans update` was
// `PUT /v1/managed-software-updates/plans/feature-toggle` on a resource whose
// `list`, `get` and `create` are real plan CRUD. `pro csa delete` deleted the
// CSA token. None of them collided with anything, so no existing pass reached
// them: resolveNoParamConflicts renames a verb that *collides*, and a lone verb
// on a sub-path collides with nothing.
//
// So the sub-path becomes a command of its own and its verbs become plain
// again: `pro sso-settings cert delete`, `pro sso-settings cert download`.

// Three questions the rule leaves open, and the answer taken for each.
//
// **A sub-path with a deeper operation but no write on itself does not split.**
// `/v1/sso/failover` is a GET plus `POST /v1/sso/failover/generate`, so
// `failover` and `generate` both stay flat and `generate` is another lone verb
// on the parent. Widening the rule to "any sub-path with a deeper operation of
// its own" would fix that and would also pull in every `/history` pair with an
// `/export` beneath it, `/pki/certificate-authority/active` with its `/der` and
// `/pem`, `/apns-client-push-status/enable-all-clients` with its `/status`,
// `/inventory-preload/csv` and `/mdm/commands` — turning eighteen appends into
// nested sub-resources for no correctness gain. The rule's justification is
// *ownership*, and "has a deeper operation" is a fact about URL shape rather
// than about ownership. `generate` is also not the defect this exists to fix: it
// shadows no CRUD verb, so TestNoPlainVerbLeavesItsResource does not flag it,
// where `delete`, `update` and `patch` on a sub-path all did.
//
// **A create-and-read-only sub-resource (GET+POST, no PUT or DELETE) will not
// split**, and none exists today. That is the one shape where the rule might be
// wrong and cannot be judged from the document, so it is left to arrive:
// TestSubResourcePartitionIsPinned lists every refused multi-method sub-path, so
// a new GET+POST candidate appears as a diff in that list and gets decided then
// rather than silently.
//
// **`csa` keeping only `tenant-id` beside the split-out `token` reads fine.**
// `pro csa tenant-id` and `pro csa token get|delete` is a better surface than
// `pro csa delete` deleting a token, which is what it did.

// subResourceWriteMethods are the methods that make a sub-path an object rather
// than an operation.
//
// PUT, PATCH or DELETE *on the sub-path itself* is ownership of a separately
// writable thing. A POST is an append or a command submission — an
// `add-history-note`, an `export`, a `delete-multiple` — and belongs to whatever
// it is posted at. That distinction is the whole rule, and it is read off the
// methods the spec declares, so a path added to an existing tag is classified
// with nothing here to update.
//
// Measured over the committed document, the rule admits 9 sub-paths and
// correctly excludes 131 others, 18 of which are `/history` (GET+POST). The
// exclusion is by the rule and not by a `history` special case, which is the
// right outcome: if Jamf ever ships `DELETE /x/history`, splitting it is
// defensible. TestSubResourcePartitionIsPinned holds both halves.
var subResourceWriteMethods = map[string]bool{
	"PUT":    true,
	"PATCH":  true,
	"DELETE": true,
}

// subResourceRoots returns the version-stripped sub-paths of one group that are
// independently writable, shallowest first.
//
// Four things disqualify a candidate, and each is a property of the group rather
// than a taste call:
//
//   - It is the group's own declared root. The root's verbs are the resource's
//     verbs; that is what 50a8677e established.
//   - The root sits inside its subtree, so splitting it out would take the
//     resource's own endpoint with it.
//   - It sits inside another candidate's subtree. One level of nesting is
//     enough for every case the document has, and a second would put a CRUD verb
//     three tokens deep.
//   - **The group holds no operation outside its subtree.** This is the
//     non-degeneracy clause and it is load-bearing: `/v1/app-request/settings`
//     and `/v1/service-discovery-enrollment/well-known-settings` are each the
//     *entire* resource — GET plus PUT, nothing else in the group, and no bare
//     GET on the root. Splitting those produces `pro app-request settings get`
//     for a resource with one object in it, which is a token of stutter and
//     resolves no ambiguity, because there is no sibling verb for the plain one
//     to be confused with. The defect this exists to fix is a verb that reads as
//     the parent's and is not; where the sub-path *is* the parent there is
//     nothing to mis-read.
//
// Note the candidate need not sit beneath the root. `/v1/adue-session-token-
// settings` is grouped under the `enrollment` tag and is nowhere near
// `/v4/enrollment`, and it is exactly the shape this exists for — its GET and
// PUT arrived flattened as `adue-session-token-settings` and
// `update-adue-session-token-settings`.
func subResourceRoots(root []string, ops []*Operation) []string {
	if len(root) == 0 {
		// The per-file ParseSpec path and the platform parser pass no root, so
		// there is nothing to measure a sub-path against. Those keep the naming
		// they had — the same exemption isResourceRootPath makes.
		return nil
	}
	rootPath := "/" + strings.Join(root, "/")

	methodsAt := map[string]map[string]bool{}
	for _, op := range ops {
		p := stripVersionSegments(op.Path)
		if strings.Contains(p, "{") {
			continue
		}
		if methodsAt[p] == nil {
			methodsAt[p] = map[string]bool{}
		}
		methodsAt[p][op.Method] = true
	}

	var candidates []string
	for p, methods := range methodsAt {
		if p == rootPath || strings.HasPrefix(rootPath+"/", p+"/") {
			continue
		}
		writable := false
		for m := range methods {
			if subResourceWriteMethods[m] {
				writable = true
				break
			}
		}
		if writable {
			candidates = append(candidates, p)
		}
	}
	// Shallowest first, so the nesting check below keeps the outermost.
	sort.Slice(candidates, func(i, j int) bool {
		ci, cj := strings.Count(candidates[i], "/"), strings.Count(candidates[j], "/")
		if ci != cj {
			return ci < cj
		}
		return candidates[i] < candidates[j]
	})

	var kept []string
	for _, p := range candidates {
		nested := false
		for _, outer := range kept {
			if strings.HasPrefix(p, outer+"/") {
				nested = true
				break
			}
		}
		if nested {
			continue
		}
		if !opsOutsideSubtree(p, ops) {
			continue
		}
		kept = append(kept, p)
	}
	return kept
}

// opsOutsideSubtree reports whether the group holds an operation that is not
// part of subPath's subtree — the non-degeneracy clause above.
func opsOutsideSubtree(subPath string, ops []*Operation) bool {
	for _, op := range ops {
		if !inSubtree(subPath, op) {
			return true
		}
	}
	return false
}

// inSubtree reports whether an operation belongs to subPath's subtree, comparing
// version-stripped paths so a sub-resource served at two versions stays whole.
func inSubtree(subPath string, op *Operation) bool {
	p := stripVersionSegments(op.Path)
	return p == subPath || strings.HasPrefix(p, subPath+"/")
}

// subResourceName is the command token a sub-path takes beneath its parent: the
// segments the parent's root does not already spell.
//
// A sub-path outside the root's subtree has no shared prefix to trim, so it
// keeps its whole path — `/v1/adue-session-token-settings` under `enrollment`
// becomes `pro enrollment adue-session-token-settings`, which is the name it
// already shipped under, one level deeper.
func subResourceName(root []string, subPath string) string {
	segs := splitPathSegments(subPath)
	rootPath := "/" + strings.Join(root, "/")
	if strings.HasPrefix(subPath, rootPath+"/") {
		segs = segs[len(root):]
	}
	parts := make([]string, 0, len(segs))
	for _, s := range segs {
		parts = append(parts, strcase.ToKebab(s))
	}
	return strings.Join(parts, "-")
}
