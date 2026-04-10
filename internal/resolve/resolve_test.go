// Copyright 2026, Jamf Software LLC

package resolve

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mockClient implements registry.HTTPClient for testing.
type mockClient struct {
	responses map[string]mockResponse
}

type mockResponse struct {
	status int
	body   string
}

func (m *mockClient) Do(_ context.Context, method, path string, _ io.Reader) (*http.Response, error) {
	key := method + " " + path
	// Longest-match-wins: prefer more specific patterns.
	bestPattern := ""
	var bestResp mockResponse
	for pattern, resp := range m.responses {
		if strings.Contains(key, pattern) && len(pattern) > len(bestPattern) {
			bestPattern = pattern
			bestResp = resp
		}
	}
	if bestPattern != "" {
		return &http.Response{
			StatusCode: bestResp.status,
			Body:       io.NopCloser(strings.NewReader(bestResp.body)),
			Header:     make(http.Header),
		}, nil
	}
	return &http.Response{
		StatusCode: http.StatusNotFound,
		Body:       io.NopCloser(strings.NewReader(`{"httpStatus":404}`)),
		Header:     make(http.Header),
	}, nil
}

const computerV3Response = `{
	"totalCount": 1,
	"results": [{
		"id": "42",
		"udid": "AAAA-BBBB-CCCC",
		"general": {
			"name": "Neil's MacBook",
			"managementId": "73226fb6-61df-4c10-9552-eb9bc353d507"
		},
		"hardware": {
			"serialNumber": "C02X1234"
		}
	}]
}`

const computerV3DetailResponse = `{
	"id": "42",
	"udid": "AAAA-BBBB-CCCC",
	"general": {
		"name": "Neil's MacBook",
		"managementId": "73226fb6-61df-4c10-9552-eb9bc353d507"
	},
	"hardware": {
		"serialNumber": "C02X1234"
	}
}`

const mobileV2Response = `{
	"totalCount": 1,
	"results": [{
		"mobileDeviceId": "99",
		"managementId": "mgmt-uuid-mobile",
		"udid": "MOBILE-UDID",
		"displayName": "Lab iPad",
		"serialNumber": "F4GH5678"
	}]
}`

const mobileV2DetailResponse = `{
	"id": "99",
	"managementId": "mgmt-uuid-mobile",
	"udid": "MOBILE-UDID",
	"name": "Lab iPad",
	"serialNumber": "F4GH5678"
}`

