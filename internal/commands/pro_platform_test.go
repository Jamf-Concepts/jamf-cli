// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/auth"
	"github.com/Jamf-Concepts/jamf-cli/internal/config"
	"github.com/Jamf-Concepts/jamf-cli/internal/progress"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/blueprints"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/compliancebenchmarks"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/ddmreport"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/devicegroups"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/devices"
)

func strPtr(s string) *string { return &s }

// ── Audit: Platform Checks ─────────────────────────────────────────────────

func TestCheckUndeployedBlueprints(t *testing.T) {
	bps := []blueprints.BlueprintOverview{
		{ID: "bp-1", DeploymentState: &blueprints.DeploymentState{State: "DEPLOYED"}},
		{ID: "bp-2", DeploymentState: &blueprints.DeploymentState{State: "NOT_DEPLOYED"}},
		{ID: "bp-3", DeploymentState: &blueprints.DeploymentState{State: "NOT_DEPLOYED"}},
	}
	result := checkUndeployedBlueprints(bps)
	if result == nil {
		t.Fatal("expected result, got nil")
		return
	}
	if result.AffectedCount != 2 {
		t.Errorf("affected = %d, want 2", result.AffectedCount)
	}
}

func TestCheckUndeployedBlueprints_AllDeployed(t *testing.T) {
	bps := []blueprints.BlueprintOverview{
		{ID: "bp-1", DeploymentState: &blueprints.DeploymentState{State: "DEPLOYED"}},
	}
	result := checkUndeployedBlueprints(bps)
	if result != nil {
		t.Errorf("expected nil, got %+v", result)
	}
}

func TestCheckBlueprintFailures(t *testing.T) {
	cliCtx, mux, _ := newTestPlatformContext(t)
	mux.HandleFunc("/api/blueprints/v1/blueprints/bp-1/report", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, &blueprints.BlueprintStatusDetail{Succeeded: 10})
	})
	mux.HandleFunc("/api/blueprints/v1/blueprints/bp-2/report", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, &blueprints.BlueprintStatusDetail{Succeeded: 8, Failed: 2})
	})
	bps := []blueprints.BlueprintOverview{
		{ID: "bp-1", DeploymentState: &blueprints.DeploymentState{State: "DEPLOYED"}},
		{ID: "bp-2", DeploymentState: &blueprints.DeploymentState{State: "DEPLOYED"}},
	}
	result := checkBlueprintFailures(context.Background(), cliCtx.PlatformSDKClient, bps)
	if result == nil {
		t.Fatal("expected result, got nil")
		return
	}
	if result.AffectedCount != 1 {
		t.Errorf("affected = %d, want 1", result.AffectedCount)
	}
	if result.Severity != severityCritical {
		t.Errorf("severity = %q, want %q", result.Severity, severityCritical)
	}
}

func TestCheckBenchmarkUpdates(t *testing.T) {
	resp := &compliancebenchmarks.BenchmarksResponseV2{
		Benchmarks: []compliancebenchmarks.BenchmarkV2{
			{ID: "bm-1", UpdateAvailable: true},
			{ID: "bm-2", UpdateAvailable: false},
			{ID: "bm-3", UpdateAvailable: true},
		},
	}
	result := checkBenchmarkUpdates(resp)
	if result == nil {
		t.Fatal("expected result, got nil")
		return
	}
	if result.AffectedCount != 2 {
		t.Errorf("affected = %d, want 2", result.AffectedCount)
	}
}

func TestCheckEmptyPlatformScope(t *testing.T) {
	cliCtx, mux, _ := newTestPlatformContext(t)
	mux.HandleFunc("/api/blueprints/v1/blueprints/bp-1", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, &blueprints.BlueprintDetail{Scope: &blueprints.BlueprintScope{DeviceGroups: []string{"g1"}}})
	})
	mux.HandleFunc("/api/blueprints/v1/blueprints/bp-2", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, &blueprints.BlueprintDetail{Scope: &blueprints.BlueprintScope{DeviceGroups: nil}})
	})
	bps := []blueprints.BlueprintOverview{{ID: "bp-1"}, {ID: "bp-2"}}
	bmResp := &compliancebenchmarks.BenchmarksResponseV2{
		Benchmarks: []compliancebenchmarks.BenchmarkV2{
			{ID: "bm-1", Target: &compliancebenchmarks.TargetV2{DeviceGroups: nil}},
		},
	}
	result := checkEmptyPlatformScope(context.Background(), cliCtx.PlatformSDKClient, bps, bmResp)
	if result == nil {
		t.Fatal("expected result, got nil")
		return
	}
	if result.AffectedCount != 2 {
		t.Errorf("affected = %d, want 2 (1 blueprint + 1 benchmark)", result.AffectedCount)
	}
}

func TestCheckFailedDDMDeclarations(t *testing.T) {
	cliCtx, mux, _ := newTestPlatformContext(t)
	mux.HandleFunc("/api/devices/v1/devices", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"results": []devices.DeviceListReadRepresentationV1{{ID: "dev-1"}, {ID: "dev-2"}},
		})
	})
	mux.HandleFunc("/api/ddm/report/v1/devices/dev-1/declarations", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, &ddmreport.FilteredDeviceReportDto{TotalCount: 1, Results: []ddmreport.FilteredResultDto{
			{Status: "SUCCESSFUL", ValidityState: "VALID"},
		}})
	})
	mux.HandleFunc("/api/ddm/report/v1/devices/dev-2/declarations", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, &ddmreport.FilteredDeviceReportDto{TotalCount: 1, Results: []ddmreport.FilteredResultDto{
			{Status: "UNSUCCESSFUL", ValidityState: "INVALID", Reasons: []ddmreport.StatusReportDeclarationReasonDto{
				{Code: "Error.ProfileFailed", Description: "Profile installation failed"},
			}},
		}})
	})
	result := checkFailedDDMDeclarations(context.Background(), cliCtx.PlatformSDKClient)
	if result == nil {
		t.Fatal("expected result, got nil")
		return
	}
	if result.AffectedCount != 1 {
		t.Errorf("affected = %d, want 1", result.AffectedCount)
	}
}

func TestCheckFailedDDMDeclarations_IgnoresInfoReasons(t *testing.T) {
	cliCtx, mux, _ := newTestPlatformContext(t)
	mux.HandleFunc("/api/devices/v1/devices", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"results": []devices.DeviceListReadRepresentationV1{{ID: "dev-1"}},
		})
	})
	mux.HandleFunc("/api/ddm/report/v1/devices/dev-1/declarations", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, &ddmreport.FilteredDeviceReportDto{TotalCount: 1, Results: []ddmreport.FilteredResultDto{
			{Status: "UNSUCCESSFUL", ValidityState: "INVALID", Reasons: []ddmreport.StatusReportDeclarationReasonDto{
				{Code: "Info.DeclarationNotInstalled", Description: "not applicable"},
			}},
		}})
	})
	result := checkFailedDDMDeclarations(context.Background(), cliCtx.PlatformSDKClient)
	if result != nil {
		t.Errorf("expected nil (info-only reasons should be ignored), got %+v", result)
	}
}

// ── Overview: Platform Section ──────────────────────────────────────────────

func TestFetchPlatformOverview(t *testing.T) {
	cliCtx, mux, _ := newTestPlatformContext(t)
	mux.HandleFunc("/api/blueprints/v1/blueprints", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"results": []blueprints.BlueprintOverview{
				{ID: "bp-1", DeploymentState: &blueprints.DeploymentState{State: "DEPLOYED"}},
				{ID: "bp-2", DeploymentState: &blueprints.DeploymentState{State: "NOT_DEPLOYED"}},
			},
		})
	})
	mux.HandleFunc("/api/compliance-benchmarks/v1/benchmarks", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, &compliancebenchmarks.BenchmarksResponseV2{
			Benchmarks: []compliancebenchmarks.BenchmarkV2{
				{ID: "bm-1", UpdateAvailable: true},
			},
		})
	})
	mux.HandleFunc("/api/compliance-benchmarks/v1/benchmarks/bm-1/compliance-percentage", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, &compliancebenchmarks.CompliancePercentage{CompliancePercentage: 92.5})
	})

	section := fetchPlatformOverview(context.Background(), cliCtx)
	if section == nil {
		t.Fatal("expected platform section, got nil")
		return
	}
	if section.Name != "Platform" {
		t.Errorf("section name = %q, want Platform", section.Name)
	}
	if len(section.Items) < 4 {
		t.Errorf("expected at least 4 items, got %d", len(section.Items))
	}
}

func TestFetchPlatformOverview_UsesAllMockData(t *testing.T) {
	cliCtx, mux, _ := newTestPlatformContext(t)
	mux.HandleFunc("/api/blueprints/v1/blueprints", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"results": []blueprints.BlueprintOverview{
				{ID: "bp-1", Name: "Test", DeploymentState: &blueprints.DeploymentState{State: "DEPLOYED"}},
			},
		})
	})
	mux.HandleFunc("/api/compliance-benchmarks/v1/benchmarks", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, &compliancebenchmarks.BenchmarksResponseV2{
			Benchmarks: []compliancebenchmarks.BenchmarkV2{
				{ID: "bm-1", Title: "CIS Benchmark"},
			},
		})
	})
	mux.HandleFunc("/api/compliance-benchmarks/v1/benchmarks/bm-1/compliance-percentage", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, &compliancebenchmarks.CompliancePercentage{CompliancePercentage: 95.0})
	})

	section := fetchPlatformOverview(context.Background(), cliCtx)
	if section == nil {
		t.Fatal("expected platform section, got nil")
		return
	}
	if section.Name != "Platform" {
		t.Errorf("section name = %q, want Platform", section.Name)
	}
	if len(section.Items) < 3 {
		t.Errorf("expected at least 3 items, got %d", len(section.Items))
	}
}

