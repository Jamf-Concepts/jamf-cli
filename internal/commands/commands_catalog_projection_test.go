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
// Two fields have already been lost this way: gatewaySuccessor was computed,
// stored on the struct and never added to the map (PR #345), and `scopes` was
// about to go the same way — `commands -o json` reported none for all 110
// commands that carry the annotation, with every unit test passing, because the
// tests read scopesOf and the annotation directly.
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
		if knownMissingCatalogKeys[name] != "" {
			// Two-way: an allowlisted key that has started appearing means the
			// fix landed and the entry is now hiding nothing, so it has to go
			// rather than sit here reading as current knowledge.
			if present[name] {
				t.Errorf("%q is in knownMissingCatalogKeys but the catalog now carries it — "+
					"delete the entry (%s)", name, knownMissingCatalogKeys[name])
			}
			continue
		}
		if !present[name] {
			t.Errorf("commandEntry.%s is projected to %q, which no command in the catalog carries — "+
				"add it to commandEntriesToMaps, or drop the field: a struct field alone does not "+
				"reach `commands -o json`", typ.Field(i).Name, name)
		}
	}
}

// knownMissingCatalogKeys are keys this test would otherwise fail on, each with
// the reason and where the fix lives. Deliberately not fixed here: duplicating
// an open PR's change guarantees a conflict on it.
var knownMissingCatalogKeys = map[string]string{
	// gatewaySuccessorOf is called with a command path that has no binary name
	// on the front, while gateway.Successor drops the first field as the binary
	// before matching — so "pro static-computer-groups list" is matched as
	// "static-computer-groups list" and the single curated key never hits. The
	// field is computed, stored and silently empty for every command. Fixed by
	// PR #345; delete this entry when that merges.
	"gatewaySuccessor": "fixed by PR #345",
}
