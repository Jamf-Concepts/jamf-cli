// Copyright 2026, Jamf Software LLC

package gateway

import (
	"fmt"
	"sort"
	"strings"
)

// Level is whether the Jamf Platform gateway serves an operation.
type Level string

const (
	// Served means the gateway's published surface carries it. It is also the
	// answer when there is no manifest to consult.
	Served Level = ""

	// Unserved means it is not part of that surface, and a gateway profile is
	// refused before a request is sent.
	//
	// The gateway currently routes some endpoints its published spec omits, and
	// that is transitional: the deployed route set is being narrowed onto the
	// published surface. So "it works today" is not a reason to allow it —
	// allowing it means a workflow keeps being built on an endpoint that is
	// going away, and the failure then arrives as a breakage with no warning.
	// Basis records which evidence produced the verdict, because the two want
	// different wording, not different behaviour.
	Unserved Level = "unserved"
)

// Basis is the evidence behind an Unserved verdict. It selects the wording of
// the refusal and nothing else.
type Basis string

const (
	// BasisProbe: a recorded wire probe found the gateway does not route it.
	BasisProbe Basis = "probe"
	// BasisUnpublished: the gateway's published spec does not carry it. It may
	// still be routed today.
	BasisUnpublished Basis = "unpublished"
)

// Verdict is the answer for one operation, with the evidence that produced it
// so a user-facing message can state what is known rather than assert.
type Verdict struct {
	Level Level
	Basis Basis
	// Detail is a sentence fragment naming the evidence, e.g. "not declared by
	// the gateway's Jamf Pro API 11.31.0".
	Detail string
	// Scopes are the gateway scopes the operation requires, from the spec's
	// x-required-privileges. Empty for an unserved operation by definition, and
	// legitimately empty for the 44 unauthenticated Jamf Pro endpoints.
	Scopes []string
	// ScopesByMethod is the subtree form of Scopes, set only by VerdictSubtree:
	// a Classic resource is judged as a whole but its scopes are per method
	// (accounts:read for a GET, accounts:update for a PUT), so one flat list
	// would tell a reader that a list needs the delete permission.
	ScopesByMethod map[string][]string
}

// probedUnserved records operations a wire probe found unrouted. It exists
// alongside the derived verdict rather than instead of it: an endpoint with a
// probe behind it is stated as fact, where one that is merely unpublished is
// described as outside the supported surface.
//
// Keyed by "{METHOD} {gateway path}" with parameters normalised to {}; a bare
// path with no method covers every method at or beneath it.
//
// Note what a 403 can and cannot establish, because the intuitive reading is
// wrong. An unrouted path and an under-privileged one are byte-identical:
// /pro/v1/definitely-not-a-real-endpoint and /pro/v1/auth both answer 403 with
// the same {httpStatus, traceId, errors} body, the same BAD_PERMISSIONS code and
// the same headers, differing only in the trace id. A bare Tyk
// "404 page not found" distinguishes an unknown *namespace*
// (/nosuchnamespace/v1/x) and nothing finer. So an entry here needs corroboration
// — the same credential reaching the rest of the namespace in the same run,
// across regions — not just one 403.
// Empty, and it was not always: /pro/v1/app-installers held the only entry for
// three months. That is the shape to expect an entry to end in. It was probed
// unrouted on EU and US on 2026-08-28 and re-probed on 2026-08-31, both times
// against a credential reading the rest of the namespace in the same run — and
// the reason was never a gateway defect: the surface sits under hiddenapi/ in
// jamf/jss, so no bundle published it and no route existed. public-apis-oas#430
// published all 23 operations on 2026-09-03 and the gateway opened the same day
// (verified here: GET /pro/v1/app-installers/titles returns 363 titles on EU
// against a bogus-path 403 control in the same run), so the manifest now
// declares them and the entry would refuse a working surface.
//
// Which is the argument for the test that asserts every key still matches a
// shipped path: nothing in a spec announces that routing has landed, and a
// stale entry here keeps refusing an endpoint that works.
var probedUnserved = map[string]string{}

