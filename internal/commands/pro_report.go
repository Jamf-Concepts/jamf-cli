// Copyright 2026, Jamf Software LLC

package commands

import (
	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// newReportCmd creates the report parent command group.
func newReportCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Generate operational reports from Jamf Pro data",
		// Do not enumerate subcommands here — cobra renders the real, complete
		// list under "Available Commands:" from the AddCommand calls below.
		// A hand-maintained copy drifts (see issue #327).
		Long: `Generate operational reports by aggregating Jamf Pro inventory and
configuration data into tabular summaries.`,
	}

	cmd.AddCommand(newReportPatchStatusCmd(cliCtx))
	cmd.AddCommand(newReportDeviceComplianceCmd(cliCtx))
	cmd.AddCommand(newReportInventorySummaryCmd(cliCtx))
	cmd.AddCommand(newReportSoftwareInstallsCmd(cliCtx))
	cmd.AddCommand(newReportEAResultsCmd(cliCtx))
	cmd.AddCommand(newReportSecurityCmd(cliCtx))
	cmd.AddCommand(newReportPolicyStatusCmd(cliCtx))
	cmd.AddCommand(newReportProfileStatusCmd(cliCtx))
	cmd.AddCommand(newReportAppStatusCmd(cliCtx))
	cmd.AddCommand(newReportUpdateStatusCmd(cliCtx))
	cmd.AddCommand(newReportDuplicateSerialsCmd(cliCtx))

	// Platform API reports
	cmd.AddCommand(newReportBlueprintStatusCmd(cliCtx))
	cmd.AddCommand(newReportComplianceRulesCmd(cliCtx))
	cmd.AddCommand(newReportComplianceDevicesCmd(cliCtx))
	cmd.AddCommand(newReportDDMStatusCmd(cliCtx))

	return cmd
}