// ── Blueprint Helpers ───────────────────────────────────────────────────────

func TestRandomizePayloadIdentifiers(t *testing.T) {
	config := json.RawMessage(`{
		"payloadContent": [
			{"payloadType": "com.apple.appstore", "payloadIdentifier": "original-id-1"},
			{"payloadType": "com.apple.restrictions", "payloadIdentifier": "original-id-2"}
		],
		"payloadDisplayName": "Test Profile"
	}`)

	steps := []blueprints.BlueprintStep{
		{
			Name: strPtr("Step 1"),
			Components: []blueprints.Component{
				{Identifier: "com.jamf.ddm-configuration-profile", Configuration: config},
			},
		},
	}

	result := randomizePayloadIdentifiers(steps)

	var resultConfig map[string]any
	if err := json.Unmarshal(result[0].Components[0].Configuration, &resultConfig); err != nil {
		t.Fatalf("unmarshalling result: %v", err)
	}

	content, ok := resultConfig["payloadContent"].([]any)
	if !ok || len(content) != 2 {
		t.Fatal("expected 2 payloadContent items")
		return
	}

	for i, item := range content {
		m := item.(map[string]any)
		pid := m["payloadIdentifier"].(string)
		if pid == "original-id-1" || pid == "original-id-2" {
			t.Errorf("payload[%d] identifier was not randomized: %s", i, pid)
		}
		if len(pid) != 36 { // UUID format: 8-4-4-4-12
			t.Errorf("payload[%d] identifier doesn't look like a UUID: %s", i, pid)
		}
	}

	// Display name should be unchanged
	if resultConfig["payloadDisplayName"] != "Test Profile" {
		t.Error("payloadDisplayName was modified")
	}
}

func TestRandomizePayloadIdentifiers_NoPayloads(t *testing.T) {
	config := json.RawMessage(`{"RequirePasscode": true}`)
	steps := []blueprints.BlueprintStep{
		{
			Name: strPtr("Step 1"),
			Components: []blueprints.Component{
				{Identifier: "com.jamf.ddm.passcode-settings", Configuration: config},
			},
		},
	}

	result := randomizePayloadIdentifiers(steps)

	// Config should be unchanged for DDM-native components
	var resultConfig map[string]any
	if err := json.Unmarshal(result[0].Components[0].Configuration, &resultConfig); err != nil {
		t.Fatalf("unmarshalling result: %v", err)
	}
	if resultConfig["RequirePasscode"] != true {
		t.Error("DDM-native config was modified")
	}
}

// ── DDM Classification ─────────────────────────────────────────────────────

func TestExtractBlueprintIDFromDecl(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Blueprint_086bb302-ba21-4190-9740-0d71916616b6_s1_c1_sys_cfg1", "086bb302-ba21-4190-9740-0d71916616b6"},
		{"Blueprint_d695bccd-7341-4129-a84a-dd26ebbd9306_s1_c1_sys_act27", "d695bccd-7341-4129-a84a-dd26ebbd9306"},
		{"blueprint-device-groups", ""},
		{"4e17c157-9afc-46fc-9574-636755da5584", ""},
		{"short", ""},
	}
	for _, tt := range tests {
		got := extractBlueprintIDFromDecl(tt.input)
		if got != tt.want {
			t.Errorf("extractBlueprintIDFromDecl(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestClassifyDeclaration(t *testing.T) {
	bpNames := map[string]string{
		"086bb302-ba21-4190-9740-0d71916616b6": "I Love jamf-cli",
	}

	tests := []struct {
		declID   string
		wantSrc  string
		wantKind string
	}{
		{"Blueprint_086bb302-ba21-4190-9740-0d71916616b6_s1_c1_sys_cfg1", "I Love jamf-cli", "blueprint"},
		{"Blueprint_aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee_s1", "aaaaaaaa...", "blueprint"},
		{"blueprint-device-groups", "Device Group Membership", "system"},
		{"4e17c157-9afc-46fc-9574-636755da5584", "4e17c157-9afc-46fc-9574-636755da5584", "standalone"},
	}
	for _, tt := range tests {
		src, kind := classifyDeclaration(tt.declID, bpNames, nil)
		if src != tt.wantSrc {
			t.Errorf("classifyDeclaration(%q) source = %q, want %q", tt.declID, src, tt.wantSrc)
		}
		if kind != tt.wantKind {
			t.Errorf("classifyDeclaration(%q) kind = %q, want %q", tt.declID, kind, tt.wantKind)
		}
	}
}

func TestOnlyHasIgnorableReasons(t *testing.T) {
	tests := []struct {
		name    string
		reasons []ddmreport.StatusReportDeclarationReasonDto
		want    bool
	}{
		{"no reasons", nil, true},
		{"only ignorable", []ddmreport.StatusReportDeclarationReasonDto{
			{Code: "Info.DeclarationNotInstalled"},
		}, true},
		{"actionable reason", []ddmreport.StatusReportDeclarationReasonDto{
			{Code: "Error.ProfileFailed", Description: "Profile installation failed"},
		}, false},
		{"mixed", []ddmreport.StatusReportDeclarationReasonDto{
			{Code: "Info.DeclarationNotInstalled"},
			{Code: "Error.ConfigurationAlreadyPresent"},
		}, false},
	}
	for _, tt := range tests {
		got := onlyHasIgnorableReasons(tt.reasons)
		if got != tt.want {
			t.Errorf("onlyHasIgnorableReasons(%s) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// ── Scope Helpers ───────────────────────────────────────────────────────────

func TestScopeOverlaps(t *testing.T) {
	target := map[string]bool{"g1": true, "g2": true}

	if !scopeOverlaps([]string{"g1", "g3"}, target) {
		t.Error("expected overlap with g1")
	}
	if scopeOverlaps([]string{"g3", "g4"}, target) {
		t.Error("expected no overlap")
	}
	if scopeOverlaps(nil, target) {
		t.Error("expected no overlap for nil scope")
	}
}

// ── Test helpers ────────────────────────────────────────────────────────────

// captureOutput implements registry.OutputFormatter and stores data written via PrintRaw.
type captureOutput struct {
	rawData []byte
}

func (o *captureOutput) PrintResponse(_ *http.Response) error { return nil }
func (o *captureOutput) PrintRaw(data []byte) error {
	o.rawData = data
	return nil
}
func (o *captureOutput) PrintBytes(data []byte) error { o.rawData = data; return nil }
func (o *captureOutput) Format() string               { return "json" }
func (o *captureOutput) PaginationProgress() *progress.Reporter {
	return progress.New(io.Discard, progress.Silent)
}

// writeTempJSON marshals v to a temporary JSON file, returning the path. Caller must remove it.
func writeTempJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshalling temp JSON: %v", err)
	}
	f, err := os.CreateTemp("", "jamf-cli-test-*.json")
	if err != nil {
		t.Fatalf("creating temp file: %v", err)
	}
	if _, err := f.Write(data); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}
	_ = f.Close()
	t.Cleanup(func() { _ = os.Remove(f.Name()) })
	return f.Name()
}

// ── Compliance Benchmark Helpers ────────────────────────────────────────────

func TestBenchmarkToPortable_ResolvesGroupNames(t *testing.T) {
	bm := &compliancebenchmarks.BenchmarkResponseV2{
		Title:           "My Benchmark",
		Description:     "desc",
		BaselineID:      "bl-1",
		EnforcementMode: "AUDIT",
		Sources:         []compliancebenchmarks.Source{{Branch: "main"}},
		Rules: []compliancebenchmarks.RuleInfo{
			{ID: "rule-1", Enabled: true},
			{ID: "rule-2", Enabled: false},
		},
		SelectedOsVersions: []compliancebenchmarks.OsVersion{{OsType: "MAC_OS", OsVersion: 26}},
		Target:             &compliancebenchmarks.TargetV2{DeviceGroups: []string{"grp-id-1", "grp-id-unknown"}},
	}
	groupByID := map[string]devicegroups.DeviceGroupListReadRepresentationV1{
		"grp-id-1": {Name: "All Managed Clients", DeviceType: "COMPUTER", GroupType: "SMART"},
	}

	portable := benchmarkToPortable(bm, groupByID)

	if portable.Title != "My Benchmark" {
		t.Errorf("title = %q, want %q", portable.Title, "My Benchmark")
	}
	if portable.SourceBaselineID != "bl-1" {
		t.Errorf("sourceBaselineId = %q, want %q", portable.SourceBaselineID, "bl-1")
	}
	if len(portable.Rules) != 2 {
		t.Fatalf("rules count = %d, want 2", len(portable.Rules))
	}
	if portable.Rules[0].ID != "rule-1" || !portable.Rules[0].Enabled {
		t.Errorf("rule[0] = {%s, %v}, want {rule-1, true}", portable.Rules[0].ID, portable.Rules[0].Enabled)
	}
	if portable.Rules[1].ID != "rule-2" || portable.Rules[1].Enabled {
		t.Errorf("rule[1] = {%s, %v}, want {rule-2, false}", portable.Rules[1].ID, portable.Rules[1].Enabled)
	}
	if len(portable.Target.DeviceGroups) != 2 {
		t.Fatalf("device groups count = %d, want 2", len(portable.Target.DeviceGroups))
	}
	// Known group gets resolved to name
	if portable.Target.DeviceGroups[0].Name != "All Managed Clients" {
		t.Errorf("group[0].Name = %q, want %q", portable.Target.DeviceGroups[0].Name, "All Managed Clients")
	}
	if portable.Target.DeviceGroups[0].DeviceType != "COMPUTER" {
		t.Errorf("group[0].DeviceType = %q, want %q", portable.Target.DeviceGroups[0].DeviceType, "COMPUTER")
	}
	// Unknown group falls back to raw ID
	if portable.Target.DeviceGroups[1].Name != "grp-id-unknown" {
		t.Errorf("group[1].Name = %q, want raw ID %q", portable.Target.DeviceGroups[1].Name, "grp-id-unknown")
	}
	// Selected OS versions round-trip into the portable export.
	if len(portable.SelectedOsVersions) != 1 || portable.SelectedOsVersions[0].OsType != "MAC_OS" || portable.SelectedOsVersions[0].OsVersion != 26 {
		t.Errorf("selectedOsVersions not carried: %v", portable.SelectedOsVersions)
	}
}

func TestBenchmarkToPortable_PreservesODV(t *testing.T) {
	odvVal := "90"
	bm := &compliancebenchmarks.BenchmarkResponseV2{
		Rules: []compliancebenchmarks.RuleInfo{
			{ID: "rule-odv", Enabled: true, ODV: &compliancebenchmarks.OrganizationDefinedValue{Value: odvVal}},
			{ID: "rule-no-odv", Enabled: true},
		},
	}
	portable := benchmarkToPortable(bm, nil)
	if portable.Rules[0].ODV == nil || portable.Rules[0].ODV.Value != odvVal {
		t.Errorf("rule[0] ODV not preserved: got %v", portable.Rules[0].ODV)
	}
	if portable.Rules[1].ODV != nil {
		t.Errorf("rule[1] ODV should be nil, got %v", portable.Rules[1].ODV)
	}
}

func TestCBScaffold_StaticTemplate(t *testing.T) {
	cliCtx, _, _ := newTestPlatformContext(t)

	cmd := newCBApplyCmd(cliCtx)
	cmd.SetArgs([]string{"--scaffold"})
	raw := captureStdout(t, func() {
		if err := cmd.Execute(); err != nil {
			t.Errorf("scaffold: %v", err)
		}
	})

	var result benchmarkPortableInput
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal scaffold: %v\nraw: %s", err, raw)
	}
	if result.Title == "" {
		t.Error("scaffold title should not be empty")
	}
	if result.EnforcementMode == "" {
		t.Error("scaffold enforcementMode should not be empty")
	}
	if len(result.Target.DeviceGroups) == 0 {
		t.Error("scaffold should include at least one device group placeholder")
	}
	if result.Target.DeviceGroups[0].DeviceType == "" {
		t.Error("scaffold device group should include deviceType")
	}
}

func TestCBScaffoldFromBaseline(t *testing.T) {
	cliCtx, mux, _ := newTestPlatformContext(t)
	mux.HandleFunc("/api/compliance-benchmarks/v1/baselines", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, &compliancebenchmarks.BaselinesResponse{
			Baselines: []compliancebenchmarks.BaselineInfo{
				{ID: "bl-uuid-1", Title: "macOS Security Compliance", Description: "CIS Level 1 for macOS"},
			},
		})
	})
	mux.HandleFunc("/api/compliance-benchmarks/v1/rules", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, &compliancebenchmarks.SourcedRules{
			Sources: []compliancebenchmarks.Source{{Branch: "main"}},
			Rules: []compliancebenchmarks.RuleInfo{
				{ID: "auth_pam_sudo_smartcard", Title: "Enforce Smartcard"},
				{ID: "os_airdrop_disable", Title: "Disable AirDrop"},
				{
					ID:    "os_password_hint_remove",
					Title: "Password History",
					ODV: &compliancebenchmarks.OrganizationDefinedValue{
						Placeholder: "5",
						Value:       "3",
						Hint:        "Number of passwords to remember",
					},
				},
				{
					ID:    "os_max_retry_unlock",
					Title: "Max Retry Unlock",
					ODV: &compliancebenchmarks.OrganizationDefinedValue{
						Placeholder: "",
						Value:       "10",
					},
				},
				{
					ID:    "os_screensaver_timeout",
					Title: "Screensaver Timeout",
					ODV:   &compliancebenchmarks.OrganizationDefinedValue{},
				},
			},
		})
	})

	cmd := newCBApplyCmd(cliCtx)
	cmd.SetArgs([]string{"--scaffold-from-baseline", "bl-uuid-1"})
	raw := captureStdout(t, func() {
		if err := cmd.Execute(); err != nil {
			t.Errorf("scaffold-from-baseline: %v", err)
		}
	})

	var result benchmarkPortableInput
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal scaffold: %v\nraw: %s", err, raw)
	}
	if result.SourceBaselineID != "bl-uuid-1" {
		t.Errorf("sourceBaselineId = %q, want %q", result.SourceBaselineID, "bl-uuid-1")
	}
	if result.Title != "macOS Security Compliance" {
		t.Errorf("title = %q, want %q", result.Title, "macOS Security Compliance")
	}
	if result.Description != "CIS Level 1 for macOS" {
		t.Errorf("description = %q, want %q", result.Description, "CIS Level 1 for macOS")
	}
	if result.EnforcementMode != "MONITOR" {
		t.Errorf("enforcementMode = %q, want MONITOR", result.EnforcementMode)
	}
	if len(result.Rules) != 5 {
		t.Fatalf("rules count = %d, want 5", len(result.Rules))
	}
	// All rules enabled in scaffold
	for i, r := range result.Rules {
		if !r.Enabled {
			t.Errorf("rule[%d] (%s): scaffold should have enabled=true", i, r.ID)
		}
	}
	if result.Rules[0].ID != "auth_pam_sudo_smartcard" {
		t.Errorf("rule[0].ID = %q, want auth_pam_sudo_smartcard", result.Rules[0].ID)
	}
	// scaffold-from-baseline omits selectedOsVersions so the benchmark tracks all
	// OS versions available for the baseline (the API default).
	if result.SelectedOsVersions != nil {
		t.Errorf("selectedOsVersions should be omitted by scaffold-from-baseline, got %v", result.SelectedOsVersions)
	}
	// ODV enrichment: Placeholder wins
	if result.Rules[2].ODV == nil {
		t.Fatal("rule[2] (os_password_hint_remove): ODV should be non-nil")
		return
	}
	if result.Rules[2].ODV.Value != "5" {
		t.Errorf("rule[2].ODV.Value = %q, want placeholder %q", result.Rules[2].ODV.Value, "5")
	}
	// ODV enrichment: Value fallback
	if result.Rules[3].ODV == nil {
		t.Fatal("rule[3] (os_max_retry_unlock): ODV should be non-nil")
		return
	}
	if result.Rules[3].ODV.Value != "10" {
		t.Errorf("rule[3].ODV.Value = %q, want value fallback %q", result.Rules[3].ODV.Value, "10")
	}
	// ODV enrichment: sentinel fallback
	if result.Rules[4].ODV == nil {
		t.Fatal("rule[4] (os_screensaver_timeout): ODV should be non-nil")
		return
	}
	if result.Rules[4].ODV.Value != "<odv-value>" {
		t.Errorf("rule[4].ODV.Value = %q, want sentinel %q", result.Rules[4].ODV.Value, "<odv-value>")
	}
	// Non-ODV rules should have nil ODV
	if result.Rules[0].ODV != nil {
		t.Errorf("rule[0] (no ODV): ODV should be nil, got %v", result.Rules[0].ODV)
	}
}

