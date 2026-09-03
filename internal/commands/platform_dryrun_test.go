// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	platformgen "github.com/Jamf-Concepts/jamf-cli/internal/commands/platform/generated"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// TestGeneratedPlatformDeletePreviewsBeforeItAuthorises pins the ordering of
// the two gates on a destructive generated platform command, which
// TestGeneratedPlatformMutationsHonourDryRun does not: that one passes --yes,
// so it cannot see a confirmation standing in front of the preview.
//
// ConfirmAction errors when --yes is absent and stdin is not a terminal, which
// is what a CI runner hands every process. With the confirmation first, `-n` on
// a delete previewed nothing and exited 1, and the operator's fix was
// `-n --yes` — a command line that deletes for real the day the -n falls off
// it, or out of JAMF_CLI_ARGS. Interactively the same order prompted
// "Continue? [y/N]" and printed [dry-run] afterwards, teaching the operator
// that confirming is harmless.
func TestGeneratedPlatformDeletePreviewsBeforeItAuthorises(t *testing.T) {
	sdk, mux := newTestPlatformSDK(t)
	var hits int
	mux.HandleFunc("/securitycloud/v1/groups/abc123", func(w http.ResponseWriter, _ *http.Request) {
		hits++
		writeJSON(w, map[string]any{"id": "abc123"})
	})

	cliCtx := &registry.CLIContext{PlatformSDKClient: sdk, Output: &captureOutput{}, DryRun: true}
	cmd := platformgen.NewDeviceGroupsCmd(cliCtx)
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	// No --yes, and go test's stdin is not a terminal.
	cmd.SetArgs([]string{"delete", "abc123"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("delete --dry-run without --yes: %v", err)
	}

	if hits != 0 {
		t.Errorf("server saw %d request(s) under --dry-run, want 0", hits)
	}
	const want = "[dry-run] DELETE /securitycloud/v1/groups/abc123"
	if got := stderr.String(); !strings.Contains(got, want) {
		t.Errorf("stderr = %q, want it to contain %q", got, want)
	}
	if strings.Contains(stderr.String(), "Continue?") {
		t.Errorf("stderr = %q, want no confirmation prompt ahead of the preview", stderr.String())
	}
}

// TestGeneratedPlatformDeletePreviewReportsTheResolvedPath covers the half the
// reordering could have broken: --name resolution has to stay ahead of both
// gates, or the preview reports an unsubstituted {groupId} rather than the
// request that would be sent. The lookup itself is a read and is still made
// under -n — it is what makes the preview specific.
func TestGeneratedPlatformDeletePreviewReportsTheResolvedPath(t *testing.T) {
	sdk, mux := newTestPlatformSDK(t)
	var deletes int
	mux.HandleFunc("/securitycloud/v2/groups", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"groups": []any{
			map[string]any{"id": "abc123", "name": "Engineering"},
		}})
	})
	mux.HandleFunc("/securitycloud/v1/groups/abc123", func(w http.ResponseWriter, _ *http.Request) {
		deletes++
		writeJSON(w, map[string]any{"id": "abc123"})
	})

	cliCtx := &registry.CLIContext{PlatformSDKClient: sdk, Output: &captureOutput{}, DryRun: true}
	cmd := platformgen.NewDeviceGroupsCmd(cliCtx)
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"delete", "--name", "Engineering"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("delete --name --dry-run: %v", err)
	}

	if deletes != 0 {
		t.Errorf("the delete was sent %d time(s) under --dry-run, want 0", deletes)
	}
	const want = "[dry-run] DELETE /securitycloud/v1/groups/abc123"
	if got := stderr.String(); !strings.Contains(got, want) {
		t.Errorf("stderr = %q, want the resolved path %q", got, want)
	}
}
