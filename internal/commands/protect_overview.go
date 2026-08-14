// Copyright 2026, Jamf Software LLC

package commands

import (
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/output"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// runProtectOverview executes all Jamf Protect API calls in parallel and returns
// grouped sections for display.
func runProtectOverview(cmd *cobra.Command, cliCtx *registry.CLIContext) ([]overviewSection, error) {
	ctx := cmd.Context()
	client := cliCtx.ProtectClient

	var mu sync.Mutex
	results := make(map[string]string)
	colorHints := make(map[string]string)
	var wg sync.WaitGroup

	send := func(key, value string, err error) {
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			results[key] = "N/A"
		} else {
			results[key] = value
		}
	}

	sendWithColor := func(key, value, color string, err error) {
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			results[key] = "N/A"
		} else {
			results[key] = value
			if color != "" {
				colorHints[key] = color
			}
		}
	}

	// Security Configuration

	wg.Go(func() {
		plans, err := client.ListPlans(ctx)
		send("plans", formatCount(len(plans)), err)
	})

	wg.Go(func() {
		analytics, err := client.ListAnalytics(ctx)
		send("analytics", formatCount(len(analytics)), err)
	})

	wg.Go(func() {
		sets, err := client.ListAnalyticSets(ctx)
		send("analytic_sets", formatCount(len(sets)), err)
	})

	wg.Go(func() {
		sets, err := client.ListExceptionSets(ctx)
		send("exception_sets", formatCount(len(sets)), err)
	})

	wg.Go(func() {
		configs, err := client.ListActionConfigs(ctx)
		send("action_configs", formatCount(len(configs)), err)
	})

	wg.Go(func() {
		telemetries, err := client.ListTelemetriesV2(ctx)
		send("telemetry_configs", formatCount(len(telemetries)), err)
	})

	wg.Go(func() {
		sets, err := client.ListRemovableStorageControlSets(ctx)
		send("usb_control_sets", formatCount(len(sets)), err)
	})

	wg.Go(func() {
		lists, err := client.ListCustomPreventLists(ctx)
		send("custom_prevent_lists", formatCount(len(lists)), err)
	})

	wg.Go(func() {
		filters, err := client.ListUnifiedLoggingFilters(ctx)
		send("unified_logging_filters", formatCount(len(filters)), err)
	})

	wg.Go(func() {
		sets, err := client.ListUnifiedLoggingFilterSets(ctx)
		send("unified_logging_filter_sets", formatCount(len(sets)), err)
	})

	// Endpoints

	wg.Go(func() {
		computers, err := client.ListComputers(ctx)
		send("computers", formatCount(len(computers)), err)
	})

	wg.Go(func() {
		counts, err := client.GetAlertStatusCounts(ctx)
		if err != nil {
			send("alerts_new", "", err)
			return
		}
		if counts.New > 0 {
			sendWithColor("alerts_new", fmt.Sprintf("%d New  %d In Progress", counts.New, counts.InProgress), "yellow", nil)
		} else {
			send("alerts_new", fmt.Sprintf("%d New  %d In Progress", counts.New, counts.InProgress), nil)
		}
	})

	wg.Go(func() {
		score, err := client.GetFleetComplianceScore(ctx, "")
		if err != nil {
			send("compliance_score", "", err)
			return
		}
		send("compliance_score", fmt.Sprintf("%.1f%%", score.Score), nil)
	})

	// Organization

	wg.Go(func() {
		cfg, err := client.GetConfigFreeze(ctx)
		if err != nil {
			send("config_freeze", "", err)
			return
		}
		if cfg.ConfigFreeze {
			sendWithColor("config_freeze", "enabled", "yellow", nil)
		} else {
			send("config_freeze", "disabled", nil)
		}
	})

	wg.Go(func() {
		df, err := client.GetDataForwarding(ctx)
		if err != nil {
			send("data_forwarding", "", err)
			return
		}
		var destinations []string
		if df.Forward.S3.Enabled {
			destinations = append(destinations, "S3")
		}
		if df.Forward.Sentinel.Enabled {
			destinations = append(destinations, "Sentinel")
		}
		if df.Forward.SentinelV2.Enabled {
			destinations = append(destinations, "Sentinel v2")
		}
		if len(destinations) == 0 {
			send("data_forwarding", "disabled", nil)
		} else {
			send("data_forwarding", strings.Join(destinations, ", "), nil)
		}
	})

	wg.Go(func() {
		connections, err := client.ListConnections(ctx)
		send("connections", formatCount(len(connections)), err)
	})

	// Access & Identity

	wg.Go(func() {
		roles, err := client.ListRoles(ctx)
		send("roles", formatCount(len(roles)), err)
	})

	wg.Wait()

	// Build sections
	get := func(key string) string {
		if v, ok := results[key]; ok {
			return v
		}
		return "N/A"
	}

	getItem := func(resource, key string) overviewItem {
		return overviewItem{resource, get(key), colorHints[key]}
	}

	item := func(resource, value string) overviewItem {
		return overviewItem{resource, value, ""}
	}

	sections := []overviewSection{
		{
			Name: "Endpoints",
			Items: []overviewItem{
				item("Computers", get("computers")),
				getItem("Alerts", "alerts_new"),
				item("CIS Compliance Score", get("compliance_score")),
			},
		},
		{
			Name: "Security Configuration",
			Items: []overviewItem{
				item("Plans", get("plans")),
				item("Analytics", get("analytics")),
				item("Analytic Sets", get("analytic_sets")),
				item("Exception Sets", get("exception_sets")),
				item("Action Configs", get("action_configs")),
				item("Telemetry Configs", get("telemetry_configs")),
				item("USB Control Sets", get("usb_control_sets")),
				item("Custom Prevent Lists", get("custom_prevent_lists")),
				item("Unified Logging Filters", get("unified_logging_filters")),
				item("Unified Logging Filter Sets", get("unified_logging_filter_sets")),
			},
		},
		{
			Name: "Organization",
			Items: []overviewItem{
				getItem("Config Freeze", "config_freeze"),
				item("Data Forwarding", get("data_forwarding")),
				item("Identity Connections", get("connections")),
			},
		},
		{
			Name: "Access",
			Items: []overviewItem{
				item("Roles", get("roles")),
			},
		},
	}

	return sections, nil
}

