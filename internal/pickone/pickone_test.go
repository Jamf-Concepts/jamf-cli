// Copyright 2026, Jamf Software LLC

package pickone

import (
	"errors"
	"testing"
)

type rec struct{ id, serial, name string }

var tiers = []Tier[rec]{
	Exact(func(r rec) string { return r.id }),
	Exact(func(r rec) string { return r.serial }),
	Exact(func(r rec) string { return r.name }),
	Fold(func(r rec) string { return r.name }),
}

func TestOne(t *testing.T) {
	items := []rec{
		{"A1", "S1", "Lab"},
		{"A2", "S2", "S1"},
		{"A3", "S3", "Cart"},
		{"A4", "S4", "Cart"},
		{"A5", "S5", "loaners"},
		{"A6", "S6", "Loaners"},
	}
	cases := []struct {
		arg     string
		wantID  string
		wantErr error
		wantN   int
	}{
		{"A2", "A2", nil, 0},
		{"S1", "A1", nil, 0},
		{"Lab", "A1", nil, 0},
		{"lab", "A1", nil, 0},
		{"loaners", "A5", nil, 0},
		{"LOANERS", "", ErrAmbiguous, 2},
		{"Cart", "", ErrAmbiguous, 2},
		{"missing", "", ErrNone, 0},
		{"", "", ErrNone, 0},
	}
	for _, tc := range cases {
		got, cands, err := One(items, tc.arg, tiers...)
		if !errors.Is(err, tc.wantErr) {
			t.Errorf("One(%q) err = %v, want %v", tc.arg, err, tc.wantErr)
		}
		if got.id != tc.wantID {
			t.Errorf("One(%q) = %q, want %q", tc.arg, got.id, tc.wantID)
		}
		if len(cands) != tc.wantN {
			t.Errorf("One(%q) candidates = %v, want %d", tc.arg, cands, tc.wantN)
		}
	}
}
