package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/Jamf-Concepts/jamfpro-cli/internal/commands/generated"
	"github.com/Jamf-Concepts/jamfpro-cli/internal/output"
)

// newGroupToolsCmd builds the "group-tools" parent command with subcommands.
func newGroupToolsCmd(cliCtx *generated.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "group-tools",
		Short: "Analyze and manage computer groups",
		Long: `Tools for inspecting, analyzing, and exporting computer groups.

Subcommands provide filtering, membership inspection, unused-group detection,
and full export of group definitions.`,
	}

	cmd.AddCommand(newGroupToolsListCmd(cliCtx))
	cmd.AddCommand(newGroupToolsMembersCmd(cliCtx))
	cmd.AddCommand(newGroupToolsAnalyzeCmd(cliCtx))
	cmd.AddCommand(newGroupToolsExportCmd(cliCtx))

	return cmd
}

// ─────────────────────────────────────────────────────────────────
// list
// ─────────────────────────────────────────────────────────────────

func newGroupToolsListCmd(cliCtx *generated.CLIContext) *cobra.Command {
	var (
		groupType   string
		emptyOnly   bool
		namePattern string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List computer groups with optional filters",
		Long: `List all computer groups. Optionally filter by type (smart or static),
membership count, or name pattern (case-insensitive substring match).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGroupToolsList(cmd.Context(), cliCtx, groupType, emptyOnly, namePattern)
		},
	}

	cmd.Flags().StringVar(&groupType, "type", "", "filter by type: smart or static")
	cmd.Flags().BoolVar(&emptyOnly, "empty", false, "only show groups with zero members")
	cmd.Flags().StringVar(&namePattern, "name-pattern", "", "filter by name (case-insensitive substring match)")

	return cmd
}

func runGroupToolsList(ctx context.Context, cliCtx *generated.CLIContext, groupType string, emptyOnly bool, namePattern string) error {
	groups, err := FetchAllPaginated(ctx, cliCtx.Client, "/v1/computer-groups", 100)
	if err != nil {
		return fmt.Errorf("fetching computer groups: %w", err)
	}

	var rows []map[string]interface{}
	for _, g := range groups {
		// Type filter
		if groupType != "" {
			smart, _ := g["smartGroup"].(bool)
			wantSmart := strings.EqualFold(groupType, "smart")
			wantStatic := strings.EqualFold(groupType, "static")
			if wantSmart && !smart {
				continue
			}
			if wantStatic && smart {
				continue
			}
		}

		// Empty filter
		if emptyOnly {
			count := groupMemberCount(g)
			if count != 0 {
				continue
			}
		}

		// Name pattern filter (case-insensitive substring)
		if namePattern != "" {
			name, _ := g["name"].(string)
			if !strings.Contains(strings.ToLower(name), strings.ToLower(namePattern)) {
				continue
			}
		}

		rows = append(rows, groupSummaryRow(g))
	}

	if len(rows) == 0 {
		rows = []map[string]interface{}{}
	}

	formatter := output.New(outputFmt, noColor, wide)
	return formatter.Print(rows)
}

// ─────────────────────────────────────────────────────────────────
// members
// ─────────────────────────────────────────────────────────────────

func newGroupToolsMembersCmd(cliCtx *generated.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "members <name>",
		Short: "Show members of a computer group by name",
		Long:  `Fetch and display all member computers for the named computer group.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGroupToolsMembers(cmd.Context(), cliCtx, args[0])
		},
	}
	return cmd
}

