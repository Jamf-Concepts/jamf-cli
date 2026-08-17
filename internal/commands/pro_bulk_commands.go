// Copyright 2026, Jamf Software LLC

package commands

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/cooldown"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// validMDMCommands is the set of MDM commands accepted by send-command.
// Destructive commands (EraseDevice, DeviceLock) are a subset of this set.
var validMDMCommands = map[string]bool{
	"DeviceInformation":               true,
	"ScheduleOSUpdate":                true,
	"Settings":                        true,
	"UpdateInventory":                 true,
	"BlankPush":                       true,
	"UnlockUserAccount":               true,
	"DeleteUser":                      true,
	"EnableRemoteDesktop":             true,
	"DisableRemoteDesktop":            true,
	"RedeployJamfManagementFramework": true,
	"DeviceLock":                      true,
	"EraseDevice":                     true,
}

func newSendCommandCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		command            string
		fromFile           string
		fromGroup          string
		yes                bool
		confirmDestructive bool
	)

	cmd := &cobra.Command{
		Use:   "send-command",
		Short: "Send an MDM command to a set of computers",
		Long: `Send a Classic API MDM command to multiple computers.

Targets are specified via --from-file (one computer ID or serial per line) or
--group (all members of a computer group).

Serials in --from-file are resolved to computer IDs first. A line that matches
no computer is reported and skipped, and counts as a failure in the summary and
exit code (use --allow-partial-failure to tolerate it); if no line resolves at
all, or the file holds no entries, the command fails without sending anything.

Destructive commands (EraseDevice, DeviceLock) require both --yes and
--confirm-destructive.

Without --yes the command prints a preview table and exits without making any
changes.

Available commands: BlankPush, DeviceInformation, DeviceLock, DeleteUser,
  DisableRemoteDesktop, EnableRemoteDesktop, EraseDevice,
  RedeployJamfManagementFramework, ScheduleOSUpdate, Settings,
  UnlockUserAccount, UpdateInventory`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSendCommand(cmd, cliCtx, command, fromFile, fromGroup, yes, confirmDestructive)
		},
	}

	cmd.Flags().StringVar(&command, "command", "", "MDM command name (required)")
	cmd.Flags().StringVar(&fromFile, "from-file", "", "file containing one computer ID or serial per line")
	cmd.Flags().StringVar(&fromGroup, "group", "", "computer group whose members receive the command")
	cmd.Flags().BoolVar(&yes, "yes", false, "execute mutations (default: dry-run preview only)")
	cmd.Flags().BoolVar(&confirmDestructive, "confirm-destructive", false, "required for EraseDevice and DeviceLock")

	_ = cmd.MarkFlagRequired("command")

	return cmd
}

func runSendCommand(
	cmd *cobra.Command,
	cliCtx *registry.CLIContext,
	command, fromFile, fromGroup string,
	yes, confirmDestructive bool,
) error {
	ctx := cmd.Context()
	client := cliCtx.Client
	stderr := cmd.ErrOrStderr()

	// 1. Validate command name.
	if !validMDMCommands[command] {
		return fmt.Errorf("unknown MDM command %q; valid commands: %s", command, strings.Join(sortedKeys(validMDMCommands), ", "))
	}

	// 2. Gate destructive commands.
	if destructiveMDMCommands[command] {
		if !yes {
			return fmt.Errorf("command %q is destructive; both --yes and --confirm-destructive are required", command)
		}
		if !confirmDestructive {
			return fmt.Errorf("command %q is destructive; --confirm-destructive is required in addition to --yes", command)
		}
	}

	// 3. Resolve targets.
	targets, unresolved, err := resolveComputerTargets(ctx, client, fromFile, fromGroup)
	if err != nil {
		return err
	}
	warnUnresolvedTargets(stderr, unresolved)
	if len(targets) == 0 {
		_, _ = fmt.Fprintf(stderr, "No target computers found.\n")
		return nil
	}

	// 4. Build preview rows.
	previewRows := make([]map[string]any, len(targets))
	for i, t := range targets {
		previewRows[i] = map[string]any{
			"computer_id":   t["id"],
			"computer_name": t["name"],
			"command":       command,
		}
	}

	// 5. Dry-run: print preview table to stdout and log intent to stderr.
	if !yes {
		_, _ = fmt.Fprintf(stderr, "[dry-run] Would send %q to %d computers (use --yes to apply):\n", command, len(targets))
		bulkPreviewTable(previewRows)
		return nil
	}

	// 6. Execute mutations.
	_, _ = fmt.Fprintf(stderr, "Sending %q to %d computers...\n", command, len(targets))

	// Unresolvable --from-file entries count as failures (see runGroupMutation).
	successCount, failCount := 0, unresolved
	var firstErr error
	for _, t := range targets {
		if destructiveMDMCommands[command] {
			if err := cooldown.Enforce(cliCtx.ProfileName, noInput, cliCtx.DestructiveCooldown); err != nil {
				return err
			}
		}
		if err := sendMDMCommand(ctx, client, t["id"], command); err != nil {
			bulkLogW(stderr, "send-command", t["name"], "ERROR: "+err.Error())
			if firstErr == nil {
				firstErr = err
			}
			failCount++
		} else {
			if destructiveMDMCommands[command] {
				cooldown.Record(cliCtx.ProfileName)
			}
			bulkLogW(stderr, "send-command", t["name"], "ok")
			successCount++
		}
	}

	_, _ = fmt.Fprintf(stderr, "Command %q sent: %d succeeded, %d failed%s.\n",
		command, successCount, failCount, unresolvedNote(unresolved))
	return finishBatch(stderr, "send-command operations", successCount, failCount, firstErr)
}

// sortedKeys returns the keys of a map[string]bool in sorted order.
func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Simple insertion sort (small map, never performance-critical)
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}
