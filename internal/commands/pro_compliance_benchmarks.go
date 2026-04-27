// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	platformgen "github.com/Jamf-Concepts/jamf-cli/internal/commands/platform/generated"
	"github.com/Jamf-Concepts/jamf-cli/internal/platform"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/compliancebenchmarks"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/devicegroups"
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
	Title            string                             `json:"title"`
	Description      string                             `json:"description,omitempty"`
	SourceBaselineID string                             `json:"sourceBaselineId"`
	Sources          []compliancebenchmarks.Source      `json:"sources"`
	Rules            []compliancebenchmarks.RuleRequest `json:"rules"`
	Target           benchmarkPortableTarget            `json:"target"`
	EnforcementMode  string                             `json:"enforcementMode"`
}

// ─── Command wiring ───────────────────────────────────────────────────────────

func newComplianceBenchmarksCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "compliance-benchmarks",
		Short: "Manage compliance benchmarks (Platform API)",
		Long:  "Create, monitor, and manage mSCP compliance benchmarks. Requires platform gateway auth.",
	}

	// Generated CRUD: list, get, delete (supports --name); skip create — apply covers it
	for _, sub := range platformgen.NewBenchmarksCmd(cliCtx).Commands() {
		if sub.Name() == "create" {
			continue
		}
		cmd.AddCommand(sub)
	}

	// Business logic: portable upsert, export, clone
	cmd.AddCommand(newCBApplyCmd(cliCtx))
	cmd.AddCommand(newCBExportCmd(cliCtx))
	cmd.AddCommand(newCBCloneCmd(cliCtx))

	return cmd
}

// ─── Apply ────────────────────────────────────────────────────────────────────

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
				var legacy compliancebenchmarks.BenchmarkRequestV2
				if err := unmarshalInput(data, &legacy); err == nil && len(legacy.Target.DeviceGroups) > 0 {
					desc := ""
					if legacy.Description != nil {
						desc = *legacy.Description
					}
					input = benchmarkPortableInput{
						Title:            legacy.Title,
						Description:      desc,
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
				groupIDs, err = cbResolveTargetGroups(ctx, cliCtx.PlatformSDKClient, input.Target.DeviceGroups, computerGroups)
				if err != nil {
					return err
				}
			}
			req := &compliancebenchmarks.BenchmarkRequestV2{
				Title:            input.Title,
				Description:      &input.Description,
				SourceBaselineID: input.SourceBaselineID,
				Sources:          input.Sources,
				Rules:            input.Rules,
				Target:           compliancebenchmarks.TargetV2{DeviceGroups: groupIDs},
				EnforcementMode:  input.EnforcementMode,
			}
			result, err := compliancebenchmarks.New(cliCtx.PlatformSDKClient).CreateBenchmark(ctx, req)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Created benchmark %q (id: %s)\n", input.Title, result.BenchmarkID)
			return platform.PrintOne(cliCtx.Output, result)
		},
	}
	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON/YAML input file (or pipe to stdin)")
	cmd.Flags().BoolVar(&scaffold, "scaffold", false, "Print a JSON template for the input format")
	cmd.Flags().StringVar(&scaffoldFromBaseline, "scaffold-from-baseline", "", "Generate scaffold pre-populated with all rules from a baseline (pass baseline ID from 'baselines list')")
	cmd.Flags().StringArrayVar(&computerGroups, "computer-group", nil, "Override target device group by name (repeatable)")
	return cmd
}

// ─── Export ───────────────────────────────────────────────────────────────────

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
			cb := compliancebenchmarks.New(cliCtx.PlatformSDKClient)
			id, err := cb.ResolveBenchmarkIDByName(ctx, args[0])
			if err != nil {
				return err
			}
			bm, err := compliancebenchmarks.New(cliCtx.PlatformSDKClient).GetBenchmark(ctx, id)
			if err != nil {
				return err
			}
			groups, err := devicegroups.New(cliCtx.PlatformSDKClient).ListDeviceGroups(ctx, nil, "")
			if err != nil {
				return fmt.Errorf("listing device groups for export: %w", err)
			}
			groupByID := make(map[string]devicegroups.DeviceGroupListReadRepresentationV1, len(groups))
			for _, g := range groups {
				groupByID[g.ID] = g
			}
			return printExport(benchmarkToPortable(bm, groupByID))
		},
	}
}

