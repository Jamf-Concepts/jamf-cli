// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/pickone"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamf-cli/internal/resolve"
)

// policyEntry is one selected policy and its full Classic detail.
type policyEntry struct {
	id     string
	detail map[string]any
}

func (p policyEntry) general() map[string]any {
	g, _ := p.detail["general"].(map[string]any)
	return g
}

func (p policyEntry) name() string {
	n, _ := p.general()["name"].(string)
	return n
}

func (p policyEntry) enabled() bool {
	e, _ := p.general()["enabled"].(bool)
	return e
}

// policyFilterFlags holds the filter flags shared by `pro classic-policies
// enable|disable` and the deprecated `pro bulk enable|disable-policies`.
type policyFilterFlags struct {
	namePattern        string
	category           string
	allComputers       bool
	scopeGroups        []string
	scopeBuildings     []string
	scopeDepartments   []string
	limitNetSegments   []string
	limitUserGroups    []string
	excludeGroups      []string
	excludeBuildings   []string
	excludeDepartments []string
}

// policyFilterFlagNames lists every filter flag, for mutual exclusion with the
// explicit target forms.
var policyFilterFlagNames = []string{
	"name-pattern", "category", "all-computers",
	"scope-group", "scope-building", "scope-department",
	"limit-network-segment", "limit-user-group",
	"exclude-group", "exclude-building", "exclude-department",
}

func (pf *policyFilterFlags) register(cmd *cobra.Command) {
	cmd.Flags().StringVar(&pf.namePattern, "name-pattern", "", "only policies whose name matches this glob (e.g. \"Deploy *\")")
	cmd.Flags().StringVar(&pf.category, "category", "", "only policies in this category (case-insensitive)")

	cmd.Flags().StringArrayVar(&pf.scopeGroups, "scope-group", nil, "only policies scoped to this computer group (repeatable)")
	cmd.Flags().StringArrayVar(&pf.scopeBuildings, "scope-building", nil, "only policies scoped to this building (repeatable)")
	cmd.Flags().StringArrayVar(&pf.scopeDepartments, "scope-department", nil, "only policies scoped to this department (repeatable)")
	cmd.Flags().BoolVar(&pf.allComputers, "all-computers", false, "only policies scoped to all computers")

	cmd.Flags().StringArrayVar(&pf.limitNetSegments, "limit-network-segment", nil, "only policies limited to this network segment (repeatable)")
	cmd.Flags().StringArrayVar(&pf.limitUserGroups, "limit-user-group", nil, "only policies limited to this user group (repeatable)")

	cmd.Flags().StringArrayVar(&pf.excludeGroups, "exclude-group", nil, "only policies excluding this computer group (repeatable)")
	cmd.Flags().StringArrayVar(&pf.excludeBuildings, "exclude-building", nil, "only policies excluding this building (repeatable)")
	cmd.Flags().StringArrayVar(&pf.excludeDepartments, "exclude-department", nil, "only policies excluding this department (repeatable)")
}

// filters builds the matcher input; --all-computers filters only when given.
func (pf *policyFilterFlags) filters(cmd *cobra.Command) policyBulkFilters {
	f := policyBulkFilters{
		namePattern:        pf.namePattern,
		category:           pf.category,
		scopeGroups:        pf.scopeGroups,
		scopeBuildings:     pf.scopeBuildings,
		scopeDepartments:   pf.scopeDepartments,
		limitNetSegments:   pf.limitNetSegments,
		limitUserGroups:    pf.limitUserGroups,
		excludeGroups:      pf.excludeGroups,
		excludeBuildings:   pf.excludeBuildings,
		excludeDepartments: pf.excludeDepartments,
	}
	if cmd.Flags().Changed("all-computers") {
		f.allComputers = &pf.allComputers
	}
	return f
}

// anyFilterSet reports whether the invocation selects policies by filter.
func anyFilterSet(cmd *cobra.Command) bool {
	for _, n := range policyFilterFlagNames {
		if cmd.Flags().Changed(n) {
			return true
		}
	}
	return false
}

