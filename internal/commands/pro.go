// Copyright 2026, Jamf Software LLC

package commands

import (
	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/commands/pro/generated"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

func newProCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pro",
		Short: "Jamf Pro commands",
		Long:  "Commands for interacting with Jamf Pro — device management, inventory, configuration, and reporting.",
	}

	// Setup (creates API roles/integrations on Jamf Pro)
	cmd.AddCommand(newConfigSetupCmd())

	// Handwritten Jamf Pro commands
	cmd.AddCommand(newOverviewCmd(cliCtx))
	cmd.AddCommand(newBackupCmd(cliCtx))
	cmd.AddCommand(newAuditCmd(cliCtx))
	cmd.AddCommand(newBulkCmd(cliCtx))
	cmd.AddCommand(newReportCmd(cliCtx))
	cmd.AddCommand(newDiffCmd())
	cmd.AddCommand(newGroupToolsCmd(cliCtx))
	cmd.AddCommand(newDeviceCmd(cliCtx))
	cmd.AddCommand(newPolicyExecuteCmd(cliCtx))

	// Generated modern API commands
	generated.RegisterCommands(cmd, cliCtx)

	// Generated Classic API commands
	generated.RegisterClassicCommands(cmd, cliCtx)

	// Suppress generated commands that don't work for singleton/sub-resource patterns (see #45)
	removeSubcommand(cmd, []string{"jamf-protects"}, "apply")
	removeSubcommand(cmd, []string{"jamf-protect-deployment-tasks"}, "get-by-name")

	// Suppress generated commands duplicated by richer handwritten versions (see #39)
	// Handwritten counterparts support --serial/--name/--group/--from-file targeting and bulk ops.
	removeSubcommand(cmd, []string{}, "erase-device-computers")              // → pro comp erase
	removeSubcommand(cmd, []string{}, "erase-device-mobiles")                // → pro md erase
	removeSubcommand(cmd, []string{}, "renew-mdm-profiles")                  // → pro comp renew-mdm
	removeSubcommand(cmd, []string{}, "redeploy-jamf-management-frameworks") // → pro comp redeploy-framework
	removeSubcommand(cmd, []string{}, "remove-computer-mdm-profiles")        // → pro comp remove-mdm
	removeSubcommand(cmd, []string{}, "remove-mobile-device-mdm-profiles")   // → pro md unmanage

	// Replace broken generated upload with handwritten streaming upload
	replaceSubcommand(cmd, []string{"packages"}, "upload", newPackagesUploadCmd(cliCtx))

	// Add upload subcommands to generated resources
	addSubcommand(cmd, []string{"scripts"}, newScriptsUploadCmd(cliCtx))
	addSubcommand(cmd, []string{"classic-macos-config-profiles"}, newMacOSProfileUploadCmd(cliCtx))
	addSubcommand(cmd, []string{"classic-mobile-config-profiles"}, newMobileProfileUploadCmd(cliCtx))

	// Add device action subcommands to generated resource parents
	addSubcommand(cmd, []string{"computers"}, newComputerEraseCmd(cliCtx))
	addSubcommand(cmd, []string{"computers"}, newComputerRemoveMDMCmd(cliCtx))
	addSubcommand(cmd, []string{"computers"}, newComputerRedeployFrameworkCmd(cliCtx))
	addSubcommand(cmd, []string{"computers"}, newComputerBlankPushCmd(cliCtx))
	addSubcommand(cmd, []string{"computers"}, newComputerDDMSyncCmd(cliCtx))
	addSubcommand(cmd, []string{"computers"}, newComputerRenewMDMCmd(cliCtx))
	addSubcommand(cmd, []string{"mobile-devices"}, newMobileEraseCmd(cliCtx))
	addSubcommand(cmd, []string{"mobile-devices"}, newMobileUnmanageCmd(cliCtx))

	// Classic API computer MDM commands (no modern API equivalent)
	addSubcommand(cmd, []string{"computers"}, newComputerLockCmd(cliCtx))
	addSubcommand(cmd, []string{"computers"}, newComputerEnableRemoteDesktopCmd(cliCtx))
	addSubcommand(cmd, []string{"computers"}, newComputerDisableRemoteDesktopCmd(cliCtx))

	// Classic API mobile device MDM commands (no modern API equivalent)
	addSubcommand(cmd, []string{"mobile-devices"}, newMobileRestartCmd(cliCtx))
	addSubcommand(cmd, []string{"mobile-devices"}, newMobileShutdownCmd(cliCtx))
	addSubcommand(cmd, []string{"mobile-devices"}, newMobileUpdateInventoryCmd(cliCtx))

	// Apply aliases and groups to pro's children
	applyAliases(cmd)
	applyProGroups(cmd)

	return cmd
}

// addSubcommand finds a parent command by path and adds a child to it.
func addSubcommand(root *cobra.Command, parentPath []string, child *cobra.Command) {
	parent, _, err := root.Find(parentPath)
	if err != nil {
		return
	}
	parent.AddCommand(child)
}

// removeSubcommand finds a parent command by path and removes a named child.
func removeSubcommand(root *cobra.Command, parentPath []string, childName string) {
	parent, _, err := root.Find(parentPath)
	if err != nil {
		return
	}
	for _, child := range parent.Commands() {
		if child.Name() == childName {
			parent.RemoveCommand(child)
			return
		}
	}
}

// replaceSubcommand finds a parent command by path and replaces a named child.
func replaceSubcommand(root *cobra.Command, parentPath []string, childName string, replacement *cobra.Command) {
	parent, _, err := root.Find(parentPath)
	if err != nil {
		return
	}
	for _, child := range parent.Commands() {
		if child.Name() == childName {
			parent.RemoveCommand(child)
			break
		}
	}
	parent.AddCommand(replacement)
}
