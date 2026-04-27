// Copyright 2026, Jamf Software LLC

package commands

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/platform"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

func newDDMReportsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ddm-reports",
		Short: "DDM declaration reports (Platform API)",
		Long:  "View Declarative Device Management status reports. Requires platform gateway auth.",
	}

	cmd.AddCommand(newDDMDeviceReportCmd(cliCtx))
	cmd.AddCommand(newDDMDeclarationReportCmd(cliCtx))
	cmd.AddCommand(newDDMDeclarationErrorsCmd(cliCtx))

	return cmd
}

func newDDMDeviceReportCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "device <id|serial>",
		Short: "Get declaration report for a device",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			ctx := cmd.Context()
			id, err := resolveDeviceID(ctx, cliCtx.PlatformClient, args[0])
			if err != nil {
				return err
			}
			report, err := cliCtx.PlatformClient.GetDeviceDeclarationReport(ctx, id)
			if err != nil {
				return err
			}
			return platform.PrintOne(cliCtx.Output, report)
		},
	}
}

func newDDMDeclarationReportCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var sortFields []string
	cmd := &cobra.Command{
		Use:   "declaration <declaration-identifier>",
		Short: "List devices reporting a declaration",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			clients, err := cliCtx.PlatformClient.ListDeclarationReportClients(cmd.Context(), args[0], sortFields)
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(clients))
			for _, c := range clients {
				row := map[string]any{
					"deviceId":      c.DeviceID,
					"channel":       c.Channel,
					"active":        c.Active,
					"validityState": c.ValidityState,
					"serverToken":   c.ServerToken,
				}
				if c.DateUpdated != nil {
					row["dateUpdated"] = c.DateUpdated.Format("2006-01-02T15:04:05Z07:00")
				}
				rows = append(rows, row)
			}
			data, err := json.Marshal(rows)
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}
	cmd.Flags().StringSliceVar(&sortFields, "sort", nil, "Sort fields")
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
			clients, err := cliCtx.PlatformClient.ListDeclarationReportClients(cmd.Context(), args[0], nil)
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