func TestCBScaffoldFromBaseline_UnknownID(t *testing.T) {
	cliCtx, mux, _ := newTestPlatformContext(t)
	mux.HandleFunc("/api/compliance-benchmarks/v1/rules", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONStatus(w, http.StatusNotFound, map[string]string{"error": "not found"})
	})

	cmd := newCBApplyCmd(cliCtx)
	cmd.SetArgs([]string{"--scaffold-from-baseline", "bad-id"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for unknown baseline ID, got nil")
		return
	}
}

func TestCBExport(t *testing.T) {
	cliCtx, mux, _ := newTestPlatformContext(t)
	mux.HandleFunc("/api/compliance-benchmarks/v1/benchmarks", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, &compliancebenchmarks.BenchmarksResponseV2{
			Benchmarks: []compliancebenchmarks.BenchmarkV2{
				{ID: "bm-1", Title: "CIS Level 1"},
			},
		})
	})
	mux.HandleFunc("/api/compliance-benchmarks/v1/benchmarks/bm-1", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, &compliancebenchmarks.BenchmarkResponseV2{
			BenchmarkID:     "bm-1",
			Title:           "CIS Level 1",
			BaselineID:      "bl-cis",
			EnforcementMode: "AUDIT",
			Sources:         []compliancebenchmarks.Source{{Branch: "main"}},
			Rules:           []compliancebenchmarks.RuleInfo{{ID: "rule-1", Enabled: true}},
			Target:          &compliancebenchmarks.TargetV2{DeviceGroups: []string{"grp-123"}},
		})
	})
	mux.HandleFunc("/api/device-groups/v1/device-groups", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"results": []devicegroups.DeviceGroupListReadRepresentationV1{
				{ID: "grp-123", Name: "All Mac Clients", DeviceType: "COMPUTER", GroupType: "SMART"},
			},
		})
	})

	cmd := newCBExportCmd(cliCtx)
	cmd.SetArgs([]string{"CIS Level 1"})
	raw := captureStdout(t, func() {
		if err := cmd.Execute(); err != nil {
			t.Errorf("export: %v", err)
		}
	})

	var result benchmarkPortableInput
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal export: %v\nraw: %s", err, raw)
	}
	if result.Title != "CIS Level 1" {
		t.Errorf("title = %q, want %q", result.Title, "CIS Level 1")
	}
	if result.SourceBaselineID != "bl-cis" {
		t.Errorf("sourceBaselineId = %q, want %q", result.SourceBaselineID, "bl-cis")
	}
	// Group ID replaced with name
	if len(result.Target.DeviceGroups) != 1 {
		t.Fatalf("device groups = %d, want 1", len(result.Target.DeviceGroups))
	}
	if result.Target.DeviceGroups[0].Name != "All Mac Clients" {
		t.Errorf("group name = %q, want %q", result.Target.DeviceGroups[0].Name, "All Mac Clients")
	}
	if result.Target.DeviceGroups[0].DeviceType != "COMPUTER" {
		t.Errorf("group deviceType = %q, want COMPUTER", result.Target.DeviceGroups[0].DeviceType)
	}
	if len(result.Rules) != 1 || result.Rules[0].ID != "rule-1" {
		t.Errorf("rules not exported correctly: %v", result.Rules)
	}
}