const policyFilterHelp = `Filters (combined with AND; a repeatable flag's values with OR):

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
    --exclude-department exclusion department (repeatable)`

func newClassicPolicyToggleCmd(cliCtx *registry.CLIContext, enable bool) *cobra.Command {
	var (
		pf       policyFilterFlags
		name     string
		fromFile string
		yes      bool
	)
	verb := "enable"
	if !enable {
		verb = "disable"
	}

	cmd := &cobra.Command{
		Use:   verb + " [<id>]",
		Short: fmt.Sprintf("%s one or more policies", capitalize(verb)),
		Long: fmt.Sprintf(`%s policies, chosen one of three ways:

  <id> or --name   one policy
  --from-file      a file listing policy IDs or names, one per line (# comments ignored)
  filters          every policy matching the filters below

A policy already %sd is reported and left alone. A name matching no policy, or
more than one, is reported and skipped and counts as a failure in the summary
and exit code (use --allow-partial-failure to tolerate it).

The write is a minimal Classic PUT carrying only <general><enabled>; no other
field of the policy is sent or changed.

With --from-file or filters the command prints a preview and changes nothing
unless --yes is given. -n/--dry-run previews any form.

%s

Output: the preview table, or one row per policy with its result, on stdout;
progress on stderr.`, capitalize(verb), verb, policyFilterHelp),
		Example: fmt.Sprintf(`  jamf-cli pro classic-policies %s 42
  jamf-cli pro classic-policies %s --name "Deploy Chrome"
  jamf-cli pro classic-policies %s --category "Testing" --scope-group "Lab Macs" --yes
  jamf-cli pro classic-policies %s --from-file policies.txt -n`, verb, verb, verb, verb),
		Annotations: map[string]string{annotationAPI: apiProClassic},
		Args:        cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			stderr := cmd.ErrOrStderr()
			filtered := anyFilterSet(cmd)

			forms := 0
			for _, set := range []bool{len(args) == 1, name != "", fromFile != "", filtered} {
				if set {
					forms++
				}
			}
			switch {
			case forms == 0:
				return fmt.Errorf("choose policies with an <id>, --name, --from-file, or filters")
			case forms > 1:
				return fmt.Errorf("pass one of <id>, --name, --from-file, or filters, not a combination")
			}

			var (
				selected   []policyEntry
				unresolved int
				err        error
			)
			switch {
			case filtered:
				selected, err = selectPoliciesByFilter(ctx, cliCtx.Client, stderr, pf.filters(cmd))
			case fromFile != "":
				var entries []string
				entries, err = readPolicyEntries(fromFile)
				if err == nil {
					selected, unresolved, err = selectPoliciesByTarget(ctx, cliCtx.Client, stderr, entries)
				}
			case name != "":
				selected, unresolved, err = selectPoliciesByTarget(ctx, cliCtx.Client, stderr, []string{name})
			default:
				if !resolve.IsNumericID(args[0]) {
					return fmt.Errorf("%q is not a policy ID; pass a name with --name", args[0])
				}
				selected, unresolved, err = selectPoliciesByTarget(ctx, cliCtx.Client, stderr, []string{args[0]})
			}
			if err != nil {
				return err
			}
			if !filtered && len(selected) == 0 {
				return finishBatch(stderr, "policy "+verb+" operations", 0, unresolved, nil)
			}
			list := filtered || fromFile != ""
			return togglePolicies(cmd, cliCtx, enable, selected, unresolved, policyToggleMode{
				preview:      dryRun || (list && !yes),
				dryRun:       dryRun,
				printResults: true,
			})
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "policy name (instead of the positional ID)")
	cmd.Flags().StringVar(&fromFile, "from-file", "", "file listing policy IDs or names, one per line (# comments ignored)")
	cmd.Flags().BoolVar(&yes, "yes", false, "apply a --from-file or filtered change (default: preview only)")
	pf.register(cmd)

	return markGatewayCoverage(cmd, "PUT", "/JSSResource/policies/id/{id}")
}

