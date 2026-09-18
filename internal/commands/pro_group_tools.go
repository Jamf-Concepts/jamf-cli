// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/jamf/jamfplatform-go-sdk/jamfplatform"
	"github.com/jamf/jamfplatform-go-sdk/jamfplatform/blueprints"
	"github.com/jamf/jamfplatform-go-sdk/jamfplatform/compliancebenchmarks"
	"github.com/jamf/jamfplatform-go-sdk/jamfplatform/devicegroups"
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
	groups, err := FetchAllPaginated(ctx, cliCtx.Client, "/v1/computer-groups", PageSizeFromPath)
	if err != nil {
		return fmt.Errorf("fetching computer groups: %w", err)
	}

	counts, countErr := computerGroupCounts(ctx, cliCtx.Client)
	unknown := warnUnknownCounts(groups, counts, countErr)
	if unknown > 0 && emptyOnly {
		fmt.Fprintf(os.Stderr, "WARNING: --empty lists only groups proved empty, so those %d are not listed\n", unknown)
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

		// Empty filter. An unknown count is not an empty group — see
		// computerGroupCounts.
		if emptyOnly {
			count, known := counts.count(g)
			if !known || count != 0 {
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

		rows = append(rows, groupSummaryRow(g, counts))
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
	groups, err := FetchAllPaginated(ctx, cliCtx.Client, "/v1/computer-groups", PageSizeFromPath)
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

--unused detects groups that no policy scope references and that hold no
members. A group with members is not a removal candidate, so it is left out
even when nothing scopes to it; a group whose member count cannot be read is
left out too and reported on stderr, because an unreadable count is not
evidence of emptiness.

When platform gateway auth is configured, --unused also checks for platform
device groups not referenced by any blueprint or compliance benchmark.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !unused {
				return fmt.Errorf("specify an analysis mode: --unused")
			}
			return runGroupToolsAnalyzeUnused(cmd.Context(), cliCtx)
		},
	}

	cmd.Flags().BoolVar(&unused, "unused", false, "find empty groups not referenced by any policy (includes platform groups when platform auth is active)")

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
	groups, err := FetchAllPaginated(ctx, cliCtx.Client, "/v1/computer-groups", PageSizeFromPath)
	if err != nil {
		return fmt.Errorf("fetching computer groups: %w", err)
	}

	counts, countErr := computerGroupCounts(ctx, cliCtx.Client)
	unknown := warnUnknownCounts(groups, counts, countErr)
	if unknown > 0 {
		fmt.Fprintf(os.Stderr, "WARNING: %d group(s) are not listed below — an unreadable count cannot prove a group is empty, and this list names removal candidates\n", unknown)
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

	// Find empty groups not referenced by anything. Membership gates the list
	// because --unused names removal candidates: a group holding 45 computers
	// that no policy scopes is a question for the administrator, not a group
	// to delete. An unknown count is not empty, so it is excluded too.
	var rows []map[string]any
	for _, g := range groups {
		name, _ := g["name"].(string)
		if referenced[name] {
			continue
		}
		if count, known := counts.count(g); !known || count != 0 {
			continue
		}
		rows = append(rows, groupSummaryRow(g, counts))
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
			// Scope is *BlueprintScope with omitempty, so a blueprint that
			// targets nothing arrives with it nil. Dereferencing it crashed
			// the whole command: `pro group-tools analyze --unused` panicked
			// with a nil pointer dereference on any tenant holding one
			// unscoped blueprint, and there is nothing an operator can do
			// about that from the command line.
			if detail.Scope == nil {
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
			// Same shape as the blueprint scope above — BenchmarkV2.Target is
			// *TargetV2 with omitempty — and the same crash. This half had not
			// fired yet only because the tenant that exposed the blueprint one
			// happened to have every benchmark targeted; an untargeted
			// benchmark reaches it identically.
			if b.Target == nil {
				continue
			}
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
	prestages, err := FetchAllPaginated(ctx, client, "/v3/computer-prestages", PageSizeFromPath)
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

	groups, err := FetchAllPaginated(ctx, cliCtx.Client, "/v1/computer-groups", PageSizeFromPath)
	if err != nil {
		return fmt.Errorf("fetching computer groups: %w", err)
	}

	rows := append([]map[string]any{}, groups...)

	if len(rows) == 0 {
		rows = []map[string]any{}
	}

	return printThrough(formatterFor(cliCtx, format), rows)
}

// ─────────────────────────────────────────────────────────────────
// Shared helpers
// ─────────────────────────────────────────────────────────────────

// memberCountUnknown is what a summary row carries in place of a member count
// this CLI could not read. Deliberately not 0, and deliberately not an absent
// key: a table's columns are the keys of its first row, so dropping the key
// would drop the column for every row below it.
const memberCountUnknown = "unknown"

// groupCountIndex maps a computer group id to its member count.
//
// The zero value is a usable empty index that answers "unknown" for every
// group, which is the point: the count is a fact about the instance that has
// to be fetched, and a group missing from the index has an unknown count
// rather than a count of zero.
type groupCountIndex struct {
	byID map[string]int

	// unreadable counts the groups a collection listed without a usable count
	// field. Such a group is left out of byID rather than entered as zero.
	unreadable int
}

// count reports a group's member count. The second return is false when the
// count is unknown, which every caller filtering on emptiness has to check —
// unknown is not empty.
func (idx groupCountIndex) count(g map[string]any) (int, bool) {
	id := extractID(g)
	if id == "" || idx.byID == nil {
		return 0, false
	}
	n, ok := idx.byID[id]
	return n, ok
}

// sweep reads one computer-group collection into the index, under the count
// field that collection carries. A group whose count field is absent or
// non-numeric is counted in unreadable and left out of the index.
func (idx *groupCountIndex) sweep(ctx context.Context, client registry.HTTPClient, path, countField string) error {
	// PageSizeFromPath, not a literal: both v3 collections are
	// {totalCount, results} Jamf Pro endpoints, so this resolves to the
	// wire-verified 2000 rather than the API default. It is the ceiling
	// itself, which is what makes the short-page termination inside
	// FetchAllPaginated sound here — do not raise it past that on these
	// paths (see MaxPageSizeFor, and #385 for why an oversized page-size
	// truncates instead of failing).
	groups, err := FetchAllPaginated(ctx, client, path, PageSizeFromPath)
	if err != nil {
		return fmt.Errorf("listing %s: %w", path, err)
	}
	if idx.byID == nil {
		idx.byID = make(map[string]int, len(groups))
	}
	for _, g := range groups {
		id := extractID(g)
		n, ok := g[countField].(float64)
		if id == "" || !ok {
			idx.unreadable++
			continue
		}
		idx.byID[id] = int(n)
	}
	return nil
}

// computerGroupCounts builds the member-count index for every computer group.
//
// /v1/computer-groups carries no count at all. Wire-checked against Jamf Pro
// 11.32 on 2026-09-18: it answers description, id, name and smartGroup and
// nothing else, so the memberCount this file used to type-assert was always
// absent and every group reported 0 members.
//
// The two v3 collections each carry one — smart groups under membershipCount,
// static groups under count — and share /v1's id space, so two paginated
// sweeps index the whole instance. Both are plain Jamf Pro API paths and both
// are served on the platform gateway (/pro/v3/computer-groups/smart-groups and
// .../static-groups, GET, in specs/gateway/coverage.json), so this works
// unchanged on a token or oauth2 profile and on a gateway profile. Platform
// /v2/groups carries the same counts but is gateway-only, which would have
// made these commands gateway-only with it.
//
// The per-group alternatives cost a request each:
// /v3/computer-groups/smart-group-membership/{id} for a smart group and the
// Classic detail for a static one. Two sweeps answer for a 500-group instance
// what 500 requests would.
//
// A sweep that fails returns its error and leaves its half of the index
// absent. Every caller then sees those groups as unknown, never as empty.
func computerGroupCounts(ctx context.Context, client registry.HTTPClient) (groupCountIndex, error) {
	var idx groupCountIndex
	smartErr := idx.sweep(ctx, client, "/v3/computer-groups/smart-groups", "membershipCount")
	staticErr := idx.sweep(ctx, client, "/v3/computer-groups/static-groups", "count")
	return idx, errors.Join(smartErr, staticErr)
}

// warnUnknownCounts reports on stderr how many of the listed groups have no
// readable member count, and returns that number. A silent gap is what shipped
// this bug, so the gap is named even when nothing filters on it.
func warnUnknownCounts(groups []map[string]any, idx groupCountIndex, err error) int {
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: member counts are incomplete: %v\n", err)
	}
	unknown := 0
	for _, g := range groups {
		if _, known := idx.count(g); !known {
			unknown++
		}
	}
	if unknown > 0 {
		fmt.Fprintf(os.Stderr, "WARNING: %d of %d groups have no readable member count, reported as %q\n", unknown, len(groups), memberCountUnknown)
	}
	return unknown
}

// groupSummaryRow converts a computer group map to a summary row for output.
// The count comes from the index rather than from the group map: the
// collection that lists groups does not carry one.
func groupSummaryRow(g map[string]any, idx groupCountIndex) map[string]any {
	smart, _ := g["smartGroup"].(bool)
	groupTypeStr := "static"
	if smart {
		groupTypeStr = "smart"
	}
	row := map[string]any{
		"id":   extractID(g),
		"name": extractName(g, "", ""),
		"type": groupTypeStr,
	}
	if n, known := idx.count(g); known {
		row["memberCount"] = n
	} else {
		row["memberCount"] = memberCountUnknown
	}
	return row
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
