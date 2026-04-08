// Copyright 2026, Jamf Software LLC

package commands

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamf-cli/internal/resolve"
)

// flushStatus maps user-facing --status flag values to the Classic API path segment.
// The API accepts "Failed", "Pending", or "Pending+Failed" (URL-encoded as "Pending%2BFailed").
var flushStatusMap = map[string]string{
	"failed":  "Failed",
	"pending": "Pending",
	"both":    "Pending+Failed",
}

// newComputerFlushCommandsCmd creates the `computers flush-commands` subcommand.
func newComputerFlushCommandsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		dt     deviceTarget
		status string
		yes    bool
	)

	cmd := &cobra.Command{
		Use:   "flush-commands",
		Short: "Flush pending and/or failed MDM commands from a computer's queue",
		Long: `Flush pending and/or failed MDM commands from the MDM command queue for a
computer or computer group.

The --status flag controls which commands are removed:
  failed   Remove only failed commands (default — safest option)
  pending  Remove only pending commands
  both     Remove both pending and failed commands

WARNING: Flushing pending commands removes queued commands that have not yet
been delivered. Only use "pending" or "both" when you intend to clear the
entire queue. Failed commands can be safely flushed without affecting delivery
of commands that have not yet been attempted.

For group targets, a single API call flushes all members of the group at once.`,
		Example: `  # Flush failed commands by serial number (safe default)
  jamf-cli pro computers flush-commands --serial C02X1234ABCD --yes

  # Flush failed commands by device name
  jamf-cli pro computers flush-commands --name "Neil's MacBook Pro" --yes

  # Flush both pending and failed from one device (requires explicit --status both)
  jamf-cli pro computers flush-commands --serial C02X1234ABCD --status both --yes

  # Flush failed commands from all members of a group (one API call)
  jamf-cli pro computers flush-commands --group "Problem Devices" --yes

  # Flush both statuses from a group
  jamf-cli pro computers flush-commands --group "Lab Macs" --status both --yes

  # Dry-run: see what would be flushed without executing
  jamf-cli pro computers flush-commands --serial C02X1234ABCD --dry-run`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if dt.serial == "" && dt.name == "" && dt.id == "" && dt.group == "" {
				return fmt.Errorf("one of --serial, --name, --id, or --group is required")
			}
			apiStatus, ok := flushStatusMap[strings.ToLower(status)]
			if !ok {
				return fmt.Errorf("invalid --status %q: must be failed, pending, or both", status)
			}
			ctx := cmd.Context()
			stderr := cmd.ErrOrStderr()

			// Group flush: resolve group name → Classic group ID → one API call.
			if dt.group != "" {
				groupID, err := resolve.ResolveClassicComputerGroupID(ctx, cliCtx.Client, dt.group)
				if err != nil {
					return err
				}
				if dryRun {
					_, _ = fmt.Fprintf(stderr, "[dry-run] Would flush %s commands from computer group %q (id: %s)\n", status, dt.group, groupID)
					return nil
				}
				// Group path never offers an interactive prompt — the blast radius is too high.
				// --yes is always required; --no-input without --yes is an error, not a silent no-op.
				if !yes {
					isNoInput, _ := cmd.Flags().GetBool("no-input")
					if isNoInput {
						return fmt.Errorf("flush-commands requires --yes when --no-input is set")
					}
					_, _ = fmt.Fprintf(stderr, "This will flush %s MDM commands from all computers in group %q. Use --yes to execute.\n", status, dt.group)
					return nil
				}
				path := fmt.Sprintf("/JSSResource/commandflush/computergroups/id/%s/status/%s",
					url.PathEscape(groupID), url.PathEscape(apiStatus))
				resp, err := cliCtx.Client.Do(ctx, "DELETE", path, nil)
				if err != nil {
					return err
				}
				defer func() { _ = resp.Body.Close() }()
				_, _ = fmt.Fprintf(stderr, "Flushed %s MDM commands from computer group %q.\n", status, dt.group)
				return cliCtx.Output.PrintResponse(resp)
			}

			// Single-device flush: resolve device → Classic numeric ID → per-device API call.
			d, err := resolve.ResolveComputer(ctx, cliCtx.Client, dt.serial, dt.name, dt.id)
			if err != nil {
				return err
			}
			if dryRun {
				_, _ = fmt.Fprintf(stderr, "[dry-run] Would flush %s commands from computer %s\n", status, resolve.FormatDeviceDesc(d))
				return nil
			}
			if !yes {
				isNoInput, _ := cmd.Flags().GetBool("no-input")
				if isNoInput {
					return fmt.Errorf("flush-commands requires --yes when --no-input is set")
				}
				_, _ = fmt.Fprintf(stderr, "This will flush %s MDM commands from computer %s. Type 'yes' to confirm: ", status, resolve.FormatDeviceDesc(d))
				var confirm string
				_, _ = fmt.Scanln(&confirm)
				if confirm != "yes" {
					return fmt.Errorf("aborted")
				}
			}
			path := fmt.Sprintf("/JSSResource/commandflush/computers/id/%s/status/%s",
				url.PathEscape(d.ID), url.PathEscape(apiStatus))
			resp, err := cliCtx.Client.Do(ctx, "DELETE", path, nil)
			if err != nil {
				return err
			}
			defer func() { _ = resp.Body.Close() }()
			_, _ = fmt.Fprintf(stderr, "Flushed %s MDM commands from computer %s.\n", status, resolve.FormatDeviceDesc(d))
			return cliCtx.Output.PrintResponse(resp)
		},
	}

	dt.addFlushFlags(cmd, "computer")
	cmd.Flags().StringVar(&status, "status", "failed", "commands to flush: failed, pending, or both")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation prompt")

	return cmd
}

