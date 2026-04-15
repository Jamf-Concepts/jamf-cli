// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"

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
	baselines          *jamfplatform.CBEngineBaselinesResponseV1
	baselineRules      map[string]*jamfplatform.CBEngineSourcedRulesV1
	createdBenchmark   *jamfplatform.CBEngineBenchmarkRequestV2
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
	if m.baselines != nil {
		return m.baselines, nil
	}
	return &jamfplatform.CBEngineBaselinesResponseV1{}, nil
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

func (m *platformMockClient) GetBenchmarkByTitle(_ context.Context, title string) (*jamfplatform.CBEngineBenchmarkResponseV2, error) {
	// Search bmDetails directly by title
	for _, d := range m.bmDetails {
		if d.Title == title {
			return d, nil
		}
	}
	// Fall through to benchmarks list → bmDetails lookup
	if m.benchmarks != nil {
		for _, bm := range m.benchmarks.Benchmarks {
			if bm.Title == title {
				if d, ok := m.bmDetails[bm.ID]; ok {
					return d, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("benchmark %q not found", title)
}

func (m *platformMockClient) CreateBenchmark(_ context.Context, req *jamfplatform.CBEngineBenchmarkRequestV2) (*jamfplatform.CBEngineBenchmarkResponseV2, error) {
	m.createdBenchmark = req
	return &jamfplatform.CBEngineBenchmarkResponseV2{
		BenchmarkID:     "new-bm-id",
		Title:           req.Title,
		Description:     req.Description,
		BaselineID:      req.SourceBaselineID,
		Sources:         req.Sources,
		Target:          req.Target,
		EnforcementMode: req.EnforcementMode,
	}, nil
}

func (m *platformMockClient) DeleteBenchmark(_ context.Context, _ string) error { return nil }

func (m *platformMockClient) GetBaselineRules(_ context.Context, baselineID string) (*jamfplatform.CBEngineSourcedRulesV1, error) {
	if m.baselineRules != nil {
		if r, ok := m.baselineRules[baselineID]; ok {
			return r, nil
		}
	}
	return nil, fmt.Errorf("baseline %q not found", baselineID)
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
	result := checkUndeployedBlueprints(pc.blueprints)
	if result == nil {
		t.Fatal("expected result, got nil")
		return
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
	result := checkUndeployedBlueprints(pc.blueprints)
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
	result := checkBlueprintFailures(context.Background(), pc, pc.blueprints)
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
	pc := &platformMockClient{
		benchmarks: &jamfplatform.CBEngineBenchmarksResponseV2{
			Benchmarks: []jamfplatform.CBEngineBenchmarkV2{
				{ID: "bm-1", UpdateAvailable: true},
				{ID: "bm-2", UpdateAvailable: false},
				{ID: "bm-3", UpdateAvailable: true},
			},
		},
	}
	result := checkBenchmarkUpdates(pc.benchmarks)
	if result == nil {
		t.Fatal("expected result, got nil")
		return
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
	result := checkEmptyPlatformScope(context.Background(), pc, pc.blueprints, pc.benchmarks)
	if result == nil {
		t.Fatal("expected result, got nil")
		return
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
		return
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
		return
	}
	if section.Name != "Platform" {
		t.Errorf("section name = %q, want Platform", section.Name)
	}
	// Should have: Blueprints, Deployed, Compliance Benchmarks, Updates Available, Overall Compliance
	if len(section.Items) < 4 {
		t.Errorf("expected at least 4 items, got %d", len(section.Items))
	}
}

func TestFetchPlatformOverview_UsesAllMockData(t *testing.T) {
	// Verify that the overview section contains expected items when data is
	// available for all Platform API calls. The caller (overview RunE) guards
	// against nil PlatformClient so we don't test that path here.
	pc := &platformMockClient{
		blueprints: []jamfplatform.BlueprintOverviewV1{
			{ID: "bp-1", Name: "Test"},
		},
		benchmarks: &jamfplatform.CBEngineBenchmarksResponseV2{
			Benchmarks: []jamfplatform.CBEngineBenchmarkV2{
				{ID: "bm-1", Title: "CIS Benchmark"},
			},
		},
		compliance: map[string]*jamfplatform.CBEngineCompliancePercentageV1{
			"bm-1": {CompliancePercentage: 95.0},
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
	bm := &jamfplatform.CBEngineBenchmarkResponseV2{
		Title:           "My Benchmark",
		Description:     "desc",
		BaselineID:      "bl-1",
		EnforcementMode: "AUDIT",
		Sources:         []jamfplatform.CBEngineSourceV1{{Branch: "main"}},
		Rules: []jamfplatform.CBEngineRuleInfoV1{
			{ID: "rule-1", Enabled: true},
			{ID: "rule-2", Enabled: false},
		},
		Target: jamfplatform.CBEngineTargetV2{DeviceGroups: []string{"grp-id-1", "grp-id-unknown"}},
	}
	groupByID := map[string]jamfplatform.DeviceGroupListReadRepresentationV1{
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
}

func TestBenchmarkToPortable_PreservesODV(t *testing.T) {
	odvVal := "90"
	bm := &jamfplatform.CBEngineBenchmarkResponseV2{
		Rules: []jamfplatform.CBEngineRuleInfoV1{
			{ID: "rule-odv", Enabled: true, ODV: &jamfplatform.CBEngineOrganizationDefinedValueV1{Value: odvVal}},
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
	cliCtx := &registry.CLIContext{
		PlatformClient: &platformMockClient{},
		Output:         &captureOutput{},
	}

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
	pc := &platformMockClient{
		baselines: &jamfplatform.CBEngineBaselinesResponseV1{
			Baselines: []jamfplatform.CBEngineBaselineInfoV1{
				{ID: "bl-uuid-1", Title: "macOS Security Compliance", Description: "CIS Level 1 for macOS"},
			},
		},
		baselineRules: map[string]*jamfplatform.CBEngineSourcedRulesV1{
			"bl-uuid-1": {
				Sources: []jamfplatform.CBEngineSourceV1{{Branch: "main"}},
				Rules: []jamfplatform.CBEngineRuleInfoV1{
					{ID: "auth_pam_sudo_smartcard", Title: "Enforce Smartcard"},
					{ID: "os_airdrop_disable", Title: "Disable AirDrop"},
					// ODV rule: Placeholder takes precedence
					{
						ID:    "os_password_hint_remove",
						Title: "Password History",
						ODV: &jamfplatform.CBEngineOrganizationDefinedValueV1{
							Placeholder: "5",
							Value:       "3",
							Hint:        "Number of passwords to remember",
						},
					},
					// ODV rule: no Placeholder, falls back to Value
					{
						ID:    "os_max_retry_unlock",
						Title: "Max Retry Unlock",
						ODV: &jamfplatform.CBEngineOrganizationDefinedValueV1{
							Placeholder: "",
							Value:       "10",
						},
					},
					// ODV rule: no Placeholder, no Value, falls back to sentinel
					{
						ID:    "os_screensaver_timeout",
						Title: "Screensaver Timeout",
						ODV:   &jamfplatform.CBEngineOrganizationDefinedValueV1{},
					},
				},
			},
		},
	}
	cliCtx := &registry.CLIContext{PlatformClient: pc, Output: &captureOutput{}}

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
	if len(result.Sources) == 0 || result.Sources[0].Branch != "main" {
		t.Errorf("sources not injected from baseline: %v", result.Sources)
	}
	// ODV enrichment: Placeholder wins
	if result.Rules[2].ODV == nil {
		t.Fatal("rule[2] (os_password_hint_remove): ODV should be non-nil")
	}
	if result.Rules[2].ODV.Value != "5" {
		t.Errorf("rule[2].ODV.Value = %q, want placeholder %q", result.Rules[2].ODV.Value, "5")
	}
	// ODV enrichment: Value fallback
	if result.Rules[3].ODV == nil {
		t.Fatal("rule[3] (os_max_retry_unlock): ODV should be non-nil")
	}
	if result.Rules[3].ODV.Value != "10" {
		t.Errorf("rule[3].ODV.Value = %q, want value fallback %q", result.Rules[3].ODV.Value, "10")
	}
	// ODV enrichment: sentinel fallback
	if result.Rules[4].ODV == nil {
		t.Fatal("rule[4] (os_screensaver_timeout): ODV should be non-nil")
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
	pc := &platformMockClient{
		baselineRules: map[string]*jamfplatform.CBEngineSourcedRulesV1{},
	}
	cliCtx := &registry.CLIContext{PlatformClient: pc, Output: &captureOutput{}}

	cmd := newCBApplyCmd(cliCtx)
	cmd.SetArgs([]string{"--scaffold-from-baseline", "bad-id"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for unknown baseline ID, got nil")
	}
}

func TestCBExport(t *testing.T) {
	pc := &platformMockClient{
		bmDetails: map[string]*jamfplatform.CBEngineBenchmarkResponseV2{
			"bm-1": {
				BenchmarkID:     "bm-1",
				Title:           "CIS Level 1",
				BaselineID:      "bl-cis",
				EnforcementMode: "AUDIT",
				Sources:         []jamfplatform.CBEngineSourceV1{{Branch: "main"}},
				Rules:           []jamfplatform.CBEngineRuleInfoV1{{ID: "rule-1", Enabled: true}},
				Target:          jamfplatform.CBEngineTargetV2{DeviceGroups: []string{"grp-123"}},
			},
		},
		devGroups: []jamfplatform.DeviceGroupListReadRepresentationV1{
			{ID: "grp-123", Name: "All Mac Clients", DeviceType: "COMPUTER", GroupType: "SMART"},
		},
	}
	cliCtx := &registry.CLIContext{PlatformClient: pc, Output: &captureOutput{}}

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
	out := &captureOutput{}
	pc := &platformMockClient{
		bmDetails: map[string]*jamfplatform.CBEngineBenchmarkResponseV2{
			"bm-src": {
				BenchmarkID:     "bm-src",
				Title:           "Source Benchmark",
				Description:     "original desc",
				BaselineID:      "bl-1",
				EnforcementMode: "AUDIT",
				Sources:         []jamfplatform.CBEngineSourceV1{{Branch: "main"}},
				Rules:           []jamfplatform.CBEngineRuleInfoV1{{ID: "r1", Enabled: true}, {ID: "r2", Enabled: false}},
				Target:          jamfplatform.CBEngineTargetV2{DeviceGroups: []string{"grp-src-id"}},
			},
		},
	}
	cliCtx := &registry.CLIContext{PlatformClient: pc, Output: out}

	cmd := newCBCloneCmd(cliCtx)
	cmd.SetArgs([]string{"Source Benchmark", "Cloned Benchmark"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("clone: %v", err)
	}

	req := pc.createdBenchmark
	if req == nil {
		t.Fatal("CreateBenchmark was not called")
	}
	if req.Title != "Cloned Benchmark" {
		t.Errorf("cloned title = %q, want %q", req.Title, "Cloned Benchmark")
	}
	if req.Description != "original desc" {
		t.Errorf("description not copied: %q", req.Description)
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
	// Target groups copied from source
	if len(req.Target.DeviceGroups) != 1 || req.Target.DeviceGroups[0] != "grp-src-id" {
		t.Errorf("target groups = %v, want [grp-src-id]", req.Target.DeviceGroups)
	}
}

func TestCBClone_WithComputerGroupOverride(t *testing.T) {
	out := &captureOutput{}
	pc := &platformMockClient{
		bmDetails: map[string]*jamfplatform.CBEngineBenchmarkResponseV2{
			"bm-src": {
				Title:      "Source",
				BaselineID: "bl-1",
				Target:     jamfplatform.CBEngineTargetV2{DeviceGroups: []string{"old-grp-id"}},
			},
		},
		devGroups: []jamfplatform.DeviceGroupListReadRepresentationV1{
			{ID: "new-grp-id", Name: "New Group"},
		},
	}
	cliCtx := &registry.CLIContext{PlatformClient: pc, Output: out}

	cmd := newCBCloneCmd(cliCtx)
	cmd.SetArgs([]string{"Source", "Cloned", "--computer-group", "New Group"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("clone with override: %v", err)
	}

	req := pc.createdBenchmark
	if req == nil {
		t.Fatal("CreateBenchmark was not called")
	}
	if len(req.Target.DeviceGroups) != 1 || req.Target.DeviceGroups[0] != "new-grp-id" {
		t.Errorf("target groups = %v, want [new-grp-id]", req.Target.DeviceGroups)
	}
}

func TestCBDeleteByID(t *testing.T) {
	pc := &platformMockClient{}
	cliCtx := &registry.CLIContext{PlatformClient: pc, Output: &captureOutput{}}

	cmd := newCBDeleteCmd(cliCtx)
	cmd.SetArgs([]string{"bm-abc-123", "--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("delete by ID: %v", err)
	}
}

func TestCBDeleteByName(t *testing.T) {
	pc := &platformMockClient{
		benchmarks: &jamfplatform.CBEngineBenchmarksResponseV2{
			Benchmarks: []jamfplatform.CBEngineBenchmarkV2{
				{ID: "bm-named-id", Title: "Named Benchmark"},
			},
		},
	}
	cliCtx := &registry.CLIContext{PlatformClient: pc, Output: &captureOutput{}}

	cmd := newCBDeleteCmd(cliCtx)
	cmd.SetArgs([]string{"--name", "Named Benchmark", "--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("delete by name: %v", err)
	}
}

func TestCBDeleteNoArgs(t *testing.T) {
	pc := &platformMockClient{}
	cliCtx := &registry.CLIContext{PlatformClient: pc, Output: &captureOutput{}}

	cmd := newCBDeleteCmd(cliCtx)
	cmd.SetArgs([]string{"--yes"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when no ID or --name provided")
	}
}

func TestCBApply_ResolvesGroupNames(t *testing.T) {
	pc := &platformMockClient{
		devGroups: []jamfplatform.DeviceGroupListReadRepresentationV1{
			{ID: "grp-resolved-id", Name: "My Device Group"},
		},
	}
	cliCtx := &registry.CLIContext{PlatformClient: pc, Output: &captureOutput{}}

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

	req := pc.createdBenchmark
	if req == nil {
		t.Fatal("CreateBenchmark was not called")
	}
	if len(req.Target.DeviceGroups) != 1 || req.Target.DeviceGroups[0] != "grp-resolved-id" {
		t.Errorf("target groups = %v, want [grp-resolved-id]", req.Target.DeviceGroups)
	}
}

func TestCBApply_ComputerGroupOverride(t *testing.T) {
	pc := &platformMockClient{
		devGroups: []jamfplatform.DeviceGroupListReadRepresentationV1{
			{ID: "override-id", Name: "Override Group"},
		},
	}
	cliCtx := &registry.CLIContext{PlatformClient: pc, Output: &captureOutput{}}

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

	req := pc.createdBenchmark
	if req == nil {
		t.Fatal("CreateBenchmark was not called")
	}
	if len(req.Target.DeviceGroups) != 1 || req.Target.DeviceGroups[0] != "override-id" {
		t.Errorf("target groups = %v, want [override-id]", req.Target.DeviceGroups)
	}
}

func TestCBApply_LegacyFormat(t *testing.T) {
	pc := &platformMockClient{}
	cliCtx := &registry.CLIContext{PlatformClient: pc, Output: &captureOutput{}}

	// Legacy format: target.deviceGroups is []string (raw IDs), not []object.
	legacy := jamfplatform.CBEngineBenchmarkRequestV2{
		Title:            "Legacy Benchmark",
		SourceBaselineID: "bl-1",
		EnforcementMode:  "AUDIT",
		Sources:          []jamfplatform.CBEngineSourceV1{{Branch: "main"}},
		Rules:            []jamfplatform.CBEngineRuleRequestV2{{ID: "r1", Enabled: true}},
		Target:           jamfplatform.CBEngineTargetV2{DeviceGroups: []string{"raw-group-id-1"}},
	}
	path := writeTempJSON(t, legacy)

	cmd := newCBApplyCmd(cliCtx)
	cmd.SetArgs([]string{"--from-file", path})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("apply with legacy format: %v", err)
	}

	req := pc.createdBenchmark
	if req == nil {
		t.Fatal("CreateBenchmark was not called")
	}
	if req.Title != "Legacy Benchmark" {
		t.Errorf("title = %q, want %q", req.Title, "Legacy Benchmark")
	}
	// Legacy IDs should pass through directly without resolution.
	if len(req.Target.DeviceGroups) != 1 || req.Target.DeviceGroups[0] != "raw-group-id-1" {
		t.Errorf("target groups = %v, want [raw-group-id-1]", req.Target.DeviceGroups)
	}
}

func TestCBApply_LegacyFormatWithGroupOverride(t *testing.T) {
	pc := &platformMockClient{
		devGroups: []jamfplatform.DeviceGroupListReadRepresentationV1{
			{ID: "override-id", Name: "Override Group"},
		},
	}
	cliCtx := &registry.CLIContext{PlatformClient: pc, Output: &captureOutput{}}

	legacy := jamfplatform.CBEngineBenchmarkRequestV2{
		Title:            "Legacy With Override",
		SourceBaselineID: "bl-1",
		EnforcementMode:  "AUDIT",
		Target:           jamfplatform.CBEngineTargetV2{DeviceGroups: []string{"old-id"}},
	}
	path := writeTempJSON(t, legacy)

	cmd := newCBApplyCmd(cliCtx)
	cmd.SetArgs([]string{"--from-file", path, "--computer-group", "Override Group"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("apply legacy with override: %v", err)
	}

	req := pc.createdBenchmark
	if req == nil {
		t.Fatal("CreateBenchmark was not called")
	}
	// --computer-group should override even in legacy mode.
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

func TestClassicProfilePath_EscapesSpecialCharacters(t *testing.T) {
	tests := []struct {
		profileType string
		name        string
		wantSuffix  string
	}{
		{"computer", "Simple", "/JSSResource/osxconfigurationprofiles/name/Simple"},
		{"mobile", "Simple", "/JSSResource/mobiledeviceconfigurationprofiles/name/Simple"},
		{"computer", "Has Spaces", "/JSSResource/osxconfigurationprofiles/name/Has%20Spaces"},
		{"computer", "Slash/Test", "/JSSResource/osxconfigurationprofiles/name/Slash%2FTest"},
		{"computer", "Plus+Sign", "/JSSResource/osxconfigurationprofiles/name/Plus+Sign"},
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
	pc := &platformMockClient{
		blueprints: []jamfplatform.BlueprintOverviewV1{
			{ID: "bp-id-1", Name: "Test BP"},
		},
	}
	cliCtx := &registry.CLIContext{PlatformClient: pc}
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
	}
}

func TestResolveBlueprintID_NeitherErrors(t *testing.T) {
	_, err := resolveBlueprintID(context.Background(), &registry.CLIContext{}, nil, "")
	if err == nil {
		t.Fatal("expected error when neither args nor name flag provided")
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

	data, err := downloadClassicProfile(cmd, cliCtx, "Test Profile", "computer")
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

	_, err := downloadClassicProfile(cmd, cliCtx, "Missing Profile", "computer")
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got %q", err.Error())
	}
}

func TestDownloadClassicProfile_NoPayloads(t *testing.T) {
	xmlResp := `<os_x_configuration_profile><general><name>Empty</name></general></os_x_configuration_profile>`
	mock := &classicHTTPMock{statusCode: 200, body: xmlResp}
	cliCtx := &registry.CLIContext{Client: mock}
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	_, err := downloadClassicProfile(cmd, cliCtx, "Empty Profile", "computer")
	if err == nil {
		t.Fatal("expected error for missing payloads")
	}
	if !strings.Contains(err.Error(), "no <payloads>") {
		t.Errorf("expected 'no <payloads>' error, got %q", err.Error())
	}
}

func TestDownloadClassicProfile_NilClient(t *testing.T) {
	cliCtx := &registry.CLIContext{Client: nil}
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	_, err := downloadClassicProfile(cmd, cliCtx, "Test", "computer")
	if err == nil {
		t.Fatal("expected error for nil client")
	}
	if !strings.Contains(err.Error(), "authentication") {
		t.Errorf("expected authentication error, got %q", err.Error())
	}
}

// ── Portable export/apply tests ───────────────────────────────────────────

func TestReverseResolveGroups(t *testing.T) {
	pc := &platformMockClient{
		devGroups: []jamfplatform.DeviceGroupListReadRepresentationV1{
			{ID: "uuid-1", Name: "Lab Macs", DeviceType: "COMPUTER"},
			{ID: "uuid-2", Name: "Shared iPads", DeviceType: "MOBILE_DEVICE"},
		},
	}

	groups := reverseResolveGroups(context.Background(), pc, []string{"uuid-1", "uuid-2", "uuid-deleted"})

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
	pc := &platformMockClient{}
	groups := reverseResolveGroups(context.Background(), pc, nil)
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
	}
}

func TestBlueprintExportRoundTrip(t *testing.T) {
	// Simulate: export from source → marshal → unmarshal on target via parseBlueprintApplyInput
	pc := &platformMockClient{
		devGroups: []jamfplatform.DeviceGroupListReadRepresentationV1{
			{ID: "source-uuid", Name: "Lab Macs", DeviceType: "COMPUTER"},
		},
		details: map[string]*jamfplatform.BlueprintDetailV1{
			"bp-1": {
				ID:          "bp-1",
				Name:        "Round Trip BP",
				Description: "test",
				Scope:       jamfplatform.BlueprintUpdateScopeV1{DeviceGroups: []string{"source-uuid"}},
				Steps: []jamfplatform.BlueprintStepV1{
					{Name: "Step 1", Components: []jamfplatform.BlueprintComponentV1{
						{Identifier: "com.jamf.ddm.passcode-settings", Configuration: json.RawMessage(`{"RequirePasscode": true}`)},
					}},
				},
			},
		},
	}

	// Export
	bp := pc.details["bp-1"]
	exported := blueprintToExport(context.Background(), pc, bp)

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
		t.Errorf("target scope: got %v, want [target-uuid]", req.Scope.DeviceGroups)
	}
	if len(req.Steps) != 1 || req.Steps[0].Name != "Step 1" {
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

func TestReverseResolveGroups_ListError(t *testing.T) {
	// platformMockClient with no devGroups and a nil slice simulates an
	// empty return. To simulate an error we'd need to extend the mock,
	// but we can verify the degraded path: all groups become UUID-only.
	pc := &platformMockClient{} // no devGroups populated
	groups := reverseResolveGroups(context.Background(), pc, []string{"uuid-1"})

	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if groups[0].ID != "uuid-1" || groups[0].Name != "" {
		t.Errorf("expected UUID-only fallback, got %+v", groups[0])
	}
}
