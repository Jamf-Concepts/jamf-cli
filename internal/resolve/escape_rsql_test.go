// Copyright 2026, Jamf Software LLC

package resolve

import "testing"

func TestEscapeRSQL_BackslashAndQuote(t *testing.T) {
	cases := []struct{ in, want string }{
		{`a\b`, `a\\b`},
		{`\"`, `\\\"`},
		{`say "hi"`, `say \"hi\"`},
		{`Firefox (1).pkg`, `Firefox (1).pkg`},
		{`a,b;c*`, `a,b;c*`},
	}
	for _, tc := range cases {
		if got := EscapeRSQL(tc.in); got != tc.want {
			t.Errorf("EscapeRSQL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
