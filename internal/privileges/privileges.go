// Copyright 2026, Jamf Software LLC

// Package privileges renders a set of Jamf Platform API capability permissions
// into the words Jamf Account prints beside its checkboxes.
//
// It exists because the two vocabularies a 403 can be about are not
// interchangeable, and neither is derivable from the other. A Jamf Pro instance
// enforces API-role privileges with names like "Read Categories"; the platform
// gateway enforces GA capability permissions like categories:read, granted in
// Jamf Account when the API integration is created. The GA consolidation mapped
// several pre-GA privileges onto one capability, so no per-row translation
// exists in either direction — the only correct thing to do is print the
// vocabulary that matches the credential in hand, which is why the caller
// selects the source (see EnrichPrivilegeError) rather than this package
// guessing.
//
// The capability slugs come from a spec — specs/gateway/coverage.json for Pro
// and Classic through the gateway, x-required-privileges for Platform commands.
// catalogue.go turns a slug into a section and permission name, and nothing
// else in here invents a requirement.
package privileges

import (
	"fmt"
	"sort"
	"strings"
)

// Requirement is one row of Jamf Account's permission picker: the section, the
// permission name, and every action on it the caller needs. One row per
// capability rather than one per capability-action pair, because that is how
// the picker presents it — a permission with a checkbox per action — which
// collapses an ordinary CRUD run into a single row.
//
// Slugs holds the capability permissions that produced the row, in the form the
// spec declared them. It is rendered alongside the names because the names are
// what a human ticks and the slugs are what an error message from the gateway,
// or the commands catalog, will say.
type Requirement struct {
	Category   string
	Permission string
	Actions    []string
	Slugs      []string
	// Unknown marks a capability with no catalogue row. Rendered verbatim
	// rather than dropped: a permission this CLI cannot name is still a
	// permission the operator has to find, and silently omitting it would
	// describe an integration that cannot make the call.
	Unknown bool
}

// Collect turns capability permissions in {capability}:{action} form into
// deduplicated picker rows, sorted by section then permission name. That order
// comes from the catalogue rather than from a second hand-maintained list:
// Jamf Account's row order is a weaker contract than its names, since the
// picker can be reordered without anything being renamed.
//
// A slug this package cannot parse is kept as its own row rather than merged
// into a sibling, so an unreadable value cannot disappear into another row's
// checkboxes.
func Collect(scopes []string) []Requirement {
	byKey := map[string]*Requirement{}
	var order []string

	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			continue
		}
		capability, action, ok := splitScope(scope)
		e, known := catalogue[capability]
		key := capability
		if !ok {
			// Unparseable: key on the whole value so it stands alone.
			key = "\x00" + scope
		}
		r, seen := byKey[key]
		if !seen {
			r = &Requirement{Slugs: []string{}}
			switch {
			case !ok:
				r.Permission, r.Unknown = scope, true
			case !known:
				r.Permission, r.Unknown = capability, true
			default:
				r.Category, r.Permission = e.category, e.name
			}
			byKey[key] = r
			order = append(order, key)
		}
		if ok && action != "" {
			r.Actions = appendUnique(r.Actions, action)
		}
		r.Slugs = appendUnique(r.Slugs, scope)
	}

	reqs := make([]Requirement, 0, len(order))
	for _, k := range order {
		reqs = append(reqs, *byKey[k])
	}
	sort.SliceStable(reqs, func(i, j int) bool {
		// Unknown rows sort last: they carry no picker names to sort on, and
		// putting them at the end keeps the readable part of a hint readable.
		if reqs[i].Unknown != reqs[j].Unknown {
			return reqs[j].Unknown
		}
		if reqs[i].Category != reqs[j].Category {
			return reqs[i].Category < reqs[j].Category
		}
		return reqs[i].Permission < reqs[j].Permission
	})
	return reqs
}

