// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamf-cli/internal/resolve"
)

func TestDeviceTarget_Validate(t *testing.T) {
	tests := []struct {
		name    string
		dt      deviceTarget
		wantErr string
	}{
		{"serial", deviceTarget{serial: "C02X"}, ""},
		{"name", deviceTarget{name: "Mac"}, ""},
		{"id", deviceTarget{id: "42"}, ""},
		{"group", deviceTarget{group: "All"}, ""},
		{"from-file", deviceTarget{fromFile: "f.txt"}, ""},
		{"none", deviceTarget{}, "required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.dt.validate()
			if tt.wantErr == "" && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("expected error")
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error = %q, want to contain %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

func TestDeviceTarget_IsBulk(t *testing.T) {
	if (&deviceTarget{serial: "C02X"}).isBulk() {
		t.Error("serial should not be bulk")
	}
	if !(&deviceTarget{group: "All"}).isBulk() {
		t.Error("group should be bulk")
	}
	if !(&deviceTarget{fromFile: "f.txt"}).isBulk() {
		t.Error("from-file should be bulk")
	}
}

func TestComputerActionSubcommands_Exist(t *testing.T) {
	resetGlobals()
	root := NewRootCmd("test", "abc", "2024-01-01")

	wantComputer := []string{
		"erase", "remove-mdm", "redeploy-framework",
		"blank-push", "ddm-sync", "renew-mdm",
		"lock", "enable-remote-desktop", "disable-remote-desktop",
		"restart", "shutdown", "set-recovery-lock",
	}
	for _, name := range wantComputer {
		t.Run("computers/"+name, func(t *testing.T) {
			root.SetArgs([]string{"pro", "computers", name, "--help"})
			if err := root.Execute(); err != nil {
				t.Errorf("command 'pro computers %s --help' failed: %v", name, err)
			}
		})
	}
}

func TestMobileActionSubcommands_Exist(t *testing.T) {
	resetGlobals()
	root := NewRootCmd("test", "abc", "2024-01-01")

	wantMobile := []string{
		"erase", "unmanage",
		"restart", "shutdown", "update-inventory",
		"lock", "clear-passcode", "enable-lost-mode", "disable-lost-mode",
		"play-lost-mode-sound", "clear-restrictions-password",
	}
	for _, name := range wantMobile {
		t.Run("mobile-devices/"+name, func(t *testing.T) {
			root.SetArgs([]string{"pro", "mobile-devices", name, "--help"})
			if err := root.Execute(); err != nil {
				t.Errorf("command 'pro mobile-devices %s --help' failed: %v", name, err)
			}
		})
	}
}

func TestComputerAliasSubcommands(t *testing.T) {
	resetGlobals()
	root := NewRootCmd("test", "abc", "2024-01-01")
	root.SetArgs([]string{"pro", "comp", "erase", "--help"})
	if err := root.Execute(); err != nil {
		t.Errorf("alias 'pro comp erase --help' failed: %v", err)
	}
}

func TestMobileAliasSubcommands(t *testing.T) {
	resetGlobals()
	root := NewRootCmd("test", "abc", "2024-01-01")
	root.SetArgs([]string{"pro", "md", "erase", "--help"})
	if err := root.Execute(); err != nil {
		t.Errorf("alias 'pro md erase --help' failed: %v", err)
	}
}

func TestComputerEraseScaffold(t *testing.T) {
	resetGlobals()
	root := NewRootCmd("test", "abc", "2024-01-01")
	root.SetArgs([]string{"pro", "computers", "erase", "--scaffold"})
	if err := root.Execute(); err != nil {
		t.Errorf("scaffold failed: %v", err)
	}
}

func TestMobileEraseScaffold(t *testing.T) {
	resetGlobals()
	root := NewRootCmd("test", "abc", "2024-01-01")
	root.SetArgs([]string{"pro", "mobile-devices", "erase", "--scaffold"})
	if err := root.Execute(); err != nil {
		t.Errorf("scaffold failed: %v", err)
	}
}

// --- Safety gate tests ---

// newTestCmd creates a minimal cobra command for testing executeAction.
func newTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().Bool("no-input", false, "")
	return cmd
}

func TestExecuteAction_DryRun(t *testing.T) {
	resetGlobals()
	dryRun = true
	defer func() { dryRun = false }()

	var stderr bytes.Buffer
	cmd := newTestCmd()
	cmd.SetErr(&stderr)

	devices := []*resolve.DeviceIdentifiers{
		{ID: "1", Name: "Mac-1", SerialNumber: "C02X1"},
		{ID: "2", Name: "Mac-2", SerialNumber: "C02X2"},
	}
	dt := &deviceTarget{group: "TestGroup"}
	called := false
	cfg := deviceActionConfig{
		actionName: "blank-push",
		deviceType: "computer",
		execSingle: func(_ *resolve.DeviceIdentifiers, _ io.Reader) error {
			called = true
			return nil
		},
	}

	err := executeAction(cmd, dt, devices, true, false, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Error("execSingle should not be called in dry-run mode")
	}
	if !strings.Contains(stderr.String(), "[dry-run]") {
		t.Errorf("expected dry-run output, got: %s", stderr.String())
	}
}

func TestExecuteAction_BulkDestructive_BlockedWithoutConfirmDestructive(t *testing.T) {
	resetGlobals()

	var stderr bytes.Buffer
	cmd := newTestCmd()
	cmd.SetErr(&stderr)

	devices := []*resolve.DeviceIdentifiers{
		{ID: "1", Name: "Mac-1"},
		{ID: "2", Name: "Mac-2"},
	}
	dt := &deviceTarget{group: "TestGroup"}
	called := false
	cfg := deviceActionConfig{
		actionName:  "erase",
		deviceType:  "computer",
		destructive: true,
		execSingle: func(_ *resolve.DeviceIdentifiers, _ io.Reader) error {
			called = true
			return nil
		},
	}

	// yes=true but confirmDestructive=false → should be blocked
	err := executeAction(cmd, dt, devices, true, false, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Error("execSingle should not be called without --confirm-destructive")
	}
	if !strings.Contains(stderr.String(), "--confirm-destructive") {
		t.Errorf("expected --confirm-destructive warning, got: %s", stderr.String())
	}
}

func TestExecuteAction_BulkDestructive_ProceedsWithBothFlags(t *testing.T) {
	resetGlobals()

	var stderr bytes.Buffer
	cmd := newTestCmd()
	cmd.SetErr(&stderr)

	devices := []*resolve.DeviceIdentifiers{
		{ID: "1", Name: "Mac-1", SerialNumber: "C02X1"},
	}
	dt := &deviceTarget{group: "TestGroup"}
	callCount := 0
	cfg := deviceActionConfig{
		actionName:  "erase",
		deviceType:  "computer",
		destructive: true,
		execSingle: func(_ *resolve.DeviceIdentifiers, _ io.Reader) error {
			callCount++
			return nil
		},
	}

	// yes=true, confirmDestructive=true → should proceed
	err := executeAction(cmd, dt, devices, true, true, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected execSingle called once, got %d", callCount)
	}
}

func TestExecuteAction_SingleDestructive_NoInput_RequiresYes(t *testing.T) {
	resetGlobals()

	cmd := newTestCmd()
	cmd.SetErr(&bytes.Buffer{})
	_ = cmd.Flags().Set("no-input", "true")

	devices := []*resolve.DeviceIdentifiers{
		{ID: "1", Name: "Mac-1"},
	}
	dt := &deviceTarget{serial: "C02X1"}
	cfg := deviceActionConfig{
		actionName:  "erase",
		deviceType:  "computer",
		destructive: true,
		execSingle: func(_ *resolve.DeviceIdentifiers, _ io.Reader) error {
			t.Error("execSingle should not be called")
			return nil
		},
	}

	// yes=false, no-input=true → should error
	err := executeAction(cmd, dt, devices, false, false, cfg)
	if err == nil {
		t.Fatal("expected error for destructive op without --yes in no-input mode")
	}
	if !strings.Contains(err.Error(), "--yes") {
		t.Errorf("error should mention --yes, got: %v", err)
	}
}

// mockOutput is a no-op OutputFormatter for tests that don't need output assertions.
type mockOutput struct{}

func (mockOutput) PrintResponse(_ *http.Response) error { return nil }
func (mockOutput) PrintRaw(_ []byte) error              { return nil }
func (mockOutput) PrintBytes(_ []byte) error            { return nil }
func (mockOutput) Format() string                       { return "json" }

// bodyCapturingClient captures the request body for assertion in tests.
type bodyCapturingClient struct {
	capturedBody string
}

func (c *bodyCapturingClient) Do(_ context.Context, _, _ string, body io.Reader) (*http.Response, error) {
	if body != nil {
		b, _ := io.ReadAll(body)
		c.capturedBody = string(b)
	}
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(`{}`)),
		Header:     make(http.Header),
	}, nil
}