// readPolicyEntries reads policy IDs or names from a file, one per line,
// skipping blanks and #-comments.
func readPolicyEntries(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading --from-file: %w", err)
	}
	var entries []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		entries = append(entries, line)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("--from-file %s contains no entries", path)
	}
	return entries, nil
}

// policyRef is one entry of the Classic policy listing.
type policyRef struct {
	id   string
	name string
}

// selectPoliciesByTarget resolves IDs or names to policies and reads each one's
// detail. The listing is read once, whatever the number of names: a numeric
// entry is an ID, anything else must name exactly one policy — a unique exact
// match, else a unique case-insensitive one — since Classic names are not
// unique. An entry that resolves to nothing warns and counts as unresolved.
func selectPoliciesByTarget(ctx context.Context, client registry.HTTPClient, stderr io.Writer, entries []string) ([]policyEntry, int, error) {
	var refs []policyRef
	needList := false
	for _, e := range entries {
		if !resolve.IsNumericID(e) {
			needList = true
			break
		}
	}
	if needList {
		raw, err := FetchClassicList(ctx, client, "/JSSResource/policies", "policies")
		if err != nil {
			return nil, 0, fmt.Errorf("listing policies: %w", err)
		}
		for _, r := range raw {
			if m, ok := r.(map[string]any); ok {
				refs = append(refs, policyRef{id: extractID(m), name: fmt.Sprint(m["name"])})
			}
		}
	}

	var selected []policyEntry
	unresolved := 0
	seen := map[string]bool{}
	for _, e := range entries {
		id := e
		if !resolve.IsNumericID(e) {
			ref, candidates, err := pickone.One(refs, e,
				pickone.Exact(func(r policyRef) string { return r.name }),
				pickone.Fold(func(r policyRef) string { return r.name }))
			switch {
			case errors.Is(err, pickone.ErrNone):
				_, _ = fmt.Fprintf(stderr, "  warning: no policy named %q\n", e)
				unresolved++
				continue
			case errors.Is(err, pickone.ErrAmbiguous):
				ids := make([]string, len(candidates))
				for i, c := range candidates {
					ids[i] = c.id
				}
				_, _ = fmt.Fprintf(stderr, "  warning: %q names %d policies (ids %s); pass the ID of the one you mean\n",
					e, len(candidates), strings.Join(ids, ", "))
				unresolved++
				continue
			}
			id = ref.id
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		detail, err := fetchClassicPolicyDetail(ctx, client, id)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "  warning: could not read policy %s: %v\n", id, err)
			unresolved++
			continue
		}
		selected = append(selected, policyEntry{id: id, detail: detail})
	}
	return selected, unresolved, nil
}

// selectPoliciesByFilter lists every policy, reads the detail of each one the
// name pattern does not already exclude, and keeps those matching every filter.
func selectPoliciesByFilter(ctx context.Context, client registry.HTTPClient, stderr io.Writer, f policyBulkFilters) ([]policyEntry, error) {
	rawPolicies, err := FetchClassicList(ctx, client, "/JSSResource/policies", "policies")
	if err != nil {
		return nil, fmt.Errorf("listing policies: %w", err)
	}

	var matched []policyEntry
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
			ok, err := matchGlob(f.namePattern, listName)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
		}

		detail, err := fetchClassicPolicyDetail(ctx, client, id)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "WARNING: failed to fetch policy id=%s: %v\n", id, err)
			continue
		}
		match, err := policyMatchesFilters(detail, f)
		if err != nil {
			return nil, err
		}
		if match {
			matched = append(matched, policyEntry{id: id, detail: detail})
		}
	}
	return matched, nil
}

// policyToggleMode is how togglePolicies reports and whether it writes.
type policyToggleMode struct {
	preview      bool // print the preview and stop
	dryRun       bool // the preview is a -n preview rather than a missing --yes
	printResults bool // print one result row per policy on stdout
}

