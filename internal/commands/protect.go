package commands

import (
	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

func newProtectCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "protect",
		Short: "Jamf Protect commands",
		Long:  "Commands for interacting with Jamf Protect — endpoint security, analytics, threat prevention, and configuration.",
	}

	// Core
	cmd.AddCommand(newProtectSetupCmd())
	cmd.AddCommand(newProtectOverviewCmd(cliCtx))
	cmd.AddCommand(newProtectAuthCmd(cliCtx))

	// Security Configuration
	cmd.AddCommand(newProtectPlansCmd(cliCtx))
	cmd.AddCommand(newProtectAnalyticsCmd(cliCtx))
	cmd.AddCommand(newProtectAnalyticSetsCmd(cliCtx))
	cmd.AddCommand(newProtectExceptionSetsCmd(cliCtx))
	cmd.AddCommand(newProtectRemovableStorageControlSetsCmd(cliCtx))
	cmd.AddCommand(newProtectActionConfigsCmd(cliCtx))
	cmd.AddCommand(newProtectTelemetryCmd(cliCtx))
	cmd.AddCommand(newProtectCustomPreventListsCmd(cliCtx))
	cmd.AddCommand(newProtectUnifiedLoggingFiltersCmd(cliCtx))

	// Endpoints
	cmd.AddCommand(newProtectComputersCmd(cliCtx))

	// Organization
	cmd.AddCommand(newProtectDataForwardingCmd(cliCtx))
	cmd.AddCommand(newProtectDataRetentionCmd(cliCtx))
	cmd.AddCommand(newProtectDownloadsCmd(cliCtx))
	cmd.AddCommand(newProtectConfigFreezeCmd(cliCtx))
	cmd.AddCommand(newProtectConnectionsCmd(cliCtx))

	// Access & Identity
	cmd.AddCommand(newProtectRolesCmd(cliCtx))
	cmd.AddCommand(newProtectUsersCmd(cliCtx))
	cmd.AddCommand(newProtectGroupsCmd(cliCtx))
	cmd.AddCommand(newProtectApiClientsCmd(cliCtx))

	// Apply aliases and groups
	applyProtectAliases(cmd)
	applyProtectGroups(cmd)

	return cmd
}
