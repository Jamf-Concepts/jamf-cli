// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/output"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

func newReportPatchStatusCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var scanFailures bool

	cmd := &cobra.Command{
		Use:   "patch-status",
		Short: "Patch compliance and policy failure report",
		Long: `Fetch all patch software title configurations and report compliance
percentages per title.

Use --scan-failures to also fetch patch policy failure counts. This
queries the patch policies list endpoint for per-policy status counts.

Output columns: title, id, on_latest, on_other, total, latest, compliance_pct`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("output") {
				outputFmt = "table"
			}
			return runReportPatchStatusFull(cmd.Context(), cliCtx.Client, scanFailures)
		},
	}

	cmd.Flags().BoolVar(&scanFailures, "scan-failures", false, "include patch policy failure counts")
	return cmd
}

func runReportPatchStatusFull(ctx context.Context, client registry.HTTPClient, scanFailures bool) error {
	rows, err := runReportPatchStatus(ctx, client)
	if err != nil {
		return err
	}

	formatter := output.New(outputFmt, noColor, wide)

	if !scanFailures {
		return formatter.Print(rows)
	}

	// Print compliance section
	fmt.Println("── Patch Title Compliance ──")
	if err := formatter.Print(rows); err != nil {
		return err
	}

	// Fetch patch policy failure counts
	policyRows, err := runReportPatchPolicyFailures(ctx, client)
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: failed to fetch patch policy failures: %v\n", err)
		return nil
	}

	if len(policyRows) > 0 {
		fmt.Printf("\n── Patch Policies With Failures (%d) ──\n", len(policyRows))
		if err := formatter.Print(policyRows); err != nil {
			return err
		}

		// Fetch device-level failures for policies that have them
		rawDeviceRows, err := fetchPatchDeviceFailures(ctx, client, policyRows)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: failed to fetch device-level failures: %v\n", err)
		} else if len(rawDeviceRows) > 0 {
			// Enrich with inventory data
			lookup := fetchUpdateDeviceLookup(ctx, client)
			for i, row := range rawDeviceRows {
				devID, _ := row["device_id"].(string)
				meta := lookup[devID]
				rawDeviceRows[i]["serial"] = meta.serial
				rawDeviceRows[i]["os_version"] = meta.osVersion
				rawDeviceRows[i]["username"] = meta.username
			}
			fmt.Printf("\n── Devices With Patch Failures (%d) ──\n", len(rawDeviceRows))
			return formatter.Print(rawDeviceRows)
		}
	} else {
		fmt.Fprintln(os.Stderr, "\nNo patch policy failures found.")
	}

	return nil
}

// fetchPatchDeviceFailures queries per-policy logs for policies that have
// failures and returns per-device failure rows.
func fetchPatchDeviceFailures(ctx context.Context, client registry.HTTPClient, policyRows []map[string]any) ([]map[string]any, error) {
	var deviceRows []map[string]any

	for _, pr := range policyRows {
		policyID := extractField(pr, "policy_id")
		policyName, _ := pr["policy"].(string)
		if policyID == "" {
			continue
		}

		path := fmt.Sprintf("/v2/patch-policies/%s/logs", policyID)
		logs, err := FetchAllPaginated(ctx, client, path, 200)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: failed to fetch logs for policy %s: %v\n", policyID, err)
			continue
		}

		for _, l := range logs {
			if strVal(l, "statusEnum") != "FAILED" {
				continue
			}
			deviceID := strVal(l, "deviceId")
			deviceRows = append(deviceRows, map[string]any{
				"policy":      policyName,
				"policy_id":   policyID,
				"device":      strVal(l, "deviceName"),
				"device_id":   deviceID,
				"status_date": strVal(l, "statusDate"),
				"attempt":     extractField(l, "attemptNumber"),
				"last_action": fetchPatchLogLastAction(ctx, client, policyID, deviceID),
			})
		}
	}

	return deviceRows, nil
}

