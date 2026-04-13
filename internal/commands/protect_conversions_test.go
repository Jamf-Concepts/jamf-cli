// Copyright 2026, Jamf Software LLC

package commands

import (
	"testing"

	"github.com/Jamf-Concepts/jamfprotect-go-sdk/jamfprotect"
)

// ─── flattenPlan ────────────────────────────────────────────────────────────

func TestFlattenPlan_BasicFields(t *testing.T) {
	p := jamfprotect.Plan{
		Name:        "Test Plan",
		Description: "A test plan",
		LogLevel:    "info",
		AutoUpdate:  true,
	}
	m := flattenPlan(p)

	if m["name"] != "Test Plan" {
		t.Errorf("name = %v, want %q", m["name"], "Test Plan")
	}
	if _, ok := m["description"]; ok {
		t.Error("description should not be in flattened plan output")
	}
	if m["logLevel"] != "info" {
		t.Errorf("logLevel = %v, want %q", m["logLevel"], "info")
	}
	if m["autoUpdate"] != true {
		t.Errorf("autoUpdate = %v, want true", m["autoUpdate"])
	}
}

func TestFlattenPlan_ActionConfigName(t *testing.T) {
	p := jamfprotect.Plan{
		Name: "Plan with AC",
		ActionConfigs: &jamfprotect.PlanRef{
			ID:   "ac-123",
			Name: "My Action Config",
		},
	}
	m := flattenPlan(p)

	if m["actionConfig"] != "My Action Config" {
		t.Errorf("actionConfig = %v, want %q", m["actionConfig"], "My Action Config")
	}
}

func TestFlattenPlan_NilOptionalFields(t *testing.T) {
	p := jamfprotect.Plan{
		Name:          "Minimal Plan",
		USBControlSet: nil,
		TelemetryV2:   nil,
		Telemetry:     nil,
		ActionConfigs: nil,
	}

	// Should not panic
	m := flattenPlan(p)

	if _, ok := m["usbControlSet"]; ok {
		t.Error("usbControlSet should be absent when nil")
	}
	if _, ok := m["telemetry"]; ok {
		t.Error("telemetry should be absent when nil")
	}
	if _, ok := m["actionConfig"]; ok {
		t.Error("actionConfig should be absent when nil")
	}
}

// ─── flattenComputer ────────────────────────────────────────────────────────

func TestFlattenComputer_OmitsNilFields(t *testing.T) {
	c := jamfprotect.Computer{
		HostName: new("test-host"),
		// All other pointer fields are nil
	}
	m := flattenComputer(c)

	if _, ok := m["serial"]; ok {
		t.Error("serial should be absent when nil")
	}
	if _, ok := m["osString"]; ok {
		t.Error("osString should be absent when nil")
	}
	if _, ok := m["osMajor"]; ok {
		t.Error("osMajor should be absent when nil")
	}
}

func TestFlattenComputer_IncludesPresentFields(t *testing.T) {
	c := jamfprotect.Computer{
		HostName:         new("my-mac"),
		Serial:           new("C02XYZ"),
		OSString:         new("14.4.1"),
		OSMajor:          new(int64(14)),
		OSMinor:          new(int64(4)),
		OSPatch:          new(int64(1)),
		ConnectionStatus: new("connected"),
		MemorySize:       new(16.0),
	}
	m := flattenComputer(c)

	if m["hostname"] != "my-mac" {
		t.Errorf("hostname = %v, want %q", m["hostname"], "my-mac")
	}
	if m["serial"] != "C02XYZ" {
		t.Errorf("serial = %v, want %q", m["serial"], "C02XYZ")
	}
	if m["osString"] != "14.4.1" {
		t.Errorf("osString = %v, want %q", m["osString"], "14.4.1")
	}
	if m["osMajor"] != int64(14) {
		t.Errorf("osMajor = %v, want %d", m["osMajor"], 14)
	}
	if m["connectionStatus"] != "connected" {
		t.Errorf("connectionStatus = %v, want %q", m["connectionStatus"], "connected")
	}
	if m["memorySize"] != int64(16) {
		t.Errorf("memorySize = %v, want %d", m["memorySize"], 16)
	}
}

