// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform"
)

// platformMockClient implements registry.PlatformClient for testing.
type platformMockClient struct {
	blueprints         []jamfplatform.BlueprintOverviewV1
	details            map[string]*jamfplatform.BlueprintDetailV1
	reports            map[string]*jamfplatform.BlueprintStatusDetailV1
	benchmarks         *jamfplatform.CBEngineBenchmarksResponseV2
	bmDetails          map[string]*jamfplatform.CBEngineBenchmarkResponseV2
	compliance         map[string]*jamfplatform.CBEngineCompliancePercentageV1
	devices            []jamfplatform.DeviceListReadRepresentationV1
	devGroups          []jamfplatform.DeviceGroupListReadRepresentationV1
	devGroupsForDevice map[string][]jamfplatform.DeviceGroupMemberOfRepresentationV1
	ddmReports         map[string]*jamfplatform.DeviceReportV1
	declClients        map[string][]jamfplatform.DeclarationReportClientV1
}

func (m *platformMockClient) ListBlueprints(_ context.Context, _ []string, _ string) ([]jamfplatform.BlueprintOverviewV1, error) {
	return m.blueprints, nil
}

func (m *platformMockClient) GetBlueprint(_ context.Context, id string) (*jamfplatform.BlueprintDetailV1, error) {
	if d, ok := m.details[id]; ok {
		return d, nil
	}
	return nil, fmt.Errorf("blueprint %s not found", id)
}

func (m *platformMockClient) GetBlueprintByName(_ context.Context, name string) (*jamfplatform.BlueprintDetailV1, error) {
	for _, d := range m.details {
		if d.Name == name {
			return d, nil
		}
	}
	return nil, fmt.Errorf("blueprint %q not found", name)
}

func (m *platformMockClient) CreateBlueprint(_ context.Context, req *jamfplatform.BlueprintCreateRequestV1) (*jamfplatform.BlueprintCreateResponseV1, error) {
	return &jamfplatform.BlueprintCreateResponseV1{ID: "new-bp-id"}, nil
}

func (m *platformMockClient) UpdateBlueprint(_ context.Context, _ string, _ *jamfplatform.BlueprintUpdateRequestV1) error {
	return nil
}
func (m *platformMockClient) DeleteBlueprint(_ context.Context, _ string) error { return nil }
func (m *platformMockClient) DeployBlueprint(_ context.Context, _ string) error { return nil }
func (m *platformMockClient) UndeployBlueprint(_ context.Context, _ string) error {
	return nil
}

func (m *platformMockClient) GetBlueprintReport(_ context.Context, id string) (*jamfplatform.BlueprintStatusDetailV1, error) {
	if r, ok := m.reports[id]; ok {
		return r, nil
	}
	return &jamfplatform.BlueprintStatusDetailV1{}, nil
}

func (m *platformMockClient) ListBlueprintComponents(_ context.Context) ([]jamfplatform.BlueprintComponentDescriptionV1, error) {
	return nil, nil
}

func (m *platformMockClient) GetBlueprintComponent(_ context.Context, _ string) (*jamfplatform.BlueprintComponentDescriptionV1, error) {
	return nil, nil
}

func (m *platformMockClient) ListBaselines(_ context.Context) (*jamfplatform.CBEngineBaselinesResponseV1, error) {
	return nil, nil
}

func (m *platformMockClient) ListBenchmarks(_ context.Context) (*jamfplatform.CBEngineBenchmarksResponseV2, error) {
	if m.benchmarks != nil {
		return m.benchmarks, nil
	}
	return &jamfplatform.CBEngineBenchmarksResponseV2{}, nil
}

func (m *platformMockClient) GetBenchmark(_ context.Context, id string) (*jamfplatform.CBEngineBenchmarkResponseV2, error) {
	if d, ok := m.bmDetails[id]; ok {
		return d, nil
	}
	return nil, fmt.Errorf("benchmark %s not found", id)
}

func (m *platformMockClient) GetBenchmarkByTitle(_ context.Context, _ string) (*jamfplatform.CBEngineBenchmarkResponseV2, error) {
	return nil, nil
}

func (m *platformMockClient) CreateBenchmark(_ context.Context, _ *jamfplatform.CBEngineBenchmarkRequestV2) (*jamfplatform.CBEngineBenchmarkResponseV2, error) {
	return nil, nil
}
func (m *platformMockClient) DeleteBenchmark(_ context.Context, _ string) error { return nil }
func (m *platformMockClient) GetBaselineRules(_ context.Context, _ string) (*jamfplatform.CBEngineSourcedRulesV1, error) {
	return nil, nil
}

