// Copyright 2026, Jamf Software LLC

package registry

import "testing"

func TestEscapeClassicPathSegment(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "Aged 5 Years", "Aged%205%20Years"},
		{"plus", "Aged 5+ Years", "Aged%205%2B%20Years"},
		{"ampersand", "X & Y", "X%20%26%20Y"},
		{"equals", "a=b", "a%3Db"},
		{"colon", "a:b", "a%3Ab"},
		{"at", "a@b", "a%40b"},
		{"parens", "v (1)", "v%20%281%29"},
		{"comma", "a,b", "a%2Cb"},
		{"percent", "a%b", "a%25b"},
		{"unicode", "café", "caf%C3%A9"},
		{"unreserved", "a-b_c.d~e", "a-b_c.d~e"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EscapeClassicPathSegment(tt.in); got != tt.want {
				t.Errorf("EscapeClassicPathSegment(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