func TestFlattenComputer_PlanName(t *testing.T) {
	c := jamfprotect.Computer{
		HostName: new("plan-host"),
		Plan: &jamfprotect.ComputerPlan{
			ID:   new("plan-1"),
			Name: new("Production Plan"),
		},
	}
	m := flattenComputer(c)

	if m["plan"] != "Production Plan" {
		t.Errorf("plan = %v, want %q", m["plan"], "Production Plan")
	}
}

// ─── flattenAnalyticSet ─────────────────────────────────────────────────────

func TestFlattenAnalyticSet_AnalyticsCount(t *testing.T) {
	s := jamfprotect.AnalyticSet{
		Name: "Test Set",
		Analytics: []jamfprotect.AnalyticSetAnalytic{
			{UUID: "a1", Name: "Analytic 1"},
			{UUID: "a2", Name: "Analytic 2"},
			{UUID: "a3", Name: "Analytic 3"},
		},
	}
	m := flattenAnalyticSet(s)

	if m["analyticsCount"] != 3 {
		t.Errorf("analyticsCount = %v, want 3", m["analyticsCount"])
	}
}

// ─── roleToInput ────────────────────────────────────────────────────────────

func TestRoleToInput_MapsPermissions(t *testing.T) {
	r := &jamfprotect.Role{
		Name: "Admin",
		Permissions: jamfprotect.RolePermissions{
			Read:  []string{"plans", "analytics", "computers"},
			Write: []string{"plans"},
		},
	}
	input := roleToInput(r)

	if input.Name != "Admin" {
		t.Errorf("Name = %q, want %q", input.Name, "Admin")
	}
	if len(input.ReadResources) != 3 {
		t.Fatalf("ReadResources length = %d, want 3", len(input.ReadResources))
	}
	if input.ReadResources[0] != "plans" {
		t.Errorf("ReadResources[0] = %q, want %q", input.ReadResources[0], "plans")
	}
	if len(input.WriteResources) != 1 {
		t.Fatalf("WriteResources length = %d, want 1", len(input.WriteResources))
	}
	if input.WriteResources[0] != "plans" {
		t.Errorf("WriteResources[0] = %q, want %q", input.WriteResources[0], "plans")
	}
}

// ─── planToExport ───────────────────────────────────────────────────────────

func TestPlanToExport_ExceptionSetNames(t *testing.T) {
	p := &jamfprotect.Plan{
		Name: "Plan with ES",
		ExceptionSets: []jamfprotect.PlanExceptionSet{
			{UUID: "es-1", Name: "Exception Set 1"},
			{UUID: "es-2", Name: "Exception Set 2"},
		},
	}
	export := planToExport(p)

	if len(export.ExceptionSets) != 2 {
		t.Fatalf("ExceptionSets length = %d, want 2", len(export.ExceptionSets))
	}
	if export.ExceptionSets[0] != "Exception Set 1" {
		t.Errorf("ExceptionSets[0] = %q, want %q", export.ExceptionSets[0], "Exception Set 1")
	}
	if export.ExceptionSets[1] != "Exception Set 2" {
		t.Errorf("ExceptionSets[1] = %q, want %q", export.ExceptionSets[1], "Exception Set 2")
	}
}

