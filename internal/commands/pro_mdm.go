// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// singleMDMCommands maps CLI subcommand names to their Classic API MDM command names.
var singleMDMCommands = map[string]string{
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

// singleDestructiveMDMCommands is the set of API command names that require
// --confirm-destructive in addition to --yes.
var singleDestructiveMDMCommands = map[string]bool{
	"EraseDevice": true,
	"DeviceLock":  true,
}

// validSingleMDMCommands is the set of API command names accepted by
// runMDMCommand. Built from singleMDMCommands values.
var validSingleMDMCommands = func() map[string]bool {
	m := make(map[string]bool, len(singleMDMCommands))
	for _, v := range singleMDMCommands {
		m[v] = true
	}
	return m
}()

// newMdmCmd builds the "mdm" parent command with subcommands for each
// single-device MDM operation.
func newMdmCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mdm",
		Short: "Send MDM commands to a single device",
		Long: `Send MDM commands to a single device by name, serial number, or Jamf ID.

Default behavior is a dry-run preview — no changes are made unless --yes is
provided. Destructive commands (lock, erase) additionally require
--confirm-destructive.`,
	}

	for name, apiCommand := range singleMDMCommands {
		cmd.AddCommand(newMdmSubcommand(cliCtx, name, apiCommand))
	}

	return cmd
}

// newMdmSubcommand creates a single MDM subcommand (e.g., "lock", "restart").
func newMdmSubcommand(cliCtx *registry.CLIContext, name, apiCommand string) *cobra.Command {
	var (
		yes                bool
		confirmDestructive bool
	)

	short := fmt.Sprintf("Send %s command to a device", apiCommand)
	if singleDestructiveMDMCommands[apiCommand] {
		short += " (destructive — requires --yes --confirm-destructive)"
	}

	cmd := &cobra.Command{
		Use:   name + " <device>",
		Short: short,
		Long: fmt.Sprintf(`Send the %s MDM command to a single device.

The <device> argument can be a Jamf ID, serial number, or computer name.
Resolution tries ID first, then serial, then name.

Without --yes the command resolves the device and prints what would happen
without sending the command.`, apiCommand),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMDMCommand(cmd.Context(), cliCtx.Client, args[0], apiCommand, yes, confirmDestructive)
		},
	}

	cmd.Flags().BoolVar(&yes, "yes", false, "execute the command (default: dry-run preview only)")
	cmd.Flags().BoolVar(&confirmDestructive, "confirm-destructive", false, "required for destructive commands (lock, erase)")

	return cmd
}

// runMDMCommand validates, resolves the device, and sends a single MDM command.
func runMDMCommand(
	ctx context.Context,
	client registry.HTTPClient,
	identifier, command string,
	yes, confirmDestructive bool,
) error {
	// 1. Validate command name against known set.
	if !validSingleMDMCommands[command] {
		return fmt.Errorf("unknown MDM command %q; valid commands: %s",
			command, strings.Join(sortedKeys(validSingleMDMCommands), ", "))
	}

	// 2. Gate destructive commands.
	if singleDestructiveMDMCommands[command] {
		if !yes || !confirmDestructive {
			return fmt.Errorf(
				"command %q is destructive; both --yes and --confirm-destructive are required",
				command,
			)
		}
	}

	// 3. Resolve device.
	deviceID, deviceName, err := resolveDeviceByIdentifier(ctx, client, identifier)
	if err != nil {
		return err
	}

	// 4. Dry-run: print what would happen and return.
	if !yes {
		_, _ = fmt.Fprintf(os.Stderr,
			"[dry-run] Would send %q to %s (id=%s). Use --yes to execute.\n",
			command, deviceName, deviceID,
		)
		return nil
	}

	// 5. Execute.
	_, _ = fmt.Fprintf(os.Stderr, "Sending %q to %s (id=%s)...\n", command, deviceName, deviceID)

	if err := sendMDMCommand(ctx, client, deviceID, command); err != nil {
		return fmt.Errorf("sending %q to %s (id=%s): %w", command, deviceName, deviceID, err)
	}

	_, _ = fmt.Fprintf(os.Stderr, "Successfully sent %q to %s (id=%s).\n", command, deviceName, deviceID)
	return nil
}
