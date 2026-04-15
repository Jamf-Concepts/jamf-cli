// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"
)

// ---------------------------------------------------------------------------
// runReportPatchPolicyFailures
// ---------------------------------------------------------------------------

func TestRunReportPatchPolicyFailures_WithFailures(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v2/patch-policies": {200, `{
				"totalCount": 2,
				"results": [
					{"id": "10", "name": "Chrome Update", "failed": 5, "completed": 90, "pending": 3, "deferred": 2},
					{"id": "20", "name": "Firefox Update", "failed": 0, "completed": 50, "pending": 0, "deferred": 0}
				]
			}`},
		},
	}

	rows, err := runReportPatchPolicyFailures(context.Background(), client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only Chrome should appear (Firefox has 0 failures)
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0]["policy"] != "Chrome Update" {
		t.Errorf("policy = %q, want Chrome Update", rows[0]["policy"])
	}
	if rows[0]["failed"] != 5 {
		t.Errorf("failed = %v, want 5", rows[0]["failed"])
	}
}

func TestRunReportPatchPolicyFailures_NoFailures(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v2/patch-policies": {200, `{
				"totalCount": 1,
				"results": [
					{"id": "10", "name": "All Good", "failed": 0, "completed": 100, "pending": 0, "deferred": 0}
				]
			}`},
		},
	}

	rows, err := runReportPatchPolicyFailures(context.Background(), client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("got %d rows, want 0", len(rows))
	}
}

func TestRunReportPatchPolicyFailures_FetchError(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v2/patch-policies": {500, `{}`},
		},
	}

	_, err := runReportPatchPolicyFailures(context.Background(), client)
	if err == nil {
		t.Fatal("expected error, got nil")
		return
	}
}

// ---------------------------------------------------------------------------
// fetchPatchDeviceFailures
// ---------------------------------------------------------------------------

func TestFetchPatchDeviceFailures_Basic(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v2/patch-policies/10/logs": {200, `{
				"totalCount": 3,
				"results": [
					{"deviceName": "Mac-A", "deviceId": "100", "statusDate": "2026-04-01", "attemptNumber": 1, "statusEnum": "FAILED"},
					{"deviceName": "Mac-B", "deviceId": "200", "statusDate": "2026-04-02", "attemptNumber": 2, "statusEnum": "FAILED"},
					{"deviceName": "Mac-C", "deviceId": "300", "statusDate": "2026-04-03", "attemptNumber": 1, "statusEnum": "COMPLETED"}
				]
			}`},
			"/v2/patch-policies/10/logs/100/details": {200, `[
				{"attemptNumber": 1, "deviceId": "100", "actions": [
					{"actionOrder": 1, "action": "Downloading..."},
					{"actionOrder": 2, "action": "Install failed: exit code 1"}
				]}
			]`},
			"/v2/patch-policies/10/logs/200/details": {200, `[
				{"attemptNumber": 1, "deviceId": "200", "actions": [
					{"actionOrder": 1, "action": "Downloading..."}
				]},
				{"attemptNumber": 2, "deviceId": "200", "actions": [
					{"actionOrder": 1, "action": "Downloading..."},
					{"actionOrder": 2, "action": "Installing..."},
					{"actionOrder": 3, "action": "Reboot required"}
				]}
			]`},
		},
	}

	policyRows := []map[string]any{
		{"policy": "Chrome Update", "policy_id": "10"},
	}

	rows, err := fetchPatchDeviceFailures(context.Background(), client, policyRows)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	if rows[0]["device"] != "Mac-A" {
		t.Errorf("device = %q, want Mac-A", rows[0]["device"])
	}
	if rows[0]["policy"] != "Chrome Update" {
		t.Errorf("policy = %q, want Chrome Update", rows[0]["policy"])
	}
	// last_action: last action from the only attempt for Mac-A
	if rows[0]["last_action"] != "Install failed: exit code 1" {
		t.Errorf("last_action = %q, want Install failed: exit code 1", rows[0]["last_action"])
	}
	// last_action: last action from attempt 2 (highest attemptNumber) for Mac-B
	if rows[1]["last_action"] != "Reboot required" {
		t.Errorf("last_action = %q, want Reboot required", rows[1]["last_action"])
	}
}

func TestFetchPatchDeviceFailures_NoDetails(t *testing.T) {
	// Details endpoint unavailable — last_action should be empty, not an error.
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v2/patch-policies/10/logs": {200, `{
				"totalCount": 1,
				"results": [
					{"deviceName": "Mac-A", "deviceId": "100", "statusDate": "2026-04-01", "attemptNumber": 1, "statusEnum": "FAILED"}
				]
			}`},
			"/v2/patch-policies/10/logs/100/details": {404, `{}`},
		},
	}

	rows, err := fetchPatchDeviceFailures(context.Background(), client, []map[string]any{
		{"policy": "Chrome Update", "policy_id": "10"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0]["last_action"] != "" {
		t.Errorf("last_action = %q, want empty when details unavailable", rows[0]["last_action"])
	}
}

// ---------------------------------------------------------------------------
// runReportUpdateStatus
// ---------------------------------------------------------------------------

func TestRunReportUpdateStatus_WithErrors(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v1/managed-software-updates/update-statuses": {200, `{
				"totalCount": 3,
				"results": [
					{"status": "IDLE", "device": {"deviceId": "1", "objectType": "COMPUTER"}},
					{"status": "ERROR", "device": {"deviceId": "2", "objectType": "COMPUTER"}, "productKey": "macOS15", "updated": "2026-04-01"},
					{"status": "INSTALL_FAILED", "device": {"deviceId": "3", "objectType": "MOBILE_DEVICE"}, "productKey": "iOS18", "updated": "2026-04-02"}
				]
			}`},
			"/v3/computers-inventory": {200, `{
				"totalCount": 2,
				"results": [
					{"id": "1", "general": {"name": "Mac-Good"}, "hardware": {"serialNumber": "S1"}, "operatingSystem": {"version": "15.0"}, "userAndLocation": {"username": "alice"}},
					{"id": "2", "general": {"name": "Mac-Bad"}, "hardware": {"serialNumber": "S2"}, "operatingSystem": {"version": "14.5"}, "userAndLocation": {"username": "bob"}}
				]
			}`},
			"/v2/mobile-devices": {200, `[
				{"id": "3", "name": "iPad-Fail", "serialNumber": "M1", "managementId": "mgmt-3"}
			]`},
			"/v2/mobile-devices/detail": {200, `{
				"totalCount": 1,
				"results": [
					{"mobileDeviceId": 3, "general": {"managementId": "mgmt-3", "displayName": "iPad-Fail", "osVersion": "18.0"}}
				]
			}`},
		},
	}

	err := runReportUpdateStatus(context.Background(), client, true, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunReportUpdateStatus_StaleDevicesDropped(t *testing.T) {
	// Device 999 does not exist in inventory — its error and plan rows
	// should be silently dropped rather than showing blank fields.
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v1/managed-software-updates/update-statuses": {200, `{
				"totalCount": 2,
				"results": [
					{"status": "ERROR", "device": {"deviceId": "2", "objectType": "COMPUTER"}, "productKey": "macOS15", "updated": "2026-04-01"},
					{"status": "INSTALL_FAILED", "device": {"deviceId": "999", "objectType": "APPLE_TV"}, "productKey": "tvOS18", "updated": "2026-04-02"}
				]
			}`},
			"/v1/managed-software-updates/plans": {200, `{
				"totalCount": 2,
				"results": [
					{"planUuid": "aaa", "device": {"deviceId": "2", "objectType": "COMPUTER"}, "updateAction": "DOWNLOAD_INSTALL", "versionType": "LATEST_ANY", "status": {"state": "PlanFailed", "errorReasons": ["SOME_ERROR"]}},
					{"planUuid": "bbb", "device": {"deviceId": "999", "objectType": "APPLE_TV"}, "updateAction": "DOWNLOAD_INSTALL", "versionType": "LATEST_ANY", "status": {"state": "PlanFailed", "errorReasons": ["EXISTING_PLAN_FOR_DEVICE_IN_PROGRESS"]}}
				]
			}`},
			"/v3/computers-inventory": {200, `{
				"totalCount": 1,
				"results": [
					{"id": "2", "general": {"name": "Mac-Bad"}, "hardware": {"serialNumber": "S2"}, "operatingSystem": {"version": "14.5"}, "userAndLocation": {"username": "bob"}}
				]
			}`},
			"/v2/mobile-devices":        {200, `[]`},
			"/v2/mobile-devices/detail": {200, `{"totalCount": 0, "results": []}`},
		},
	}

	// Capture stdout to verify device 999 is absent from JSON output.
	outputFmt = "json"
	defer func() { outputFmt = "table" }()

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runReportUpdateStatus(context.Background(), client, true, 0)
	_ = w.Close()
	os.Stdout = old
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()

	var result []map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("bad JSON output: %v", err)
	}
	data := result[0]

	errorDevices, _ := data["error_devices"].([]any)
	if len(errorDevices) != 1 {
		t.Errorf("error_devices: got %d entries, want 1 (stale device 999 should be dropped)", len(errorDevices))
	}
	failedPlans, _ := data["failed_plans"].([]any)
	if len(failedPlans) != 1 {
		t.Errorf("failed_plans: got %d entries, want 1 (stale device 999 should be dropped)", len(failedPlans))
	}
}

func TestRunReportUpdateStatus_NoStatuses(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v1/managed-software-updates/update-statuses": {200, `{"totalCount": 0, "results": []}`},
		},
	}

	err := runReportUpdateStatus(context.Background(), client, false, -1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunReportUpdateStatus_BothFetchesFail(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v1/managed-software-updates/update-statuses": {500, `{}`},
			"/v1/managed-software-updates/plans":           {500, `{}`},
		},
	}

	// Both failures are warnings, not fatal — returns nil with "no data found" message
	err := runReportUpdateStatus(context.Background(), client, false, -1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunReportUpdateStatus_SummaryOnly(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v1/managed-software-updates/update-statuses": {200, `{
				"totalCount": 2,
				"results": [
					{"status": "IDLE", "device": {"deviceId": "1", "objectType": "COMPUTER"}},
					{"status": "ERROR", "device": {"deviceId": "2", "objectType": "COMPUTER"}, "productKey": "macOS15", "updated": "2026-04-01"}
				]
			}`},
			"/v1/managed-software-updates/plans": {200, `{"totalCount": 0, "results": []}`},
		},
	}

	// Without --scan-failures the inventory endpoints must not be called.
	// The mock has no inventory routes — any call to them would return 500 and
	// cause fetchUpdateDeviceLookup to silently fail, but the real guard is
	// that runReportUpdateStatus should return before reaching that code path.
	err := runReportUpdateStatus(context.Background(), client, false, -1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// extractLastEventType
// ---------------------------------------------------------------------------

func TestExtractLastEventType_Valid(t *testing.T) {
	eventsJSON := `{"events": [{"type": ".PlanCreatedEvent"}, {"type": ".PlanAcceptedEvent"}, {"type": ".PlanRejectedEvent"}]}`
	got := extractLastEventType(eventsJSON)
	if got != "PlanRejectedEvent" {
		t.Errorf("got %q, want PlanRejectedEvent", got)
	}
}

func TestExtractLastEventType_Empty(t *testing.T) {
	got := extractLastEventType(`{"events": []}`)
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestExtractLastEventType_BadJSON(t *testing.T) {
	got := extractLastEventType(`not json`)
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// ---------------------------------------------------------------------------
// fetchUpdateDeviceLookup
// ---------------------------------------------------------------------------

func TestFetchUpdateDeviceLookup_Computers(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v3/computers-inventory": {200, `{
				"totalCount": 1,
				"results": [
					{"id": "42", "general": {"name": "MacBook"}, "hardware": {"serialNumber": "C02X"}, "operatingSystem": {"version": "15.3"}, "userAndLocation": {"username": "jsmith"}}
				]
			}`},
			"/v2/mobile-devices":        {200, `[]`},
			"/v2/mobile-devices/detail": {200, `{"totalCount": 0, "results": []}`},
		},
	}

	lookup := fetchUpdateDeviceLookup(context.Background(), client)
	meta := lookup["42"]
	if meta.name != "MacBook" {
		t.Errorf("name = %q, want MacBook", meta.name)
	}
	if meta.serial != "C02X" {
		t.Errorf("serial = %q, want C02X", meta.serial)
	}
	if meta.osVersion != "15.3" {
		t.Errorf("osVersion = %q, want 15.3", meta.osVersion)
	}
	if meta.username != "jsmith" {
		t.Errorf("username = %q, want jsmith", meta.username)
	}
	if meta.deviceType != "Computer" {
		t.Errorf("deviceType = %q, want Computer", meta.deviceType)
	}
}
