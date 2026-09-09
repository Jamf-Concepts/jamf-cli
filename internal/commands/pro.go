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
	cmd.AddCommand(newDiffCmd(cliCtx))
	cmd.AddCommand(newGroupToolsCmd(cliCtx))
	cmd.AddCommand(newDeviceCmd(cliCtx))
	cmd.AddCommand(newClassicComputerAppUsageCmd(cliCtx))
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

	// Suppress generated commands duplicated by richer handwritten versions (see #39).
	// The handwritten counterparts target by --serial/--name/--group/--from-file,
	// confirm the action, honour --dry-run and carry the Find My PIN body, where
	// the generated ones take an <id>.
	//
	// These used to name six standalone resources, which is what a per-file spec
	// layout produced: `/v1/computer-inventory/{id}/erase` sat in its own file
	// and became `pro erase-device-computers`. Grouping by path files each
	// action under the collection it acts on, so what has to be suppressed is
	// now a whole resource in two cases and a single subcommand in three.
	//
	// `computers` is no longer suppressed. It used to be the Classic basic v1
	// list; that path is dropped at ingest now (see parser.KeepPath), and the
	// name belongs to `POST /v1/computers/{id}/recalculate-smart-groups`, which
	// has no handwritten counterpart and should ship.
	removeSubcommand(cmd, []string{}, "computer-inventory")        // both ops → pro comp erase / remove-mdm
	removeSubcommand(cmd, []string{}, "jamf-management-framework") // → pro comp redeploy-framework
	removeSubcommand(cmd, []string{"mobile-devices"}, "erase")     // → pro md erase
	removeSubcommand(cmd, []string{"mobile-devices"}, "unmanage")  // → pro md unmanage
	removeSubcommand(cmd, []string{"mdm"}, "renew-profile")        // → pro comp renew-mdm

	// Replace broken generated upload with handwritten streaming upload.
	// The JCDS binary-upload endpoint needs special chunked-upload handling
	// that the generated multipart template can't produce.
	replaceSubcommand(cmd, []string{"packages"}, "upload", newPackagesUploadCmd(cliCtx))

	// Add handwritten jcds commands to generated parent (multi-step orchestration).
	addSubcommand(cmd, []string{"jcds"}, newJcdsDownloadCmd(cliCtx))
	addSubcommand(cmd, []string{"jcds"}, newJcdsSyncCmd(cliCtx))

	// Also expose sync under packages — JCDS is the backing store for packages.
	addSubcommand(cmd, []string{"packages"}, newJcdsSyncCmd(cliCtx))

	// Add handwritten retry-failed to generated parent (orchestrates computer
	// resolution + task lookup/filter before calling the retry endpoint).
	addSubcommand(cmd, []string{"jamf-protect-deployment-tasks"}, newJamfProtectDeploymentRetryFailedCmd(cliCtx))

	// Add device action subcommands to generated resource parents
	// Both replace a generated v4 sibling rather than sitting beside it. The
	// generated erase/remove-mdm-profile pair arrived with v4 computers-inventory
	// and takes an <id> alone; these target by serial, name or group, confirm a
	// destructive action, honour --dry-run and carry the Find My PIN body. A
	// second `erase` under one parent is also not a choice cobra can make —
	// before this, `pro comp --help` listed the name twice.
	replaceSubcommand(cmd, []string{"computers-inventory"}, "erase", newComputerEraseCmd(cliCtx))
	removeSubcommand(cmd, []string{"computers-inventory"}, "remove-mdm-profile")
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

	// Wire classic-mobile-devices delete under mobile-devices
	for _, sub := range generated.NewClassicMobileDevicesCmd(cliCtx).Commands() {
		if sub.Name() == "delete" {
			addSubcommand(cmd, []string{"mobile-devices"}, sub)
			break
		}
	}

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
