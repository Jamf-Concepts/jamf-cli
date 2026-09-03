// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// deviceMockClient routes responses based on method+path, supporting both exact
// and prefix matching for query-param-bearing paths.
type deviceMockClient struct {
	handler func(method, path string) (int, string, error)
}

func (m *deviceMockClient) Do(_ context.Context, method, path string, _ io.Reader) (*http.Response, error) {
	code, body, err := m.handler(method, path)
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: code,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}, nil
}

// fullDeviceDetailJSON is a representative computers-inventory-detail response.
const fullDeviceDetailJSON = `{
	"id": "42",
	"general": {
		"name": "MacBook-Lab1",
		"managementId": "2435abf1-f8f0-4633-b133-891be9b974fd",
		"lastIpAddress": "10.0.1.42",
		"platform": "Mac",
		"managed": true,
		"remoteManagement": {"managed": true},
		"mdmCapable": {"capable": true},
		"enrolledViaAutomatedDeviceEnrollment": true,
		"supervised": false,
		"lastCheckIn": "2026-03-30T14:22:00Z"
	},
	"hardware": {
		"make": "Apple",
		"model": "MacBook Pro (16-inch, 2024)",
		"serialNumber": "C02X1234ABCD",
		"processorType": "Apple M3 Max",
		"totalRamMegabytes": 36864
	},
	"operatingSystem": {
		"name": "macOS",
		"version": "15.3.1",
		"build": "24D70"
	},
	"security": {
		"sipStatus": "ENABLED",
		"gatekeeperStatus": "APP_STORE_AND_IDENTIFIED_DEVELOPERS",
		"firewallEnabled": true,
		"bootstrapTokenAllowed": true,
		"bootstrapTokenEscrowedStatus": "ESCROWED"
	},
	"userAndLocation": {
		"username": "jsmith",
		"realname": "Jane Smith",
		"email": "jsmith@example.com",
		"department": "Engineering",
		"building": "HQ"
	}
}`

// mdmCommandsJSON mirrors the real /v2/mdm/commands shape: the status field is
// "commandState" (not "status") and completion is "dateCompleted". A pending
// command carries the epoch-zero sentinel, which must render without a date.
const mdmCommandsJSON = `{
	"totalCount": 2,
	"results": [
		{
			"uuid": "cmd-001",
			"commandType": "ProfileList",
			"commandState": "ACKNOWLEDGED",
			"dateSent": "2026-03-30T10:00:00Z",
			"dateCompleted": "2026-03-30T10:01:00Z"
		},
		{
			"uuid": "cmd-002",
			"commandType": "EraseDevice",
			"commandState": "PENDING",
			"dateSent": "2026-03-29T08:00:00Z",
			"dateCompleted": "1970-01-01T00:00:00Z"
		}
	]
}`

const policyHistoryXML = `<?xml version="1.0" encoding="UTF-8"?>
<computer_history>
	<policy_logs>
		<policy_log>
			<policy_id>10</policy_id>
			<policy_name>Install Chrome</policy_name>
			<date_completed_epoch>1711800000000</date_completed_epoch>
			<date_completed_utc>2026-03-30T12:00:00.000+0000</date_completed_utc>
			<status>Completed</status>
		</policy_log>
		<policy_log>
			<policy_id>11</policy_id>
			<policy_name>Update Flash</policy_name>
			<date_completed_epoch>1711790000000</date_completed_epoch>
			<date_completed_utc>2026-03-30T09:13:20.000+0000</date_completed_utc>
			<status>Failed</status>
		</policy_log>
	</policy_logs>
</computer_history>`

