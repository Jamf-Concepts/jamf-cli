// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
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
		"lastIpAddress": "10.0.1.42",
		"platform": "Mac",
		"managed": true,
		"remoteManagement": {"managed": true},
		"mdmCapable": {"capable": true},
		"enrolledViaAutomatedDeviceEnrollment": true,
		"supervised": false,
		"lastContactTime": "2026-03-30T14:22:00Z"
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

const mdmCommandsJSON = `{
	"totalCount": 2,
	"results": [
		{
			"uuid": "cmd-001",
			"commandType": "ProfileList",
			"status": "Acknowledged",
			"dateSent": "2026-03-30T10:00:00Z",
			"completedDateTime": "2026-03-30T10:01:00Z"
		},
		{
			"uuid": "cmd-002",
			"commandType": "EraseDevice",
			"status": "Error",
			"dateSent": "2026-03-29T08:00:00Z",
			"completedDateTime": "2026-03-29T08:05:00Z"
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
			<status>Completed</status>
		</policy_log>
		<policy_log>
			<policy_id>11</policy_id>
			<policy_name>Update Flash</policy_name>
			<date_completed_epoch>1711790000000</date_completed_epoch>
			<status>Failed</status>
		</policy_log>
	</policy_logs>
</computer_history>`

func TestRunDeviceDeepDive_Basic(t *testing.T) {
	client := &deviceMockClient{
		handler: func(_, path string) (int, string, error) {
			switch {
			// Device resolution — direct ID lookup
			case path == "/v1/computers-inventory-detail/42":
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

	sections, err := runDeviceDeepDive(context.Background(), client, "42")
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
}

func TestRunDeviceDeepDive_NotFound(t *testing.T) {
	client := &deviceMockClient{
		handler: func(_, path string) (int, string, error) {
			// ID lookup fails
			if strings.HasPrefix(path, "/v1/computers-inventory-detail/") {
				return 404, `{"errors":[]}`, nil
			}
			// Serial and name searches return 0 results
			if strings.HasPrefix(path, "/v1/computers-inventory") {
				return 200, `{"totalCount":0,"results":[]}`, nil
			}
			return 0, "", fmt.Errorf("unexpected path: %s", path)
		},
	}

	_, err := runDeviceDeepDive(context.Background(), client, "ghost-machine")
	if err == nil {
		t.Fatal("expected error for unresolvable device, got nil")
	}
	if !strings.Contains(err.Error(), "no device found") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "no device found")
	}
}

func TestRunDeviceDeepDive_PartialFailure(t *testing.T) {
	client := &deviceMockClient{
		handler: func(_, path string) (int, string, error) {
			switch {
			case path == "/v1/computers-inventory-detail/42":
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

	sections, err := runDeviceDeepDive(context.Background(), client, "42")
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

func TestManagedStr(t *testing.T) {
	managed := map[string]any{
		"remoteManagement": map[string]any{"managed": true},
	}
	if got := managedStr(managed); got != "Yes" {
		t.Errorf("managedStr(managed=true) = %q, want %q", got, "Yes")
	}

	unmanaged := map[string]any{
		"remoteManagement": map[string]any{"managed": false},
	}
	if got := managedStr(unmanaged); got != "No" {
		t.Errorf("managedStr(managed=false) = %q, want %q", got, "No")
	}

	missing := map[string]any{}
	if got := managedStr(missing); got != "Unknown" {
		t.Errorf("managedStr(missing) = %q, want %q", got, "Unknown")
	}
}

func TestMdmCapableStr(t *testing.T) {
	capable := map[string]any{
		"mdmCapable": map[string]any{"capable": true},
	}
	if got := mdmCapableStr(capable); got != "Yes" {
		t.Errorf("mdmCapableStr(capable=true) = %q, want %q", got, "Yes")
	}

	notCapable := map[string]any{
		"mdmCapable": map[string]any{"capable": false},
	}
	if got := mdmCapableStr(notCapable); got != "No" {
		t.Errorf("mdmCapableStr(capable=false) = %q, want %q", got, "No")
	}
}
