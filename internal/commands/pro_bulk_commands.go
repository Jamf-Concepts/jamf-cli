// Copyright 2026, Jamf Software LLC

package commands

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// validMDMCommands is the set of MDM commands send-command recognises. Only
// the ones absent from deadClassicComputerCommands are still sent.
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

// deadClassicCommand says why Jamf Pro no longer queues a Classic computer
// command, and names the command to use instead ("" for none).
type deadClassicCommand struct {
	answer    string
	successor string
}

// deadClassicComputerCommands are the send-command commands Jamf Pro no
// longer queues through POST /computercommands/command/{command}/id/{id}.
// Wire-checked 2026-10-04 on Jamf Pro 11.32 against simulated computers, both
// direct and through the gateway; the Classic API documents only
// EnableRemoteDesktop and DisableRemoteDesktop. They are refused before
// anything is sent, because the answer was a 400, a 500 or a 401 per computer —
// a fleet-wide run reported every target failed, and the 401 read as a
// credential problem. EraseDevice was not sent; it is undocumented like the
// rest, and computer-inventory erase reaches the gateway.
var deadClassicComputerCommands = map[string]deadClassicCommand{
	"BlankPush":                       {`it answers 400 "No command was queued"`, "jamf-cli pro computer-inventory blank-push"},
	"DeleteUser":                      {`it answers 400 "No command was queued"`, ""},
	"DeviceLock":                      {`it answers 400 "No command was queued"`, "jamf-cli pro computer-inventory lock (not through a platform gateway profile)"},
	"ScheduleOSUpdate":                {`it answers 400 "No command was queued"`, "jamf-cli pro managed-software-updates-plans create"},
	"UnlockUserAccount":               {`it answers 400 "user_name required", which send-command has no way to pass`, ""},
	"DeviceInformation":               {"it answers 500 through the gateway and 401 direct", ""},
	"UpdateInventory":                 {"it answers 500 through the gateway and 401 direct", ""},
	"Settings":                        {"it answers 500 through the gateway and 401 direct", "jamf-cli pro computer-inventory settings"},
	"RedeployJamfManagementFramework": {"it answers 500 through the gateway and 401 direct", "jamf-cli pro computer-inventory redeploy-framework"},
	"EraseDevice":                     {"the Classic API no longer documents it", "jamf-cli pro computer-inventory erase"},
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

Jamf Pro now queues only EnableRemoteDesktop and DisableRemoteDesktop through
the Classic API, and pro computer-inventory enable-remote-desktop /
disable-remote-desktop send those through it on a platform gateway profile.
Every other command is refused before anything is sent, naming its
replacement where there is one.

Targets are specified via --from-file (one computer per line: ID, serial number,
UDID, management ID or name) or --group (all members of a computer group).

Entries in --from-file are resolved to computer IDs first. A line that matches
no computer is reported and skipped, and counts as a failure in the summary and
exit code (use --allow-partial-failure to tolerate it); if no line resolves at
all, or the file holds no entries, the command fails without sending anything.

Without --yes, or with -n/--dry-run, the command prints a preview table and
exits without making any changes.

Commands still sent: EnableRemoteDesktop, DisableRemoteDesktop`,
		Deprecated: "use `jamf-cli pro computer-inventory enable-remote-desktop` or `disable-remote-desktop` (--group / --from-file), which reach a platform gateway through the Classic API; `pro bulk send-command` will be removed in a future release",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSendCommand(cmd, cliCtx, command, fromFile, fromGroup, yes)
		},
	}

	cmd.Flags().StringVar(&command, "command", "", "MDM command name (required)")
	cmd.Flags().StringVar(&fromFile, "from-file", "", "file listing one computer per line: ID, serial number, UDID, management ID or name")
	cmd.Flags().StringVar(&fromGroup, "group", "", "computer group whose members receive the command")
	cmd.Flags().BoolVar(&yes, "yes", false, "execute mutations (default: dry-run preview only)")
	// Kept so an existing invocation still parses; nothing send-command can
	// still send is destructive.
	cmd.Flags().BoolVar(&confirmDestructive, "confirm-destructive", false, "no effect: EraseDevice and DeviceLock are no longer sent")
	_ = cmd.Flags().MarkDeprecated("confirm-destructive", "EraseDevice and DeviceLock are no longer sent; use pro computer-inventory erase / lock")

	_ = cmd.MarkFlagRequired("command")

	return cmd
}

// refuseDeadClassicCommand refuses a command Jamf Pro no longer queues through
// the Classic API, naming what it answers and what to use instead.
func refuseDeadClassicCommand(command string) error {
	dead, ok := deadClassicComputerCommands[command]
	if !ok {
		return nil
	}
	hint := "there is no replacement command for computers"
	if dead.successor != "" {
		hint = "use `" + dead.successor + "`"
	}
	return &exitcode.Error{
		Code:    exitcode.Unsupported,
		Message: fmt.Sprintf("Jamf Pro no longer queues %s through the Classic API: %s", command, dead.answer),
		Hint:    hint,
	}
}

func runSendCommand(
	cmd *cobra.Command,
	cliCtx *registry.CLIContext,
	command, fromFile, fromGroup string,
	yes bool,
) error {
	ctx := cmd.Context()
	client := cliCtx.Client
	stderr := cmd.ErrOrStderr()

	// 1. Validate command name.
	if !validMDMCommands[command] {
		return fmt.Errorf("unknown MDM command %q; valid commands: %s", command, strings.Join(sortedKeys(validMDMCommands), ", "))
	}
	if err := refuseDeadClassicCommand(command); err != nil {
		return err
	}

	// 2. Resolve targets.
	targets, unresolved, err := resolveComputerTargets(ctx, client, fromFile, fromGroup)
	if err != nil {
		return err
	}
	warnUnresolvedTargets(stderr, unresolved)
	if len(targets) == 0 {
		_, _ = fmt.Fprintf(stderr, "No target computers found.\n")
		return nil
	}

	// 3. Build preview rows.
	previewRows := make([]map[string]any, len(targets))
	for i, t := range targets {
		previewRows[i] = map[string]any{
			"computer_id":   t["id"],
			"computer_name": t["name"],
			"command":       command,
		}
	}

	// 4. Preview: print the table to stdout and the intent to stderr.
	if dryRun || !yes {
		_, _ = fmt.Fprintf(stderr, "[dry-run] Would send %q to %d computers (use --yes to apply):\n", command, len(targets))
		bulkPreviewTable(previewRows)
		return nil
	}

	// 5. Execute.
	_, _ = fmt.Fprintf(stderr, "Sending %q to %d computers...\n", command, len(targets))

	// Unresolvable --from-file entries count as failures (see runGroupMutation).
	successCount, failCount := 0, unresolved
	var firstErr error
	for _, t := range targets {
		if err := sendMDMCommand(ctx, client, t["id"], command); err != nil {
			bulkLogW(stderr, "send-command", t["name"], "ERROR: "+err.Error())
			if firstErr == nil {
				firstErr = err
			}
			failCount++
		} else {
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
