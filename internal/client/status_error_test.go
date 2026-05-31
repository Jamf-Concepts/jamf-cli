// Copyright 2026, Jamf Software LLC

package client

import (
	"errors"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
)

func TestHTTPStatusError_Hints(t *testing.T) {
	cases := []struct {
		status int
		code   int
		hint   string // substring (lowercased)
	}{
		{401, exitcode.Authentication, "config validate"},
		{403, exitcode.PermissionDenied, "api privileges"},
		{404, exitcode.NotFound, "list"},
		{429, exitcode.RateLimited, "retry"},
	}
	for _, c := range cases {
		err := httpStatusError(c.status, "GET", "/api/v1/x", []byte("body"))
		var e *exitcode.Error
		if !errors.As(err, &e) {
			t.Fatalf("status %d: not an *exitcode.Error", c.status)
		}
		if e.Code != c.code {
			t.Fatalf("status %d: code = %d, want %d", c.status, e.Code, c.code)
		}
		if !strings.Contains(strings.ToLower(e.Hint), c.hint) {
			t.Fatalf("status %d: hint %q missing %q", c.status, e.Hint, c.hint)
		}
	}

	// Unmapped status -> General, no hint.
	var e *exitcode.Error
	if errors.As(httpStatusError(500, "GET", "/x", []byte("boom")), &e) {
		if e.Code != exitcode.General {
			t.Fatalf("500 code = %d, want General(%d)", e.Code, exitcode.General)
		}
	}
}