func TestCBClone(t *testing.T) {
	cliCtx, mux, _ := newTestPlatformContext(t)
	var captured *compliancebenchmarks.BenchmarkRequestV2
	mux.HandleFunc("/api/compliance-benchmarks/v1/benchmarks", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, &compliancebenchmarks.BenchmarksResponseV2{
				Benchmarks: []compliancebenchmarks.BenchmarkV2{{ID: "bm-src", Title: "Source Benchmark"}},
			})
		case http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			captured = &compliancebenchmarks.BenchmarkRequestV2{}
			_ = json.Unmarshal(body, captured)
			writeJSONStatus(w, http.StatusAccepted, &compliancebenchmarks.BenchmarkResponseV2{BenchmarkID: "new-id"})
		}
	})
	mux.HandleFunc("/api/compliance-benchmarks/v1/benchmarks/bm-src", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, &compliancebenchmarks.BenchmarkResponseV2{
			BenchmarkID:        "bm-src",
			Title:              "Source Benchmark",
			Description:        "original desc",
			BaselineID:         "bl-1",
			EnforcementMode:    "AUDIT",
			Sources:            []compliancebenchmarks.Source{{Branch: "main"}},
			Rules:              []compliancebenchmarks.RuleInfo{{ID: "r1", Enabled: true}, {ID: "r2", Enabled: false}},
			SelectedOsVersions: []compliancebenchmarks.OsVersion{{OsType: "MAC_OS", OsVersion: 26}},
			Target:             &compliancebenchmarks.TargetV2{DeviceGroups: []string{"grp-src-id"}},
		})
	})

	cmd := newCBCloneCmd(cliCtx)
	cmd.SetArgs([]string{"Source Benchmark", "Cloned Benchmark"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("clone: %v", err)
	}

	req := captured
	if req == nil {
		t.Fatal("CreateBenchmark was not called")
		return
	}
	if req.Title != "Cloned Benchmark" {
		t.Errorf("cloned title = %q, want %q", req.Title, "Cloned Benchmark")
	}
	if req.Description == nil || *req.Description != "original desc" {
		t.Errorf("description not copied: %v", req.Description)
	}
	if req.SourceBaselineID != "bl-1" {
		t.Errorf("sourceBaselineId = %q, want bl-1", req.SourceBaselineID)
	}
	if len(req.Rules) != 2 {
		t.Fatalf("rules count = %d, want 2", len(req.Rules))
	}
	if req.Rules[0].ID != "r1" || !req.Rules[0].Enabled {
		t.Errorf("rule[0] not copied correctly: %+v", req.Rules[0])
	}
	if req.Rules[1].ID != "r2" || req.Rules[1].Enabled {
		t.Errorf("rule[1] not copied correctly: %+v", req.Rules[1])
	}
	// Selected OS versions copied from source
	if req.SelectedOsVersions == nil || len(*req.SelectedOsVersions) != 1 || (*req.SelectedOsVersions)[0].OsVersion != 26 {
		t.Errorf("selectedOsVersions not copied from source: %v", req.SelectedOsVersions)
	}
	// Target groups copied from source
	if len(req.Target.DeviceGroups) != 1 || req.Target.DeviceGroups[0] != "grp-src-id" {
		t.Errorf("target groups = %v, want [grp-src-id]", req.Target.DeviceGroups)
	}
}

func TestCBClone_WithComputerGroupOverride(t *testing.T) {
	cliCtx, mux, _ := newTestPlatformContext(t)
	var captured *compliancebenchmarks.BenchmarkRequestV2
	mux.HandleFunc("/api/compliance-benchmarks/v1/benchmarks", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, &compliancebenchmarks.BenchmarksResponseV2{
				Benchmarks: []compliancebenchmarks.BenchmarkV2{{ID: "bm-src", Title: "Source"}},
			})
		case http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			captured = &compliancebenchmarks.BenchmarkRequestV2{}
			_ = json.Unmarshal(body, captured)
			writeJSONStatus(w, http.StatusAccepted, &compliancebenchmarks.BenchmarkResponseV2{BenchmarkID: "new-id"})
		}
	})
	mux.HandleFunc("/api/compliance-benchmarks/v1/benchmarks/bm-src", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, &compliancebenchmarks.BenchmarkResponseV2{
			Title:      "Source",
			BaselineID: "bl-1",
			Target:     &compliancebenchmarks.TargetV2{DeviceGroups: []string{"old-grp-id"}},
		})
	})
	mux.HandleFunc("/api/device-groups/v1/device-groups", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"results": []devicegroups.DeviceGroupListReadRepresentationV1{
				{ID: "new-grp-id", Name: "New Group"},
			},
		})
	})

	cmd := newCBCloneCmd(cliCtx)
	cmd.SetArgs([]string{"Source", "Cloned", "--computer-group", "New Group"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("clone with override: %v", err)
	}

	req := captured
	if req == nil {
		t.Fatal("CreateBenchmark was not called")
		return
	}
	if len(req.Target.DeviceGroups) != 1 || req.Target.DeviceGroups[0] != "new-grp-id" {
		t.Errorf("target groups = %v, want [new-grp-id]", req.Target.DeviceGroups)
	}
}

func TestCBDeleteByID(t *testing.T) {
	cliCtx, mux, _ := newTestPlatformContext(t)
	mux.HandleFunc("/api/compliance-benchmarks/v1/benchmarks/bm-abc-123", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	cmd := newComplianceBenchmarksCmd(cliCtx)
	cmd.SetArgs([]string{"delete", "bm-abc-123", "--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("delete by ID: %v", err)
	}
}

func TestCBDeleteByName(t *testing.T) {
	cliCtx, mux, _ := newTestPlatformContext(t)
	mux.HandleFunc("/api/compliance-benchmarks/v1/benchmarks", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"benchmarks": []map[string]any{
				{"id": "bm-named-id", "title": "Named Benchmark"},
			},
		})
	})
	mux.HandleFunc("/api/compliance-benchmarks/v1/benchmarks/bm-named-id", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	cmd := newComplianceBenchmarksCmd(cliCtx)
	cmd.SetArgs([]string{"delete", "--name", "Named Benchmark", "--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("delete by name: %v", err)
	}
}

func TestCBDeleteNoArgs(t *testing.T) {
	cliCtx, _, _ := newTestPlatformContext(t)

	cmd := newComplianceBenchmarksCmd(cliCtx)
	cmd.SetArgs([]string{"delete", "--yes"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when no ID or --name provided")
		return
	}
}

// cbApplyHandlers wires the standard CB apply test endpoints — POST captures
// the create-benchmark request, optional GET returns the named device groups.
func cbApplyHandlers(mux *http.ServeMux, groups []devicegroups.DeviceGroupListReadRepresentationV1) **compliancebenchmarks.BenchmarkRequestV2 {
	captured := new(*compliancebenchmarks.BenchmarkRequestV2)
	mux.HandleFunc("/api/compliance-benchmarks/v1/benchmarks", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		req := &compliancebenchmarks.BenchmarkRequestV2{}
		_ = json.Unmarshal(body, req)
		*captured = req
		writeJSONStatus(w, http.StatusAccepted, &compliancebenchmarks.BenchmarkResponseV2{BenchmarkID: "new-id"})
	})
	if groups != nil {
		mux.HandleFunc("/api/device-groups/v1/device-groups", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, map[string]any{"results": groups})
		})
	}
	return captured
}

func TestCBApply_ResolvesGroupNames(t *testing.T) {
	cliCtx, mux, _ := newTestPlatformContext(t)
	captured := cbApplyHandlers(mux, []devicegroups.DeviceGroupListReadRepresentationV1{
		{ID: "grp-resolved-id", Name: "My Device Group"},
	})

	input := benchmarkPortableInput{
		Title:            "Test Benchmark",
		SourceBaselineID: "bl-1",
		EnforcementMode:  "AUDIT",
		Target: benchmarkPortableTarget{
			DeviceGroups: []benchmarkPortableGroup{
				{Name: "My Device Group", DeviceType: "COMPUTER", GroupType: "SMART"},
			},
		},
	}
	path := writeTempJSON(t, input)

	cmd := newCBApplyCmd(cliCtx)
	cmd.SetArgs([]string{"--from-file", path})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("apply: %v", err)
	}

	req := *captured
	if req == nil {
		t.Fatal("CreateBenchmark was not called")
		return
	}
	if len(req.Target.DeviceGroups) != 1 || req.Target.DeviceGroups[0] != "grp-resolved-id" {
		t.Errorf("target groups = %v, want [grp-resolved-id]", req.Target.DeviceGroups)
	}
}

