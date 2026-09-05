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
// Asserted against the whole shipped tree rather than a fixture, because a
// positive-only field is only present when some command has a value for it: a
// synthetic entry would have to be hand-populated, which is the same
// hand-maintained list this test exists to replace.
func TestEveryCommandEntryFieldReachesTheCatalog(t *testing.T) {
	entries := collectCommands(NewRootCmd("test", "", "", ""), "", "", "")
	if len(entries) == 0 {
		t.Fatal("no commands collected")
	}
	maps := commandEntriesToMaps(entries, true)

	present := map[string]bool{}
	for _, m := range maps {
		for k := range m {
			present[k] = true
		}
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
		if !present[name] {
			t.Errorf("commandEntry.%s is projected to %q, which no command in the catalog carries — "+
				"add it to commandEntriesToMaps, or drop the field: a struct field alone does not "+
				"reach `commands -o json`", typ.Field(i).Name, name)
		}
	}
}
