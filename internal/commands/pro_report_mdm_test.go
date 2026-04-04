// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// parseMDMCommand
// ---------------------------------------------------------------------------

func TestParseMDMCommand_FullFields(t *testing.T) {
	m := map[string]any{
		"uuid":          "abc-123",
		"commandType":   "INSTALL_PROFILE",
		"commandState":  "ERROR",
		"dateCompleted": "2026-04-01T08:00:00.000Z",
		"dateSent":      "2026-03-31T12:00:00.000Z",
		"profileId":     float64(42),
		"client": map[string]any{
			"managementId": "mgmt-id-1",
			"clientType":   "COMPUTER",
		},
		"commandError": map[string]any{
			"errorCode":                 float64(10),
			"errorDomain":               "SPErrorDomain",
			"errorLocalizedDescription": "Something went wrong",
		},
	}

	result := parseMDMCommand(m)
	if result.CommandType != "INSTALL_PROFILE" {
		t.Errorf("commandType = %q, want INSTALL_PROFILE", result.CommandType)
	}
	if result.ProfileID != "42" {
		t.Errorf("profileId = %q, want 42", result.ProfileID)
	}
	if result.ClientMgmtID != "mgmt-id-1" {
		t.Errorf("clientMgmtID = %q, want mgmt-id-1", result.ClientMgmtID)
	}
	if result.ErrorDescription != "Something went wrong" {
		t.Errorf("errorDescription = %q", result.ErrorDescription)
	}
	if result.DateCompleted.IsZero() {
		t.Error("dateCompleted should not be zero")
	}
}

func TestParseMDMCommand_MinimalFields(t *testing.T) {
	m := map[string]any{
		"commandType":  "INSTALL_PROFILE",
		"commandState": "ERROR",
	}

	result := parseMDMCommand(m)
	if result.CommandType != "INSTALL_PROFILE" {
		t.Errorf("commandType = %q", result.CommandType)
	}
	if result.ProfileID != "" {
		t.Errorf("profileId = %q, want empty", result.ProfileID)
	}
}

// ---------------------------------------------------------------------------
// aggregateMDMFailures
// ---------------------------------------------------------------------------

func TestAggregateMDMFailures_GroupsByProfile(t *testing.T) {
	results := []mdmCommandResult{
		{ProfileID: "10", ClientMgmtID: "dev-1", DateCompleted: time.Now(), ErrorDescription: "error A"},
		{ProfileID: "10", ClientMgmtID: "dev-2", DateCompleted: time.Now(), ErrorDescription: "error A"},
		{ProfileID: "10", ClientMgmtID: "dev-1", DateCompleted: time.Now(), ErrorDescription: "error B"},
		{ProfileID: "20", ClientMgmtID: "dev-3", DateCompleted: time.Now(), ErrorDescription: "error C"},
	}

	lookup := map[string]string{"10": "WiFi Profile", "20": "VPN Profile"}
	report := aggregateMDMFailures(results, lookup, "profile", 30)

	if report.Summary.TotalErrors != 4 {
		t.Errorf("total_errors = %d, want 4", report.Summary.TotalErrors)
	}
	if report.Summary.UniqueProfiles != 2 {
		t.Errorf("unique_profiles = %d, want 2", report.Summary.UniqueProfiles)
	}
	if report.Summary.UniqueDevices != 3 {
		t.Errorf("unique_devices = %d, want 3", report.Summary.UniqueDevices)
	}
	if len(report.Failures) != 2 {
		t.Fatalf("got %d failures, want 2", len(report.Failures))
	}

	// Sorted by error count descending
	if report.Failures[0].Errors != 3 {
		t.Errorf("first failure errors = %d, want 3", report.Failures[0].Errors)
	}
	if report.Failures[0].Name != "WiFi Profile" {
		t.Errorf("first failure name = %q, want WiFi Profile", report.Failures[0].Name)
	}
	if report.Failures[0].Devices != 2 {
		t.Errorf("first failure devices = %d, want 2", report.Failures[0].Devices)
	}
	if report.Failures[0].TopError != "error A" {
		t.Errorf("top_error = %q, want error A", report.Failures[0].TopError)
	}
}

func TestAggregateMDMFailures_Empty(t *testing.T) {
	report := aggregateMDMFailures(nil, nil, "profile", 30)
	if report.Summary.TotalErrors != 0 {
		t.Errorf("total_errors = %d, want 0", report.Summary.TotalErrors)
	}
	if len(report.Failures) != 0 {
		t.Errorf("got %d failures, want 0", len(report.Failures))
	}
}

func TestAggregateMDMFailures_NoLookup(t *testing.T) {
	results := []mdmCommandResult{
		{ProfileID: "10", ClientMgmtID: "dev-1", DateCompleted: time.Now()},
	}

	report := aggregateMDMFailures(results, nil, "profile", 30)
	if len(report.Failures) != 1 {
		t.Fatalf("got %d failures, want 1", len(report.Failures))
	}
	// Without lookup, name falls back to profile ID
	if report.Failures[0].Name != "10" {
		t.Errorf("name = %q, want 10 (fallback)", report.Failures[0].Name)
	}
}

func TestAggregateMDMFailures_AppCommandFallback(t *testing.T) {
	results := []mdmCommandResult{
		{CommandType: "InstallApplication", ClientMgmtID: "dev-1", DateCompleted: time.Now()},
		{CommandType: "InstallApplication", ClientMgmtID: "dev-2", DateCompleted: time.Now()},
	}

	report := aggregateMDMFailures(results, nil, "app", 30)
	if len(report.Failures) != 1 {
		t.Fatalf("got %d failures, want 1", len(report.Failures))
	}
	// Falls back to command type as key
	if report.Failures[0].Name != "InstallApplication" {
		t.Errorf("name = %q, want InstallApplication", report.Failures[0].Name)
	}
}

