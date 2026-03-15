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

	"github.com/jamf/jamfpro-cli/internal/commands/generated"
	"github.com/jamf/jamfpro-cli/internal/output"
)

// newBulkCmd builds the "bulk" parent command with all subcommands attached.
func newBulkCmd(cliCtx *generated.CLIContext) *cobra.Command {
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
func bulkPreviewTable(rows []map[string]interface{}) {
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
func fetchClassicPolicyDetail(ctx context.Context, client generated.HTTPClient, id string) (map[string]interface{}, error) {
	path := fmt.Sprintf("/JSSResource/policies/id/%s", id)
	data, err := fetchJSON(ctx, client, path)
	if err != nil {
		return nil, err
	}
	return unwrapClassicDetail(data), nil
}

// policyMatchesFilters returns true when the policy satisfies all active
// filter criteria.  Empty values disable the corresponding filter.
func policyMatchesFilters(policy map[string]interface{}, scopeGroup, category, namePattern string) (bool, error) {
	// Name pattern (glob-style: only * wildcard supported)
	if namePattern != "" {
		name, _ := policy["name"].(string)
		matched, err := matchGlob(namePattern, name)
		if err != nil {
			return false, fmt.Errorf("invalid name pattern %q: %w", namePattern, err)
		}
		if !matched {
			return false, nil
		}
	}

	// Category filter
	if category != "" {
		cat, _ := policy["category"].(map[string]interface{})
		catName, _ := cat["name"].(string)
		if !strings.EqualFold(catName, category) {
			return false, nil
		}
	}

	// Scope group filter
	if scopeGroup != "" {
		scope, _ := policy["scope"].(map[string]interface{})
		groups, _ := scope["computer_groups"].([]interface{})
		found := false
		for _, g := range groups {
			gm, ok := g.(map[string]interface{})
			if !ok {
				continue
			}
			gName, _ := gm["name"].(string)
			if strings.EqualFold(gName, scopeGroup) {
				found = true
				break
			}
		}
		if !found {
			return false, nil
		}
	}

	return true, nil
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
func doClassicPolicyUpdate(ctx context.Context, client generated.HTTPClient, id string, enabled bool) error {
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
func fetchComputerGroupMemberIDs(ctx context.Context, client generated.HTTPClient, groupName string) ([]string, error) {
	raw, err := FetchClassicList(ctx, client, "/JSSResource/computergroups", "computer_groups")
	if err != nil {
		return nil, fmt.Errorf("listing computer groups: %w", err)
	}

	groupID := ""
	for _, r := range raw {
		m, ok := r.(map[string]interface{})
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

	computers, _ := detail["computers"].(map[string]interface{})
	if computers == nil {
		computers, _ = data["computers"].(map[string]interface{})
	}

	var members []interface{}
	if computers != nil {
		members, _ = computers["computer"].([]interface{})
	}
	if members == nil {
		flat, _ := detail["computers"].([]interface{})
		members = flat
	}

	var ids []string
	for _, m := range members {
		mm, ok := m.(map[string]interface{})
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
func resolveComputerTargets(ctx context.Context, client generated.HTTPClient, fromFile, groupName string) ([]map[string]string, error) {
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
func sendMDMCommand(ctx context.Context, client generated.HTTPClient, computerID, command string) error {
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
func bulkPolicyRows(policies []map[string]interface{}) []map[string]interface{} {
	rows := make([]map[string]interface{}, len(policies))
	for i, p := range policies {
		id := extractID(p)
		name, _ := p["name"].(string)
		cat, _ := p["category"].(map[string]interface{})
		catName, _ := cat["name"].(string)
		enabled, _ := p["enabled"].(bool)
		rows[i] = map[string]interface{}{
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
func lookupStaticGroupID(ctx context.Context, client generated.HTTPClient, groupName string) (string, error) {
	raw, err := FetchClassicList(ctx, client, "/JSSResource/computergroups", "computer_groups")
	if err != nil {
		return "", fmt.Errorf("listing computer groups: %w", err)
	}
	for _, r := range raw {
		m, ok := r.(map[string]interface{})
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
func applyStaticGroupMutation(ctx context.Context, client generated.HTTPClient, groupID, computerID string, add bool) error {
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