func TestPlanToExport_AnalyticSetNames(t *testing.T) {
	p := &jamfprotect.Plan{
		Name: "Plan with AS",
		AnalyticSets: []jamfprotect.PlanAnalyticSet{
			{
				Type: "Report",
				AnalyticSet: jamfprotect.PlanAnalyticSetRef{
					UUID: "as-uuid-1",
					Name: "Custom Set",
				},
			},
			{
				Type: "Prevent",
				AnalyticSet: jamfprotect.PlanAnalyticSetRef{
					UUID: "as-uuid-2",
					Name: "Managed Set",
				},
			},
		},
	}
	export := planToExport(p)

	if len(export.AnalyticSets) != 2 {
		t.Fatalf("AnalyticSets length = %d, want 2", len(export.AnalyticSets))
	}
	if export.AnalyticSets[0].Name != "Custom Set" {
		t.Errorf("AnalyticSets[0].Name = %q, want %q", export.AnalyticSets[0].Name, "Custom Set")
	}
	if export.AnalyticSets[0].Type != "Report" {
		t.Errorf("AnalyticSets[0].Type = %q, want %q", export.AnalyticSets[0].Type, "Report")
	}
	if export.AnalyticSets[1].Name != "Managed Set" {
		t.Errorf("AnalyticSets[1].Name = %q, want %q", export.AnalyticSets[1].Name, "Managed Set")
	}
}

// ─── analyticYAMLToInput ────────────────────────────────────────────────────

func TestAnalyticYAMLToInput_EmptySlices(t *testing.T) {
	ay := analyticYAML{
		Name:      "Empty Slices",
		InputType: "event",
		// Tags, Categories, SnapshotFiles all nil
	}
	input := analyticYAMLToInput(ay)

	if input.Tags == nil {
		t.Error("Tags should be non-nil empty slice, got nil")
	}
	if len(input.Tags) != 0 {
		t.Errorf("Tags length = %d, want 0", len(input.Tags))
	}
	if input.Categories == nil {
		t.Error("Categories should be non-nil empty slice, got nil")
	}
	if len(input.Categories) != 0 {
		t.Errorf("Categories length = %d, want 0", len(input.Categories))
	}
	if input.SnapshotFiles == nil {
		t.Error("SnapshotFiles should be non-nil empty slice, got nil")
	}
	if len(input.SnapshotFiles) != 0 {
		t.Errorf("SnapshotFiles length = %d, want 0", len(input.SnapshotFiles))
	}
}

// ─── analyticToYAML round-trip ──────────────────────────────────────────────

func TestAnalyticToYAML_RoundTrip(t *testing.T) {
	original := jamfprotect.Analytic{
		UUID:            "uuid-123",
		Name:            "Test Analytic",
		InputType:       "event",
		Description:     "Short description",
		LongDescription: "Long description",
		Level:           3,
		Severity:        "high",
		Filter:          "process.name == 'test'",
		Tags:            []string{"persistence", "defense-evasion"},
		Categories:      []string{"Malware"},
		SnapshotFiles:   []string{"/usr/bin/test"},
		Remediation:     "Remove the process",
		AnalyticActions: []jamfprotect.AnalyticAction{
			{Name: "Log", Parameters: ""},
			{Name: "SmartGroup", Parameters: "group-1"},
		},
		Context: []jamfprotect.AnalyticContext{
			{Name: "proc", Type: "process", Exprs: []string{"$event.process"}},
		},
	}

	// Convert to YAML representation and back
	yamlRep := analyticToYAML(original)
	input := analyticYAMLToInput(yamlRep)

	if input.Name != original.Name {
		t.Errorf("Name = %q, want %q", input.Name, original.Name)
	}
	if input.InputType != original.InputType {
		t.Errorf("InputType = %q, want %q", input.InputType, original.InputType)
	}
	if input.Description != original.Description {
		t.Errorf("Description = %q, want %q", input.Description, original.Description)
	}
	if input.Level != original.Level {
		t.Errorf("Level = %d, want %d", input.Level, original.Level)
	}
	if input.Severity != original.Severity {
		t.Errorf("Severity = %q, want %q", input.Severity, original.Severity)
	}
	if input.Filter != original.Filter {
		t.Errorf("Filter = %q, want %q", input.Filter, original.Filter)
	}
	if len(input.Tags) != len(original.Tags) {
		t.Fatalf("Tags length = %d, want %d", len(input.Tags), len(original.Tags))
	}
	for i, tag := range input.Tags {
		if tag != original.Tags[i] {
			t.Errorf("Tags[%d] = %q, want %q", i, tag, original.Tags[i])
		}
	}
	if len(input.Categories) != len(original.Categories) {
		t.Fatalf("Categories length = %d, want %d", len(input.Categories), len(original.Categories))
	}
	if len(input.SnapshotFiles) != len(original.SnapshotFiles) {
		t.Fatalf("SnapshotFiles length = %d, want %d", len(input.SnapshotFiles), len(original.SnapshotFiles))
	}
	if len(input.AnalyticActions) != len(original.AnalyticActions) {
		t.Fatalf("AnalyticActions length = %d, want %d", len(input.AnalyticActions), len(original.AnalyticActions))
	}
	if input.AnalyticActions[0].Name != "Log" {
		t.Errorf("AnalyticActions[0].Name = %q, want %q", input.AnalyticActions[0].Name, "Log")
	}
	if input.AnalyticActions[1].Parameters != "group-1" {
		t.Errorf("AnalyticActions[1].Parameters = %q, want %q", input.AnalyticActions[1].Parameters, "group-1")
	}
	if len(input.Context) != 1 {
		t.Fatalf("Context length = %d, want 1", len(input.Context))
	}
	if input.Context[0].Name != "proc" {
		t.Errorf("Context[0].Name = %q, want %q", input.Context[0].Name, "proc")
	}
}

