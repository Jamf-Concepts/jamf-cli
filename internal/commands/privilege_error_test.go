// Copyright 2026, Jamf Software LLC

package commands

import (
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/privileges"
)

func TestEnrichPrivilegeError_Enriches403(t *testing.T) {
	cmd := &cobra.Command{Use: "get", Annotations: map[string]string{
		"jamf:privileges": "Read Computers,Read Mobile Devices",
	}}
	err := exitcode.New(exitcode.PermissionDenied, "permission denied (HTTP 403)").
		WithHint("the authenticated account lacks the required API privileges; check its API role")

	got := EnrichPrivilegeError(cmd, err)

	var e *exitcode.Error
	if !errors.As(got, &e) {
		t.Fatalf("expected *exitcode.Error, got %T", got)
	}
	if !strings.Contains(e.Hint, "Read Computers") || !strings.Contains(e.Hint, "Read Mobile Devices") {
		t.Errorf("hint not enriched with privileges: %q", e.Hint)
	}
}

func TestEnrichPrivilegeError_NoAnnotationUnchanged(t *testing.T) {
	cmd := &cobra.Command{Use: "get"}
	err := exitcode.New(exitcode.PermissionDenied, "denied").WithHint("check its API role")

	got := EnrichPrivilegeError(cmd, err)

	var e *exitcode.Error
	errors.As(got, &e)
	if strings.Contains(e.Hint, "Required privilege") {
		t.Errorf("hint should be unchanged without annotation: %q", e.Hint)
	}
}

func TestEnrichPrivilegeError_Non403Untouched(t *testing.T) {
	cmd := &cobra.Command{Use: "get", Annotations: map[string]string{"jamf:privileges": "Read Computers"}}
	err := exitcode.New(exitcode.NotFound, "nope").WithHint("list to find ids")

	got := EnrichPrivilegeError(cmd, err)

	var e *exitcode.Error
	errors.As(got, &e)
	if strings.Contains(e.Hint, "Required privilege") {
		t.Errorf("non-403 error should be untouched: %q", e.Hint)
	}
}

// fakeAPIError stands in for the platform SDK's *APIResponseError, whose
// concrete type lives in the SDK's internal package and cannot be constructed
// from here.
type fakeAPIError struct{ status int }

func (e *fakeAPIError) Error() string        { return "platform: unexpected status" }
func (e *fakeAPIError) HasStatus(c int) bool { return c == e.status }

func platformCmd(privs string) *cobra.Command {
	return &cobra.Command{
		Use:         "list",
		Annotations: map[string]string{"jamf:privileges": privs, "jamf:api": "platform-gateway"},
	}
}

// A platform 403 arrives from the SDK with no exit code attached at all, so the
// documented permission-denied code has to be applied here or the failure with
// the most specific remedy reports the generic one.
func TestEnrichPrivilegeError_PlatformForbiddenBecomesPermissionDenied(t *testing.T) {
	err := EnrichPrivilegeError(platformCmd("device-groups:read"), &fakeAPIError{status: 403})

	if got := exitcode.CodeFrom(err); got != exitcode.PermissionDenied {
		t.Fatalf("exit code = %d, want %d", got, exitcode.PermissionDenied)
	}
	var e *exitcode.Error
	if !errors.As(err, &e) {
		t.Fatal("want an *exitcode.Error")
	}
	for _, want := range []string{"Jamf Account", "Inventory > Device groups: Read", "device-groups:read"} {
		if !strings.Contains(e.Hint, want) {
			t.Errorf("hint = %q, missing %q", e.Hint, want)
		}
	}
	// The Jamf Pro vocabulary must not appear: those grants do not exist in
	// Jamf Account, so naming them sends the operator to the wrong console.
	if strings.Contains(e.Hint, "Required privilege(s)") {
		t.Errorf("hint = %q, leaked the Jamf Pro privilege wording", e.Hint)
	}
}

func TestEnrichPrivilegeError_PlatformNon403Untouched(t *testing.T) {
	original := &fakeAPIError{status: 404}
	got := EnrichPrivilegeError(platformCmd("device-groups:read"), original)
	if got != error(original) {
		t.Errorf("got %v, want the error unchanged", got)
	}
}

// An operation whose spec declares no privileges still has to name the right
// console: the Jamf Account specs are published with x-required-privileges
// stripped, so "no annotation" is common and is not "no permission needed".
func TestEnrichPrivilegeError_PlatformWithoutAnnotationStillNamesJamfAccount(t *testing.T) {
	err := EnrichPrivilegeError(platformCmd(""), &fakeAPIError{status: 403})
	var e *exitcode.Error
	if !errors.As(err, &e) {
		t.Fatal("want an *exitcode.Error")
	}
	if !privileges.HasHint(e.Hint) {
		t.Errorf("hint = %q, want the platform fallback", e.Hint)
	}
}

// internal/client answers a gateway 403 for the request it actually sent. This
// pass must leave that alone: the command's annotation is the Jamf Pro
// vocabulary, and appending it would name grants Jamf Account does not offer.
func TestEnrichPrivilegeError_GatewayAnswerSuppressesTheProVocabulary(t *testing.T) {
	cmd := &cobra.Command{
		Use:         "list",
		Annotations: map[string]string{"jamf:privileges": "Read Categories", "jamf:api": "pro"},
	}
	gatewayHint := privileges.Hint([]string{"categories:read"})
	err := exitcode.New(exitcode.PermissionDenied, "permission denied (HTTP 403)").WithHint(gatewayHint)

	got := EnrichPrivilegeError(cmd, err)
	var e *exitcode.Error
	if !errors.As(got, &e) {
		t.Fatal("want an *exitcode.Error")
	}
	if e.Hint != gatewayHint {
		t.Errorf("hint = %q, want it left as the gateway answer %q", e.Hint, gatewayHint)
	}
	if strings.Contains(e.Hint, "Read Categories") {
		t.Error("appended the Jamf Pro privilege names over a gateway answer")
	}
}
