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

	// Generated modern API commands
	generated.RegisterCommands(cmd, cliCtx)

	// Generated Classic API commands
	generated.RegisterClassicCommands(cmd, cliCtx)

	// Replace broken generated upload with handwritten streaming upload
	replaceSubcommand(cmd, []string{"packages"}, "upload", newPackagesUploadCmd(cliCtx))

	// Apply aliases and groups to pro's children
	applyAliases(cmd)
	applyProGroups(cmd)

	return cmd
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
