// Copyright 2026, Jamf Software LLC

package commands

import (
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/auth"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// The gateway does not publish POST /v2/mdm/commands, and Jamf Pro still
// queues the two remote-desktop commands through the Classic API, so a
// gateway profile sends those and a direct one the modern command.
func TestRemoteDesktopFallsBackToClassicOnTheGateway(t *testing.T) {
	record := v3Computer("42", "Mac-42", "C02X1234")
	record = strings.Replace(record, `"general":{`, `"general":{"managementId":"73226fb6-61df-4c10-9552-eb9bc353d507",`, 1)
	for _, tc := range []struct {
		name     string
		provider auth.Provider
		want     string
		notWant  string
	}{
		{"gateway", &auth.PlatformOAuth2Provider{}, "POST /JSSResource/computercommands/command/EnableRemoteDesktop/id/42", "/v2/mdm/commands"},
		{"direct", nil, "POST /v2/mdm/commands", "computercommands"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mock := &bulkMockClient{
				responses: map[string]overviewMockResponse{
					"GET /v4/computers-inventory/42": {200, record},
					"POST /JSSResource/computercommands/command/EnableRemoteDesktop/id/42": {
						201,
						`<computer_command><command><name>EnableRemoteDesktop</name></command></computer_command>`,
					},
					"POST /v2/mdm/commands": {201, `[{"id":"1","href":"/v2/mdm/commands/1"}]`},
				},
			}
			cliCtx := &registry.CLIContext{Client: mock, AuthProvider: tc.provider, Output: &captureOutput{}}
			if _, _, err := runCobraCmd(t, newComputerEnableRemoteDesktopCmd(cliCtx), "--id", "42"); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(mock.callsMatching(tc.want)) != 1 {
				t.Errorf("calls = %v, want one %s", mock.calls, tc.want)
			}
			if len(mock.callsMatching(tc.notWant)) != 0 {
				t.Errorf("calls = %v, want none to %s", mock.calls, tc.notWant)
			}
		})
	}
}

// A command with no Classic counterpart is still refused before anything is
// sent on a gateway profile.
func TestLockIsStillRefusedOnTheGateway(t *testing.T) {
	root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")
	cmd, _, err := root.Find([]string{"pro", "computers-inventory", "lock"})
	if err != nil {
		t.Fatal(err)
	}
	if err := checkAPIMatch(cmd, &auth.PlatformOAuth2Provider{}, "platform-ga"); err == nil {
		t.Error("lock was allowed through on a gateway profile, where nothing can send it")
	}
}
