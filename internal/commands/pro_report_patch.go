package commands

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/output"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

func newReportPatchStatusCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "patch-status",
		Short: "Per-title patch compliance across the fleet",
		Long: `Fetch all patch software title configurations and report compliance
percentages per title.

Output columns: title, id, installed, total, latest, compliance_pct`,
		RunE: func(cmd *cobra.Command, args []string) error {
			rows, err := runReportPatchStatus(cmd.Context(), cliCtx.Client)
			if err != nil {
				return err
			}
			formatter := output.New(outputFmt, noColor, wide)
			return formatter.Print(rows)
		},
	}
}

// runReportPatchStatus fetches patch title configurations and computes
// per-title compliance metrics.
func runReportPatchStatus(ctx context.Context, client registry.HTTPClient) ([]map[string]interface{}, error) {
	titles, err := FetchAllPaginated(ctx, client, "/v2/patch-software-title-configurations", 100)
	if err != nil {
		return nil, fmt.Errorf("fetching patch title configurations: %w", err)
	}

	rows := make([]map[string]interface{}, 0, len(titles))
	for _, t := range titles {
		titleName, _ := t["softwareTitleName"].(string)
		if titleName == "" {
			titleName, _ = t["displayName"].(string)
		}
		if titleName == "" {
			titleName, _ = t["id"].(string)
		}

		titleID := extractID(t)

		summary, _ := t["patchSummary"].(map[string]interface{})

		var installed, total int
		var latestVersion string

		if summary != nil {
			if v, ok := summary["installedCount"].(float64); ok {
				installed = int(v)
			}
			if v, ok := summary["totalCount"].(float64); ok {
				total = int(v)
			}
			if v, ok := summary["latestVersion"].(string); ok {
				latestVersion = v
			}
		}

		if latestVersion == "" {
			latestVersion, _ = t["latestVersion"].(string)
		}

		compliancePct := "N/A"
		if total > 0 {
			pct := float64(installed) / float64(total) * 100
			compliancePct = fmt.Sprintf("%.1f%%", pct)
		}

		rows = append(rows, map[string]interface{}{
			"title":          titleName,
			"id":             titleID,
			"installed":      installed,
			"total":          total,
			"latest":         latestVersion,
			"compliance_pct": compliancePct,
		})
	}

	return rows, nil
}
