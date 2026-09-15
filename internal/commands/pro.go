// Copyright 2026, Jamf Software LLC

package commands

import (
	"slices"
	"strings"

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

	// Two suppressions used to sit here (#45), and the wiring guard below is
	// what found them dead. One removed `apply` from the resource `main` calls
	// `jamf-protects`, the other `get-by-name` from
	// `jamf-protect-deployment-tasks`; this branch produces neither resource,
	// and neither name is generated under the `jamf-protect` that absorbed
	// them. It has no nameResolutionPath, so no `apply` is synthesized, and the
	// deployment-tasks lookup ships as `pro jamf-protect tasks`. Re-keying them
	// onto `jamf-protect` — the first pass of this branch — left two calls that
	// resolved a parent and then removed nothing.

	// Suppress generated commands duplicated by richer handwritten versions (see #39).
	// The handwritten counterparts target by --serial/--name/--group/--from-file,
	// confirm the action, honour --dry-run and carry the Find My PIN body, where
	// the generated ones take an <id>.
	//
	// These used to name six standalone resources, which is what a per-file spec
	// layout produced: `/v1/computer-inventory/{id}/erase` sat in its own file
	// and became `pro erase-device-computers`. Grouping by tag files each action
	// under the resource it acts on, so what has to be suppressed is a
	// subcommand rather than a resource — and `computer-inventory` is the
	// primary computer resource now, so removing it would take `pro comp list`
	// with it.
	//
	// `computers` is no longer suppressed. It used to be the Classic basic v1
	// list; that path is dropped at ingest now (see parser.KeepPath), and the
	// name belongs to `POST /v1/computers/{id}/recalculate-smart-groups`, which
	// has no handwritten counterpart and should ship.
	removeSubcommand(cmd, []string{}, "jamf-management-framework") // → pro comp redeploy-framework
	removeSubcommand(cmd, []string{"mobile-devices"}, "erase")     // → pro md erase
	removeSubcommand(cmd, []string{"mobile-devices"}, "unmanage")  // → pro md unmanage
	removeSubcommand(cmd, []string{"mdm"}, "renew-profile")        // → pro comp renew-mdm

	// Replace broken generated upload with handwritten streaming upload.
	// The JCDS binary-upload endpoint needs special chunked-upload handling
	// that the generated multipart template can't produce.
	replaceSubcommand(cmd, []string{"packages"}, "upload", newPackagesUploadCmd(cliCtx))

	// Add handwritten jcds commands to generated parent (multi-step orchestration).
	addSubcommand(cmd, []string{"jamf-cloud-distribution-service"}, newJcdsDownloadCmd(cliCtx))
	addSubcommand(cmd, []string{"jamf-cloud-distribution-service"}, newJcdsSyncCmd(cliCtx))

	// Also expose sync under packages — JCDS is the backing store for packages.
	addSubcommand(cmd, []string{"packages"}, newJcdsSyncCmd(cliCtx))

	// Add handwritten retry-failed to generated parent (orchestrates computer
	// resolution + task lookup/filter before calling the retry endpoint).
	addSubcommand(cmd, []string{"jamf-protect"}, newJamfProtectDeploymentRetryFailedCmd(cliCtx))

	// Add device action subcommands to generated resource parents
	// Both replace a generated v4 sibling rather than sitting beside it. The
	// generated erase/remove-mdm-profile pair arrived with v4 computers-inventory
	// and takes an <id> alone; these target by serial, name or group, confirm a
	// destructive action, honour --dry-run and carry the Find My PIN body. A
	// second `erase` under one parent is also not a choice cobra can make —
	// before this, `pro comp --help` listed the name twice.
	replaceSubcommand(cmd, []string{"computer-inventory"}, "erase", newComputerEraseCmd(cliCtx))
	removeSubcommand(cmd, []string{"computer-inventory"}, "remove-mdm-profile")
	addSubcommand(cmd, []string{"computer-inventory"}, newComputerRemoveMDMCmd(cliCtx))
	addSubcommand(cmd, []string{"computer-inventory"}, newComputerRedeployFrameworkCmd(cliCtx))
	addSubcommand(cmd, []string{"computer-inventory"}, newComputerBlankPushCmd(cliCtx))
	addSubcommand(cmd, []string{"computer-inventory"}, newComputerDDMSyncCmd(cliCtx))
	addSubcommand(cmd, []string{"computer-inventory"}, newComputerRenewMDMCmd(cliCtx))
	addSubcommand(cmd, []string{"mobile-devices"}, newMobileEraseCmd(cliCtx))
	addSubcommand(cmd, []string{"mobile-devices"}, newMobileUnmanageCmd(cliCtx))

	// Modern API computer MDM commands
	addSubcommand(cmd, []string{"computer-inventory"}, newComputerLockCmd(cliCtx))
	addSubcommand(cmd, []string{"computer-inventory"}, newComputerEnableRemoteDesktopCmd(cliCtx))
	addSubcommand(cmd, []string{"computer-inventory"}, newComputerDisableRemoteDesktopCmd(cliCtx))
	addSubcommand(cmd, []string{"computer-inventory"}, newComputerRestartCmd(cliCtx))
	addSubcommand(cmd, []string{"computer-inventory"}, newComputerShutdownCmd(cliCtx))
	addSubcommand(cmd, []string{"computer-inventory"}, newComputerSetRecoveryLockCmd(cliCtx))
	addSubcommand(cmd, []string{"computer-inventory"}, newComputerSettingsCmd(cliCtx))
	addSubcommand(cmd, []string{"computer-inventory"}, newComputerSetAutoAdminPasswordCmd(cliCtx))

	addSubcommand(cmd, []string{"computer-inventory"}, newComputerFlushCommandsCmd(cliCtx))
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

	// Retired resource names, before applyAliases so a deprecated name and a
	// curated alias cannot both be appended for the same string.
	applyDeprecatedNames(cmd, cliCtx)

	// Apply aliases and groups to pro's children
	applyAliases(cmd)
	applyProGroups(cmd)

	return cmd
}

