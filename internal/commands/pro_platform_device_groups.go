// Copyright 2026, Jamf Software LLC

package commands

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/platform"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform"
)

func newPlatformDeviceGroupsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "platform-device-groups",
		Short: "Manage device groups (Platform API)",
		Long:  "Create and manage unified device groups via the Jamf Platform API. Requires platform gateway auth.",
	}

	cmd.AddCommand(newPDGListCmd(cliCtx))
	cmd.AddCommand(newPDGGetCmd(cliCtx))
	cmd.AddCommand(newPDGApplyCmd(cliCtx))
	cmd.AddCommand(newPDGDeleteCmd(cliCtx))
	cmd.AddCommand(newPDGMembersCmd(cliCtx))
	cmd.AddCommand(newPDGAddMembersCmd(cliCtx))
	cmd.AddCommand(newPDGRemoveMembersCmd(cliCtx))

	return cmd
}

func flattenDeviceGroup(dg jamfplatform.DeviceGroupListReadRepresentationV1) map[string]any {
	return map[string]any{
		"id":          dg.ID,
		"name":        dg.Name,
		"description": dg.Description,
		"deviceType":  dg.DeviceType,
		"groupType":   dg.GroupType,
		"memberCount": dg.MemberCount,
	}
}

func newPDGListCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		sortFields []string
		filter     string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all device groups",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			groups, err := cliCtx.PlatformClient.ListDeviceGroups(cmd.Context(), sortFields, filter)
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(groups))
			for _, g := range groups {
				rows = append(rows, flattenDeviceGroup(g))
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

func newPDGGetCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get <name>",
		Short: "Get a device group by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			ctx := cmd.Context()
			r := platform.NewResolver(cliCtx.PlatformClient)
			id, err := r.ResolveDeviceGroupID(ctx, args[0])
			if err != nil {
				return err
			}
			group, err := cliCtx.PlatformClient.GetDeviceGroup(ctx, id)
			if err != nil {
				return err
			}
			return platform.PrintOne(cliCtx.Output, group)
		},
	}
}

func newPDGApplyCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		fromFile string
		yes      bool
		scaffold bool
	)
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Create or update a device group",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if scaffold {
				return printScaffold(deviceGroupScaffold())
			}
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			ctx := cmd.Context()

			data, err := readInput(fromFile)
			if err != nil {
				return err
			}

			var createReq jamfplatform.DeviceGroupCreateRepresentationV1
			if err := unmarshalInput(data, &createReq); err != nil {
				return fmt.Errorf("parsing input: %w", err)
			}
			if createReq.Name == "" {
				return fmt.Errorf("input must include a 'name' field")
			}

			r := platform.NewResolver(cliCtx.PlatformClient)
			id, resolveErr := r.ResolveDeviceGroupID(ctx, createReq.Name)
			if resolveErr != nil {
				// Not found — create
				result, err := cliCtx.PlatformClient.CreateDeviceGroup(ctx, &createReq)
				if err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "Created device group %q (id: %s)\n", createReq.Name, result.ID)
				group, err := cliCtx.PlatformClient.GetDeviceGroup(ctx, result.ID)
				if err != nil {
					return err
				}
				return platform.PrintOne(cliCtx.Output, group)
			}

			// Found — confirm before updating
			proceed, err := confirmReplace("device group", createReq.Name, yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			updateReq := &jamfplatform.DeviceGroupUpdateRepresentationV1{
				Name:        &createReq.Name,
				Description: createReq.Description,
				Criteria:    createReq.Criteria,
			}
			if err := cliCtx.PlatformClient.UpdateDeviceGroup(ctx, id, updateReq); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Updated device group %q\n", createReq.Name)
			group, err := cliCtx.PlatformClient.GetDeviceGroup(ctx, id)
			if err != nil {
				return err
			}
			return platform.PrintOne(cliCtx.Output, group)
		},
	}
	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON input file (or pipe JSON to stdin)")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt when replacing")
	cmd.Flags().BoolVar(&scaffold, "scaffold", false, "Print a JSON template for the input format")
	return cmd
}

func deviceGroupScaffold() *jamfplatform.DeviceGroupCreateRepresentationV1 {
	desc := ""
	return &jamfplatform.DeviceGroupCreateRepresentationV1{
		Name:        "My Device Group",
		Description: &desc,
		DeviceType:  "COMPUTER",
		GroupType:   "STATIC",
		Members:     []string{"<device-id>"},
	}
}

func newPDGDeleteCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a device group",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			ctx := cmd.Context()
			r := platform.NewResolver(cliCtx.PlatformClient)
			id, err := r.ResolveDeviceGroupID(ctx, args[0])
			if err != nil {
				return err
			}
			proceed, err := confirmDelete("device group", args[0], yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}
			if err := cliCtx.PlatformClient.DeleteDeviceGroup(ctx, id); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Deleted device group %q\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	return cmd
}

func newPDGMembersCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "members <name>",
		Short: "List member device IDs in a group",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			ctx := cmd.Context()
			r := platform.NewResolver(cliCtx.PlatformClient)
			id, err := r.ResolveDeviceGroupID(ctx, args[0])
			if err != nil {
				return err
			}
			members, err := cliCtx.PlatformClient.ListDeviceGroupMembers(ctx, id)
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(members))
			for _, m := range members {
				rows = append(rows, map[string]any{"deviceId": m})
			}
			data, err := json.Marshal(rows)
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}
}

func newPDGAddMembersCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var ids []string
	cmd := &cobra.Command{
		Use:   "add-members <name>",
		Short: "Add devices to a static group",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			if len(ids) == 0 {
				return fmt.Errorf("at least one --id is required")
			}
			ctx := cmd.Context()
			r := platform.NewResolver(cliCtx.PlatformClient)
			groupID, err := r.ResolveDeviceGroupID(ctx, args[0])
			if err != nil {
				return err
			}
			patch := &jamfplatform.DeviceGroupMemberPatchRepresentationV1{
				Added: ids,
			}
			if err := cliCtx.PlatformClient.UpdateDeviceGroupMembers(ctx, groupID, patch); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Added %d device(s) to group %q\n", len(ids), args[0])
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&ids, "id", nil, "Device ID to add (repeatable)")
	return cmd
}

func newPDGRemoveMembersCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var ids []string
	cmd := &cobra.Command{
		Use:   "remove-members <name>",
		Short: "Remove devices from a static group",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			if len(ids) == 0 {
				return fmt.Errorf("at least one --id is required")
			}
			ctx := cmd.Context()
			r := platform.NewResolver(cliCtx.PlatformClient)
			groupID, err := r.ResolveDeviceGroupID(ctx, args[0])
			if err != nil {
				return err
			}
			patch := &jamfplatform.DeviceGroupMemberPatchRepresentationV1{
				Removed: ids,
			}
			if err := cliCtx.PlatformClient.UpdateDeviceGroupMembers(ctx, groupID, patch); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Removed %d device(s) from group %q\n", len(ids), args[0])
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&ids, "id", nil, "Device ID to remove (repeatable)")
	return cmd
}