// newMobileFlushCommandsCmd creates the `mobile-devices flush-commands` subcommand.
func newMobileFlushCommandsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		dt     deviceTarget
		status string
		yes    bool
	)

	cmd := &cobra.Command{
		Use:   "flush-commands",
		Short: "Flush pending and/or failed MDM commands from a mobile device's queue",
		Long: `Flush pending and/or failed MDM commands from the MDM command queue for a
mobile device or mobile device group.

The --status flag controls which commands are removed:
  failed   Remove only failed commands (default — safest option)
  pending  Remove only pending commands
  both     Remove both pending and failed commands

WARNING: Flushing pending commands removes queued commands that have not yet
been delivered. Only use "pending" or "both" when you intend to clear the
entire queue. Failed commands can be safely flushed without affecting delivery
of commands that have not yet been attempted.

For group targets, a single API call flushes all members of the group at once.`,
		Example: `  # Flush failed commands by serial number (safe default)
  jamf-cli pro mobile-devices flush-commands --serial F4GH5678 --yes

  # Flush both pending and failed from one device
  jamf-cli pro mobile-devices flush-commands --serial F4GH5678 --status both --yes

  # Flush failed commands from all members of a group (one API call)
  jamf-cli pro mobile-devices flush-commands --group "Problem iPads" --yes

  # Dry-run: see what would be flushed without executing
  jamf-cli pro mobile-devices flush-commands --serial F4GH5678 --dry-run`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if dt.serial == "" && dt.name == "" && dt.id == "" && dt.group == "" {
				return fmt.Errorf("one of --serial, --name, --id, or --group is required")
			}
			apiStatus, ok := flushStatusMap[strings.ToLower(status)]
			if !ok {
				return fmt.Errorf("invalid --status %q: must be failed, pending, or both", status)
			}
			ctx := cmd.Context()
			stderr := cmd.ErrOrStderr()

			// Group flush: resolve group name → Classic group ID → one API call.
			if dt.group != "" {
				groupID, err := resolve.ResolveClassicMobileGroupID(ctx, cliCtx.Client, dt.group)
				if err != nil {
					return err
				}
				if dryRun {
					_, _ = fmt.Fprintf(stderr, "[dry-run] Would flush %s commands from mobile device group %q (id: %s)\n", status, dt.group, groupID)
					return nil
				}
				// Group path never offers an interactive prompt — the blast radius is too high.
				// --yes is always required; --no-input without --yes is an error, not a silent no-op.
				if !yes {
					isNoInput, _ := cmd.Flags().GetBool("no-input")
					if isNoInput {
						return fmt.Errorf("flush-commands requires --yes when --no-input is set")
					}
					_, _ = fmt.Fprintf(stderr, "This will flush %s MDM commands from all mobile devices in group %q. Use --yes to execute.\n", status, dt.group)
					return nil
				}
				path := fmt.Sprintf("/JSSResource/commandflush/mobiledevicegroups/id/%s/status/%s",
					url.PathEscape(groupID), url.PathEscape(apiStatus))
				resp, err := cliCtx.Client.Do(ctx, "DELETE", path, nil)
				if err != nil {
					return err
				}
				defer func() { _ = resp.Body.Close() }()
				_, _ = fmt.Fprintf(stderr, "Flushed %s MDM commands from mobile device group %q.\n", status, dt.group)
				return cliCtx.Output.PrintResponse(resp)
			}

			// Single-device flush: resolve device → Classic numeric ID → per-device API call.
			d, err := resolve.ResolveMobileDevice(ctx, cliCtx.Client, dt.serial, dt.name, dt.id)
			if err != nil {
				return err
			}
			if dryRun {
				_, _ = fmt.Fprintf(stderr, "[dry-run] Would flush %s commands from mobile device %s\n", status, resolve.FormatDeviceDesc(d))
				return nil
			}
			if !yes {
				isNoInput, _ := cmd.Flags().GetBool("no-input")
				if isNoInput {
					return fmt.Errorf("flush-commands requires --yes when --no-input is set")
				}
				_, _ = fmt.Fprintf(stderr, "This will flush %s MDM commands from mobile device %s. Type 'yes' to confirm: ", status, resolve.FormatDeviceDesc(d))
				var confirm string
				_, _ = fmt.Scanln(&confirm)
				if confirm != "yes" {
					return fmt.Errorf("aborted")
				}
			}
			path := fmt.Sprintf("/JSSResource/commandflush/mobiledevices/id/%s/status/%s",
				url.PathEscape(d.ID), url.PathEscape(apiStatus))
			resp, err := cliCtx.Client.Do(ctx, "DELETE", path, nil)
			if err != nil {
				return err
			}
			defer func() { _ = resp.Body.Close() }()
			_, _ = fmt.Fprintf(stderr, "Flushed %s MDM commands from mobile device %s.\n", status, resolve.FormatDeviceDesc(d))
			return cliCtx.Output.PrintResponse(resp)
		},
	}

	dt.addFlushFlags(cmd, "mobile device")
	cmd.Flags().StringVar(&status, "status", "failed", "commands to flush: failed, pending, or both")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation prompt")

	return cmd
}