func TestSendMobileModernMDMCommand_Success(t *testing.T) {
	client := &bodyCapturingClient{}
	cmd := &cobra.Command{Use: "test"}
	cliCtx := &registry.CLIContext{Client: client, Output: mockOutput{}}
	d := &resolve.DeviceIdentifiers{ID: "7", ManagementID: "aaaa-bbbb", Name: "iPad-1"}

	err := sendMobileModernMDMCommand(cmd, cliCtx, d, map[string]any{"commandType": "RESTART_DEVICE"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(client.capturedBody, `"RESTART_DEVICE"`) {
		t.Errorf("request body = %q, want it to contain commandType", client.capturedBody)
	}
	if !strings.Contains(client.capturedBody, `"aaaa-bbbb"`) {
		t.Errorf("request body = %q, want it to contain managementId", client.capturedBody)
	}
}

func TestSendMobileModernMDMCommand_MissingManagementID(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	cliCtx := &registry.CLIContext{}
	d := &resolve.DeviceIdentifiers{ID: "7", Name: "iPad-1"} // no ManagementID

	err := sendMobileModernMDMCommand(cmd, cliCtx, d, map[string]any{"commandType": "RESTART_DEVICE"})
	if err == nil {
		t.Fatal("expected error for missing managementId, got nil")
	}
	if !strings.Contains(err.Error(), "managementId") {
		t.Errorf("error = %q, want it to mention managementId", err.Error())
	}
}

func TestSendComputerModernMDMCommand_Success(t *testing.T) {
	client := &bodyCapturingClient{}
	cmd := &cobra.Command{Use: "test"}
	cliCtx := &registry.CLIContext{Client: client, Output: mockOutput{}}
	d := &resolve.DeviceIdentifiers{ID: "42", ManagementID: "cccc-dddd", Name: "MacBook-1"}

	err := sendComputerModernMDMCommand(cmd, cliCtx, d, map[string]any{"commandType": "DEVICE_LOCK"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(client.capturedBody, `"DEVICE_LOCK"`) {
		t.Errorf("request body = %q, want it to contain commandType", client.capturedBody)
	}
	if !strings.Contains(client.capturedBody, `"cccc-dddd"`) {
		t.Errorf("request body = %q, want it to contain managementId", client.capturedBody)
	}
}

func TestSendComputerModernMDMCommand_MissingManagementID(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	cliCtx := &registry.CLIContext{}
	d := &resolve.DeviceIdentifiers{ID: "42", Name: "MacBook-1"} // no ManagementID

	err := sendComputerModernMDMCommand(cmd, cliCtx, d, map[string]any{"commandType": "DEVICE_LOCK"})
	if err == nil {
		t.Fatal("expected error for missing managementId, got nil")
	}
	if !strings.Contains(err.Error(), "managementId") {
		t.Errorf("error = %q, want it to mention managementId", err.Error())
	}
}
