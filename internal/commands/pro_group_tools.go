// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/blueprints"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/compliancebenchmarks"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/devicegroups"
)

// newGroupToolsCmd builds the "group-tools" parent command with subcommands.
func newGroupToolsCmd(cliCtx *registry.CLIContext) *cobra.Command {
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

func newGroupToolsListCmd(cliCtx *registry.CLIContext) *cobra.Command {
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

func runGroupToolsList(ctx context.Context, cliCtx *registry.CLIContext, groupType string, emptyOnly bool, namePattern string) error {
	groups, err := FetchAllPaginated(ctx, cliCtx.Client, "/v1/computer-groups", 100)
	if err != nil {
		return fmt.Errorf("fetching computer groups: %w", err)
	}

	var rows []map[string]any
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
		rows = []map[string]any{}
	}

	return printRows(cliCtx, rows)
}

// ─────────────────────────────────────────────────────────────────
// members
// ─────────────────────────────────────────────────────────────────

func newGroupToolsMembersCmd(cliCtx *registry.CLIContext) *cobra.Command {
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

func runGroupToolsMembers(ctx context.Context, cliCtx *registry.CLIContext, name string) error {
	groups, err := FetchAllPaginated(ctx, cliCtx.Client, "/v1/computer-groups", 100)
	if err != nil {
		return fmt.Errorf("fetching computer groups: %w", err)
	}

	// Find the group by exact name (case-insensitive), capturing type
	var groupID string
	var isSmart bool
	for _, g := range groups {
		n, _ := g["name"].(string)
		if strings.EqualFold(n, name) {
			groupID = extractID(g)
			isSmart, _ = g["smartGroup"].(bool)
			break
		}
	}
	if groupID == "" {
		return fmt.Errorf("group %q not found", name)
	}

	// Smart groups use the v3 membership endpoint; static groups use Classic API
	var rows []map[string]any
	if isSmart {
		detail, err := FetchJSON(ctx, cliCtx.Client, fmt.Sprintf("/v3/computer-groups/smart-group-membership/%s", groupID))
		if err != nil {
			return fmt.Errorf("fetching smart group membership: %w", err)
		}
		members, _ := detail["members"].([]any)
		for _, m := range members {
			rows = append(rows, map[string]any{"id": anyToIDString(m)})
		}
	} else {
		data, err := FetchJSON(ctx, cliCtx.Client, fmt.Sprintf("/JSSResource/computergroups/id/%s", groupID))
		if err != nil {
			return fmt.Errorf("fetching static group detail: %w", err)
		}
		detail := unwrapClassicDetail(data)

		computers, _ := detail["computers"].(map[string]any)
		if computers == nil {
			computers, _ = data["computers"].(map[string]any)
		}

		var members []any
		if computers != nil {
			members, _ = computers["computer"].([]any)
			if members == nil {
				// Single-item case: Classic API returns a map instead of an array
				if single, ok := computers["computer"].(map[string]any); ok {
					members = []any{single}
				}
			}
		}
		if members == nil {
			flat, _ := detail["computers"].([]any)
			members = flat
		}

		for _, m := range members {
			mm, ok := m.(map[string]any)
			if !ok {
				continue
			}
			row := map[string]any{"id": extractID(mm)}
			if n := extractName(mm, "", ""); n != "" {
				row["name"] = n
			}
			rows = append(rows, row)
		}
	}

	if len(rows) == 0 {
		rows = []map[string]any{}
	}

	return printRows(cliCtx, rows)
}

// anyToIDString converts a JSON value (float64 or string) to an ID string.
func anyToIDString(v any) string {
	switch id := v.(type) {
	case float64:
		return fmt.Sprintf("%d", int(id))
	case string:
		return id
	default:
		return fmt.Sprintf("%v", v)
	}
}

// ─────────────────────────────────────────────────────────────────
// analyze
// ─────────────────────────────────────────────────────────────────

func newGroupToolsAnalyzeCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var unused bool

	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "Analyze computer groups for hygiene issues",
		Long: `Run hygiene analysis on computer groups.

--unused detects groups not referenced by any policy scope. When platform
gateway auth is configured, also checks for platform device groups not
referenced by any blueprint or compliance benchmark.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !unused {
				return fmt.Errorf("specify an analysis mode: --unused")
			}
			return runGroupToolsAnalyzeUnused(cmd.Context(), cliCtx)
		},
	}

	cmd.Flags().BoolVar(&unused, "unused", false, "find groups not referenced by any policy (includes platform groups when platform auth is active)")

	return cmd
}

// scopeableResource defines a Classic API resource type that scopes to computer groups.
type scopeableResource struct {
	Name       string // display name for logging
	ListPath   string // Classic API list endpoint
	WrapperKey string // JSON wrapper key for the list response
	DetailPath string // Classic API detail endpoint with %s for ID
}

// scopeableResources lists all Classic API resources that can scope to computer groups.
var scopeableResources = []scopeableResource{
	{"policies", "/JSSResource/policies", "policies", "/JSSResource/policies/id/%s"},
	{"macOS profiles", "/JSSResource/osxconfigurationprofiles", "os_x_configuration_profiles", "/JSSResource/osxconfigurationprofiles/id/%s"},
	{"restricted software", "/JSSResource/restrictedsoftware", "restricted_software", "/JSSResource/restrictedsoftware/id/%s"},
	{"ebooks", "/JSSResource/ebooks", "ebooks", "/JSSResource/ebooks/id/%s"},
	{"patch policies", "/JSSResource/patchpolicies", "patch_policies", "/JSSResource/patchpolicies/id/%s"},
}

func runGroupToolsAnalyzeUnused(ctx context.Context, cliCtx *registry.CLIContext) error {
	// Fetch all computer groups
	groups, err := FetchAllPaginated(ctx, cliCtx.Client, "/v1/computer-groups", 100)
	if err != nil {
		return fmt.Errorf("fetching computer groups: %w", err)
	}

	referenced := make(map[string]bool)

	// Check all scopeable Classic API resources
	for _, res := range scopeableResources {
		fmt.Fprintf(os.Stderr, "Checking %s...\n", res.Name)
		addReferencedGroupsFromClassic(ctx, cliCtx.Client, res, referenced)
	}

	// Check modern API resources with group scoping
	fmt.Fprintf(os.Stderr, "Checking computer prestages...\n")
	addReferencedGroupsFromPrestages(ctx, cliCtx.Client, referenced)

	// Also mark groups referenced by platform blueprints/benchmarks
	if cliCtx.PlatformSDKClient != nil {
		fmt.Fprintf(os.Stderr, "Checking platform blueprints and benchmarks...\n")
		addPlatformReferencedGroups(ctx, cliCtx.PlatformSDKClient, referenced)
	}

	fmt.Fprintln(os.Stderr)

	// Find groups not referenced by anything
	var rows []map[string]any
	for _, g := range groups {
		name, _ := g["name"].(string)
		if referenced[name] {
			continue
		}
		rows = append(rows, groupSummaryRow(g))
	}

	if len(rows) == 0 {
		rows = []map[string]any{}
	}

	return printRows(cliCtx, rows)
}

// addPlatformReferencedGroups adds group names referenced by blueprints
// and compliance benchmarks to the referenced set. Silently skips on errors.
func addPlatformReferencedGroups(ctx context.Context, c *jamfplatform.Client, referenced map[string]bool) {
	bp := blueprints.New(c)
	cb := compliancebenchmarks.New(c)
	dg := devicegroups.New(c)

	// Build ID→name map from platform device groups
	groups, err := dg.ListDeviceGroups(ctx, nil, "")
	if err != nil {
		return
	}
	idToName := make(map[string]string, len(groups))
	for _, g := range groups {
		idToName[g.ID] = g.Name
	}

	// Mark groups referenced by blueprints
	bps, err := bp.ListBlueprints(ctx, nil, "")
	if err == nil {
		for _, item := range bps {
			detail, err := bp.GetBlueprint(ctx, item.ID)
			if err != nil {
				continue
			}
			for _, gid := range detail.Scope.DeviceGroups {
				if name, ok := idToName[gid]; ok {
					referenced[name] = true
				}
			}
		}
	}

	// Mark groups referenced by benchmarks
	resp, err := cb.ListBenchmarks(ctx)
	if err == nil {
		for _, b := range resp.Benchmarks {
			for _, gid := range b.Target.DeviceGroups {
				if name, ok := idToName[gid]; ok {
					referenced[name] = true
				}
			}
		}
	}
}

// addReferencedGroupsFromClassic fetches all items of a scopeable Classic resource,
// gets their detail in parallel, and adds any referenced group names to the set.
func addReferencedGroupsFromClassic(ctx context.Context, client registry.HTTPClient, res scopeableResource, referenced map[string]bool) {
	items, err := FetchClassicList(ctx, client, res.ListPath, res.WrapperKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: failed to list %s: %v\n", res.Name, err)
		return
	}

	type stub struct{ id string }
	var stubs []stub
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if id := extractID(m); id != "" {
			stubs = append(stubs, stub{id})
		}
	}

	details, fetchErrs := BoundedParallelFetch(ctx, stubs, 10, func(ctx context.Context, s stub) (map[string]any, error) {
		path := fmt.Sprintf(res.DetailPath, s.id)
		data, err := FetchJSON(ctx, client, path)
		if err != nil {
			return nil, err
		}
		return unwrapClassicDetail(data), nil
	})
	if len(fetchErrs) > 0 {
		fmt.Fprintf(os.Stderr, "WARNING: %d of %d %s detail fetches failed\n", len(fetchErrs), len(stubs), res.Name)
	}

	for _, detail := range details {
		if detail == nil {
			continue
		}
		scope, _ := detail["scope"].(map[string]any)
		if scope == nil {
			continue
		}
		addGroupNamesFromScope(scope, referenced)
	}
}

// addReferencedGroupsFromPrestages checks computer prestage scopes (modern API).
func addReferencedGroupsFromPrestages(ctx context.Context, client registry.HTTPClient, referenced map[string]bool) {
	prestages, err := FetchAllPaginated(ctx, client, "/v3/computer-prestages", 100)
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: failed to list computer prestages: %v\n", err)
		return
	}
	for _, ps := range prestages {
		// Prestage scope uses locationInformation.departmentId or direct group references
		// but the key mechanism is versionLock + scope assignments. Check for group references.
		scope, _ := ps["purchasingInformation"].(map[string]any)
		if scope != nil {
			addGroupNamesFromScope(scope, referenced)
		}
		// Also check if there's a direct scope block
		if s, ok := ps["scope"].(map[string]any); ok {
			addGroupNamesFromScope(s, referenced)
		}
	}
}

// addGroupNamesFromScope extracts computer group names from a Classic API scope object.
func addGroupNamesFromScope(scope map[string]any, out map[string]bool) {
	for _, key := range []string{"computerGroups", "computer_groups"} {
		arr, _ := scope[key].([]any)
		for _, item := range arr {
			m, ok := item.(map[string]any)
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

func newGroupToolsExportCmd(cliCtx *registry.CLIContext) *cobra.Command {
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

func runGroupToolsExport(ctx context.Context, cliCtx *registry.CLIContext, format string) error {
	switch format {
	case "yaml", "json":
	default:
		return fmt.Errorf("unsupported format %q: must be yaml or json", format)
	}

	groups, err := FetchAllPaginated(ctx, cliCtx.Client, "/v1/computer-groups", 100)
	if err != nil {
		return fmt.Errorf("fetching computer groups: %w", err)
	}

	rows := append([]map[string]any{}, groups...)

	if len(rows) == 0 {
		rows = []map[string]any{}
	}

	return formatterFor(cliCtx, format).Print(rows)
}

// ─────────────────────────────────────────────────────────────────
// Shared helpers
// ─────────────────────────────────────────────────────────────────

// groupSummaryRow converts a computer group map to a summary row for output.
func groupSummaryRow(g map[string]any) map[string]any {
	smart, _ := g["smartGroup"].(bool)
	groupTypeStr := "static"
	if smart {
		groupTypeStr = "smart"
	}
	return map[string]any{
		"id":          extractID(g),
		"name":        extractName(g, "", ""),
		"type":        groupTypeStr,
		"memberCount": groupMemberCount(g),
	}
}

// groupMemberCount extracts the member count from a group map.
// The field may be "memberCount" (float64) or derived from the "members" array.
func groupMemberCount(g map[string]any) int {
	if mc, ok := g["memberCount"].(float64); ok {
		return int(mc)
	}
	if members, ok := g["members"].([]any); ok {
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
func marshalGroupsYAML(groups []map[string]any) ([]byte, error) {
	return yaml.Marshal(groups)
}

// marshalGroupsJSON marshals a slice of group maps to indented JSON bytes.
// A nil slice is normalised to an empty slice so the output is always a JSON
// array rather than null.
func marshalGroupsJSON(groups []map[string]any) ([]byte, error) {
	if groups == nil {
		groups = []map[string]any{}
	}
	return json.MarshalIndent(groups, "", "  ")
}
