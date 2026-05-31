// Copyright 2026, Jamf Software LLC

package commands

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/output"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// finishBatch maps a bulk tally to the process result. When some items
// succeeded and some failed it returns a PartialFailure (exit 7), unless
// --allow-partial-failure is set, in which case it warns and returns nil.
// A total failure propagates firstErr's exit code.
func finishBatch(stderr io.Writer, noun string, succeeded, failed int, firstErr error) error {
	if failed > 0 && succeeded > 0 && allowPartialFailure {
		_, _ = fmt.Fprintf(stderr, "warning: %d of %d %s failed; continuing (--allow-partial-failure)\n",
			failed, succeeded+failed, noun)
		return nil
	}
	msg := fmt.Sprintf("%d of %d %s failed", failed, succeeded+failed, noun)
	return exitcode.PartialOrPropagate(succeeded, failed, firstErr, msg)
}

// newBulkCmd builds the "bulk" parent command with all subcommands attached.
func newBulkCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bulk",
		Short: "Bulk operations across multiple Jamf Pro resources",
		Long: `Perform bulk mutations across policies, computer groups, and MDM commands.

Default behavior is a dry-run preview — no changes are made unless --yes is
provided. Destructive MDM commands (EraseDevice, DeviceLock) additionally
require --confirm-destructive.

Output: preview table on stdout; mutation log on stderr.`,
	}

	cmd.AddCommand(newEnablePoliciesCmd(cliCtx))
	cmd.AddCommand(newDisablePoliciesCmd(cliCtx))
	cmd.AddCommand(newAddToGroupCmd(cliCtx))
	cmd.AddCommand(newRemoveFromGroupCmd(cliCtx))
	cmd.AddCommand(newSendCommandCmd(cliCtx))

	return cmd
}

// ─────────────────────────────────────────────────────────────────
// Shared helpers
// ─────────────────────────────────────────────────────────────────

// bulkPreviewTable prints a table of affected items to stdout.
// rows must have consistent keys; the first row's keys determine columns.
func bulkPreviewTable(rows []map[string]any) {
	if len(rows) == 0 {
		fmt.Println("(no items match the given filters)")
		return
	}
	formatter := output.New("table", noColor, wide)
	if err := formatter.Print(rows); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "WARNING: table render error: %v\n", err)
	}
}

// bulkLogW writes a mutation event to the given writer.
func bulkLogW(w io.Writer, action, target, result string) {
	_, _ = fmt.Fprintf(w, "[bulk] %-30s %-40s %s\n", action, target, result)
}

// fetchClassicPolicyDetail fetches a single Classic policy by ID and returns
// its parsed detail map (unwrapped from the "policy" key).
func fetchClassicPolicyDetail(ctx context.Context, client registry.HTTPClient, id string) (map[string]any, error) {
	path := fmt.Sprintf("/JSSResource/policies/id/%s", id)
	data, err := fetchJSON(ctx, client, path)
	if err != nil {
		return nil, err
	}
	return unwrapClassicDetail(data), nil
}

// policyBulkFilters groups all filter parameters for bulk policy operations.
// Within a single slice filter, values are OR (match any). Between different
// filters, AND (all must match).
type policyBulkFilters struct {
	namePattern        string
	category           string
	allComputers       *bool // nil = filter disabled
	scopeGroups        []string
	scopeBuildings     []string
	scopeDepartments   []string
	limitNetSegments   []string
	limitUserGroups    []string
	excludeGroups      []string
	excludeBuildings   []string
	excludeDepartments []string
}