// ─── flattenAlert ────────────────────────────────────────────────────────────

func TestFlattenAlert_BasicFields(t *testing.T) {
	a := jamfprotect.Alert{
		UUID:      "alert-uuid-1",
		Status:    "New",
		Severity:  "HIGH",
		EventType: "GPUProcessLaunch",
		Received:  "2026-04-13T10:00:00Z",
		Created:   "2026-04-13T10:00:00Z",
	}
	m := flattenAlert(a)

	if m["uuid"] != "alert-uuid-1" {
		t.Errorf("uuid = %v, want %q", m["uuid"], "alert-uuid-1")
	}
	if m["status"] != "New" {
		t.Errorf("status = %v, want %q", m["status"], "New")
	}
	if m["severity"] != "HIGH" {
		t.Errorf("severity = %v, want %q", m["severity"], "HIGH")
	}
	if m["eventType"] != "GPUProcessLaunch" {
		t.Errorf("eventType = %v, want %q", m["eventType"], "GPUProcessLaunch")
	}
}

func TestFlattenAlert_WithComputer(t *testing.T) {
	a := jamfprotect.Alert{
		UUID: "alert-uuid-2",
		Computer: &jamfprotect.AlertComputer{
			UUID:     "computer-uuid-1",
			HostName: "my-mac.local",
		},
	}
	m := flattenAlert(a)

	if m["computer"] != "my-mac.local" {
		t.Errorf("computer = %v, want %q", m["computer"], "my-mac.local")
	}
}

func TestFlattenAlert_NoComputer(t *testing.T) {
	a := jamfprotect.Alert{UUID: "alert-uuid-3"}
	m := flattenAlert(a)

	if _, ok := m["computer"]; ok {
		t.Error("computer should be absent when nil")
	}
}

func TestFlattenAlert_AnalyticsJoined(t *testing.T) {
	a := jamfprotect.Alert{
		UUID: "alert-uuid-4",
		Analytics: []jamfprotect.AlertAnalytic{
			{Name: "Analytic One"},
			{Name: "Analytic Two"},
		},
	}
	m := flattenAlert(a)

	if m["analytics"] != "Analytic One, Analytic Two" {
		t.Errorf("analytics = %v, want %q", m["analytics"], "Analytic One, Analytic Two")
	}
}

// ─── flattenInsight ──────────────────────────────────────────────────────────

