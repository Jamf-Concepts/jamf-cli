// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/output"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

func newReportInventorySummaryCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		groupFilter string
		groupBy     string
	)

	cmd := &cobra.Command{
		Use:   "inventory-summary",
		Short: "Hardware model and OS version breakdown",
		Long: `Aggregate computer inventory into a breakdown by hardware model and OS version.

Use --group to filter to a specific computer group name or ID.
Use --by to choose the grouping dimension: "model" (default), "os", or "both".

Output columns vary by --by flag:
  model: model, count
  os:    os_version, count
  both:  model, os_version, count (default)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			rows, err := runReportInventorySummary(cmd.Context(), cliCtx.Client, groupFilter, groupBy)
			if err != nil {
				return err
			}
			formatter := output.New(outputFmt, noColor, wide)
			return formatter.Print(rows)
		},
	}

	cmd.Flags().StringVar(&groupFilter, "group", "", "filter to a computer group name or ID")
	cmd.Flags().StringVar(&groupBy, "by", "both", "grouping dimension: model, os, or both")

	return cmd
}

// inventoryKey is used to bucket computers by model + OS version.
type inventoryKey struct {
	model     string
	osVersion string
}

// runReportInventorySummary fetches computer inventory and aggregates counts.
// groupBy controls the bucketing: "model", "os", or "both" (default).
func runReportInventorySummary(ctx context.Context, client registry.HTTPClient, groupFilter, groupBy string) ([]map[string]any, error) {
	basePath := "/v3/computers-inventory?section=HARDWARE&section=OPERATING_SYSTEM"
	if groupFilter != "" {
		basePath = fmt.Sprintf("%s&filter=general.groupMemberships.groupName%%3D%%3D\"%s\"",
			basePath, groupFilter)
	}

	computers, err := FetchAllPaginated(ctx, client, basePath, 100)
	if err != nil {
		return nil, fmt.Errorf("fetching computer inventory: %w", err)
	}

	counts := make(map[inventoryKey]int)

	for _, c := range computers {
		model := "Unknown"
		osVersion := "Unknown"

		if hw, ok := c["hardware"].(map[string]any); ok {
			if m, ok := hw["model"].(string); ok && m != "" {
				model = m
			}
		}
		if os, ok := c["operatingSystem"].(map[string]any); ok {
			if v, ok := os["version"].(string); ok && v != "" {
				osVersion = v
			}
		}

		// Build the key based on groupBy dimension
		var key inventoryKey
		switch groupBy {
		case "model":
			key = inventoryKey{model: model}
		case "os":
			key = inventoryKey{osVersion: osVersion}
		default: // "both"
			key = inventoryKey{model: model, osVersion: osVersion}
		}
		counts[key]++
	}

	keys := make([]inventoryKey, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].model != keys[j].model {
			return keys[i].model < keys[j].model
		}
		return keys[i].osVersion > keys[j].osVersion
	})

	rows := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		row := map[string]any{"count": counts[k]}
		switch groupBy {
		case "model":
			row["model"] = k.model
		case "os":
			row["os_version"] = k.osVersion
		default:
			row["model"] = k.model
			row["os_version"] = k.osVersion
		}
		rows = append(rows, row)
	}

	return rows, nil
}
