package scope

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// NewScopeCmd creates the "scope" subcommand group with get, add, and remove
// subcommands for the given Classic API resource.
func NewScopeCmd(ctx *registry.CLIContext, res Resource) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scope",
		Short: "View and modify scope",
	}

	cmd.AddCommand(newScopeGetCmd(ctx, res))
	cmd.AddCommand(newScopeAddCmd(ctx, res))
	cmd.AddCommand(newScopeRemoveCmd(ctx, res))

	return cmd
}

func outputFormat(cmd *cobra.Command) string {
	if f := cmd.Flag("output"); f != nil {
		return f.Value.String()
	}
	return "json"
}

func newScopeGetCmd(ctx *registry.CLIContext, res Resource) *cobra.Command {
	return &cobra.Command{
		Use:   "get <name>",
		Short: "Display the current scope",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, s, err := FetchScope(cmd.Context(), ctx.Client, res, args[0])
			if err != nil {
				return err
			}
			return OutputScope(ctx.Output, s, res.SingularKey, outputFormat(cmd))
		},
	}
}

func newScopeAddCmd(ctx *registry.CLIContext, res Resource) *cobra.Command {
	var section string

	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Add an item to the scope",
		Example: `  # Add computer group to targets (default section)
  scope add "Deploy Chrome" --computer-group "All Managed Clients"

  # Add building to exclusions
  scope add "Deploy Chrome" --section exclusion --building "London"

  # Add network segment to limitations
  scope add "Deploy Chrome" --section limitation --network-segment "Guest"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := DetermineScopeTarget(cmd)
			if err != nil {
				return err
			}

			if err := ValidateScopeCombination(res.SingularKey, section, target.FlagName); err != nil {
				return err
			}

			id, s, err := FetchScope(cmd.Context(), ctx.Client, res, args[0])
			if err != nil {
				return err
			}

			if !AddToScope(s, res.SingularKey, section, target.FlagName, target.Name) {
				fmt.Fprintf(os.Stderr, "%s %q already in %s scope of %q\n",
					target.FlagName, target.Name, section, args[0])
				return nil
			}

			if err := PutScope(cmd.Context(), ctx.Client, res, id, s); err != nil {
				return err
			}

			fmt.Fprintf(os.Stderr, "Added %s %q to %s scope of %q\n",
				target.FlagName, target.Name, section, args[0])
			return OutputScope(ctx.Output, s, res.SingularKey, outputFormat(cmd))
		},
	}

	AddScopeFlags(cmd, &section)
	return cmd
}

func newScopeRemoveCmd(ctx *registry.CLIContext, res Resource) *cobra.Command {
	var section string

	cmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove an item from the scope",
		Example: `  # Remove computer group from targets
  scope remove "Deploy Chrome" --computer-group "Test Group"

  # Remove building from exclusions
  scope remove "Deploy Chrome" --section exclusion --building "London"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := DetermineScopeTarget(cmd)
			if err != nil {
				return err
			}

			if err := ValidateScopeCombination(res.SingularKey, section, target.FlagName); err != nil {
				return err
			}

			id, s, err := FetchScope(cmd.Context(), ctx.Client, res, args[0])
			if err != nil {
				return err
			}

			if !RemoveFromScope(s, res.SingularKey, section, target.FlagName, target.Name) {
				fmt.Fprintf(os.Stderr, "%s %q not found in %s scope of %q\n",
					target.FlagName, target.Name, section, args[0])
				return nil
			}

			if err := PutScope(cmd.Context(), ctx.Client, res, id, s); err != nil {
				return err
			}

			fmt.Fprintf(os.Stderr, "Removed %s %q from %s scope of %q\n",
				target.FlagName, target.Name, section, args[0])
			return OutputScope(ctx.Output, s, res.SingularKey, outputFormat(cmd))
		},
	}

	AddScopeFlags(cmd, &section)
	return cmd
}
