// Copyright 2026, Jamf Software LLC

package browser

import (
	"runtime"
	"testing"
)

// TestValidateRefusesEverythingThatIsNotAWebURL is the guard that matters most
// here: the URL comes from a config profile or an API response, and a platform
// opener hands a non-http scheme to the OS handler registry, which selects a
// program rather than a browser.
func TestValidateRefusesEverythingThatIsNotAWebURL(t *testing.T) {
	refused := []struct {
		name string
		raw  string
	}{
		{"empty", ""},
		{"whitespace", "   "},
		{"no scheme", "tenant.jamfcloud.com"},
		{"file scheme", "file:///etc/passwd"},
		{"javascript scheme", "javascript:alert(1)"},
		{"custom app scheme", "jamf://open"},
		{"scheme with no host", "https://"},
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := Validate(tc.raw); err == nil {
				t.Errorf("Validate(%q) = %q, want an error", tc.raw, got)
			}
		})
	}
}

func TestValidateNormalisesAWebURL(t *testing.T) {
	cases := map[string]string{
		"https://tenant.jamfcloud.com":   "https://tenant.jamfcloud.com",
		"https://tenant.jamfcloud.com/":  "https://tenant.jamfcloud.com",
		" https://radar.wandera.com/ ":   "https://radar.wandera.com",
		"http://jss.example.com:8443/ui": "http://jss.example.com:8443/ui",
		// An on-premise context path has to survive: it is part of the URL the
		// instance is served under, not decoration.
		"https://jss.example.com:8443/jss/": "https://jss.example.com:8443/jss",
	}
	for raw, want := range cases {
		got, err := Validate(raw)
		if err != nil {
			t.Fatalf("Validate(%q): %v", raw, err)
		}
		if got != want {
			t.Errorf("Validate(%q) = %q, want %q", raw, got, want)
		}
	}
}

// TestCommandPrefersBROWSER pins the override, and that the URL is passed as
// one argument rather than interpolated into a shell string.
func TestCommandPrefersBROWSER(t *testing.T) {
	argv, ok := command("https://example.com/a&b", "firefox")
	if !ok {
		t.Fatal("command reported no opener with $BROWSER set")
	}
	want := []string{"firefox", "https://example.com/a&b"}
	if len(argv) != len(want) {
		t.Fatalf("argv = %v, want %v", argv, want)
	}
	for i := range want {
		if argv[i] != want[i] {
			t.Fatalf("argv = %v, want %v", argv, want)
		}
	}
}

// TestCommandNamesAnOpenerOnThisPlatform keeps the switch honest for whichever
// platform the suite runs on, rather than asserting one OS's answer.
func TestCommandNamesAnOpenerOnThisPlatform(t *testing.T) {
	argv, ok := command("https://example.com", "")
	switch runtime.GOOS {
	case "darwin", "windows", "linux", "freebsd", "openbsd", "netbsd":
		if !ok {
			t.Fatalf("no opener for %s", runtime.GOOS)
		}
		if len(argv) < 2 || argv[len(argv)-1] != "https://example.com" {
			t.Fatalf("argv = %v: the URL must be the last argument", argv)
		}
	default:
		if ok {
			t.Fatalf("unexpected opener %v for %s", argv, runtime.GOOS)
		}
	}
}

// TestOpenValidatesBeforeLaunching asserts a refused URL never reaches an
// exec. The empty $BROWSER path would otherwise run the platform opener, and a
// test that launched a browser is a test nobody can run twice.
func TestOpenValidatesBeforeLaunching(t *testing.T) {
	if _, err := Open("file:///etc/passwd", ""); err == nil {
		t.Fatal("Open accepted a file:// URL")
	}
}