// togglePolicies sets enabled on every selected policy not already in that
// state, one minimal PUT each, and maps the tally to the exit code.
// unresolved entries count as failures.
func togglePolicies(cmd *cobra.Command, cliCtx *registry.CLIContext, enable bool, selected []policyEntry, unresolved int, mode policyToggleMode) error {
	stderr := cmd.ErrOrStderr()
	verb := "enable"
	if !enable {
		verb = "disable"
	}

	var change, unchanged []policyEntry
	for _, p := range selected {
		if p.enabled() == enable {
			unchanged = append(unchanged, p)
		} else {
			change = append(change, p)
		}
	}
	if len(unchanged) > 0 {
		_, _ = fmt.Fprintf(stderr, "%d polic%s already %sd; left alone.\n", len(unchanged), pluralY(len(unchanged)), verb)
	}

	if len(change) == 0 {
		_, _ = fmt.Fprintf(stderr, "No policies require changes.\n")
		if mode.printResults {
			if err := printRows(cliCtx, policyResultRows(nil, unchanged, enable, nil)); err != nil {
				return err
			}
		}
		return finishBatch(stderr, fmt.Sprintf("policy %s operations", verb), len(unchanged), unresolved, nil)
	}

	if mode.preview {
		tail := ""
		if !mode.dryRun {
			tail = " (use --yes to apply)"
		}
		_, _ = fmt.Fprintf(stderr, "[dry-run] Would %s %d polic%s%s:\n", verb, len(change), pluralY(len(change)), tail)
		details := make([]map[string]any, len(change))
		for i, p := range change {
			details[i] = p.detail
		}
		bulkPreviewTable(bulkPolicyRows(details))
		return nil
	}

	_, _ = fmt.Fprintf(stderr, "%sing %d polic%s...\n", capitalize(strings.TrimSuffix(verb, "e")), len(change), pluralY(len(change)))

	failed := map[string]error{}
	var firstErr error
	for _, p := range change {
		if err := doClassicPolicyUpdate(cmd.Context(), cliCtx.Client, p.id, enable); err != nil {
			_, _ = fmt.Fprintf(stderr, "  %-40s ERROR: %v\n", policyDesc(p), err)
			failed[p.id] = err
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		_, _ = fmt.Fprintf(stderr, "  %-40s ok\n", policyDesc(p))
	}

	succeeded := len(change) - len(failed)
	_, _ = fmt.Fprintf(stderr, "%sd %d polic%s; %d failed%s.\n", capitalize(verb), succeeded, pluralY(succeeded), len(failed)+unresolved, unresolvedNote(unresolved))
	if mode.printResults {
		if err := printRows(cliCtx, policyResultRows(change, unchanged, enable, failed)); err != nil {
			return err
		}
	}
	return finishBatch(stderr, fmt.Sprintf("policy %s operations", verb), succeeded+len(unchanged), len(failed)+unresolved, firstErr)
}

// policyResultRows is one row per policy: what was done to it, or why not.
func policyResultRows(changed, unchanged []policyEntry, enable bool, failed map[string]error) []map[string]any {
	done, already := "enabled", "already enabled"
	if !enable {
		done, already = "disabled", "already disabled"
	}
	rows := make([]map[string]any, 0, len(changed)+len(unchanged))
	add := func(p policyEntry, result string) {
		cat, _ := p.general()["category"].(map[string]any)
		catName, _ := cat["name"].(string)
		rows = append(rows, map[string]any{"id": p.id, "name": p.name(), "category": catName, "result": result})
	}
	for _, p := range changed {
		if err, ok := failed[p.id]; ok {
			add(p, "failed: "+err.Error())
		} else {
			add(p, done)
		}
	}
	for _, p := range unchanged {
		add(p, already)
	}
	return rows
}

// policyDesc names a policy for a progress line.
func policyDesc(p policyEntry) string {
	return fmt.Sprintf("%s (id %s)", p.name(), p.id)
}

func pluralY(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}
