// Copyright 2026, Jamf Software LLC

// Package browser opens a URL in the user's default web browser.
//
// The URL reaching Open comes from a config profile or from an API response,
// neither of which this CLI authored, so Validate is not a formality: a
// platform opener hands its argument to the OS handler registry, where a
// non-http scheme selects a program rather than a browser. macOS `open` will
// launch an application for a custom scheme, and on Windows `cmd /c start`
// treats its argument as a shell token. So every Open validates first, and
// callers that only print the URL validate too — the same value is wrong in
// both directions.
package browser

import (
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
)

// Validate returns raw trimmed of a trailing slash, once it is an absolute
// http or https URL with a host. Anything else is an error naming what
// arrived, because the usual cause is a profile holding a host with no
// scheme and the remedy is to fix the profile.
func Validate(raw string) (string, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	if trimmed == "" {
		return "", fmt.Errorf("no URL to open")
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid URL %q: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("refusing to open %q: only http and https URLs are opened in a browser", raw)
	}
	if u.Host == "" {
		return "", fmt.Errorf("refusing to open %q: URL has no host", raw)
	}
	return trimmed, nil
}

// command returns the argv that opens rawURL on this platform, and reports
// false when the platform has no known opener. $BROWSER wins where it is set,
// matching xdg-open and the convention every other CLI follows; it is read as a
// program name rather than a shell string, so a value carrying arguments is
// passed as one word and fails visibly instead of being interpreted.
func command(rawURL, browserEnv string) ([]string, bool) {
	if browserEnv != "" {
		return []string{browserEnv, rawURL}, true
	}
	switch runtime.GOOS {
	case "darwin":
		return []string{"open", rawURL}, true
	case "windows":
		// rundll32 rather than `cmd /c start`: start is a cmd builtin that
		// re-parses its argument, so a URL carrying & or ^ is split.
		return []string{"rundll32", "url.dll,FileProtocolHandler", rawURL}, true
	case "linux", "freebsd", "openbsd", "netbsd":
		return []string{"xdg-open", rawURL}, true
	}
	return nil, false
}

// Open launches rawURL in the default browser. It validates first and returns
// the validated URL so a caller can report exactly what was opened.
func Open(rawURL, browserEnv string) (string, error) {
	validated, err := Validate(rawURL)
	if err != nil {
		return "", err
	}
	argv, ok := command(validated, browserEnv)
	if !ok {
		return "", fmt.Errorf("no known way to open a browser on %s — use --print and open the URL yourself", runtime.GOOS)
	}
	if err := exec.Command(argv[0], argv[1:]...).Start(); err != nil {
		return "", fmt.Errorf("launching %s: %w", argv[0], err)
	}
	return validated, nil
}
