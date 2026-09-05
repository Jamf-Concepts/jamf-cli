// Copyright 2026, Jamf Software LLC

package commands

import (
	"errors"
	"strings"
	"testing"

	jamfplatform "github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform"
	"github.com/spf13/cobra"

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

// platformCmdDeclaringScopes is platformCmd plus the jamf:scopes annotation the
// platform generator stamps from a spec's x-scope-types. Local to this file
// rather than folded into platformCmd, because the other tests there assert the
// hint a command declaring nothing gets — which is the honest answer for the
// three Jamf Account specs, and a different case.
func platformCmdDeclaringScopes(privs, scopes string) *cobra.Command {
	return &cobra.Command{
		Use: "list",
		Annotations: map[string]string{
			"jamf:privileges": privs,
			"jamf:api":        "platform-gateway",
			annotationScopes:  scopes,
		},
	}
}

// The scope-mismatch hint cannot say which level the *credential* belongs to —
// a gateway token is opaque and carries an empty scope — so the levels the
// endpoint accepts are the only concrete thing it can add, and they are what
// sends an operator holding a correctly-granted credential to the right
// remedy instead of comparing two IDs.
//
// Pinned because it is one sentence appended to a hint: deleting it leaves the
// hint reading correctly and says nothing about the level.
func TestTheScopeMismatchHintNamesTheDeclaredLevels(t *testing.T) {
	err := EnrichPrivilegeError(platformCmdDeclaringScopes("blueprints:read", "environment"), &fakePlatformAPIError{
		status: 403,
		details: []jamfplatform.ErrorDetail{
			{Code: "OWNERSHIP_FORBIDDEN", Description: "forbidden"},
		},
	})
	hint := hintOf(t, err)
	if !strings.Contains(hint, "declares environment scope") {
		t.Errorf("the hint does not name the levels the API declares: %s", hint)
	}
	// "declares", never "requires": build v2082 moved six Platform specs to
	// environment-only while the gateway still answers a tenant credential on
	// some of them, so nothing here may present the declaration as a rule.
	if strings.Contains(hint, "requires environment") {
		t.Errorf("the hint overstates the declaration: %s", hint)
	}

	// A command whose spec is silent gets no sentence rather than a guess.
	silent := EnrichPrivilegeError(platformCmdDeclaringScopes("blueprints:read", ""), &fakePlatformAPIError{
		status: 403,
		details: []jamfplatform.ErrorDetail{
			{Code: "OWNERSHIP_FORBIDDEN", Description: "forbidden"},
		},
	})
	if strings.Contains(hintOf(t, silent), "declares") {
		t.Errorf("a command declaring no level should claim none: %s", hintOf(t, silent))
	}
}
