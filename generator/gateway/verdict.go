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
	// legitimately empty for the 44 unauthenticated Jamf Pro endpoints; carried
	// because this lookup is the natural home for wiring the Platform 403
	// privilege hint.
	Scopes []string
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
var probedUnserved = map[string]string{
	// Wire-confirmed 2026-08-28 against EU and US, re-confirmed on EU
	// 2026-08-31: every /pro/v1/app-installers path answers 403 BAD_PERMISSIONS
	// on a credential that reads Pro, Classic and Platform in the same run. The
	// surface never had a published spec (the SDK dropped it in 7ed7af2) and
	// stays instance-only.
	"/pro/v1/app-installers": "wire-confirmed unserved on EU and US, 2026-08-28, re-confirmed 2026-08-31",
}

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
		return Verdict{Level: Served}
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