// policyMatchesFilters returns true when the policy satisfies all active
// filter criteria. Empty slices and zero-value fields disable the corresponding filter.
func policyMatchesFilters(policy map[string]any, f policyBulkFilters) (bool, error) {
	// Name pattern (glob-style: only * wildcard supported)
	if f.namePattern != "" {
		general, _ := policy["general"].(map[string]any)
		name, _ := general["name"].(string)
		matched, err := matchGlob(f.namePattern, name)
		if err != nil {
			return false, fmt.Errorf("invalid name pattern %q: %w", f.namePattern, err)
		}
		if !matched {
			return false, nil
		}
	}

	// Category filter — Classic API nests category under "general".
	if f.category != "" {
		general, _ := policy["general"].(map[string]any)
		cat, _ := general["category"].(map[string]any)
		catName, _ := cat["name"].(string)
		if !strings.EqualFold(catName, f.category) {
			return false, nil
		}
	}

	scope, _ := policy["scope"].(map[string]any)

	// All-computers filter
	if f.allComputers != nil {
		allComp, _ := scope["all_computers"].(bool)
		if allComp != *f.allComputers {
			return false, nil
		}
	}

	// Target scope filters
	if len(f.scopeGroups) > 0 {
		if !scopeItemsContainAny(extractScopeItems(scope, "computer_groups", "computer_group"), f.scopeGroups) {
			return false, nil
		}
	}
	if len(f.scopeBuildings) > 0 {
		if !scopeItemsContainAny(extractScopeItems(scope, "buildings", "building"), f.scopeBuildings) {
			return false, nil
		}
	}
	if len(f.scopeDepartments) > 0 {
		if !scopeItemsContainAny(extractScopeItems(scope, "departments", "department"), f.scopeDepartments) {
			return false, nil
		}
	}

	// Limitation filters
	limitations, _ := scope["limitations"].(map[string]any)
	if len(f.limitNetSegments) > 0 {
		if !scopeItemsContainAny(extractScopeItems(limitations, "network_segments", "network_segment"), f.limitNetSegments) {
			return false, nil
		}
	}
	if len(f.limitUserGroups) > 0 {
		if !scopeItemsContainAny(extractScopeItems(limitations, "user_groups", "user_group"), f.limitUserGroups) {
			return false, nil
		}
	}

	// Exclusion filters
	exclusions, _ := scope["exclusions"].(map[string]any)
	if len(f.excludeGroups) > 0 {
		if !scopeItemsContainAny(extractScopeItems(exclusions, "computer_groups", "computer_group"), f.excludeGroups) {
			return false, nil
		}
	}
	if len(f.excludeBuildings) > 0 {
		if !scopeItemsContainAny(extractScopeItems(exclusions, "buildings", "building"), f.excludeBuildings) {
			return false, nil
		}
	}
	if len(f.excludeDepartments) > 0 {
		if !scopeItemsContainAny(extractScopeItems(exclusions, "departments", "department"), f.excludeDepartments) {
			return false, nil
		}
	}

	return true, nil
}

// extractScopeItems extracts named items from an xmlconv-parsed scope collection.
// xmlconv produces different forms depending on the XML:
//   - []any when the element had a <size> child
//   - map[string]any{childKey: ...} when no <size>
//   - string (empty) for self-closing elements like <buildings/>
func extractScopeItems(parent map[string]any, collectionKey, childKey string) []map[string]any {
	if parent == nil {
		return nil
	}
	raw := parent[collectionKey]

	var items []any
	switch v := raw.(type) {
	case []any:
		items = v
	case map[string]any:
		switch inner := v[childKey].(type) {
		case []any:
			items = inner
		case map[string]any:
			items = []any{inner}
		}
	}

	if len(items) == 0 {
		return nil
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			result = append(result, m)
		}
	}
	return result
}

// scopeItemsContainAny returns true if any item in the collection has a name
// matching (case-insensitive) any of the given target names.
func scopeItemsContainAny(items []map[string]any, names []string) bool {
	for _, item := range items {
		itemName, _ := item["name"].(string)
		for _, want := range names {
			if strings.EqualFold(itemName, want) {
				return true
			}
		}
	}
	return false
}

