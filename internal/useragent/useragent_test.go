// Copyright 2026, Jamf Software LLC

package useragent

import "testing"

func TestString(t *testing.T) {
	t.Cleanup(func() { Set("dev") })
	for _, c := range []struct{ in, want string }{
		{"1.36.0", "jamf-cli/1.36.0 (+https://github.com/Jamf-Concepts/jamf-cli)"},
		{"v1.36.0", "jamf-cli/1.36.0 (+https://github.com/Jamf-Concepts/jamf-cli)"},
		{"v1.36.1-0.20261111084745-7a0400ddf1e9", "jamf-cli/1.36.1-0.20261111084745-7a0400ddf1e9 (+https://github.com/Jamf-Concepts/jamf-cli)"},
		{"", "jamf-cli/dev (+https://github.com/Jamf-Concepts/jamf-cli)"},
	} {
		Set(c.in)
		if got := String(); got != c.want {
			t.Errorf("Set(%q): String() = %q, want %q", c.in, got, c.want)
		}
	}
}
