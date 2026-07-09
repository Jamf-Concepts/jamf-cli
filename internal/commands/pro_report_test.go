// Copyright 2026, Jamf Software LLC

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
					{"id": "1", "displayName": "Google Chrome"},
					{"id": "2", "displayName": "Firefox"}
				]
			}`},
			"/v2/patch-software-title-configurations/1/patch-summary": {200, `{
				"title": "Google Chrome", "latestVersion": "123.0", "upToDate": 80, "outOfDate": 20
			}`},
			"/v2/patch-software-title-configurations/2/patch-summary": {200, `{
				"title": "Firefox", "latestVersion": "124.0", "upToDate": 50, "outOfDate": 0
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
	if chrome["on_latest"] != 80 {
		t.Errorf("on_latest = %v, want 80", chrome["on_latest"])
	}
	if chrome["compliance_pct"] != "80%" {
		t.Errorf("compliance_pct = %q, want %q", chrome["compliance_pct"], "80%")
	}

	// Firefox row — 100% compliance
	ff := rows[1]
	if ff["compliance_pct"] != "100%" {
		t.Errorf("firefox compliance_pct = %q, want %q", ff["compliance_pct"], "100%")
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
				"results": [{"id": "1", "displayName": "Zoom"}]
			}`},
			"/v2/patch-software-title-configurations/1/patch-summary": {200, `{
				"title": "Zoom", "upToDate": 0, "outOfDate": 0
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
		return
	}
}

func TestRunReportPatchStatus_FallbackToDisplayName(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v2/patch-software-title-configurations": {200, `{
				"totalCount": 1,
				"results": [{"id": "99", "displayName": "MyApp"}]
			}`},
			"/v2/patch-software-title-configurations/99/patch-summary": {200, `{
				"title": "MyApp", "upToDate": 5, "outOfDate": 5
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
				{"id": "1", "displayName": "Google Chrome"},
				{"id": "2", "displayName": "Slack"}
			]`},
			"/v2/patch-software-title-configurations/1/patch-summary": {200, `{
				"title": "Google Chrome", "latestVersion": "123.0", "upToDate": 80, "outOfDate": 20
			}`},
			"/v2/patch-software-title-configurations/2/patch-summary": {200, `{
				"title": "Slack", "latestVersion": "4.0", "upToDate": 0, "outOfDate": 0
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
	if rows[0]["title"] != "Google Chrome" {
		t.Errorf("row 0 title = %q, want Google Chrome", rows[0]["title"])
	}
	if rows[0]["compliance_pct"] != "80%" {
		t.Errorf("row 0 compliance = %q, want 80%%", rows[0]["compliance_pct"])
	}
	if rows[1]["title"] != "Slack" {
		t.Errorf("row 1 title = %q, want Slack", rows[1]["title"])
	}
}

// ---------------------------------------------------------------------------
// device-compliance
// ---------------------------------------------------------------------------

