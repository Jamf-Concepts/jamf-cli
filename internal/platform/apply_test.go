// Copyright 2026, Jamf Software LLC

package platform

import (
	"strings"
	"testing"
)

func TestApplyName(t *testing.T) {
	tests := []struct {
		name    string
		body    any
		field   string
		want    string
		wantErr bool
	}{
		{"reads the field", map[string]any{"name": "corp-internal"}, "name", "corp-internal", false},
		{"non-standard field", map[string]any{"domain": "example.com"}, "domain", "example.com", false},
		// ReadBody returns nil when neither --from-file nor a pipe nor --set
		// supplied anything. apply cannot proceed from that, and the message has
		// to name both input routes or the caller is told to use a flag they
		// deliberately left off in favour of the pipe.
		{"nil body", nil, "name", "", true},
		{"array body", []any{"a"}, "name", "", true},
		{"field absent", map[string]any{"description": "x"}, "name", "", true},
		// Not coerced. apply matches this value against list results, and "1"
		// matching an item whose name is the integer 1 is a coincidence rather
		// than a resolution — so a numeric or null name is rejected here, where
		// the message can say which field is wrong.
		{"numeric name", map[string]any{"name": float64(1)}, "name", "", true},
		{"null name", map[string]any{"name": nil}, "name", "", true},
		{"empty name", map[string]any{"name": ""}, "name", "", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ApplyName(tc.body, tc.field)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ApplyName() error = nil, want error (got %q)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ApplyName() error = %v", err)
			}
			if got != tc.want {
				t.Errorf("ApplyName() = %q, want %q", got, tc.want)
			}
		})
	}
}

// The two input routes have to both appear in the message a missing body
// produces: a caller piping into apply and getting "use --from-file" reads it
// as the pipe being unsupported, which is exactly what it used to be.
func TestApplyName_NoBodyNamesBothInputRoutes(t *testing.T) {
	_, err := ApplyName(nil, "name")
	if err == nil {
		t.Fatal("ApplyName() error = nil, want error")
	}
	for _, want := range []string{"--from-file", "stdin"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("ApplyName() error = %q, want it to mention %q", err, want)
		}
	}
}
