// Copyright 2026, Jamf Software LLC

// Package gateway answers whether the Jamf Platform gateway serves a given
// Jamf Pro or Classic API request, and says so in a way an operator can act on.
//
// The gateway does not expose every Jamf Pro endpoint, and its refusals are not
// self-describing: an unrouted namespace answers 403 BAD_PERMISSIONS — byte for
// byte what a missing API-role privilege answers — or Tyk's bare
// "404 page not found". So without this, `pro app-installer-titles list` on a
// platform profile sends an operator to grant a privilege that cannot help.
//
// The table is generated (coverage_gen.go) from the gateway's own published
// artefacts. See generator/gateway for how a verdict is decided and what the
// two levels mean.
package gateway

import "strings"

// AnyMethod and AnySubpath are the wildcards a table entry may carry. A method
// of "*" matches every method; a terminal "**" segment matches the path it sits
// under and everything below it. Both are used by Classic entries, whose
// verdict is resource-wide because a Classic path is assembled at runtime from
// the resource path plus whichever lookup is in play.
const (
	AnyMethod  = "*"
	AnySubpath = "**"
)

// Level is whether the Jamf Platform gateway serves an operation. Mirrors
// generator/gateway.Level; the runtime does not import the generator.
type Level string

const (
	// Served means the gateway's published surface carries it.
	Served Level = ""
	// Unserved means it is not part of that surface, and a gateway profile is
	// refused before a request is sent. The gateway currently routes some
	// endpoints its published artefacts omit, and that is transitional — the
	// route set is being narrowed onto the published surface — so "it works
	// today" is not a reason to let a workflow be built on it.
	Unserved Level = "unserved"
)

// Basis is the evidence behind an Unserved verdict. It selects the wording of
// the refusal and nothing else.
type Basis string

const (
	// BasisProbe: a recorded wire probe found the gateway does not route it.
	BasisProbe Basis = "probe"
	// BasisUnpublished: absent from the gateway's published artefacts. May still
	// be routed today.
	BasisUnpublished Basis = "unpublished"
)

// Finding is one entry in the generated table.
type Finding struct {
	Method string
	Path   string
	Level  Level
	Basis  Basis
	Detail string
}

// Lookup reports what is known about a gateway-form request — the path as it
// will be sent, e.g. "/pro/v1/app-installers/titles/3" or
// "/proclassic/computerconfigurations". Returns false when the gateway is not
// known to omit it, which includes every request in a tree with no manifest.
func Lookup(method, path string) (Finding, bool) {
	method = strings.ToUpper(method)
	// Strip a query string: the table is keyed on paths.
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	for _, f := range unserved {
		if f.Method != AnyMethod && f.Method != method {
			continue
		}
		if matchPath(f.Path, path) {
			return f, true
		}
	}
	return Finding{}, false
}

// matchPath compares a table pattern against a concrete path segment by
// segment. "{}" matches one segment; a terminal "**" matches the path it sits
// under and every path below it.
//
// Segment-wise rather than by plain prefix because a prefix match is wrong in
// both directions here: "/pro/v1/auth" would swallow
// "/pro/v1/authentication-settings", and a pattern ending in {} has to match
// exactly one segment rather than the rest of the path. "**" is opt-in for the
// cases that really are whole-subtree.
func matchPath(pattern, path string) bool {
	ps := strings.Split(strings.Trim(pattern, "/"), "/")
	cs := strings.Split(strings.Trim(path, "/"), "/")

	if n := len(ps); n > 0 && ps[n-1] == AnySubpath {
		// "/a/b/**" covers /a/b and everything under it.
		ps = ps[:n-1]
		if len(cs) < len(ps) {
			return false
		}
		cs = cs[:len(ps)]
	} else if len(ps) != len(cs) {
		return false
	}

	for i := range ps {
		if ps[i] == "{}" {
			if cs[i] == "" {
				return false
			}
			continue
		}
		if ps[i] != cs[i] {
			return false
		}
	}
	return true
}
