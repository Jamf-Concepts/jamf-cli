// Copyright 2026, Jamf Software LLC

package commands

import (
	"reflect"
	"strings"
	"testing"
)

// commandEntry is projected into a map by hand before it is printed, so adding
// a field to the struct does not put it in the catalog — and the compiler says
// nothing. This walks the struct's json tags and requires each to appear as a
// key on at least one real command.
//
// Two fields have been lost this way. gatewaySuccessor was computed, stored on
// the struct and never added to the map — dead in the catalog while the runtime
// refusal and the --help caveat both named the successor correctly (fixed in
// #345). And `scopes` was about to go the same way: `commands -o json` reported
// none for all 110 commands carrying the annotation, with every unit test
// passing, because the tests read scopesOf and the annotation directly.
//
// This is the generic half. TestCatalogJSONCarriesTheSuccessorKey is the
// specific one and both are wanted: that test pins gatewaySuccessor's
// positive-only contract in *both* directions (absent for a served command,
// never present-but-empty), which a key-presence sweep cannot see, while this
// one covers a field nobody has written a targeted test for yet.
//
// Run over one reflectively-populated entry rather than the shipped tree. It
// used to sweep the tree, on the argument that a positive-only field is only
// present when some command has a value for it and that a synthetic entry would
// need hand-populating — the same hand-maintained list this test replaces. That
// argument is answered by populating every field through reflection instead of
// by hand: a field added to the struct is populated automatically, so it is
// still the compiler-free half that catches a missing projection, and the test
// no longer depends on the shipped tree happening to carry a value.
//
// The dependency was not hypothetical. gatewaySuccessor is emitted only for a
// refused command with a curated replacement, and the curated table is now
// legitimately empty, so a tree sweep reported the field as unprojected while
// the projection was intact — the guard failing on the absence of live data
// rather than on a defect.
func TestEveryCommandEntryFieldReachesTheCatalog(t *testing.T) {
	maps := commandEntriesToMaps([]commandEntry{populatedEntry(t)}, true)
	if len(maps) != 1 {
		t.Fatalf("commandEntriesToMaps returned %d rows for one entry", len(maps))
	}

	// Aliases and Flags are joined into comma-separated strings under their own
	// keys, so the tag name is what to look for either way.
	typ := reflect.TypeOf(commandEntry{})
	for i := range typ.NumField() {
		tag := typ.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.Split(tag, ",")[0]
		if _, present := maps[0][name]; !present {
			t.Errorf("commandEntry.%s is projected to %q, which the catalog does not carry for an "+
				"entry that has a value for every field — add it to commandEntriesToMaps, or drop "+
				"the field: a struct field alone does not reach `commands -o json`", typ.Field(i).Name, name)
		}
	}
}

// populatedEntry returns a commandEntry with every field set to a non-zero
// value, so a positive-only projection has something to emit for all of them.
//
// Reflective rather than a literal: a literal is a list to keep in step with the
// struct, and forgetting to extend it makes this test pass for a field that
// never reaches the catalog — the failure it exists to catch, one level up. An
// unhandled field kind fails rather than being skipped, for the same reason.
func populatedEntry(t *testing.T) commandEntry {
	t.Helper()
	var e commandEntry
	v := reflect.ValueOf(&e).Elem()
	for i := range v.NumField() {
		f := v.Field(i)
		switch f.Kind() {
		case reflect.String:
			f.SetString("x")
		case reflect.Bool:
			f.SetBool(true)
		case reflect.Slice:
			if f.Type().Elem().Kind() != reflect.String {
				t.Fatalf("commandEntry.%s is a slice of %s, which this populator cannot fill — extend it",
					v.Type().Field(i).Name, f.Type().Elem().Kind())
			}
			f.Set(reflect.ValueOf([]string{"x"}))
		default:
			t.Fatalf("commandEntry.%s is a %s, which this populator cannot fill — extend it, or the "+
				"field is silently exempt from the sweep", v.Type().Field(i).Name, f.Kind())
		}
	}
	return e
}
