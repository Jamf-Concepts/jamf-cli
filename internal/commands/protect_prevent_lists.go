// Copyright 2026, Jamf Software LLC

package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/protect"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfprotect-go-sdk/jamfprotect"
)

func newProtectCustomPreventListsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "custom-prevent-lists",
		Short: "Manage Jamf Protect custom prevent lists",
	}

	cmd.AddCommand(newProtectPreventListsListCmd(cliCtx))
	cmd.AddCommand(newProtectPreventListsGetCmd(cliCtx))
	cmd.AddCommand(newProtectPreventListsApplyCmd(cliCtx))
	cmd.AddCommand(newProtectPreventListsDeleteCmd(cliCtx))
	cmd.AddCommand(newProtectPreventListsExportCmd(cliCtx))

	return cmd
}

func newProtectPreventListsListCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all custom prevent lists",
		RunE: func(cmd *cobra.Command, _ []string) error {
			items, err := cliCtx.ProtectClient.ListCustomPreventLists(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(items))
			for _, p := range items {
				rows = append(rows, map[string]any{
					"name":  p.Name,
					"type":  p.Type,
					"count": p.Count,
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

func newProtectPreventListsGetCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get <name>",
		Short: "Get a custom prevent list by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r := protect.NewResolver(cliCtx.ProtectClient)
			id, err := r.ResolveCustomPreventListID(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			item, err := cliCtx.ProtectClient.GetCustomPreventList(cmd.Context(), id)
			if err != nil {
				return err
			}
			return protect.PrintOne(cliCtx.Output, item)
		},
	}
}

func newProtectPreventListsApplyCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		fromFile, name, listType, listValues string
		yes                                  bool
		scaffold                             bool
	)
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Create or update a custom prevent list",
		Long:  "Create or update a custom prevent list from a JSON file (--from-file), stdin, or from flags (--name, --type, --list).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if scaffold {
				return printExport(jamfprotect.CustomPreventListInput{})
			}
			ctx := cmd.Context()
			var input jamfprotect.CustomPreventListInput

			if fromFile != "" || !hasInlineFlags(name, listType) {
				data, err := readInput(fromFile)
				if err != nil {
					return err
				}
				if err := unmarshalInput(data, &input); err != nil {
					return fmt.Errorf("parsing input JSON: %w", err)
				}
			} else {
				if name == "" || listType == "" {
					return fmt.Errorf("--name and --type are required when not using --from-file")
				}
				input = jamfprotect.CustomPreventListInput{
					Name: name,
					Type: listType,
					Tags: []string{},
				}
				if listValues != "" {
					parts := strings.Split(listValues, ",")
					items := make([]string, 0, len(parts))
					for _, p := range parts {
						if v := strings.TrimSpace(p); v != "" {
							items = append(items, v)
						}
					}
					input.List = items
				}
			}

			if input.Name == "" {
				return fmt.Errorf("input must include a 'Name' field")
			}

			// Check if custom prevent list exists by name
			r := protect.NewResolver(cliCtx.ProtectClient)
			id, err := r.ResolveCustomPreventListID(ctx, input.Name)
			if err != nil {
				// Not found — create
				result, err := cliCtx.ProtectClient.CreateCustomPreventList(ctx, input)
				if err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "Created custom prevent list %q\n", input.Name)
				return protect.PrintOne(cliCtx.Output, result)
			}

			// Found — confirm before replacing
			proceed, err := confirmReplace("custom prevent list", input.Name, yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			result, err := cliCtx.ProtectClient.UpdateCustomPreventList(ctx, id, input)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Updated custom prevent list %q\n", input.Name)
			return protect.PrintOne(cliCtx.Output, result)
		},
	}
	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON input file (or pipe JSON to stdin)")
	cmd.Flags().StringVar(&name, "name", "", "Name of the prevent list (used with --type)")
	cmd.Flags().StringVar(&listType, "type", "", "List type (e.g. \"HASH\")")
	cmd.Flags().StringVar(&listValues, "list", "", "Comma-separated list values")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt when replacing")
	cmd.Flags().BoolVar(&scaffold, "scaffold", false, "Print an empty JSON template and exit")
	return cmd
}

// hasInlineFlags returns true if any of the given flag values are non-empty.
func hasInlineFlags(vals ...string) bool {
	for _, v := range vals {
		if v != "" {
			return true
		}
	}
	return false
}

func newProtectPreventListsDeleteCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a custom prevent list",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r := protect.NewResolver(cliCtx.ProtectClient)
			id, err := r.ResolveCustomPreventListID(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			proceed, err := confirmDelete("custom prevent list", args[0], yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			if err := cliCtx.ProtectClient.DeleteCustomPreventList(cmd.Context(), id); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Deleted custom prevent list %q\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	return cmd
}

func newProtectPreventListsExportCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "export <name>",
		Short: "Export a custom prevent list as JSON or YAML",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)
			id, err := r.ResolveCustomPreventListID(ctx, args[0])
			if err != nil {
				return err
			}
			item, err := cliCtx.ProtectClient.GetCustomPreventList(ctx, id)
			if err != nil {
				return err
			}
			return printExport(preventListToInput(item))
		},
	}
}

// preventListToInput converts a CustomPreventList response to a CustomPreventListInput, stripping server-only fields.
func preventListToInput(p *jamfprotect.CustomPreventList) jamfprotect.CustomPreventListInput {
	return jamfprotect.CustomPreventListInput{
		Name:        p.Name,
		Description: p.Description,
		Type:        p.Type,
		Tags:        p.Tags,
		List:        p.List,
	}
}
