// Copyright 2026, Jamf Software LLC

package commands

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfprotect-go-sdk/jamfprotect"
)

func newProtectAlertsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "alerts",
		Short: "Manage Jamf Protect alerts",
	}

	cmd.AddCommand(newProtectAlertsListCmd(cliCtx))
	cmd.AddCommand(newProtectAlertsGetCmd(cliCtx))
	cmd.AddCommand(newProtectAlertsUpdateStatusCmd(cliCtx))
	cmd.AddCommand(newProtectAlertsStatusCountsCmd(cliCtx))

	return cmd
}

func newProtectAlertsListCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all alerts",
		RunE: func(cmd *cobra.Command, _ []string) error {
			alerts, err := cliCtx.ProtectClient.ListAlerts(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(alerts))
			for _, a := range alerts {
				rows = append(rows, flattenAlert(a))
			}
			data, err := json.Marshal(rows)
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}
}

func newProtectAlertsGetCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get <uuid>",
		Short: "Get an alert by UUID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			alert, err := cliCtx.ProtectClient.GetAlert(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return printResult(cliCtx.Output, alert, flattenAlert(*alert))
		},
	}
}

func newProtectAlertsUpdateStatusCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var uuids []string
	var status string

	cmd := &cobra.Command{
		Use:   "update-status",
		Short: "Bulk-update alert status",
		Long: `Bulk-update the status of one or more alerts.

Valid statuses: New, InProgress, Resolved`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if len(uuids) == 0 {
				return fmt.Errorf("--uuid is required (may be specified multiple times)")
			}
			validStatuses := []string{"New", "InProgress", "Resolved"}
			statusOK := false
			for _, v := range validStatuses {
				if status == v {
					statusOK = true
					break
				}
			}
			if !statusOK {
				return fmt.Errorf("invalid --status %q: must be one of New, InProgress, Resolved", status)
			}
			updated, err := cliCtx.ProtectClient.UpdateAlerts(cmd.Context(), jamfprotect.AlertUpdateInput{
				UUIDs:  uuids,
				Status: status,
			})
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(updated))
			for _, a := range updated {
				rows = append(rows, flattenAlert(a))
			}
			data, err := json.Marshal(rows)
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}

	cmd.Flags().StringArrayVar(&uuids, "uuid", nil, "Alert UUID to update (repeatable)")
	cmd.Flags().StringVar(&status, "status", "", "New status: New, InProgress, or Resolved")
	_ = cmd.MarkFlagRequired("status")

	return cmd
}

func newProtectAlertsStatusCountsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "status-counts",
		Short: "Show alert counts by status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			counts, err := cliCtx.ProtectClient.GetAlertStatusCounts(cmd.Context())
			if err != nil {
				return err
			}
			row := map[string]any{
				"new":          counts.New,
				"inProgress":   counts.InProgress,
				"resolved":     counts.Resolved,
				"autoResolved": counts.AutoResolved,
			}
			data, err := json.Marshal([]map[string]any{row})
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}
}

// flattenAlert converts an Alert to a map for table output.
func flattenAlert(a jamfprotect.Alert) map[string]any {
	m := map[string]any{
		"uuid":      a.UUID,
		"status":    a.Status,
		"severity":  a.Severity,
		"eventType": a.EventType,
		"received":  a.Received,
		"created":   a.Created,
	}
	if a.Computer != nil {
		m["computer"] = a.Computer.HostName
	}
	if len(a.Analytics) > 0 {
		names := make([]string, 0, len(a.Analytics))
		for _, an := range a.Analytics {
			names = append(names, an.Name)
		}
		m["analytics"] = strings.Join(names, ", ")
	}
	return m
}
