package commands

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/commands/generated"
)

// newEnablePoliciesCmd creates the "bulk enable-policies" subcommand.
func newEnablePoliciesCmd(cliCtx *generated.CLIContext) *cobra.Command {
	return newTogglePoliciesCmd(cliCtx, true)
}

// newDisablePoliciesCmd creates the "bulk disable-policies" subcommand.
func newDisablePoliciesCmd(cliCtx *generated.CLIContext) *cobra.Command {
	return newTogglePoliciesCmd(cliCtx, false)
}

// newTogglePoliciesCmd is the shared builder for enable-policies / disable-policies.
func newTogglePoliciesCmd(cliCtx *generated.CLIContext, enable bool) *cobra.Command {
	var (
		scopeGroup  string
		category    string
		namePattern string
		yes         bool
	)

	verb := "enable"
	if !enable {
		verb = "disable"
	}

	cmd := &cobra.Command{
		Use:   verb + "-policies",
		Short: fmt.Sprintf("Bulk %s policies matching the given filters", verb),
		Long: fmt.Sprintf(`Fetch all Classic API policies and %s those that match all provided
filters (--scope-group, --category, --name-pattern).  Multiple filters are
combined with AND logic.

Without --yes the command prints a preview table and exits without making any
changes.`, verb),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTogglePolicies(cmd, cliCtx, enable, scopeGroup, category, namePattern, yes)
		},
	}

	cmd.Flags().StringVar(&scopeGroup, "scope-group", "", "only policies scoped to this computer group name")
	cmd.Flags().StringVar(&category, "category", "", "only policies in this category (case-insensitive)")
	cmd.Flags().StringVar(&namePattern, "name-pattern", "", "only policies whose name matches this glob (e.g. \"Deploy *\")")
	cmd.Flags().BoolVar(&yes, "yes", false, "execute mutations (default: dry-run preview only)")

	return cmd
}

func runTogglePolicies(
	cmd *cobra.Command,
	cliCtx *generated.CLIContext,
	enable bool,
	scopeGroup, category, namePattern string,
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
		detail map[string]interface{}
	}

	var candidates []policyEntry
	for _, r := range rawPolicies {
		m, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		id := extractID(m)
		if id == "" {
			continue
		}

		// Quick name pre-filter avoids a detail fetch for non-matching names.
		if namePattern != "" {
			listName, _ := m["name"].(string)
			matched, err := matchGlob(namePattern, listName)
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

	// 3. Apply remaining filters (category, scope group) against the full detail.
	var matched []policyEntry
	for _, p := range candidates {
		ok, err := policyMatchesFilters(p.detail, scopeGroup, category, "")
		if err != nil {
			return err
		}
		if ok {
			// Skip policies that are already in the desired state.
			currentlyEnabled, _ := p.detail["enabled"].(bool)
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
	detailMaps := make([]map[string]interface{}, len(matched))
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
	for _, p := range matched {
		name, _ := p.detail["name"].(string)
		if err := doClassicPolicyUpdate(ctx, client, p.id, enable); err != nil {
			bulkLogW(stderr, verb+" policy", name, "ERROR: "+err.Error())
			failCount++
		} else {
			bulkLogW(stderr, verb+" policy", name, "ok")
			successCount++
		}
	}

	_, _ = fmt.Fprintf(stderr, "%sd %d policies; %d failed.\n", capitalize(verb), successCount, failCount)
	if failCount > 0 {
		return fmt.Errorf("%d of %d policy %s operations failed", failCount, successCount+failCount, verb)
	}
	return nil
}

// capitalize returns the string with its first letter uppercased.
func capitalize(s string) string {
	if len(s) == 0 {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
