// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	securitygen "github.com/Jamf-Concepts/jamf-cli/internal/commands/security/generated"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamf-cli/internal/security"
)

// newDryRunSecurityClient returns a Security Cloud client pointed at a test
// server that answers the JWT exchange and counts every other request. The
// {customerId} a Device Lifecycle path carries is never user-facing — it comes
// off the Lifecycle JWT's customer_id claim — so the claim is what the purge
// preview's path is asserted against.
func newDryRunSecurityClient(t *testing.T) (*security.Client, *int) {
	t.Helper()
	var hits int
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/login", func(w http.ResponseWriter, _ *http.Request) {
		claims := base64.RawURLEncoding.EncodeToString([]byte(`{"customer_id":"cust-1","exp":9999999999}`))
		writeJSON(w, map[string]any{"token": "h." + claims + ".s"})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		hits++
		writeJSON(w, map[string]any{"ok": true})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return security.NewClient(
		security.WithAPIBaseURL(srv.URL),
		security.WithSSEBaseURL(srv.URL),
		security.WithRiskCredentials("id", "secret"),
		security.WithLifecycleCredentials("id", "secret"),
	), &hits
}

// dryRunPreviewBody returns the JSON document a [dry-run] preview reported.
// Unconditional on purpose: guarding the parse on "is there a { in stderr"
// meant a regression that dropped the body skipped the assertion entirely
// while the remaining [dry-run] check still passed.
func dryRunPreviewBody(t *testing.T, stderr string) map[string]any {
	t.Helper()
	const marker = "[dry-run] Request body:\n"
	i := strings.Index(stderr, marker)
	if i < 0 {
		t.Fatalf("preview reported no request body:\n%s", stderr)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(stderr[i+len(marker):]), &body); err != nil {
		t.Fatalf("preview body is not JSON (%v):\n%s", err, stderr)
	}
	return body
}

// TestGeneratedRadarMutationsHonourDryRun is the Radar-served half of the
// dry-run gap: internal/security is a hand-rolled client that never passes
// through dryRunClient either, so `risk override` and `device-lifecycle purge`
// — the destructive one — both executed for real under -n.
//
// It also pins the ordering of the two gates. `purge` is run with no --yes and
// no terminal on stdin, which is exactly what a CI runner hands the process:
// with the confirmation ahead of the preview that combination errored out
// having previewed nothing, so the operator's only route to a preview was
// `-n --yes` — a command line that purges for real the day the -n falls off it,
// or out of JAMF_CLI_ARGS.
//
// Asserting on request count as well as output: the point is that nothing
// reaches the API.
func TestGeneratedRadarMutationsHonourDryRun(t *testing.T) {
	cases := []struct {
		name       string
		newCmd     func(*registry.CLIContext) *cobra.Command
		args       []string
		wantPre    string
		wantBodyIs func(*testing.T, map[string]any)
	}{
		{
			name:    "risk override",
			newCmd:  securitygen.NewRiskCmd,
			args:    []string{"override", "--set", "riskLevel=LOW"},
			wantPre: "[dry-run] PUT /risk/v1/override",
			wantBodyIs: func(t *testing.T, body map[string]any) {
				if body["riskLevel"] != "LOW" {
					t.Errorf("preview body = %v, want the --set value echoed back", body)
				}
			},
		},
		{
			// The most destructive command in the CLI, run the way CI runs it:
			// no --yes, stdin not a terminal.
			name:    "device-lifecycle purge",
			newCmd:  securitygen.NewDeviceLifecycleCmd,
			args:    []string{"purge", "--set", "externalIds=device-1"},
			wantPre: "[dry-run] POST /device-lifecycle/v1/cust-1/devices/purge/async/external",
			wantBodyIs: func(t *testing.T, body map[string]any) {
				if body["externalIds"] != "device-1" {
					t.Errorf("preview body = %v, want the --set scope echoed back", body)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sc, hits := newDryRunSecurityClient(t)
			cliCtx := &registry.CLIContext{SecurityClient: sc, Output: &captureOutput{}, DryRun: true}
			cmd := tc.newCmd(cliCtx)
			var stderr bytes.Buffer
			cmd.SetErr(&stderr)
			cmd.SetArgs(tc.args)
			if err := cmd.Execute(); err != nil {
				t.Fatalf("%s --dry-run: %v", tc.name, err)
			}

			if *hits != 0 {
				t.Errorf("server saw %d request(s) under --dry-run, want 0", *hits)
			}
			// The method and the resolved path, not merely the "[dry-run]"
			// marker: a preview that reports the wrong endpoint is a preview of
			// something else.
			if got := stderr.String(); !strings.Contains(got, tc.wantPre) {
				t.Errorf("stderr = %q, want it to contain %q", got, tc.wantPre)
			}
			tc.wantBodyIs(t, dryRunPreviewBody(t, stderr.String()))
		})
	}
}
