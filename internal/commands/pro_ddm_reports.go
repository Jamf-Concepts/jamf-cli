// Copyright 2026, Jamf Software LLC

package commands

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	platformgen "github.com/Jamf-Concepts/jamf-cli/internal/commands/platform/generated"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/ddmreport"
)

// ddmAllDeclarationsFilter matches every declaration. The filtered DDM report
// endpoints (/devices/{id}/declarations and /declarations/{id}/devices) that
// replaced the deprecated non-filtered endpoints require a "filter" query
// param — omitting it returns HTTP 400. "active" is a non-nullable boolean on
// every declaration, so in=(true,false) is a tautology that returns all rows.
const ddmAllDeclarationsFilter = "active=in=(true,false)"

func newDDMReportsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ddm-reports",
		Short: "DDM declaration reports (Platform API)",
		Long:  "View Declarative Device Management status reports. Requires platform gateway auth.",
	}

	// Generated: raw declaration/device report queries
	declCmd := platformgen.NewDeclarationReportsCmd(cliCtx)
	declCmd.Use = "declaration"
	declCmd.Short = "Get declaration status report for all devices"
	cmd.AddCommand(declCmd)

	devCmd := platformgen.NewDeviceReportsCmd(cliCtx)
	devCmd.Use = "device"
	devCmd.Short = "Get all declaration statuses for a device"
	cmd.AddCommand(devCmd)

	// Business logic: filtered error view
	cmd.AddCommand(newDDMDeclarationErrorsCmd(cliCtx))

	return cmd
}

func newDDMDeclarationErrorsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "errors <declaration-identifier>",
		Short: "List devices with failed declarations and error reasons",
		Long: `Shows only devices where a declaration has an UNSUCCESSFUL status or INVALID
validity state, including the error reason codes and descriptions.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			clients, err := ddmreport.New(cliCtx.PlatformSDKClient).ListDeclarationReportClientsFiltered(cmd.Context(), args[0], ddmAllDeclarationsFilter, nil)
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0)
			for _, c := range clients {
				if c.ValidityState == "VALID" && len(c.Reasons) == 0 {
					continue
				}
				// Filter out non-actionable reasons
				var actionableReasons []string
				for _, r := range c.Reasons {
					if ignorableDDMReasonCodes[r.Code] {
						continue
					}
					actionableReasons = append(actionableReasons, r.Code+": "+r.Description)
				}
				// Skip entirely if only reason was the non-actionable one
				if c.ValidityState == "VALID" && len(actionableReasons) == 0 {
					continue
				}
				row := map[string]any{
					"deviceId":      c.DeviceID,
					"channel":       c.Channel,
					"active":        c.Active,
					"validityState": c.ValidityState,
				}
				if c.DateUpdated != nil {
					row["dateUpdated"] = c.DateUpdated.Format("2006-01-02T15:04:05Z07:00")
				}
				if len(actionableReasons) > 0 {
					row["reasons"] = strings.Join(actionableReasons, "; ")
				}
				rows = append(rows, row)
			}
			if len(rows) == 0 {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "No errors found for this declaration.")
				return nil
			}
			data, err := json.Marshal(rows)
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}
}