// fetchPatchLogLastAction returns the last action string from the most recent
// attempt in the per-device details log for a given policy+device pair.
// Returns empty string if the endpoint is unavailable or returns no actions.
func fetchPatchLogLastAction(ctx context.Context, client registry.HTTPClient, policyID, deviceID string) string {
	path := fmt.Sprintf("/v2/patch-policies/%s/logs/%s/details", policyID, deviceID)
	details, err := FetchAllPaginated(ctx, client, path, 0)
	if err != nil || len(details) == 0 {
		return ""
	}
	// Find the attempt with the highest attemptNumber.
	var lastAttempt map[string]any
	var maxAttempt float64
	for _, d := range details {
		n, _ := d["attemptNumber"].(float64)
		if n >= maxAttempt {
			maxAttempt = n
			lastAttempt = d
		}
	}
	if lastAttempt == nil {
		return ""
	}
	// Find the action with the highest actionOrder within that attempt.
	actions, _ := lastAttempt["actions"].([]any)
	var lastAction string
	var maxOrder float64
	for _, a := range actions {
		am, _ := a.(map[string]any)
		order, _ := am["actionOrder"].(float64)
		if order >= maxOrder {
			maxOrder = order
			lastAction, _ = am["action"].(string)
		}
	}
	return lastAction
}

// runReportPatchPolicyFailures fetches patch policies and returns those
// with failures, sorted by failure count descending.
func runReportPatchPolicyFailures(ctx context.Context, client registry.HTTPClient) ([]map[string]any, error) {
	policies, err := FetchAllPaginated(ctx, client, "/v2/patch-policies", 200)
	if err != nil {
		return nil, err
	}

	var rows []map[string]any
	for _, p := range policies {
		failed, _ := p["failed"].(float64)
		if failed == 0 {
			continue
		}
		pending, _ := p["pending"].(float64)
		completed, _ := p["completed"].(float64)
		deferred, _ := p["deferred"].(float64)
		total := pending + completed + deferred + failed

		name := strVal(p, "name")
		if name == "" {
			name = extractID(p)
		}

		rows = append(rows, map[string]any{
			"policy":       name,
			"policy_id":    extractID(p),
			"failed":       int(failed),
			"completed":    int(completed),
			"pending":      int(pending),
			"deferred":     int(deferred),
			"failure_rate": pctStr(int(failed), int(total)),
		})
	}

	sort.Slice(rows, func(i, j int) bool {
		fi, _ := rows[i]["failed"].(int)
		fj, _ := rows[j]["failed"].(int)
		return fi > fj
	})

	return rows, nil
}

// runReportPatchStatus fetches patch title configurations and their summaries
// to compute per-title compliance metrics.
func runReportPatchStatus(ctx context.Context, client registry.HTTPClient) ([]map[string]any, error) {
	titles, err := FetchAllPaginated(ctx, client, "/v3/patch-software-title-configurations", 100)
	if err != nil {
		return nil, fmt.Errorf("fetching patch title configurations: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Fetching summaries for %d patch titles...\n", len(titles))

	// Fetch each title's summary in parallel (bounded)
	type titleWithSummary struct {
		id, name string
		summary  map[string]any
	}

	results, errs := BoundedParallelFetch(ctx, titles, 3,
		func(ctx context.Context, t map[string]any) (titleWithSummary, error) {
			titleID := extractID(t)
			titleName, _ := t["displayName"].(string)
			if titleName == "" {
				titleName = titleID
			}

			path := fmt.Sprintf("/v3/patch-software-title-configurations/%s/patch-summary", titleID)
			summary, err := fetchJSON(ctx, client, path)
			if err != nil {
				return titleWithSummary{id: titleID, name: titleName}, nil // non-fatal
			}
			return titleWithSummary{id: titleID, name: titleName, summary: summary}, nil
		})

	if len(errs) > 0 {
		fmt.Fprintf(os.Stderr, "WARNING: %d patch summary fetches failed.\n", len(errs))
	}

	rows := make([]map[string]any, 0, len(results))
	for _, r := range results {
		var upToDate, outOfDate int
		var latestVersion string

		if r.summary != nil {
			if v, ok := r.summary["upToDate"].(float64); ok {
				upToDate = int(v)
			}
			if v, ok := r.summary["outOfDate"].(float64); ok {
				outOfDate = int(v)
			}
			latestVersion, _ = r.summary["latestVersion"].(string)
		}

		total := upToDate + outOfDate
		compliancePct := "N/A"
		if total > 0 {
			pct := float64(upToDate) / float64(total) * 100
			compliancePct = fmt.Sprintf("%.0f%%", pct)
		}

		rows = append(rows, map[string]any{
			"title":          r.name,
			"id":             r.id,
			"on_latest":      upToDate,
			"on_other":       outOfDate,
			"total":          total,
			"latest":         latestVersion,
			"compliance_pct": compliancePct,
		})
	}

	return rows, nil
}
