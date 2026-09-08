// Copyright 2026, Jamf Software LLC

package commands

import "testing"

func TestSuggestFlag(t *testing.T) {
	known := []string{"compact", "output", "quiet", "verbose"}
	if got := suggestFlag("compcat", known); got != "compact" {
		t.Fatalf("suggestFlag(compcat) = %q, want compact", got)
	}
	if got := suggestFlag("ouput", known); got != "output" {
		t.Fatalf("suggestFlag(ouput) = %q, want output", got)
	}
	if got := suggestFlag("zzzzzz", known); got != "" {
		t.Fatalf("suggestFlag(zzzzzz) = %q, want empty (too far)", got)
	}
}

func TestSuggestFlag_ShortTypos(t *testing.T) {
	// Regression: short typos must anchor on the first letter and pick the
	// intended flag, not an unrelated one at the same edit distance.
	flags := []string{"all", "compact", "field", "output", "quiet", "wide"}

	// --fld and --all are both distance 2 from "fld"; only "field" shares the
	// first letter, so it must win instead of losing on alphabetical order.
	if got := suggestFlag("fld", flags); got != "field" {
		t.Errorf("suggestFlag(fld) = %q, want field", got)
	}
	// No flag starts with 'i'; --id must produce no hint rather than --wide.
	if got := suggestFlag("id", flags); got != "" {
		t.Errorf("suggestFlag(id) = %q, want empty", got)
	}
	// A first-letter mismatch is intentionally not suggested.
	if got := suggestFlag("xompact", flags); got != "" {
		t.Errorf("suggestFlag(xompact) = %q, want empty", got)
	}
}

// TestSuggestFlag_RenamedFlagBeatsTheDistanceSearch pins the rename hint.
//
// Edit distance is the wrong tool for a deliberate rename and --file is the
// case that proves it: "file" is two edits from "field" and five from
// "from-file", so before renamedFlags existed everyone migrating off --file was
// pointed at --field — an output-field selector with nothing to do with a
// request body, on a breaking change whose whole point was to be navigable.
func TestSuggestFlag_RenamedFlagBeatsTheDistanceSearch(t *testing.T) {
	// A body command: --from-file is present, --file is not. The rename wins
	// even though --field is nearer by distance.
	body := []string{"field", "from-file", "output", "scaffold", "set"}
	if got := suggestFlag("file", body); got != "from-file" {
		t.Errorf("suggestFlag(file) on a body command = %q, want from-file", got)
	}

	// An upload command: --file is present and correct, so the reverse entry
	// fires and the forward one must not.
	upload := []string{"category-id", "field", "file", "name", "priority"}
	if got := suggestFlag("from-file", upload); got != "file" {
		t.Errorf("suggestFlag(from-file) on an upload command = %q, want file", got)
	}
	if got := suggestFlag("file", upload); got == "from-file" {
		t.Error("suggestFlag(file) on an upload command suggested from-file; --file is the real flag there")
	}
}

// TestSuggestFlag_RenameIsScopedToCommandsThatHaveTheReplacement is the guard
// that lets the two renamedFlags entries coexist. Each fires only where its
// destination is a real flag on the command in hand, so neither can send a
// caller to a flag that command does not have — which would be the same
// misdirection the table exists to remove.
func TestSuggestFlag_RenameIsScopedToCommandsThatHaveTheReplacement(t *testing.T) {
	for from, to := range renamedFlags {
		// Neither name present: no hint, rather than a hint to a flag the
		// command does not accept.
		if got := suggestFlag(from, []string{"output", "quiet"}); got == to {
			t.Errorf("suggestFlag(%q) suggested %q on a command that has neither", from, to)
		}
	}
	// And no command may declare both, or the table would be ambiguous there.
	leaves := runnableLeaves(NewRootCmd("test", "none", "none", "none"))
	for _, l := range leaves {
		if l.cmd.Flags().Lookup("file") != nil && l.cmd.Flags().Lookup("from-file") != nil {
			t.Errorf("leaf %q declares both --file and --from-file; the rename hint is ambiguous there", l.path)
		}
	}
}
