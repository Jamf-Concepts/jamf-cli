// Copyright 2026, Jamf Software LLC

package commands

import (
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
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
