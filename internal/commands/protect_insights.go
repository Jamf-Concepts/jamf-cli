// Copyright 2026, Jamf Software LLC

package commands

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/protect"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfprotect-go-sdk/jamfprotect"
)

func newProtectInsightsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "insights",
		Short: "Manage Jamf Protect CIS benchmark insights",
	}

	cmd.AddCommand(newProtectInsightsListCmd(cliCtx))
	cmd.AddCommand(newProtectInsightsEnableCmd(cliCtx))
	cmd.AddCommand(newProtectInsightsDisableCmd(cliCtx))
	cmd.AddCommand(newProtectInsightsComputersCmd(cliCtx))
	cmd.AddCommand(newProtectInsightsComplianceScoreCmd(cliCtx))

	return cmd
}

func newProtectInsightsListCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all CIS benchmark insights",
		RunE: func(cmd *cobra.Command, _ []string) error {
			insights, err := cliCtx.ProtectClient.ListInsights(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(insights))
			for _, i := range insights {
				rows = append(rows, flattenInsight(i))
			}
			data, err := json.Marshal(rows)
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}
}

func newProtectInsightsEnableCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "enable <label>",
		Short: "Enable an insight by label",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)
			uuid, err := r.ResolveInsightUUID(ctx, args[0])
			if err != nil {
				return err
			}
			insight, err := cliCtx.ProtectClient.UpdateInsightStatus(ctx, uuid, true)
			if err != nil {
				return err
			}
			return printResult(cliCtx.Output, insight, flattenInsight(insight))
		},
	}
}

func newProtectInsightsDisableCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "disable <label>",
		Short: "Disable an insight by label",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)
			uuid, err := r.ResolveInsightUUID(ctx, args[0])
			if err != nil {
				return err
			}
			insight, err := cliCtx.ProtectClient.UpdateInsightStatus(ctx, uuid, false)
			if err != nil {
				return err
			}
			return printResult(cliCtx.Output, insight, flattenInsight(insight))
		},
	}
}

func newProtectInsightsComputersCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "computers <label>",
		Short: "List computers affected by an insight",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)
			uuid, err := r.ResolveInsightUUID(ctx, args[0])
			if err != nil {
				return err
			}
			computers, err := cliCtx.ProtectClient.ListInsightComputers(ctx, uuid)
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(computers))
			for _, c := range computers {
				rows = append(rows, flattenInsightComputer(c))
			}
			data, err := json.Marshal(rows)
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}
}

func newProtectInsightsComplianceScoreCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var date string

	cmd := &cobra.Command{
		Use:   "compliance-score",
		Short: "Get fleet CIS compliance baseline score",
		Long: `Get the fleet CIS compliance baseline score.

Omit --date for today's score, or pass an ISO date (e.g. 2026-03-12) for a historical score.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if date != "" {
				if _, err := time.Parse("2006-01-02", date); err != nil {
					return fmt.Errorf("invalid --date %q: must be YYYY-MM-DD (e.g. 2026-03-12)", date)
				}
			}
			score, err := cliCtx.ProtectClient.GetFleetComplianceScore(cmd.Context(), date)
			if err != nil {
				return err
			}
			row := map[string]any{
				"score":   fmt.Sprintf("%.1f%%", score.Score),
				"updated": score.Updated,
			}
			data, err := json.Marshal([]map[string]any{row})
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}

	cmd.Flags().StringVar(&date, "date", "", "ISO date for historical score (e.g. 2026-03-12); omit for today")

	return cmd
}

// flattenInsight converts an Insight to a map for table output.
func flattenInsight(i jamfprotect.Insight) map[string]any {
	cisIDs := make([]string, 0, len(i.CisID))
	for _, c := range i.CisID {
		cisIDs = append(cisIDs, c.ID)
	}
	return map[string]any{
		"label":     i.Label,
		"section":   i.Section,
		"enabled":   i.Enabled,
		"totalPass": i.TotalPass,
		"totalFail": i.TotalFail,
		"totalNone": i.TotalNone,
		"cisIDs":    strings.Join(cisIDs, ", "),
	}
}

// flattenInsightComputer converts an InsightComputer to a map for table output.
func flattenInsightComputer(c jamfprotect.InsightComputer) map[string]any {
	return map[string]any{
		"uuid":            c.UUID,
		"hostName":        c.HostName,
		"statsFail":       c.InsightsStatsFail,
		"statsPass":       c.InsightsStatsPass,
		"statsUnknown":    c.InsightsStatsUnknown,
		"insightsUpdated": c.InsightsUpdated,
	}
}