func runGroupToolsMembers(ctx context.Context, cliCtx *generated.CLIContext, name string) error {
	groups, err := FetchAllPaginated(ctx, cliCtx.Client, "/v1/computer-groups", 100)
	if err != nil {
		return fmt.Errorf("fetching computer groups: %w", err)
	}

	// Find the group by exact name (case-insensitive)
	var groupID string
	for _, g := range groups {
		n, _ := g["name"].(string)
		if strings.EqualFold(n, name) {
			groupID = extractID(g)
			break
		}
	}
	if groupID == "" {
		return fmt.Errorf("group %q not found", name)
	}

	// Fetch group detail to get members
	detail, err := FetchJSON(ctx, cliCtx.Client, fmt.Sprintf("/v1/computer-groups/%s", groupID))
	if err != nil {
		return fmt.Errorf("fetching group detail: %w", err)
	}

	members, _ := detail["members"].([]interface{})
	var rows []map[string]interface{}
	for _, m := range members {
		member, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		row := map[string]interface{}{
			"id":   extractID(member),
			"name": extractName(member),
		}
		// Include managementId if present
		if mid, ok := member["managementId"].(string); ok && mid != "" {
			row["managementId"] = mid
		}
		rows = append(rows, row)
	}

	if len(rows) == 0 {
		rows = []map[string]interface{}{}
	}

	formatter := output.New(outputFmt, noColor, wide)
	return formatter.Print(rows)
}

// ─────────────────────────────────────────────────────────────────
// analyze
// ─────────────────────────────────────────────────────────────────

func newGroupToolsAnalyzeCmd(cliCtx *generated.CLIContext) *cobra.Command {
	var unused bool

	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "Analyze computer groups for hygiene issues",
		Long: `Run hygiene analysis on computer groups.

--unused detects groups not referenced by any policy scope.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGroupToolsAnalyze(cmd.Context(), cliCtx, unused)
		},
	}

	cmd.Flags().BoolVar(&unused, "unused", false, "find groups not referenced by any policy")

	return cmd
}

func runGroupToolsAnalyze(ctx context.Context, cliCtx *generated.CLIContext, unused bool) error {
	if !unused {
		return fmt.Errorf("specify an analysis mode: --unused")
	}
	return runGroupToolsAnalyzeUnused(ctx, cliCtx)
}

func runGroupToolsAnalyzeUnused(ctx context.Context, cliCtx *generated.CLIContext) error {
	// Fetch all computer groups
	groups, err := FetchAllPaginated(ctx, cliCtx.Client, "/v1/computer-groups", 100)
	if err != nil {
		return fmt.Errorf("fetching computer groups: %w", err)
	}

	// Fetch policy list from Classic API
	policyItems, err := FetchClassicList(ctx, cliCtx.Client, "/JSSResource/policies", "policies")
	if err != nil {
		return fmt.Errorf("fetching policy list: %w", err)
	}

	// Collect policy IDs for parallel detail fetch
	type policyStub struct {
		id   string
		name string
	}
	var stubs []policyStub
	for _, item := range policyItems {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		stubs = append(stubs, policyStub{id: extractID(m), name: extractName(m)})
	}

	// Fetch policy details in parallel (bounded concurrency)
	details, fetchErrs := BoundedParallelFetch(ctx, stubs, 10, func(ctx context.Context, stub policyStub) (map[string]interface{}, error) {
		path := fmt.Sprintf("/JSSResource/policies/id/%s", stub.id)
		data, err := FetchJSON(ctx, cliCtx.Client, path)
		if err != nil {
			return nil, err
		}
		return unwrapClassicDetail(data), nil
	})
	if len(fetchErrs) > 0 {
		fmt.Fprintf(os.Stderr, "WARNING: %d of %d policy detail fetches failed\n", len(fetchErrs), len(stubs))
		if len(fetchErrs) == len(stubs) {
			return fmt.Errorf("all policy detail fetches failed; cannot determine group usage")
		}
	}

	// Build set of group names referenced by any policy scope
	referenced := make(map[string]bool)
	for _, detail := range details {
		if detail == nil {
			continue
		}
		scope, _ := detail["scope"].(map[string]interface{})
		if scope == nil {
			continue
		}
		addGroupNamesFromScope(scope, referenced)
	}

	// Find groups not referenced
	var rows []map[string]interface{}
	for _, g := range groups {
		name, _ := g["name"].(string)
		if referenced[name] {
			continue
		}
		row := groupSummaryRow(g)
		row["referenced_by_policies"] = 0
		rows = append(rows, row)
	}

	if len(rows) == 0 {
		rows = []map[string]interface{}{}
	}

	formatter := output.New(outputFmt, noColor, wide)
	return formatter.Print(rows)
}

// addGroupNamesFromScope extracts computer group names from a Classic API scope object.
func addGroupNamesFromScope(scope map[string]interface{}, out map[string]bool) {
	for _, key := range []string{"computerGroups", "computer_groups"} {
		arr, _ := scope[key].([]interface{})
		for _, item := range arr {
			m, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			if n, ok := m["name"].(string); ok && n != "" {
				out[n] = true
			}
		}
	}
}

// ─────────────────────────────────────────────────────────────────
// export
// ─────────────────────────────────────────────────────────────────

func newGroupToolsExportCmd(cliCtx *generated.CLIContext) *cobra.Command {
	var exportFormat string

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export all computer group definitions",
		Long:  `Fetch all computer groups and print their definitions in YAML or JSON format.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGroupToolsExport(cmd.Context(), cliCtx, exportFormat)
		},
	}

	cmd.Flags().StringVar(&exportFormat, "format", "json", "export format: yaml or json")

	return cmd
}

