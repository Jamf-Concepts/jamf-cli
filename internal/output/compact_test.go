// Copyright 2026, Jamf Software LLC

package output

import "testing"

func TestProjectCompact_ListAllowlistAndFrequency(t *testing.T) {
	rows := []map[string]any{
		{"id": "1", "name": "a", "rareNote": "x", "lastSeen": "t1"},
		{"id": "2", "name": "b", "lastSeen": "t2"},
		{"id": "3", "name": "c", "lastSeen": "t3"},
	}
	out := Projector{Compact: true}.Apply(rows)
	for _, r := range out {
		if _, ok := r["id"]; !ok {
			t.Fatalf("id dropped: %v", r)
		}
	}
	// rareNote present in 1/3 (33%) and not allowlisted -> dropped.
	if _, ok := out[0]["rareNote"]; ok {
		t.Fatalf("rare non-allowlisted scalar should be dropped: %v", out[0])
	}
	// lastSeen present in all rows -> kept by frequency rule.
	if _, ok := out[0]["lastSeen"]; !ok {
		t.Fatalf("frequent scalar should be kept: %v", out[0])
	}
}

func TestProjectCompact_SingleObjectBlocklist(t *testing.T) {
	rows := []map[string]any{
		{"id": "1", "name": "a", "description": "long blob", "udid": "U-1"},
	}
	out := Projector{Compact: true}.Apply(rows)
	if _, ok := out[0]["description"]; ok {
		t.Fatalf("blocklisted field should be dropped: %v", out[0])
	}
	if _, ok := out[0]["udid"]; !ok {
		t.Fatalf("allowlisted/scalar field should be kept: %v", out[0])
	}
}
