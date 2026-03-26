package commands

import (
	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamfpro-cli/internal/commands/generated"
)

// newReportCmd creates the report parent command group.
func newReportCmd(cliCtx *generated.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Generate operational reports from Jamf Pro data",
		Long: `Generate operational reports by aggregating Jamf Pro inventory and
configuration data into tabular summaries.

Available subcommands:
  patch-status       Per-title patch compliance across the fleet
  device-compliance  Devices with stale check-ins, failed commands, or missing profiles
  inventory-summary  Hardware model and OS version breakdown
  software-installs  Installed software version distribution
  ea-results         Extension attribute results across devices`,
	}

	cmd.AddCommand(newReportPatchStatusCmd(cliCtx))
	cmd.AddCommand(newReportDeviceComplianceCmd(cliCtx))
	cmd.AddCommand(newReportInventorySummaryCmd(cliCtx))
	cmd.AddCommand(newReportSoftwareInstallsCmd(cliCtx))
	cmd.AddCommand(newReportEAResultsCmd(cliCtx))

	return cmd
}
