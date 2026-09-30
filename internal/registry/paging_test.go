// Copyright 2026, Jamf Software LLC

package registry

import (
	"errors"
	"fmt"
	"testing"
)

func TestShrinkAfterTimeout(t *testing.T) {
	timedOut := fmt.Errorf("fetching page 3: %w", ErrServerTimeout)
	cases := []struct {
		in, want int
		ok       bool
	}{
		{2000, 1000, true},
		{1000, 500, true},
		{250, 125, true},
		{150, MinShrunkPageSize, true}, // halving would undershoot the floor
		{MinShrunkPageSize, MinShrunkPageSize, false},
		{5, 5, false}, // a --limit 5 page is already smaller than the floor
	}
	for _, c := range cases {
		got, ok := ShrinkAfterTimeout(timedOut, c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("ShrinkAfterTimeout(timeout, %d) = (%d, %v), want (%d, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
	// Any other failure is the caller's to return, not a reason to ask for less.
	if _, ok := ShrinkAfterTimeout(errors.New("HTTP 500"), 2000); ok {
		t.Error("ShrinkAfterTimeout shrank a page after a non-timeout error")
	}
}

// Every row a walk already holds must be neither re-fetched into the result
// nor skipped past, whatever size it shrinks to.
func TestPageAt_ResumesAtTheNextUnfetchedRow(t *testing.T) {
	for _, c := range []struct{ offset, size, page, skip int }{
		{0, 1000, 0, 0},
		{6000, 1000, 6, 0},
		{2250, 375, 6, 0},
		{2250, 187, 12, 6},
	} {
		page, skip := PageAt(c.offset, c.size)
		if page != c.page || skip != c.skip {
			t.Errorf("PageAt(%d, %d) = (%d, %d), want (%d, %d)", c.offset, c.size, page, skip, c.page, c.skip)
		}
		if page*c.size+skip != c.offset {
			t.Errorf("PageAt(%d, %d) resumes at row %d", c.offset, c.size, page*c.size+skip)
		}
	}
}
