// Copyright 2026, Jamf Software LLC

package commands

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// newEnablePoliciesCmd creates the "bulk enable-policies" subcommand.
func newEnablePoliciesCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return newTogglePoliciesCmd(cliCtx, true)
}

// newDisablePoliciesCmd creates the "bulk disable-policies" subcommand.
func newDisablePoliciesCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return newTogglePoliciesCmd(cliCtx, false)
}

// newTogglePoliciesCmd is the shared builder for enable-policies / disable-policies.
func newTogglePoliciesCmd(cliCtx *registry.CLIContext, enable bool) *cobra.Command {
	var (
		category           string
		namePattern        string
		allComputers       bool
		scopeGroups        []string
		scopeBuildings     []string
		scopeDepartments   []string
		limitNetSegments   []string
		limitUserGroups    []string
		excludeGroups      []string
		excludeBuildings   []string
		excludeDepartments []string
		yes                bool
	)

	verb := "enable"
	if !enable {
		verb = "disable"
	}

	cmd := &cobra.Command{
		Use:   verb + "-policies",
		Short: fmt.Sprintf("Bulk %s policies matching the given filters", verb),
		Long: fmt.Sprintf(`Fetch all Classic API policies and %s those that match all provided
filters. Multiple filters are combined with AND logic. Within a repeatable
flag, values are combined with OR logic.

Without --yes the command prints a preview table and exits without making any
changes.

Available filters:

  Identity:
    --name-pattern    glob match on policy name (e.g. "Deploy *")
    --category        category name (case-insensitive)

  Target scope:
    --scope-group     target computer group (repeatable)
    --scope-building  target building (repeatable)
    --scope-department target department (repeatable)
    --all-computers   only policies scoped to all computers

  Limitations:
    --limit-network-segment  limitation network segment (repeatable)
    --limit-user-group       limitation user group (repeatable)

  Exclusions:
    --exclude-group      exclusion computer group (repeatable)
    --exclude-building   exclusion building (repeatable)
    --exclude-department exclusion department (repeatable)`, verb),
		RunE: func(cmd *cobra.Command, args []string) error {
			f := policyBulkFilters{
				namePattern:        namePattern,
				category:           category,
				scopeGroups:        scopeGroups,
				scopeBuildings:     scopeBuildings,
				scopeDepartments:   scopeDepartments,
				limitNetSegments:   limitNetSegments,
				limitUserGroups:    limitUserGroups,
				excludeGroups:      excludeGroups,
				excludeBuildings:   excludeBuildings,
				excludeDepartments: excludeDepartments,
			}
			if cmd.Flags().Changed("all-computers") {
				f.allComputers = &allComputers
			}
			return runTogglePolicies(cmd, cliCtx, enable, f, yes)
		},
	}

	cmd.Flags().StringVar(&namePattern, "name-pattern", "", "only policies whose name matches this glob (e.g. \"Deploy *\")")
	cmd.Flags().StringVar(&category, "category", "", "only policies in this category (case-insensitive)")

	cmd.Flags().StringArrayVar(&scopeGroups, "scope-group", nil, "only policies scoped to this computer group (repeatable)")
	cmd.Flags().StringArrayVar(&scopeBuildings, "scope-building", nil, "only policies scoped to this building (repeatable)")
	cmd.Flags().StringArrayVar(&scopeDepartments, "scope-department", nil, "only policies scoped to this department (repeatable)")
	cmd.Flags().BoolVar(&allComputers, "all-computers", false, "only policies scoped to all computers")

	cmd.Flags().StringArrayVar(&limitNetSegments, "limit-network-segment", nil, "only policies limited to this network segment (repeatable)")
	cmd.Flags().StringArrayVar(&limitUserGroups, "limit-user-group", nil, "only policies limited to this user group (repeatable)")

	cmd.Flags().StringArrayVar(&excludeGroups, "exclude-group", nil, "only policies excluding this computer group (repeatable)")
	cmd.Flags().StringArrayVar(&excludeBuildings, "exclude-building", nil, "only policies excluding this building (repeatable)")
	cmd.Flags().StringArrayVar(&excludeDepartments, "exclude-department", nil, "only policies excluding this department (repeatable)")

	cmd.Flags().BoolVar(&yes, "yes", false, "execute mutations (default: dry-run preview only)")

	return cmd
}

