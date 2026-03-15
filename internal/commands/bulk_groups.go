package commands

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jamf/jamfpro-cli/internal/commands/generated"
)

// newAddToGroupCmd creates the "bulk add-to-group" subcommand.
func newAddToGroupCmd(cliCtx *generated.CLIContext) *cobra.Command {
	return newGroupMutationCmd(cliCtx, true)
}

// newRemoveFromGroupCmd creates the "bulk remove-from-group" subcommand.
func newRemoveFromGroupCmd(cliCtx *generated.CLIContext) *cobra.Command {
	return newGroupMutationCmd(cliCtx, false)
}

// newGroupMutationCmd is the shared builder for add-to-group / remove-from-group.
func newGroupMutationCmd(cliCtx *generated.CLIContext, add bool) *cobra.Command {
	var (
		groupName string
		fromFile  string
		fromGroup string
		yes       bool
	)

	verb := "add-to-group"
	shortVerb := "Add computers to"
	if !add {
		verb = "remove-from-group"
		shortVerb = "Remove computers from"
	}

	cmd := &cobra.Command{
		Use:   verb,
		Short: fmt.Sprintf("%s a static computer group", shortVerb),
		Long: fmt.Sprintf(`%s a static computer group using a list of computer IDs/serials
from a file (--from-file) or members of another computer group (--group).

--target-group   Name of the static group to modify (required).
--from-file      Plain-text file: one computer ID or serial per line.
--group          Source computer group whose members are the targets.
--from-file and --group are mutually exclusive.

Without --yes the command prints a preview table and exits without making any
changes.`, shortVerb),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGroupMutation(cmd, cliCtx, add, groupName, fromFile, fromGroup, yes)
		},
	}

	cmd.Flags().StringVar(&groupName, "target-group", "", "static computer group to modify (required)")
	cmd.Flags().StringVar(&fromFile, "from-file", "", "file containing one computer ID/serial per line")
	cmd.Flags().StringVar(&fromGroup, "group", "", "source computer group whose members become the targets")
	cmd.Flags().BoolVar(&yes, "yes", false, "execute mutations (default: dry-run preview only)")

	_ = cmd.MarkFlagRequired("target-group")

	return cmd
}

func runGroupMutation(
	cmd *cobra.Command,
	cliCtx *generated.CLIContext,
	add bool,
	groupName, fromFile, fromGroup string,
	yes bool,
) error {
	ctx := cmd.Context()
	client := cliCtx.Client
	stderr := cmd.ErrOrStderr()

	verb := "add to"
	if !add {
		verb = "remove from"
	}

	// 1. Resolve targets (from file or group membership).
	targets, err := resolveComputerTargets(ctx, client, fromFile, fromGroup)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		_, _ = fmt.Fprintf(stderr, "No target computers found.\n")
		return nil
	}

	// 2. Locate the target static group.
	groupID, err := lookupStaticGroupID(ctx, client, groupName)
	if err != nil {
		return err
	}

	// 3. Build preview rows.
	previewRows := make([]map[string]interface{}, len(targets))
	for i, t := range targets {
		previewRows[i] = map[string]interface{}{
			"computer_id":   t["id"],
			"computer_name": t["name"],
			"group":         groupName,
			"action":        verb,
		}
	}

	// 4. Dry-run: print preview table to stdout and log intent to stderr.
	if !yes {
		_, _ = fmt.Fprintf(stderr, "[dry-run] Would %s %d computers %s group %q (use --yes to apply):\n",
			verb, len(targets), directionWord(add), groupName)
		bulkPreviewTable(previewRows)
		return nil
	}

	// 5. Execute mutations.
	_, _ = fmt.Fprintf(stderr, "Applying group membership changes to %d computers...\n", len(targets))

	var successCount, failCount int
	for _, t := range targets {
		if err := applyStaticGroupMutation(ctx, client, groupID, t["id"], add); err != nil {
			bulkLogW(stderr, verb+" group", t["name"], "ERROR: "+err.Error())
			failCount++
		} else {
			bulkLogW(stderr, verb+" group", t["name"], "ok")
			successCount++
		}
	}

	_, _ = fmt.Fprintf(stderr, "Group update complete: %d succeeded, %d failed.\n", successCount, failCount)
	return nil
}

// directionWord returns "to" or "from" for logging messages.
func directionWord(add bool) string {
	if add {
		return "to"
	}
	return "from"
}