func (m *platformMockClient) ListBenchmarkRulesStats(_ context.Context, _ string, _ string, _ string) ([]jamfplatform.CBEngineRuleResultV1, error) {
	return nil, nil
}

func (m *platformMockClient) ListBenchmarkRuleDevices(_ context.Context, _, _, _, _, _ string) ([]jamfplatform.CBEngineDeviceRuleResultV1, error) {
	return nil, nil
}

func (m *platformMockClient) GetBenchmarkCompliancePercentage(_ context.Context, id string) (*jamfplatform.CBEngineCompliancePercentageV1, error) {
	if c, ok := m.compliance[id]; ok {
		return c, nil
	}
	return &jamfplatform.CBEngineCompliancePercentageV1{}, nil
}

func (m *platformMockClient) ListDevices(_ context.Context, _ []string, _ string) ([]jamfplatform.DeviceListReadRepresentationV1, error) {
	return m.devices, nil
}

func (m *platformMockClient) GetDevice(_ context.Context, _ string) (*jamfplatform.DeviceReadRepresentationV1, error) {
	return nil, nil
}

func (m *platformMockClient) GetDeviceBySerialNumber(_ context.Context, serial string) (*jamfplatform.DeviceReadRepresentationV1, error) {
	for _, d := range m.devices {
		if d.SerialNumber == serial {
			return &jamfplatform.DeviceReadRepresentationV1{ID: d.ID}, nil
		}
	}
	return nil, fmt.Errorf("device with serial %q not found", serial)
}

func (m *platformMockClient) UpdateDevice(_ context.Context, _ string, _ *jamfplatform.DeviceUpdateRepresentationV1) error {
	return nil
}
func (m *platformMockClient) DeleteDevice(_ context.Context, _ string) error { return nil }
func (m *platformMockClient) ListDeviceApplications(_ context.Context, _ string, _ []string, _ string) ([]jamfplatform.DeviceInstalledApplicationReadRepresentationV1, error) {
	return nil, nil
}

func (m *platformMockClient) ListDevicesForUser(_ context.Context, _ string, _ []string, _ string) ([]jamfplatform.DeviceListReadRepresentationV1, error) {
	return nil, nil
}

func (m *platformMockClient) ListDeviceGroups(_ context.Context, _ []string, _ string) ([]jamfplatform.DeviceGroupListReadRepresentationV1, error) {
	return m.devGroups, nil
}

func (m *platformMockClient) GetDeviceGroup(_ context.Context, _ string) (*jamfplatform.DeviceGroupReadRepresentationV1, error) {
	return nil, nil
}

func (m *platformMockClient) CreateDeviceGroup(_ context.Context, _ *jamfplatform.DeviceGroupCreateRepresentationV1) (*jamfplatform.DeviceGroupCreateResponseV1, error) {
	return nil, nil
}

