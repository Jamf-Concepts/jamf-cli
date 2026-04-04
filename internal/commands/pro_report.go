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
		Long: `Generate operational reports by aggregating Jamf Pro inventory and
configuration data into tabular summaries.

Available subcommands:
  patch-status       Per-title patch compliance across the fleet
  device-compliance  Devices with stale check-ins, failed commands, or missing profiles
  inventory-summary  Hardware model and OS version breakdown
  software-installs  Installed software version distribution
  ea-results         Extension attribute results across devices
  policy-status      Policy execution status and health checks`,
	}

	cmd.AddCommand(newReportPatchStatusCmd(cliCtx))
	cmd.AddCommand(newReportDeviceComplianceCmd(cliCtx))
	cmd.AddCommand(newReportInventorySummaryCmd(cliCtx))
	cmd.AddCommand(newReportSoftwareInstallsCmd(cliCtx))
	cmd.AddCommand(newReportEAResultsCmd(cliCtx))
	cmd.AddCommand(newReportSecurityCmd(cliCtx))
	cmd.AddCommand(newReportPolicyStatusCmd(cliCtx))

	return cmd
}
