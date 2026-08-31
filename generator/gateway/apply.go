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
		v := cov.Verdict(op.Method, op.GatewayPath)
		if op.Wildcard {
			// A wildcard entry covers a subtree, so it is judged on the
			// subtree — see Coverage.VerdictSubtree.
			v = cov.VerdictSubtree(op.GatewayPath)
		}
		op.Set(string(v.Level), string(v.Basis), v.Detail)
		if v.Level == Served {
			continue
		}
		method, path := strings.ToUpper(op.Method), NormalisePath(op.GatewayPath)
		if op.Wildcard {
			method, path = AnyMethod, strings.TrimSuffix(path, "/")+"/"+AnySubpath
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

// Op is one operation to be judged, decoupled from the parser types.
type Op struct {
	Method      string
	GatewayPath string
	// Wildcard asks for the emitted entry to cover every method at
	// GatewayPath and everything beneath it, as "* <path>/**". Set for Classic
	// resources, whose verdict is resource-wide: a Classic command builds its
	// path at runtime from the resource path plus whichever lookup is in play
	// (/id/{}, /name/{}, /serialnumber/{} …), so enumerating op paths at
	// generate time would mean re-deriving the template's own logic and would
	// miss a shape the day one is added.
	Wildcard bool
	// Set records the verdict back onto whatever the caller parsed.
	Set func(level, basis, detail string)
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
