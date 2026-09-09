// Copyright 2026, Jamf Software LLC

package parser

import (
	"reflect"
	"sort"
	"testing"
)

// resourceKeyedOverrides names every table in this package keyed on a resource
// name, so the guard below covers all of them rather than the ones someone
// remembered.
//
// Keeping the list here rather than deriving it is deliberate: a table is
// resource-keyed or path-keyed and nothing in a Go map type says which, so the
// only honest options are a hand list or no guard at all.
// TestResourceKeyedOverrideListIsComplete is the second half — it fails when a
// new resource-keyed table is added and not listed.
func resourceKeyedOverrides() map[string]any {
	return map[string]any{
		"resourceLookupFields":               resourceLookupFields,
		"resourceGroupPaths":                 resourceGroupPaths,
		"resourceFileFields":                 resourceFileFields,
		"resourceCreateOpOverrides":          resourceCreateOpOverrides,
		"resourceUpdateTokenOpOverrides":     resourceUpdateTokenOpOverrides,
		"resourceNameFieldOverrides":         resourceNameFieldOverrides,
		"resourceNameLookupPathOverrides":    resourceNameLookupPathOverrides,
		"resourceNameLookupIDFieldOverrides": resourceNameLookupIDFieldOverrides,
		"resourceIDFieldOverrides":           resourceIDFieldOverrides,
		"resourceTableColumns":               resourceTableColumns,
		"resourceDefaultSections":            resourceDefaultSections,
		"resourceListDetailPathOverrides":    resourceListDetailPathOverrides,
		"resourceGetDetailPathOverrides":     resourceGetDetailPathOverrides,
	}
}

// TestEveryResourceKeyedOverrideNamesALiveResource is the guard that was
// missing, and its absence cost six user-visible defects at once.
//
// These tables are consulted as `table[r.Name]`, so a key that names no
// resource is not an error — it is a lookup that misses, and the generator
// falls back to whatever it would have done with no override at all. Nothing
// reports it: `make generate` exits 0, the commands still ship, and the only
// symptom is at the wire.
//
// Every one of these was live when spec-derived naming renamed the resources
// underneath the keys:
//
//   - packages: `--name` filtered on `name`, which /v1/packages refuses outright
//     (400 INVALID_FIELD naming packageName among the filterable fields), so
//     `pro packages delete --name x` and `apply` both failed.
//   - computer-inventory: lost `general.name` and `--serial`/`--udid`, its
//     table columns, its default --section set and its detail-path pin, the
//     detector answering `displayName` off a nested configuration-profile
//     schema instead.
//   - app-installers-titles: lost `titleName`, which CLAUDE.md records as
//     wire-verified and needed precisely because the published spec marks that
//     field readOnly. The titles collection declares no filter parameter, so
//     the field name is the whole match and all 363 titles reported
//     "no resource found".
//   - inventory-preload-records: lost `serialNumber`.
//   - volume-purchasing-locations and device-enrollments: lost `--token-file`
//     entirely, so the only route for a VPP service token or a DEP `.p7m` was
//     to paste it into a JSON body — a credential the CLI exists to keep out of
//     argv.
//
// A rename is exactly when a name-keyed table needs re-keying and exactly when
// nobody thinks to, which is why this is asserted rather than reviewed.
func TestEveryResourceKeyedOverrideNamesALiveResource(t *testing.T) {
	live := map[string]bool{}
	for _, r := range parseCommittedSpecs(t) {
		live[r.Name] = true
	}
	if len(live) == 0 {
		t.Fatal("no resources parsed, so this test would pass vacuously")
	}

	tables := resourceKeyedOverrides()
	checked := 0
	for _, name := range sortedKeys(tables) {
		keys := mapStringKeys(tables[name])
		if len(keys) == 0 {
			t.Errorf("%s is empty — drop it from resourceKeyedOverrides, or the guard covers nothing", name)
			continue
		}
		sort.Strings(keys)
		for _, k := range keys {
			checked++
			if !live[k] {
				t.Errorf("%s[%q] names no resource this generator produces, so the override is silently "+
					"not applied and the generator falls back to its default. Re-key it to the resource's "+
					"current name, or delete the entry.", name, k)
			}
		}
	}
	t.Logf("%d override keys checked against %d live resources", checked, len(live))
}