func runTogglePolicies(
	cmd *cobra.Command,
	cliCtx *registry.CLIContext,
	enable bool,
	f policyBulkFilters,
	yes bool,
) error {
	ctx := cmd.Context()
	client := cliCtx.Client
	stderr := cmd.ErrOrStderr()

	verb := "enable"
	if !enable {
		verb = "disable"
	}

	// 1. List all policies.
	rawPolicies, err := FetchClassicList(ctx, client, "/JSSResource/policies", "policies")
	if err != nil {
		return fmt.Errorf("listing policies: %w", err)
	}

	// 2. For each policy that passes the quick list-level name pre-filter, fetch
	//    the full detail (needed for category, scope group, and current state).
	type policyEntry struct {
		id     string
		detail map[string]any
	}

	var candidates []policyEntry
	for _, r := range rawPolicies {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		id := extractID(m)
		if id == "" {
			continue
		}

		// Quick name pre-filter avoids a detail fetch for non-matching names.
		if f.namePattern != "" {
			listName, _ := m["name"].(string)
			matched, err := matchGlob(f.namePattern, listName)
			if err != nil {
				return err
			}
			if !matched {
				continue
			}
		}

		detail, err := fetchClassicPolicyDetail(ctx, client, id)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "WARNING: failed to fetch policy id=%s: %v\n", id, err)
			continue
		}
		candidates = append(candidates, policyEntry{id: id, detail: detail})
	}

	// 3. Apply remaining filters against the full detail.
	var matched []policyEntry
	for _, p := range candidates {
		ok, err := policyMatchesFilters(p.detail, f)
		if err != nil {
			return err
		}
		if ok {
			// Skip policies that are already in the desired state.
			// The Classic API nests enabled under "general".
			general, _ := p.detail["general"].(map[string]any)
			currentlyEnabled, _ := general["enabled"].(bool)
			if currentlyEnabled == enable {
				continue
			}
			matched = append(matched, p)
		}
	}

	if len(matched) == 0 {
		_, _ = fmt.Fprintf(stderr, "No policies require changes.\n")
		return nil
	}

	// 4. Build preview rows.
	detailMaps := make([]map[string]any, len(matched))
	for i, p := range matched {
		detailMaps[i] = p.detail
	}
	previewRows := bulkPolicyRows(detailMaps)

	// 5. Dry-run: print preview table to stdout and log intent to stderr.
	if !yes {
		_, _ = fmt.Fprintf(stderr, "[dry-run] Would %s %d policies (use --yes to apply):\n", verb, len(matched))
		bulkPreviewTable(previewRows)
		return nil
	}

	// 6. Execute mutations.
	_, _ = fmt.Fprintf(stderr, "%sing %d policies...\n", capitalize(verb), len(matched))

	var successCount, failCount int
	var firstErr error
	for _, p := range matched {
		general, _ := p.detail["general"].(map[string]any)
		name, _ := general["name"].(string)
		if err := doClassicPolicyUpdate(ctx, client, p.id, enable); err != nil {
			bulkLogW(stderr, verb+" policy", name, "ERROR: "+err.Error())
			if firstErr == nil {
				firstErr = err
			}
			failCount++
		} else {
			bulkLogW(stderr, verb+" policy", name, "ok")
			successCount++
		}
	}

	_, _ = fmt.Fprintf(stderr, "%sd %d policies; %d failed.\n", capitalize(verb), successCount, failCount)
	return finishBatch(stderr, fmt.Sprintf("policy %s operations", verb), successCount, failCount, firstErr)
}

// capitalize returns the string with its first letter uppercased.
func capitalize(s string) string {
	if len(s) == 0 {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
