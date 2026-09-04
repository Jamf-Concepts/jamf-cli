// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
)

// ---------------------------------------------------------------------------
// patch-status
// ---------------------------------------------------------------------------

func TestRunReportPatchStatus_Basic(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v3/patch-software-title-configurations": {200, `{
				"totalCount": 2,
				"results": [
					{"id": "1", "displayName": "Google Chrome"},
					{"id": "2", "displayName": "Firefox"}
				]
			}`},
			"/v3/patch-software-title-configurations/1/patch-summary": {200, `{
				"title": "Google Chrome", "latestVersion": "123.0", "upToDate": 80, "outOfDate": 20
			}`},
			"/v3/patch-software-title-configurations/2/patch-summary": {200, `{
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
			"/v3/patch-software-title-configurations": {200, `{"totalCount":0,"results":[]}`},
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
			"/v3/patch-software-title-configurations": {200, `{
				"totalCount": 1,
				"results": [{"id": "1", "displayName": "Zoom"}]
			}`},
			"/v3/patch-software-title-configurations/1/patch-summary": {200, `{
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
			"/v3/patch-software-title-configurations": {500, `internal error`},
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
			"/v3/patch-software-title-configurations": {200, `{
				"totalCount": 1,
				"results": [{"id": "99", "displayName": "MyApp"}]
			}`},
			"/v3/patch-software-title-configurations/99/patch-summary": {200, `{
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
	// Real /v3/patch-software-title-configurations returns a plain array, not paginated
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v3/patch-software-title-configurations": {200, `[
				{"id": "1", "displayName": "Google Chrome"},
				{"id": "2", "displayName": "Slack"}
			]`},
			"/v3/patch-software-title-configurations/1/patch-summary": {200, `{
				"title": "Google Chrome", "latestVersion": "123.0", "upToDate": 80, "outOfDate": 20
			}`},
			"/v3/patch-software-title-configurations/2/patch-summary": {200, `{
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
			"/v4/computers-inventory?section=GENERAL&section=HARDWARE&section=OPERATING_SYSTEM&page=0&page-size=100": `{
				"totalCount": 2,
				"results": [
					{
						"id": "1",
						"general": {
							"name": "MacBook-001",
							"lastCheckIn": "2026-01-01T00:00:00Z",
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
							"lastCheckIn": "2026-03-14T00:00:00Z",
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
	// days_since_contact must be computed, not "N/A". A reader asking for a
	// field the v4 response does not carry leaves it "N/A" on every row with
	// stale=false, which reads as a healthy fleet — so assert the value, not
	// just the flag.
	if row0["days_since_contact"] == "N/A" {
		t.Errorf("days_since_contact = %v, want a computed value", row0["days_since_contact"])
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
			"/v4/computers-inventory?section=GENERAL&section=HARDWARE&section=OPERATING_SYSTEM&page=0&page-size=100": `{"totalCount":0,"results":[]}`,
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
			"/v4/computers-inventory?section=GENERAL&section=HARDWARE&section=OPERATING_SYSTEM&page=0&page-size=100": `{
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
	// overviewMockClient strips query params, so /v4/computers-inventory
	// will match and return HTTP 500, triggering an error in FetchAllPaginated.
	errClient := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v4/computers-inventory": {500, `{}`},
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
			"/v4/computers-inventory?section=HARDWARE&section=OPERATING_SYSTEM&page=0&page-size=100": `{
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
			"/v4/computers-inventory?section=HARDWARE&section=OPERATING_SYSTEM&page=0&page-size=100": `{
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
			"/v4/computers-inventory?section=HARDWARE&section=OPERATING_SYSTEM&page=0&page-size=100": `{"totalCount":0,"results":[]}`,
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
			"/v4/computers-inventory": {200, `{
				"totalCount": 5,
				"results": [
					{"id":"10","general":{"name":"Mac-new","lastCheckIn":"2026-06-01T00:00:00Z"},"hardware":{"serialNumber":"C02X1234"}},
					{"id":"2","general":{"name":"Mac-old","lastCheckIn":"2025-01-01T00:00:00Z"},"hardware":{"serialNumber":"C02X1234"}},
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
			"/v4/computers-inventory": {200, `{
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
			"/v4/computers-inventory": {500, `{}`},
		},
	}

	_, err := runReportDuplicateSerials(context.Background(), client)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// Serials that differ only in surrounding whitespace are the same serial and
// must collide (guards the strings.TrimSpace in the grouping key).
func TestRunReportDuplicateSerials_WhitespaceCollision(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v4/computers-inventory": {200, `{
				"totalCount": 2,
				"results": [
					{"id":"1","general":{"name":"A"},"hardware":{"serialNumber":"C02X1234"}},
					{"id":"2","general":{"name":"B"},"hardware":{"serialNumber":"  C02X1234  "}}
				]
			}`},
		},
	}

	rows, err := runReportDuplicateSerials(context.Background(), client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2 (whitespace-variant serials must collide)", len(rows))
	}
	if rows[0]["serial"] != "C02X1234" || rows[1]["serial"] != "C02X1234" {
		t.Errorf("serials = %q/%q, want both trimmed to C02X1234", rows[0]["serial"], rows[1]["serial"])
	}
}

func TestIDLess(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"2", "10", true},    // numeric: 2 < 10 (not lexical)
		{"10", "2", false},   // numeric reverse
		{"abc", "abd", true}, // non-numeric fallback: lexical
		{"b", "a", false},    // non-numeric fallback reverse
		{"9", "x", true},     // mixed → lexical ("9" < "x")
	}
	for _, c := range cases {
		if got := idLess(c.a, c.b); got != c.want {
			t.Errorf("idLess(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// software-installs
// ---------------------------------------------------------------------------

func TestRunReportSoftwareInstalls_Basic(t *testing.T) {
	client := &paginatedMockClient{
		pages: map[string]string{
			"/v4/computers-inventory?section=APPLICATIONS&page=0&page-size=100": `{
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

	rows, err := runReportSoftwareInstalls(context.Background(), client, "", true, false, false)
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
			"/v4/computers-inventory?section=APPLICATIONS&page=0&page-size=100": `{
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

	rows, err := runReportSoftwareInstalls(context.Background(), client, "chrome", true, false, false)
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
			"/v4/computers-inventory?section=APPLICATIONS&page=0&page-size=100": `{
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

	_, err := runReportSoftwareInstalls(context.Background(), client, "nonexistent-app-xyz", true, false, false)
	if err == nil {
		t.Fatal("expected error for no matches with filter, got nil")
		return
	}
}

func TestRunReportSoftwareInstalls_Empty(t *testing.T) {
	client := &paginatedMockClient{
		pages: map[string]string{
			"/v4/computers-inventory?section=APPLICATIONS&page=0&page-size=100": `{"totalCount":0,"results":[]}`,
		},
	}

	rows, err := runReportSoftwareInstalls(context.Background(), client, "", true, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("got %d rows, want 0", len(rows))
	}
}

// --bundle-id must extend the grouping key, not merely add a column: these two
// installs share a title and version, so a display-only column would collapse
// them into one row and hide a bundle ID.
func TestRunReportSoftwareInstalls_BundleIDExtendsTheGroupingKey(t *testing.T) {
	client := &paginatedMockClient{
		pages: map[string]string{
			"/v4/computers-inventory?section=APPLICATIONS&page=0&page-size=100": `{
				"totalCount": 3,
				"results": [
					{
						"id": "1",
						"applications": [
							{"name": "Zoom", "version": "5.0", "bundleId": "us.zoom.xos", "path": "/Applications/zoom.us.app"}
						]
					},
					{
						"id": "2",
						"applications": [
							{"name": "Zoom", "version": "5.0", "bundleId": "us.zoom.xos", "path": "/Applications/zoom.us.app"}
						]
					},
					{
						"id": "3",
						"applications": [
							{"name": "Zoom", "version": "5.0", "bundleId": "com.example.zoom-repack", "path": "/Applications/zoom.us.app"}
						]
					}
				]
			}`,
		},
	}

	rows, err := runReportSoftwareInstalls(context.Background(), client, "", true, true, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2 (one per bundle ID)", len(rows))
	}

	counts := map[string]any{}
	for _, r := range rows {
		bundleID, ok := r["bundle_id"].(string)
		if !ok {
			t.Fatalf("row %v carries no bundle_id", r)
		}
		counts[bundleID] = r["device_count"]
	}
	if counts["us.zoom.xos"] != 2 {
		t.Errorf("us.zoom.xos device_count = %v, want 2", counts["us.zoom.xos"])
	}
	if counts["com.example.zoom-repack"] != 1 {
		t.Errorf("com.example.zoom-repack device_count = %v, want 1", counts["com.example.zoom-repack"])
	}
}

// Same shape as the bundle-ID case, for --path: one title and version installed
// at two different paths must not merge.
func TestRunReportSoftwareInstalls_PathExtendsTheGroupingKey(t *testing.T) {
	client := &paginatedMockClient{
		pages: map[string]string{
			"/v4/computers-inventory?section=APPLICATIONS&page=0&page-size=100": `{
				"totalCount": 3,
				"results": [
					{
						"id": "1",
						"applications": [
							{"name": "Slack", "version": "4.0", "path": "/Applications/Slack.app"}
						]
					},
					{
						"id": "2",
						"applications": [
							{"name": "Slack", "version": "4.0", "path": "/Applications/Slack.app"}
						]
					},
					{
						"id": "3",
						"applications": [
							{"name": "Slack", "version": "4.0", "path": "/Users/bob/Applications/Slack.app"}
						]
					}
				]
			}`,
		},
	}

	rows, err := runReportSoftwareInstalls(context.Background(), client, "", true, false, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2 (one per install path)", len(rows))
	}

	counts := map[string]any{}
	for _, r := range rows {
		path, ok := r["path"].(string)
		if !ok {
			t.Fatalf("row %v carries no path", r)
		}
		counts[path] = r["device_count"]
	}
	if counts["/Applications/Slack.app"] != 2 {
		t.Errorf("/Applications/Slack.app device_count = %v, want 2", counts["/Applications/Slack.app"])
	}
	if counts["/Users/bob/Applications/Slack.app"] != 1 {
		t.Errorf("/Users/bob/Applications/Slack.app device_count = %v, want 1", counts["/Users/bob/Applications/Slack.app"])
	}
}

// With both flags off the row map must carry exactly the three original keys —
// an extra key would change the column set of every existing user's table.
func TestRunReportSoftwareInstalls_DefaultRowsCarryOnlyTheThreeOriginalKeys(t *testing.T) {
	client := &paginatedMockClient{
		pages: map[string]string{
			"/v4/computers-inventory?section=APPLICATIONS&page=0&page-size=100": `{
				"totalCount": 1,
				"results": [
					{
						"id": "1",
						"applications": [
							{"name": "Zoom", "version": "5.0", "bundleId": "us.zoom.xos", "path": "/Applications/zoom.us.app"},
							{"name": "Slack", "version": "4.0", "bundleId": "com.tinyspeck.slackmacgap", "path": "/Applications/Slack.app"}
						]
					}
				]
			}`,
		},
	}

	rows, err := runReportSoftwareInstalls(context.Background(), client, "", true, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	for _, r := range rows {
		if len(r) != 3 {
			t.Errorf("row %v has %d keys, want 3", r, len(r))
		}
		if _, ok := r["bundle_id"]; ok {
			t.Errorf("row %v carries bundle_id with --bundle-id off", r)
		}
		if _, ok := r["path"]; ok {
			t.Errorf("row %v carries path with --path off", r)
		}
		for _, want := range []string{"title", "version", "device_count"} {
			if _, ok := r[want]; !ok {
				t.Errorf("row %v is missing key %q", r, want)
			}
		}
	}
}

// Both flags on: the two fields are independent parts of the key, so one title
// and version spanning two bundle IDs and two paths reports a row per pair.
func TestRunReportSoftwareInstalls_BundleIDAndPathTogether(t *testing.T) {
	client := &paginatedMockClient{
		pages: map[string]string{
			"/v4/computers-inventory?section=APPLICATIONS&page=0&page-size=100": `{
				"totalCount": 3,
				"results": [
					{
						"id": "1",
						"applications": [
							{"name": "Zoom", "version": "5.0", "bundleId": "us.zoom.xos", "path": "/Applications/zoom.us.app"}
						]
					},
					{
						"id": "2",
						"applications": [
							{"name": "Zoom", "version": "5.0", "bundleId": "us.zoom.xos", "path": "/Users/bob/Applications/zoom.us.app"}
						]
					},
					{
						"id": "3",
						"applications": [
							{"name": "Zoom", "version": "5.0", "bundleId": "com.example.zoom-repack", "path": "/Applications/zoom.us.app"}
						]
					}
				]
			}`,
		},
	}

	rows, err := runReportSoftwareInstalls(context.Background(), client, "", true, true, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3 (one per bundle ID + path pair)", len(rows))
	}
	for _, r := range rows {
		if len(r) != 5 {
			t.Errorf("row %v has %d keys, want 5", r, len(r))
		}
		for _, want := range []string{"title", "version", "device_count", "bundle_id", "path"} {
			if _, ok := r[want]; !ok {
				t.Errorf("row %v is missing key %q", r, want)
			}
		}
		if r["device_count"] != 1 {
			t.Errorf("row %v device_count = %v, want 1", r, r["device_count"])
		}
	}
}

// --bundle-id and --path are boolean, so `--path /Applications/Foo.app` leaves
// the value as a positional. Before Args was set the command discarded it and
// ran the whole report, so the operator got fleet-wide output for what they had
// typed as a filter. Cobra validates Args ahead of PersistentPreRunE, so the
// refusal lands before any credential is resolved or request sent.
func TestReportSoftwareInstalls_StrayPositionalIsRefused(t *testing.T) {
	root := NewRootCmd("test", "none", "none", "none")
	root.SetArgs([]string{"pro", "report", "software-installs", "junkarg"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)

	err := ClassifyError(root.Execute())
	if err == nil {
		t.Fatal("expected an error for a stray positional, got nil")
	}
	if code := exitcode.CodeFrom(err); code != exitcode.Usage {
		t.Errorf("exit code = %d, want %d (usage)", code, exitcode.Usage)
	}
	if !strings.Contains(err.Error(), `unknown command "junkarg"`) {
		t.Errorf("error should name the stray argument, got %q", err.Error())
	}
	if strings.Contains(err.Error(), "fetching computer inventory") {
		t.Errorf("the report ran despite the stray positional: %q", err.Error())
	}
}

// The comparator has four levels and nothing asserted row order at all, so a
// control mutation flipping even the original title comparison left every test
// green. This fixture puts a tie at each level in turn: two titles, two
// versions inside one title, two bundle IDs inside one title and version, and
// two paths inside one title, version and bundle ID. Both flags are on because
// the path level is unreachable otherwise — with --path off every key holds the
// same empty path, so any two keys reaching that comparison are the same key.
func TestRunReportSoftwareInstalls_RowOrderFollowsEveryComparatorLevel(t *testing.T) {
	client := &paginatedMockClient{
		pages: map[string]string{
			"/v4/computers-inventory?section=APPLICATIONS&page=0&page-size=100": `{
				"totalCount": 1,
				"results": [
					{
						"id": "1",
						"applications": [
							{"name": "Zulu", "version": "1.0", "bundleId": "com.z.one", "path": "/Applications/Z1.app"},
							{"name": "Alpha", "version": "1.0", "bundleId": "com.a.one", "path": "/Applications/A1.app"},
							{"name": "Alpha", "version": "2.0", "bundleId": "com.b.two", "path": "/Applications/A1.app"},
							{"name": "Alpha", "version": "2.0", "bundleId": "com.a.one", "path": "/Applications/A2.app"},
							{"name": "Alpha", "version": "2.0", "bundleId": "com.a.one", "path": "/Applications/A1.app"}
						]
					}
				]
			}`,
		},
	}

	rows, err := runReportSoftwareInstalls(context.Background(), client, "", true, true, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := [][4]string{
		{"Alpha", "2.0", "com.a.one", "/Applications/A1.app"},
		{"Alpha", "2.0", "com.a.one", "/Applications/A2.app"},
		{"Alpha", "2.0", "com.b.two", "/Applications/A1.app"},
		{"Alpha", "1.0", "com.a.one", "/Applications/A1.app"},
		{"Zulu", "1.0", "com.z.one", "/Applications/Z1.app"},
	}
	if len(rows) != len(want) {
		t.Fatalf("got %d rows, want %d", len(rows), len(want))
	}
	for i, w := range want {
		got := [4]string{
			rows[i]["title"].(string),
			rows[i]["version"].(string),
			rows[i]["bundle_id"].(string),
			rows[i]["path"].(string),
		}
		if got != w {
			t.Errorf("row %d = %v, want %v", i, got, w)
		}
	}
}

// The row builder gates bundle_id on the flag, never on whether the value is
// empty, so an application the wire reports with no bundleId still gets a row
// and still carries the column. Gating on emptiness instead would drop it and
// take the column with it, since a table's columns are the keys of its first
// row.
func TestRunReportSoftwareInstalls_AppWithNoBundleIDKeepsTheKeyWithAnEmptyValue(t *testing.T) {
	client := &paginatedMockClient{
		pages: map[string]string{
			"/v4/computers-inventory?section=APPLICATIONS&page=0&page-size=100": `{
				"totalCount": 3,
				"results": [
					{
						"id": "1",
						"applications": [
							{"name": "Zoom", "version": "5.0", "bundleId": "us.zoom.xos", "path": "/Applications/zoom.us.app"}
						]
					},
					{
						"id": "2",
						"applications": [
							{"name": "Zoom", "version": "5.0", "path": "/Applications/zoom.us.app"}
						]
					},
					{
						"id": "3",
						"applications": [
							{"name": "Zoom", "version": "5.0", "path": "/Applications/zoom.us.app"}
						]
					}
				]
			}`,
		},
	}

	rows, err := runReportSoftwareInstalls(context.Background(), client, "", true, true, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2 (the app with no bundleId keeps its own row)", len(rows))
	}

	var nameless map[string]any
	for _, r := range rows {
		v, ok := r["bundle_id"]
		if !ok {
			t.Fatalf("row %v carries no bundle_id key", r)
		}
		if v == "" {
			nameless = r
		}
	}
	if nameless == nil {
		t.Fatalf("no row for the app with no bundleId, got %v", rows)
	}
	if nameless["device_count"] != 2 {
		t.Errorf("empty bundle_id device_count = %v, want 2", nameless["device_count"])
	}
}

// The grouping and row-building halves are the same shape as the bundle-ID
// case, but an empty path has one consumer a missing bundleId has none of:
// isSystemApp("") is false, so a path-less application is never filtered as a
// system app and reaches the report even in the default output. This pins that
// combination — includeSystem off, --path on, a row with an empty path.
func TestRunReportSoftwareInstalls_AppWithNoPathSurvivesTheDefaultSystemFilter(t *testing.T) {
	client := &paginatedMockClient{
		pages: map[string]string{
			"/v4/computers-inventory?section=APPLICATIONS&page=0&page-size=100": `{
				"totalCount": 2,
				"results": [
					{
						"id": "1",
						"applications": [
							{"name": "Slack", "version": "4.0", "path": "/Applications/Slack.app"},
							{"name": "Slack", "version": "4.0"}
						]
					},
					{
						"id": "2",
						"applications": [
							{"name": "Slack", "version": "4.0"}
						]
					}
				]
			}`,
		},
	}

	rows, err := runReportSoftwareInstalls(context.Background(), client, "", false, false, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2 (the app with no path keeps its own row)", len(rows))
	}

	var pathless map[string]any
	for _, r := range rows {
		v, ok := r["path"]
		if !ok {
			t.Fatalf("row %v carries no path key", r)
		}
		if v == "" {
			pathless = r
		}
	}
	if pathless == nil {
		t.Fatalf("no row for the app with no path, got %v", rows)
	}
	if pathless["device_count"] != 2 {
		t.Errorf("empty path device_count = %v, want 2", pathless["device_count"])
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
			"/v4/computers-inventory": {200, `{
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
			"/v4/computers-inventory": {200, `{
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
			"/v4/computers-inventory": {200, `{
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
			"/v4/computers-inventory": {200, `{"totalCount":0,"results":[]}`},
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
			"/v4/computers-inventory": {500, `{}`},
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
			"/v4/computers-inventory": {200, `{
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
