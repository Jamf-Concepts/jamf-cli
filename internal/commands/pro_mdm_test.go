// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"strings"
	"testing"
)

func TestRunMDMCommand_LockSucceeds(t *testing.T) {
	client := &deviceResolveMockClient{
		handler: func(method, path string) (int, string, error) {
			// Device resolution: direct ID lookup succeeds
			if method == "GET" && path == "/v1/computers-inventory-detail/42" {
				return 200, `{"id":"42","general":{"name":"MacBook-Lab1"}}`, nil
			}
			// MDM command endpoint
			if method == "POST" && path == "/JSSResource/computercommands/command/DeviceLock/id/42" {
				return 201, `<computer_command/>`, nil
			}
			t.Fatalf("unexpected request: %s %s", method, path)
			return 0, "", nil
		},
	}

	err := runMDMCommand(context.Background(), client, "42", "DeviceLock", true, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunMDMCommand_RestartSucceeds(t *testing.T) {
	client := &deviceResolveMockClient{
		handler: func(method, path string) (int, string, error) {
			if method == "GET" && path == "/v1/computers-inventory-detail/42" {
				return 200, `{"id":"42","general":{"name":"MacBook-Lab1"}}`, nil
			}
			if method == "POST" && path == "/JSSResource/computercommands/command/RestartDevice/id/42" {
				return 201, `<computer_command/>`, nil
			}
			t.Fatalf("unexpected request: %s %s", method, path)
			return 0, "", nil
		},
	}

	// Non-destructive command: confirmDestructive=false is fine
	err := runMDMCommand(context.Background(), client, "42", "RestartDevice", true, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunMDMCommand_DestructiveWithoutConfirm(t *testing.T) {
	// No mock needed — validation should fail before any HTTP call
	client := &deviceResolveMockClient{
		handler: func(method, path string) (int, string, error) {
			t.Fatalf("unexpected request: %s %s — should have failed before HTTP call", method, path)
			return 0, "", nil
		},
	}

	err := runMDMCommand(context.Background(), client, "42", "EraseDevice", true, false)
	if err == nil {
		t.Fatal("expected error for destructive command without --confirm-destructive, got nil")
	}
	if !strings.Contains(err.Error(), "confirm-destructive") {
		t.Errorf("error = %q, want it to mention --confirm-destructive", err.Error())
	}
}

func TestRunMDMCommand_DryRun(t *testing.T) {
	resolveCallCount := 0
	client := &deviceResolveMockClient{
		handler: func(method, path string) (int, string, error) {
			if method == "GET" && path == "/v1/computers-inventory-detail/42" {
				resolveCallCount++
				return 200, `{"id":"42","general":{"name":"MacBook-Lab1"}}`, nil
			}
			// Any POST means we tried to send the command — that's a bug in dry-run
			if method == "POST" {
				t.Fatalf("dry-run should not send MDM command, got: %s %s", method, path)
			}
			t.Fatalf("unexpected request: %s %s", method, path)
			return 0, "", nil
		},
	}

	err := runMDMCommand(context.Background(), client, "42", "BlankPush", false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolveCallCount == 0 {
		t.Error("expected device resolution to be called during dry-run")
	}
}

func TestRunMDMCommand_DeviceNotFound(t *testing.T) {
	client := &deviceResolveMockClient{
		handler: func(_, path string) (int, string, error) {
			if strings.HasPrefix(path, "/v1/computers-inventory-detail/") {
				return 404, `{"errors":[]}`, nil
			}
			// Both serial and name searches return 0 results
			return 200, `{"totalCount":0,"results":[]}`, nil
		},
	}

	err := runMDMCommand(context.Background(), client, "ghost-machine", "BlankPush", true, false)
	if err == nil {
		t.Fatal("expected error for device not found, got nil")
	}
	if !strings.Contains(err.Error(), "no device found") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "no device found")
	}
}

func TestRunMDMCommand_InvalidCommand(t *testing.T) {
	client := &deviceResolveMockClient{
		handler: func(method, path string) (int, string, error) {
			t.Fatalf("unexpected request: %s %s — should have failed before HTTP call", method, path)
			return 0, "", nil
		},
	}

	err := runMDMCommand(context.Background(), client, "42", "FakeCommand", true, false)
	if err == nil {
		t.Fatal("expected error for invalid command, got nil")
	}
	if !strings.Contains(err.Error(), "unknown MDM command") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "unknown MDM command")
	}
}

func TestSingleMDMCommandsMap(t *testing.T) {
	expected := map[string]string{
		"lock":                   "DeviceLock",
		"restart":                "RestartDevice",
		"shutdown":               "ShutDownDevice",
		"erase":                  "EraseDevice",
		"blank-push":             "BlankPush",
		"update-inventory":       "UpdateInventory",
		"enable-remote-desktop":  "EnableRemoteDesktop",
		"disable-remote-desktop": "DisableRemoteDesktop",
		"unmanage":               "UnmanageDevice",
	}
	for subCmd, apiCmd := range expected {
		got, ok := singleMDMCommands[subCmd]
		if !ok {
			t.Errorf("singleMDMCommands missing key %q", subCmd)
			continue
		}
		if got != apiCmd {
			t.Errorf("singleMDMCommands[%q] = %q, want %q", subCmd, got, apiCmd)
		}
	}
	if len(singleMDMCommands) != len(expected) {
		t.Errorf("singleMDMCommands has %d entries, want %d", len(singleMDMCommands), len(expected))
	}
}

func TestSingleDestructiveMDMCommands(t *testing.T) {
	if !singleDestructiveMDMCommands["EraseDevice"] {
		t.Error("expected EraseDevice to be destructive")
	}
	if !singleDestructiveMDMCommands["DeviceLock"] {
		t.Error("expected DeviceLock to be destructive")
	}
	if len(singleDestructiveMDMCommands) != 2 {
		t.Errorf("singleDestructiveMDMCommands has %d entries, want 2", len(singleDestructiveMDMCommands))
	}
}
