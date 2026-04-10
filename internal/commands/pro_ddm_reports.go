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
			id := args[0]
			// Resolve serial number to ID if needed
			if !strings.Contains(id, "-") {
				dev, err := cliCtx.PlatformClient.GetDeviceBySerialNumber(ctx, id)
				if err != nil {
					return err
				}
				id = dev.ID
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
				if c.DateUpdated != "" {
					row["dateUpdated"] = c.DateUpdated
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
