// Copyright 2026, Jamf Software LLC

package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamf-cli/internal/school"
	"github.com/Jamf-Concepts/jamfschool-go-sdk/jamfschool"
)

func newSchoolDeviceGroupsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "device-groups",
		Short: "Manage Jamf School device groups",
	}

	cmd.AddCommand(newSchoolDeviceGroupsListCmd(cliCtx))
	cmd.AddCommand(newSchoolDeviceGroupsGetCmd(cliCtx))
	cmd.AddCommand(newSchoolDeviceGroupsApplyCmd(cliCtx))
	cmd.AddCommand(newSchoolDeviceGroupsDeleteCmd(cliCtx))
	cmd.AddCommand(newSchoolDeviceGroupsExportCmd(cliCtx))
	cmd.AddCommand(newSchoolDeviceGroupsMembersCmd(cliCtx))
	cmd.AddCommand(newSchoolDeviceGroupsAddDevicesCmd(cliCtx))
	cmd.AddCommand(newSchoolDeviceGroupsRemoveDevicesCmd(cliCtx))

	return cmd
}

func newSchoolDeviceGroupsListCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all device groups",
		RunE: func(cmd *cobra.Command, _ []string) error {
			items, err := cliCtx.SchoolClient.GetDeviceGroups(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(items))
			for _, g := range items {
				rows = append(rows, flattenSchoolDeviceGroup(g))
			}
			data, err := json.Marshal(rows)
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}
}

func flattenSchoolDeviceGroup(g jamfschool.DeviceGroup) map[string]any {
	return map[string]any{
		"id":           g.ID,
		"name":         g.Name,
		"description":  g.Description,
		"isSmartGroup": g.IsSmartGroup,
		"members":      g.Members,
		"shared":       g.Shared,
		"type":         g.Type,
		"locationId":   g.LocationID,
	}
}

func newSchoolDeviceGroupsGetCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get <name>",
		Short: "Get a device group by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := school.NewResolver(cliCtx.SchoolClient)

			id, err := r.ResolveDeviceGroupID(ctx, args[0])
			if err != nil {
				return err
			}

			item, err := cliCtx.SchoolClient.GetDeviceGroup(ctx, id)
			if err != nil {
				return err
			}
			return printResult(cliCtx.Output, item, flattenSchoolDeviceGroup(*item))
		},
	}
}

func newSchoolDeviceGroupsApplyCmd(cliCtx *registry.CLIContext) *cobra.Command {
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
				return printExport(jamfschool.DeviceGroupCreateInput{})
			}
			ctx := cmd.Context()
			data, err := readInput(fromFile)
			if err != nil {
				return err
			}
			var input jamfschool.DeviceGroupCreateInput
			if err := unmarshalInput(data, &input); err != nil {
				return fmt.Errorf("parsing input file: %w", err)
			}

			if input.Name == "" {
				return fmt.Errorf("input must include a 'Name' field")
			}

			// Check if device group exists by name
			r := school.NewResolver(cliCtx.SchoolClient)
			id, err := r.ResolveDeviceGroupID(ctx, input.Name)
			if err != nil {
				// Not found — create
				newID, err := cliCtx.SchoolClient.CreateDeviceGroup(ctx, input)
				if err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "Created device group %q (ID: %d)\n", input.Name, newID)
				item, err := cliCtx.SchoolClient.GetDeviceGroup(ctx, newID)
				if err != nil {
					return nil
				}
				return printResult(cliCtx.Output, item, flattenSchoolDeviceGroup(*item))
			}

			// Found — confirm before replacing
			proceed, err := confirmReplace("device group", input.Name, yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			updateInput := jamfschool.DeviceGroupUpdateInput{
				Name:        input.Name,
				Description: input.Description,
			}
			if err := cliCtx.SchoolClient.UpdateDeviceGroup(ctx, id, updateInput); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Updated device group %q\n", input.Name)
			item, err := cliCtx.SchoolClient.GetDeviceGroup(ctx, id)
			if err != nil {
				return nil
			}
			return printResult(cliCtx.Output, item, flattenSchoolDeviceGroup(*item))
		},
	}

	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON input file (or pipe JSON to stdin)")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt when replacing")
	cmd.Flags().BoolVar(&scaffold, "scaffold", false, "Print an empty JSON template and exit")

	return cmd
}

func newSchoolDeviceGroupsDeleteCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a device group",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := school.NewResolver(cliCtx.SchoolClient)

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

			if err := cliCtx.SchoolClient.DeleteDeviceGroup(ctx, id); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Deleted device group %q\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	return cmd
}

func newSchoolDeviceGroupsExportCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "export <name>",
		Short: "Export a device group as JSON or YAML",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := school.NewResolver(cliCtx.SchoolClient)
			id, err := r.ResolveDeviceGroupID(ctx, args[0])
			if err != nil {
				return err
			}
			item, err := cliCtx.SchoolClient.GetDeviceGroup(ctx, id)
			if err != nil {
				return err
			}
			return printExport(schoolDeviceGroupToInput(item))
		},
	}
}

func schoolDeviceGroupToInput(g *jamfschool.DeviceGroup) jamfschool.DeviceGroupCreateInput {
	return jamfschool.DeviceGroupCreateInput{
		Name:        g.Name,
		Description: g.Description,
		Information: g.Information,
		Shared:      g.Shared,
		LocationID:  &g.LocationID,
	}
}

func newSchoolDeviceGroupsMembersCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "members <name>",
		Short: "List device UDIDs in a device group",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := school.NewResolver(cliCtx.SchoolClient)

			id, err := r.ResolveDeviceGroupID(ctx, args[0])
			if err != nil {
				return err
			}

			udids, err := cliCtx.SchoolClient.GetDeviceGroupMembers(ctx, id)
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(udids))
			for _, udid := range udids {
				rows = append(rows, map[string]any{"udid": udid})
			}
			data, err := json.Marshal(rows)
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}
}

func newSchoolDeviceGroupsAddDevicesCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var udids string

	cmd := &cobra.Command{
		Use:   "add-devices <name>",
		Short: "Add devices to a device group",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := school.NewResolver(cliCtx.SchoolClient)

			id, err := r.ResolveDeviceGroupID(ctx, args[0])
			if err != nil {
				return err
			}

			udidList := splitAndTrimSchool(udids)
			if len(udidList) == 0 {
				return fmt.Errorf("--udids is required")
			}

			if err := cliCtx.SchoolClient.AddDevicesToGroup(ctx, id, udidList); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Added %d device(s) to group %q\n", len(udidList), args[0])
			return nil
		},
	}

	cmd.Flags().StringVar(&udids, "udids", "", "Comma-separated device UDIDs to add")
	_ = cmd.MarkFlagRequired("udids")

	return cmd
}

func newSchoolDeviceGroupsRemoveDevicesCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var udids string

	cmd := &cobra.Command{
		Use:   "remove-devices <name>",
		Short: "Remove devices from a device group",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := school.NewResolver(cliCtx.SchoolClient)

			id, err := r.ResolveDeviceGroupID(ctx, args[0])
			if err != nil {
				return err
			}

			udidList := splitAndTrimSchool(udids)
			if len(udidList) == 0 {
				return fmt.Errorf("--udids is required")
			}

			if err := cliCtx.SchoolClient.RemoveDevicesFromGroup(ctx, id, udidList); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Removed %d device(s) from group %q\n", len(udidList), args[0])
			return nil
		},
	}

	cmd.Flags().StringVar(&udids, "udids", "", "Comma-separated device UDIDs to remove")
	_ = cmd.MarkFlagRequired("udids")

	return cmd
}

// splitAndTrimSchool splits a comma-separated string and trims whitespace.
func splitAndTrimSchool(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
