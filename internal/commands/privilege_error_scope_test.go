// Copyright 2026, Jamf Software LLC

package commands

import (
	"errors"
	"strings"
	"testing"

	jamfplatform "github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
)

// fakePlatformAPIError stands in for *jamfplatform.APIResponseError, which
// nothing outside the SDK can construct. Both interfaces the hint logic reads
// are satisfied by behaviour, which is what makes a double possible at all.
type fakePlatformAPIError struct {
	status  int
	details []jamfplatform.ErrorDetail
}

func (e *fakePlatformAPIError) Error() string                       { return "api error" }
func (e *fakePlatformAPIError) HasStatus(code int) bool             { return e.status == code }
func (e *fakePlatformAPIError) Details() []jamfplatform.ErrorDetail { return e.details }

// TestPlatformForbiddenDistinguishesItsTwoCauses is the regression test for the
// platform 403 hint giving the wrong remedy.
//
// enrichPlatformPrivilegeError tested only HasStatus(403) and never the
// structured code, so every platform 403 rendered the permissions-grant hint.
// OWNERSHIP_FORBIDDEN is a different fault on the same status — an environment
// ID sent for a tenant-scoped integration, or the reverse — where the grants are
// already correct and the fix is one line of the profile. The operator, or their
// Jamf Account admin, was sent to work a remediation that cannot help.
func TestPlatformForbiddenDistinguishesItsTwoCauses(t *testing.T) {
	t.Run("ownership mismatch names the scope, not the grants", func(t *testing.T) {
		err := EnrichPrivilegeError(platformCmd("categories:read"), &fakePlatformAPIError{
			status: 403,
			details: []jamfplatform.ErrorDetail{
				{Code: "OWNERSHIP_FORBIDDEN", Description: "forbidden"},
			},
		})

		if got := exitcode.CodeFrom(err); got != exitcode.PermissionDenied {
			t.Errorf("exit code = %d, want %d", got, exitcode.PermissionDenied)
		}
		hint := hintOf(t, err)
		for _, want := range []string{"environment-id", "tenant-id", "scope"} {
			if !strings.Contains(hint, want) {
				t.Errorf("hint does not mention %q: %s", want, hint)
			}
		}
		// The whole point is that it does NOT send them to grant permissions.
		if strings.Contains(hint, "categories:read") {
			t.Errorf("hint names a capability permission for a scope mismatch: %s", hint)
		}
	})

	t.Run("a permissions 403 still names the capability", func(t *testing.T) {
		err := EnrichPrivilegeError(platformCmd("categories:read"), &fakePlatformAPIError{
			status: 403,
			details: []jamfplatform.ErrorDetail{
				{Code: "BAD_PERMISSIONS", Description: "forbidden"},
			},
		})
		if got := exitcode.CodeFrom(err); got != exitcode.PermissionDenied {
			t.Errorf("exit code = %d, want %d", got, exitcode.PermissionDenied)
		}
		hint := hintOf(t, err)
		if strings.Contains(hint, "environment-id") {
			t.Errorf("a permissions 403 rendered the scope hint: %s", hint)
		}
		if hint == "" {
			t.Error("a permissions 403 rendered no hint at all")
		}
	})

	t.Run("a 403 carrying no structured code takes the permissions hint", func(t *testing.T) {
		// The gateway's own refusals carry a code; a proxy's may not. The
		// permissions answer is the right default, being the common cause.
		err := EnrichPrivilegeError(platformCmd("categories:read"), &fakePlatformAPIError{status: 403})
		if strings.Contains(hintOf(t, err), "environment-id") {
			t.Error("a code-less 403 took the scope branch")
		}
	})

	t.Run("a non-403 is left alone", func(t *testing.T) {
		in := &fakePlatformAPIError{status: 404}
		if got := EnrichPrivilegeError(platformCmd("categories:read"), in); got != error(in) {
			t.Errorf("a 404 was rewritten: %v", got)
		}
	})
}

func hintOf(t *testing.T, err error) string {
	t.Helper()
	var e *exitcode.Error
	if !errors.As(err, &e) {
		t.Fatalf("error is not an *exitcode.Error: %v", err)
	}
	return e.Hint
}