func TestCBApply_ComputerGroupOverride(t *testing.T) {
	cliCtx, mux, _ := newTestPlatformContext(t)
	captured := cbApplyHandlers(mux, []devicegroups.DeviceGroupListReadRepresentationV1{
		{ID: "override-id", Name: "Override Group"},
	})

	input := benchmarkPortableInput{
		Title:            "Test",
		SourceBaselineID: "bl-1",
		EnforcementMode:  "AUDIT",
		Target: benchmarkPortableTarget{
			DeviceGroups: []benchmarkPortableGroup{
				{Name: "Original Group"},
			},
		},
	}
	path := writeTempJSON(t, input)

	cmd := newCBApplyCmd(cliCtx)
	cmd.SetArgs([]string{"--from-file", path, "--computer-group", "Override Group"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("apply with override: %v", err)
	}

	req := *captured
	if req == nil {
		t.Fatal("CreateBenchmark was not called")
		return
	}
	if len(req.Target.DeviceGroups) != 1 || req.Target.DeviceGroups[0] != "override-id" {
		t.Errorf("target groups = %v, want [override-id]", req.Target.DeviceGroups)
	}
}

func TestCBApply_SelectedOsVersions(t *testing.T) {
	cliCtx, mux, _ := newTestPlatformContext(t)
	captured := cbApplyHandlers(mux, []devicegroups.DeviceGroupListReadRepresentationV1{
		{ID: "grp-id", Name: "My Device Group"},
	})

	input := benchmarkPortableInput{
		Title:              "Pinned Benchmark",
		SourceBaselineID:   "bl-1",
		EnforcementMode:    "MONITOR",
		SelectedOsVersions: []compliancebenchmarks.OsVersion{{OsType: "MAC_OS", OsVersion: 26}, {OsType: "MAC_OS", OsVersion: 27}},
		Target: benchmarkPortableTarget{
			DeviceGroups: []benchmarkPortableGroup{{Name: "My Device Group"}},
		},
	}
	path := writeTempJSON(t, input)

	cmd := newCBApplyCmd(cliCtx)
	cmd.SetArgs([]string{"--from-file", path})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("apply: %v", err)
	}

	req := *captured
	if req == nil {
		t.Fatal("CreateBenchmark was not called")
		return
	}
	if req.SelectedOsVersions == nil {
		t.Fatal("selectedOsVersions not sent to API")
		return
	}
	if got := *req.SelectedOsVersions; len(got) != 2 || got[0].OsVersion != 26 || got[1].OsVersion != 27 {
		t.Errorf("selectedOsVersions = %v, want [{MAC_OS 26} {MAC_OS 27}]", got)
	}
}

// TestCBApply_OmitSelectedOsVersions verifies that omitting the field leaves the
// request pointer nil, so the API applies its all-available default.
func TestCBApply_OmitSelectedOsVersions(t *testing.T) {
	cliCtx, mux, _ := newTestPlatformContext(t)
	captured := cbApplyHandlers(mux, []devicegroups.DeviceGroupListReadRepresentationV1{
		{ID: "grp-id", Name: "My Device Group"},
	})

	input := benchmarkPortableInput{
		Title:            "Tracking Benchmark",
		SourceBaselineID: "bl-1",
		EnforcementMode:  "MONITOR",
		Target: benchmarkPortableTarget{
			DeviceGroups: []benchmarkPortableGroup{{Name: "My Device Group"}},
		},
	}
	path := writeTempJSON(t, input)

	cmd := newCBApplyCmd(cliCtx)
	cmd.SetArgs([]string{"--from-file", path})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("apply: %v", err)
	}

	req := *captured
	if req == nil {
		t.Fatal("CreateBenchmark was not called")
		return
	}
	if req.SelectedOsVersions != nil {
		t.Errorf("selectedOsVersions should be nil when omitted, got %v", *req.SelectedOsVersions)
	}
}

func TestCBApply_LegacyFormat(t *testing.T) {
	cliCtx, mux, _ := newTestPlatformContext(t)
	captured := cbApplyHandlers(mux, nil)

	// Legacy format: target.deviceGroups is []string (raw IDs), not []object.
	legacy := compliancebenchmarks.BenchmarkRequestV2{
		Title:            "Legacy Benchmark",
		SourceBaselineID: "bl-1",
		EnforcementMode:  "AUDIT",
		Rules:            []compliancebenchmarks.RuleRequest{{ID: "r1", Enabled: true}},
		Target:           compliancebenchmarks.TargetV2{DeviceGroups: []string{"raw-group-id-1"}},
	}
	path := writeTempJSON(t, legacy)

	cmd := newCBApplyCmd(cliCtx)
	cmd.SetArgs([]string{"--from-file", path})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("apply with legacy format: %v", err)
	}

	req := *captured
	if req == nil {
		t.Fatal("CreateBenchmark was not called")
		return
	}
	if req.Title != "Legacy Benchmark" {
		t.Errorf("title = %q, want %q", req.Title, "Legacy Benchmark")
	}
	if len(req.Target.DeviceGroups) != 1 || req.Target.DeviceGroups[0] != "raw-group-id-1" {
		t.Errorf("target groups = %v, want [raw-group-id-1]", req.Target.DeviceGroups)
	}
}

