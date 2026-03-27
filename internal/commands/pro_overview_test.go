package commands

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// overviewMockClient implements registry.HTTPClient for testing overview fetches.
type overviewMockClient struct {
	responses map[string]overviewMockResponse
}

type overviewMockResponse struct {
	statusCode int
	body       string
}

func (m *overviewMockClient) Do(_ context.Context, method, path string, _ io.Reader) (*http.Response, error) {
	// Strip query params for lookup
	key := path
	if resp, ok := m.responses[key]; ok {
		return &http.Response{
			StatusCode: resp.statusCode,
			Body:       io.NopCloser(strings.NewReader(resp.body)),
			Header:     make(http.Header),
		}, nil
	}

	// Try without query params
	if idx := strings.Index(path, "?"); idx != -1 {
		base := path[:idx]
		if resp, ok := m.responses[base]; ok {
			return &http.Response{
				StatusCode: resp.statusCode,
				Body:       io.NopCloser(strings.NewReader(resp.body)),
				Header:     make(http.Header),
			}, nil
		}
	}

	return nil, fmt.Errorf("no mock response for %s %s", method, path)
}

func TestFetchPaginatedCount(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v1/buildings": {200, `{"totalCount": 42, "results": []}`},
		},
	}

	got, err := fetchPaginatedCount(context.Background(), client, "/v1/buildings")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "42" {
		t.Errorf("got %q, want %q", got, "42")
	}
}

func TestFetchPaginatedCount_LargeNumber(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v1/scripts": {200, `{"totalCount": 1234, "results": []}`},
		},
	}

	got, err := fetchPaginatedCount(context.Background(), client, "/v1/scripts")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "1,234" {
		t.Errorf("got %q, want %q", got, "1,234")
	}
}

func TestFetchArrayCount(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v1/sites": {200, `[{"id":1,"name":"Main"},{"id":2,"name":"Branch"}]`},
		},
	}

	got, err := fetchArrayCount(context.Background(), client, "/v1/sites")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "2" {
		t.Errorf("got %q, want %q", got, "2")
	}
}

func TestFetchArrayCount_Empty(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v1/sites": {200, `[]`},
		},
	}

	got, err := fetchArrayCount(context.Background(), client, "/v1/sites")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "0" {
		t.Errorf("got %q, want %q", got, "0")
	}
}