// ---------------------------------------------------------------------------
// fetchProfileNameLookup (integration)
// ---------------------------------------------------------------------------

func TestFetchProfileNameLookup_Integration(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/JSSResource/osxconfigurationprofiles": {200, `{
				"os_x_configuration_profiles": [
					{"id": 10, "name": "WiFi"},
					{"id": 20, "name": "VPN"}
				]
			}`},
			"/JSSResource/mobiledeviceconfigurationprofiles": {200, `{
				"configuration_profiles": [
					{"id": 30, "name": "iOS WiFi"}
				]
			}`},
		},
	}

	lookup := fetchProfileNameLookup(context.Background(), client)
	if lookup["10"] != "WiFi" {
		t.Errorf("lookup[10] = %q, want WiFi", lookup["10"])
	}
	if lookup["20"] != "VPN" {
		t.Errorf("lookup[20] = %q, want VPN", lookup["20"])
	}
	if lookup["30"] != "iOS WiFi" {
		t.Errorf("lookup[30] = %q, want iOS WiFi", lookup["30"])
	}
}

func TestFetchProfileNameLookup_HandlesErrors(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/JSSResource/osxconfigurationprofiles":          {500, `{}`},
			"/JSSResource/mobiledeviceconfigurationprofiles": {500, `{}`},
		},
	}

	lookup := fetchProfileNameLookup(context.Background(), client)
	if len(lookup) != 0 {
		t.Errorf("expected empty lookup on error, got %d entries", len(lookup))
	}
}

// ---------------------------------------------------------------------------
// aggregateByDevice
// ---------------------------------------------------------------------------

func TestAggregateByDevice_ThresholdFilters(t *testing.T) {
	results := []mdmCommandResult{
		{ClientMgmtID: "dev-1", ClientType: "COMPUTER"},
		{ClientMgmtID: "dev-1", ClientType: "COMPUTER"},
		{ClientMgmtID: "dev-1", ClientType: "COMPUTER"},
		{ClientMgmtID: "dev-2", ClientType: "MOBILE_DEVICE"},
	}

	// Threshold 3: only dev-1 qualifies
	summaries := aggregateByDevice(results, 3, nil)
	if len(summaries) != 1 {
		t.Fatalf("got %d summaries, want 1", len(summaries))
	}
	if summaries[0].ManagementID != "dev-1" {
		t.Errorf("managementID = %q, want dev-1", summaries[0].ManagementID)
	}
	if summaries[0].DeviceType != "Computer" {
		t.Errorf("deviceType = %q, want Computer", summaries[0].DeviceType)
	}
	if summaries[0].Count != 3 {
		t.Errorf("count = %d, want 3", summaries[0].Count)
	}
}

func TestAggregateByDevice_Empty(t *testing.T) {
	summaries := aggregateByDevice(nil, 1, nil)
	if len(summaries) != 0 {
		t.Errorf("got %d summaries, want 0", len(summaries))
	}
}

func TestEnrichDeviceSummaries(t *testing.T) {
	summaries := []mdmDeviceSummary{
		{ManagementID: "mgmt-1", DeviceType: "Computer", Count: 10},
		{ManagementID: "mgmt-2", DeviceType: "Mobile", Count: 5},
	}
	lookup := map[string]mdmDeviceMeta{
		"mgmt-1": {name: "MacBook-A", serial: "C02X", osVersion: "15.3", username: "jsmith"},
		"mgmt-2": {name: "iPad-B", serial: "DMPQ", osVersion: "17.4"},
	}

	enriched := enrichDeviceSummaries(summaries, lookup)
	if enriched[0].Name != "MacBook-A" {
		t.Errorf("name = %q, want MacBook-A", enriched[0].Name)
	}
	if enriched[0].Serial != "C02X" {
		t.Errorf("serial = %q, want C02X", enriched[0].Serial)
	}
	if enriched[1].OSVersion != "17.4" {
		t.Errorf("osVersion = %q, want 17.4", enriched[1].OSVersion)
	}
}

// ---------------------------------------------------------------------------
// fetchAppNameLookup
// ---------------------------------------------------------------------------

func TestFetchAppNameLookup_Integration(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/JSSResource/mobiledeviceapplications": {200, `{
				"mobile_device_applications": [
					{"id": 10, "name": "Slack"},
					{"id": 20, "name": "Teams"}
				]
			}`},
			"/JSSResource/macapplications": {200, `{
				"mac_applications": [
					{"id": 30, "name": "Xcode"}
				]
			}`},
		},
	}

	lookup := fetchAppNameLookup(context.Background(), client)
	if lookup["10"] != "Slack" {
		t.Errorf("lookup[10] = %q, want Slack", lookup["10"])
	}
	if lookup["30"] != "Xcode" {
		t.Errorf("lookup[30] = %q, want Xcode", lookup["30"])
	}
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

func TestSplitCommaSeparated(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"INSTALL_PROFILE", 1},
		{"InstallApplication,InstallEnterpriseApplication", 2},
		{"", 0},
	}
	for _, tc := range tests {
		got := splitCommaSeparated(tc.input)
		if len(got) != tc.want {
			t.Errorf("splitCommaSeparated(%q) = %d parts, want %d", tc.input, len(got), tc.want)
		}
	}
}