// forceServed keeps an operation available despite being absent from the
// gateway's published spec. Keyed the same way as probedUnserved.
//
// Empty, and the bar for adding to it is high. It is NOT for "the wire says this
// still works": several endpoints the spec omits do answer today —
// /pro/v1/api-roles returns 15 roles, /pro/v1/api-integrations 81, and
// /pro/v1/api-role-privileges/search and /pro/v2/mdm/commands answer Jamf's own
// 400 — and that is exactly the transitional state the refusal exists to get
// ahead of. An entry here asserts the published surface is wrong, not merely
// ahead of the wire, and needs evidence for that specific claim.
//
// The escape hatch it provides matters most for Classic, whose published spec
// trails the Pro one (11.28.0 against 11.31.0): a Classic resource added since
// 11.28 is indistinguishable here from one withdrawn, and would be refused on a
// stale spec. One resource is affected today (computerconfigurations), and the
// instance 404s it too, so it is dead rather than new.
var forceServed = map[string]string{}

// Verdict answers for one operation. method is the HTTP method; path is the
// gateway-form path (ProPrefix or ClassicPrefix), normalised or not.
//
// A nil Coverage answers Served: no manifest, no verdict. That is deliberate
// rather than defensive. The manifest is committed, but `make generate` has to
// keep working in a tree where nobody has synced the specs, and the honest
// answer there is "unknown", which must not refuse anything.
func (c *Coverage) Verdict(method, path string) Verdict {
	key := NormalisePath(path)
	method = strings.ToUpper(method)

	if reason, ok := matchPrefix(forceServed, method, key); ok {
		return Verdict{Level: Served, Detail: reason}
	}
	if reason, ok := matchPrefix(probedUnserved, method, key); ok {
		return Verdict{Level: Unserved, Basis: BasisProbe, Detail: reason}
	}
	if c == nil {
		return Verdict{}
	}

	specMethods, specHasPath := c.Spec[key]
	if specHasPath && contains(specMethods, method) {
		// Declared, so served. An absent scope list is not an absent operation:
		// 44 Jamf Pro endpoints are unauthenticated and declare none.
		return Verdict{Level: Served, Scopes: c.Scopes[key][method]}
	}

	_, apiName, apiVersion := c.namespace(key)
	if specHasPath {
		return Verdict{
			Level:  Unserved,
			Basis:  BasisUnpublished,
			Detail: fmt.Sprintf("the gateway's %s %s declares %s on this path but not %s", apiName, apiVersion, strings.Join(specMethods, "/"), method),
		}
	}
	return Verdict{
		Level:  Unserved,
		Basis:  BasisUnpublished,
		Detail: fmt.Sprintf("not declared by the gateway's %s %s", apiName, apiVersion),
	}
}

// VerdictSubtree answers for a whole subtree rather than one endpoint: does the
// gateway carry anything at or beneath path?
//
// This is how a Classic resource is judged, and the exact-path form is wrong for
// it. Five Classic resources have no bare collection endpoint at all —
// computerhistory, computerapplications, mobiledevicehistory,
// patchavailabletitles and patchreports are reachable only as
// /computerhistory/id/{} and friends — so asking about /proclassic/computerhistory
// answered "absent" for five resources the gateway serves perfectly well.
func (c *Coverage) VerdictSubtree(path string) Verdict {
	key := strings.TrimSuffix(NormalisePath(path), "/")

	if reason, ok := matchPrefix(forceServed, AnyMethod, key); ok {
		return Verdict{Level: Served, Detail: reason}
	}
	if reason, ok := matchPrefix(probedUnserved, AnyMethod, key); ok {
		return Verdict{Level: Unserved, Basis: BasisProbe, Detail: reason}
	}
	if c == nil {
		return Verdict{}
	}
	if hasSubtree(c.Spec, key) {
		return Verdict{Level: Served, ScopesByMethod: c.subtreeScopes(key)}
	}

	ns, apiName, apiVersion := c.namespace(key)
	if ns == ClassicPrefix {
		return Verdict{
			Level: Unserved,
			Basis: BasisUnpublished,
			Detail: fmt.Sprintf("not declared by the gateway's %s %s, which trails the Pro API's version",
				apiName, apiVersion),
		}
	}
	return Verdict{
		Level:  Unserved,
		Basis:  BasisUnpublished,
		Detail: fmt.Sprintf("not declared by the gateway's %s %s", apiName, apiVersion),
	}
}