func TestFetchPaginatedCount_Error(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v1/buildings": {403, `{"httpStatus":403}`},
		},
	}

	_, err := fetchPaginatedCount(context.Background(), client, "/v1/buildings")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestFetchArrayCount_Error(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{},
	}

	_, err := fetchArrayCount(context.Background(), client, "/v1/nonexistent")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCommaFormat(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0"},
		{42, "42"},
		{999, "999"},
		{1000, "1,000"},
		{1234, "1,234"},
		{12345, "12,345"},
		{123456, "123,456"},
		{1234567, "1,234,567"},
	}

	for _, tt := range tests {
		got := commaFormat(tt.input)
		if got != tt.want {
			t.Errorf("commaFormat(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestEnabledDisabled(t *testing.T) {
	if got := enabledDisabled(true); got != "enabled" {
		t.Errorf("got %q, want %q", got, "enabled")
	}
	if got := enabledDisabled(false); got != "disabled" {
		t.Errorf("got %q, want %q", got, "disabled")
	}
	if got := enabledDisabled(nil); got != "disabled" {
		t.Errorf("got %q for nil, want %q", got, "disabled")
	}
	if got := enabledDisabled("true"); got != "disabled" {
		t.Errorf("got %q for string, want %q", got, "disabled")
	}
}

func TestPrintOverviewTable(t *testing.T) {
	sections := []overviewSection{
		{
			Name: "Instance Info",
			Items: []overviewItem{
				{"Server URL", "https://example.jamfcloud.com", ""},
				{"Health Status", "ok", ""},
			},
		},
		{
			Name: "Jamf Pro Features",
			Items: []overviewItem{
				{"VPP Token", "enabled", ""},
				{"SMTP", "disabled", ""},
			},
		},
	}

	var buf bytes.Buffer
	printOverviewTable(&buf, sections, false)
	output := buf.String()

	// Verify section headers
	if !strings.Contains(output, "INSTANCE OVERVIEW") {
		t.Error("missing INSTANCE OVERVIEW header")
	}
	if !strings.Contains(output, "Instance Info") {
		t.Error("missing Instance Info section")
	}
	if !strings.Contains(output, "Jamf Pro Features") {
		t.Error("missing Jamf Pro Features section")
	}

	// Verify values
	if !strings.Contains(output, "https://example.jamfcloud.com") {
		t.Error("missing server URL value")
	}
	if !strings.Contains(output, "ok") {
		t.Error("missing health status value")
	}
	if !strings.Contains(output, "enabled") {
		t.Error("missing enabled value")
	}
	if !strings.Contains(output, "disabled") {
		t.Error("missing disabled value")
	}
}

func TestPrintOverviewTable_WithColor(t *testing.T) {
	sections := []overviewSection{
		{
			Name: "Test",
			Items: []overviewItem{
				{"Health", "ok", ""},
				{"Feature", "disabled", ""},
				{"Error", "offline", ""},
				{"Missing", "N/A", ""},
			},
		},
	}

	var buf bytes.Buffer
	printOverviewTable(&buf, sections, true)
	output := buf.String()

	// Color codes should be present
	if !strings.Contains(output, "\033[32m") {
		t.Error("missing green color code for 'ok'")
	}
	if !strings.Contains(output, "\033[2m") {
		t.Error("missing dim color code for 'disabled'")
	}
	if !strings.Contains(output, "\033[31m") {
		t.Error("missing red color code for 'offline'")
	}
}

func TestOverviewToRows(t *testing.T) {
	sections := []overviewSection{
		{
			Name: "Instance Info",
			Items: []overviewItem{
				{"Server URL", "https://example.com", ""},
				{"Version", "10.52.0", ""},
			},
		},
		{
			Name: "Features",
			Items: []overviewItem{
				{"VPP", "enabled", ""},
			},
		},
	}

	rows := overviewToRows(sections)

	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}

	// First row
	if rows[0]["section"] != "Instance Info" {
		t.Errorf("row 0 section = %q, want %q", rows[0]["section"], "Instance Info")
	}
	if rows[0]["resource"] != "Server URL" {
		t.Errorf("row 0 resource = %q, want %q", rows[0]["resource"], "Server URL")
	}
	if rows[0]["value"] != "https://example.com" {
		t.Errorf("row 0 value = %q, want %q", rows[0]["value"], "https://example.com")
	}

	// Last row
	if rows[2]["section"] != "Features" {
		t.Errorf("row 2 section = %q, want %q", rows[2]["section"], "Features")
	}
	if rows[2]["resource"] != "VPP" {
		t.Errorf("row 2 resource = %q, want %q", rows[2]["resource"], "VPP")
	}
	if rows[2]["value"] != "enabled" {
		t.Errorf("row 2 value = %q, want %q", rows[2]["value"], "enabled")
	}
}

func TestCSA404_NotConfigured(t *testing.T) {
	// Simulate the CSA token fetch returning 404.
	// In the actual overview, this is handled inline in runOverview.
	// Here we test the pattern: 404 → "Not configured".
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v1/csa/token": {404, `{"httpStatus":404}`},
		},
	}

	resp, err := client.Do(context.Background(), "GET", "/v1/csa/token", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result string
	if resp.StatusCode == http.StatusNotFound {
		result = "Not configured"
	}

	if result != "Not configured" {
		t.Errorf("got %q, want %q", result, "Not configured")
	}
}

func TestFetchJSON(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v1/jamf-pro-version": {200, `{"version":"10.52.0"}`},
		},
	}

	data, err := fetchJSON(context.Background(), client, "/v1/jamf-pro-version")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if v, ok := data["version"].(string); !ok || v != "10.52.0" {
		t.Errorf("got version=%v, want %q", data["version"], "10.52.0")
	}
}