// ─── Clone ────────────────────────────────────────────────────────────────────

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
			cb := compliancebenchmarks.New(cliCtx.PlatformSDKClient)
			srcID, err := cb.ResolveBenchmarkIDByName(ctx, args[0])
			if err != nil {
				return err
			}
			src, err := compliancebenchmarks.New(cliCtx.PlatformSDKClient).GetBenchmark(ctx, srcID)
			if err != nil {
				return err
			}
			var targetGroupIDs []string
			if len(computerGroups) > 0 {
				targetGroupIDs, err = cbResolveNameList(ctx, cliCtx.PlatformSDKClient, computerGroups)
				if err != nil {
					return err
				}
			} else if src.Target != nil {
				targetGroupIDs = src.Target.DeviceGroups
			}
			req := &compliancebenchmarks.BenchmarkRequestV2{
				Title:            args[1],
				Description:      &src.Description,
				SourceBaselineID: src.BaselineID,
				Sources:          src.Sources,
				Rules:            cbRuleInfosToRequests(src.Rules),
				Target:           compliancebenchmarks.TargetV2{DeviceGroups: targetGroupIDs},
				EnforcementMode:  src.EnforcementMode,
			}
			result, err := compliancebenchmarks.New(cliCtx.PlatformSDKClient).CreateBenchmark(ctx, req)
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

// ─── Helpers ──────────────────────────────────────────────────────────────────

// benchmarkScaffold returns a static template for apply input.
func benchmarkScaffold() *benchmarkPortableInput {
	return &benchmarkPortableInput{
		Title:            "My Benchmark",
		SourceBaselineID: "<baseline-id>",
		Sources:          []compliancebenchmarks.Source{{Branch: "main"}},
		Rules:            []compliancebenchmarks.RuleRequest{{ID: "<rule-id>", Enabled: true}},
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
func benchmarkToPortable(bm *compliancebenchmarks.BenchmarkResponseV2, groupByID map[string]devicegroups.DeviceGroupListReadRepresentationV1) *benchmarkPortableInput {
	var deviceGroups []string
	if bm.Target != nil {
		deviceGroups = bm.Target.DeviceGroups
	}
	portableGroups := make([]benchmarkPortableGroup, 0, len(deviceGroups))
	for _, gid := range deviceGroups {
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
func cbRuleInfosToRequests(rules []compliancebenchmarks.RuleInfo) []compliancebenchmarks.RuleRequest {
	reqs := make([]compliancebenchmarks.RuleRequest, 0, len(rules))
	for _, rule := range rules {
		req := compliancebenchmarks.RuleRequest{ID: rule.ID, Enabled: rule.Enabled}
		if rule.ODV != nil {
			req.ODV = &compliancebenchmarks.ODVRequest{Value: rule.ODV.Value}
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

// cbResolveTargetGroups resolves group names to IDs via the SDK's
// devicegroups subpackage. If overrideNames is non-empty it takes precedence
// over the portable groups from input.
func cbResolveTargetGroups(ctx context.Context, c *jamfplatform.Client, portableGroups []benchmarkPortableGroup, overrideNames []string) ([]string, error) {
	names := overrideNames
	if len(names) == 0 {
		names = make([]string, 0, len(portableGroups))
		for _, g := range portableGroups {
			names = append(names, g.Name)
		}
	}
	return cbResolveNameList(ctx, c, names)
}

// cbResolveNameList resolves a list of device group names to IDs.
func cbResolveNameList(ctx context.Context, c *jamfplatform.Client, names []string) ([]string, error) {
	dg := devicegroups.New(c)
	ids := make([]string, 0, len(names))
	for _, name := range names {
		id, err := dg.ResolveDeviceGroupIDByName(ctx, name)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// cbScaffoldFromBaseline fetches baseline rules and emits a scaffold with all rules pre-populated.
// baselineID is the raw API ID (from 'pro compliance-benchmarks baselines list').
// All rules are set to enabled: true as a starting point.
func cbScaffoldFromBaseline(ctx context.Context, cliCtx *registry.CLIContext, baselineID string) error {
	resp, err := compliancebenchmarks.New(cliCtx.PlatformSDKClient).GetBaselineRules(ctx, baselineID)
	if err != nil {
		return err
	}

	// Look up title and description from the baselines list.
	var baselineTitle, baselineDescription string
	bls, blsErr := compliancebenchmarks.New(cliCtx.PlatformSDKClient).ListBaselines(ctx)
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
	rules := make([]compliancebenchmarks.RuleRequest, 0, len(resp.Rules))
	for _, rule := range resp.Rules {
		req := compliancebenchmarks.RuleRequest{ID: rule.ID, Enabled: true}
		if rule.ODV != nil {
			val := rule.ODV.Placeholder
			if val == "" {
				val = rule.ODV.Value
			}
			if val == "" {
				val = "<odv-value>"
			}
			req.ODV = &compliancebenchmarks.ODVRequest{Value: val}
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
