// Copyright 2026, Jamf Software LLC

package commands

import (
	"github.com/spf13/cobra"

	platformgen "github.com/Jamf-Concepts/jamf-cli/internal/commands/platform/generated"
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
	cmd.AddCommand(newProAuthCmd(cliCtx))
	cmd.AddCommand(newOverviewCmd(cliCtx))
	cmd.AddCommand(newBackupCmd(cliCtx))
	cmd.AddCommand(newAuditCmd(cliCtx))
	cmd.AddCommand(newBulkCmd(cliCtx))
	cmd.AddCommand(newReportCmd(cliCtx))
	cmd.AddCommand(newDiffCmd())
	cmd.AddCommand(newGroupToolsCmd(cliCtx))
	cmd.AddCommand(newDeviceCmd(cliCtx))
	// Platform API commands (require platform gateway auth)
	cmd.AddCommand(newBlueprintsCmd(cliCtx))
	cmd.AddCommand(newComplianceBenchmarksCmd(cliCtx))
	cmd.AddCommand(newPlatformDevicesCmd(cliCtx))
	cmd.AddCommand(newPlatformDeviceGroupsCmd(cliCtx))
	cmd.AddCommand(newDDMReportsCmd(cliCtx))

	// Spec-generated Platform API commands. Resources without a hand-written
	// equivalent are wired here; resources that collide (blueprints,
	// compliance-benchmarks/benchmarks, platform-devices/devices,
	// platform-device-groups/device-groups) stay served by the existing
	// hand-written commands until those migrate to call generated functions.
	cmd.AddCommand(platformgen.NewBaselinesCmd(cliCtx))
	cmd.AddCommand(platformgen.NewBenchmarkReportsCmd(cliCtx))
	cmd.AddCommand(platformgen.NewBlueprintComponentsCmd(cliCtx))
	cmd.AddCommand(platformgen.NewRulesCmd(cliCtx))
	cmd.AddCommand(platformgen.NewPlatformUsersCmd(cliCtx))

	// Generated modern API commands
	generated.RegisterCommands(cmd, cliCtx)

	// Generated Classic API commands
	generated.RegisterClassicCommands(cmd, cliCtx)

	// Suppress generated commands that don't work for singleton/sub-resource patterns (see #45)
	removeSubcommand(cmd, []string{"jamf-protects"}, "apply")
	removeSubcommand(cmd, []string{"jamf-protect-deployment-tasks"}, "get-by-name")

	// Suppress generated Classic "computers" (basic v1 list) — replaced by
	// "computers-inventory" which is aliased to "computers"/"comp" and has the
	// full modern v3 CRUD plus curated table output. MDM actions continue to be
	// wired into "computers" via addSubcommand (resolves via alias).
	removeSubcommand(cmd, []string{}, "computers")

	// Suppress generated commands duplicated by richer handwritten versions (see #39)
	// Handwritten counterparts support --serial/--name/--group/--from-file targeting and bulk ops.
	removeSubcommand(cmd, []string{}, "erase-device-computers")              // → pro comp erase
	removeSubcommand(cmd, []string{}, "erase-device-mobiles")                // → pro md erase
	removeSubcommand(cmd, []string{}, "renew-mdm-profiles")                  // → pro comp renew-mdm
	removeSubcommand(cmd, []string{}, "redeploy-jamf-management-frameworks") // → pro comp redeploy-framework
	removeSubcommand(cmd, []string{}, "remove-computer-mdm-profiles")        // → pro comp remove-mdm
	removeSubcommand(cmd, []string{}, "remove-mobile-device-mdm-profiles")   // → pro md unmanage

	// Replace broken generated upload with handwritten streaming upload.
	// The JCDS binary-upload endpoint needs special chunked-upload handling
	// that the generated multipart template can't produce.
	replaceSubcommand(cmd, []string{"packages"}, "upload", newPackagesUploadCmd(cliCtx))

	// Add device action subcommands to generated resource parents
	addSubcommand(cmd, []string{"computers-inventory"}, newComputerEraseCmd(cliCtx))
	addSubcommand(cmd, []string{"computers-inventory"}, newComputerRemoveMDMCmd(cliCtx))
	addSubcommand(cmd, []string{"computers-inventory"}, newComputerRedeployFrameworkCmd(cliCtx))
	addSubcommand(cmd, []string{"computers-inventory"}, newComputerBlankPushCmd(cliCtx))
	addSubcommand(cmd, []string{"computers-inventory"}, newComputerDDMSyncCmd(cliCtx))
	addSubcommand(cmd, []string{"computers-inventory"}, newComputerRenewMDMCmd(cliCtx))
	addSubcommand(cmd, []string{"mobile-devices"}, newMobileEraseCmd(cliCtx))
	addSubcommand(cmd, []string{"mobile-devices"}, newMobileUnmanageCmd(cliCtx))

	// Modern API computer MDM commands
	addSubcommand(cmd, []string{"computers-inventory"}, newComputerLockCmd(cliCtx))
	addSubcommand(cmd, []string{"computers-inventory"}, newComputerEnableRemoteDesktopCmd(cliCtx))
	addSubcommand(cmd, []string{"computers-inventory"}, newComputerDisableRemoteDesktopCmd(cliCtx))
	addSubcommand(cmd, []string{"computers-inventory"}, newComputerRestartCmd(cliCtx))
	addSubcommand(cmd, []string{"computers-inventory"}, newComputerShutdownCmd(cliCtx))
	addSubcommand(cmd, []string{"computers-inventory"}, newComputerSetRecoveryLockCmd(cliCtx))
	addSubcommand(cmd, []string{"computers-inventory"}, newComputerSettingsCmd(cliCtx))
	addSubcommand(cmd, []string{"computers-inventory"}, newComputerSetAutoAdminPasswordCmd(cliCtx))

	addSubcommand(cmd, []string{"computers-inventory"}, newComputerFlushCommandsCmd(cliCtx))
	addSubcommand(cmd, []string{"mobile-devices"}, newMobileFlushCommandsCmd(cliCtx))

	// Mobile device MDM commands (modern API where available, Classic where not)
	addSubcommand(cmd, []string{"mobile-devices"}, newMobileRestartCmd(cliCtx))         // modern: RESTART_DEVICE
	addSubcommand(cmd, []string{"mobile-devices"}, newMobileShutdownCmd(cliCtx))        // modern: SHUT_DOWN_DEVICE
	addSubcommand(cmd, []string{"mobile-devices"}, newMobileUpdateInventoryCmd(cliCtx)) // classic: no modern equivalent
	addSubcommand(cmd, []string{"mobile-devices"}, newMobileLockCmd(cliCtx))
	addSubcommand(cmd, []string{"mobile-devices"}, newMobileClearPasscodeCmd(cliCtx))
	addSubcommand(cmd, []string{"mobile-devices"}, newMobileEnableLostModeCmd(cliCtx))
	addSubcommand(cmd, []string{"mobile-devices"}, newMobileDisableLostModeCmd(cliCtx))
	addSubcommand(cmd, []string{"mobile-devices"}, newMobilePlayLostModeSoundCmd(cliCtx))
	addSubcommand(cmd, []string{"mobile-devices"}, newMobileClearRestrictionsPasswordCmd(cliCtx))
	addSubcommand(cmd, []string{"mobile-devices"}, newMobileSettingsCmd(cliCtx))
	addSubcommand(cmd, []string{"mobile-devices"}, newMobileRequestMirroringCmd(cliCtx))
	addSubcommand(cmd, []string{"mobile-devices"}, newMobileStopMirroringCmd(cliCtx))
	addSubcommand(cmd, []string{"mobile-devices"}, newMobileRefreshCellularPlansCmd(cliCtx))
	addSubcommand(cmd, []string{"mobile-devices"}, newMobileApplyRedemptionCodeCmd(cliCtx))
	addSubcommand(cmd, []string{"mobile-devices"}, newMobileDeleteUserCmd(cliCtx))
	addSubcommand(cmd, []string{"mobile-devices"}, newMobileLogOutUserCmd(cliCtx))
	addSubcommand(cmd, []string{"mobile-devices"}, newMobileUnlockUserAccountCmd(cliCtx))

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
