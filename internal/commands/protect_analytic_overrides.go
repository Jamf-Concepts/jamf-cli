// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/protect"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfprotect-go-sdk/jamfprotect"
)

// severityValues is the SEVERITY enum from the Jamf Protect GraphQL schema.
var severityValues = []string{"High", "Medium", "Low", "Informational"}

// analyticOverrideAction is one entry in a tenant's action override.
type analyticOverrideAction struct {
	Name       string `json:"name" yaml:"name"`
	Parameters string `json:"parameters,omitempty" yaml:"parameters,omitempty"`
}

// analyticOverride is the portable representation of one tenant's customisation
// of a Jamf-managed analytic. Keyed by analytic *name* rather than UUID so the
// document carries across tenants — the same reason analytic sets and plans
// export member names.
type analyticOverride struct {
	Analytic string                   `json:"analytic" yaml:"analytic"`
	Severity string                   `json:"severity,omitempty" yaml:"severity,omitempty"`
	Actions  []analyticOverrideAction `json:"actions,omitempty" yaml:"actions,omitempty"`
}

// analyticOverridesDoc is the export/apply envelope.
type analyticOverridesDoc struct {
	Overrides []analyticOverride `json:"overrides" yaml:"overrides"`
}

// hasOverride reports whether the tenant has customised a Jamf-managed analytic.
func hasOverride(a jamfprotect.Analytic) bool {
	return a.TenantSeverity != "" || len(a.TenantActions) > 0
}

// overrideFromAnalytic projects an Analytic onto the portable override shape.
func overrideFromAnalytic(a jamfprotect.Analytic) analyticOverride {
	o := analyticOverride{Analytic: a.Name, Severity: a.TenantSeverity}
	for _, act := range a.TenantActions {
		o.Actions = append(o.Actions, analyticOverrideAction{Name: act.Name, Parameters: act.Parameters})
	}
	return o
}

// flattenOverride is the table/csv/plain view. It shows Jamf's baseline next to
// the tenant value and the effective result, because `analytics list` reports
// only the baseline severity and so hides an active override.
func flattenOverride(a jamfprotect.Analytic) map[string]any {
	effective := a.Severity
	if a.TenantSeverity != "" {
		effective = a.TenantSeverity
	}
	names := make([]string, 0, len(a.TenantActions))
	for _, act := range a.TenantActions {
		names = append(names, act.Name)
	}
	baseline := make([]string, 0, len(a.AnalyticActions))
	for _, act := range a.AnalyticActions {
		baseline = append(baseline, act.Name)
	}
	effectiveActions := strings.Join(baseline, ", ")
	if len(names) > 0 {
		effectiveActions = strings.Join(names, ", ")
	}
	return map[string]any{
		"name":             a.Name,
		"baselineSeverity": a.Severity,
		"tenantSeverity":   a.TenantSeverity,
		"severity":         effective,
		"tenantActions":    strings.Join(names, ", "),
		"actions":          effectiveActions,
	}
}

// listAnalyticsByName fetches every analytic once and indexes it by name. Used
// instead of the resolver's per-name lookup so bulk apply/export stay at one
// API call rather than one per analytic.
func listAnalyticsByName(ctx context.Context, c registry.ProtectClient) (map[string]jamfprotect.Analytic, error) {
	all, err := c.ListAnalytics(ctx)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]jamfprotect.Analytic, len(all))
	for _, a := range all {
		byName[a.Name] = a
	}
	return byName, nil
}

// parseActionFlag parses a --action value of the form "Name" or "Name=<json>".
func parseActionFlag(raw string) (jamfprotect.AnalyticActionInput, error) {
	name, params, found := strings.Cut(raw, "=")
	name = strings.TrimSpace(name)
	if name == "" {
		return jamfprotect.AnalyticActionInput{}, fmt.Errorf("--action %q: action name is empty", raw)
	}
	if !found || strings.TrimSpace(params) == "" {
		params = "{}"
	}
	return jamfprotect.AnalyticActionInput{Name: name, Parameters: params}, nil
}

// validateOverrideSetFlags rejects a no-op invocation and the two
// set-and-clear contradictions, so 'overrides set' fails before it spends an
// API call or sends a mutation with ambiguous intent.
func validateOverrideSetFlags(severity string, actions []string, clearSeverity, clearActions bool) error {
	if severity == "" && len(actions) == 0 && !clearSeverity && !clearActions {
		return fmt.Errorf("nothing to do: pass --severity, --action, --clear-severity or --clear-actions")
	}
	if severity != "" && clearSeverity {
		return fmt.Errorf("--severity and --clear-severity are mutually exclusive")
	}
	if len(actions) > 0 && clearActions {
		return fmt.Errorf("--action and --clear-actions are mutually exclusive")
	}
	return nil
}

func newProtectAnalyticsOverridesCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "overrides",
		Short: "Manage tenant customisations of Jamf-managed analytics",
		Long: `Manage tenant customisations of Jamf-managed analytics.

Jamf publishes analytics centrally and a tenant may not edit their definitions —
'analytics apply' against one is refused by the server ("This mutation may only
be used for custom analytics"). What a tenant *can* change is an overlay: the
severity it is reported at, and the actions it triggers. These subcommands read
and write that overlay.

The overlay is what makes a Jamf-managed analytic worth capturing at all, since
the definition itself is identical in every tenant. 'export' emits it keyed by
analytic name so it carries to another tenant, and 'apply' replays it.`,
	}

	cmd.AddCommand(newProtectAnalyticsOverridesListCmd(cliCtx))
	cmd.AddCommand(newProtectAnalyticsOverridesGetCmd(cliCtx))
	cmd.AddCommand(newProtectAnalyticsOverridesSetCmd(cliCtx))
	cmd.AddCommand(newProtectAnalyticsOverridesClearCmd(cliCtx))
	cmd.AddCommand(newProtectAnalyticsOverridesExportCmd(cliCtx))
	cmd.AddCommand(newProtectAnalyticsOverridesApplyCmd(cliCtx))

	return cmd
}

func newProtectAnalyticsOverridesListCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var all bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List analytics whose severity or actions this tenant has overridden",
		RunE: func(cmd *cobra.Command, _ []string) error {
			analytics, err := cliCtx.ProtectClient.ListAnalytics(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(analytics))
			for _, a := range analytics {
				if !all && !hasOverride(a) {
					continue
				}
				rows = append(rows, flattenOverride(a))
			}
			sort.Slice(rows, func(i, j int) bool {
				return rows[i]["name"].(string) < rows[j]["name"].(string)
			})
			return protect.PrintList(cliCtx.Output, rows)
		},
	}

	cmd.Flags().BoolVar(&all, "all", false, "Include analytics with no override")

	return cmd
}

func newProtectAnalyticsOverridesGetCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get <name>",
		Short: "Show the tenant override for one analytic",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			byName, err := listAnalyticsByName(cmd.Context(), cliCtx.ProtectClient)
			if err != nil {
				return err
			}
			a, ok := byName[args[0]]
			if !ok {
				return fmt.Errorf("analytic %q not found", args[0])
			}
			if !hasOverride(a) {
				fmt.Fprintf(os.Stderr, "Analytic %q has no tenant override (baseline severity %s)\n", a.Name, a.Severity)
			}
			return printResult(cliCtx.Output, overrideFromAnalytic(a), flattenOverride(a))
		},
	}
}

func newProtectAnalyticsOverridesSetCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		severity      string
		actions       []string
		clearSeverity bool
		clearActions  bool
	)

	cmd := &cobra.Command{
		Use:   "set <name>",
		Short: "Set the severity or actions override for a Jamf-managed analytic",
		Long: `Set the severity or actions override for a Jamf-managed analytic.

Only the flags you pass are sent — an omitted flag leaves that half of the
overlay untouched. Use --clear-severity or --clear-actions to remove one half
explicitly, or 'overrides clear' to remove both.

--action may be repeated and takes "Name" or "Name=<json parameters>", e.g.
  --action Report --action 'Alert={"notify":true}'`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			if err := validateOverrideSetFlags(severity, actions, clearSeverity, clearActions); err != nil {
				return err
			}

			byName, err := listAnalyticsByName(ctx, cliCtx.ProtectClient)
			if err != nil {
				return err
			}
			a, ok := byName[args[0]]
			if !ok {
				return fmt.Errorf("analytic %q not found", args[0])
			}
			if !a.Jamf {
				return fmt.Errorf("analytic %q is a custom analytic — edit it with 'protect analytics apply' instead; overrides apply only to Jamf-managed analytics", a.Name)
			}

			input := jamfprotect.InternalAnalyticInput{
				TenantSeverityNull: clearSeverity,
				TenantActionsNull:  clearActions,
			}
			if severity != "" {
				input.TenantSeverity = &severity
			}
			for _, raw := range actions {
				act, err := parseActionFlag(raw)
				if err != nil {
					return err
				}
				input.TenantActions = append(input.TenantActions, act)
			}

			result, err := cliCtx.ProtectClient.UpdateInternalAnalytic(ctx, a.UUID, input)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Updated override for analytic %q\n", a.Name)
			return printResult(cliCtx.Output, overrideFromAnalytic(result), flattenOverride(result))
		},
	}

	cmd.Flags().StringVar(&severity, "severity", "", "Tenant severity override (High, Medium, Low, Informational)")
	cmd.Flags().StringArrayVar(&actions, "action", nil, "Tenant action override as Name or Name=<json> (repeatable)")
	cmd.Flags().BoolVar(&clearSeverity, "clear-severity", false, "Remove the severity override, restoring Jamf's baseline")
	cmd.Flags().BoolVar(&clearActions, "clear-actions", false, "Remove the actions override, restoring Jamf's baseline")

	_ = cmd.RegisterFlagCompletionFunc("severity", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return severityValues, cobra.ShellCompDirectiveNoFileComp
	})

	return cmd
}

func newProtectAnalyticsOverridesClearCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:         "clear <name>",
		Short:       "Remove the tenant override, restoring Jamf's baseline",
		Annotations: map[string]string{"jamf:destructive": "true"},
		Args:        cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			byName, err := listAnalyticsByName(ctx, cliCtx.ProtectClient)
			if err != nil {
				return err
			}
			a, ok := byName[args[0]]
			if !ok {
				return fmt.Errorf("analytic %q not found", args[0])
			}
			if !hasOverride(a) {
				fmt.Fprintf(os.Stderr, "Analytic %q has no tenant override — nothing to clear\n", a.Name)
				return nil
			}

			proceed, err := confirmAction("clear the override for", a.Name, yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			result, err := cliCtx.ProtectClient.UpdateInternalAnalytic(ctx, a.UUID, jamfprotect.InternalAnalyticInput{
				TenantSeverityNull: true,
				TenantActionsNull:  true,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Cleared override for analytic %q\n", a.Name)
			return printResult(cliCtx.Output, overrideFromAnalytic(result), flattenOverride(result))
		},
	}

	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")

	return cmd
}

func newProtectAnalyticsOverridesExportCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "export",
		Short: "Export every tenant override as a portable, name-keyed document",
		Long: `Export every tenant override as a portable, name-keyed document.

Pipe the result into 'overrides apply' against another tenant to reproduce the
customisations. Analytics with no override are omitted.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			analytics, err := cliCtx.ProtectClient.ListAnalytics(cmd.Context())
			if err != nil {
				return err
			}
			doc := analyticOverridesDoc{Overrides: []analyticOverride{}}
			for _, a := range analytics {
				if hasOverride(a) {
					doc.Overrides = append(doc.Overrides, overrideFromAnalytic(a))
				}
			}
			sort.Slice(doc.Overrides, func(i, j int) bool {
				return doc.Overrides[i].Analytic < doc.Overrides[j].Analytic
			})
			return printExport(doc)
		},
	}
}

func newProtectAnalyticsOverridesApplyCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		fromFile string
		yes      bool
		scaffold bool
	)

	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply a tenant override document produced by 'overrides export'",
		Long: `Apply a tenant override document produced by 'overrides export'.

Each entry is matched to the target tenant by analytic name, so a document
exported from one tenant applies to another. Entries naming an analytic that is
absent, or that is custom rather than Jamf-managed, are reported and skipped —
the rest still apply. An entry the server refuses is reported and the run
continues, so the summary always says how many landed; the command exits
non-zero if any failed.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if scaffold {
				return printExport(analyticOverridesDoc{Overrides: []analyticOverride{{
					Analytic: "BlazingKeylogger",
					Severity: "Low",
					Actions:  []analyticOverrideAction{{Name: "Report", Parameters: "{}"}},
				}}})
			}

			ctx := cmd.Context()

			data, err := readInput(fromFile)
			if err != nil {
				return err
			}
			var doc analyticOverridesDoc
			if err := unmarshalInput(data, &doc); err != nil {
				return fmt.Errorf("parsing input: %w", err)
			}
			if len(doc.Overrides) == 0 {
				return fmt.Errorf("input contains no overrides")
			}

			byName, err := listAnalyticsByName(ctx, cliCtx.ProtectClient)
			if err != nil {
				return err
			}

			proceed, err := confirmAction(fmt.Sprintf("apply %d analytic override(s) to", len(doc.Overrides)), "this tenant", yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			var applied, skipped, failed int
			for _, o := range doc.Overrides {
				a, ok := byName[o.Analytic]
				if !ok {
					fmt.Fprintf(os.Stderr, "Skipped %q: not present in this tenant\n", o.Analytic)
					skipped++
					continue
				}
				if !a.Jamf {
					fmt.Fprintf(os.Stderr, "Skipped %q: custom analytic, not Jamf-managed\n", o.Analytic)
					skipped++
					continue
				}

				// An absent half means "no override", so clear it rather than
				// leaving whatever the target happened to have — otherwise
				// applying the same document twice is not idempotent.
				input := jamfprotect.InternalAnalyticInput{
					TenantSeverityNull: o.Severity == "",
					TenantActionsNull:  len(o.Actions) == 0,
				}
				if o.Severity != "" {
					input.TenantSeverity = &o.Severity
				}
				for _, act := range o.Actions {
					params := act.Parameters
					if params == "" {
						params = "{}"
					}
					input.TenantActions = append(input.TenantActions, jamfprotect.AnalyticActionInput{
						Name: act.Name, Parameters: params,
					})
				}

				if _, err := cliCtx.ProtectClient.UpdateInternalAnalytic(ctx, a.UUID, input); err != nil {
					// Report and continue rather than abandoning the document at
					// the first failure: aborting left the operator knowing only
					// which entry failed, with no indication of how many had
					// already been written, so a retry was unsafe to reason
					// about. 'protect restore' takes the same approach.
					fmt.Fprintf(os.Stderr, "FAILED %q: %v\n", o.Analytic, err)
					failed++
					continue
				}
				applied++
			}

			fmt.Fprintf(os.Stderr, "Applied %d override(s), skipped %d, %d failed\n", applied, skipped, failed)
			if failed > 0 {
				return fmt.Errorf("%d override(s) failed to apply", failed)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON or YAML input file (or pipe to stdin)")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&scaffold, "scaffold", false, "Print an example document and exit")

	return cmd
}
