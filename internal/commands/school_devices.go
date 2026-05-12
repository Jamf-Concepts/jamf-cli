// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamf-cli/internal/school"
	"github.com/Jamf-Concepts/jamfschool-go-sdk/jamfschool"
)

func newSchoolDevicesCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "devices",
		Short: "Manage Jamf School devices",
	}

	cmd.AddCommand(newSchoolDevicesListCmd(cliCtx))
	cmd.AddCommand(newSchoolDevicesGetCmd(cliCtx))
	cmd.AddCommand(newSchoolDevicesRestartCmd(cliCtx))
	cmd.AddCommand(newSchoolDevicesRefreshCmd(cliCtx))
	cmd.AddCommand(newSchoolDevicesUnenrollCmd(cliCtx))
	cmd.AddCommand(newSchoolDevicesEraseCmd(cliCtx))
	cmd.AddCommand(newSchoolDevicesClearActivationLockCmd(cliCtx))
	cmd.AddCommand(newSchoolDevicesTrashCmd(cliCtx))
	cmd.AddCommand(newSchoolDevicesRestoreCmd(cliCtx))

	return cmd
}

func newSchoolDevicesListCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all devices",
		RunE: func(cmd *cobra.Command, _ []string) error {
			items, err := cliCtx.SchoolClient.GetDevices(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(items))
			for _, d := range items {
				rows = append(rows, flattenSchoolDevice(d))
			}
			data, err := json.Marshal(rows)
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}
}

func flattenSchoolDevice(d jamfschool.Device) map[string]any {
	return map[string]any{
		"name":         d.Name,
		"udid":         d.UDID,
		"serialNumber": d.SerialNumber,
		"model":        d.Model.Name,
		"os":           d.OS.Prefix + " " + d.OS.Version,
		"isManaged":    d.IsManaged,
		"isSupervised": d.IsSupervised,
		"lastCheckin":  d.LastCheckin,
		"inTrash":      d.InTrash,
	}
}

func newSchoolDevicesGetCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get <name-or-udid>",
		Short: "Get a device by name, serial number, or UDID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := school.NewResolver(cliCtx.SchoolClient)

			udid, err := r.ResolveDeviceUDID(ctx, args[0])
			if err != nil {
				// Try as direct UDID
				udid = args[0]
			}

			item, err := cliCtx.SchoolClient.GetDevice(ctx, udid)
			if err != nil {
				return err
			}
			return printResult(cliCtx.Output, item, flattenSchoolDevice(*item))
		},
	}
}

func newSchoolDevicesRestartCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		yes           bool
		clearPasscode bool
	)
	cmd := &cobra.Command{
		Use:   "restart <name-or-udid>",
		Short: "Restart a device",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			udid, err := resolveSchoolDeviceUDID(ctx, cliCtx, args[0])
			if err != nil {
				return err
			}
			proceed, err := confirmAction("restart", args[0], yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}
			if err := cliCtx.SchoolClient.RestartDevice(ctx, udid, clearPasscode); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Restart command sent to %q\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&clearPasscode, "clear-passcode", false, "Clear passcode on restart")
	return cmd
}

func newSchoolDevicesRefreshCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var clearErrors bool
	cmd := &cobra.Command{
		Use:   "refresh <name-or-udid>",
		Short: "Refresh device inventory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			udid, err := resolveSchoolDeviceUDID(ctx, cliCtx, args[0])
			if err != nil {
				return err
			}
			if err := cliCtx.SchoolClient.RefreshDevice(ctx, udid, clearErrors); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Refresh command sent to %q\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&clearErrors, "clear-errors", false, "Clear errors on refresh")
	return cmd
}

func newSchoolDevicesUnenrollCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "unenroll <name-or-udid>",
		Short: "Unenroll a device",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			udid, err := resolveSchoolDeviceUDID(ctx, cliCtx, args[0])
			if err != nil {
				return err
			}
			proceed, err := confirmAction("unenroll", args[0], yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}
			if err := cliCtx.SchoolClient.UnenrollDevice(ctx, udid); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Unenroll command sent to %q\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	return cmd
}

func newSchoolDevicesEraseCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		yes                 bool
		clearActivationLock bool
	)
	cmd := &cobra.Command{
		Use:         "erase <name-or-udid>",
		Short:       "Erase a device",
		Annotations: map[string]string{"jamf:destructive": "true"},
		Args:        cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			udid, err := resolveSchoolDeviceUDID(ctx, cliCtx, args[0])
			if err != nil {
				return err
			}
			proceed, err := confirmAction("erase", args[0], yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}
			if err := cliCtx.SchoolClient.EraseDevice(ctx, udid, clearActivationLock); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Erase command sent to %q\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&clearActivationLock, "clear-activation-lock", false, "Clear activation lock on erase")
	return cmd
}

func newSchoolDevicesClearActivationLockCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "clear-activation-lock <name-or-udid>",
		Short: "Clear activation lock on a device",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			udid, err := resolveSchoolDeviceUDID(ctx, cliCtx, args[0])
			if err != nil {
				return err
			}
			proceed, err := confirmAction("clear activation lock on", args[0], yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}
			if err := cliCtx.SchoolClient.ClearDeviceActivationLock(ctx, udid); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Clear activation lock command sent to %q\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	return cmd
}

func newSchoolDevicesTrashCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "trash <name-or-udid>",
		Short: "Move a device to trash",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			udid, err := resolveSchoolDeviceUDID(ctx, cliCtx, args[0])
			if err != nil {
				return err
			}
			proceed, err := confirmAction("trash", args[0], yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}
			if err := cliCtx.SchoolClient.TrashDevice(ctx, udid); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Trashed device %q\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	return cmd
}

func newSchoolDevicesRestoreCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore <name-or-udid>",
		Short: "Restore a device from trash",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			udid, err := resolveSchoolDeviceUDID(ctx, cliCtx, args[0])
			if err != nil {
				return err
			}
			if err := cliCtx.SchoolClient.RestoreDevice(ctx, udid); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Restored device %q\n", args[0])
			return nil
		},
	}
	return cmd
}

// resolveSchoolDeviceUDID resolves a device name/serial to UDID, falling back
// to treating the input as a direct UDID.
func resolveSchoolDeviceUDID(ctx context.Context, cliCtx *registry.CLIContext, nameOrUDID string) (string, error) {
	r := school.NewResolver(cliCtx.SchoolClient)
	udid, err := r.ResolveDeviceUDID(ctx, nameOrUDID)
	if err != nil {
		return nameOrUDID, nil //nolint:nilerr // fall back to direct UDID
	}
	return udid, nil
}
