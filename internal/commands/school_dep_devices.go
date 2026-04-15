// Copyright 2026, Jamf Software LLC

package commands

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfschool-go-sdk/jamfschool"
)

func newSchoolDEPDevicesCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dep-devices",
		Short: "View Jamf School DEP devices",
	}

	cmd.AddCommand(newSchoolDEPDevicesListCmd(cliCtx))
	cmd.AddCommand(newSchoolDEPDevicesGetCmd(cliCtx))

	return cmd
}

func newSchoolDEPDevicesListCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all DEP devices",
		RunE: func(cmd *cobra.Command, _ []string) error {
			items, err := cliCtx.SchoolClient.GetDEPDevices(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(items))
			for _, d := range items {
				rows = append(rows, flattenSchoolDEPDevice(d))
			}
			data, err := json.Marshal(rows)
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}
}

func flattenSchoolDEPDevice(d jamfschool.DEPDevice) map[string]any {
	return map[string]any{
		"id":           d.ID,
		"serialNumber": d.SerialNumber,
		"model":        d.Model,
		"color":        d.Color,
		"status":       d.Status,
		"deviceName":   d.DeviceName,
		"profileName":  d.ProfileName,
		"locationId":   d.LocationID,
	}
}

func newSchoolDEPDevicesGetCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get <serial>",
		Short: "Get a DEP device by serial number",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			item, err := cliCtx.SchoolClient.GetDEPDevice(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return printResult(cliCtx.Output, item, flattenSchoolDEPDevice(*item))
		},
	}
}
