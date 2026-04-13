// Copyright 2026, Jamf Software LLC

package commands

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfprotect-go-sdk/jamfprotect"
)

func newProtectAuditLogsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "audit-logs",
		Short: "View Jamf Protect audit logs",
	}

	cmd.AddCommand(newProtectAuditLogsListCmd(cliCtx))

	return cmd
}

func newProtectAuditLogsListCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var startStr, endStr string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List audit logs",
		Long: `List audit log entries within a date range.

Defaults to the last 7 days. The SDK enforces a maximum window of 7 days.
Dates must be in RFC3339 format (e.g. 2026-04-06T00:00:00Z).`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			var dateRange *jamfprotect.AuditLogDateRange
			if startStr != "" || endStr != "" {
				dr := &jamfprotect.AuditLogDateRange{}
				if startStr != "" {
					t, err := time.Parse(time.RFC3339, startStr)
					if err != nil {
						return fmt.Errorf("invalid --start value %q: must be RFC3339 (e.g. 2026-04-06T00:00:00Z)", startStr)
					}
					dr.StartDate = t
				}
				if endStr != "" {
					t, err := time.Parse(time.RFC3339, endStr)
					if err != nil {
						return fmt.Errorf("invalid --end value %q: must be RFC3339 (e.g. 2026-04-13T00:00:00Z)", endStr)
					}
					dr.EndDate = t
				} else {
					dr.EndDate = time.Now().UTC()
				}
				if dr.StartDate.IsZero() {
					dr.StartDate = dr.EndDate.AddDate(0, 0, -jamfprotect.MaxAuditLogDays)
				}
				dateRange = dr
			}

			logs, err := cliCtx.ProtectClient.ListAuditLogsByDate(ctx, dateRange)
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(logs))
			for _, l := range logs {
				rows = append(rows, flattenAuditLog(l))
			}
			data, err := json.Marshal(rows)
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}

	cmd.Flags().StringVar(&startStr, "start", "", "Start date in RFC3339 format (default: 7 days ago)")
	cmd.Flags().StringVar(&endStr, "end", "", "End date in RFC3339 format (default: now)")

	return cmd
}

// flattenAuditLog converts an AuditLog to a map for table output.
func flattenAuditLog(l jamfprotect.AuditLog) map[string]any {
	m := map[string]any{
		"date": l.Date,
		"op":   l.Op,
		"user": l.User,
		"ips":  l.IPs,
	}
	if l.ResourceID != "" {
		m["resourceId"] = l.ResourceID
	}
	if l.Error != nil {
		m["error"] = *l.Error
	}
	return m
}
