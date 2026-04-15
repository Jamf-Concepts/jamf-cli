// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/platform"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform"
)

// ─── Portable export/import types ────────────────────────────────────────────

// benchmarkPortableGroup is a device group reference with name+type for cross-instance portability.
// The apply command resolves names to IDs before calling the API.
type benchmarkPortableGroup struct {
	Name       string `json:"name"`
	DeviceType string `json:"deviceType,omitempty"`
	GroupType  string `json:"groupType,omitempty"`
}

// benchmarkPortableTarget holds the portable target representation (group names, not IDs).
type benchmarkPortableTarget struct {
	DeviceGroups []benchmarkPortableGroup `json:"deviceGroups"`
}

// benchmarkPortableInput is the export/import format for benchmarks.
// target.deviceGroups carries group names+types for cross-instance portability.
// apply resolves names to IDs before calling the API.
type benchmarkPortableInput struct {
	Title            string                               `json:"title"`
	Description      string                               `json:"description,omitempty"`
	SourceBaselineID string                               `json:"sourceBaselineId"`
	Sources          []jamfplatform.CBEngineSourceV1      `json:"sources"`
	Rules            []jamfplatform.CBEngineRuleRequestV2 `json:"rules"`
	Target           benchmarkPortableTarget              `json:"target"`
	EnforcementMode  string                               `json:"enforcementMode"`
}

// ─── Command wiring ───────────────────────────────────────────────────────────

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
	cmd.AddCommand(newCBExportCmd(cliCtx))
	cmd.AddCommand(newCBCloneCmd(cliCtx))
	cmd.AddCommand(newCBDeleteCmd(cliCtx))
	cmd.AddCommand(newCBRulesCmd(cliCtx))
	cmd.AddCommand(newCBStatsCmd(cliCtx))
	cmd.AddCommand(newCBDeviceResultsCmd(cliCtx))
	cmd.AddCommand(newCBComplianceCmd(cliCtx))

	return cmd
}

// ─── Baselines ────────────────────────────────────────────────────────────────

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

// ─── Benchmark CRUD ───────────────────────────────────────────────────────────

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
		fromFile             string
		scaffold             bool
		scaffoldFromBaseline string
		computerGroups       []string
	)
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Create a benchmark",
		Long:  "Create a new compliance benchmark from a JSON/YAML definition. Benchmarks cannot be updated after creation.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if scaffold {
				return printScaffold(benchmarkScaffold())
			}
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			ctx := cmd.Context()
			if scaffoldFromBaseline != "" {
				return cbScaffoldFromBaseline(ctx, cliCtx, scaffoldFromBaseline)
			}
			data, err := readInput(fromFile)
			if err != nil {
				return err
			}
			var input benchmarkPortableInput
			portableErr := unmarshalInput(data, &input)

			// Detect legacy format: portable unmarshal may "succeed" via YAML leniency
			// but produce empty-name groups when the input has []string device group IDs.
			portableGroupsValid := portableErr == nil && cbPortableGroupsValid(input.Target.DeviceGroups)

			// If portable format failed or produced invalid groups, try the old SDK format.
			var legacyGroupIDs []string
			if !portableGroupsValid {
				var legacy jamfplatform.CBEngineBenchmarkRequestV2
				if err := unmarshalInput(data, &legacy); err == nil && len(legacy.Target.DeviceGroups) > 0 {
					input = benchmarkPortableInput{
						Title:            legacy.Title,
						Description:      legacy.Description,
						SourceBaselineID: legacy.SourceBaselineID,
						Sources:          legacy.Sources,
						Rules:            legacy.Rules,
						EnforcementMode:  legacy.EnforcementMode,
					}
					legacyGroupIDs = legacy.Target.DeviceGroups
				} else if portableErr != nil {
					return fmt.Errorf("parsing input: %w", portableErr)
				}
			}
			if input.Title == "" {
				return fmt.Errorf("input must include a 'title' field")
			}
			var groupIDs []string
			if len(legacyGroupIDs) > 0 && len(computerGroups) == 0 {
				// Legacy format with no override: pass IDs through directly.
				groupIDs = legacyGroupIDs
			} else {
				r := platform.NewResolver(cliCtx.PlatformClient)
				groupIDs, err = cbResolveTargetGroups(ctx, r, input.Target.DeviceGroups, computerGroups)
				if err != nil {
					return err
				}
			}
			req := &jamfplatform.CBEngineBenchmarkRequestV2{
				Title:            input.Title,
				Description:      input.Description,
				SourceBaselineID: input.SourceBaselineID,
				Sources:          input.Sources,
				Rules:            input.Rules,
				Target:           jamfplatform.CBEngineTargetV2{DeviceGroups: groupIDs},
				EnforcementMode:  input.EnforcementMode,
			}
			result, err := cliCtx.PlatformClient.CreateBenchmark(ctx, req)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Created benchmark %q (id: %s)\n", input.Title, result.BenchmarkID)
			return platform.PrintOne(cliCtx.Output, result)
		},
	}
	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON/YAML input file (or pipe to stdin)")
	cmd.Flags().BoolVar(&scaffold, "scaffold", false, "Print a JSON template for the input format")
	cmd.Flags().StringVar(&scaffoldFromBaseline, "scaffold-from-baseline", "", "Generate scaffold pre-populated with all rules from a baseline (pass baseline ID from 'baselines' list)")
	cmd.Flags().StringArrayVar(&computerGroups, "computer-group", nil, "Override target device group by name (repeatable)")
	return cmd
}

func newCBExportCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "export <title>",
		Short: "Export a benchmark as portable JSON/YAML",
		Long:  "Export a benchmark in portable format — group IDs are replaced with names for cross-instance use. Output can be piped to 'apply'.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			ctx := cmd.Context()
			bm, err := cliCtx.PlatformClient.GetBenchmarkByTitle(ctx, args[0])
			if err != nil {
				return err
			}
			groups, err := cliCtx.PlatformClient.ListDeviceGroups(ctx, nil, "")
			if err != nil {
				return fmt.Errorf("listing device groups for export: %w", err)
			}
			groupByID := make(map[string]jamfplatform.DeviceGroupListReadRepresentationV1, len(groups))
			for _, g := range groups {
				groupByID[g.ID] = g
			}
			return printExport(benchmarkToPortable(bm, groupByID))
		},
	}
}

func newCBCloneCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var computerGroups []string
	cmd := &cobra.Command{
		Use:   "clone <source-title> <new-title>",
		Short: "Clone a benchmark with a new title",
		Long:  "Clone an existing benchmark. Target device groups are copied from the source; use --computer-group to override.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			ctx := cmd.Context()
			src, err := cliCtx.PlatformClient.GetBenchmarkByTitle(ctx, args[0])
			if err != nil {
				return err
			}
			var targetGroupIDs []string
			if len(computerGroups) > 0 {
				r := platform.NewResolver(cliCtx.PlatformClient)
				targetGroupIDs, err = cbResolveNameList(ctx, r, computerGroups)
				if err != nil {
					return err
				}
			} else {
				targetGroupIDs = src.Target.DeviceGroups
			}
			req := &jamfplatform.CBEngineBenchmarkRequestV2{
				Title:            args[1],
				Description:      src.Description,
				SourceBaselineID: src.BaselineID,
				Sources:          src.Sources,
				Rules:            cbRuleInfosToRequests(src.Rules),
				Target:           jamfplatform.CBEngineTargetV2{DeviceGroups: targetGroupIDs},
				EnforcementMode:  src.EnforcementMode,
			}
			result, err := cliCtx.PlatformClient.CreateBenchmark(ctx, req)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Cloned benchmark %q → %q (id: %s)\n", args[0], args[1], result.BenchmarkID)
			return platform.PrintOne(cliCtx.Output, result)
		},
	}
	cmd.Flags().StringArrayVar(&computerGroups, "computer-group", nil, "Override target device group by name (repeatable)")
	return cmd
}

func newCBDeleteCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		yes  bool
		name string
	)
	cmd := &cobra.Command{
		Use:   "delete [<id>]",
		Short: "Delete a benchmark",
		Long:  "Delete a benchmark by ID (primary) or by title with --name.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			ctx := cmd.Context()
			var id, displayName string
			switch {
			case name != "":
				r := platform.NewResolver(cliCtx.PlatformClient)
				var err error
				id, err = r.ResolveBenchmarkID(ctx, name)
				if err != nil {
					return err
				}
				displayName = name
			case len(args) == 1:
				id = args[0]
				displayName = id
			default:
				return fmt.Errorf("provide an ID argument or --name flag")
			}
			proceed, err := confirmDelete("benchmark", displayName, yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}
			if err := cliCtx.PlatformClient.DeleteBenchmark(ctx, id); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Deleted benchmark %q\n", displayName)
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	cmd.Flags().StringVar(&name, "name", "", "Identify the benchmark by title instead of ID")
	return cmd
}

// ─── Rules ────────────────────────────────────────────────────────────────────

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

// ─── Reporting ────────────────────────────────────────────────────────────────

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

// ─── Helpers ──────────────────────────────────────────────────────────────────

// benchmarkScaffold returns a static template for apply input.
func benchmarkScaffold() *benchmarkPortableInput {
	return &benchmarkPortableInput{
		Title:            "My Benchmark",
		SourceBaselineID: "<baseline-id>",
		Sources:          []jamfplatform.CBEngineSourceV1{{Branch: "main"}},
		Rules:            []jamfplatform.CBEngineRuleRequestV2{{ID: "<rule-id>", Enabled: true}},
		Target: benchmarkPortableTarget{
			DeviceGroups: []benchmarkPortableGroup{
				{Name: "<device-group-name>", DeviceType: "COMPUTER", GroupType: "SMART"},
			},
		},
		EnforcementMode: "MONITOR",
	}
}

