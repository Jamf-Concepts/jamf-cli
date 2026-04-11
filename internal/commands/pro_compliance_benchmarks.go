// Copyright 2026, Jamf Software LLC

package commands

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/platform"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform"
)

func newComplianceBenchmarksCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "compliance-benchmarks",
		Short: "Manage compliance benchmarks (Platform API)",
		Long:  "Create, monitor, and manage mSCP compliance benchmarks. Requires platform gateway auth.",
	}

	cmd.AddCommand(newCBBaselinesCmd(cliCtx))
	cmd.AddCommand(newCBListCmd(cliCtx))
	cmd.AddCommand(newCBGetCmd(cliCtx))
	cmd.AddCommand(newCBApplyCmd(cliCtx))
	cmd.AddCommand(newCBDeleteCmd(cliCtx))
	cmd.AddCommand(newCBRulesCmd(cliCtx))
	cmd.AddCommand(newCBStatsCmd(cliCtx))
	cmd.AddCommand(newCBDeviceResultsCmd(cliCtx))
	cmd.AddCommand(newCBComplianceCmd(cliCtx))

	return cmd
}

// ─── Baselines ─────────────────────────────────────────────────────────────────

func newCBBaselinesCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "baselines",
		Short: "List available mSCP baselines",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			resp, err := cliCtx.PlatformClient.ListBaselines(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(resp.Baselines))
			for _, bl := range resp.Baselines {
				rows = append(rows, map[string]any{
					"id":          bl.ID,
					"baselineId":  bl.BaselineID,
					"title":       bl.Title,
					"description": bl.Description,
					"ruleCount":   bl.RuleCount,
				})
			}
			data, err := json.Marshal(rows)
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}
}

// ─── Benchmark CRUD ────────────────────────────────────────────────────────────

func newCBListCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all benchmarks",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			resp, err := cliCtx.PlatformClient.ListBenchmarks(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(resp.Benchmarks))
			for _, bm := range resp.Benchmarks {
				rows = append(rows, flattenBenchmark(bm))
			}
			data, err := json.Marshal(rows)
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}
}

func flattenBenchmark(bm jamfplatform.CBEngineBenchmarkV2) map[string]any {
	return map[string]any{
		"id":              bm.ID,
		"title":           bm.Title,
		"description":     bm.Description,
		"syncState":       bm.SyncState,
		"updateAvailable": bm.UpdateAvailable,
		"modified":        bm.Modified,
		"deviceGroups":    len(bm.Target.DeviceGroups),
	}
}

func newCBGetCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get <title>",
		Short: "Get a benchmark by title",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			bm, err := cliCtx.PlatformClient.GetBenchmarkByTitle(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return platform.PrintOne(cliCtx.Output, bm)
		},
	}
}

func newCBApplyCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		fromFile string
		scaffold bool
	)
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Create a benchmark",
		Long:  "Create a new compliance benchmark from a JSON definition. Benchmarks cannot be updated after creation.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if scaffold {
				return printScaffold(benchmarkScaffold())
			}
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			data, err := readInput(fromFile)
			if err != nil {
				return err
			}
			var req jamfplatform.CBEngineBenchmarkRequestV2
			if err := unmarshalInput(data, &req); err != nil {
				return fmt.Errorf("parsing input: %w", err)
			}
			if req.Title == "" {
				return fmt.Errorf("input must include a 'title' field")
			}
			result, err := cliCtx.PlatformClient.CreateBenchmark(cmd.Context(), &req)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Created benchmark %q (id: %s)\n", req.Title, result.BenchmarkID)
			return platform.PrintOne(cliCtx.Output, result)
		},
	}
	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON input file (or pipe JSON to stdin)")
	cmd.Flags().BoolVar(&scaffold, "scaffold", false, "Print a JSON template for the input format")
	return cmd
}

func benchmarkScaffold() *jamfplatform.CBEngineBenchmarkRequestV2 {
	return &jamfplatform.CBEngineBenchmarkRequestV2{
		Title:            "My Benchmark",
		Description:      "",
		SourceBaselineID: "<baseline-id>",
		Sources:          []jamfplatform.CBEngineSourceV1{{Branch: "main", Revision: ""}},
		Rules: []jamfplatform.CBEngineRuleRequestV2{
			{ID: "<rule-id>", Enabled: true},
		},
		Target:          jamfplatform.CBEngineTargetV2{DeviceGroups: []string{"<device-group-id>"}},
		EnforcementMode: "AUDIT",
	}
}

func newCBDeleteCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <title>",
		Short: "Delete a benchmark",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			ctx := cmd.Context()
			r := platform.NewResolver(cliCtx.PlatformClient)
			id, err := r.ResolveBenchmarkID(ctx, args[0])
			if err != nil {
				return err
			}
			proceed, err := confirmDelete("benchmark", args[0], yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}
			if err := cliCtx.PlatformClient.DeleteBenchmark(ctx, id); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Deleted benchmark %q\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	return cmd
}

// ─── Rules ─────────────────────────────────────────────────────────────────────

func newCBRulesCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "rules <baseline-title>",
		Short: "List rules for a baseline",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			ctx := cmd.Context()
			r := platform.NewResolver(cliCtx.PlatformClient)
			id, err := r.ResolveBaselineID(ctx, args[0])
			if err != nil {
				return err
			}
			resp, err := cliCtx.PlatformClient.GetBaselineRules(ctx, id)
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(resp.Rules))
			for _, rule := range resp.Rules {
				rows = append(rows, map[string]any{
					"id":          rule.ID,
					"title":       rule.Title,
					"sectionName": rule.SectionName,
					"enabled":     rule.Enabled,
				})
			}
			data, err := json.Marshal(rows)
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}
}

// ─── Reporting ─────────────────────────────────────────────────────────────────

func newCBStatsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		sort       string
		ruleSearch string
	)
	cmd := &cobra.Command{
		Use:   "stats <title>",
		Short: "Show per-rule compliance stats for a benchmark",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			ctx := cmd.Context()
			r := platform.NewResolver(cliCtx.PlatformClient)
			id, err := r.ResolveBenchmarkID(ctx, args[0])
			if err != nil {
				return err
			}
			stats, err := cliCtx.PlatformClient.ListBenchmarkRulesStats(ctx, id, sort, ruleSearch)
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(stats))
			for _, s := range stats {
				rows = append(rows, map[string]any{
					"ruleId":         s.RuleID,
					"ruleTitle":      s.RuleTitle,
					"passed":         s.Passed,
					"failed":         s.Failed,
					"unknown":        s.Unknown,
					"passPercentage": s.PassPercentage,
					"devices":        s.NumberOfDevices,
				})
			}
			data, err := json.Marshal(rows)
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}
	cmd.Flags().StringVar(&sort, "sort", "", "Sort fields (e.g. ruleTitle:asc)")
	cmd.Flags().StringVar(&ruleSearch, "search", "", "Filter rules by keyword")
	return cmd
}

func newCBDeviceResultsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		sort         string
		deviceSearch string
		state        string
	)
	cmd := &cobra.Command{
		Use:   "device-results <title> <rule-id>",
		Short: "Show device compliance for a specific rule",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			ctx := cmd.Context()
			r := platform.NewResolver(cliCtx.PlatformClient)
			bmID, err := r.ResolveBenchmarkID(ctx, args[0])
			if err != nil {
				return err
			}
			results, err := cliCtx.PlatformClient.ListBenchmarkRuleDevices(ctx, bmID, args[1], sort, deviceSearch, state)
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(results))
			for _, d := range results {
				row := map[string]any{
					"deviceId": d.DeviceID,
					"state":    d.State,
				}
				if d.DeviceName != nil {
					row["deviceName"] = *d.DeviceName
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
	cmd.Flags().StringVar(&sort, "sort", "", "Sort fields")
	cmd.Flags().StringVar(&deviceSearch, "search", "", "Filter devices by keyword")
	cmd.Flags().StringVar(&state, "state", "", "Filter by rule result (PASSED, FAILED, UNKNOWN)")
	return cmd
}

func newCBComplianceCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "compliance <title>",
		Short: "Show overall compliance percentage for a benchmark",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			ctx := cmd.Context()
			r := platform.NewResolver(cliCtx.PlatformClient)
			id, err := r.ResolveBenchmarkID(ctx, args[0])
			if err != nil {
				return err
			}
			pct, err := cliCtx.PlatformClient.GetBenchmarkCompliancePercentage(ctx, id)
			if err != nil {
				return err
			}
			m := map[string]any{
				"benchmark":            args[0],
				"compliancePercentage": pct.CompliancePercentage,
			}
			data, err := json.Marshal(m)
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}
}