func TestResolveComputer_BySerial(t *testing.T) {
	client := &mockClient{responses: map[string]mockResponse{
		"v3/computers-inventory?": {200, computerV3Response},
	}}
	d, err := ResolveComputer(context.Background(), client, "C02X1234", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.ID != "42" {
		t.Errorf("ID = %q, want %q", d.ID, "42")
	}
	if d.SerialNumber != "C02X1234" {
		t.Errorf("SerialNumber = %q, want %q", d.SerialNumber, "C02X1234")
	}
	if d.ManagementID != "73226fb6-61df-4c10-9552-eb9bc353d507" {
		t.Errorf("ManagementID = %q, want UUID", d.ManagementID)
	}
	if d.UDID != "AAAA-BBBB-CCCC" {
		t.Errorf("UDID = %q, want %q", d.UDID, "AAAA-BBBB-CCCC")
	}
	if d.Name != "Neil's MacBook" {
		t.Errorf("Name = %q, want %q", d.Name, "Neil's MacBook")
	}
}

func TestResolveComputer_ByName(t *testing.T) {
	client := &mockClient{responses: map[string]mockResponse{
		"v3/computers-inventory?": {200, computerV3Response},
	}}
	d, err := ResolveComputer(context.Background(), client, "", "Neil's MacBook", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.ID != "42" {
		t.Errorf("ID = %q, want %q", d.ID, "42")
	}
}

func TestResolveComputer_ByID(t *testing.T) {
	client := &mockClient{responses: map[string]mockResponse{
		"v3/computers-inventory/42": {200, computerV3DetailResponse},
	}}
	d, err := ResolveComputer(context.Background(), client, "", "", "42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.ID != "42" {
		t.Errorf("ID = %q, want %q", d.ID, "42")
	}
	if d.SerialNumber != "C02X1234" {
		t.Errorf("SerialNumber = %q, want %q", d.SerialNumber, "C02X1234")
	}
}

func TestResolveComputer_NotFound(t *testing.T) {
	client := &mockClient{responses: map[string]mockResponse{
		"v3/computers-inventory?": {200, `{"totalCount": 0, "results": []}`},
	}}
	_, err := ResolveComputer(context.Background(), client, "NOSUCH", "", "")
	if err == nil {
		t.Fatal("expected error for not found")
	}
	if !strings.Contains(err.Error(), "no computer found") {
		t.Errorf("error = %q, want to contain 'no computer found'", err.Error())
	}
}

func TestResolveComputer_MultipleMatches(t *testing.T) {
	multiResponse := `{
		"totalCount": 3,
		"results": [
			{"id": "1", "udid": "U1", "general": {"name": "Mac", "managementId": "m1"}, "hardware": {"serialNumber": "S1"}},
			{"id": "2", "udid": "U2", "general": {"name": "Mac", "managementId": "m2"}, "hardware": {"serialNumber": "S2"}}
		]
	}`
	client := &mockClient{responses: map[string]mockResponse{
		"v3/computers-inventory?": {200, multiResponse},
	}}
	_, err := ResolveComputer(context.Background(), client, "", "Mac", "")
	if err == nil {
		t.Fatal("expected error for multiple matches")
	}
	if !strings.Contains(err.Error(), "multiple computers found") {
		t.Errorf("error = %q, want to contain 'multiple computers found'", err.Error())
	}
}

func TestResolveComputer_NoFlags(t *testing.T) {
	client := &mockClient{}
	_, err := ResolveComputer(context.Background(), client, "", "", "")
	if err == nil {
		t.Fatal("expected error when no flags provided")
	}
	if !strings.Contains(err.Error(), "required") {
		t.Errorf("error = %q, want to contain 'required'", err.Error())
	}
}

func TestResolveMobileDevice_BySerial(t *testing.T) {
	client := &mockClient{responses: map[string]mockResponse{
		"v2/mobile-devices/detail?": {200, mobileV2Response},
	}}
	d, err := ResolveMobileDevice(context.Background(), client, "F4GH5678", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.ID != "99" {
		t.Errorf("ID = %q, want %q", d.ID, "99")
	}
	if d.SerialNumber != "F4GH5678" {
		t.Errorf("SerialNumber = %q, want %q", d.SerialNumber, "F4GH5678")
	}
	if d.ManagementID != "mgmt-uuid-mobile" {
		t.Errorf("ManagementID = %q, want %q", d.ManagementID, "mgmt-uuid-mobile")
	}
}

func TestResolveMobileDevice_ByID(t *testing.T) {
	client := &mockClient{responses: map[string]mockResponse{
		"v2/mobile-devices/99": {200, mobileV2DetailResponse},
	}}
	d, err := ResolveMobileDevice(context.Background(), client, "", "", "99")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Name != "Lab iPad" {
		t.Errorf("Name = %q, want %q", d.Name, "Lab iPad")
	}
}

func TestResolveComputersFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "targets.txt")
	content := "# Comment\nC02X1234\n\n42\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	client := &mockClient{responses: map[string]mockResponse{
		"v3/computers-inventory?":  {200, computerV3Response},
		"v3/computers-inventory/4": {200, computerV3DetailResponse},
	}}

	results, err := ResolveComputersFromFile(context.Background(), client, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
}

