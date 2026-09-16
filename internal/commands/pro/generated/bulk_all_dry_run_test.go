// Copyright 2026, Jamf Software LLC

package generated

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// recordingClient fails the test if it is called at all. A dry run's only
// correct behaviour is to send nothing, and asserting on printed output would
// pass just as well for a run that previewed *and* sent.
type recordingClient struct {
	t     *testing.T
	calls []string
}

func (c *recordingClient) Do(_ context.Context, method, path string, _ io.Reader) (*http.Response, error) {
	c.t.Helper()
	c.calls = append(c.calls, method+" "+path)
	return &http.Response{
		StatusCode: http.StatusNoContent,
		Body:       io.NopCloser(strings.NewReader("")),
		Header:     make(http.Header),
	}, nil
}

// A destructive command's --all reaches the collection endpoint directly, and
// --dry-run has to stop it there.
//
// The trap is that a destructive generated command declares its OWN --dry-run/-n,
// which shadows the root persistent flag — so root.go never installs
// dryRunClient for it, and the command's own branch is the only thing honouring
// -n. That branch sits after the --all block, so --all returned from RunE with a
// live request already sent: wire-checked against a real tenant, `delete --all
// --yes -n` issued DELETE /v1/notifications and got a 204.
//
// jamf-pro-notifications is the only command in this shape today (Jamf Pro 11.32
// added the collection DELETE beside the per-notification one). A bulk --all on a
// NON-destructive op declares no local --dry-run, so dryRunClient is installed
// and covers it.
func TestBulkAllHonoursDryRunBeforeSending(t *testing.T) {
	client := &recordingClient{t: t}
	cmd := NewJamfProNotificationsCmd(&registry.CLIContext{Client: client, Output: newJSONOutput()})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"delete", "--all", "--yes", "--dry-run"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("delete --all --yes --dry-run: %v", err)
	}
	if len(client.calls) > 0 {
		t.Errorf("--dry-run sent %v; a dry run must send nothing", client.calls)
	}
}

// The same command without --dry-run must still reach the collection endpoint,
// or the test above would pass for a --all that does nothing at all.
func TestBulkAllSendsTheCollectionRequestWithoutDryRun(t *testing.T) {
	client := &recordingClient{t: t}
	cmd := NewJamfProNotificationsCmd(&registry.CLIContext{Client: client, Output: newJSONOutput()})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"delete", "--all", "--yes"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("delete --all --yes: %v", err)
	}
	if len(client.calls) != 1 || client.calls[0] != "DELETE /v1/notifications" {
		t.Errorf("calls = %v, want one DELETE /v1/notifications", client.calls)
	}
}

// --all addresses the whole collection, so combining it with an identifier is a
// contradiction rather than a narrowing — and it must be refused before a
// request goes out, not resolved in favour of one of the two.
func TestBulkAllRefusesAPositionalBeforeSending(t *testing.T) {
	client := &recordingClient{t: t}
	cmd := NewJamfProNotificationsCmd(&registry.CLIContext{Client: client, Output: newJSONOutput()})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"delete", "--all", "5", "--yes"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("delete --all 5 was accepted; it has to be refused")
	}
	if !strings.Contains(err.Error(), "--all applies to every") {
		t.Errorf("error %q should explain that --all takes no identifier", err)
	}
	if len(client.calls) > 0 {
		t.Errorf("the refusal still sent %v", client.calls)
	}
}
