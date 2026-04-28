// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"

	"github.com/spf13/cobra"

	platformgen "github.com/Jamf-Concepts/jamf-cli/internal/commands/platform/generated"
	"github.com/Jamf-Concepts/jamf-cli/internal/platform"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform"
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

	// Generated CRUD and queries: list, patch, applications, device-groups
	// get and delete are kept hand-written for serial/UUID dual lookup.
	for _, sub := range platformgen.NewDevicesCmd(cliCtx).Commands() {
		switch sub.Name() {
		case "get", "delete":
			continue
		}
		cmd.AddCommand(sub)
	}
	// Generated device actions: check-in, erase, restart, shutdown, unmanage
	for _, sub := range platformgen.NewDeviceActionsCmd(cliCtx).Commands() {
		cmd.AddCommand(sub)
	}
	// Business logic: serial/UUID dual lookup and cross-resource queries
	cmd.AddCommand(newPlatformDevicesGetCmd(cliCtx))
	cmd.AddCommand(newPlatformDevicesUpdateCmd(cliCtx))
	cmd.AddCommand(newPlatformDevicesDeleteCmd(cliCtx))
	cmd.AddCommand(newPlatformDevicesGroupsCmd(cliCtx))
	cmd.AddCommand(newPlatformDevicesUserCmd(cliCtx))

	return cmd
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

// flattenDeviceList produces a table-friendly row for a device list entry.
// Used by the user sub-command which lists devices for a specific user.
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
			list, err := devices.New(cliCtx.PlatformSDKClient).ListDevicesForUser(cmd.Context(), args[0], sortFields, filter)
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(list))
			for _, d := range list {
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