func TestRunDeviceDeepDive_Basic(t *testing.T) {
	client := &deviceMockClient{
		handler: func(_, path string) (int, string, error) {
			switch {
			// Device resolution — direct ID lookup
			case path == "/v4/computers-inventory-detail/42":
				return 200, fullDeviceDetailJSON, nil
			// MDM command history
			case strings.HasPrefix(path, "/v2/mdm/commands"):
				return 200, mdmCommandsJSON, nil
			// Classic policy history
			case path == "/JSSResource/computerhistory/id/42":
				return 200, policyHistoryXML, nil
			default:
				return 0, "", fmt.Errorf("unexpected path: %s", path)
			}
		},
	}

	sections, err := runDeviceDeepDive(context.Background(), &registry.CLIContext{Client: client}, "42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sections) < 3 {
		t.Fatalf("expected at least 3 sections, got %d", len(sections))
	}

	// First section should be Identity.
	if sections[0].Name != "Identity" {
		t.Errorf("first section name = %q, want %q", sections[0].Name, "Identity")
	}

	// Verify Identity has the device name.
	found := false
	for _, item := range sections[0].Items {
		if item.Resource == "Name" && item.Value == "MacBook-Lab1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Identity section missing Name=MacBook-Lab1 item")
	}

	// Verify we got Hardware & OS and Security sections.
	sectionNames := make(map[string]bool)
	for _, s := range sections {
		sectionNames[s.Name] = true
	}
	for _, want := range []string{"Identity", "Hardware & OS", "Security", "User & Location"} {
		if !sectionNames[want] {
			t.Errorf("missing section %q", want)
		}
	}

	// MDM history: completed command shows state + date; pending (epoch-zero)
	// shows state only. Regression guard for the commandState/dateCompleted fix.
	mdm := findSection(t, sections, "MDM Command History (Last 10)")
	assertItem(t, mdm, "ProfileList", "ACKNOWLEDGED  2026-03-30 10:01")
	assertItem(t, mdm, "EraseDevice", "PENDING")

	// Policy history: status + completion date from date_completed_utc.
	pol := findSection(t, sections, "Policy History (Last 10)")
	assertItem(t, pol, "Install Chrome", "Completed  2026-03-30 12:00")
	assertItem(t, pol, "Update Flash", "Failed  2026-03-30 09:13")
}

