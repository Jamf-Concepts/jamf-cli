// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/protect"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfprotect-go-sdk/jamfprotect"
)

func newProtectPreventListSetsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prevent-list-sets",
		Short: "Manage Jamf Protect prevent list sets",
		Long: `Manage Jamf Protect prevent list sets.

A prevent list set groups custom prevent lists so they can be assigned to plans
as a unit. A custom prevent list only reaches endpoints on the plans its sets
are assigned to. Use 'protect plans apply' (preventListSets) to attach sets.`,
	}

	cmd.AddCommand(newProtectPreventListSetsListCmd(cliCtx))
	cmd.AddCommand(newProtectPreventListSetsGetCmd(cliCtx))
	cmd.AddCommand(newProtectPreventListSetsApplyCmd(cliCtx))
	cmd.AddCommand(newProtectPreventListSetsDeleteCmd(cliCtx))
	cmd.AddCommand(newProtectPreventListSetsAddListCmd(cliCtx))
	cmd.AddCommand(newProtectPreventListSetsRemoveListCmd(cliCtx))
	cmd.AddCommand(newProtectPreventListSetsExportCmd(cliCtx))

	return cmd
}

func newProtectPreventListSetsListCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all prevent list sets",
		RunE: func(cmd *cobra.Command, _ []string) error {
			items, err := cliCtx.ProtectClient.ListPreventListSets(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(items))
			for _, s := range items {
				rows = append(rows, flattenPreventListSet(s))
			}
			data, err := json.Marshal(rows)
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}
}

// flattenPreventListSet reduces nested slices to names/counts for table output.
func flattenPreventListSet(s jamfprotect.PreventListSet) map[string]any {
	listNames := make([]string, 0, len(s.PreventLists))
	for _, l := range s.PreventLists {
		listNames = append(listNames, l.Name)
	}
	planNames := make([]string, 0, len(s.Plans))
	for _, p := range s.Plans {
		planNames = append(planNames, p.Name)
	}
	return map[string]any{
		"uuid":              s.UUID,
		"name":              s.Name,
		"description":       s.Description,
		"preventListsCount": len(s.PreventLists),
		// always present: table/csv columns come from row 0
		"preventLists": strings.Join(listNames, ", "),
		"plans":        strings.Join(planNames, ", "),
	}
}

func newProtectPreventListSetsGetCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get <name>",
		Short: "Get a prevent list set by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)
			uuid, err := r.ResolvePreventListSetUUID(ctx, args[0])
			if err != nil {
				return err
			}
			item, err := cliCtx.ProtectClient.GetPreventListSet(ctx, uuid)
			if err != nil {
				return err
			}
			return printResult(cliCtx.Output, item, flattenPreventListSet(*item))
		},
	}
}

func newProtectPreventListSetsApplyCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		fromFile string
		yes      bool
		scaffold bool
	)

	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Create or update a prevent list set",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if scaffold {
				return printExport(preventListSetExport{})
			}
			ctx := cmd.Context()
			data, err := readInput(fromFile)
			if err != nil {
				return err
			}

			var export preventListSetExport
			if err := unmarshalInput(data, &export); err != nil {
				return fmt.Errorf("parsing input: %w", err)
			}
			if export.Name == "" {
				return fmt.Errorf("input must include a 'name' field")
			}

			r := protect.NewResolver(cliCtx.ProtectClient)
			input, err := preventListSetExportToInput(ctx, export, r)
			if err != nil {
				return err
			}

			uuid, err := r.ResolvePreventListSetUUID(ctx, input.Name)
			if err != nil {
				if !errors.Is(err, protect.ErrNotFound) {
					return err
				}
				result, err := cliCtx.ProtectClient.CreatePreventListSet(ctx, input)
				if err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "Created prevent list set %q\n", input.Name)
				return printResult(cliCtx.Output, result, flattenPreventListSet(result))
			}

			proceed, err := confirmReplace("prevent list set", input.Name, yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			result, err := cliCtx.ProtectClient.UpdatePreventListSet(ctx, uuid, input)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Updated prevent list set %q\n", input.Name)
			return printResult(cliCtx.Output, result, flattenPreventListSet(result))
		},
	}

	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON input file (or pipe JSON to stdin)")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt when replacing")
	cmd.Flags().BoolVar(&scaffold, "scaffold", false, "Print an empty JSON template and exit")
	return cmd
}

func newProtectPreventListSetsDeleteCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:         "delete <name>",
		Short:       "Delete a prevent list set",
		Annotations: map[string]string{"jamf:destructive": "true"},
		Args:        cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)
			uuid, err := r.ResolvePreventListSetUUID(ctx, args[0])
			if err != nil {
				return err
			}
			proceed, err := confirmDelete("prevent list set", args[0], yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}
			if err := cliCtx.ProtectClient.DeletePreventListSet(ctx, uuid); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Deleted prevent list set %q\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	return cmd
}

func newProtectPreventListSetsAddListCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var listName string
	cmd := &cobra.Command{
		Use:   "add-list <set-name>",
		Short: "Add a custom prevent list to a set",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)
			setUUID, err := r.ResolvePreventListSetUUID(ctx, args[0])
			if err != nil {
				return err
			}
			listID, err := r.ResolveCustomPreventListID(ctx, listName)
			if err != nil {
				return err
			}
			set, err := cliCtx.ProtectClient.GetPreventListSet(ctx, setUUID)
			if err != nil {
				return err
			}
			ids := make([]string, 0, len(set.PreventLists)+1)
			for _, l := range set.PreventLists {
				if l.ID == listID {
					fmt.Fprintf(os.Stderr, "Custom prevent list %q is already in set %q\n", listName, args[0])
					return nil
				}
				ids = append(ids, l.ID)
			}
			ids = append(ids, listID)
			result, err := cliCtx.ProtectClient.UpdatePreventListSet(ctx, setUUID, jamfprotect.PreventListSetInput{
				Name:         set.Name,
				Description:  set.Description,
				PreventLists: ids,
			})
			if err != nil {
				return err
			}
			return printResult(cliCtx.Output, result, flattenPreventListSet(result))
		},
	}
	cmd.Flags().StringVar(&listName, "list", "", "Name of the custom prevent list to add")
	_ = cmd.MarkFlagRequired("list")
	return cmd
}

func newProtectPreventListSetsRemoveListCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var listName string
	cmd := &cobra.Command{
		Use:   "remove-list <set-name>",
		Short: "Remove a custom prevent list from a set",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)
			setUUID, err := r.ResolvePreventListSetUUID(ctx, args[0])
			if err != nil {
				return err
			}
			listID, err := r.ResolveCustomPreventListID(ctx, listName)
			if err != nil {
				return err
			}
			set, err := cliCtx.ProtectClient.GetPreventListSet(ctx, setUUID)
			if err != nil {
				return err
			}
			ids := make([]string, 0, len(set.PreventLists))
			for _, l := range set.PreventLists {
				if l.ID != listID {
					ids = append(ids, l.ID)
				}
			}
			// ids is non-nil even when empty, so the SDK sends [] and clears.
			result, err := cliCtx.ProtectClient.UpdatePreventListSet(ctx, setUUID, jamfprotect.PreventListSetInput{
				Name:         set.Name,
				Description:  set.Description,
				PreventLists: ids,
			})
			if err != nil {
				return err
			}
			return printResult(cliCtx.Output, result, flattenPreventListSet(result))
		},
	}
	cmd.Flags().StringVar(&listName, "list", "", "Name of the custom prevent list to remove")
	_ = cmd.MarkFlagRequired("list")
	return cmd
}

func newProtectPreventListSetsExportCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "export <name>",
		Short: "Export a prevent list set as JSON or YAML",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)
			uuid, err := r.ResolvePreventListSetUUID(ctx, args[0])
			if err != nil {
				return err
			}
			item, err := cliCtx.ProtectClient.GetPreventListSet(ctx, uuid)
			if err != nil {
				return err
			}
			return printExport(preventListSetToExport(item))
		},
	}
}

// preventListSetExport is the portable export/import format. Lists are named,
// not identified by ID, so a document applies to another tenant. The read-only
// plans back-reference is left out.
type preventListSetExport struct {
	Name         string   `json:"name" yaml:"name"`
	Description  string   `json:"description" yaml:"description"`
	PreventLists []string `json:"preventLists" yaml:"preventLists"`
}

func preventListSetToExport(s *jamfprotect.PreventListSet) preventListSetExport {
	names := make([]string, len(s.PreventLists))
	for i, l := range s.PreventLists {
		names[i] = l.Name
	}
	// Server order is its own; sort so backups diff cleanly.
	sort.Strings(names)
	return preventListSetExport{Name: s.Name, Description: s.Description, PreventLists: names}
}

func preventListSetExportToInput(ctx context.Context, e preventListSetExport, r *protect.Resolver) (jamfprotect.PreventListSetInput, error) {
	// Non-nil even when empty: a set's own membership is always sent, so an
	// omitted list clears it (same contract as ulfs and analytic-sets).
	ids := make([]string, len(e.PreventLists))
	for i, name := range e.PreventLists {
		id, err := r.ResolveCustomPreventListID(ctx, name)
		if err != nil {
			return jamfprotect.PreventListSetInput{}, fmt.Errorf("resolving custom prevent list %q: %w", name, err)
		}
		ids[i] = id
	}
	return jamfprotect.PreventListSetInput{Name: e.Name, Description: e.Description, PreventLists: ids}, nil
}