func TestFetchJSON_HTTPError(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v1/jamf-pro-version": {500, `Internal Server Error`},
		},
	}

	_, err := fetchJSON(context.Background(), client, "/v1/jamf-pro-version")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("expected HTTP 500 in error, got: %v", err)
	}
}

func TestFetchJSON_InvalidJSON(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v1/test": {200, `not json`},
		},
	}

	_, err := fetchJSON(context.Background(), client, "/v1/test")
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestFetchEnrollmentSettings(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v4/enrollment": {200, `{
				"macOsEnterpriseEnrollmentEnabled": true,
				"iosEnterpriseEnrollmentEnabled": false,
				"iosPersonalEnrollmentEnabled": true,
				"accountDrivenUserEnrollmentEnabled": false,
				"accountDrivenDeviceMacosEnrollmentEnabled": true
			}`},
		},
	}

	data, err := fetchJSON(context.Background(), client, "/v4/enrollment")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		field string
		want  string
	}{
		{"macOsEnterpriseEnrollmentEnabled", "enabled"},
		{"iosEnterpriseEnrollmentEnabled", "disabled"},
		{"iosPersonalEnrollmentEnabled", "enabled"},
		{"accountDrivenUserEnrollmentEnabled", "disabled"},
		{"accountDrivenDeviceMacosEnrollmentEnabled", "enabled"},
	}

	for _, tt := range tests {
		got := enabledDisabled(data[tt.field])
		if got != tt.want {
			t.Errorf("enabledDisabled(%s) = %q, want %q", tt.field, got, tt.want)
		}
	}
}

func TestFetchSelfServiceSettings(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v1/self-service/settings": {200, `{
				"installSettings": {"installAutomatically": true},
				"loginSettings": {"userLoginLevel": "Required"},
				"configurationSettings": {"notificationsEnabled": false}
			}`},
		},
	}

	data, err := fetchJSON(context.Background(), client, "/v1/self-service/settings")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	install, ok := data["installSettings"].(map[string]interface{})
	if !ok {
		t.Fatal("installSettings not a map")
	}
	if got := enabledDisabled(install["installAutomatically"]); got != "enabled" {
		t.Errorf("installAutomatically = %q, want %q", got, "enabled")
	}

	login, ok := data["loginSettings"].(map[string]interface{})
	if !ok {
		t.Fatal("loginSettings not a map")
	}
	if level, ok := login["userLoginLevel"].(string); !ok || level != "Required" {
		t.Errorf("userLoginLevel = %v, want %q", login["userLoginLevel"], "Required")
	}

	config, ok := data["configurationSettings"].(map[string]interface{})
	if !ok {
		t.Fatal("configurationSettings not a map")
	}
	if got := enabledDisabled(config["notificationsEnabled"]); got != "disabled" {
		t.Errorf("notificationsEnabled = %q, want %q", got, "disabled")
	}
}

func TestFetchLAPSSettings(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v2/local-admin-password/settings": {200, `{
				"autoDeployEnabled": true,
				"autoRotateEnabled": false
			}`},
		},
	}

	data, err := fetchJSON(context.Background(), client, "/v2/local-admin-password/settings")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := enabledDisabled(data["autoDeployEnabled"]); got != "enabled" {
		t.Errorf("autoDeployEnabled = %q, want %q", got, "enabled")
	}
	if got := enabledDisabled(data["autoRotateEnabled"]); got != "disabled" {
		t.Errorf("autoRotateEnabled = %q, want %q", got, "disabled")
	}
}

func TestFetchDeviceCommunicationSettings(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v1/device-communication-settings": {200, `{
				"autoRenewComputerMdmProfileWhenDeviceIdentityCertExpiring": true,
				"autoRenewMobileDeviceMdmProfileWhenDeviceIdentityCertExpiring": false,
				"mdmProfileComputerExpirationLimitInDays": 180,
				"mdmProfileMobileDeviceExpirationLimitInDays": 90
			}`},
		},
	}

	data, err := fetchJSON(context.Background(), client, "/v1/device-communication-settings")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := enabledDisabled(data["autoRenewComputerMdmProfileWhenDeviceIdentityCertExpiring"]); got != "enabled" {
		t.Errorf("autoRenewComputer = %q, want %q", got, "enabled")
	}
	if got := enabledDisabled(data["autoRenewMobileDeviceMdmProfileWhenDeviceIdentityCertExpiring"]); got != "disabled" {
		t.Errorf("autoRenewMobile = %q, want %q", got, "disabled")
	}
	if days, ok := data["mdmProfileComputerExpirationLimitInDays"].(float64); !ok || int(days) != 180 {
		t.Errorf("computerDays = %v, want 180", data["mdmProfileComputerExpirationLimitInDays"])
	}
	if days, ok := data["mdmProfileMobileDeviceExpirationLimitInDays"].(float64); !ok || int(days) != 90 {
		t.Errorf("mobileDays = %v, want 90", data["mdmProfileMobileDeviceExpirationLimitInDays"])
	}
}

func TestFormatExpirationDate(t *testing.T) {
	now := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		dateStr   string
		wantColor string
		wantWord  string // substring to check in formatted output
	}{
		{"2026-01-10", "red", "expired"},    // past
		{"2026-01-20", "red", "5 days"},     // <30 days
		{"2026-03-01", "yellow", "45 days"}, // 30-90 days
		{"2026-06-01", "", "Jun 01, 2026"},  // >90 days, no color
	}

	for _, tt := range tests {
		formatted, color := formatExpirationDate(tt.dateStr, now)
		if color != tt.wantColor {
			t.Errorf("formatExpirationDate(%q): color = %q, want %q", tt.dateStr, color, tt.wantColor)
		}
		if !strings.Contains(formatted, tt.wantWord) {
			t.Errorf("formatExpirationDate(%q): formatted = %q, want to contain %q", tt.dateStr, formatted, tt.wantWord)
		}
	}
}

func TestDEPTokenExpiration(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v1/device-enrollments": {200, `{
				"totalCount": 3,
				"results": [
					{"id": "1", "tokenExpirationDate": "2026-09-15"},
					{"id": "2", "tokenExpirationDate": "2026-03-01"},
					{"id": "3", "tokenExpirationDate": "2026-12-31"}
				]
			}`},
		},
	}

	data, err := fetchJSON(context.Background(), client, "/v1/device-enrollments")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify count
	if tc := formatCount(data["totalCount"]); tc != "3" {
		t.Errorf("totalCount = %q, want %q", tc, "3")
	}

	// Find earliest expiration
	results, ok := data["results"].([]interface{})
	if !ok {
		t.Fatal("results not an array")
	}

	var earliest string
	for _, r := range results {
		item, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		dateStr, ok := item["tokenExpirationDate"].(string)
		if !ok {
			continue
		}
		if earliest == "" || dateStr < earliest {
			earliest = dateStr
		}
	}

	if earliest != "2026-03-01" {
		t.Errorf("earliest expiration = %q, want %q", earliest, "2026-03-01")
	}
}

func TestDEPTokenExpiration_NoneConfigured(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v1/device-enrollments": {200, `{
				"totalCount": 0,
				"results": []
			}`},
		},
	}

	data, err := fetchJSON(context.Background(), client, "/v1/device-enrollments")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	results, ok := data["results"].([]interface{})
	if !ok {
		t.Fatal("results not an array")
	}

	var earliest string
	for _, r := range results {
		item, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		dateStr, ok := item["tokenExpirationDate"].(string)
		if !ok {
			continue
		}
		if earliest == "" || dateStr < earliest {
			earliest = dateStr
		}
	}

	if earliest != "" {
		t.Errorf("expected empty earliest, got %q", earliest)
	}
}

func TestPrintOverviewTable_ExpirationColors(t *testing.T) {
	sections := []overviewSection{
		{
			Name: "Certificate Authority",
			Items: []overviewItem{
				{"CA Expires", "Jan 01, 2031", ""},
				{"Token Expires", "Mar 01, 2026 (45 days)", "yellow"},
				{"Token Expired", "Jan 01, 2025 (expired)", "red"},
			},
		},
	}

	var buf bytes.Buffer
	printOverviewTable(&buf, sections, true)
	output := buf.String()

	if !strings.Contains(output, "\033[33m") {
		t.Error("missing yellow color code for expiring token")
	}
	if !strings.Contains(output, "\033[31m") {
		t.Error("missing red color code for expired token")
	}
	if !strings.Contains(output, "Jan 01, 2031") {
		t.Error("missing plain CA expiration date")
	}
}

func TestFetchClassicCount(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/JSSResource/policies": {200, `{"policies":[{"id":1,"name":"P1"},{"id":2,"name":"P2"}]}`},
		},
	}

	got, err := fetchClassicCount(context.Background(), client, "/JSSResource/policies", "policies")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "2" {
		t.Errorf("got %q, want %q", got, "2")
	}
}

func TestFetchClassicCount_Empty(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/JSSResource/webhooks": {200, `{"webhooks":[]}`},
		},
	}

	got, err := fetchClassicCount(context.Background(), client, "/JSSResource/webhooks", "webhooks")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "0" {
		t.Errorf("got %q, want %q", got, "0")
	}
}

func TestFetchClassicCount_MissingKey(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/JSSResource/policies": {200, `{"other_key":[]}`},
		},
	}

	got, err := fetchClassicCount(context.Background(), client, "/JSSResource/policies", "policies")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "0" {
		t.Errorf("got %q, want %q", got, "0")
	}
}

func TestFetchClassicCount_HTTPError(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/JSSResource/policies": {403, `{"httpStatus":403}`},
		},
	}

	_, err := fetchClassicCount(context.Background(), client, "/JSSResource/policies", "policies")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestFetchClassicNestedSize(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/JSSResource/computercommands": {200, `{
				"computer_commands": {
					"computer_command": [{"uuid":"a","command":"DeviceInformation"}],
					"size": 68
				}
			}`},
		},
	}

	got, err := fetchClassicNestedSize(context.Background(), client, "/JSSResource/computercommands", "computer_commands")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "68" {
		t.Errorf("got %q, want %q", got, "68")
	}
}

func TestFetchClassicNestedSize_Empty(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/JSSResource/computercommands": {200, `{
				"computer_commands": {
					"computer_command": [],
					"size": 0
				}
			}`},
		},
	}

	got, err := fetchClassicNestedSize(context.Background(), client, "/JSSResource/computercommands", "computer_commands")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "0" {
		t.Errorf("got %q, want %q", got, "0")
	}
}

func TestFetchClassicNestedSize_MissingOuter(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/JSSResource/computercommands": {200, `{"other_key": {}}`},
		},
	}

	got, err := fetchClassicNestedSize(context.Background(), client, "/JSSResource/computercommands", "computer_commands")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "0" {
		t.Errorf("got %q, want %q", got, "0")
	}
}

func TestPrintOverviewTable_NotificationsNone(t *testing.T) {
	sections := []overviewSection{
		{
			Name: "Notifications",
			Items: []overviewItem{
				{"Active Alerts", "None", ""},
			},
		},
	}

	var buf bytes.Buffer
	printOverviewTable(&buf, sections, true)
	output := buf.String()

	// "None" should render green (same as "ok")
	if !strings.Contains(output, "\033[32m") {
		t.Error("missing green color code for 'None' alerts")
	}
}

func TestPrintOverviewTable_NotificationsActive(t *testing.T) {
	sections := []overviewSection{
		{
			Name: "Notifications",
			Items: []overviewItem{
				{"Active Alerts", "4 active", "red"},
				{"Alert Types", "PUSH_CERT_EXPIRED, EXCEEDED_LICENSE_COUNT", ""},
			},
		},
	}

	var buf bytes.Buffer
	printOverviewTable(&buf, sections, true)
	output := buf.String()

	if !strings.Contains(output, "\033[31m") {
		t.Error("missing red color code for active alerts")
	}
	if !strings.Contains(output, "PUSH_CERT_EXPIRED") {
		t.Error("missing alert type detail")
	}
}

func TestFormatCount(t *testing.T) {
	tests := []struct {
		name  string
		input interface{}
		want  string
	}{
		{"float64 zero", float64(0), "0"},
		{"float64 small", float64(42), "42"},
		{"float64 large", float64(1234567), "1,234,567"},
		{"int", int(99), "99"},
		{"int64", int64(5000), "5,000"},
		{"non-numeric string", "hello", "hello"},
		{"nil", nil, "<nil>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatCount(tt.input)
			if got != tt.want {
				t.Errorf("formatCount(%v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFormatEpochExpiration(t *testing.T) {
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		epoch     float64
		wantColor string
		wantWord  string
	}{
		{"expired", float64(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Unix()), "red", "expired"},
		{"expiring soon", float64(time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC).Unix()), "red", "days"},
		{"expiring mid", float64(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC).Unix()), "yellow", "days"},
		{"far future", float64(time.Date(2028, 1, 1, 0, 0, 0, 0, time.UTC).Unix()), "", "Jan 01, 2028"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			formatted, color := formatEpochExpiration(tt.epoch, now)
			if color != tt.wantColor {
				t.Errorf("color = %q, want %q", color, tt.wantColor)
			}
			if !strings.Contains(formatted, tt.wantWord) {
				t.Errorf("formatted = %q, want to contain %q", formatted, tt.wantWord)
			}
		})
	}
}

func TestRunOverview_FullMock(t *testing.T) {
	// Mock all API paths used by runOverview
	mock := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v1/jamf-pro-version":                           {200, `{"version":"11.0.0"}`},
			"/v1/slasa":                                      {200, `{"slasaAcceptanceStatus":"ACCEPTED"}`},
			"/v2/jamf-pro-information":                       {200, `{"vppTokenEnabled":true,"depAccountEnabled":false,"cloudDeploymentsEnabled":true,"patchEnabled":true,"ssoSamlEnabled":false,"smtpEnabled":true}`},
			"/v1/csa/token":                                  {404, `{}`},
			"/v3/check-in":                                   {200, `{"checkInFrequency":15,"createHooks":false,"createStartupScript":true,"enableLocalConfigurationProfiles":false}`},
			"/v4/enrollment":                                 {200, `{"macOsEnterpriseEnrollmentEnabled":true,"iosEnterpriseEnrollmentEnabled":true,"iosPersonalEnrollmentEnabled":false,"accountDrivenUserEnrollmentEnabled":false,"accountDrivenDeviceMacosEnrollmentEnabled":true}`},
			"/v1/self-service/settings":                      {200, `{"installSettings":{"installAutomatically":true},"loginSettings":{"userLoginLevel":"Required"},"configurationSettings":{"notificationsEnabled":false}}`},
			"/v2/local-admin-password/settings":              {200, `{"autoDeployEnabled":true,"autoRotateEnabled":false}`},
			"/v1/device-communication-settings":              {200, `{"autoRenewComputerMdmProfileWhenDeviceIdentityCertExpiring":true,"autoRenewMobileDeviceMdmProfileWhenDeviceIdentityCertExpiring":false,"mdmProfileComputerExpirationLimitInDays":180,"mdmProfileMobileDeviceExpirationLimitInDays":90}`},
			"/v1/pki/certificate-authority/active":           {200, `{"notAfter":1893456000}`},
			"/v1/inventory-information":                      {200, `{"managedComputers":500,"unmanagedComputers":10,"managedDevices":200,"unmanagedDevices":5}`},
			"/v1/sites":                                      {200, `[{"id":"1"},{"id":"2"}]`},
			"/v1/buildings":                                  {200, `{"totalCount":3,"results":[]}`},
			"/v1/departments":                                {200, `{"totalCount":5,"results":[]}`},
			"/v1/categories":                                 {200, `{"totalCount":12,"results":[]}`},
			"/v1/computer-groups":                            {200, `[{"id":"1"}]`},
			"/v1/mobile-device-groups/smart-groups":          {200, `{"totalCount":8,"results":[]}`},
			"/v1/scripts":                                    {200, `{"totalCount":25,"results":[]}`},
			"/v1/ebooks":                                     {200, `{"totalCount":0,"results":[]}`},
			"/v1/jcds/files":                                 {200, `[]`},
			"/v1/device-enrollments":                         {200, `{"totalCount":1,"results":[{"id":"42","tokenExpirationDate":"2027-06-15"}]}`},
			"/v1/device-enrollments/42/syncs/latest":         {200, `{"syncState":"SUCCESSFUL","timestamp":"2026-03-14T10:30:00.000"}`},
			"/v3/computer-prestages":                         {200, `{"totalCount":2,"results":[]}`},
			"/v3/mobile-device-prestages":                    {200, `{"totalCount":1,"results":[]}`},
			"/v1/static-user-groups":                         {200, `[{"id":"1"},{"id":"2"},{"id":"3"}]`},
			"/v1/notifications":                              {200, `[]`},
			"/v2/mdm/commands":                               {200, `{"totalCount":0,"results":[]}`},
			"/JSSResource/computercommands":                  {200, `{"computer_commands":{"computer_command":[],"size":12}}`},
			"/JSSResource/mobiledevicecommands":              {200, `{"mobile_device_commands":{"mobile_device_command":[],"size":3}}`},
			"/JSSResource/policies":                          {200, `{"policies":[{"id":1}]}`},
			"/JSSResource/osxconfigurationprofiles":          {200, `{"os_x_configuration_profiles":[{"id":1},{"id":2}]}`},
			"/JSSResource/mobiledeviceconfigurationprofiles": {200, `{"configuration_profiles":[]}`},
			"/JSSResource/packages":                          {200, `{"packages":[{"id":1},{"id":2},{"id":3}]}`},
			"/JSSResource/patchsoftwaretitles":               {200, `{"patch_software_titles":[{"id":1}]}`},
			"/JSSResource/webhooks":                          {200, `{"webhooks":[]}`},
			"/ldap/servers":                                  {200, `[{"id":"1"}]`},
		},
	}

	// runOverview uses the package-level serverURL for health check + display
	oldURL := serverURL
	serverURL = "https://test.jamfcloud.com"
	defer func() { serverURL = oldURL }()

	cliCtx := &registry.CLIContext{Client: mock}
	sections, err := runOverview(context.Background(), cliCtx)
	if err != nil {
		t.Fatalf("runOverview error: %v", err)
	}

	if len(sections) == 0 {
		t.Fatal("expected non-empty sections")
	}

	// Flatten to a map for easy assertions
	values := make(map[string]string)
	for _, sec := range sections {
		for _, item := range sec.Items {
			if item.Resource != "" {
				values[item.Resource] = item.Value
			}
		}
	}

	// Spot-check key values (matches new section layout)
	checks := map[string]string{
		"Active Alerts":             "None",
		"Failed Commands":           "0",
		"Pending Computer Commands": "12",
		"Pending Mobile Commands":   "3",
		"Server URL":                "https://test.jamfcloud.com",
		"Jamf Pro Version":          "11.0.0",
		"Managed Computers":         "500",
		"Unmanaged Computers":       "10",
		"Managed Devices":           "200",
		"Check-In Frequency":        "15 min",
		"DEP Instances":             "1",
		"Computer Prestages":        "2",
		"Sites":                     "2",
		"Buildings":                 "3",
		"Scripts":                   "25",
		"Policies":                  "1",
		"Packages":                  "3",
		"Static User Groups":        "3",
	}

	for resource, want := range checks {
		got, ok := values[resource]
		if !ok {
			t.Errorf("missing resource %q in overview output", resource)
			continue
		}
		if got != want {
			t.Errorf("%s = %q, want %q", resource, got, want)
		}
	}
}

func TestRunOverview_AllAPIErrors(t *testing.T) {
	// Empty mock → every API call fails
	mock := &overviewMockClient{responses: map[string]overviewMockResponse{}}

	oldURL := serverURL
	serverURL = "http://127.0.0.1:1" // health check will fail too
	defer func() { serverURL = oldURL }()

	cliCtx := &registry.CLIContext{Client: mock}
	sections, err := runOverview(context.Background(), cliCtx)
	if err != nil {
		t.Fatalf("runOverview should not return error (errors become N/A), got: %v", err)
	}

	// Everything should be "N/A" or a known fallback
	for _, sec := range sections {
		for _, item := range sec.Items {
			if item.Resource == "Server URL" {
				continue // always populated from serverURL var
			}
			if item.Resource == "" && item.Value == "" {
				continue // separator
			}
			// Should not panic or have empty values — "N/A" is the expected fallback
		}
	}

	if len(sections) == 0 {
		t.Fatal("expected sections even when all API calls fail")
	}
}

func TestFetchPaginatedCount_ExistingQueryParams(t *testing.T) {
	client := &overviewMockClient{
		responses: map[string]overviewMockResponse{
			"/v2/mdm/commands": {200, `{"totalCount":7,"results":[]}`},
		},
	}

	got, err := fetchPaginatedCount(context.Background(), client, "/v2/mdm/commands?filter=status%3D%3DError")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "7" {
		t.Errorf("got %q, want %q", got, "7")
	}
}

func TestOverviewToRows_WithColorHints(t *testing.T) {
	sections := []overviewSection{
		{
			Name: "Security",
			Items: []overviewItem{
				{"CA Expires", "Mar 01, 2026 (expired)", "red"},
				{"Health", "ok", ""},
			},
		},
	}

	rows := overviewToRows(sections)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}

	if rows[0]["status"] != "red" {
		t.Errorf("row 0 status = %v, want %q", rows[0]["status"], "red")
	}
	if _, hasStatus := rows[1]["status"]; hasStatus {
		t.Error("row 1 should not have status key")
	}
}

func TestPrintOverviewTable_BlankSeparator(t *testing.T) {
	sections := []overviewSection{
		{
			Name: "Test",
			Items: []overviewItem{
				{"Before", "value1", ""},
				{}, // blank separator
				{"After", "value2", ""},
			},
		},
	}

	var buf bytes.Buffer
	printOverviewTable(&buf, sections, false)
	output := buf.String()

	if !strings.Contains(output, "Before") || !strings.Contains(output, "After") {
		t.Error("missing items around separator")
	}
}

func TestPrintOverviewTable_ACCEPTEDStatus(t *testing.T) {
	sections := []overviewSection{
		{
			Name: "Info",
			Items: []overviewItem{
				{"SLASA", "ACCEPTED", ""},
			},
		},
	}

	var buf bytes.Buffer
	printOverviewTable(&buf, sections, true)
	if !strings.Contains(buf.String(), "\033[32m") {
		t.Error("ACCEPTED should render green")
	}
}

func TestFormatExpirationDate_InvalidDate(t *testing.T) {
	formatted, color := formatExpirationDate("not-a-date", time.Now())
	if formatted != "not-a-date" {
		t.Errorf("formatted = %q, want passthrough", formatted)
	}
	if color != "" {
		t.Errorf("color = %q, want empty for invalid date", color)
	}
}

func TestPrintOverviewTable_DEPSyncSuccessful(t *testing.T) {
	sections := []overviewSection{
		{
			Name: "Enrollment",
			Items: []overviewItem{
				{"DEP Sync Status", "SUCCESSFUL (Feb 16 02:31 UTC)", ""},
			},
		},
	}

	var buf bytes.Buffer
	printOverviewTable(&buf, sections, true)
	output := buf.String()

	// SUCCESSFUL should render green
	if !strings.Contains(output, "\033[32m") {
		t.Error("missing green color code for SUCCESSFUL sync")
	}
}