func TestCBApply_LegacyFormatWithGroupOverride(t *testing.T) {
	cliCtx, mux, _ := newTestPlatformContext(t)
	captured := cbApplyHandlers(mux, []devicegroups.DeviceGroupListReadRepresentationV1{
		{ID: "override-id", Name: "Override Group"},
	})

	legacy := compliancebenchmarks.BenchmarkRequestV2{
		Title:            "Legacy With Override",
		SourceBaselineID: "bl-1",
		EnforcementMode:  "AUDIT",
		Target:           compliancebenchmarks.TargetV2{DeviceGroups: []string{"old-id"}},
	}
	path := writeTempJSON(t, legacy)

	cmd := newCBApplyCmd(cliCtx)
	cmd.SetArgs([]string{"--from-file", path, "--computer-group", "Override Group"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("apply legacy with override: %v", err)
	}

	req := *captured
	if req == nil {
		t.Fatal("CreateBenchmark was not called")
		return
	}
	if len(req.Target.DeviceGroups) != 1 || req.Target.DeviceGroups[0] != "override-id" {
		t.Errorf("target groups = %v, want [override-id]", req.Target.DeviceGroups)
	}
}

// ── Blueprint/Profile Tests (from main) ─────────────────────────────────────

func TestExtractPayloadsFromXML(t *testing.T) {
	tests := []struct {
		name string
		xml  string
		want string
	}{
		{
			name: "standard payloads tag",
			xml:  `<os_x_configuration_profile><general><payloads><![CDATA[<?xml version="1.0"?>]]></payloads></general></os_x_configuration_profile>`,
			want: `<?xml version="1.0"?>`,
		},
		{
			name: "CDATA wrapped",
			xml:  `<os_x_configuration_profile><general><payloads><![CDATA[<?xml version="1.0" encoding="UTF-8"?><plist></plist>]]></payloads></general></os_x_configuration_profile>`,
			want: `<?xml version="1.0" encoding="UTF-8"?><plist></plist>`,
		},
		{
			name: "no payloads tag",
			xml:  `<os_x_configuration_profile><general><name>Test</name></general></os_x_configuration_profile>`,
			want: "",
		},
		{
			name: "entity-encoded payloads",
			xml:  `<payloads>&lt;?xml version=&#34;1.0&#34;?&gt;&lt;plist&gt;&lt;/plist&gt;</payloads>`,
			want: `<?xml version="1.0"?><plist></plist>`,
		},
		{
			name: "empty payloads",
			xml:  `<payloads></payloads>`,
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractPayloadsFromXML(tt.xml)
			if got != tt.want {
				t.Errorf("extractPayloadsFromXML() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClassicProfilePath_ByID(t *testing.T) {
	tests := []struct {
		profileType string
		name        string
		wantSuffix  string
	}{
		{"computer", "1", "/JSSResource/osxconfigurationprofiles/id/1"},
		{"mobile", "1", "/JSSResource/mobiledeviceconfigurationprofiles/id/1"},
		{"computer", "4242", "/JSSResource/osxconfigurationprofiles/id/4242"},
	}
	for _, tt := range tests {
		got := classicProfilePath(tt.profileType, tt.name)
		if got != tt.wantSuffix {
			t.Errorf("classicProfilePath(%q, %q) = %q, want %q", tt.profileType, tt.name, got, tt.wantSuffix)
		}
	}
}

func TestResolveBlueprintID_IDFromArgs(t *testing.T) {
	id, err := resolveBlueprintID(context.Background(), &registry.CLIContext{}, []string{"abc-123"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "abc-123" {
		t.Errorf("got %q, want abc-123", id)
	}
}

func TestResolveBlueprintID_NameFlag(t *testing.T) {
	cliCtx, mux, _ := newTestPlatformContext(t)
	mux.HandleFunc("/api/blueprints/v1/blueprints", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"results": []blueprints.BlueprintOverview{
				{ID: "bp-id-1", Name: "Test BP"},
			},
			"totalCount": 1,
		})
	})
	id, err := resolveBlueprintID(context.Background(), cliCtx, nil, "Test BP")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "bp-id-1" {
		t.Errorf("got %q, want bp-id-1", id)
	}
}

func TestResolveBlueprintID_BothErrors(t *testing.T) {
	_, err := resolveBlueprintID(context.Background(), &registry.CLIContext{}, []string{"abc"}, "name")
	if err == nil {
		t.Fatal("expected error when both args and name flag provided")
		return
	}
}

func TestResolveBlueprintID_NeitherErrors(t *testing.T) {
	_, err := resolveBlueprintID(context.Background(), &registry.CLIContext{}, nil, "")
	if err == nil {
		t.Fatal("expected error when neither args nor name flag provided")
		return
	}
}

func TestBlueprintLabel(t *testing.T) {
	if got := blueprintLabel([]string{"my-id"}, ""); got != "my-id" {
		t.Errorf("got %q, want my-id", got)
	}
	if got := blueprintLabel(nil, "My Blueprint"); got != "My Blueprint" {
		t.Errorf("got %q, want My Blueprint", got)
	}
	// Empty args and empty flag should not panic
	if got := blueprintLabel(nil, ""); got != "<unknown>" {
		t.Errorf("got %q, want <unknown>", got)
	}
}

func TestResolveComponentIdentifier(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		// Full identifier — exact match
		{"com.jamf.ddm.software-update-settings", "com.jamf.ddm.software-update-settings"},
		{"com.jamf.ddm.passcode-settings", "com.jamf.ddm.passcode-settings"},
		{"com.jamf.ddm-configuration-profile", "com.jamf.ddm-configuration-profile"},
		// Short name from ShortNames map
		{"software-update-settings", "com.jamf.ddm.software-update-settings"},
		{"sw-updates", "com.jamf.ddm.sw-updates"},
		{"ddm-configuration-profile", "com.jamf.ddm-configuration-profile"},
		// Auto-prefix for dotless input
		{"passcode-settings", "com.jamf.ddm.passcode-settings"},
		{"disk-management", "com.jamf.ddm.disk-management"},
		// Unknown — returns as-is
		{"nonexistent", "nonexistent"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := resolveComponentIdentifier(tt.input)
			if got != tt.want {
				t.Errorf("resolveComponentIdentifier(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ── HTTP mock for Classic API tests ────────────────────────────────────────

// classicHTTPMock implements registry.HTTPClient and returns canned responses.
type classicHTTPMock struct {
	statusCode int
	body       string
}

func (m *classicHTTPMock) Do(_ context.Context, _, _ string, _ io.Reader) (*http.Response, error) {
	return &http.Response{
		StatusCode: m.statusCode,
		Body:       io.NopCloser(strings.NewReader(m.body)),
		Header:     make(http.Header),
	}, nil
}

// classicRouteMock implements registry.HTTPClient with per-path canned bodies
// and records the paths it was asked for. Unrouted paths return 404.
type classicRouteMock struct {
	routes map[string]string
	paths  []string
}

func (m *classicRouteMock) Do(_ context.Context, _, path string, _ io.Reader) (*http.Response, error) {
	m.paths = append(m.paths, path)
	body, ok := m.routes[path]
	status := http.StatusOK
	if !ok {
		status, body = http.StatusNotFound, `<html>Not Found</html>`
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}, nil
}

func (m *classicRouteMock) sawPath(path string) bool {
	for _, p := range m.paths {
		if p == path {
			return true
		}
	}
	return false
}

// ── extractAndResolveScope tests ───────────────────────────────────────────

func TestExtractAndResolveScope_ComputerGroups(t *testing.T) {
	groupsResponse := `{"results":[{"groupPlatformId":"uuid-lab-macs","groupName":"Lab Macs"}]}`
	mock := &classicHTTPMock{statusCode: 200, body: groupsResponse}

	xmlBody := []byte(`<os_x_configuration_profile>
		<scope>
			<all_computers>false</all_computers>
			<computer_groups>
				<computer_group><id>1</id><name>Lab Macs</name></computer_group>
			</computer_groups>
		</scope>
	</os_x_configuration_profile>`)

	ids, warnings := extractAndResolveScope(context.Background(), mock, xmlBody)
	if len(ids) != 1 {
		t.Fatalf("expected 1 resolved ID, got %d", len(ids))
	}
	if ids[0] != "uuid-lab-macs" {
		t.Errorf("resolved ID = %q, want uuid-lab-macs", ids[0])
	}
	for _, w := range warnings {
		t.Logf("warning: %s", w)
	}
}

func TestExtractAndResolveScope_NoScopeSection(t *testing.T) {
	xmlBody := []byte(`<os_x_configuration_profile><general><name>Test</name></general></os_x_configuration_profile>`)
	ids, warnings := extractAndResolveScope(context.Background(), nil, xmlBody)
	if len(ids) != 0 {
		t.Errorf("expected no IDs, got %d", len(ids))
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "no <scope> section") {
		t.Errorf("expected 'no <scope> section' warning, got %v", warnings)
	}
}

func TestExtractAndResolveScope_MalformedScope(t *testing.T) {
	xmlBody := []byte(`<root><scope><all_computers>false</all_computers></root>`)
	ids, warnings := extractAndResolveScope(context.Background(), nil, xmlBody)
	if len(ids) != 0 {
		t.Errorf("expected no IDs, got %d", len(ids))
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "malformed") {
		t.Errorf("expected 'malformed' warning, got %v", warnings)
	}
}

func TestExtractAndResolveScope_AllComputers(t *testing.T) {
	xmlBody := []byte(`<root><scope><all_computers>true</all_computers></scope></root>`)
	ids, warnings := extractAndResolveScope(context.Background(), nil, xmlBody)
	if len(ids) != 0 {
		t.Errorf("expected no IDs for all_computers scope, got %d", len(ids))
	}
	found := false
	for _, w := range warnings {
		if strings.Contains(w, "All Computers") {
			found = true
		}
	}
	if !found {
		t.Error("expected warning about All Computers scope")
	}
}

func TestExtractAndResolveScope_DropsUnsupportedElements(t *testing.T) {
	xmlBody := []byte(`<root><scope>
		<all_computers>false</all_computers>
		<computers><computer><id>1</id><name>Mac-01</name></computer></computers>
		<buildings><building><id>1</id><name>HQ</name></building></buildings>
		<departments><department><id>1</id><name>IT</name></department></departments>
	</scope></root>`)

	ids, warnings := extractAndResolveScope(context.Background(), nil, xmlBody)
	if len(ids) != 0 {
		t.Errorf("expected no IDs, got %d", len(ids))
	}
	// Should have warnings for individual computers, buildings, departments
	if len(warnings) < 3 {
		t.Errorf("expected at least 3 warnings, got %d: %v", len(warnings), warnings)
	}
}

// ── downloadClassicProfile tests ───────────────────────────────────────────

func TestDownloadClassicProfile_Success(t *testing.T) {
	xmlResp := `<os_x_configuration_profile><general>
		<payloads><![CDATA[<?xml version="1.0"?><plist><dict></dict></plist>]]></payloads>
	</general></os_x_configuration_profile>`
	mock := &classicHTTPMock{statusCode: 200, body: xmlResp}

	cliCtx := &registry.CLIContext{Client: mock}
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	data, err := downloadClassicProfile(cmd, cliCtx, "42", "", "computer")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(data), "<plist>") {
		t.Errorf("expected plist content, got %q", string(data))
	}
}

func TestDownloadClassicProfile_NotFound(t *testing.T) {
	mock := &classicHTTPMock{statusCode: 404, body: `<html>Not Found</html>`}
	cliCtx := &registry.CLIContext{Client: mock}
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	_, err := downloadClassicProfile(cmd, cliCtx, "99", "", "computer")
	if err == nil {
		t.Fatal("expected error for 404 response")
		return
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got %q", err.Error())
	}
}

func TestDownloadClassicProfile_ByName(t *testing.T) {
	mock := &classicRouteMock{routes: map[string]string{
		"/JSSResource/osxconfigurationprofiles": `<os_x_configuration_profiles><size>2</size>
			<os_x_configuration_profile><id>7</id><name>Passcode Policy</name></os_x_configuration_profile>
			<os_x_configuration_profile><id>9</id><name>Other</name></os_x_configuration_profile>
		</os_x_configuration_profiles>`,
		"/JSSResource/osxconfigurationprofiles/id/7": `<os_x_configuration_profile><general>
			<payloads><![CDATA[<?xml version="1.0"?><plist><dict></dict></plist>]]></payloads>
		</general></os_x_configuration_profile>`,
	}}

	cliCtx := &registry.CLIContext{Client: mock}
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	data, err := downloadClassicProfile(cmd, cliCtx, "", "Passcode Policy", "computer")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(data), "<plist>") {
		t.Errorf("expected plist content, got %q", string(data))
	}
	if !mock.sawPath("/JSSResource/osxconfigurationprofiles/id/7") {
		t.Errorf("expected fetch by resolved id 7, saw %v", mock.paths)
	}
}

// ── resolveClassicProfileID tests ──────────────────────────────────────────

func TestResolveClassicProfileID_NumericArgSkipsLookup(t *testing.T) {
	mock := &classicRouteMock{}
	id, name, err := resolveClassicProfileID(context.Background(), mock, "computer", "42", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "42" || name != "" {
		t.Errorf("got id=%q name=%q, want id=42 and empty name", id, name)
	}
	if len(mock.paths) != 0 {
		t.Errorf("expected no API calls for a numeric id, saw %v", mock.paths)
	}
}

func TestResolveClassicProfileID_NonNumericArgResolvesAsName(t *testing.T) {
	mock := &classicRouteMock{routes: map[string]string{
		"/JSSResource/osxconfigurationprofiles": `<os_x_configuration_profiles><size>1</size>
			<os_x_configuration_profile><id>3</id><name>My Restrictions</name></os_x_configuration_profile>
		</os_x_configuration_profiles>`,
	}}
	id, name, err := resolveClassicProfileID(context.Background(), mock, "computer", "My Restrictions", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "3" || name != "My Restrictions" {
		t.Errorf("got id=%q name=%q, want id=3 name=My Restrictions", id, name)
	}
}

func TestResolveClassicProfileID_DuplicateNamesListIDs(t *testing.T) {
	restore := noInput
	noInput = true
	t.Cleanup(func() { noInput = restore })

	mock := &classicRouteMock{routes: map[string]string{
		"/JSSResource/osxconfigurationprofiles": `<os_x_configuration_profiles><size>2</size>
			<os_x_configuration_profile><id>11</id><name>Duplicate</name></os_x_configuration_profile>
			<os_x_configuration_profile><id>12</id><name>Duplicate</name></os_x_configuration_profile>
		</os_x_configuration_profiles>`,
	}}
	_, _, err := resolveClassicProfileID(context.Background(), mock, "computer", "", "Duplicate")
	if err == nil {
		t.Fatal("expected an error for duplicate names")
	}
	if !strings.Contains(err.Error(), "11") || !strings.Contains(err.Error(), "12") {
		t.Errorf("expected both colliding ids in error, got %q", err.Error())
	}
	// import-profile creates a blueprint; it never "updates" anything, so the
	// remedy must point at re-running with an ID, not at the generated
	// apply/update paths' generic wording.
	if strings.Contains(err.Error(), "update") {
		t.Errorf("expected no apply-path 'update' wording, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "re-run with one of these IDs") {
		t.Errorf("expected an ID-based remedy, got %q", err.Error())
	}
}

func TestResolveClassicProfileID_WrongTypeHint(t *testing.T) {
	restore := noInput
	noInput = true
	t.Cleanup(func() { noInput = restore })

	mock := &classicRouteMock{routes: map[string]string{
		"/JSSResource/osxconfigurationprofiles": `<os_x_configuration_profiles><size>0</size></os_x_configuration_profiles>`,
		"/JSSResource/mobiledeviceconfigurationprofiles": `<configuration_profiles><size>1</size>
			<configuration_profile><id>5</id><name>Managed Restrictions</name></configuration_profile>
		</configuration_profiles>`,
	}}
	_, _, err := resolveClassicProfileID(context.Background(), mock, "computer", "", "Managed Restrictions")
	if err == nil {
		t.Fatal("expected an error when the name only exists under the other type")
	}
	if !strings.Contains(err.Error(), "--type mobile") {
		t.Errorf("expected a --type mobile hint, got %q", err.Error())
	}
}

func TestResolveClassicProfileID_ArgAndNameFlagConflict(t *testing.T) {
	_, _, err := resolveClassicProfileID(context.Background(), &classicRouteMock{}, "computer", "42", "Some Profile")
	if err == nil || !strings.Contains(err.Error(), "not both") {
		t.Errorf("expected an 'either <id> or --name, not both' error, got %v", err)
	}
}

func TestResolveClassicProfileID_NoIdentifier(t *testing.T) {
	_, _, err := resolveClassicProfileID(context.Background(), &classicRouteMock{}, "computer", "", "")
	if err == nil || !strings.Contains(err.Error(), "--name") {
		t.Errorf("expected a 'provide an <id> or use --name' error, got %v", err)
	}
}

// --id is passed into resolveClassicProfileID's positional slot; a non-numeric
// value there is silently treated as a name lookup, which reports a confusing
// "not found" for what the user explicitly flagged as an ID. Validate it
// upfront instead.
func TestBlueprintsComponentsConfigProfile_NonNumericIDErrors(t *testing.T) {
	cliCtx := &registry.CLIContext{}
	cmd := newBlueprintsComponentsConfigProfileCmd(cliCtx)
	cmd.SetArgs([]string{"--id", "My Restrictions"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected an error for a non-numeric --id")
	}
	if !strings.Contains(err.Error(), "--id must be a numeric") {
		t.Errorf("expected a numeric-ID error, got %q", err.Error())
	}
}

func TestDownloadClassicProfile_NoPayloads(t *testing.T) {
	xmlResp := `<os_x_configuration_profile><general><name>Empty</name></general></os_x_configuration_profile>`
	mock := &classicHTTPMock{statusCode: 200, body: xmlResp}
	cliCtx := &registry.CLIContext{Client: mock}
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	_, err := downloadClassicProfile(cmd, cliCtx, "13", "", "computer")
	if err == nil {
		t.Fatal("expected error for missing payloads")
		return
	}
	if !strings.Contains(err.Error(), "no <payloads>") {
		t.Errorf("expected 'no <payloads>' error, got %q", err.Error())
	}
}

func TestDownloadClassicProfile_NilClient(t *testing.T) {
	cliCtx := &registry.CLIContext{Client: nil}
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	_, err := downloadClassicProfile(cmd, cliCtx, "1", "", "computer")
	if err == nil {
		t.Fatal("expected error for nil client")
		return
	}
	if !strings.Contains(err.Error(), "authentication") {
		t.Errorf("expected authentication error, got %q", err.Error())
	}
}

// ── Portable export/apply tests ───────────────────────────────────────────

func TestReverseResolveGroups(t *testing.T) {
	cliCtx, mux, _ := newTestPlatformContext(t)
	mux.HandleFunc("/api/device-groups/v1/device-groups", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"results": []devicegroups.DeviceGroupListReadRepresentationV1{
				{ID: "uuid-1", Name: "Lab Macs", DeviceType: "COMPUTER"},
				{ID: "uuid-2", Name: "Shared iPads", DeviceType: "MOBILE_DEVICE"},
			},
		})
	})

	groups := reverseResolveGroups(context.Background(), cliCtx.PlatformSDKClient, []string{"uuid-1", "uuid-2", "uuid-deleted"})

	if len(groups) != 3 {
		t.Fatalf("expected 3 groups, got %d", len(groups))
	}
	if groups[0].Name != "Lab Macs" || groups[0].DeviceType != "COMPUTER" {
		t.Errorf("group 0: got %+v", groups[0])
	}
	if groups[1].Name != "Shared iPads" || groups[1].DeviceType != "MOBILE_DEVICE" {
		t.Errorf("group 1: got %+v", groups[1])
	}
	if groups[2].ID != "uuid-deleted" || groups[2].Name != "" {
		t.Errorf("group 2 (deleted): got %+v", groups[2])
	}
}

func TestReverseResolveGroups_Empty(t *testing.T) {
	cliCtx, _, _ := newTestPlatformContext(t)
	groups := reverseResolveGroups(context.Background(), cliCtx.PlatformSDKClient, nil)
	if groups != nil {
		t.Errorf("expected nil for empty input, got %v", groups)
	}
}

func TestDeviceTypeToGroupType(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"COMPUTER", "COMPUTER"},
		{"MOBILE_DEVICE", "MOBILE"},
		{"MOBILE", "MOBILE"},
		{"", ""},
		{"UNKNOWN", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := deviceTypeToGroupType(tt.input)
			if got != tt.want {
				t.Errorf("deviceTypeToGroupType(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseBlueprintApplyInput_OldFormat(t *testing.T) {
	input := `{
		"name": "Test BP",
		"description": "desc",
		"scope": {"deviceGroups": ["uuid-1", "uuid-2"]},
		"steps": [{"name": "Step 1", "components": []}]
	}`

	req, err := parseBlueprintApplyInput(context.Background(), []byte(input), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Name != "Test BP" {
		t.Errorf("name: got %q, want Test BP", req.Name)
	}
	if len(req.Scope.DeviceGroups) != 2 {
		t.Fatalf("scope: expected 2 groups, got %d", len(req.Scope.DeviceGroups))
	}
	if req.Scope.DeviceGroups[0] != "uuid-1" {
		t.Errorf("scope[0]: got %q, want uuid-1", req.Scope.DeviceGroups[0])
	}
}

func TestParseBlueprintApplyInput_OldFormat_ScopeOverride(t *testing.T) {
	input := `{
		"name": "Test BP",
		"scope": {"deviceGroups": ["uuid-original"]},
		"steps": []
	}`

	req, err := parseBlueprintApplyInput(context.Background(), []byte(input), nil, []string{"uuid-override"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(req.Scope.DeviceGroups) != 1 || req.Scope.DeviceGroups[0] != "uuid-override" {
		t.Errorf("scope override not applied: got %v", req.Scope.DeviceGroups)
	}
}

func TestParseBlueprintApplyInput_PortableFormat(t *testing.T) {
	// Portable format with group objects — needs an HTTP client for name resolution.
	// Use a mock that returns the expected group UUID.
	groupsResp := `{"results":[{"groupPlatformId":"target-uuid-1","groupName":"Lab Macs"}]}`
	mock := &classicHTTPMock{statusCode: 200, body: groupsResp}

	input := `{
		"name": "Portable BP",
		"scope": {
			"deviceGroups": [
				{"id": "source-uuid-1", "name": "Lab Macs", "deviceType": "COMPUTER"}
			]
		},
		"steps": [{"name": "Step 1", "components": []}]
	}`

	req, err := parseBlueprintApplyInput(context.Background(), []byte(input), mock, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Name != "Portable BP" {
		t.Errorf("name: got %q", req.Name)
	}
	if len(req.Scope.DeviceGroups) != 1 {
		t.Fatalf("scope: expected 1 group, got %d", len(req.Scope.DeviceGroups))
	}
	if req.Scope.DeviceGroups[0] != "target-uuid-1" {
		t.Errorf("scope[0]: got %q, want target-uuid-1", req.Scope.DeviceGroups[0])
	}
}

func TestParseBlueprintApplyInput_PortableFormat_ScopeOverride(t *testing.T) {
	input := `{
		"name": "Portable BP",
		"scope": {
			"deviceGroups": [
				{"id": "source-uuid", "name": "Lab Macs", "deviceType": "COMPUTER"}
			]
		},
		"steps": []
	}`

	// Scope override bypasses name resolution, so no HTTP client needed for the groups in file
	req, err := parseBlueprintApplyInput(context.Background(), []byte(input), nil, []string{"override-uuid"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(req.Scope.DeviceGroups) != 1 || req.Scope.DeviceGroups[0] != "override-uuid" {
		t.Errorf("scope override not applied: got %v", req.Scope.DeviceGroups)
	}
}

func TestParseBlueprintApplyInput_PortableFormat_FallbackUUID(t *testing.T) {
	// Group with no name (deleted on source) — falls back to embedded UUID
	input := `{
		"name": "Fallback BP",
		"scope": {
			"deviceGroups": [
				{"id": "orphan-uuid", "name": "", "deviceType": ""}
			]
		},
		"steps": []
	}`

	req, err := parseBlueprintApplyInput(context.Background(), []byte(input), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(req.Scope.DeviceGroups) != 1 || req.Scope.DeviceGroups[0] != "orphan-uuid" {
		t.Errorf("expected fallback to UUID: got %v", req.Scope.DeviceGroups)
	}
}

func TestParseBlueprintApplyInput_NoName(t *testing.T) {
	input := `{"scope": {"deviceGroups": []}}`
	_, err := parseBlueprintApplyInput(context.Background(), []byte(input), nil, nil)
	if err == nil {
		t.Fatal("expected error for missing name")
		return
	}
}

func TestBlueprintExportRoundTrip(t *testing.T) {
	cliCtx, mux, _ := newTestPlatformContext(t)
	mux.HandleFunc("/api/device-groups/v1/device-groups", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"results": []devicegroups.DeviceGroupListReadRepresentationV1{
				{ID: "source-uuid", Name: "Lab Macs", DeviceType: "COMPUTER"},
			},
		})
	})

	bp := &blueprints.BlueprintDetail{
		ID:          "bp-1",
		Name:        "Round Trip BP",
		Description: strPtr("test"),
		Scope:       &blueprints.BlueprintScope{DeviceGroups: []string{"source-uuid"}},
		Steps: []blueprints.BlueprintStep{
			{Name: strPtr("Step 1"), Components: []blueprints.Component{
				{Identifier: "com.jamf.ddm.passcode-settings", Configuration: json.RawMessage(`{"RequirePasscode": true}`)},
			}},
		},
	}

	// Export
	exported := blueprintToExport(context.Background(), cliCtx.PlatformSDKClient, bp)

	// Verify export has enriched scope
	if len(exported.Scope.DeviceGroups) != 1 {
		t.Fatalf("export scope: expected 1 group, got %d", len(exported.Scope.DeviceGroups))
	}
	if exported.Scope.DeviceGroups[0].Name != "Lab Macs" {
		t.Errorf("export scope name: got %q", exported.Scope.DeviceGroups[0].Name)
	}
	if exported.Scope.DeviceGroups[0].DeviceType != "COMPUTER" {
		t.Errorf("export scope type: got %q", exported.Scope.DeviceGroups[0].DeviceType)
	}

	// Marshal to JSON (simulating writing to file)
	data, err := json.Marshal(exported)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Parse on target with a mock that resolves "Lab Macs" → "target-uuid"
	targetMock := &classicHTTPMock{
		statusCode: 200,
		body:       `{"results":[{"groupPlatformId":"target-uuid","groupName":"Lab Macs"}]}`,
	}
	req, err := parseBlueprintApplyInput(context.Background(), data, targetMock, nil)
	if err != nil {
		t.Fatalf("parse on target: %v", err)
	}

	if req.Name != "Round Trip BP" {
		t.Errorf("name: got %q", req.Name)
	}
	if len(req.Scope.DeviceGroups) != 1 || req.Scope.DeviceGroups[0] != "target-uuid" {
		t.Errorf("target scope: got %v, want [target-uuid]", req.Scope)
	}
	if len(req.Steps) != 1 || req.Steps[0].Name == nil || *req.Steps[0].Name != "Step 1" {
		t.Errorf("steps not preserved: got %+v", req.Steps)
	}
}

func TestIsPortableScopeFormat(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"portable objects", `{"scope":{"deviceGroups":[{"id":"x","name":"y"}]}}`, true},
		{"old uuid strings", `{"scope":{"deviceGroups":["uuid-1"]}}`, false},
		{"empty groups array", `{"scope":{"deviceGroups":[]}}`, false},
		{"null scope", `{"scope":null}`, false},
		{"missing scope", `{"name":"test"}`, false},
		{"empty scope object", `{"scope":{}}`, false},
		{"null deviceGroups", `{"scope":{"deviceGroups":null}}`, false},
		{"invalid json", `not json`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isPortableScopeFormat([]byte(tt.input))
			if got != tt.want {
				t.Errorf("isPortableScopeFormat(%s) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestDDMReportsDeclarationRouting(t *testing.T) {
	cliCtx, mux, _ := newTestPlatformContext(t)
	called := false
	mux.HandleFunc("/api/ddm/report/v1/declarations/com.example.decl", func(w http.ResponseWriter, _ *http.Request) {
		called = true
		writeJSON(w, map[string]any{"results": []any{}})
	})

	cmd := newDDMReportsCmd(cliCtx)
	cmd.SetArgs([]string{"declaration", "get", "com.example.decl"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("ddm-reports declaration get: %v", err)
	}
	if !called {
		t.Error("declaration report endpoint was not called")
	}
}

func TestDDMReportsDeviceRouting(t *testing.T) {
	cliCtx, mux, _ := newTestPlatformContext(t)
	called := false
	mux.HandleFunc("/api/ddm/report/v1/devices/device-uuid-123", func(w http.ResponseWriter, _ *http.Request) {
		called = true
		writeJSON(w, map[string]any{"channels": []any{}})
	})

	cmd := newDDMReportsCmd(cliCtx)
	cmd.SetArgs([]string{"device", "get", "device-uuid-123"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("ddm-reports device get: %v", err)
	}
	if !called {
		t.Error("device report endpoint was not called")
	}
}

func TestReverseResolveGroups_ListError(t *testing.T) {
	// Empty list response simulates the degraded path: all groups become
	// UUID-only since the lookup table has nothing to resolve them to.
	cliCtx, mux, _ := newTestPlatformContext(t)
	mux.HandleFunc("/api/device-groups/v1/device-groups", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"results": []any{}})
	})
	groups := reverseResolveGroups(context.Background(), cliCtx.PlatformSDKClient, []string{"uuid-1"})

	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if groups[0].ID != "uuid-1" || groups[0].Name != "" {
		t.Errorf("expected UUID-only fallback, got %+v", groups[0])
	}
}

// TestResolveScope covers all three levels an API integration can be created at.
// They are mutually exclusive — the credential carries the choice, and the
// gateway refuses the other level's header with 403 OWNERSHIP_FORBIDDEN — so
// resolution has to land on exactly one, and organization scope has to resolve
// to no header rather than to a missing value.
func TestResolveScope(t *testing.T) {
	cases := []struct {
		name       string
		env        map[string]string
		profile    config.Profile
		wantKind   auth.ScopeKind
		wantID     string
		wantHeader string
	}{
		{
			name:       "environment from the profile",
			profile:    config.Profile{AuthMethod: "platform", EnvironmentID: "env-1"},
			wantKind:   auth.ScopeEnvironment,
			wantID:     "env-1",
			wantHeader: "X-Environment-Id",
		},
		{
			name:       "tenant from the profile",
			profile:    config.Profile{AuthMethod: "platform", TenantID: "ten-1"},
			wantKind:   auth.ScopeTenant,
			wantID:     "ten-1",
			wantHeader: "X-Tenant-Id",
		},
		{
			name:     "organization when the profile names neither",
			profile:  config.Profile{AuthMethod: "platform"},
			wantKind: auth.ScopeOrganization,
		},
		{
			name:       "JAMF_ENVIRONMENT_ID overrides the profile",
			env:        map[string]string{"JAMF_ENVIRONMENT_ID": "env-env"},
			profile:    config.Profile{AuthMethod: "platform", TenantID: "ten-1"},
			wantKind:   auth.ScopeEnvironment,
			wantID:     "env-env",
			wantHeader: "X-Environment-Id",
		},
		{
			name:       "JAMF_TENANT_ID overrides the profile",
			env:        map[string]string{"JAMF_TENANT_ID": "ten-env"},
			profile:    config.Profile{AuthMethod: "platform", EnvironmentID: "env-1"},
			wantKind:   auth.ScopeTenant,
			wantID:     "ten-env",
			wantHeader: "X-Tenant-Id",
		},
		{
			name:       "environment wins over tenant in one profile",
			profile:    config.Profile{AuthMethod: "platform", EnvironmentID: "env-1", TenantID: "ten-1"},
			wantKind:   auth.ScopeEnvironment,
			wantID:     "env-1",
			wantHeader: "X-Environment-Id",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("JAMF_ENVIRONMENT_ID", tc.env["JAMF_ENVIRONMENT_ID"])
			t.Setenv("JAMF_TENANT_ID", tc.env["JAMF_TENANT_ID"])
			cfg := &config.Config{
				DefaultProfile: "p",
				Profiles:       map[string]config.Profile{"p": tc.profile},
			}

			got := resolveScope(cfg, "p")
			if got.Kind != tc.wantKind || got.ID != tc.wantID {
				t.Errorf("resolveScope = {%v %q}, want {%v %q}", got.Kind, got.ID, tc.wantKind, tc.wantID)
			}
			name, value := got.Header()
			if name != tc.wantHeader || value != tc.wantID {
				t.Errorf("Header() = (%q, %q), want (%q, %q)", name, value, tc.wantHeader, tc.wantID)
			}
		})
	}
}

// TestCheckScopeConflict pins the one combination that has to be refused rather
// than resolved: a profile naming both levels. Precedence would hide it, and the
// symptom is a 403 from whichever half the credential does not match.
func TestCheckScopeConflict(t *testing.T) {
	both := &config.Config{
		DefaultProfile: "p",
		Profiles: map[string]config.Profile{
			"p": {AuthMethod: "platform", EnvironmentID: "env-1", TenantID: "ten-1"},
		},
	}
	err := checkScopeConflict(both, "p")
	if err == nil {
		t.Fatal("a profile naming both levels must be refused")
	}
	for _, want := range []string{"env-1", "ten-1", "one level"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q: %v", want, err)
		}
	}

	for _, p := range []config.Profile{
		{AuthMethod: "platform", EnvironmentID: "env-1"},
		{AuthMethod: "platform", TenantID: "ten-1"},
		{AuthMethod: "platform"},
	} {
		cfg := &config.Config{DefaultProfile: "p", Profiles: map[string]config.Profile{"p": p}}
		if err := checkScopeConflict(cfg, "p"); err != nil {
			t.Errorf("unexpected conflict for %+v: %v", p, err)
		}
	}
}
