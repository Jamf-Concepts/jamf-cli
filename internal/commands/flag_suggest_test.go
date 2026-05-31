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