func runGroupToolsExport(ctx context.Context, cliCtx *generated.CLIContext, format string) error {
	switch format {
	case "yaml", "json":
	default:
		return fmt.Errorf("unsupported format %q: must be yaml or json", format)
	}

	groups, err := FetchAllPaginated(ctx, cliCtx.Client, "/v1/computer-groups", 100)
	if err != nil {
		return fmt.Errorf("fetching computer groups: %w", err)
	}

	rows := append([]map[string]interface{}{}, groups...)

	if len(rows) == 0 {
		rows = []map[string]interface{}{}
	}

	formatter := output.New(format, noColor, wide)
	return formatter.Print(rows)
}

// ─────────────────────────────────────────────────────────────────
// Shared helpers
// ─────────────────────────────────────────────────────────────────

// groupSummaryRow converts a computer group map to a summary row for output.
func groupSummaryRow(g map[string]interface{}) map[string]interface{} {
	smart, _ := g["smartGroup"].(bool)
	groupTypeStr := "static"
	if smart {
		groupTypeStr = "smart"
	}
	return map[string]interface{}{
		"id":          extractID(g),
		"name":        extractName(g),
		"type":        groupTypeStr,
		"memberCount": groupMemberCount(g),
	}
}

// groupMemberCount extracts the member count from a group map.
// The field may be "memberCount" (float64) or derived from the "members" array.
func groupMemberCount(g map[string]interface{}) int {
	if mc, ok := g["memberCount"].(float64); ok {
		return int(mc)
	}
	if members, ok := g["members"].([]interface{}); ok {
		return len(members)
	}
	return 0
}

// unwrapClassicDetail is defined in backup.go; declared here as a reminder
// that it is available to group_tools.go within the same package.
// (No re-declaration needed — single package, already accessible.)

// ─────────────────────────────────────────────────────────────────
// Ensure yaml import is used (export subcommand uses output formatter,
// but we keep the import for direct marshalling used in tests/future use).
// ─────────────────────────────────────────────────────────────────

// marshalGroupsYAML marshals a slice of group maps to YAML bytes.
// Used by export when a caller needs raw bytes rather than formatted output.
func marshalGroupsYAML(groups []map[string]interface{}) ([]byte, error) {
	return yaml.Marshal(groups)
}

// marshalGroupsJSON marshals a slice of group maps to indented JSON bytes.
// A nil slice is normalised to an empty slice so the output is always a JSON
// array rather than null.
func marshalGroupsJSON(groups []map[string]interface{}) ([]byte, error) {
	if groups == nil {
		groups = []map[string]interface{}{}
	}
	return json.MarshalIndent(groups, "", "  ")
}
