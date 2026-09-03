// Copyright 2026, Jamf Software LLC

package gateway

import (
	"fmt"
	"sort"
	"strings"
)

// AnyMethod and AnySubpath are the wildcards a table entry may carry: a method
// of "*" matches every method, and a terminal "**" segment matches the path it
// sits under and everything below it.
const (
	AnyMethod  = "*"
	AnySubpath = "**"
)

// Entry is one operation the gateway is not known to serve, in the form the
// runtime needs: a gateway-form path with {} wildcards, plus the method it
// applies to and the evidence behind it.
type Entry struct {
	Method string
	Path   string
	Level  Level
	Basis  Basis
	Detail string
}

// Apply stamps a verdict onto every operation of every parsed resource and
// returns the entries worth carrying into the binary.
//
// modern takes the Pro prefix and classic the Classic one; a caller passes
// accessor closures rather than the parser types so this package stays free of
// an import cycle with generator/parser (which will import it).
func Apply(cov *Coverage, ops []Op) []Entry {
	var entries []Entry
	seen := map[string]bool{}
	for _, op := range ops {
		var v Verdict
		switch op.Scope {
		case ScopeSubtree:
			// Covers a subtree, so it is judged on the subtree — see
			// Coverage.VerdictSubtree.
			v = cov.VerdictSubtree(op.GatewayPath)
		case ScopeSubtreeMethod:
			v = cov.VerdictSubtreeMethod(op.GatewayPath, op.Method)
		default:
			v = cov.Verdict(op.Method, op.GatewayPath)
		}
		op.Set(v)
		if v.Level == Served {
			continue
		}
		method, path := strings.ToUpper(op.Method), NormalisePath(op.GatewayPath)
		switch op.Scope {
		case ScopeSubtree:
			method, path = AnyMethod, strings.TrimSuffix(path, "/")+"/"+AnySubpath
		case ScopeSubtreeMethod:
			path = strings.TrimSuffix(path, "/") + "/" + AnySubpath
		}
		key := method + " " + path
		if seen[key] {
			continue
		}
		seen[key] = true
		entries = append(entries, Entry{Method: method, Path: path, Level: v.Level, Basis: v.Basis, Detail: v.Detail})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Path != entries[j].Path {
			return entries[i].Path < entries[j].Path
		}
		return entries[i].Method < entries[j].Method
	})
	return entries
}

// Scope selects how an Op is judged and how its entry is emitted.
type Scope int

const (
	// ScopeExact is one method on one path — the modern API's shape, where the
	// path is fixed at generate time.
	ScopeExact Scope = iota
	// ScopeSubtree is every method at or beneath the path, emitted as
	// "* <path>/**". A Classic *resource* is judged this way: a Classic command
	// builds its path at runtime from the resource path plus whichever lookup is
	// in play (/id/{}, /name/{}, /serialnumber/{} …), so enumerating op paths at
	// generate time would mean re-deriving the template's own logic and would
	// miss a shape the day one is added.
	ScopeSubtree
	// ScopeSubtreeMethod is one method anywhere at or beneath the path, emitted
	// as "<METHOD> <path>/**". A Classic *subcommand* is judged this way,
	// because the method it sends is fixed even though its path is not: a method
	// the gateway declares nowhere beneath the resource cannot work under any
	// lookup. Without it, a resource that keeps one method and loses the rest
	// reports every subcommand as served — which is what Classic 11.28.0's
	// patchsoftwaretitles withdrawal did.
	ScopeSubtreeMethod
)

// Op is one operation to be judged, decoupled from the parser types.
type Op struct {
	Method      string
	GatewayPath string
	// Scope is the granularity of the verdict; see Scope.
	Scope Scope
	// Set records the verdict back onto whatever the caller parsed. It takes the
	// whole Verdict rather than its strings because the scopes travel with it —
	// per operation for a modern path, per method for a Classic subtree.
	Set func(Verdict)
}

// Summary renders a human-readable count per level, for the generator's log.
func Summary(entries []Entry) string {
	counts := map[Basis]int{}
	for _, e := range entries {
		counts[e.Basis]++
	}
	if len(counts) == 0 {
		return "every operation is served by the gateway"
	}
	var parts []string
	for _, b := range []Basis{BasisProbe, BasisUnpublished} {
		if counts[b] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[b], b))
		}
	}
	return strings.Join(parts, ", ") + " (all refused on a gateway profile)"
}