// benchmarkToPortable converts a benchmark API response to the portable export format.
// Device group IDs are replaced with name+type for cross-instance portability.
// Rules are converted from the detailed info format to the request format.
func benchmarkToPortable(bm *jamfplatform.CBEngineBenchmarkResponseV2, groupByID map[string]jamfplatform.DeviceGroupListReadRepresentationV1) *benchmarkPortableInput {
	portableGroups := make([]benchmarkPortableGroup, 0, len(bm.Target.DeviceGroups))
	for _, gid := range bm.Target.DeviceGroups {
		pg := benchmarkPortableGroup{Name: gid} // fallback to raw ID if group not found
		if g, ok := groupByID[gid]; ok {
			pg = benchmarkPortableGroup{
				Name:       g.Name,
				DeviceType: g.DeviceType,
				GroupType:  g.GroupType,
			}
		}
		portableGroups = append(portableGroups, pg)
	}
	return &benchmarkPortableInput{
		Title:            bm.Title,
		Description:      bm.Description,
		SourceBaselineID: bm.BaselineID,
		Sources:          bm.Sources,
		Rules:            cbRuleInfosToRequests(bm.Rules),
		Target:           benchmarkPortableTarget{DeviceGroups: portableGroups},
		EnforcementMode:  bm.EnforcementMode,
	}
}

// cbRuleInfosToRequests converts detailed rule info (from API responses) to request format.
func cbRuleInfosToRequests(rules []jamfplatform.CBEngineRuleInfoV1) []jamfplatform.CBEngineRuleRequestV2 {
	reqs := make([]jamfplatform.CBEngineRuleRequestV2, 0, len(rules))
	for _, rule := range rules {
		req := jamfplatform.CBEngineRuleRequestV2{ID: rule.ID, Enabled: rule.Enabled}
		if rule.ODV != nil {
			req.ODV = &jamfplatform.CBEngineODVRequestV2{Value: rule.ODV.Value}
		}
		reqs = append(reqs, req)
	}
	return reqs
}

// cbPortableGroupsValid returns true if every group in the slice has a non-empty name.
// An empty name indicates the input was likely legacy format ([]string IDs) that was
// leniently unmarshalled into the portable struct.
func cbPortableGroupsValid(groups []benchmarkPortableGroup) bool {
	for _, g := range groups {
		if g.Name == "" {
			return false
		}
	}
	return true
}

// cbResolveTargetGroups resolves group names to IDs.
// If overrideNames is non-empty it takes precedence over the portable groups from input.
func cbResolveTargetGroups(ctx context.Context, r *platform.Resolver, portableGroups []benchmarkPortableGroup, overrideNames []string) ([]string, error) {
	names := overrideNames
	if len(names) == 0 {
		names = make([]string, 0, len(portableGroups))
		for _, g := range portableGroups {
			names = append(names, g.Name)
		}
	}
	return cbResolveNameList(ctx, r, names)
}

// cbResolveNameList resolves a list of device group names to IDs.
func cbResolveNameList(ctx context.Context, r *platform.Resolver, names []string) ([]string, error) {
	ids := make([]string, 0, len(names))
	for _, name := range names {
		id, err := r.ResolveDeviceGroupID(ctx, name)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// cbScaffoldFromBaseline fetches baseline rules and emits a scaffold with all rules pre-populated.
// baselineID is the raw API ID (from 'pro compliance-benchmarks baselines').
// All rules are set to enabled: true as a starting point.
func cbScaffoldFromBaseline(ctx context.Context, cliCtx *registry.CLIContext, baselineID string) error {
	resp, err := cliCtx.PlatformClient.GetBaselineRules(ctx, baselineID)
	if err != nil {
		return err
	}

	// Look up title and description from the baselines list.
	var baselineTitle, baselineDescription string
	bls, blsErr := cliCtx.PlatformClient.ListBaselines(ctx)
	if blsErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not fetch baseline metadata for title/description: %v\n", blsErr)
	} else {
		for _, bl := range bls.Baselines {
			if bl.ID == baselineID {
				baselineTitle = bl.Title
				baselineDescription = bl.Description
				break
			}
		}
	}
	rules := make([]jamfplatform.CBEngineRuleRequestV2, 0, len(resp.Rules))
	for _, rule := range resp.Rules {
		req := jamfplatform.CBEngineRuleRequestV2{ID: rule.ID, Enabled: true}
		if rule.ODV != nil {
			val := rule.ODV.Placeholder
			if val == "" {
				val = rule.ODV.Value
			}
			if val == "" {
				val = "<odv-value>"
			}
			req.ODV = &jamfplatform.CBEngineODVRequestV2{Value: val}
		}
		rules = append(rules, req)
	}
	if baselineTitle == "" {
		baselineTitle = "My Benchmark"
	}
	scaffold := &benchmarkPortableInput{
		Title:            baselineTitle,
		Description:      baselineDescription,
		SourceBaselineID: baselineID,
		Sources:          resp.Sources,
		Rules:            rules,
		Target: benchmarkPortableTarget{
			DeviceGroups: []benchmarkPortableGroup{
				{Name: "<device-group-name>", DeviceType: "COMPUTER", GroupType: "SMART"},
			},
		},
		EnforcementMode: "MONITOR",
	}
	return printScaffold(scaffold)
}