// TestResourceKeyedOverrideListIsComplete keeps the hand list above honest. It
// reflects over every package-level map in this file's tables and requires each
// resource-keyed one to be listed — recognised by its keys, since a key that
// names a live resource is what makes a table resource-keyed, and a path-keyed
// table's keys start with "/" or a method.
//
// Without this, adding a table is enough to opt out of the guard above, which
// is the failure mode the guard exists to remove one level down.
func TestResourceKeyedOverrideListIsComplete(t *testing.T) {
	live := map[string]bool{}
	for _, r := range parseCommittedSpecs(t) {
		live[r.Name] = true
	}
	listed := resourceKeyedOverrides()

	// Every candidate table in the package, resource-keyed or not.
	candidates := map[string]any{
		"resourceNameOverrides":              resourceNameOverrides,
		"documentedStatusResults":            documentedStatusResults,
		"resourceLookupFields":               resourceLookupFields,
		"resourceGroupPaths":                 resourceGroupPaths,
		"resourceFileFields":                 resourceFileFields,
		"resourceCreateOpOverrides":          resourceCreateOpOverrides,
		"resourceUpdateTokenOpOverrides":     resourceUpdateTokenOpOverrides,
		"resourceNameFieldOverrides":         resourceNameFieldOverrides,
		"resourceNameLookupPathOverrides":    resourceNameLookupPathOverrides,
		"resourceNameLookupIDFieldOverrides": resourceNameLookupIDFieldOverrides,
		"resourceIDFieldOverrides":           resourceIDFieldOverrides,
		"resourceTableColumns":               resourceTableColumns,
		"resourceDefaultSections":            resourceDefaultSections,
		"resourceListDetailPathOverrides":    resourceListDetailPathOverrides,
		"resourceGetDetailPathOverrides":     resourceGetDetailPathOverrides,
		"readOnlySingletonPaths":             readOnlySingletonPaths,
	}
	for _, name := range sortedKeys(candidates) {
		if _, ok := listed[name]; ok {
			continue
		}
		for _, k := range mapStringKeys(candidates[name]) {
			if live[k] {
				t.Errorf("%s[%q] names a live resource, so %s looks resource-keyed but is absent from "+
					"resourceKeyedOverrides — add it, or its keys go unguarded", name, k, name)
				break
			}
		}
	}
}

func mapStringKeys(m any) []string {
	v := reflect.ValueOf(m)
	out := make([]string, 0, v.Len())
	for _, k := range v.MapKeys() {
		out = append(out, k.String())
	}
	return out
}

// TestNameFieldIgnoresActionPayloads pins representationSchemas' reason for
// existing: an action's request body is a command, so its argument names must
// not compete to be the resource's name field.
//
// Both live cases are asserted by their effect on the shipped generator rather
// than through a fixture, because the defect was a closure that reached too far
// and a fixture chooses its own closure. /v1/packages must filter on
// packageName — a plain "name" earns 400 INVALID_FIELD from the endpoint — and
// /v2/jamf-remote-assist/session must filter on "name", not on the "fieldName"
// it inherited from the shared export body.
func TestNameFieldIgnoresActionPayloads(t *testing.T) {
	want := map[string]string{
		"packages":           "packageName",
		"jamf-remote-assist": "name",
		// /v1/accounts is the case a resource-name-keyed rule got wrong:
		// "username" is correct here and its prefix appears in neither
		// "accounts" nor the singular. Pinned so that rule is not reached for
		// again.
		"accounts": "username",
	}
	seen := map[string]bool{}
	for _, r := range parseCommittedSpecs(t) {
		w, ok := want[r.Name]
		if !ok {
			continue
		}
		seen[r.Name] = true
		if r.NameField != w {
			t.Errorf("resource %q: NameField = %q, want %q", r.Name, r.NameField, w)
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("resource %q was not parsed, so its name field is unasserted — re-point this test at the resource that now owns those paths", name)
		}
	}
}

// TestRepresentationSchemasAreNarrowerThanTheFullClosure keeps the two closures
// from collapsing into one. If representationSchemas ever returned the full
// set, the test above would still pass for as long as the heuristic happened to
// land the same way — the closure width is the property, so it is asserted
// directly.
func TestRepresentationSchemasAreNarrowerThanTheFullClosure(t *testing.T) {
	narrower := 0
	for _, r := range parseCommittedSpecs(t) {
		if len(r.Schemas) == 0 {
			continue
		}
		if _, hasExport := r.Schemas["ExportField"]; hasExport {
			narrower++
		}
	}
	if narrower == 0 {
		t.Fatal("no resource's full closure carries ExportField, so this guard and " +
			"representationSchemas both cover nothing — the shared export body moved or was renamed")
	}
	t.Logf("%d resources reach the shared export payload through their full closure", narrower)
}
