// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"errors"
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
		Use:   "get <udid|serial|name>",
		Short: "Get a device by name, serial number, or UDID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			udid, _, err := resolveSchoolDevice(ctx, cliCtx, args[0])
			if err != nil {
				return err
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
		Use:         "restart <udid|serial|name>",
		Short:       "Restart a device",
		Annotations: map[string]string{"jamf:destructive": "true"},
		Args:        cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			udid, label, err := resolveSchoolDevice(ctx, cliCtx, args[0])
			if err != nil {
				return err
			}
			proceed, err := confirmAction("restart", label, yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}
			if err := cliCtx.SchoolClient.RestartDevice(ctx, udid, clearPasscode); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Restart command sent to %q\n", label)
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
		Use:   "refresh <udid|serial|name>",
		Short: "Refresh device inventory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			udid, label, err := resolveSchoolDevice(ctx, cliCtx, args[0])
			if err != nil {
				return err
			}
			if err := cliCtx.SchoolClient.RefreshDevice(ctx, udid, clearErrors); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Refresh command sent to %q\n", label)
			return nil
		},
	}
	cmd.Flags().BoolVar(&clearErrors, "clear-errors", false, "Clear errors on refresh")
	return cmd
}

func newSchoolDevicesUnenrollCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "unenroll <udid|serial|name>",
		Short: "Unenroll a device",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			udid, label, err := resolveSchoolDevice(ctx, cliCtx, args[0])
			if err != nil {
				return err
			}
			proceed, err := confirmAction("unenroll", label, yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}
			if err := cliCtx.SchoolClient.UnenrollDevice(ctx, udid); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Unenroll command sent to %q\n", label)
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
		Use:         "erase <udid|serial|name>",
		Short:       "Erase a device",
		Annotations: map[string]string{"jamf:destructive": "true"},
		Args:        cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			udid, label, err := resolveSchoolDevice(ctx, cliCtx, args[0])
			if err != nil {
				return err
			}
			proceed, err := confirmAction("erase", label, yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}
			if err := cliCtx.SchoolClient.EraseDevice(ctx, udid, clearActivationLock); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Erase command sent to %q\n", label)
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
		Use:   "clear-activation-lock <udid|serial|name>",
		Short: "Clear activation lock on a device",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			udid, label, err := resolveSchoolDevice(ctx, cliCtx, args[0])
			if err != nil {
				return err
			}
			proceed, err := confirmAction("clear activation lock on", label, yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}
			if err := cliCtx.SchoolClient.ClearDeviceActivationLock(ctx, udid); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Clear activation lock command sent to %q\n", label)
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	return cmd
}

func newSchoolDevicesTrashCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "trash <udid|serial|name>",
		Short: "Move a device to trash",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			udid, label, err := resolveSchoolDevice(ctx, cliCtx, args[0])
			if err != nil {
				return err
			}
			proceed, err := confirmAction("trash", label, yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}
			if err := cliCtx.SchoolClient.TrashDevice(ctx, udid); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Trashed device %q\n", label)
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	return cmd
}

func newSchoolDevicesRestoreCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore <udid|serial|name>",
		Short: "Restore a device from trash",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			udid, label, err := resolveSchoolDevice(ctx, cliCtx, args[0])
			if err != nil {
				return err
			}
			if err := cliCtx.SchoolClient.RestoreDevice(ctx, udid); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Restored device %q\n", label)
			return nil
		},
	}
	return cmd
}

// resolveSchoolDevice resolves a UDID, serial or name to one device's UDID and
// a label naming the device for confirmations. Only a device absent from the
// listing falls back to treating the argument as a UDID (a trashed device is
// not listed); an ambiguous or failed lookup is returned.
func resolveSchoolDevice(ctx context.Context, cliCtx *registry.CLIContext, arg string) (udid, label string, err error) {
	d, err := school.NewResolver(cliCtx.SchoolClient).ResolveDevice(ctx, arg)
	var notFound *school.ErrNotFound
	if errors.As(err, &notFound) {
		return arg, arg, nil
	}
	if err != nil {
		return "", "", err
	}
	return d.UDID, school.DescribeDevice(d), nil
}
