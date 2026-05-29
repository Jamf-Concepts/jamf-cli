// Copyright 2026, Jamf Software LLC

package commands

import "testing"

func TestCompareProVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"11.28.0", "11.28.0", 0},
		{"11.27.0", "11.28.0", -1},
		{"11.29.0", "11.28.0", 1},
		{"10.50.0", "11.0.0", -1},
		{"11.28.1", "11.28.0", 1},
		{"11.28.0-t1234", "11.28.0", 0}, // suffix stripped
		{"11.28.0", "11.28.0-t1234", 0}, // suffix stripped
		{"11.27.0-t9", "11.28.0", -1},   // suffix stripped before compare
		{"unknown", "11.28.0", -1},      // unparseable treats as 0.0.0
		{"11.28.0", "unknown", 1},
	}
	for _, tc := range tests {
		got := compareProVersions(tc.a, tc.b)
		if got != tc.want {
			t.Errorf("compareProVersions(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}