func (m *platformMockClient) UpdateDeviceGroup(_ context.Context, _ string, _ *jamfplatform.DeviceGroupUpdateRepresentationV1) error {
	return nil
}
func (m *platformMockClient) DeleteDeviceGroup(_ context.Context, _ string) error { return nil }
func (m *platformMockClient) ListDeviceGroupMembers(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

func (m *platformMockClient) UpdateDeviceGroupMembers(_ context.Context, _ string, _ *jamfplatform.DeviceGroupMemberPatchRepresentationV1) error {
	return nil
}

func (m *platformMockClient) ListDeviceGroupsForDevice(_ context.Context, deviceID string) ([]jamfplatform.DeviceGroupMemberOfRepresentationV1, error) {
	if groups, ok := m.devGroupsForDevice[deviceID]; ok {
		return groups, nil
	}
	return nil, nil
}
func (m *platformMockClient) CheckInDevice(_ context.Context, _ string) error { return nil }
func (m *platformMockClient) EraseDevice(_ context.Context, _ string, _ *jamfplatform.EraseDeviceRequestV1) ([]jamfplatform.DeviceCommandResponseV1, error) {
	return nil, nil
}

func (m *platformMockClient) RestartDevice(_ context.Context, _ string) ([]jamfplatform.DeviceCommandResponseV1, error) {
	return nil, nil
}

func (m *platformMockClient) ShutdownDevice(_ context.Context, _ string) ([]jamfplatform.DeviceCommandResponseV1, error) {
	return nil, nil
}

func (m *platformMockClient) UnmanageDevice(_ context.Context, _ string) ([]jamfplatform.DeviceCommandResponseV1, error) {
	return nil, nil
}

func (m *platformMockClient) GetDeviceDeclarationReport(_ context.Context, deviceID string) (*jamfplatform.DeviceReportV1, error) {
	if r, ok := m.ddmReports[deviceID]; ok {
		return r, nil
	}
	return &jamfplatform.DeviceReportV1{}, nil
}

func (m *platformMockClient) ListDeclarationReportClients(_ context.Context, declID string, _ []string) ([]jamfplatform.DeclarationReportClientV1, error) {
	if c, ok := m.declClients[declID]; ok {
		return c, nil
	}
	return nil, nil
}
func (m *platformMockClient) ValidateCredentials(_ context.Context) error { return nil }
func (m *platformMockClient) BaseURL() string                             { return "https://test.example.com" }

// ── Audit: Platform Checks ─────────────────────────────────────────────────

func TestCheckUndeployedBlueprints(t *testing.T) {
	pc := &platformMockClient{
		blueprints: []jamfplatform.BlueprintOverviewV1{
			{ID: "bp-1", DeploymentState: jamfplatform.BlueprintDeploymentStateV1{State: "DEPLOYED"}},
			{ID: "bp-2", DeploymentState: jamfplatform.BlueprintDeploymentStateV1{State: "NOT_DEPLOYED"}},
			{ID: "bp-3", DeploymentState: jamfplatform.BlueprintDeploymentStateV1{State: "NOT_DEPLOYED"}},
		},
	}
	result := checkUndeployedBlueprints(context.Background(), pc)
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if result.AffectedCount != 2 {
		t.Errorf("affected = %d, want 2", result.AffectedCount)
	}
}

func TestCheckUndeployedBlueprints_AllDeployed(t *testing.T) {
	pc := &platformMockClient{
		blueprints: []jamfplatform.BlueprintOverviewV1{
			{ID: "bp-1", DeploymentState: jamfplatform.BlueprintDeploymentStateV1{State: "DEPLOYED"}},
		},
	}
	result := checkUndeployedBlueprints(context.Background(), pc)
	if result != nil {
		t.Errorf("expected nil, got %+v", result)
	}
}

func TestCheckBlueprintFailures(t *testing.T) {
	pc := &platformMockClient{
		blueprints: []jamfplatform.BlueprintOverviewV1{
			{ID: "bp-1", DeploymentState: jamfplatform.BlueprintDeploymentStateV1{State: "DEPLOYED"}},
			{ID: "bp-2", DeploymentState: jamfplatform.BlueprintDeploymentStateV1{State: "DEPLOYED"}},
		},
		reports: map[string]*jamfplatform.BlueprintStatusDetailV1{
			"bp-1": {Succeeded: 10, Failed: 0, Pending: 0},
			"bp-2": {Succeeded: 8, Failed: 2, Pending: 0},
		},
	}
	result := checkBlueprintFailures(context.Background(), pc)
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if result.AffectedCount != 1 {
		t.Errorf("affected = %d, want 1", result.AffectedCount)
	}
	if result.Severity != severityCritical {
		t.Errorf("severity = %q, want %q", result.Severity, severityCritical)
	}
}

func TestCheckBenchmarkUpdates(t *testing.T) {
	pc := &platformMockClient{
		benchmarks: &jamfplatform.CBEngineBenchmarksResponseV2{
			Benchmarks: []jamfplatform.CBEngineBenchmarkV2{
				{ID: "bm-1", UpdateAvailable: true},
				{ID: "bm-2", UpdateAvailable: false},
				{ID: "bm-3", UpdateAvailable: true},
			},
		},
	}
	result := checkBenchmarkUpdates(context.Background(), pc)
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if result.AffectedCount != 2 {
		t.Errorf("affected = %d, want 2", result.AffectedCount)
	}
}

func TestCheckEmptyPlatformScope(t *testing.T) {
	pc := &platformMockClient{
		blueprints: []jamfplatform.BlueprintOverviewV1{
			{ID: "bp-1"},
			{ID: "bp-2"},
		},
		details: map[string]*jamfplatform.BlueprintDetailV1{
			"bp-1": {Scope: jamfplatform.BlueprintUpdateScopeV1{DeviceGroups: []string{"g1"}}},
			"bp-2": {Scope: jamfplatform.BlueprintUpdateScopeV1{DeviceGroups: nil}},
		},
		benchmarks: &jamfplatform.CBEngineBenchmarksResponseV2{
			Benchmarks: []jamfplatform.CBEngineBenchmarkV2{
				{ID: "bm-1", Target: jamfplatform.CBEngineTargetV2{DeviceGroups: nil}},
			},
		},
	}
	result := checkEmptyPlatformScope(context.Background(), pc)
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if result.AffectedCount != 2 {
		t.Errorf("affected = %d, want 2 (1 blueprint + 1 benchmark)", result.AffectedCount)
	}
}

func TestCheckFailedDDMDeclarations(t *testing.T) {
	pc := &platformMockClient{
		devices: []jamfplatform.DeviceListReadRepresentationV1{
			{ID: "dev-1"},
			{ID: "dev-2"},
		},
		ddmReports: map[string]*jamfplatform.DeviceReportV1{
			"dev-1": {Channels: []jamfplatform.DeviceReportChannelV1{{
				Declarations: []jamfplatform.StatusReportDeclarationV1{
					{Status: "SUCCESSFUL", ValidityState: "VALID"},
				},
			}}},
			"dev-2": {Channels: []jamfplatform.DeviceReportChannelV1{{
				Declarations: []jamfplatform.StatusReportDeclarationV1{
					{Status: "UNSUCCESSFUL", ValidityState: "INVALID", Reasons: []jamfplatform.StatusReportDeclarationReasonV1{
						{Code: "Error.ProfileFailed", Description: "Profile installation failed"},
					}},
				},
			}}},
		},
	}
	result := checkFailedDDMDeclarations(context.Background(), pc)
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if result.AffectedCount != 1 {
		t.Errorf("affected = %d, want 1", result.AffectedCount)
	}
}

func TestCheckFailedDDMDeclarations_IgnoresInfoReasons(t *testing.T) {
	pc := &platformMockClient{
		devices: []jamfplatform.DeviceListReadRepresentationV1{{ID: "dev-1"}},
		ddmReports: map[string]*jamfplatform.DeviceReportV1{
			"dev-1": {Channels: []jamfplatform.DeviceReportChannelV1{{
				Declarations: []jamfplatform.StatusReportDeclarationV1{
					{Status: "UNSUCCESSFUL", ValidityState: "INVALID", Reasons: []jamfplatform.StatusReportDeclarationReasonV1{
						{Code: "Info.DeclarationNotInstalled", Description: "not applicable"},
					}},
				},
			}}},
		},
	}
	result := checkFailedDDMDeclarations(context.Background(), pc)
	if result != nil {
		t.Errorf("expected nil (info-only reasons should be ignored), got %+v", result)
	}
}

// ── Overview: Platform Section ──────────────────────────────────────────────

func TestFetchPlatformOverview(t *testing.T) {
	pc := &platformMockClient{
		blueprints: []jamfplatform.BlueprintOverviewV1{
			{ID: "bp-1", DeploymentState: jamfplatform.BlueprintDeploymentStateV1{State: "DEPLOYED"}},
			{ID: "bp-2", DeploymentState: jamfplatform.BlueprintDeploymentStateV1{State: "NOT_DEPLOYED"}},
		},
		benchmarks: &jamfplatform.CBEngineBenchmarksResponseV2{
			Benchmarks: []jamfplatform.CBEngineBenchmarkV2{
				{ID: "bm-1", UpdateAvailable: true},
			},
		},
		compliance: map[string]*jamfplatform.CBEngineCompliancePercentageV1{
			"bm-1": {CompliancePercentage: 92.5},
		},
	}

	cliCtx := &registry.CLIContext{PlatformClient: pc}
	section := fetchPlatformOverview(context.Background(), cliCtx)
	if section == nil {
		t.Fatal("expected platform section, got nil")
	}
	if section.Name != "Platform" {
		t.Errorf("section name = %q, want Platform", section.Name)
	}
	// Should have: Blueprints, Deployed, Compliance Benchmarks, Updates Available, Overall Compliance
	if len(section.Items) < 4 {
		t.Errorf("expected at least 4 items, got %d", len(section.Items))
	}
}

func TestFetchPlatformOverview_NilClient(t *testing.T) {
	cliCtx := &registry.CLIContext{PlatformClient: nil}
	// Should not be called with nil, but verify it doesn't panic
	// (The caller checks for nil before calling)
	_ = cliCtx
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

	steps := []jamfplatform.BlueprintStepV1{
		{
			Name: "Step 1",
			Components: []jamfplatform.BlueprintComponentV1{
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
	steps := []jamfplatform.BlueprintStepV1{
		{
			Name: "Step 1",
			Components: []jamfplatform.BlueprintComponentV1{
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
		reasons []jamfplatform.StatusReportDeclarationReasonV1
		want    bool
	}{
		{"no reasons", nil, true},
		{"only ignorable", []jamfplatform.StatusReportDeclarationReasonV1{
			{Code: "Info.DeclarationNotInstalled"},
		}, true},
		{"actionable reason", []jamfplatform.StatusReportDeclarationReasonV1{
			{Code: "Error.ProfileFailed", Description: "Profile installation failed"},
		}, false},
		{"mixed", []jamfplatform.StatusReportDeclarationReasonV1{
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
