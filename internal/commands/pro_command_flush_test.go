// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// --- flushStatusMap ---

func TestFlushStatusMap_Values(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"failed", "Failed"},
		{"pending", "Pending"},
		{"both", "Pending+Failed"},
	}
	for _, c := range cases {
		t.Run(c.input, func(t *testing.T) {
			got, ok := flushStatusMap[c.input]
			if !ok {
				t.Fatalf("key %q not in flushStatusMap", c.input)
			}
			if got != c.want {
				t.Errorf("flushStatusMap[%q] = %q, want %q", c.input, got, c.want)
			}
		})
	}
}

func TestFlushStatusMap_InvalidRejected(t *testing.T) {
	for _, bad := range []string{"Failed", "PENDING", "all", "", "both+extra"} {
		if _, ok := flushStatusMap[bad]; ok {
			t.Errorf("expected %q to be absent from flushStatusMap", bad)
		}
	}
}

// --- Computer flush-commands ---

// flushMockClient records DELETE calls and can serve computer/group lookups.
type flushMockClient struct {
	responses    map[string]flushMockResponse
	deletedPaths []string
}

type flushMockResponse struct {
	status int
	body   string
}

func (m *flushMockClient) Do(_ context.Context, method, path string, _ io.Reader) (*http.Response, error) {
	if method == "DELETE" {
		m.deletedPaths = append(m.deletedPaths, path)
	}
	key := method + " " + path
	// Longest-match-wins
	bestPattern := ""
	var bestResp flushMockResponse
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

// discardOutput implements registry.OutputFormatter for tests that don't care about output.
type discardOutput struct{}

func (d *discardOutput) PrintResponse(_ *http.Response) error { return nil }
func (d *discardOutput) PrintRaw(_ []byte) error              { return nil }
func (d *discardOutput) PrintBytes(_ []byte) error            { return nil }
func (d *discardOutput) Format() string                       { return "json" }

const classicGroupXML = `<?xml version="1.0" encoding="UTF-8"?>
<computer_group>
  <id>7</id>
  <name>Lab Macs</name>
  <is_smart>true</is_smart>
</computer_group>`

const classicMobileGroupXML = `<?xml version="1.0" encoding="UTF-8"?>
<mobile_device_group>
  <id>12</id>
  <name>Lab iPads</name>
  <is_smart>false</is_smart>
</mobile_device_group>`

const commandFlushXML = `<?xml version="1.0" encoding="UTF-8"?>
<commandflush>
  <status>+failed</status>
  <computers>42</computers>
</commandflush>`

const groupFlushXML = `<?xml version="1.0" encoding="UTF-8"?>
<commandflush>
  <status>+failed</status>
  <computer_groups>7</computer_groups>
</commandflush>`

func TestComputerFlushCommands_DryRun_ByID(t *testing.T) {
	resetGlobals()
	dryRun = true
	defer func() { dryRun = false }()

	mock := &flushMockClient{
		responses: map[string]flushMockResponse{
			"v3/computers-inventory/42": {200, `{"id":"42","udid":"U","general":{"name":"Test Mac","managementId":"m"},"hardware":{"serialNumber":"C02X1234"}}`},
		},
	}
	cliCtx := &registry.CLIContext{Client: mock, Output: &discardOutput{}}
	cmd := newComputerFlushCommandsCmd(cliCtx)
	cmd.Flags().Bool("no-input", false, "")
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)

	cmd.SetArgs([]string{"--id", "42", "--status", "failed"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.deletedPaths) != 0 {
		t.Errorf("dry-run: expected no DELETE calls, got %v", mock.deletedPaths)
	}
	if !strings.Contains(stderr.String(), "[dry-run]") {
		t.Errorf("expected dry-run output, got: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "failed") {
		t.Errorf("expected status in dry-run message, got: %q", stderr.String())
	}
}

func TestComputerFlushCommands_DryRun_ByGroup(t *testing.T) {
	resetGlobals()
	dryRun = true
	defer func() { dryRun = false }()

	mock := &flushMockClient{
		responses: map[string]flushMockResponse{
			"GET /JSSResource/computergroups/name/Lab%20Macs": {200, classicGroupXML},
		},
	}
	cliCtx := &registry.CLIContext{Client: mock, Output: &discardOutput{}}
	cmd := newComputerFlushCommandsCmd(cliCtx)
	cmd.Flags().Bool("no-input", false, "")
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)

	cmd.SetArgs([]string{"--group", "Lab Macs", "--status", "failed"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.deletedPaths) != 0 {
		t.Errorf("dry-run: expected no DELETE calls, got %v", mock.deletedPaths)
	}
	if !strings.Contains(stderr.String(), "[dry-run]") {
		t.Errorf("expected dry-run output, got: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "id: 7") {
		t.Errorf("expected resolved group ID in dry-run message, got: %q", stderr.String())
	}
}

func TestComputerFlushCommands_InvalidStatus(t *testing.T) {
	resetGlobals()

	mock := &flushMockClient{}
	cliCtx := &registry.CLIContext{Client: mock, Output: &discardOutput{}}
	cmd := newComputerFlushCommandsCmd(cliCtx)
	cmd.Flags().Bool("no-input", false, "")
	cmd.SetArgs([]string{"--id", "42", "--status", "bogus"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid status")
		return
	}
	if !strings.Contains(err.Error(), "invalid --status") {
		t.Errorf("error = %q, want to contain 'invalid --status'", err.Error())
	}
	if len(mock.deletedPaths) != 0 {
		t.Errorf("expected no DELETE calls on invalid status, got %v", mock.deletedPaths)
	}
}

func TestComputerFlushCommands_NoInputGuard(t *testing.T) {
	resetGlobals()

	mock := &flushMockClient{
		responses: map[string]flushMockResponse{
			"v3/computers-inventory/42": {200, `{"id":"42","udid":"U","general":{"name":"Test Mac","managementId":"m"},"hardware":{"serialNumber":"C02X1234"}}`},
		},
	}
	cliCtx := &registry.CLIContext{Client: mock, Output: &discardOutput{}}
	cmd := newComputerFlushCommandsCmd(cliCtx)
	cmd.Flags().Bool("no-input", false, "")
	_ = cmd.Flags().Set("no-input", "true")
	cmd.SetArgs([]string{"--id", "42"}) // no --yes

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when --no-input without --yes")
		return
	}
	if !strings.Contains(err.Error(), "--yes") {
		t.Errorf("error = %q, want to contain '--yes'", err.Error())
	}
	if len(mock.deletedPaths) != 0 {
		t.Errorf("expected no DELETE calls, got %v", mock.deletedPaths)
	}
}

func TestComputerFlushCommands_WithYes_ByID(t *testing.T) {
	resetGlobals()

	mock := &flushMockClient{
		responses: map[string]flushMockResponse{
			"v3/computers-inventory/42":                         {200, `{"id":"42","udid":"U","general":{"name":"Test Mac","managementId":"m"},"hardware":{"serialNumber":"C02X1234"}}`},
			"DELETE /JSSResource/commandflush/computers/id/42/": {200, commandFlushXML},
		},
	}
	cliCtx := &registry.CLIContext{Client: mock, Output: &discardOutput{}}
	cmd := newComputerFlushCommandsCmd(cliCtx)
	cmd.Flags().Bool("no-input", false, "")
	cmd.SetArgs([]string{"--id", "42", "--status", "failed", "--yes"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.deletedPaths) != 1 {
		t.Fatalf("expected 1 DELETE call, got %v", mock.deletedPaths)
	}
	if !strings.Contains(mock.deletedPaths[0], "/JSSResource/commandflush/computers/id/42/status/Failed") {
		t.Errorf("unexpected DELETE path: %s", mock.deletedPaths[0])
	}
}

func TestComputerFlushCommands_WithYes_ByGroup(t *testing.T) {
	resetGlobals()

	mock := &flushMockClient{
		responses: map[string]flushMockResponse{
			"GET /JSSResource/computergroups/name/Lab%20Macs":             {200, classicGroupXML},
			"DELETE /JSSResource/commandflush/computergroups/id/7/status": {200, groupFlushXML},
		},
	}
	cliCtx := &registry.CLIContext{Client: mock, Output: &discardOutput{}}
	cmd := newComputerFlushCommandsCmd(cliCtx)
	cmd.Flags().Bool("no-input", false, "")
	cmd.SetArgs([]string{"--group", "Lab Macs", "--status", "failed", "--yes"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.deletedPaths) != 1 {
		t.Fatalf("expected 1 DELETE call, got %v", mock.deletedPaths)
	}
	if !strings.Contains(mock.deletedPaths[0], "/JSSResource/commandflush/computergroups/id/7/status/Failed") {
		t.Errorf("unexpected DELETE path: %s", mock.deletedPaths[0])
	}
}

func TestComputerFlushCommands_StatusBoth_URLEncoded(t *testing.T) {
	resetGlobals()

	mock := &flushMockClient{
		responses: map[string]flushMockResponse{
			"v3/computers-inventory/42":                         {200, `{"id":"42","udid":"U","general":{"name":"Test Mac","managementId":"m"},"hardware":{"serialNumber":"C02X1234"}}`},
			"DELETE /JSSResource/commandflush/computers/id/42/": {200, commandFlushXML},
		},
	}
	cliCtx := &registry.CLIContext{Client: mock, Output: &discardOutput{}}
	cmd := newComputerFlushCommandsCmd(cliCtx)
	cmd.Flags().Bool("no-input", false, "")
	cmd.SetArgs([]string{"--id", "42", "--status", "both", "--yes"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.deletedPaths) != 1 {
		t.Fatalf("expected 1 DELETE call, got %v", mock.deletedPaths)
	}
	// "Pending+Failed" — url.PathEscape keeps '+' as '+' in path segments (RFC 3986).
	// The Jamf Classic API accepts the literal '+' in the path, as confirmed by live testing.
	if !strings.Contains(mock.deletedPaths[0], "status/Pending+Failed") {
		t.Errorf("expected 'status/Pending+Failed' in path, got: %s", mock.deletedPaths[0])
	}
}

func TestComputerFlushCommands_GroupWithoutYes_DoesNotExecute(t *testing.T) {
	resetGlobals()

	mock := &flushMockClient{
		responses: map[string]flushMockResponse{
			"GET /JSSResource/computergroups/name/Lab%20Macs": {200, classicGroupXML},
		},
	}
	cliCtx := &registry.CLIContext{Client: mock, Output: &discardOutput{}}
	cmd := newComputerFlushCommandsCmd(cliCtx)
	cmd.Flags().Bool("no-input", false, "")
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--group", "Lab Macs", "--status", "failed"}) // no --yes

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.deletedPaths) != 0 {
		t.Errorf("expected no DELETE calls without --yes, got %v", mock.deletedPaths)
	}
	if !strings.Contains(stderr.String(), "--yes") {
		t.Errorf("expected '--yes' hint in output, got: %q", stderr.String())
	}
}

func TestComputerFlushCommands_NoInputGroup_Errors(t *testing.T) {
	resetGlobals()

	mock := &flushMockClient{
		responses: map[string]flushMockResponse{
			"GET /JSSResource/computergroups/name/Lab%20Macs": {200, classicGroupXML},
		},
	}
	cliCtx := &registry.CLIContext{Client: mock, Output: &discardOutput{}}
	cmd := newComputerFlushCommandsCmd(cliCtx)
	cmd.Flags().Bool("no-input", false, "")
	_ = cmd.Flags().Set("no-input", "true")
	cmd.SetArgs([]string{"--group", "Lab Macs", "--status", "failed"}) // no --yes

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when --no-input without --yes for group target")
		return
	}
	if !strings.Contains(err.Error(), "--yes") {
		t.Errorf("error = %q, want to contain '--yes'", err.Error())
	}
	if len(mock.deletedPaths) != 0 {
		t.Errorf("expected no DELETE calls, got %v", mock.deletedPaths)
	}
}

// --- Mobile flush-commands ---

func TestMobileFlushCommands_DryRun_ByID(t *testing.T) {
	resetGlobals()
	dryRun = true
	defer func() { dryRun = false }()

	mock := &flushMockClient{
		responses: map[string]flushMockResponse{
			"v2/mobile-devices/99": {200, `{"id":"99","managementId":"mgmt","udid":"U","name":"Lab iPad","serialNumber":"F4GH5678"}`},
		},
	}
	cliCtx := &registry.CLIContext{Client: mock, Output: &discardOutput{}}
	cmd := newMobileFlushCommandsCmd(cliCtx)
	cmd.Flags().Bool("no-input", false, "")
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)

	cmd.SetArgs([]string{"--id", "99", "--status", "failed"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.deletedPaths) != 0 {
		t.Errorf("dry-run: expected no DELETE calls, got %v", mock.deletedPaths)
	}
	if !strings.Contains(stderr.String(), "[dry-run]") {
		t.Errorf("expected dry-run output, got: %q", stderr.String())
	}
}

func TestMobileFlushCommands_WithYes_ByGroup(t *testing.T) {
	resetGlobals()

	mock := &flushMockClient{
		responses: map[string]flushMockResponse{
			"GET /JSSResource/mobiledevicegroups/name/Lab%20iPads":              {200, classicMobileGroupXML},
			"DELETE /JSSResource/commandflush/mobiledevicegroups/id/12/status/": {200, `<commandflush><status>+failed</status><mobile_device_groups>12</mobile_device_groups></commandflush>`},
		},
	}
	cliCtx := &registry.CLIContext{Client: mock, Output: &discardOutput{}}
	cmd := newMobileFlushCommandsCmd(cliCtx)
	cmd.Flags().Bool("no-input", false, "")
	cmd.SetArgs([]string{"--group", "Lab iPads", "--status", "failed", "--yes"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.deletedPaths) != 1 {
		t.Fatalf("expected 1 DELETE call, got %v", mock.deletedPaths)
	}
	if !strings.Contains(mock.deletedPaths[0], "/JSSResource/commandflush/mobiledevicegroups/id/12/status/Failed") {
		t.Errorf("unexpected DELETE path: %s", mock.deletedPaths[0])
	}
}

func TestMobileFlushCommands_NoInputGroup_Errors(t *testing.T) {
	resetGlobals()

	mock := &flushMockClient{
		responses: map[string]flushMockResponse{
			"GET /JSSResource/mobiledevicegroups/name/Lab%20iPads": {200, classicMobileGroupXML},
		},
	}
	cliCtx := &registry.CLIContext{Client: mock, Output: &discardOutput{}}
	cmd := newMobileFlushCommandsCmd(cliCtx)
	cmd.Flags().Bool("no-input", false, "")
	_ = cmd.Flags().Set("no-input", "true")
	cmd.SetArgs([]string{"--group", "Lab iPads", "--status", "failed"}) // no --yes

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when --no-input without --yes for group target")
		return
	}
	if !strings.Contains(err.Error(), "--yes") {
		t.Errorf("error = %q, want to contain '--yes'", err.Error())
	}
	if len(mock.deletedPaths) != 0 {
		t.Errorf("expected no DELETE calls, got %v", mock.deletedPaths)
	}
}

func TestMobileFlushCommands_NoTargetFlag_Errors(t *testing.T) {
	resetGlobals()

	mock := &flushMockClient{}
	cliCtx := &registry.CLIContext{Client: mock, Output: &discardOutput{}}
	cmd := newMobileFlushCommandsCmd(cliCtx)
	cmd.Flags().Bool("no-input", false, "")
	cmd.SetArgs([]string{"--status", "failed", "--yes"}) // no target

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when no target flag provided")
		return
	}
	if !strings.Contains(err.Error(), "required") {
		t.Errorf("error = %q, want to contain 'required'", err.Error())
	}
}