// VerdictSubtreeMethod answers for one method across a whole subtree: does the
// gateway declare that method on any path at or beneath path?
//
// This is the granularity a Classic *subcommand* needs, and VerdictSubtree is
// too coarse for it. A Classic resource is judged as a whole because its paths
// are assembled at runtime from the resource path plus whichever lookup is in
// play — but the METHOD each subcommand sends is fixed at generate time, and a
// method the gateway declares nowhere beneath the resource cannot work under any
// lookup. Classic API 11.28.0 withdrew every read on patchsoftwaretitles while
// keeping POST /patchsoftwaretitles/id/{}, so the resource is served and
// `list`, `get`, `update` and `delete` are all dead — a shape the whole-resource
// verdict reports as fine.
func (c *Coverage) VerdictSubtreeMethod(path, method string) Verdict {
	key := strings.TrimSuffix(NormalisePath(path), "/")
	method = strings.ToUpper(method)

	if reason, ok := matchPrefix(forceServed, method, key); ok {
		return Verdict{Level: Served, Detail: reason}
	}
	if reason, ok := matchPrefix(probedUnserved, method, key); ok {
		return Verdict{Level: Unserved, Basis: BasisProbe, Detail: reason}
	}
	if c == nil {
		return Verdict{}
	}
	for p, methods := range c.Spec {
		if p != key && !strings.HasPrefix(p, key+"/") {
			continue
		}
		if contains(methods, method) {
			return Verdict{Level: Served, ScopesByMethod: c.subtreeScopes(key)}
		}
	}

	_, apiName, apiVersion := c.namespace(key)
	return Verdict{
		Level: Unserved,
		Basis: BasisUnpublished,
		Detail: fmt.Sprintf("the gateway's %s %s declares no %s on this resource",
			apiName, apiVersion, method),
	}
}

// subtreeScopes unions the scopes of every operation at or beneath key, keyed by
// method.
//
// Unioning is right for a Classic resource because its paths are one resource
// reached by different lookups — /accounts/userid/{} and /accounts/username/{}
// carry identical scopes per method — so the union is that one answer rather
// than a merge of several. Were a resource ever to disagree with itself, the
// union says so by carrying both, which is the honest rendering of a fact this
// table cannot resolve.
func (c *Coverage) subtreeScopes(key string) map[string][]string {
	if c == nil {
		return nil
	}
	out := map[string][]string{}
	for path, byMethod := range c.Scopes {
		if path != key && !strings.HasPrefix(path, key+"/") {
			continue
		}
		for method, scopes := range byMethod {
			for _, sc := range scopes {
				out[method] = appendUnique(out[method], sc)
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	for _, scopes := range out {
		sort.Strings(scopes)
	}
	return out
}

func hasSubtree[V any](m map[string]V, key string) bool {
	for k := range m {
		if k == key || strings.HasPrefix(k, key+"/") {
			return true
		}
	}
	return false
}

// namespace reports which API a gateway path belongs to, with the name and
// version to quote in a message.
func (c *Coverage) namespace(key string) (prefix, name, version string) {
	if strings.HasPrefix(key, ClassicPrefix+"/") || key == ClassicPrefix {
		return ClassicPrefix, c.Sources.Classic.Title, c.Sources.Classic.Version
	}
	return ProPrefix, c.Sources.Pro.Title, c.Sources.Pro.Version
}

// matchPrefix looks an override up by exact "{METHOD} {path}" first, then by
// the path alone, then by every parent path — so a single entry can cover a
// whole namespace.
func matchPrefix(table map[string]string, method, key string) (string, bool) {
	if r, ok := table[method+" "+key]; ok {
		return r, true
	}
	for p := key; p != "" && p != "/"; p = parent(p) {
		if r, ok := table[method+" "+p]; ok {
			return r, true
		}
		if r, ok := table[p]; ok {
			return r, true
		}
	}
	return "", false
}

func parent(p string) string {
	if i := strings.LastIndex(p, "/"); i > 0 {
		return p[:i]
	}
	return ""
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// OverrideKeys returns the declared override keys, sorted, so a test can assert
// each still matches something the bundle ships. An entry that stops matching is
// how a table like this goes stale: nothing in a spec announces that routing has
// landed, so a stale probedUnserved entry keeps refusing an endpoint that works.
func OverrideKeys() (probed, served []string) {
	for k := range probedUnserved {
		probed = append(probed, k)
	}
	for k := range forceServed {
		served = append(served, k)
	}
	sort.Strings(probed)
	sort.Strings(served)
	return probed, served
}

// ProbedReason returns the recorded probe for a key, for tests and for the
// generated runtime table.
func ProbedReason(key string) string { return probedUnserved[key] }