// matchGlob matches a simple glob pattern (* wildcard only) against a string.
// Anchored at both ends (i.e., the whole string must match).
func matchGlob(pattern, s string) (bool, error) {
	parts := strings.Split(pattern, "*")
	for i, p := range parts {
		parts[i] = regexp.QuoteMeta(p)
	}
	regexStr := "(?i)^" + strings.Join(parts, ".*") + "$"
	re, err := regexp.Compile(regexStr)
	if err != nil {
		return false, err
	}
	return re.MatchString(s), nil
}

// doClassicPolicyUpdate sends a PUT to update the policy's enabled state.
// The minimal XML body only flips the <enabled> element.
func doClassicPolicyUpdate(ctx context.Context, client registry.HTTPClient, id string, enabled bool) error {
	enabledStr := "true"
	if !enabled {
		enabledStr = "false"
	}
	xmlBody := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><policy><general><enabled>%s</enabled></general></policy>`, enabledStr)
	path := fmt.Sprintf("/JSSResource/policies/id/%s", id)
	resp, err := client.Do(ctx, "PUT", path, strings.NewReader(xmlBody))
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// readIDsFromFile reads device serial numbers or IDs from a plain-text file,
// one entry per line.  Blank lines and lines starting with # are skipped.
func readIDsFromFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening file %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	var ids []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		ids = append(ids, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading file %s: %w", path, err)
	}
	return ids, nil
}

// fetchComputerGroupMemberIDs returns the computer IDs that belong to the
// named static or smart computer group (Classic API).
func fetchComputerGroupMemberIDs(ctx context.Context, client registry.HTTPClient, groupName string) ([]string, error) {
	raw, err := FetchClassicList(ctx, client, "/JSSResource/computergroups", "computer_groups")
	if err != nil {
		return nil, fmt.Errorf("listing computer groups: %w", err)
	}

	groupID := ""
	for _, r := range raw {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		name, _ := m["name"].(string)
		if strings.EqualFold(name, groupName) {
			groupID = extractID(m)
			break
		}
	}
	if groupID == "" {
		return nil, fmt.Errorf("computer group %q not found", groupName)
	}

	data, err := fetchJSON(ctx, client, fmt.Sprintf("/JSSResource/computergroups/id/%s", groupID))
	if err != nil {
		return nil, fmt.Errorf("fetching group %s: %w", groupID, err)
	}
	detail := unwrapClassicDetail(data)

	computers, _ := detail["computers"].(map[string]any)
	if computers == nil {
		computers, _ = data["computers"].(map[string]any)
	}

	var members []any
	if computers != nil {
		members, _ = computers["computer"].([]any)
	}
	if members == nil {
		flat, _ := detail["computers"].([]any)
		members = flat
	}

	var ids []string
	for _, m := range members {
		mm, ok := m.(map[string]any)
		if !ok {
			continue
		}
		id := extractID(mm)
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// resolveComputerTargets returns a list of (id, name) pairs from either
// a file or a group name.  Exactly one of fromFile/groupName must be set.
func resolveComputerTargets(ctx context.Context, client registry.HTTPClient, fromFile, groupName string) ([]map[string]string, error) {
	switch {
	case fromFile != "" && groupName != "":
		return nil, fmt.Errorf("--from-file and --group are mutually exclusive")
	case fromFile == "" && groupName == "":
		return nil, fmt.Errorf("either --from-file or --group is required")
	}

	if fromFile != "" {
		ids, err := readIDsFromFile(fromFile)
		if err != nil {
			return nil, err
		}
		targets := make([]map[string]string, len(ids))
		for i, id := range ids {
			targets[i] = map[string]string{"id": id, "name": id}
		}
		return targets, nil
	}

	// Group mode: resolve member IDs then fetch names.
	ids, err := fetchComputerGroupMemberIDs(ctx, client, groupName)
	if err != nil {
		return nil, err
	}

	targets := make([]map[string]string, 0, len(ids))
	for _, id := range ids {
		data, err := fetchJSON(ctx, client, fmt.Sprintf("/JSSResource/computers/id/%s", id))
		if err != nil {
			// Non-fatal: warn and skip name resolution but keep the ID.
			_, _ = fmt.Fprintf(os.Stderr, "WARNING: could not fetch name for computer id=%s: %v\n", id, err)
			targets = append(targets, map[string]string{"id": id, "name": id})
			continue
		}
		detail := unwrapClassicDetail(data)
		name, _ := detail["name"].(string)
		if name == "" {
			name = id
		}
		targets = append(targets, map[string]string{"id": id, "name": name})
	}
	return targets, nil
}

// sendMDMCommand posts a Classic API MDM command to a single computer.
// Used by the bulk send-command operation; individual action commands use sendComputerModernMDMCommand.
func sendMDMCommand(ctx context.Context, client registry.HTTPClient, computerID, command string) error {
	path := fmt.Sprintf("/JSSResource/computercommands/command/%s/id/%s", command, computerID)
	resp, err := client.Do(ctx, "POST", path, nil)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// bulkPolicyRows converts policy detail maps into preview rows.
func bulkPolicyRows(policies []map[string]any) []map[string]any {
	rows := make([]map[string]any, len(policies))
	for i, p := range policies {
		// Classic API nests id, name, enabled under "general".
		general, _ := p["general"].(map[string]any)
		id := extractID(general)
		name, _ := general["name"].(string)
		enabled, _ := general["enabled"].(bool)
		cat, _ := general["category"].(map[string]any)
		catName, _ := cat["name"].(string)
		rows[i] = map[string]any{
			"id":       id,
			"name":     name,
			"category": catName,
			"enabled":  enabled,
		}
	}
	return rows
}

// destructiveMDMCommands is the set of commands that require --confirm-destructive.
var destructiveMDMCommands = map[string]bool{
	"EraseDevice": true,
	"DeviceLock":  true,
}

// staticGroupAddComputerXML builds the XML body to add a computer by ID to a
// static group via PUT on the classic API.
func staticGroupAddComputerXML(computerID string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><computer_group><computer_additions><computer><id>%s</id></computer></computer_additions></computer_group>`, computerID)
}

