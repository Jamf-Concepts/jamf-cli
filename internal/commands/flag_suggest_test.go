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