// findSection returns the named section or fails the test.
func findSection(t *testing.T, sections []overviewSection, name string) overviewSection {
	t.Helper()
	for _, s := range sections {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("missing section %q", name)
	return overviewSection{}
}

// assertItem asserts the section contains an item with the given resource label
// and value.
func assertItem(t *testing.T, s overviewSection, resource, value string) {
	t.Helper()
	for _, item := range s.Items {
		if item.Resource == resource {
			if item.Value != value {
				t.Errorf("section %q item %q value = %q, want %q", s.Name, resource, item.Value, value)
			}
			return
		}
	}
	t.Errorf("section %q missing item %q", s.Name, resource)
}

func TestFormatCompletedDate(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"mdm epoch zero sentinel", "1970-01-01T00:00:00Z", ""},
		{"mdm rfc3339 millis", "2024-06-03T07:53:42.489Z", "2024-06-03 07:53"},
		{"mdm rfc3339 no fraction", "2026-03-30T10:01:00Z", "2026-03-30 10:01"},
		{"classic utc offset", "2024-06-21T10:05:45.000+0000", "2024-06-21 10:05"},
		{"unparseable returned raw", "2024/06/21 at 10:05 AM", "2024/06/21 at 10:05 AM"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatCompletedDate(tt.in); got != tt.want {
				t.Errorf("formatCompletedDate(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestHistoryValue(t *testing.T) {
	tests := []struct {
		status, completed, want string
	}{
		{"ACKNOWLEDGED", "2024-06-03 07:53", "ACKNOWLEDGED  2024-06-03 07:53"},
		{"PENDING", "", "PENDING"},
		{"", "2024-06-03 07:53", "2024-06-03 07:53"},
		{"", "", ""},
	}
	for _, tt := range tests {
		if got := historyValue(tt.status, tt.completed); got != tt.want {
			t.Errorf("historyValue(%q, %q) = %q, want %q", tt.status, tt.completed, got, tt.want)
		}
	}
}

func TestRunDeviceDeepDive_NotFound(t *testing.T) {
	client := &deviceMockClient{
		handler: func(_, path string) (int, string, error) {
			// ID lookup fails
			if strings.HasPrefix(path, "/v4/computers-inventory-detail/") {
				return 404, `{"errors":[]}`, nil
			}
			// Serial and name searches return 0 results
			if strings.HasPrefix(path, "/v4/computers-inventory") {
				return 200, `{"totalCount":0,"results":[]}`, nil
			}
			return 0, "", fmt.Errorf("unexpected path: %s", path)
		},
	}

	_, err := runDeviceDeepDive(context.Background(), &registry.CLIContext{Client: client}, "ghost-machine")
	if err == nil {
		t.Fatal("expected error for unresolvable device, got nil")
		return
	}
	if !strings.Contains(err.Error(), "no device found") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "no device found")
	}
}

func TestRunDeviceDeepDive_PartialFailure(t *testing.T) {
	client := &deviceMockClient{
		handler: func(_, path string) (int, string, error) {
			switch {
			case path == "/v4/computers-inventory-detail/42":
				return 200, fullDeviceDetailJSON, nil
			// MDM history fails
			case strings.HasPrefix(path, "/v2/mdm/commands"):
				return 500, `{"error":"server error"}`, nil
			// Policy history fails
			case path == "/JSSResource/computerhistory/id/42":
				return 500, `internal server error`, nil
			default:
				return 0, "", fmt.Errorf("unexpected path: %s", path)
			}
		},
	}

	sections, err := runDeviceDeepDive(context.Background(), &registry.CLIContext{Client: client}, "42")
	if err != nil {
		t.Fatalf("expected no error on partial failure, got: %v", err)
	}

	// Should still have at least Identity and Security sections.
	if len(sections) < 2 {
		t.Fatalf("expected at least 2 sections on partial failure, got %d", len(sections))
	}

	// Verify core sections present.
	sectionNames := make(map[string]bool)
	for _, s := range sections {
		sectionNames[s.Name] = true
	}
	if !sectionNames["Identity"] {
		t.Error("missing Identity section")
	}
	if !sectionNames["Security"] {
		t.Error("missing Security section")
	}
}

func TestStrVal(t *testing.T) {
	m := map[string]any{"name": "test", "count": 42.0}
	if got := strVal(m, "name"); got != "test" {
		t.Errorf("strVal(name) = %q, want %q", got, "test")
	}
	if got := strVal(m, "missing"); got != "" {
		t.Errorf("strVal(missing) = %q, want %q", got, "")
	}
	if got := strVal(nil, "name"); got != "" {
		t.Errorf("strVal(nil, name) = %q, want %q", got, "")
	}
}

func TestBoolVal(t *testing.T) {
	m := map[string]any{"enabled": true, "disabled": false}
	if got := boolVal(m, "enabled"); !got {
		t.Error("boolVal(enabled) = false, want true")
	}
	if got := boolVal(m, "disabled"); got {
		t.Error("boolVal(disabled) = true, want false")
	}
	if got := boolVal(m, "missing"); got {
		t.Error("boolVal(missing) = true, want false")
	}
	if got := boolVal(nil, "enabled"); got {
		t.Error("boolVal(nil, enabled) = true, want false")
	}
}

func TestNumStr(t *testing.T) {
	m := map[string]any{"ram": 36864.0, "label": "16GB"}
	if got := numStr(m, "ram"); got != "36864" {
		t.Errorf("numStr(ram) = %q, want %q", got, "36864")
	}
	if got := numStr(m, "label"); got != "16GB" {
		t.Errorf("numStr(label) = %q, want %q", got, "16GB")
	}
	if got := numStr(m, "missing"); got != "" {
		t.Errorf("numStr(missing) = %q, want %q", got, "")
	}
}

func TestNestedBoolStr(t *testing.T) {
	managed := map[string]any{
		"remoteManagement": map[string]any{"managed": true},
	}
	if got := nestedBoolStr(managed, "remoteManagement", "managed"); got != "Yes" {
		t.Errorf("nestedBoolStr(managed=true) = %q, want %q", got, "Yes")
	}

	unmanaged := map[string]any{
		"remoteManagement": map[string]any{"managed": false},
	}
	if got := nestedBoolStr(unmanaged, "remoteManagement", "managed"); got != "No" {
		t.Errorf("nestedBoolStr(managed=false) = %q, want %q", got, "No")
	}

	missing := map[string]any{}
	if got := nestedBoolStr(missing, "remoteManagement", "managed"); got != "Unknown" {
		t.Errorf("nestedBoolStr(missing) = %q, want %q", got, "Unknown")
	}

	if got := nestedBoolStr(nil, "any", "key"); got != "Unknown" {
		t.Errorf("nestedBoolStr(nil) = %q, want %q", got, "Unknown")
	}
}