// staticGroupRemoveComputerXML builds the XML body to remove a computer by ID
// from a static group via PUT on the classic API.
func staticGroupRemoveComputerXML(computerID string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><computer_group><computer_deletions><computer><id>%s</id></computer></computer_deletions></computer_group>`, computerID)
}

// lookupStaticGroupID returns the numeric ID of a static computer group by name.
func lookupStaticGroupID(ctx context.Context, client registry.HTTPClient, groupName string) (string, error) {
	raw, err := FetchClassicList(ctx, client, "/JSSResource/computergroups", "computer_groups")
	if err != nil {
		return "", fmt.Errorf("listing computer groups: %w", err)
	}
	for _, r := range raw {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		name, _ := m["name"].(string)
		if strings.EqualFold(name, groupName) {
			id := extractID(m)
			data, err := fetchJSON(ctx, client, fmt.Sprintf("/JSSResource/computergroups/id/%s", id))
			if err != nil {
				return "", fmt.Errorf("fetching group %s: %w", id, err)
			}
			detail := unwrapClassicDetail(data)
			isSmart, _ := detail["is_smart"].(bool)
			if isSmart {
				return "", fmt.Errorf("group %q is a smart group; only static groups can be modified", groupName)
			}
			return id, nil
		}
	}
	return "", fmt.Errorf("computer group %q not found", groupName)
}

// applyStaticGroupMutation sends add or remove XML to a static group for one computer.
func applyStaticGroupMutation(ctx context.Context, client registry.HTTPClient, groupID, computerID string, add bool) error {
	var xmlBody string
	if add {
		xmlBody = staticGroupAddComputerXML(computerID)
	} else {
		xmlBody = staticGroupRemoveComputerXML(computerID)
	}
	path := fmt.Sprintf("/JSSResource/computergroups/id/%s", groupID)
	resp, err := client.Do(ctx, "PUT", path, strings.NewReader(xmlBody))
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}
