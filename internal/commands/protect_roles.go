// Copyright 2026, Jamf Software LLC

package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/protect"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfprotect-go-sdk/jamfprotect"
)

// permissionsSummary returns a compact description of role permissions,
// e.g. "R: all, W: 3 resources" or "R: 5 resources, W: all".
func permissionsSummary(p jamfprotect.RolePermissions) string {
	describe := func(label string, items []string) string {
		if slices.Contains(items, "*") {
			return label + ": all"
		}
		switch len(items) {
		case 0:
			return label + ": none"
		case 1:
			return label + ": 1 resource"
		default:
			return fmt.Sprintf("%s: %d resources", label, len(items))
		}
	}
	return describe("R", p.Read) + ", " + describe("W", p.Write)
}

func newProtectRolesCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "roles",
		Short: "Manage Jamf Protect roles",
	}

	cmd.AddCommand(newProtectRolesListCmd(cliCtx))
	cmd.AddCommand(newProtectRolesGetCmd(cliCtx))
	cmd.AddCommand(newProtectRolesApplyCmd(cliCtx))
	cmd.AddCommand(newProtectRolesDeleteCmd(cliCtx))
	cmd.AddCommand(newProtectRolesExportCmd(cliCtx))

	return cmd
}

func newProtectRolesListCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all roles",
		RunE: func(cmd *cobra.Command, _ []string) error {
			items, err := cliCtx.ProtectClient.ListRoles(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(items))
			for _, r := range items {
				rows = append(rows, flattenRole(r))
			}
			data, err := json.Marshal(rows)
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}
}

// flattenRole converts a Role into a clean map for readable table output,
// summarising permissions as a compact string.
func flattenRole(r jamfprotect.Role) map[string]any {
	return map[string]any{
		"name":        r.Name,
		"permissions": permissionsSummary(r.Permissions),
		"created":     r.Created,
		"updated":     r.Updated,
	}
}

func newProtectRolesGetCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get <name>",
		Short: "Get a role by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)

			id, err := r.ResolveRoleID(ctx, args[0])
			if err != nil {
				return err
			}

			item, err := cliCtx.ProtectClient.GetRole(ctx, id)
			if err != nil {
				return err
			}
			return printResult(cliCtx.Output, item, flattenRole(*item))
		},
	}
}

func newProtectRolesApplyCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		fromFile string
		yes      bool
		scaffold bool
	)

	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Create or update a role",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if scaffold {
				return printExport(jamfprotect.RoleInput{})
			}
			ctx := cmd.Context()
			data, err := readInput(fromFile)
			if err != nil {
				return err
			}
			var input jamfprotect.RoleInput
			if err := unmarshalInput(data, &input); err != nil {
				return fmt.Errorf("parsing input file: %w", err)
			}

			if input.Name == "" {
				return fmt.Errorf("input must include a 'Name' field")
			}

			// Check if role exists by name
			r := protect.NewResolver(cliCtx.ProtectClient)
			id, err := r.ResolveRoleID(ctx, input.Name)
			if err != nil {
				// Not found — create
				result, err := cliCtx.ProtectClient.CreateRole(ctx, input)
				if err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "Created role %q\n", input.Name)
				return printResult(cliCtx.Output, result, flattenRole(result))
			}

			// Found — confirm before replacing
			proceed, err := confirmReplace("role", input.Name, yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			result, err := cliCtx.ProtectClient.UpdateRole(ctx, id, input)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Updated role %q\n", input.Name)
			return printResult(cliCtx.Output, result, flattenRole(result))
		},
	}

	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON input file (or pipe JSON to stdin)")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt when replacing")
	cmd.Flags().BoolVar(&scaffold, "scaffold", false, "Print an empty JSON template and exit")

	return cmd
}

func newProtectRolesDeleteCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:         "delete <name>",
		Short:       "Delete a role",
		Annotations: map[string]string{"jamf:destructive": "true"},
		Args:        cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)

			id, err := r.ResolveRoleID(ctx, args[0])
			if err != nil {
				return err
			}

			proceed, err := confirmDelete("role", args[0], yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			if err := cliCtx.ProtectClient.DeleteRole(ctx, id); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Deleted role %q\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	return cmd
}

func newProtectRolesExportCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "export <name>",
		Short: "Export a role as JSON or YAML",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)
			id, err := r.ResolveRoleID(ctx, args[0])
			if err != nil {
				return err
			}
			item, err := cliCtx.ProtectClient.GetRole(ctx, id)
			if err != nil {
				return err
			}
			return printExport(roleToInput(item))
		},
	}
}

// roleToInput converts a Role response to a RoleInput, stripping server-only fields.
func roleToInput(r *jamfprotect.Role) jamfprotect.RoleInput {
	return jamfprotect.RoleInput{
		Name:           r.Name,
		ReadResources:  r.Permissions.Read,
		WriteResources: r.Permissions.Write,
	}
}
