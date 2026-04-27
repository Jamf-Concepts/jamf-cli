// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/platform"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/deviceactions"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/devicegroups"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/devices"
)

// uuidPattern matches the standard UUID format (8-4-4-4-12 hex digits).
var uuidPattern = regexp.MustCompile(
	`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func newPlatformDevicesCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "platform-devices",
		Short: "Manage devices via Platform API",
		Long:  "Unified device inventory and management actions via the Jamf Platform API. Requires platform gateway auth.",
	}

	cmd.AddCommand(newPlatformDevicesListCmd(cliCtx))
	cmd.AddCommand(newPlatformDevicesGetCmd(cliCtx))
	cmd.AddCommand(newPlatformDevicesUpdateCmd(cliCtx))
	cmd.AddCommand(newPlatformDevicesDeleteCmd(cliCtx))
	cmd.AddCommand(newPlatformDevicesAppsCmd(cliCtx))
	cmd.AddCommand(newPlatformDevicesGroupsCmd(cliCtx))
	cmd.AddCommand(newPlatformDevicesUserCmd(cliCtx))
	cmd.AddCommand(newPlatformDevicesCheckInCmd(cliCtx))
	cmd.AddCommand(newPlatformDevicesEraseCmd(cliCtx))
	cmd.AddCommand(newPlatformDevicesRestartCmd(cliCtx))
	cmd.AddCommand(newPlatformDevicesShutdownCmd(cliCtx))
	cmd.AddCommand(newPlatformDevicesUnmanageCmd(cliCtx))

	return cmd
}

func flattenDeviceList(d devices.DeviceListReadRepresentationV1) map[string]any {
	m := map[string]any{
		"id":             d.ID,
		"name":           d.Name,
		"model":          d.Model,
		"serialNumber":   d.SerialNumber,
		"osVersion":      d.OperatingSystemVersion,
		"enrollmentType": d.EnrollmentType,
		"lastInventory":  d.LastInventoryUpdateTime,
	}
	if d.UserID != nil {
		m["userId"] = *d.UserID
	}
	return m
}

// resolveDeviceIDDirect resolves a device identifier (UUID or serial number)
// via the SDK directly. UUIDs match the 8-4-4-4-12 hex format and pass
// through; anything else is resolved via a filtered list query.
func resolveDeviceIDDirect(ctx context.Context, c *jamfplatform.Client, identifier string) (string, error) {
	if uuidPattern.MatchString(identifier) {
		return identifier, nil
	}
	list, err := devices.New(c).ListDevices(ctx, nil, fmt.Sprintf("serialNumber==%q", identifier))
	if err != nil {
		return "", fmt.Errorf("listing devices: %w", err)
	}
	if len(list) == 0 {
		return "", fmt.Errorf("device with serial %q not found: %w", identifier, platform.ErrNotFound)
	}
	return list[0].ID, nil
}

func newPlatformDevicesListCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		sortFields []string
		filter     string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all devices",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			devices, err := devices.New(cliCtx.PlatformSDKClient).ListDevices(cmd.Context(), sortFields, filter)
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(devices))
			for _, d := range devices {
				rows = append(rows, flattenDeviceList(d))
			}
			data, err := json.Marshal(rows)
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}
	cmd.Flags().StringSliceVar(&sortFields, "sort", nil, "Sort fields (e.g. name:asc)")
	cmd.Flags().StringVar(&filter, "filter", "", "RSQL filter expression")
	return cmd
}

func newPlatformDevicesGetCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get <id|serial>",
		Short: "Get a device by ID or serial number",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			ctx := cmd.Context()
			id, err := resolveDeviceIDDirect(ctx, cliCtx.PlatformSDKClient, args[0])
			if err != nil {
				return err
			}
			dev, err := devices.New(cliCtx.PlatformSDKClient).GetDevice(ctx, id)
			if err != nil {
				return err
			}
			return platform.PrintOne(cliCtx.Output, dev)
		},
	}
}

func newPlatformDevicesUpdateCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		fromFile string
		name     string
		userID   string
	)
	cmd := &cobra.Command{
		Use:   "update <id|serial>",
		Short: "Update a device",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			ctx := cmd.Context()
			id, err := resolveDeviceIDDirect(ctx, cliCtx.PlatformSDKClient, args[0])
			if err != nil {
				return err
			}

			var payload devices.DeviceUpdateRepresentationV1
			if fromFile != "" {
				data, err := readInput(fromFile)
				if err != nil {
					return err
				}
				if err := unmarshalInput(data, &payload); err != nil {
					return fmt.Errorf("parsing input: %w", err)
				}
			} else {
				if name != "" {
					payload.Name = &name
				}
				if cmd.Flags().Changed("user-id") {
					if userID == "" {
						payload.UserID = nil
					} else {
						payload.UserID = &userID
					}
				}
			}

			if err := devices.New(cliCtx.PlatformSDKClient).UpdateDevice(ctx, id, &payload); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Updated device %s\n", args[0])
			return nil
		},
	}
	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON input file")
	cmd.Flags().StringVar(&name, "name", "", "New device name")
	cmd.Flags().StringVar(&userID, "user-id", "", "Assign user ID (empty string to clear)")
	return cmd
}

func newPlatformDevicesDeleteCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <id|serial>",
		Short: "Delete a device",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			ctx := cmd.Context()
			id, err := resolveDeviceIDDirect(ctx, cliCtx.PlatformSDKClient, args[0])
			if err != nil {
				return err
			}
			proceed, err := confirmDelete("device", args[0], yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}
			if err := devices.New(cliCtx.PlatformSDKClient).DeleteDevice(ctx, id); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Deleted device %s\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	return cmd
}

func newPlatformDevicesAppsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		sortFields []string
		filter     string
	)
	cmd := &cobra.Command{
		Use:   "apps <id|serial>",
		Short: "List installed applications on a device",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			ctx := cmd.Context()
			id, err := resolveDeviceIDDirect(ctx, cliCtx.PlatformSDKClient, args[0])
			if err != nil {
				return err
			}
			apps, err := devices.New(cliCtx.PlatformSDKClient).ListDeviceApplications(ctx, id, sortFields, filter)
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(apps))
			for _, a := range apps {
				rows = append(rows, map[string]any{
					"name":    a.Name,
					"version": a.Version,
				})
			}
			data, err := json.Marshal(rows)
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}
	cmd.Flags().StringSliceVar(&sortFields, "sort", nil, "Sort fields")
	cmd.Flags().StringVar(&filter, "filter", "", "RSQL filter expression")
	return cmd
}

func newPlatformDevicesGroupsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "groups <id|serial>",
		Short: "List device groups a device belongs to",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			ctx := cmd.Context()
			id, err := resolveDeviceIDDirect(ctx, cliCtx.PlatformSDKClient, args[0])
			if err != nil {
				return err
			}
			groups, err := devicegroups.New(cliCtx.PlatformSDKClient).ListDeviceGroupsForDevice(ctx, id)
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(groups))
			for _, g := range groups {
				rows = append(rows, map[string]any{
					"groupId":   g.GroupID,
					"groupName": g.GroupName,
				})
			}
			data, err := json.Marshal(rows)
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}
}

func newPlatformDevicesUserCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		sortFields []string
		filter     string
	)
	cmd := &cobra.Command{
		Use:   "user <user-id>",
		Short: "List devices assigned to a user",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			devices, err := devices.New(cliCtx.PlatformSDKClient).ListDevicesForUser(cmd.Context(), args[0], sortFields, filter)
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(devices))
			for _, d := range devices {
				rows = append(rows, flattenDeviceList(d))
			}
			data, err := json.Marshal(rows)
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}
	cmd.Flags().StringSliceVar(&sortFields, "sort", nil, "Sort fields")
	cmd.Flags().StringVar(&filter, "filter", "", "RSQL filter expression")
	return cmd
}

// ─── Device Actions ────────────────────────────────────────────────────────────

func newPlatformDevicesCheckInCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "check-in <id|serial>",
		Short: "Request a device to check for pending commands",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			ctx := cmd.Context()
			id, err := resolveDeviceIDDirect(ctx, cliCtx.PlatformSDKClient, args[0])
			if err != nil {
				return err
			}
			if dryRun {
				fmt.Fprintf(os.Stderr, "[dry-run] Would send check-in to device %s\n", args[0])
				return nil
			}
			if err := deviceactions.New(cliCtx.PlatformSDKClient).CheckInDevice(ctx, id); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Check-in sent to device %s\n", args[0])
			return nil
		},
	}
}

func newPlatformDevicesEraseCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		yes                    bool
		pin                    string
		preserveDataPlan       bool
		disallowProximitySetup bool
		clearActivationLock    bool
		returnToService        bool
	)
	cmd := &cobra.Command{
		Use:   "erase <id|serial>",
		Short: "Erase a device",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			ctx := cmd.Context()
			id, err := resolveDeviceIDDirect(ctx, cliCtx.PlatformSDKClient, args[0])
			if err != nil {
				return err
			}
			proceed, err := confirmDelete("device (ERASE)", args[0], yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}
			req := &deviceactions.EraseDeviceRequest{}
			if cmd.Flags().Changed("pin") {
				req.Pin = &pin
			}
			if cmd.Flags().Changed("preserve-data-plan") {
				req.PreserveDataPlan = &preserveDataPlan
			}
			if cmd.Flags().Changed("disallow-proximity-setup") {
				req.DisallowProximitySetup = &disallowProximitySetup
			}
			if cmd.Flags().Changed("clear-activation-lock") {
				req.ClearActivationLock = &clearActivationLock
			}
			if cmd.Flags().Changed("return-to-service") {
				req.ReturnToService = &returnToService
			}
			results, err := deviceactions.New(cliCtx.PlatformSDKClient).EraseDevice(ctx, id, req)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Erase command sent to device %s\n", args[0])
			return platform.PrintList(cliCtx.Output, results)
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	cmd.Flags().StringVar(&pin, "pin", "", "PIN code for the erase")
	cmd.Flags().BoolVar(&preserveDataPlan, "preserve-data-plan", false, "Preserve eSIM data plan")
	cmd.Flags().BoolVar(&disallowProximitySetup, "disallow-proximity-setup", false, "Disallow proximity setup")
	cmd.Flags().BoolVar(&clearActivationLock, "clear-activation-lock", false, "Clear activation lock")
	cmd.Flags().BoolVar(&returnToService, "return-to-service", false, "Return device to service after erase")
	return cmd
}

func newPlatformDevicesRestartCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "restart <id|serial>",
		Short: "Restart a device",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			ctx := cmd.Context()
			id, err := resolveDeviceIDDirect(ctx, cliCtx.PlatformSDKClient, args[0])
			if err != nil {
				return err
			}
			if dryRun {
				fmt.Fprintf(os.Stderr, "[dry-run] Would restart device %s\n", args[0])
				return nil
			}
			results, err := deviceactions.New(cliCtx.PlatformSDKClient).RestartDevice(ctx, id)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Restart command sent to device %s\n", args[0])
			return platform.PrintList(cliCtx.Output, results)
		},
	}
}

func newPlatformDevicesShutdownCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "shutdown <id|serial>",
		Short: "Shut down a device",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			ctx := cmd.Context()
			id, err := resolveDeviceIDDirect(ctx, cliCtx.PlatformSDKClient, args[0])
			if err != nil {
				return err
			}
			if dryRun {
				fmt.Fprintf(os.Stderr, "[dry-run] Would shut down device %s\n", args[0])
				return nil
			}
			results, err := deviceactions.New(cliCtx.PlatformSDKClient).ShutdownDevice(ctx, id)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Shutdown command sent to device %s\n", args[0])
			return platform.PrintList(cliCtx.Output, results)
		},
	}
}

func newPlatformDevicesUnmanageCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "unmanage <id|serial>",
		Short: "Remove remote management from a device",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			ctx := cmd.Context()
			id, err := resolveDeviceIDDirect(ctx, cliCtx.PlatformSDKClient, args[0])
			if err != nil {
				return err
			}
			proceed, err := confirmDelete("device management for", args[0], yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}
			results, err := deviceactions.New(cliCtx.PlatformSDKClient).UnmanageDevice(ctx, id)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Unmanage command sent to device %s\n", args[0])
			return platform.PrintList(cliCtx.Output, results)
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	return cmd
}
