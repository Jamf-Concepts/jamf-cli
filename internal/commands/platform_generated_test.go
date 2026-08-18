// Copyright 2026, Jamf Software LLC

package commands

import (
	"net/http"
	"strings"
	"testing"

	platformgen "github.com/Jamf-Concepts/jamf-cli/internal/commands/platform/generated"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// TestGeneratedBlueprintsGet validates that `blueprints get` returns the full
// blueprint object — not just the steps array. Regression for the bug where
// detectListArrayKey fired on non-list ops and stripped the response.
func TestGeneratedBlueprintsGet(t *testing.T) {
	sdk, mux := newTestPlatformSDK(t)

	const blueprintID = "bp-123"
	wantPath := "/api/blueprints/v1/tenant/" + testTenantID + "/blueprints/" + blueprintID
	mux.HandleFunc(wantPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"id":              blueprintID,
			"name":            "My Blueprint",
			"deploymentState": map[string]any{"state": "deployed"},
			"steps":           []any{map[string]any{"id": "step-1"}},
		})
	})

	cliCtx, _, out := newTestPlatformContext(t)
	_ = mux // already registered above via sdk's server
	cliCtx.PlatformSDKClient = sdk
	cliCtx.Output = out

	cmd := platformgen.NewBlueprintsCmd(cliCtx)
	cmd.SetArgs([]string{"get", blueprintID})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("blueprints get: %v", err)
	}

	body := string(out.rawData)
	// Full object fields must be present.
	if !strings.Contains(body, `"id"`) || !strings.Contains(body, `"name"`) || !strings.Contains(body, `"deploymentState"`) {
		t.Errorf("get response missing top-level fields: %s", body)
	}
	// Must not be unwrapped to just the steps array.
	if !strings.Contains(body, `"steps"`) || strings.HasPrefix(strings.TrimSpace(body), "[") {
		t.Errorf("get response looks like unwrapped steps array instead of full object: %s", body)
	}
}

// TestGeneratedDeviceGroupsGet validates that `platform-device-groups get` returns the
// full group object — not just the criteria array. Same regression as blueprints.
func TestGeneratedPlatformDeviceGroupsGet(t *testing.T) {
	sdk, mux := newTestPlatformSDK(t)

	const groupID = "dg-456"
	wantPath := "/api/device-groups/v1/tenant/" + testTenantID + "/device-groups/" + groupID
	mux.HandleFunc(wantPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"id":          groupID,
			"name":        "Test Group",
			"groupType":   "static",
			"memberCount": 5,
			"criteria":    []any{map[string]any{"field": "serialNumber", "value": "ABC"}},
		})
	})

	cliCtx, _, out := newTestPlatformContext(t)
	cliCtx.PlatformSDKClient = sdk
	cliCtx.Output = out

	cmd := platformgen.NewPlatformDeviceGroupsCmd(cliCtx)
	cmd.SetArgs([]string{"get", groupID})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("device-groups get: %v", err)
	}

	body := string(out.rawData)
	if !strings.Contains(body, `"id"`) || !strings.Contains(body, `"name"`) || !strings.Contains(body, `"memberCount"`) {
		t.Errorf("get response missing top-level fields: %s", body)
	}
	if strings.HasPrefix(strings.TrimSpace(body), "[") {
		t.Errorf("get response is an array instead of full object: %s", body)
	}
}

// TestGeneratedDeviceActionsUnmanageRequiresConfirm validates that `device-actions
// unmanage` is treated as destructive and rejects without --yes in non-TTY mode.
func TestGeneratedDeviceActionsUnmanageRequiresConfirm(t *testing.T) {
	sdk, _ := newTestPlatformSDK(t)

	cliCtx := &registry.CLIContext{PlatformSDKClient: sdk, Output: &captureOutput{}}
	cmd := platformgen.NewDeviceActionsCmd(cliCtx)
	cmd.SetArgs([]string{"unmanage", "device-id-1"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("unmanage without --yes in non-TTY should return an error")
	}
	if !strings.Contains(err.Error(), "--yes") {
		t.Errorf("error should mention --yes, got: %v", err)
	}
}

// TestGeneratedCommandNilClientError validates that generated commands return
// the full setup guidance (not a bare one-liner) when PlatformSDKClient is nil.
func TestGeneratedCommandNilClientError(t *testing.T) {
	cliCtx := &registry.CLIContext{PlatformSDKClient: nil, Output: &captureOutput{}}
	cmd := platformgen.NewBlueprintsCmd(cliCtx)
	cmd.SetArgs([]string{"list"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when PlatformSDKClient is nil")
	}
	if !strings.Contains(err.Error(), "config add-profile") {
		t.Errorf("error should contain setup instructions, got: %v", err)
	}
}

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
	// Assert the exact wire key, not merely that some key round-tripped. The
	// spec renamed this parameter to kebab-case; while the CLI still sent
	// "baselineId" the server ignored it and `pro rules list` returned an empty
	// list for every baseline — 0 rules where "baseline-id" returns 110. The
	// old assertion passed throughout, because it checked the generator's
	// flag→query plumbing against whatever the stale spec happened to declare.
	if !strings.Contains(seenQuery, "baseline-id=cis_lvl1") {
		t.Errorf("query string missing baseline-id: %q", seenQuery)
	}
	if strings.Contains(seenQuery, "baselineId") {
		t.Errorf("sent the camelCase parameter the server ignores: %q", seenQuery)
	}
}