func TestFlattenInsight_BasicFields(t *testing.T) {
	i := jamfprotect.Insight{
		UUID:      "insight-uuid-1",
		Label:     "Ensure FileVault Is Enabled",
		Section:   "5",
		Enabled:   true,
		TotalPass: 42,
		TotalFail: 3,
		TotalNone: 1,
		CisID:     []jamfprotect.InsightCisID{{ID: "5.1.1", OSVersion: "14"}},
	}
	m := flattenInsight(i)

	if m["label"] != "Ensure FileVault Is Enabled" {
		t.Errorf("label = %v, want %q", m["label"], "Ensure FileVault Is Enabled")
	}
	if m["enabled"] != true {
		t.Errorf("enabled = %v, want true", m["enabled"])
	}
	if m["totalPass"] != int64(42) {
		t.Errorf("totalPass = %v, want 42", m["totalPass"])
	}
	if m["totalFail"] != int64(3) {
		t.Errorf("totalFail = %v, want 3", m["totalFail"])
	}
	if m["cisIDs"] != "5.1.1" {
		t.Errorf("cisIDs = %v, want %q", m["cisIDs"], "5.1.1")
	}
}

func TestFlattenInsight_MultipleCISIDs(t *testing.T) {
	i := jamfprotect.Insight{
		CisID: []jamfprotect.InsightCisID{
			{ID: "1.1", OSVersion: "14"},
			{ID: "1.2", OSVersion: "15"},
		},
	}
	m := flattenInsight(i)

	if m["cisIDs"] != "1.1, 1.2" {
		t.Errorf("cisIDs = %v, want %q", m["cisIDs"], "1.1, 1.2")
	}
}

// ─── flattenInsightComputer ──────────────────────────────────────────────────

func TestFlattenInsightComputer_BasicFields(t *testing.T) {
	c := jamfprotect.InsightComputer{
		UUID:                 "comp-uuid-1",
		HostName:             "mac-01.local",
		InsightsStatsFail:    5,
		InsightsStatsPass:    10,
		InsightsStatsUnknown: 2,
		InsightsUpdated:      "2026-04-13T09:00:00Z",
	}
	m := flattenInsightComputer(c)

	if m["uuid"] != "comp-uuid-1" {
		t.Errorf("uuid = %v, want %q", m["uuid"], "comp-uuid-1")
	}
	if m["hostName"] != "mac-01.local" {
		t.Errorf("hostName = %v, want %q", m["hostName"], "mac-01.local")
	}
	if m["statsFail"] != int64(5) {
		t.Errorf("statsFail = %v, want 5", m["statsFail"])
	}
	if m["statsPass"] != int64(10) {
		t.Errorf("statsPass = %v, want 10", m["statsPass"])
	}
}

// ─── flattenAuditLog ─────────────────────────────────────────────────────────

func TestFlattenAuditLog_BasicFields(t *testing.T) {
	l := jamfprotect.AuditLog{
		Date:       "2026-04-13T10:00:00Z",
		Op:         "createAnalytic",
		User:       "admin@example.com",
		IPs:        "192.168.1.1",
		ResourceID: "analytic-uuid-1",
	}
	m := flattenAuditLog(l)

	if m["date"] != "2026-04-13T10:00:00Z" {
		t.Errorf("date = %v, want %q", m["date"], "2026-04-13T10:00:00Z")
	}
	if m["op"] != "createAnalytic" {
		t.Errorf("op = %v, want %q", m["op"], "createAnalytic")
	}
	if m["user"] != "admin@example.com" {
		t.Errorf("user = %v, want %q", m["user"], "admin@example.com")
	}
	if m["resourceId"] != "analytic-uuid-1" {
		t.Errorf("resourceId = %v, want %q", m["resourceId"], "analytic-uuid-1")
	}
}

func TestFlattenAuditLog_NilError(t *testing.T) {
	l := jamfprotect.AuditLog{Op: "listPlans"}
	m := flattenAuditLog(l)

	if _, ok := m["error"]; ok {
		t.Error("error should be absent when nil")
	}
}

func TestFlattenAuditLog_WithError(t *testing.T) {
	errMsg := "permission denied"
	l := jamfprotect.AuditLog{
		Op:    "deletePlan",
		Error: &errMsg,
	}
	m := flattenAuditLog(l)

	if m["error"] != "permission denied" {
		t.Errorf("error = %v, want %q", m["error"], "permission denied")
	}
}
