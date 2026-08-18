// Copyright 2026, Jamf Software LLC

package commands

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/output"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/blueprints"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/compliancebenchmarks"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/ddmreport"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/devices"
)

// ── Blueprint Status Report ────────────────────────────────────────────────

func newReportBlueprintStatusCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "blueprint-status",
		Short: "Blueprint deployment status across all blueprints (Platform API)",
		Long: `Shows deployment state and device counts for every blueprint.

For deployed blueprints, fetches the deployment report to show succeeded,
failed, and pending device counts. Requires platform gateway auth.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			ctx := cmd.Context()
			c := cliCtx.PlatformSDKClient

			bps, err := blueprints.New(c).ListBlueprints(ctx, nil, "")
			if err != nil {
				return err
			}

			rows := make([]map[string]any, 0, len(bps))
			for _, bp := range bps {
				state := ""
				if bp.DeploymentState != nil {
					state = bp.DeploymentState.State
				}
				row := map[string]any{
					"name":  bp.Name,
					"state": state,
				}

				detail, err := blueprints.New(c).GetBlueprint(ctx, bp.ID)
				if err == nil {
					if detail.Scope != nil {
						row["scope"] = len(detail.Scope.DeviceGroups)
					} else {
						row["scope"] = 0
					}
					row["steps"] = len(detail.Steps)
				}

				if state == "DEPLOYED" {
					report, err := blueprints.New(c).GetBlueprintReport(ctx, bp.ID)
					if err == nil {
						row["succeeded"] = report.Succeeded
						row["failed"] = report.Failed
						row["pending"] = report.Pending
					}
				}

				rows = append(rows, row)
			}

			if len(rows) == 0 {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "No blueprints found.")
				return nil
			}

			if outputFmt == "json" || outputFmt == "yaml" {
				data, err := json.Marshal(rows)
				if err != nil {
					return fmt.Errorf("marshalling output: %w", err)
				}
				return cliCtx.Output.PrintRaw(data)
			}

			formatter := output.New(outputFmt, noColor, wide)
			return formatter.Print(rows)
		},
	}
}

// ── Compliance Rules Report ────────────────────────────────────────────────

func newReportComplianceRulesCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		sortField  string
		search     string
		stateParam string
	)
	cmd := &cobra.Command{
		Use:   "compliance-rules <benchmark-title>",
		Short: "Per-rule compliance stats for a benchmark (Platform API)",
		Long: `Shows pass/fail/unknown counts for each rule in a compliance benchmark,
sorted by failure rate by default. Requires platform gateway auth.

Use --state to filter to rules with a specific result (e.g., --state failed
shows only rules that have failing devices).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			ctx := cmd.Context()
			c := cliCtx.PlatformSDKClient

			cb := compliancebenchmarks.New(cliCtx.PlatformSDKClient)
			benchmarkID, err := cb.ResolveBenchmarkIDByName(ctx, args[0])
			if err != nil {
				return err
			}

			sort := sortField
			if sort == "" {
				sort = "failed:desc"
			}

			stats, err := compliancebenchmarks.New(c).ListBenchmarkRulesStats(ctx, benchmarkID, sort, search)
			if err != nil {
				return err
			}

			rows := make([]map[string]any, 0, len(stats))
			for _, s := range stats {
				if stateParam != "" {
					switch stateParam {
					case "failed":
						if s.Failed == 0 {
							continue
						}
					case "passed":
						if s.Passed == 0 {
							continue
						}
					case "unknown":
						if s.Unknown == 0 {
							continue
						}
					}
				}
				rows = append(rows, map[string]any{
					"ruleId":   s.RuleID,
					"rule":     s.RuleTitle,
					"passed":   s.Passed,
					"failed":   s.Failed,
					"unknown":  s.Unknown,
					"passRate": fmt.Sprintf("%.1f%%", s.PassPercentage),
					"devices":  s.NumberOfDevices,
				})
			}

			if len(rows) == 0 {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "No rule stats found.")
				return nil
			}

			if outputFmt == "json" || outputFmt == "yaml" {
				data, err := json.Marshal(rows)
				if err != nil {
					return fmt.Errorf("marshalling output: %w", err)
				}
				return cliCtx.Output.PrintRaw(data)
			}

			formatter := output.New(outputFmt, noColor, wide)
			return formatter.Print(rows)
		},
	}
	cmd.Flags().StringVar(&sortField, "sort", "", "Sort field: failed, passed, unknown, ruleTitle, ruleNumber (e.g. failed:desc)")
	cmd.Flags().StringVar(&search, "search", "", "Filter rules by title")
	cmd.Flags().StringVar(&stateParam, "state", "", "Filter by result state: passed, failed, unknown")
	return cmd
}

// ── Compliance Devices Report ──────────────────────────────────────────────

func newReportComplianceDevicesCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var stateParam string
	cmd := &cobra.Command{
		Use:   "compliance-devices <benchmark-title>",
		Short: "Non-compliant devices for a benchmark (Platform API)",
		Long: `Aggregates per-device compliance across all rules in a benchmark.
For each rule with failing devices, fetches the device list and counts
how many rules each device fails. Requires platform gateway auth.

Use --state to filter (default: failed). Shows devices sorted by number
of failing rules.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			ctx := cmd.Context()
			c := cliCtx.PlatformSDKClient

			cb := compliancebenchmarks.New(cliCtx.PlatformSDKClient)
			benchmarkID, err := cb.ResolveBenchmarkIDByName(ctx, args[0])
			if err != nil {
				return err
			}

			// Get all rules stats
			stats, err := compliancebenchmarks.New(c).ListBenchmarkRulesStats(ctx, benchmarkID, "failed:desc", "")
			if err != nil {
				return err
			}

			// Aggregate per-device failures
			state := stateParam
			if state == "" {
				state = "FAILED"
			}

			type deviceAgg struct {
				Name        string
				RulesFailed int
				RulesPassed int
			}
			devices := make(map[string]*deviceAgg)

			for _, s := range stats {
				if s.Failed == 0 && state == "FAILED" {
					continue
				}
				results, err := compliancebenchmarks.New(c).ListBenchmarkRuleDevices(ctx, benchmarkID, s.RuleID, "", "", state)
				if err != nil {
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Warning: failed to fetch devices for rule %q: %v\n", s.RuleTitle, err)
					continue
				}
				for _, d := range results {
					agg, ok := devices[d.DeviceID]
					if !ok {
						name := d.DeviceID
						if d.DeviceName != nil && *d.DeviceName != "" {
							name = *d.DeviceName
						}
						agg = &deviceAgg{Name: name}
						devices[d.DeviceID] = agg
					}
					switch d.State {
					case "FAILED":
						agg.RulesFailed++
					case "PASSED":
						agg.RulesPassed++
					}
				}
			}

			if len(devices) == 0 {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "No matching devices found.")
				return nil
			}

			// Convert to sorted rows
			rows := make([]map[string]any, 0, len(devices))
			for id, agg := range devices {
				total := agg.RulesFailed + agg.RulesPassed
				pct := float64(0)
				if total > 0 {
					pct = float64(agg.RulesPassed) / float64(total) * 100
				}
				rows = append(rows, map[string]any{
					"deviceId":    id,
					"device":      agg.Name,
					"rulesFailed": agg.RulesFailed,
					"rulesPassed": agg.RulesPassed,
					"compliance":  fmt.Sprintf("%.1f%%", pct),
				})
			}

			if outputFmt == "json" || outputFmt == "yaml" {
				data, err := json.Marshal(rows)
				if err != nil {
					return fmt.Errorf("marshalling output: %w", err)
				}
				return cliCtx.Output.PrintRaw(data)
			}

			formatter := output.New(outputFmt, noColor, wide)
			return formatter.Print(rows)
		},
	}
	cmd.Flags().StringVar(&stateParam, "state", "", "Filter by result state: PASSED, FAILED, UNKNOWN (default: FAILED)")
	return cmd
}

// ── DDM Status Report ──────────────────────────────────────────────────────

// classifyDeclaration resolves a declaration identifier to a human-readable source
// name and a kind label. Blueprint declarations resolve to the blueprint name,
// benchmark declarations to the benchmark title, system declarations get a
// descriptive label, and unknowns keep their raw identifier.
func classifyDeclaration(declID string, bpNames, bmNames map[string]string) (source, kind string) {
	// Blueprint declaration: Blueprint_{uuid}_*
	if bpID := extractBlueprintIDFromDecl(declID); bpID != "" {
		if name, ok := bpNames[bpID]; ok {
			return name, "blueprint"
		}
		return bpID[:8] + "...", "blueprint"
	}
	// System declarations
	switch {
	case declID == "blueprint-device-groups":
		return "Device Group Membership", "system"
	case strings.HasPrefix(declID, "blueprint-"):
		return declID, "system"
	}
	// Try compliance benchmark match
	if name, ok := bmNames[declID]; ok {
		return name, "benchmark"
	}
	// Unknown — standalone declaration
	return declID, "standalone"
}

// ignorableDDMReasonCodes lists DDM reason codes that are informational
// and not actionable — these are filtered from error displays.
var ignorableDDMReasonCodes = map[string]bool{
	"Info.DeclarationNotInstalled": true, // declaration not applicable to device/user context
}

// onlyHasIgnorableReasons returns true if all reasons (or no reasons) are ignorable.
func onlyHasIgnorableReasons(reasons []ddmreport.StatusReportDeclarationReasonDto) bool {
	for _, r := range reasons {
		if !ignorableDDMReasonCodes[r.Code] {
			return false
		}
	}
	return true
}

// extractBlueprintIDFromDecl extracts a blueprint UUID from a declaration identifier
// like "Blueprint_086bb302-ba21-4190-9740-0d71916616b6_s1_c1_sys_cfg1".
// Returns the UUID or empty string if the identifier doesn't match the pattern.
func extractBlueprintIDFromDecl(declID string) string {
	if !strings.HasPrefix(declID, "Blueprint_") {
		return ""
	}
	// Blueprint_{uuid}_{rest} — UUID is 36 chars (8-4-4-4-12)
	rest := declID[len("Blueprint_"):]
	if len(rest) < 36 {
		return ""
	}
	candidate := rest[:36]
	// Quick validation: check hyphens at expected positions
	if candidate[8] != '-' || candidate[13] != '-' || candidate[18] != '-' || candidate[23] != '-' {
		return ""
	}
	return candidate
}

func newReportDDMStatusCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "ddm-status",
		Short: "DDM declaration health across all devices (Platform API)",
		Long: `Fetches declaration reports for all platform devices and aggregates
per-declaration status counts. Each declaration is resolved to its source
blueprint (name and ID) where possible.

Requires platform gateway auth.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			ctx := cmd.Context()
			c := cliCtx.PlatformSDKClient

			// Build lookup maps for resolving declaration identifiers
			bpNames := make(map[string]string)
			if bps, err := blueprints.New(c).ListBlueprints(ctx, nil, ""); err == nil {
				for _, bp := range bps {
					bpNames[bp.ID] = bp.Name
				}
			}
			devices, err := devices.New(c).ListDevices(ctx, nil, "")
			if err != nil {
				return err
			}

			type sourceStats struct {
				Successful   int
				Unsuccessful int
				Declarations int
				Devices      map[string]bool
				Kind         string
			}
			agg := make(map[string]*sourceStats)

			// Track per-device actionable errors
			type deviceError struct {
				DeviceID string
				Source   string
				Reason   string
			}
			var deviceErrors []deviceError

			for _, dev := range devices {
				declarations, err := ddmreport.New(c).GetDeviceDeclarationReportFiltered(ctx, dev.ID, ddmAllDeclarationsFilter, nil)
				if err != nil {
					continue
				}
				for _, d := range declarations {
					source, kind := classifyDeclaration(d.DeclarationIdentifier, bpNames, nil)
					s, ok := agg[source]
					if !ok {
						s = &sourceStats{Devices: make(map[string]bool), Kind: kind}
						agg[source] = s
					}
					s.Declarations++
					s.Devices[dev.ID] = true

					switch {
					case d.Status == "SUCCESSFUL":
						s.Successful++
					case onlyHasIgnorableReasons(d.Reasons):
						// Don't count as unsuccessful — info-only
					default:
						s.Unsuccessful++
					}

					for _, r := range d.Reasons {
						if ignorableDDMReasonCodes[r.Code] {
							continue
						}
						deviceErrors = append(deviceErrors, deviceError{
							DeviceID: dev.ID,
							Source:   source,
							Reason:   r.Code + ": " + r.Description,
						})
					}
				}
			}

			if len(agg) == 0 {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "No DDM declaration data found.")
				return nil
			}

			// JSON/YAML: full detail including standalone
			if outputFmt == "json" || outputFmt == "yaml" {
				rows := make([]map[string]any, 0, len(agg))
				for source, s := range agg {
					rows = append(rows, map[string]any{
						"type":         s.Kind,
						"source":       source,
						"devices":      len(s.Devices),
						"declarations": s.Declarations,
						"successful":   s.Successful,
						"unsuccessful": s.Unsuccessful,
					})
				}
				data, err := json.Marshal(rows)
				if err != nil {
					return fmt.Errorf("marshalling output: %w", err)
				}
				return cliCtx.Output.PrintRaw(data)
			}

			// Table: summary, then devices with errors
			formatter := output.New(outputFmt, noColor, wide)

			summaryRows := make([]map[string]any, 0, len(agg))
			for source, s := range agg {
				if s.Kind == "standalone" {
					continue
				}
				summaryRows = append(summaryRows, map[string]any{
					"type":         s.Kind,
					"source":       source,
					"devices":      len(s.Devices),
					"declarations": s.Declarations,
					"successful":   s.Successful,
					"unsuccessful": s.Unsuccessful,
				})
			}
			if err := formatter.Print(summaryRows); err != nil {
				return err
			}

			if len(deviceErrors) > 0 {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "\n── Errors (%d) ──\n", len(deviceErrors))
				errRows := make([]map[string]any, 0, len(deviceErrors))
				for _, e := range deviceErrors {
					errRows = append(errRows, map[string]any{
						"deviceId": e.DeviceID,
						"source":   e.Source,
						"reason":   e.Reason,
					})
				}
				if err := formatter.Print(errRows); err != nil {
					return err
				}
			}

			return nil
		},
	}
}