func TestReadEntriesFromFile_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(path, []byte("# only comments\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := readEntriesFromFile(path)
	if err == nil {
		t.Fatal("expected error for empty file")
	}
	if !strings.Contains(err.Error(), "no entries") {
		t.Errorf("error = %q, want to contain 'no entries'", err.Error())
	}
}

func TestIsNumericID(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"42", true},
		{"0", true},
		{"123456", true},
		{"C02X1234", false},
		{"", false},
		{"12abc", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := isNumericID(tt.input); got != tt.want {
				t.Errorf("isNumericID(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestFormatDeviceDesc(t *testing.T) {
	d := &DeviceIdentifiers{
		ID:           "42",
		Name:         "Neil's MacBook",
		SerialNumber: "C02X1234",
	}
	got := FormatDeviceDesc(d)
	want := `"Neil's MacBook" (serial: C02X1234, id: 42)`
	if got != want {
		t.Errorf("FormatDeviceDesc = %q, want %q", got, want)
	}
}

func TestFormatDeviceDesc_NoSerial(t *testing.T) {
	d := &DeviceIdentifiers{ID: "42", Name: "Test"}
	got := FormatDeviceDesc(d)
	if !strings.Contains(got, "id: 42") {
		t.Errorf("FormatDeviceDesc = %q, want to contain 'id: 42'", got)
	}
}

func TestEscapeRSQL(t *testing.T) {
	got := EscapeRSQL(`Neil's "MacBook"`)
	want := `Neil's \"MacBook\"`
	if got != want {
		t.Errorf("EscapeRSQL = %q, want %q", got, want)
	}
}

func TestResolveClassicComputerGroupID(t *testing.T) {
	groupXML := `<?xml version="1.0" encoding="UTF-8"?>
<computer_group>
  <id>7</id>
  <name>Lab Macs</name>
  <is_smart>true</is_smart>
</computer_group>`

	client := &mockClient{responses: map[string]mockResponse{
		"GET /JSSResource/computergroups/name/Lab%20Macs": {200, groupXML},
	}}

	id, err := ResolveClassicComputerGroupID(context.Background(), client, "Lab Macs")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "7" {
		t.Errorf("ID = %q, want %q", id, "7")
	}
}

func TestResolveClassicComputerGroupID_NotFound(t *testing.T) {
	client := &mockClient{responses: map[string]mockResponse{
		"GET /JSSResource/computergroups/name/NoSuch": {404, `<error><code>404</code></error>`},
	}}

	_, err := ResolveClassicComputerGroupID(context.Background(), client, "NoSuch")
	if err == nil {
		t.Fatal("expected error for 404")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want to contain 'not found'", err.Error())
	}
}

func TestResolveClassicMobileGroupID(t *testing.T) {
	groupXML := `<?xml version="1.0" encoding="UTF-8"?>
<mobile_device_group>
  <id>12</id>
  <name>Lab iPads</name>
  <is_smart>false</is_smart>
</mobile_device_group>`

	client := &mockClient{responses: map[string]mockResponse{
		"GET /JSSResource/mobiledevicegroups/name/Lab%20iPads": {200, groupXML},
	}}

	id, err := ResolveClassicMobileGroupID(context.Background(), client, "Lab iPads")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "12" {
		t.Errorf("ID = %q, want %q", id, "12")
	}
}

func TestResolveClassicMobileGroupID_NotFound(t *testing.T) {
	client := &mockClient{responses: map[string]mockResponse{
		"GET /JSSResource/mobiledevicegroups/name/NoSuch": {404, `<error><code>404</code></error>`},
	}}

	_, err := ResolveClassicMobileGroupID(context.Background(), client, "NoSuch")
	if err == nil {
		t.Fatal("expected error for 404")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want to contain 'not found'", err.Error())
	}
}

func TestResolveComputerGroup(t *testing.T) {
	groupListResponse := `{"computer_groups": [{"id": "5", "name": "All Macs"}]}`
	groupDetailResponse := `{"computer_group": {"id": "5", "name": "All Macs", "computers": [{"id": "42", "name": "Mac1"}, {"id": "43", "name": "Mac2"}]}}`

	client := &mockClient{responses: map[string]mockResponse{
		"GET /JSSResource/computergroups":  {200, groupListResponse},
		"/JSSResource/computergroups/id/5": {200, groupDetailResponse},
		"v3/computers-inventory/42":        {200, computerV3DetailResponse},
		"v3/computers-inventory/43":        {200, strings.ReplaceAll(computerV3DetailResponse, `"42"`, `"43"`)},
	}}

	results, err := ResolveComputerGroup(context.Background(), client, "All Macs")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("got %d results, want 2", len(results))
	}
}
