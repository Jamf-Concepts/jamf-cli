// Copyright 2026, Jamf Software LLC

package commands

import (
	"net/http"
	"strings"
	"testing"

	platformgen "github.com/Jamf-Concepts/jamf-cli/internal/commands/platform/generated"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// TestGeneratedBaselinesList exercises the spec-generated `pro baselines list`
// command end-to-end using an httptest-backed SDK client. Validates that
// generated code builds the expected URL (with tenant injection), parses the
// envelope ({baselines: [...]}), and unwraps to a flat array.
func TestGeneratedBaselinesList(t *testing.T) {
	sdk, mux := newTestPlatformSDK(t)

	const wantPath = "/api/compliance-benchmarks/v1/tenant/" + testTenantID + "/baselines"
	var seenPath string
	mux.HandleFunc(wantPath, func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		writeJSON(w, map[string]any{
			"baselines": []map[string]any{
				{"id": "cis_lvl1", "title": "CIS Level 1", "ruleCount": 107},
				{"id": "cis_lvl2", "title": "CIS Level 2", "ruleCount": 130},
			},
		})
	})

	out := &captureOutput{}
	cliCtx := &registry.CLIContext{
		PlatformSDKClient: sdk,
		Output:            out,
	}

	cmd := platformgen.NewBaselinesCmd(cliCtx)
	cmd.SetArgs([]string{"list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list baselines: %v", err)
	}

	if seenPath != wantPath {
		t.Errorf("server saw path %q, want %q", seenPath, wantPath)
	}
	body := string(out.rawData)
	if !strings.Contains(body, "cis_lvl1") || !strings.Contains(body, "cis_lvl2") {
		t.Errorf("output missing expected baselines: %s", body)
	}
	// List unwrapping should emit a flat array (no envelope key).
	if strings.Contains(body, `"baselines"`) {
		t.Errorf("output should be unwrapped flat array, still has envelope: %s", body)
	}
}

// TestGeneratedRulesListWithQueryParam validates that a required query flag
// (--baseline-id) round-trips through the generator's flag → query-string path.
func TestGeneratedRulesListWithQueryParam(t *testing.T) {
	sdk, mux := newTestPlatformSDK(t)

	var seenQuery string
	mux.HandleFunc("/api/compliance-benchmarks/v1/tenant/"+testTenantID+"/rules", func(w http.ResponseWriter, r *http.Request) {
		seenQuery = r.URL.RawQuery
		writeJSON(w, map[string]any{
			"rules":   []map[string]any{{"id": "rule-1", "title": "Some rule"}},
			"sources": []map[string]any{{"branch": "main"}},
		})
	})

	out := &captureOutput{}
	cliCtx := &registry.CLIContext{PlatformSDKClient: sdk, Output: out}
	cmd := platformgen.NewRulesCmd(cliCtx)
	cmd.SetArgs([]string{"list", "--baseline-id", "cis_lvl1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list rules: %v", err)
	}
	if !strings.Contains(seenQuery, "baselineId=cis_lvl1") {
		t.Errorf("query string missing baselineId: %q", seenQuery)
	}
}
