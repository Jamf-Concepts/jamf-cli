// Copyright 2026, Jamf Software LLC

package commands

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	platformgen "github.com/Jamf-Concepts/jamf-cli/internal/commands/platform/generated"
	"github.com/Jamf-Concepts/jamf-cli/internal/platform"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/devicegroups"
)

func newPlatformDeviceGroupsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "platform-device-groups",
		Short: "Manage device groups (Platform API)",
		Long:  "Create and manage unified device groups via the Jamf Platform API. Requires platform gateway auth.",
	}

	// Generated CRUD and member ops (list, create, get, patch, delete, members, patch-members)
	for _, sub := range platformgen.NewDeviceGroupsCmd(cliCtx).Commands() {
		cmd.AddCommand(sub)
	}

	// Business logic: upsert apply and ergonomic member mutations
	cmd.AddCommand(newPDGApplyCmd(cliCtx))
	cmd.AddCommand(newPDGAddMembersCmd(cliCtx))
	cmd.AddCommand(newPDGRemoveMembersCmd(cliCtx))

	return cmd
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

			var createReq devicegroups.DeviceGroupCreateRepresentationV1
			if err := unmarshalInput(data, &createReq); err != nil {
				return fmt.Errorf("parsing input: %w", err)
			}
			if createReq.Name == "" {
				return fmt.Errorf("input must include a 'name' field")
			}

			dg := devicegroups.New(cliCtx.PlatformSDKClient)
			id, resolveErr := dg.ResolveDeviceGroupIDByName(ctx, createReq.Name)
			if resolveErr != nil {
				// Not found — create
				result, err := devicegroups.New(cliCtx.PlatformSDKClient).CreateDeviceGroup(ctx, &createReq)
				if err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "Created device group %q (id: %s)\n", createReq.Name, result.ID)
				group, err := devicegroups.New(cliCtx.PlatformSDKClient).GetDeviceGroup(ctx, result.ID)
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

			updateReq := &devicegroups.DeviceGroupUpdateRepresentationV1{
				Name:        &createReq.Name,
				Description: createReq.Description,
				Criteria:    createReq.Criteria,
			}
			if err := devicegroups.New(cliCtx.PlatformSDKClient).UpdateDeviceGroup(ctx, id, updateReq); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Updated device group %q\n", createReq.Name)
			group, err := devicegroups.New(cliCtx.PlatformSDKClient).GetDeviceGroup(ctx, id)
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

func deviceGroupScaffold() *devicegroups.DeviceGroupCreateRepresentationV1 {
	desc := ""
	return &devicegroups.DeviceGroupCreateRepresentationV1{
		Name:        "My Device Group",
		Description: &desc,
		DeviceType:  "COMPUTER",
		GroupType:   "STATIC",
		Members:     &[]string{"<device-id>"},
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
			dg := devicegroups.New(cliCtx.PlatformSDKClient)
			groupID, err := dg.ResolveDeviceGroupIDByName(ctx, args[0])
			if err != nil {
				return err
			}
			patch := &devicegroups.DeviceGroupMemberPatchRepresentationV1{
				Added: &ids,
			}
			if err := devicegroups.New(cliCtx.PlatformSDKClient).UpdateDeviceGroupMembers(ctx, groupID, patch); err != nil {
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
			dg := devicegroups.New(cliCtx.PlatformSDKClient)
			groupID, err := dg.ResolveDeviceGroupIDByName(ctx, args[0])
			if err != nil {
				return err
			}
			patch := &devicegroups.DeviceGroupMemberPatchRepresentationV1{
				Removed: &ids,
			}
			if err := devicegroups.New(cliCtx.PlatformSDKClient).UpdateDeviceGroupMembers(ctx, groupID, patch); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Removed %d device(s) from group %q\n", len(ids), args[0])
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&ids, "id", nil, "Device ID to remove (repeatable)")
	return cmd
}
