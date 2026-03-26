package commands

import (
	"context"
	"testing"
)

// ---------------------------------------------------------------------------
// patch-status
// ---------------------------------------------------------------------------

func TestRunReportPatchStatus_Basic(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v2/patch-software-title-configurations": {200, `{
				"totalCount": 2,
				"results": [
					{
						"id": "1",
						"softwareTitleName": "Google Chrome",
						"latestVersion": "123.0",
						"patchSummary": {
							"installedCount": 80,
							"totalCount": 100,
							"latestVersion": "123.0"
						}
					},
					{
						"id": "2",
						"softwareTitleName": "Firefox",
						"patchSummary": {
							"installedCount": 50,
							"totalCount": 50
						}
					}
				]
			}`},
		},
	}

	rows, err := runReportPatchStatus(context.Background(), client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}

	// Chrome row
	chrome := rows[0]
	if chrome["title"] != "Google Chrome" {
		t.Errorf("title = %q, want %q", chrome["title"], "Google Chrome")
	}
	if chrome["installed"] != 80 {
		t.Errorf("installed = %v, want 80", chrome["installed"])
	}
	if chrome["compliance_pct"] != "80.0%" {
		t.Errorf("compliance_pct = %q, want %q", chrome["compliance_pct"], "80.0%")
	}

	// Firefox row — 100% compliance
	ff := rows[1]
	if ff["compliance_pct"] != "100.0%" {
		t.Errorf("firefox compliance_pct = %q, want %q", ff["compliance_pct"], "100.0%")
	}
}

func TestRunReportPatchStatus_Empty(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v2/patch-software-title-configurations": {200, `{"totalCount":0,"results":[]}`},
		},
	}

	rows, err := runReportPatchStatus(context.Background(), client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("got %d rows, want 0", len(rows))
	}
}

func TestRunReportPatchStatus_NoTotal(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v2/patch-software-title-configurations": {200, `{
				"totalCount": 1,
				"results": [
					{
						"id": "1",
						"softwareTitleName": "Zoom",
						"patchSummary": {}
					}
				]
			}`},
		},
	}

	rows, err := runReportPatchStatus(context.Background(), client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0]["compliance_pct"] != "N/A" {
		t.Errorf("expected N/A for zero total, got %q", rows[0]["compliance_pct"])
	}
}

func TestRunReportPatchStatus_FetchError(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v2/patch-software-title-configurations": {500, `internal error`},
		},
	}

	_, err := runReportPatchStatus(context.Background(), client)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestRunReportPatchStatus_FallbackToDisplayName(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v2/patch-software-title-configurations": {200, `{
				"totalCount": 1,
				"results": [
					{
						"id": "99",
						"displayName": "MyApp",
						"patchSummary": {"installedCount": 5, "totalCount": 10}
					}
				]
			}`},
		},
	}

	rows, err := runReportPatchStatus(context.Background(), client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rows[0]["title"] != "MyApp" {
		t.Errorf("title = %q, want %q", rows[0]["title"], "MyApp")
	}
}

func TestRunReportPatchStatus_ArrayResponse(t *testing.T) {
	// Real /v2/patch-software-title-configurations returns a plain array, not paginated
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v2/patch-software-title-configurations": {200, `[
				{
					"id": "1",
					"softwareTitleName": "Google Chrome",
					"patchSummary": {"installedCount": 80, "totalCount": 100, "latestVersion": "123.0"}
				},
				{
					"id": "2",
					"displayName": "Slack",
					"patchSummary": {"installedCount": 0, "totalCount": 0}
				}
			]`},
		},
	}

	rows, err := runReportPatchStatus(context.Background(), client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	if rows[0]["title"] != "Google Chrome" {
		t.Errorf("row 0 title = %q, want Google Chrome", rows[0]["title"])
	}
	if rows[0]["compliance_pct"] != "80.0%" {
		t.Errorf("row 0 compliance = %q, want 80.0%%", rows[0]["compliance_pct"])
	}
	if rows[1]["title"] != "Slack" {
		t.Errorf("row 1 title = %q, want Slack (displayName fallback)", rows[1]["title"])
	}
}

// ---------------------------------------------------------------------------
// device-compliance
// ---------------------------------------------------------------------------

func TestRunReportDeviceCompliance_Basic(t *testing.T) {
	client := &paginatedMockClient{
		pages: map[string]string{
			"/v1/computers-inventory?section=GENERAL&section=HARDWARE&page=0&page-size=100": `{
				"totalCount": 2,
				"results": [
					{
						"id": "1",
						"general": {
							"name": "MacBook-001",
							"lastContactTime": "2026-01-01T00:00:00Z"
						},
						"hardware": {
							"serialNumber": "C02X1234"
						}
					},
					{
						"id": "2",
						"general": {
							"name": "MacBook-002",
							"lastContactTime": "2026-03-14T00:00:00Z"
						},
						"hardware": {
							"serialNumber": "C02Y5678"
						}
					}
				]
			}`,
		},
	}

	rows, err := runReportDeviceCompliance(context.Background(), client, 14)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}

	// MacBook-001 last checked in 2026-01-01 → stale relative to 2026-03-15
	row0 := rows[0]
	if row0["name"] != "MacBook-001" {
		t.Errorf("name = %q, want MacBook-001", row0["name"])
	}
	if row0["serial"] != "C02X1234" {
		t.Errorf("serial = %q, want C02X1234", row0["serial"])
	}
	if row0["stale"] != true {
		t.Errorf("stale = %v, want true (>14 days since 2026-01-01)", row0["stale"])
	}
}

func TestRunReportDeviceCompliance_Empty(t *testing.T) {
	client := &paginatedMockClient{
		pages: map[string]string{
			"/v1/computers-inventory?section=GENERAL&section=HARDWARE&page=0&page-size=100": `{"totalCount":0,"results":[]}`,
		},
	}

	rows, err := runReportDeviceCompliance(context.Background(), client, 14)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("got %d rows, want 0", len(rows))
	}
}

func TestRunReportDeviceCompliance_MissingGeneral(t *testing.T) {
	client := &paginatedMockClient{
		pages: map[string]string{
			"/v1/computers-inventory?section=GENERAL&section=HARDWARE&page=0&page-size=100": `{
				"totalCount": 1,
				"results": [{"id": "42"}]
			}`,
		},
	}

	rows, err := runReportDeviceCompliance(context.Background(), client, 14)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	// Should fall back to id as name.
	if rows[0]["name"] != "42" {
		t.Errorf("name = %q, want %q", rows[0]["name"], "42")
	}
	if rows[0]["days_since_contact"] != "N/A" {
		t.Errorf("days_since_contact = %q, want N/A", rows[0]["days_since_contact"])
	}
}

func TestRunReportDeviceCompliance_FetchError(t *testing.T) {
	// overviewMockClient strips query params, so /v1/computers-inventory
	// will match and return HTTP 500, triggering an error in FetchAllPaginated.
	errClient := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v1/computers-inventory": {500, `{}`},
		},
	}
	_, err := runReportDeviceCompliance(context.Background(), errClient, 14)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---------------------------------------------------------------------------
// inventory-summary
// ---------------------------------------------------------------------------

func TestRunReportInventorySummary_Basic(t *testing.T) {
	client := &paginatedMockClient{
		pages: map[string]string{
			"/v1/computers-inventory?section=HARDWARE&section=OPERATING_SYSTEM&page=0&page-size=100": `{
				"totalCount": 3,
				"results": [
					{
						"id": "1",
						"hardware": {"model": "MacBook Pro (16-inch, 2023)"},
						"operatingSystem": {"version": "14.4"}
					},
					{
						"id": "2",
						"hardware": {"model": "MacBook Pro (16-inch, 2023)"},
						"operatingSystem": {"version": "14.4"}
					},
					{
						"id": "3",
						"hardware": {"model": "Mac mini (2023)"},
						"operatingSystem": {"version": "14.3"}
					}
				]
			}`,
		},
	}

	rows, err := runReportInventorySummary(context.Background(), client, "", "both")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}

	// Mac mini row
	var miniRow map[string]interface{}
	for _, r := range rows {
		if r["model"] == "Mac mini (2023)" {
			miniRow = r
			break
		}
	}
	if miniRow == nil {
		t.Fatal("missing Mac mini row")
	}
	if miniRow["count"] != 1 {
		t.Errorf("Mac mini count = %v, want 1", miniRow["count"])
	}

	// MacBook Pro row
	var mbpRow map[string]interface{}
	for _, r := range rows {
		if r["model"] == "MacBook Pro (16-inch, 2023)" {
			mbpRow = r
			break
		}
	}
	if mbpRow == nil {
		t.Fatal("missing MacBook Pro row")
	}
	if mbpRow["count"] != 2 {
		t.Errorf("MacBook Pro count = %v, want 2", mbpRow["count"])
	}
}

func TestRunReportInventorySummary_UnknownModel(t *testing.T) {
	client := &paginatedMockClient{
		pages: map[string]string{
			"/v1/computers-inventory?section=HARDWARE&section=OPERATING_SYSTEM&page=0&page-size=100": `{
				"totalCount": 1,
				"results": [{"id": "1"}]
			}`,
		},
	}

	rows, err := runReportInventorySummary(context.Background(), client, "", "both")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0]["model"] != "Unknown" {
		t.Errorf("model = %q, want Unknown", rows[0]["model"])
	}
}

func TestRunReportInventorySummary_Empty(t *testing.T) {
	client := &paginatedMockClient{
		pages: map[string]string{
			"/v1/computers-inventory?section=HARDWARE&section=OPERATING_SYSTEM&page=0&page-size=100": `{"totalCount":0,"results":[]}`,
		},
	}

	rows, err := runReportInventorySummary(context.Background(), client, "", "both")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("got %d rows, want 0", len(rows))
	}
}

// ---------------------------------------------------------------------------
// software-installs
// ---------------------------------------------------------------------------

func TestRunReportSoftwareInstalls_Basic(t *testing.T) {
	client := &paginatedMockClient{
		pages: map[string]string{
			"/v1/computers-inventory?section=APPLICATIONS&page=0&page-size=100": `{
				"totalCount": 2,
				"results": [
					{
						"id": "1",
						"general": {"name": "MacBook-001"},
						"applications": [
							{"name": "Google Chrome", "version": "123.0"},
							{"name": "Firefox", "version": "124.0"}
						]
					},
					{
						"id": "2",
						"general": {"name": "MacBook-002"},
						"applications": [
							{"name": "Google Chrome", "version": "123.0"},
							{"name": "Google Chrome", "version": "122.0"}
						]
					}
				]
			}`,
		},
	}

	rows, err := runReportSoftwareInstalls(context.Background(), client, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expect 3 rows: Chrome 123.0, Chrome 122.0, Firefox 124.0
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}

	// Find Chrome 123.0 row
	var chrome123 map[string]interface{}
	for _, r := range rows {
		if r["title"] == "Google Chrome" && r["version"] == "123.0" {
			chrome123 = r
			break
		}
	}
	if chrome123 == nil {
		t.Fatal("missing Google Chrome 123.0 row")
	}
	if chrome123["device_count"] != 2 {
		t.Errorf("Chrome 123.0 device_count = %v, want 2", chrome123["device_count"])
	}
}

func TestRunReportSoftwareInstalls_TitleFilter(t *testing.T) {
	client := &paginatedMockClient{
		pages: map[string]string{
			"/v1/computers-inventory?section=APPLICATIONS&page=0&page-size=100": `{
				"totalCount": 1,
				"results": [
					{
						"id": "1",
						"applications": [
							{"name": "Google Chrome", "version": "123.0"},
							{"name": "Firefox", "version": "124.0"}
						]
					}
				]
			}`,
		},
	}

	rows, err := runReportSoftwareInstalls(context.Background(), client, "chrome")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0]["title"] != "Google Chrome" {
		t.Errorf("title = %q, want Google Chrome", rows[0]["title"])
	}
}

func TestRunReportSoftwareInstalls_NoMatchFilter(t *testing.T) {
	client := &paginatedMockClient{
		pages: map[string]string{
			"/v1/computers-inventory?section=APPLICATIONS&page=0&page-size=100": `{
				"totalCount": 1,
				"results": [
					{
						"id": "1",
						"applications": [{"name": "Firefox", "version": "124.0"}]
					}
				]
			}`,
		},
	}

	_, err := runReportSoftwareInstalls(context.Background(), client, "nonexistent-app-xyz")
	if err == nil {
		t.Fatal("expected error for no matches with filter, got nil")
	}
}

func TestRunReportSoftwareInstalls_Empty(t *testing.T) {
	client := &paginatedMockClient{
		pages: map[string]string{
			"/v1/computers-inventory?section=APPLICATIONS&page=0&page-size=100": `{"totalCount":0,"results":[]}`,
		},
	}

	rows, err := runReportSoftwareInstalls(context.Background(), client, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("got %d rows, want 0", len(rows))
	}
}

// ---------------------------------------------------------------------------
// ea-results
// ---------------------------------------------------------------------------

func TestRunReportEAResults_Basic(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/JSSResource/computerextensionattributes": {200, `{
				"computer_extension_attributes": [
					{"id": 1, "name": "Asset Tag"},
					{"id": 2, "name": "Department"}
				]
			}`},
			"/v1/computers-inventory": {200, `{
				"totalCount": 2,
				"results": [
					{
						"id": "10",
						"general": {"name": "Mac-A"},
						"extensionAttributes": [
							{"id": "1", "name": "Asset Tag", "value": "AA-001"},
							{"id": "2", "name": "Department", "value": "Engineering"}
						]
					},
					{
						"id": "11",
						"general": {"name": "Mac-B"},
						"extensionAttributes": [
							{"id": "1", "name": "Asset Tag", "value": "AA-002"}
						]
					}
				]
			}`},
		},
	}

	rows, err := runReportEAResults(context.Background(), client, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Mac-A: 2 EAs, Mac-B: 1 EA = 3 rows total
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}

	// Spot-check first row
	found := false
	for _, r := range rows {
		if r["device"] == "Mac-A" && r["ea_name"] == "Asset Tag" && r["value"] == "AA-001" {
			found = true
			break
		}
	}
	if !found {
		t.Error("missing expected row: Mac-A / Asset Tag / AA-001")
	}
}

func TestRunReportEAResults_NameFilter(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/JSSResource/computerextensionattributes": {200, `{
				"computer_extension_attributes": [
					{"id": 1, "name": "Asset Tag"},
					{"id": 2, "name": "Department"}
				]
			}`},
			"/v1/computers-inventory": {200, `{
				"totalCount": 1,
				"results": [
					{
						"id": "10",
						"general": {"name": "Mac-A"},
						"extensionAttributes": [
							{"id": "1", "name": "Asset Tag", "value": "AA-001"},
							{"id": "2", "name": "Department", "value": "Engineering"}
						]
					}
				]
			}`},
		},
	}

	rows, err := runReportEAResults(context.Background(), client, "asset")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only Asset Tag should be returned
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0]["ea_name"] != "Asset Tag" {
		t.Errorf("ea_name = %q, want Asset Tag", rows[0]["ea_name"])
	}
}

func TestRunReportEAResults_NoMatchFilter(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/JSSResource/computerextensionattributes": {200, `{
				"computer_extension_attributes": [
					{"id": 1, "name": "Asset Tag"}
				]
			}`},
		},
	}

	_, err := runReportEAResults(context.Background(), client, "nonexistent-ea-xyz")
	if err == nil {
		t.Fatal("expected error for no matching EAs, got nil")
	}
}

func TestRunReportEAResults_NoEAsConfigured(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/JSSResource/computerextensionattributes": {200, `{"computer_extension_attributes":[]}`},
		},
	}

	rows, err := runReportEAResults(context.Background(), client, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("got %d rows, want 0", len(rows))
	}
}

func TestRunReportEAResults_EAFetchError(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/JSSResource/computerextensionattributes": {403, `{}`},
		},
	}

	_, err := runReportEAResults(context.Background(), client, "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
