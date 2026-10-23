// Copyright 2026, Jamf Software LLC

package commands

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/auth"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// The gateway publishes POST /v2/mdm/commands, so a gateway profile sends the
// same modern command a direct one does — no Classic computer command and no
// Platform API device action in between.
func TestMDMDeviceActionsSendTheModernCommandOnTheGateway(t *testing.T) {
	const mgmt = "73226fb6-61df-4c10-9552-eb9bc353d507"
	computer := strings.Replace(v3Computer("42", "Mac-42", "C02X1234"), `"general":{`, `"general":{"managementId":"`+mgmt+`",`, 1)
	mobile := `{"id":"64","managementId":"` + mgmt + `","udid":"u","name":"iPad","serialNumber":"F4GH5678"}`
	for _, tc := range []struct {
		name   string
		build  func(*registry.CLIContext) *cobra.Command
		lookup string
		record string
		args   []string
	}{
		{"computer enable-remote-desktop", newComputerEnableRemoteDesktopCmd, "GET /v4/computers-inventory/42", computer, []string{"--id", "42"}},
		{"computer disable-remote-desktop", newComputerDisableRemoteDesktopCmd, "GET /v4/computers-inventory/42", computer, []string{"--id", "42"}},
		{"computer restart", newComputerRestartCmd, "GET /v4/computers-inventory/42", computer, []string{"--id", "42", "--rebuild-kernel-cache"}},
		{"computer shutdown", newComputerShutdownCmd, "GET /v4/computers-inventory/42", computer, []string{"--id", "42"}},
		{"mobile restart", newMobileRestartCmd, "GET /v2/mobile-devices/64", mobile, []string{"--id", "64"}},
		{"mobile shutdown", newMobileShutdownCmd, "GET /v2/mobile-devices/64", mobile, []string{"--id", "64"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mock := &bulkMockClient{
				responses: map[string]overviewMockResponse{
					tc.lookup:               {200, tc.record},
					"POST /v2/mdm/commands": {201, `[{"id":"1","href":"/v2/mdm/commands/1"}]`},
				},
			}
			cliCtx := &registry.CLIContext{Client: mock, AuthProvider: &auth.PlatformOAuth2Provider{}, Output: &captureOutput{}}
			if _, _, err := runCobraCmd(t, tc.build(cliCtx), append(tc.args, "--yes")...); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(mock.callsMatching("POST /v2/mdm/commands")) != 1 {
				t.Errorf("calls = %v, want one POST /v2/mdm/commands", mock.calls)
			}
			if len(mock.callsMatching("computercommands")) != 0 {
				t.Errorf("sent a Classic computer command on a gateway profile: %v", mock.calls)
			}
		})
	}
}
