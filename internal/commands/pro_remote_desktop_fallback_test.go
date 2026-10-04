// Copyright 2026, Jamf Software LLC

package commands

import (
	"net/http"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/auth"
	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
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

// Restart and shut down have no Classic route, and go through the Platform
// API's device actions on a gateway profile, by management ID.
func TestRestartAndShutdownUseThePlatformDeviceActionsOnTheGateway(t *testing.T) {
	const mgmt = "73226fb6-61df-4c10-9552-eb9bc353d507"
	computer := strings.Replace(v3Computer("42", "Mac-42", "C02X1234"), `"general":{`, `"general":{"managementId":"`+mgmt+`",`, 1)
	mobile := `{"id":"64","managementId":"` + mgmt + `","udid":"u","name":"iPad","serialNumber":"F4GH5678"}`
	for _, tc := range []struct {
		name   string
		build  func(*registry.CLIContext) *cobra.Command
		lookup string
		record string
		action string
		args   []string
	}{
		{"computer restart", newComputerRestartCmd, "GET /v4/computers-inventory/42", computer, "restart", []string{"--id", "42"}},
		{"computer shutdown", newComputerShutdownCmd, "GET /v4/computers-inventory/42", computer, "shutdown", []string{"--id", "42"}},
		{"mobile restart", newMobileRestartCmd, "GET /v2/mobile-devices/64", mobile, "restart", []string{"--id", "64"}},
		{"mobile shutdown", newMobileShutdownCmd, "GET /v2/mobile-devices/64", mobile, "shutdown", []string{"--id", "64"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cliCtx, mux, _ := newTestPlatformContext(t)
			var got string
			mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				got = r.Method + " " + r.URL.Path
				writeJSONStatus(w, http.StatusCreated, []map[string]string{{"commandUuid": "c"}})
			})
			mock := &bulkMockClient{responses: map[string]overviewMockResponse{tc.lookup: {200, tc.record}}}
			cliCtx.Client = mock
			cliCtx.AuthProvider = &auth.PlatformOAuth2Provider{}
			if _, _, err := runCobraCmd(t, tc.build(cliCtx), append(tc.args, "--yes")...); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.HasSuffix(got, "/devices/"+mgmt+"/"+tc.action) || !strings.HasPrefix(got, "POST ") {
				t.Errorf("Platform request = %q, want POST .../devices/%s/%s", got, mgmt, tc.action)
			}
			if len(mock.callsMatching("/v2/mdm/commands")) != 0 {
				t.Errorf("sent the modern MDM command on a gateway profile: %v", mock.calls)
			}
		})
	}
}

// The Platform restart takes no options, so --rebuild-kernel-cache is refused
// on a gateway profile rather than dropped.
func TestRestartRebuildKernelCacheRefusedOnTheGateway(t *testing.T) {
	cliCtx := &registry.CLIContext{Client: &bulkMockClient{}, AuthProvider: &auth.PlatformOAuth2Provider{}}
	_, _, err := runCobraCmd(t, newComputerRestartCmd(cliCtx), "--id", "42", "--rebuild-kernel-cache", "--yes")
	if exitcode.CodeFrom(err) != exitcode.Unsupported {
		t.Errorf("err = %v, want an unsupported refusal", err)
	}
}
