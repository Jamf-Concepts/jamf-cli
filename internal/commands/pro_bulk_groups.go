// Copyright 2026, Jamf Software LLC

package commands

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// newAddToGroupCmd creates the "bulk add-to-group" subcommand.
func newAddToGroupCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return newGroupMutationCmd(cliCtx, true)
}

// newRemoveFromGroupCmd creates the "bulk remove-from-group" subcommand.
func newRemoveFromGroupCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return newGroupMutationCmd(cliCtx, false)
}

// newGroupMutationCmd is the shared builder for add-to-group / remove-from-group.
func newGroupMutationCmd(cliCtx *registry.CLIContext, add bool) *cobra.Command {
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

Serials in --from-file are resolved to computer IDs first. A line that matches
no computer is reported and skipped, and counts as a failure in the summary and
exit code (use --allow-partial-failure to tolerate it); if no line resolves at
all, or the file holds no entries, the command fails without changing anything.

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
	cliCtx *registry.CLIContext,
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
	targets, unresolved, err := resolveComputerTargets(ctx, client, fromFile, fromGroup)
	if err != nil {
		return err
	}
	warnUnresolvedTargets(stderr, unresolved)
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
	previewRows := make([]map[string]any, len(targets))
	for i, t := range targets {
		previewRows[i] = map[string]any{
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

	// Entries dropped during resolution count as failures: "some lines in my
	// file didn't work" is the same outcome whether the line failed to resolve
	// or failed to mutate, so --allow-partial-failure and exit 7 govern both.
	successCount, failCount := 0, unresolved
	var firstErr error
	for _, t := range targets {
		if err := applyStaticGroupMutation(ctx, client, groupID, t["id"], add); err != nil {
			bulkLogW(stderr, verb+" group", t["name"], "ERROR: "+err.Error())
			if firstErr == nil {
				firstErr = err
			}
			failCount++
		} else {
			bulkLogW(stderr, verb+" group", t["name"], "ok")
			successCount++
		}
	}

	_, _ = fmt.Fprintf(stderr, "Group update complete: %d succeeded, %d failed%s.\n",
		successCount, failCount, unresolvedNote(unresolved))
	return finishBatch(stderr, "group membership operations", successCount, failCount, firstErr)
}

// directionWord returns "to" or "from" for logging messages.
func directionWord(add bool) string {
	if add {
		return "to"
	}
	return "from"
}