// Wiring a hand-written command into the generated tree is keyed on names —
// the parent resource's, and for a suppression the generated child's. Both
// halves are strings the generator derives from the spec, so an upstream path
// or tag change can stale one, and until this all three helpers answered a
// stale key by doing nothing.
//
// The consequence is not a missing command, which is the failure a reader
// expects and would notice. `removeSubcommand` leaves the generated command in
// place, and `replaceSubcommand` adds its replacement without removing
// anything — so a stale key ships the generated command the hand-written one
// exists to displace, beside it, under whatever name the generator gave it.
// That happened to `erase` and `remove-mdm-profile`: the deprecated v1 pair
// took the plain names, so both keys matched the deprecated pair and the served
// v4 twins shipped alongside `pro comp erase` and `pro comp remove-mdm`,
// without the `--confirm-destructive` gate the hand-written pair carry for
// bulk. A second path to a fleet-wide wipe, behind one fewer flag, from a
// suppression that reported success.
//
// So every miss is recorded rather than discarded, and
// TestProWiringNamesCommandsThatShip fails on a non-empty record. It is a
// package-level map rather than a returned error because the wiring runs once
// at startup and there is nothing useful for a CLI to do about it at that
// point; a test is the right place to answer it. Keyed on the invocation, so
// repeated tree builds record one entry rather than growing.
var staleProWiring = map[string]string{}

func recordStaleProWiring(op string, parentPath []string, childName string) {
	key := op + " " + strings.Join(append([]string{"pro"}, parentPath...), " ")
	if childName != "" {
		key += " " + childName
	}
	staleProWiring[key] = childName
}

// findWiringParent resolves a parent path, recording a miss when it does not
// resolve to the command the path names. cobra's Find falls back to the nearest
// resolvable ancestor rather than erroring on a bad leaf, so the returned
// command has to be checked against the path as well.
func findWiringParent(root *cobra.Command, op string, parentPath []string, childName string) *cobra.Command {
	parent, _, err := root.Find(parentPath)
	if err != nil || parent == nil {
		recordStaleProWiring(op, parentPath, childName)
		return nil
	}
	if len(parentPath) > 0 && parent.Name() != parentPath[len(parentPath)-1] && !slices.Contains(parent.Aliases, parentPath[len(parentPath)-1]) {
		recordStaleProWiring(op, parentPath, childName)
		return nil
	}
	return parent
}

// addSubcommand finds a parent command by path and adds a child to it.
func addSubcommand(root *cobra.Command, parentPath []string, child *cobra.Command) {
	parent := findWiringParent(root, "add", parentPath, child.Name())
	if parent == nil {
		return
	}
	parent.AddCommand(child)
}

// removeSubcommand finds a parent command by path and removes a named child.
func removeSubcommand(root *cobra.Command, parentPath []string, childName string) {
	parent := findWiringParent(root, "remove", parentPath, childName)
	if parent == nil {
		return
	}
	for _, child := range parent.Commands() {
		if child.Name() == childName {
			parent.RemoveCommand(child)
			return
		}
	}
	recordStaleProWiring("remove", parentPath, childName)
}

// replaceSubcommand finds a parent command by path and replaces a named child.
//
// The removal is looked up before the replacement is added, so a stale key is
// recorded against the tree the generator produced rather than against the tree
// this call leaves behind — where the child exists either way and a successful
// replace is indistinguishable from an addition.
func replaceSubcommand(root *cobra.Command, parentPath []string, childName string, replacement *cobra.Command) {
	parent := findWiringParent(root, "replace", parentPath, childName)
	if parent == nil {
		return
	}
	removed := false
	for _, child := range parent.Commands() {
		if child.Name() == childName {
			parent.RemoveCommand(child)
			removed = true
			break
		}
	}
	if !removed {
		recordStaleProWiring("replace", parentPath, childName)
	}
	parent.AddCommand(replacement)
}