// printProtectOverviewTable renders a grouped overview table for Jamf Protect.
func printProtectOverviewTable(w io.Writer, sections []overviewSection, useColor bool) {
	colorize := func(text, code string) string {
		if !useColor {
			return text
		}
		return code + text + "\033[0m"
	}

	bold := "\033[1m"
	green := "\033[32m"
	yellow := "\033[33m"
	red := "\033[31m"
	dim := "\033[2m"

	const labelWidth = 34
	const totalWidth = 72

	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, colorize("  JAMF PROTECT OVERVIEW", bold))
	_, _ = fmt.Fprintln(w, colorize("  "+strings.Repeat("━", totalWidth), dim))

	for _, section := range sections {
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintf(w, "  %s\n", colorize(section.Name, bold))
		_, _ = fmt.Fprintln(w, colorize("  "+strings.Repeat("─", totalWidth), dim))

		for _, item := range section.Items {
			if item.Resource == "" && item.Value == "" {
				_, _ = fmt.Fprintln(w)
				continue
			}

			displayValue := item.Value
			visibleLen := len(item.Value)

			switch {
			case item.ColorHint == "red":
				displayValue = colorize(item.Value+" ●", red)
				visibleLen += 2
			case item.ColorHint == "yellow":
				displayValue = colorize(item.Value+" ●", yellow)
				visibleLen += 2
			case item.Value == "enabled":
				displayValue = colorize(item.Value+" ●", green)
				visibleLen += 2
			case item.Value == "disabled":
				displayValue = colorize(item.Value+" ○", dim)
				visibleLen += 2
			case item.Value == "N/A":
				displayValue = colorize(item.Value, dim)
			}

			padding := totalWidth - labelWidth - visibleLen
			if padding >= 1 {
				_, _ = fmt.Fprintf(w, "  %-*s%*s%s\n", labelWidth, item.Resource, padding, "", displayValue)
			} else {
				_, _ = fmt.Fprintf(w, "  %-*s %s\n", labelWidth, item.Resource, displayValue)
			}
		}
	}
	_, _ = fmt.Fprintln(w)
}

func newProtectOverviewCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "overview",
		Short: "Show a summary of the Jamf Protect instance",
		Long: `Display a grouped summary of your Jamf Protect instance including endpoint
counts, security configuration, data forwarding status, and access controls.

Makes parallel API calls for fast results. Items that fail to load show "N/A".`,
		RunE: func(cmd *cobra.Command, args []string) error {
			sections, err := runProtectOverview(cmd, cliCtx)
			if err != nil {
				return err
			}

			if !cmd.Flags().Changed("output") || outputFmt == "table" {
				printProtectOverviewTable(cmd.OutOrStdout(), sections, !noColor)
				return nil
			}

			rows := overviewToRows(sections)
			formatter := output.New(outputFmt, noColor, wide)
			return formatter.Print(rows)
		},
	}
}
