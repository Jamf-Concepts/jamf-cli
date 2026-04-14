// Copyright 2026, Jamf Software LLC

package commands

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamf-cli/internal/school"
	"github.com/Jamf-Concepts/jamfschool-go-sdk/jamfschool"
)

func newSchoolGroupsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "groups",
		Short: "Manage Jamf School user groups",
	}

	cmd.AddCommand(newSchoolGroupsListCmd(cliCtx))
	cmd.AddCommand(newSchoolGroupsGetCmd(cliCtx))
	cmd.AddCommand(newSchoolGroupsApplyCmd(cliCtx))
	cmd.AddCommand(newSchoolGroupsDeleteCmd(cliCtx))
	cmd.AddCommand(newSchoolGroupsExportCmd(cliCtx))

	return cmd
}

func newSchoolGroupsListCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all user groups",
		RunE: func(cmd *cobra.Command, _ []string) error {
			items, err := cliCtx.SchoolClient.GetGroups(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(items))
			for _, g := range items {
				rows = append(rows, flattenSchoolGroup(g))
			}
			data, err := json.Marshal(rows)
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}
}

func flattenSchoolGroup(g jamfschool.Group) map[string]any {
	return map[string]any{
		"id":          g.ID,
		"name":        g.Name,
		"description": g.Description,
		"userCount":   g.UserCount,
		"locationId":  g.LocationID,
		"modified":    g.Modified,
	}
}

func newSchoolGroupsGetCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get <name>",
		Short: "Get a user group by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := school.NewResolver(cliCtx.SchoolClient)

			id, err := r.ResolveGroupID(ctx, args[0])
			if err != nil {
				return err
			}

			item, err := cliCtx.SchoolClient.GetGroup(ctx, id)
			if err != nil {
				return err
			}
			return printResult(cliCtx.Output, item, flattenSchoolGroup(*item))
		},
	}
}

func newSchoolGroupsApplyCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		fromFile string
		yes      bool
		scaffold bool
	)

	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Create or update a user group",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if scaffold {
				return printExport(jamfschool.GroupCreateInput{})
			}
			ctx := cmd.Context()
			data, err := readInput(fromFile)
			if err != nil {
				return err
			}
			var input jamfschool.GroupCreateInput
			if err := unmarshalInput(data, &input); err != nil {
				return fmt.Errorf("parsing input file: %w", err)
			}

			if input.Name == "" {
				return fmt.Errorf("input must include a 'Name' field")
			}

			// Check if group exists by name
			r := school.NewResolver(cliCtx.SchoolClient)
			id, err := r.ResolveGroupID(ctx, input.Name)
			if err != nil {
				// Not found — create
				newID, err := cliCtx.SchoolClient.CreateGroup(ctx, input)
				if err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "Created group %q (ID: %d)\n", input.Name, newID)
				item, err := cliCtx.SchoolClient.GetGroup(ctx, newID)
				if err != nil {
					return nil
				}
				return printResult(cliCtx.Output, item, flattenSchoolGroup(*item))
			}

			// Found — confirm before replacing
			proceed, err := confirmReplace("group", input.Name, yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			updateInput := jamfschool.GroupUpdateInput{
				Name:        input.Name,
				Description: input.Description,
				ACL:         input.ACL,
			}
			if err := cliCtx.SchoolClient.UpdateGroup(ctx, id, updateInput); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Updated group %q\n", input.Name)
			item, err := cliCtx.SchoolClient.GetGroup(ctx, id)
			if err != nil {
				return nil
			}
			return printResult(cliCtx.Output, item, flattenSchoolGroup(*item))
		},
	}

	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON input file (or pipe JSON to stdin)")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt when replacing")
	cmd.Flags().BoolVar(&scaffold, "scaffold", false, "Print an empty JSON template and exit")

	return cmd
}

func newSchoolGroupsDeleteCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a user group",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := school.NewResolver(cliCtx.SchoolClient)

			id, err := r.ResolveGroupID(ctx, args[0])
			if err != nil {
				return err
			}

			proceed, err := confirmDelete("group", args[0], yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			if err := cliCtx.SchoolClient.DeleteGroup(ctx, id); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Deleted group %q\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	return cmd
}

func newSchoolGroupsExportCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "export <name>",
		Short: "Export a user group as JSON or YAML",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := school.NewResolver(cliCtx.SchoolClient)
			id, err := r.ResolveGroupID(ctx, args[0])
			if err != nil {
				return err
			}
			item, err := cliCtx.SchoolClient.GetGroup(ctx, id)
			if err != nil {
				return err
			}
			return printExport(schoolGroupToInput(item))
		},
	}
}

func schoolGroupToInput(g *jamfschool.Group) jamfschool.GroupCreateInput {
	return jamfschool.GroupCreateInput{
		Name:        g.Name,
		Description: g.Description,
		LocationID:  &g.LocationID,
		ACL:         &g.ACL,
	}
}
