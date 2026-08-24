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

	securitygen "github.com/Jamf-Concepts/jamf-cli/internal/commands/security/generated"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamf-cli/internal/security"
)

// TestGeneratedRadarMutationsHonourDryRun is the Radar-served half of the
// dry-run gap: internal/security is a hand-rolled client that never passes
// through dryRunClient either, so `risk override` and `device-lifecycle purge`
// — the destructive one — both executed for real under -n.
//
// Asserting on request count rather than output: the point is that nothing
// reaches the API.
func TestGeneratedRadarMutationsHonourDryRun(t *testing.T) {
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

	sc := security.NewClient(
		security.WithAPIBaseURL(srv.URL),
		security.WithSSEBaseURL(srv.URL),
		security.WithRiskCredentials("id", "secret"),
		security.WithLifecycleCredentials("id", "secret"),
	)

	cliCtx := &registry.CLIContext{SecurityClient: sc, Output: &captureOutput{}, DryRun: true}
	cmd := securitygen.NewRiskCmd(cliCtx)
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"override", "--set", "riskLevel=LOW"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("risk override --dry-run: %v", err)
	}

	if hits != 0 {
		t.Errorf("server saw %d request(s) under --dry-run, want 0", hits)
	}
	if got := stderr.String(); !strings.Contains(got, "[dry-run]") {
		t.Errorf("stderr = %q, want a [dry-run] preview", got)
	}
	var body map[string]any
	if i := strings.Index(stderr.String(), "{"); i >= 0 {
		if err := json.Unmarshal([]byte(stderr.String()[i:]), &body); err != nil {
			t.Fatalf("preview body is not JSON: %v", err)
		}
		if body["riskLevel"] != "LOW" {
			t.Errorf("preview body = %v, want the --set value echoed back", body)
		}
	}
}
