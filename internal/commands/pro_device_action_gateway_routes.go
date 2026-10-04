// Copyright 2026, Jamf Software LLC

package commands

import (
	"fmt"
	"net/url"

	"github.com/jamf/jamfplatform-go-sdk/jamfplatform/deviceactions"
	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/platform"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamf-cli/internal/resolve"
)

// gatewayRoute is how a modern MDM command reaches a device on a platform
// gateway profile, where POST /v2/mdm/commands is not published. A command
// with no route is refused there before anything is sent.
//
// Wire-checked 2026-10-04 on Jamf Pro 11.32, direct and through the gateway:
//
//   - The Classic computer API queues EnableRemoteDesktop and
//     DisableRemoteDesktop and nothing else (see deadClassicComputerCommands).
//   - The Classic mobile API answers "Invalid command" for DisableLostMode,
//     RestartDevice, ShutDownDevice, ClearPasscode, ClearRestrictionsPassword
//     and a bare Settings. It accepts EnableLostMode, but with no way to turn
//     lost mode off again through the gateway that is not offered as a route.
//   - The Platform API's device actions restart and shut down a computer or a
//     mobile device by its management ID (201).
type gatewayRoute struct {
	// note is the sentence --help adds about the gateway.
	note string
	send func(cmd *cobra.Command, cliCtx *registry.CLIContext, d *resolve.DeviceIdentifiers) error
}

// classicComputerCommandRoute sends a Classic computer command.
func classicComputerCommandRoute(command string) *gatewayRoute {
	return &gatewayRoute{
		note: "the Classic API's " + command + " computer command",
		send: func(cmd *cobra.Command, cliCtx *registry.CLIContext, d *resolve.DeviceIdentifiers) error {
			return sendClassicComputerCommand(cmd, cliCtx, command, d)
		},
	}
}

// sendClassicComputerCommand sends a Classic computer command to one computer.
func sendClassicComputerCommand(cmd *cobra.Command, cliCtx *registry.CLIContext, command string, d *resolve.DeviceIdentifiers) error {
	path := fmt.Sprintf("/JSSResource/computercommands/command/%s/id/%s", url.PathEscape(command), url.PathEscape(d.ID))
	return doPostAction(cmd, cliCtx, path, nil)
}

// platformDeviceActionRoute sends a Platform API device action ("restart" or
// "shutdown"), which takes the device's management ID.
func platformDeviceActionRoute(action string) *gatewayRoute {
	return &gatewayRoute{
		note: "the Platform API's " + action + " device action",
		send: func(cmd *cobra.Command, cliCtx *registry.CLIContext, d *resolve.DeviceIdentifiers) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			if d.ManagementID == "" {
				return fmt.Errorf("%s has no management ID, which the Platform API device action needs", resolve.FormatDeviceDesc(d))
			}
			client := deviceactions.New(cliCtx.PlatformSDKClient)
			var (
				resp []deviceactions.DeviceCommandResponse
				err  error
			)
			switch action {
			case "restart":
				resp, err = client.RestartDevice(cmd.Context(), d.ManagementID)
			case "shutdown":
				resp, err = client.ShutdownDevice(cmd.Context(), d.ManagementID)
			default:
				return fmt.Errorf("no Platform API device action %q", action)
			}
			if err != nil {
				return err
			}
			return platform.PrintOne(cliCtx.Output, resp)
		},
	}
}

// sendThroughRoute sends d through route on a gateway profile, and reports
// whether it did; a direct profile, or a command with no route, sends the
// modern MDM command instead.
func sendThroughRoute(cmd *cobra.Command, cliCtx *registry.CLIContext, route *gatewayRoute, d *resolve.DeviceIdentifiers) (bool, error) {
	if route == nil || !isGatewayProvider(cliCtx.AuthProvider) {
		return false, nil
	}
	return true, route.send(cmd, cliCtx, d)
}

// withGatewayRoute documents route on cmd and leaves it unrefused on a gateway
// profile, or, with no route, stamps the refusal markGatewayCoverage reads
// from the coverage table.
func withGatewayRoute(cmd *cobra.Command, route *gatewayRoute) *cobra.Command {
	if route == nil {
		return markGatewayCoverage(cmd, "POST", mdmCommandsPath)
	}
	cmd.Long += "\n\nThrough a platform gateway profile, where the modern MDM endpoint is not published,\nthe command is sent as " + route.note + " instead."
	return cmd
}
