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

// newTogglePoliciesCmd is the shared builder for enable-policies /
// disable-policies, which select by filter only and always preview without
// --yes. The selection and the write are the ones `pro classic-policies
// enable|disable` use.
func newTogglePoliciesCmd(cliCtx *registry.CLIContext, enable bool) *cobra.Command {
	var (
		pf  policyFilterFlags
		yes bool
	)

	verb := "enable"
	if !enable {
		verb = "disable"
	}

	cmd := &cobra.Command{
		Use:   verb + "-policies",
		Short: fmt.Sprintf("Bulk %s policies matching the given filters", verb),
		Long: fmt.Sprintf(`Fetch all Classic API policies and %s those that match all provided
filters.

Without --yes, or with -n/--dry-run, the command prints a preview table and
exits without making any changes.

%s`, verb, policyFilterHelp),
		Deprecated: fmt.Sprintf("use `jamf-cli pro classic-policies %s` (same filters, plus <id>, --name and --from-file); `pro bulk %s-policies` will be removed in a future release", verb, verb),
		RunE: func(cmd *cobra.Command, args []string) error {
			selected, err := selectPoliciesByFilter(cmd.Context(), cliCtx.Client, cmd.ErrOrStderr(), pf.filters(cmd))
			if err != nil {
				return err
			}
			return togglePolicies(cmd, cliCtx, enable, selected, 0, policyToggleMode{
				preview: dryRun || !yes,
				dryRun:  dryRun,
			})
		},
	}

	pf.register(cmd)
	cmd.Flags().BoolVar(&yes, "yes", false, "execute mutations (default: dry-run preview only)")

	return cmd
}

// capitalize returns the string with its first letter uppercased.
func capitalize(s string) string {
	if len(s) == 0 {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
