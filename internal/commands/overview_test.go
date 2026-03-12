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
)

// overviewMockClient implements generated.HTTPClient for testing overview fetches.
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
	defer resp.Body.Close()

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

func TestFormatEpochDate(t *testing.T) {
	tests := []struct {
		epoch float64
		want  string
	}{
		{0, "Jan 01, 1970"},
		{1924905600, "Dec 31, 2030"},
		{1735689600, "Jan 01, 2025"},
	}

	for _, tt := range tests {
		got := formatEpochDate(tt.epoch)
		if got != tt.want {
			t.Errorf("formatEpochDate(%v) = %q, want %q", tt.epoch, got, tt.want)
		}
	}
}

func TestFormatExpirationDate(t *testing.T) {
	now := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		dateStr   string
		wantColor string
		wantWord  string // substring to check in formatted output
	}{
		{"2026-01-10", "red", "expired"},          // past
		{"2026-01-20", "red", "5 days"},            // <30 days
		{"2026-03-01", "yellow", "45 days"},        // 30-90 days
		{"2026-06-01", "", "Jun 01, 2026"},         // >90 days, no color
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