// Hint renders the permissions as a remediation hint for a gateway 403, or ""
// when there is nothing to say. The output is one line: exitcode.Error.Hint is
// printed as a single "hint:" line and included verbatim in the JSON error
// envelope.
//
// Every row carries both halves — the picker names to tick and the capability
// slug behind them — because the two audiences are different and both are
// reading the same line. The slug is what the gateway's own error, the
// `commands -o json` catalog and Jamf's spec use; the names are the only way to
// find the checkbox, since the picker is searched by name.
func Hint(scopes []string) string {
	reqs := Collect(scopes)
	if len(reqs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(reqs))
	for _, r := range reqs {
		parts = append(parts, r.String())
	}
	return fmt.Sprintf(
		"grant the %s these permissions in Jamf Account — %s. Names are as the permission picker shows them: %s",
		Marker, strings.Join(parts, "; "), permissionsMapURL)
}

// String renders one row as "Section > Permission: Read, Update (slug, slug)".
func (r Requirement) String() string {
	var b strings.Builder
	if r.Category != "" {
		b.WriteString(r.Category)
		b.WriteString(" > ")
	}
	b.WriteString(r.Permission)
	if labels := r.ActionLabels(); len(labels) > 0 {
		b.WriteString(": ")
		b.WriteString(strings.Join(labels, ", "))
	}
	b.WriteString(" (")
	b.WriteString(strings.Join(r.Slugs, ", "))
	if r.Unknown {
		// Said explicitly rather than left as a bare slug, so the reader knows
		// to search the article rather than the picker for this one.
		b.WriteString(" — no permission name recorded for this capability")
	}
	b.WriteString(")")
	return b.String()
}

// ActionLabels renders the row's actions as the picker labels them, in the
// article's own order — so an ordinary CRUD row reads "Create, Read, Update,
// Delete" rather than alphabetically. An action with no label is printed
// verbatim, after the known ones.
func (r Requirement) ActionLabels() []string {
	remaining := map[string]bool{}
	for _, a := range r.Actions {
		remaining[a] = true
	}
	var out []string
	for _, a := range actionOrder {
		if remaining[a] {
			out = append(out, actionLabels[a])
			delete(remaining, a)
		}
	}
	rest := make([]string, 0, len(remaining))
	for a := range remaining {
		rest = append(rest, a)
	}
	sort.Strings(rest)
	return append(out, rest...)
}

// splitScope divides a capability permission into its capability and action
// halves and reports whether it had that shape at all.
//
// More than one colon is rejected rather than cut at the first, because the
// retired three-part beta slug "create:pro:buildings" would otherwise yield the
// capability "create" — naming a permission that does not exist, under a hint
// blaming the wrong thing. A beta-era slug reaching this is a spec that has not
// been re-ingested, which is worth seeing verbatim.
func splitScope(scope string) (capability, action string, ok bool) {
	capability, action, ok = strings.Cut(scope, ":")
	if !ok || capability == "" || action == "" || strings.Contains(action, ":") {
		return "", "", false
	}
	return capability, action, true
}

func appendUnique(s []string, v string) []string {
	for _, existing := range s {
		if existing == v {
			return s
		}
	}
	return append(s, v)
}

// Marker opens every hint this package renders, and is the sentinel by which a
// later enrichment pass recognises one.
//
// It exists because two layers can each answer a 403 and only one vocabulary is
// correct per credential: internal/client renders the gateway capability
// permissions for the request it actually sent (only it knows the method and
// path), while EnrichPrivilegeError would otherwise append the command's Jamf
// Pro API-role privilege names. Matching on this marker rather than threading
// the resolved auth method down to the error formatter keeps the test for
// "has a platform answer already been given" where the answer is.
const Marker = "Jamf Platform API integration"

// HasHint reports whether hint already carries a platform permission answer.
func HasHint(hint string) bool { return strings.Contains(hint, Marker) }

// GatewayFallbackHint is the answer for a gateway 403 whose operation has no
// recorded capability permission — an unserved path, an unauthenticated
// endpoint, or a tree generated without the coverage manifest. It names the
// right console and the right vocabulary without inventing a permission, and it
// carries Marker so the Jamf Pro privilege names are still suppressed: on a
// gateway credential those name grants that cannot be made.
func GatewayFallbackHint() string {
	return "the Jamf Platform API integration lacks a permission this endpoint requires; check the integration's permissions in Jamf Account — " + permissionsMapURL
}
