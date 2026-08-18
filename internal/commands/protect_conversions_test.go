// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamfprotect-go-sdk/jamfprotect"

	"github.com/Jamf-Concepts/jamf-cli/internal/protect"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// mockProtectClient embeds the interface so only the methods a given test
// needs must be implemented. The zero-value embed panics if an unimplemented
// method is called, which is correct for these narrowly-scoped tests.
type mockProtectClient struct {
	registry.ProtectClient

	ulfFilters []jamfprotect.UnifiedLoggingFilter
	ulfSets    []jamfprotect.UnifiedLoggingFilterSet
}

func (m *mockProtectClient) ListUnifiedLoggingFilters(_ context.Context) ([]jamfprotect.UnifiedLoggingFilter, error) {
	return m.ulfFilters, nil
}

func (m *mockProtectClient) ListUnifiedLoggingFilterSets(_ context.Context) ([]jamfprotect.UnifiedLoggingFilterSet, error) {
	return m.ulfSets, nil
}

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
		Permissions: &jamfprotect.RolePermissions{
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

func TestPlanToExport_UnifiedLoggingFilterSetNames(t *testing.T) {
	p := &jamfprotect.Plan{
		Name: "Plan with ULF Sets",
		UnifiedLoggingFilterSets: []jamfprotect.PlanUnifiedLoggingFilterSet{
			{UUID: "ulfs-1", Name: "Set One"},
			{UUID: "ulfs-2", Name: "Set Two"},
		},
	}
	export := planToExport(p)

	want := []string{"Set One", "Set Two"}
	if len(export.ULFSets) != len(want) {
		t.Fatalf("ULFSets length = %d, want %d", len(export.ULFSets), len(want))
	}
	for i, w := range want {
		if export.ULFSets[i] != w {
			t.Errorf("ULFSets[%d] = %q, want %q", i, export.ULFSets[i], w)
		}
	}
}

func TestPlanToExport_NoUnifiedLoggingFilterSets(t *testing.T) {
	export := planToExport(&jamfprotect.Plan{Name: "Bare"})
	if export.ULFSets != nil {
		t.Errorf("ULFSets = %v, want nil so the key is omitted from export", export.ULFSets)
	}
}

func TestPlanExportToInput_ResolvesULFSetNames(t *testing.T) {
	mock := &mockProtectClient{
		ulfSets: []jamfprotect.UnifiedLoggingFilterSet{
			{UUID: "ulfs-1", Name: "Set One"},
			{UUID: "ulfs-2", Name: "Set Two"},
		},
	}
	r := protect.NewResolver(mock)

	input, err := planExportToInput(context.Background(), planExport{
		Name:    "My Plan",
		ULFSets: []string{"Set Two", "Set One"},
	}, r)
	if err != nil {
		t.Fatalf("planExportToInput() error = %v", err)
	}
	want := []string{"ulfs-2", "ulfs-1"}
	if len(input.UnifiedLoggingFilterSets) != len(want) {
		t.Fatalf("UnifiedLoggingFilterSets length = %d, want %d", len(input.UnifiedLoggingFilterSets), len(want))
	}
	for i, w := range want {
		if input.UnifiedLoggingFilterSets[i] != w {
			t.Errorf("UnifiedLoggingFilterSets[%d] = %q, want %q", i, input.UnifiedLoggingFilterSets[i], w)
		}
	}
}

func TestPlanExportToInput_UnresolvableULFSetName(t *testing.T) {
	mock := &mockProtectClient{}
	r := protect.NewResolver(mock)

	_, err := planExportToInput(context.Background(), planExport{
		Name:    "My Plan",
		ULFSets: []string{"Missing"},
	}, r)
	if err == nil {
		t.Fatal("planExportToInput() error = nil, want error for unresolvable ULF set name")
	}
	if !strings.Contains(err.Error(), `resolving unified logging filter set "Missing"`) {
		t.Errorf("error = %q, want to mention the unresolvable name", err.Error())
	}
}

func TestFlattenPlan_UnifiedLoggingFilterSetsJoined(t *testing.T) {
	m := flattenPlan(jamfprotect.Plan{
		Name: "Plan",
		UnifiedLoggingFilterSets: []jamfprotect.PlanUnifiedLoggingFilterSet{
			{UUID: "a", Name: "Set A"},
			{UUID: "b", Name: "Set B"},
		},
	})
	if got := m["unifiedLoggingFilterSets"]; got != "Set A, Set B" {
		t.Errorf("unifiedLoggingFilterSets = %v, want %q", got, "Set A, Set B")
	}
}

func TestFlattenPlan_EmptyUnifiedLoggingFilterSetsPresent(t *testing.T) {
	m := flattenPlan(jamfprotect.Plan{Name: "Plan"})
	if _, ok := m["unifiedLoggingFilterSets"]; !ok {
		t.Error("unifiedLoggingFilterSets absent, want present (and empty) so table/csv columns stay stable across rows")
	} else if got := m["unifiedLoggingFilterSets"]; got != "" {
		t.Errorf("unifiedLoggingFilterSets = %v, want empty string", got)
	}
}

// ─── unified logging filter sets ────────────────────────────────────────────

func TestFlattenULFSet_FiltersAndPlans(t *testing.T) {
	m := flattenULFSet(jamfprotect.UnifiedLoggingFilterSet{
		UUID:        "s-1",
		Name:        "My Set",
		Description: "desc",
		Filters: []jamfprotect.UnifiedLoggingFilterSetFilter{
			{UUID: "f-1", Name: "Filter One"},
			{UUID: "f-2", Name: "Filter Two"},
		},
		Plans: []jamfprotect.UnifiedLoggingFilterSetPlan{
			{ID: "p-1", Name: "Plan One"},
		},
	})

	if got := m["uuid"]; got != "s-1" {
		t.Errorf("uuid = %v, want %q", got, "s-1")
	}
	if got := m["name"]; got != "My Set" {
		t.Errorf("name = %v, want %q", got, "My Set")
	}
	if got := m["filtersCount"]; got != 2 {
		t.Errorf("filtersCount = %v, want 2", got)
	}
	if got := m["filters"]; got != "Filter One, Filter Two" {
		t.Errorf("filters = %v, want %q", got, "Filter One, Filter Two")
	}
	if got := m["plans"]; got != "Plan One" {
		t.Errorf("plans = %v, want %q", got, "Plan One")
	}
}

func TestFlattenULFSet_OmitsEmptyCollections(t *testing.T) {
	m := flattenULFSet(jamfprotect.UnifiedLoggingFilterSet{Name: "Empty"})

	if _, ok := m["filters"]; !ok {
		t.Error("filters absent, want present (and empty) so table/csv columns stay stable across rows")
	} else if got := m["filters"]; got != "" {
		t.Errorf("filters = %v, want empty string", got)
	}
	if _, ok := m["plans"]; !ok {
		t.Error("plans absent, want present (and empty) so table/csv columns stay stable across rows")
	} else if got := m["plans"]; got != "" {
		t.Errorf("plans = %v, want empty string", got)
	}
	if got := m["filtersCount"]; got != 0 {
		t.Errorf("filtersCount = %v, want 0", got)
	}
}

func TestULFSetToExport_UsesFilterNames(t *testing.T) {
	export := ulfSetToExport(&jamfprotect.UnifiedLoggingFilterSet{
		Name:        "My Set",
		Description: "desc",
		Filters: []jamfprotect.UnifiedLoggingFilterSetFilter{
			{UUID: "f-1", Name: "Filter One"},
			{UUID: "f-2", Name: "Filter Two"},
		},
		// Plans is intentionally set: it is a server-side back-reference and
		// must not leak into the portable export format.
		Plans: []jamfprotect.UnifiedLoggingFilterSetPlan{{ID: "p-1", Name: "Plan One"}},
	})

	if export.Name != "My Set" {
		t.Errorf("Name = %q, want %q", export.Name, "My Set")
	}
	if export.Description != "desc" {
		t.Errorf("Description = %q, want %q", export.Description, "desc")
	}
	want := []string{"Filter One", "Filter Two"}
	if len(export.Filters) != len(want) {
		t.Fatalf("Filters length = %d, want %d", len(export.Filters), len(want))
	}
	for i, w := range want {
		if export.Filters[i] != w {
			t.Errorf("Filters[%d] = %q, want %q", i, export.Filters[i], w)
		}
	}
}

func TestULFSetToExport_EmptyFiltersIsNonNil(t *testing.T) {
	// The API accepts an empty filter list, so an emptied set must round-trip
	// as [] rather than dropping the key and re-sending the old membership.
	export := ulfSetToExport(&jamfprotect.UnifiedLoggingFilterSet{Name: "Empty"})
	if export.Filters == nil {
		t.Error("Filters = nil, want non-nil empty slice")
	}
	if len(export.Filters) != 0 {
		t.Errorf("Filters length = %d, want 0", len(export.Filters))
	}
}

func TestULFSetExportToInput_ResolvesFilterNames(t *testing.T) {
	mock := &mockProtectClient{
		ulfFilters: []jamfprotect.UnifiedLoggingFilter{
			{UUID: "f-1", Name: "Filter One"},
			{UUID: "f-2", Name: "Filter Two"},
		},
	}
	r := protect.NewResolver(mock)

	input, err := ulfSetExportToInput(context.Background(), ulfSetExport{
		Name:        "My Set",
		Description: "desc",
		Filters:     []string{"Filter Two", "Filter One"},
	}, r)
	if err != nil {
		t.Fatalf("ulfSetExportToInput() error = %v", err)
	}
	if input.Name != "My Set" {
		t.Errorf("Name = %q, want %q", input.Name, "My Set")
	}
	if input.Description != "desc" {
		t.Errorf("Description = %q, want %q", input.Description, "desc")
	}
	want := []string{"f-2", "f-1"}
	if len(input.Filters) != len(want) {
		t.Fatalf("Filters length = %d, want %d", len(input.Filters), len(want))
	}
	for i, w := range want {
		if input.Filters[i] != w {
			t.Errorf("Filters[%d] = %q, want %q", i, input.Filters[i], w)
		}
	}
}

func TestULFSetExportToInput_UnresolvableFilterName(t *testing.T) {
	mock := &mockProtectClient{}
	r := protect.NewResolver(mock)

	_, err := ulfSetExportToInput(context.Background(), ulfSetExport{
		Name:    "My Set",
		Filters: []string{"Missing"},
	}, r)
	if err == nil {
		t.Fatal("ulfSetExportToInput() error = nil, want error for unresolvable filter name")
	}
}

// ─── flattenULF ─────────────────────────────────────────────────────────────

func TestFlattenULF_SetsJoined(t *testing.T) {
	m := flattenULF(jamfprotect.UnifiedLoggingFilter{
		UUID:        "f-1",
		Name:        "My Filter",
		Description: "desc",
		Enabled:     true,
		Filter:      `subsystem == "com.apple.TimeMachine"`,
		Tags:        []string{"macos", "backup"},
		Sets: []jamfprotect.UnifiedLoggingFilterSetRef{
			{UUID: "s-1", Name: "Set A"},
			{UUID: "s-2", Name: "Set B"},
		},
	})

	if got := m["uuid"]; got != "f-1" {
		t.Errorf("uuid = %v, want %q", got, "f-1")
	}
	if got := m["name"]; got != "My Filter" {
		t.Errorf("name = %v, want %q", got, "My Filter")
	}
	if got := m["description"]; got != "desc" {
		t.Errorf("description = %v, want %q", got, "desc")
	}
	if got := m["enabled"]; got != true {
		t.Errorf("enabled = %v, want true", got)
	}
	if got := m["tags"]; got != "macos, backup" {
		t.Errorf("tags = %v, want %q", got, "macos, backup")
	}
	if got := m["sets"]; got != "Set A, Set B" {
		t.Errorf("sets = %v, want %q", got, "Set A, Set B")
	}
}

func TestFlattenULF_OmitsEmptySets(t *testing.T) {
	m := flattenULF(jamfprotect.UnifiedLoggingFilter{Name: "Loner"})
	if _, ok := m["sets"]; !ok {
		t.Error("sets absent, want present (and empty) so table/csv columns stay stable across rows")
	} else if got := m["sets"]; got != "" {
		t.Errorf("sets = %v, want empty string", got)
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
		Cisid:     []jamfprotect.InsightCisID{{ID: "5.1.1", OSVersion: "14"}},
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
		Cisid: []jamfprotect.InsightCisID{
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

// --- Analytic document schema detection ---

// `analytics export` emits the community YAML schema and `apply --scaffold`
// emits the SDK AnalyticInput shape. apply must accept both, because the two
// differ on `actions` (objects vs strings) and decoding either into the wrong
// struct fails outright.
func TestAnalyticDocumentIsCommunitySchema(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		want bool
	}{
		{
			name: "community yaml from analytics export",
			doc: `name: BlazingKeylogger
shortDescription: Known malware IOC
severity: High
actions:
    - name: Report
      parameters: '{}'
`,
			want: true,
		},
		{
			name: "community yaml without shortDescription still detected by action objects",
			doc: `name: Something
actions:
    - name: Report
      parameters: '{}'
`,
			want: true,
		},
		{
			name: "sdk shape as json, capitalised go field names",
			doc:  `{"Name":"Custom","Actions":["Report"],"AnalyticActions":[{"Name":"Report","Parameters":"{}"}]}`,
			want: false,
		},
		{
			name: "sdk shape as yaml, lowercased field names",
			doc: `name: Custom
analyticactions:
    - name: Report
      parameters: '{}'
`,
			want: false,
		},
		{
			name: "sdk shape with string actions",
			doc:  `{"Name":"Custom","Actions":["Report"]}`,
			want: false,
		},
		{
			name: "minimal document defaults to the sdk shape",
			doc:  `{"Name":"Custom"}`,
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := analyticDocumentIsCommunitySchema([]byte(tc.doc)); got != tc.want {
				t.Errorf("analyticDocumentIsCommunitySchema() = %v, want %v", got, tc.want)
			}
		})
	}
}

// The regression this fixes: piping `analytics export` into `analytics apply`
// failed with "input is not valid JSON or YAML".
func TestAnalyticInputFromDocument_AcceptsCommunityExport(t *testing.T) {
	doc := []byte(`name: BlazingKeylogger
longDescription: A plist name associated with BlazingKeylogger was written.
shortDescription: Known malware IOC for BlazingKeylogger
remediation: Delete the file.
level: 1
inputType: GPFSEvent
filter: '"LaunchDaemon" IN $tags'
severity: High
categories:
    - Known Malicious File
actions:
    - name: Report
      parameters: '{}'
`)

	got, err := analyticInputFromDocument(doc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "BlazingKeylogger" {
		t.Errorf("Name = %q, want BlazingKeylogger", got.Name)
	}
	if got.InputType != "GPFSEvent" {
		t.Errorf("InputType = %q, want GPFSEvent", got.InputType)
	}
	if got.Description != "Known malware IOC for BlazingKeylogger" {
		t.Errorf("Description = %q, want the community shortDescription", got.Description)
	}
	if len(got.AnalyticActions) != 1 || got.AnalyticActions[0].Name != "Report" {
		t.Fatalf("AnalyticActions = %+v, want one Report action", got.AnalyticActions)
	}
	if got.AnalyticActions[0].Parameters != "{}" {
		t.Errorf("Parameters = %q, want {}", got.AnalyticActions[0].Parameters)
	}
}

func TestAnalyticInputFromDocument_AcceptsScaffoldShape(t *testing.T) {
	doc := []byte(`{"Name":"Custom","InputType":"GPProcessEvent","AnalyticActions":[{"Name":"Report","Parameters":"{}"}]}`)

	got, err := analyticInputFromDocument(doc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "Custom" || got.InputType != "GPProcessEvent" {
		t.Errorf("got %+v, want the SDK shape decoded verbatim", got)
	}
	if len(got.AnalyticActions) != 1 {
		t.Errorf("AnalyticActions = %+v, want one entry", got.AnalyticActions)
	}
}

// analyticToYAML emits longDescription and remediation, so the conversion back
// must keep them or every export/import round-trip silently drops both.
func TestAnalyticYAMLToInput_PreservesLongDescriptionAndRemediation(t *testing.T) {
	original := jamfprotect.Analytic{
		Name:            "Custom",
		Description:     "short",
		LongDescription: "the long one",
		Remediation:     "do the thing",
		InputType:       "GPFSEvent",
		Severity:        "High",
		Level:           1,
	}

	got := analyticYAMLToInput(analyticToYAML(original))

	if got.LongDescription != "the long one" {
		t.Errorf("LongDescription = %q, want %q", got.LongDescription, "the long one")
	}
	if got.Remediation != "do the thing" {
		t.Errorf("Remediation = %q, want %q", got.Remediation, "do the thing")
	}
	if got.Description != "short" {
		t.Errorf("Description = %q, want %q", got.Description, "short")
	}
}