func TestRunReportDeviceCompliance_Basic(t *testing.T) {
	client := &paginatedMockClient{
		pages: map[string]string{
			"/v3/computers-inventory?section=GENERAL&section=HARDWARE&section=OPERATING_SYSTEM&page=0&page-size=100": `{
				"totalCount": 2,
				"results": [
					{
						"id": "1",
						"general": {
							"name": "MacBook-001",
							"lastContactTime": "2026-01-01T00:00:00Z",
							"remoteManagement": {"managed": true}
						},
						"hardware": {
							"serialNumber": "C02X1234"
						},
						"operatingSystem": {
							"version": "14.4"
						}
					},
					{
						"id": "2",
						"general": {
							"name": "MacBook-002",
							"lastContactTime": "2026-03-14T00:00:00Z",
							"remoteManagement": {"managed": false}
						},
						"hardware": {
							"serialNumber": "C02Y5678"
						},
						"operatingSystem": {
							"version": "15.1"
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
	if row0["managed"] != true {
		t.Errorf("managed = %v, want true", row0["managed"])
	}
	if row0["os_version"] != "14.4" {
		t.Errorf("os_version = %q, want 14.4", row0["os_version"])
	}
	// Verify failed_commands was removed
	if _, hasFC := row0["failed_commands"]; hasFC {
		t.Error("failed_commands should not be present")
	}

	// MacBook-002 — unmanaged
	row1 := rows[1]
	if row1["managed"] != false {
		t.Errorf("row1 managed = %v, want false", row1["managed"])
	}
}

func TestRunReportDeviceCompliance_Empty(t *testing.T) {
	client := &paginatedMockClient{
		pages: map[string]string{
			"/v3/computers-inventory?section=GENERAL&section=HARDWARE&section=OPERATING_SYSTEM&page=0&page-size=100": `{"totalCount":0,"results":[]}`,
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
			"/v3/computers-inventory?section=GENERAL&section=HARDWARE&section=OPERATING_SYSTEM&page=0&page-size=100": `{
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
	// overviewMockClient strips query params, so /v3/computers-inventory
	// will match and return HTTP 500, triggering an error in FetchAllPaginated.
	errClient := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v3/computers-inventory": {500, `{}`},
		},
	}
	_, err := runReportDeviceCompliance(context.Background(), errClient, 14)
	if err == nil {
		t.Fatal("expected error, got nil")
		return
	}
}

// ---------------------------------------------------------------------------
// inventory-summary
// ---------------------------------------------------------------------------

func TestRunReportInventorySummary_Basic(t *testing.T) {
	client := &paginatedMockClient{
		pages: map[string]string{
			"/v3/computers-inventory?section=HARDWARE&section=OPERATING_SYSTEM&page=0&page-size=100": `{
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
	var miniRow map[string]any
	for _, r := range rows {
		if r["model"] == "Mac mini (2023)" {
			miniRow = r
			break
		}
	}
	if miniRow == nil {
		t.Fatal("missing Mac mini row")
		return
	}
	if miniRow["count"] != 1 {
		t.Errorf("Mac mini count = %v, want 1", miniRow["count"])
	}

	// MacBook Pro row
	var mbpRow map[string]any
	for _, r := range rows {
		if r["model"] == "MacBook Pro (16-inch, 2023)" {
			mbpRow = r
			break
		}
	}
	if mbpRow == nil {
		t.Fatal("missing MacBook Pro row")
		return
	}
	if mbpRow["count"] != 2 {
		t.Errorf("MacBook Pro count = %v, want 2", mbpRow["count"])
	}
}

func TestRunReportInventorySummary_UnknownModel(t *testing.T) {
	client := &paginatedMockClient{
		pages: map[string]string{
			"/v3/computers-inventory?section=HARDWARE&section=OPERATING_SYSTEM&page=0&page-size=100": `{
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
			"/v3/computers-inventory?section=HARDWARE&section=OPERATING_SYSTEM&page=0&page-size=100": `{"totalCount":0,"results":[]}`,
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
// duplicate-serials
// ---------------------------------------------------------------------------

func TestRunReportDuplicateSerials_Basic(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			// C02X1234 shared by ids 2 and 10 (logic-board swap); C02Z9999 unique;
			// two records with blank serials must not be treated as duplicates.
			"/v3/computers-inventory": {200, `{
				"totalCount": 5,
				"results": [
					{"id":"10","general":{"name":"Mac-new","lastContactTime":"2026-06-01T00:00:00Z"},"hardware":{"serialNumber":"C02X1234"}},
					{"id":"2","general":{"name":"Mac-old","lastContactTime":"2025-01-01T00:00:00Z"},"hardware":{"serialNumber":"C02X1234"}},
					{"id":"3","general":{"name":"Mac-unique"},"hardware":{"serialNumber":"C02Z9999"}},
					{"id":"4","general":{"name":"Pending-A"},"hardware":{"serialNumber":""}},
					{"id":"5","general":{"name":"Pending-B"},"hardware":{"serialNumber":"  "}}
				]
			}`},
		},
	}

	rows, err := runReportDuplicateSerials(context.Background(), client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only the two C02X1234 records; blank serials and the unique serial excluded.
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	// Grouped by serial, ordered by numeric ID → id 2 before id 10.
	if rows[0]["serial"] != "C02X1234" || rows[0]["id"] != "2" {
		t.Errorf("row0 = %v, want serial C02X1234 id 2", rows[0])
	}
	if rows[1]["id"] != "10" {
		t.Errorf("row1 id = %v, want 10 (numeric order, not lexical)", rows[1]["id"])
	}
	if rows[0]["name"] != "Mac-old" || rows[0]["last_contact"] != "2025-01-01T00:00:00Z" {
		t.Errorf("row0 detail = %v, want name Mac-old / last_contact 2025-01-01", rows[0])
	}
}

func TestRunReportDuplicateSerials_NoneWhenAllUnique(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v3/computers-inventory": {200, `{
				"totalCount": 2,
				"results": [
					{"id":"1","general":{"name":"A"},"hardware":{"serialNumber":"S1"}},
					{"id":"2","general":{"name":"B"},"hardware":{"serialNumber":"S2"}}
				]
			}`},
		},
	}

	rows, err := runReportDuplicateSerials(context.Background(), client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("got %d rows, want 0", len(rows))
	}
}

func TestRunReportDuplicateSerials_FetchError(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v3/computers-inventory": {500, `{}`},
		},
	}

	_, err := runReportDuplicateSerials(context.Background(), client)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---------------------------------------------------------------------------
// software-installs
// ---------------------------------------------------------------------------

func TestRunReportSoftwareInstalls_Basic(t *testing.T) {
	client := &paginatedMockClient{
		pages: map[string]string{
			"/v3/computers-inventory?section=APPLICATIONS&page=0&page-size=100": `{
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

	rows, err := runReportSoftwareInstalls(context.Background(), client, "", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expect 3 rows: Chrome 123.0, Chrome 122.0, Firefox 124.0
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}

	// Find Chrome 123.0 row
	var chrome123 map[string]any
	for _, r := range rows {
		if r["title"] == "Google Chrome" && r["version"] == "123.0" {
			chrome123 = r
			break
		}
	}
	if chrome123 == nil {
		t.Fatal("missing Google Chrome 123.0 row")
		return
	}
	if chrome123["device_count"] != 2 {
		t.Errorf("Chrome 123.0 device_count = %v, want 2", chrome123["device_count"])
	}
}

func TestRunReportSoftwareInstalls_TitleFilter(t *testing.T) {
	client := &paginatedMockClient{
		pages: map[string]string{
			"/v3/computers-inventory?section=APPLICATIONS&page=0&page-size=100": `{
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

	rows, err := runReportSoftwareInstalls(context.Background(), client, "chrome", true)
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
			"/v3/computers-inventory?section=APPLICATIONS&page=0&page-size=100": `{
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

	_, err := runReportSoftwareInstalls(context.Background(), client, "nonexistent-app-xyz", true)
	if err == nil {
		t.Fatal("expected error for no matches with filter, got nil")
		return
	}
}

func TestRunReportSoftwareInstalls_Empty(t *testing.T) {
	client := &paginatedMockClient{
		pages: map[string]string{
			"/v3/computers-inventory?section=APPLICATIONS&page=0&page-size=100": `{"totalCount":0,"results":[]}`,
		},
	}

	rows, err := runReportSoftwareInstalls(context.Background(), client, "", true)
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
			"/v3/computers-inventory": {200, `{
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

	rows, err := runReportEAResults(context.Background(), client, "", true)
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
			"/v3/computers-inventory": {200, `{
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

	rows, err := runReportEAResults(context.Background(), client, "asset", true)
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

	_, err := runReportEAResults(context.Background(), client, "nonexistent-ea-xyz", true)
	if err == nil {
		t.Fatal("expected error for no matching EAs, got nil")
		return
	}
}

func TestRunReportEAResults_NoEAsConfigured(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/JSSResource/computerextensionattributes": {200, `{"computer_extension_attributes":[]}`},
		},
	}

	rows, err := runReportEAResults(context.Background(), client, "", true)
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

	_, err := runReportEAResults(context.Background(), client, "", true)
	if err == nil {
		t.Fatal("expected error, got nil")
		return
	}
}

// ---------------------------------------------------------------------------
// security
// ---------------------------------------------------------------------------

func TestRunReportSecurity_Basic(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v3/computers-inventory": {200, `{
				"totalCount": 3,
				"results": [
					{
						"id": "1",
						"general": {"name": "Mac-A"},
						"hardware": {"serialNumber": "C02A"},
						"operatingSystem": {"version": "15.3"},
						"security": {
							"gatekeeperStatus": "ENABLED",
							"sipStatus": "ENABLED",
							"firewallEnabled": true
						},
						"diskEncryption": {
							"fileVault2Enabled": true,
							"bootPartitionEncryptionDetails": {"partitionFileVault2State": "ENCRYPTED"}
						}
					},
					{
						"id": "2",
						"general": {"name": "Mac-B"},
						"hardware": {"serialNumber": "C02B"},
						"operatingSystem": {"version": "14.1"},
						"security": {
							"gatekeeperStatus": "DISABLED",
							"sipStatus": "ENABLED",
							"firewallEnabled": false
						},
						"diskEncryption": {
							"fileVault2Enabled": false,
							"bootPartitionEncryptionDetails": {"partitionFileVault2State": "UNENCRYPTED"}
						}
					},
					{
						"id": "3",
						"general": {"name": "Mac-C"},
						"hardware": {"serialNumber": "C02C"},
						"operatingSystem": {"version": "15.3"},
						"security": {
							"gatekeeperStatus": "ENABLED",
							"sipStatus": "DISABLED",
							"firewallEnabled": true
						},
						"diskEncryption": {
							"fileVault2Enabled": true,
							"bootPartitionEncryptionDetails": {"partitionFileVault2State": "ENCRYPTED"}
						}
					}
				]
			}`},
		},
	}

	result, err := runReportSecurity(context.Background(), client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have both summary and per-device rows
	if result.Summary == nil {
		t.Fatal("expected summary section")
		return
	}
	if len(result.Devices) != 3 {
		t.Fatalf("got %d device rows, want 3", len(result.Devices))
	}

	// Check summary
	if result.Summary["filevault_encrypted_pct"] != "66.7%" {
		t.Errorf("filevault pct = %q, want 66.7%%", result.Summary["filevault_encrypted_pct"])
	}
	if result.Summary["gatekeeper_enabled_pct"] != "66.7%" {
		t.Errorf("gatekeeper pct = %q, want 66.7%%", result.Summary["gatekeeper_enabled_pct"])
	}
	if result.Summary["sip_enabled_pct"] != "66.7%" {
		t.Errorf("sip pct = %q, want 66.7%%", result.Summary["sip_enabled_pct"])
	}
	if result.Summary["firewall_enabled_pct"] != "66.7%" {
		t.Errorf("firewall pct = %q, want 66.7%%", result.Summary["firewall_enabled_pct"])
	}

	// Check that Mac-B is flagged with UNENCRYPTED status
	var macB map[string]any
	for _, d := range result.Devices {
		if d["name"] == "Mac-B" {
			macB = d
			break
		}
	}
	if macB == nil {
		t.Fatal("missing Mac-B row")
		return
	}
	if macB["filevault"] != "UNENCRYPTED" {
		t.Errorf("Mac-B filevault = %q, want UNENCRYPTED", macB["filevault"])
	}
}

func TestRunReportSecurity_Empty(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v3/computers-inventory": {200, `{"totalCount":0,"results":[]}`},
		},
	}

	result, err := runReportSecurity(context.Background(), client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Devices) != 0 {
		t.Errorf("got %d rows, want 0", len(result.Devices))
	}
}

func TestRunReportSecurity_FetchError(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v3/computers-inventory": {500, `{}`},
		},
	}

	_, err := runReportSecurity(context.Background(), client)
	if err == nil {
		t.Fatal("expected error, got nil")
		return
	}
}

func TestRunReportSecurity_OSDistribution(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v3/computers-inventory": {200, `{
				"totalCount": 3,
				"results": [
					{"id":"1","general":{"name":"A"},"hardware":{"serialNumber":"S1"},"operatingSystem":{"version":"15.3"},"security":{"gatekeeperStatus":"ENABLED","sipStatus":"ENABLED","firewallEnabled":true},"diskEncryption":{"bootPartitionEncryptionDetails":{"partitionFileVault2State":"ENCRYPTED"}}},
					{"id":"2","general":{"name":"B"},"hardware":{"serialNumber":"S2"},"operatingSystem":{"version":"15.3"},"security":{"gatekeeperStatus":"ENABLED","sipStatus":"ENABLED","firewallEnabled":true},"diskEncryption":{"bootPartitionEncryptionDetails":{"partitionFileVault2State":"ENCRYPTED"}}},
					{"id":"3","general":{"name":"C"},"hardware":{"serialNumber":"S3"},"operatingSystem":{"version":"14.1"},"security":{"gatekeeperStatus":"ENABLED","sipStatus":"ENABLED","firewallEnabled":true},"diskEncryption":{"bootPartitionEncryptionDetails":{"partitionFileVault2State":"ENCRYPTED"}}}
				]
			}`},
		},
	}

	result, err := runReportSecurity(context.Background(), client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.OSVersions) != 2 {
		t.Fatalf("got %d OS versions, want 2", len(result.OSVersions))
	}
}
