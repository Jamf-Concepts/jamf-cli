// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/protect"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfprotect-go-sdk/jamfprotect"
)

func newProtectULFSetsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unified-logging-filter-sets",
		Short: "Manage Jamf Protect unified logging filter sets",
		Long: `Manage Jamf Protect unified logging filter sets.

A filter set groups unified logging filters so they can be assigned to plans
as a unit. Use 'protect plans apply' to attach sets to a plan.`,
	}

	cmd.AddCommand(newProtectULFSetsListCmd(cliCtx))
	cmd.AddCommand(newProtectULFSetsGetCmd(cliCtx))
	cmd.AddCommand(newProtectULFSetsApplyCmd(cliCtx))
	cmd.AddCommand(newProtectULFSetsDeleteCmd(cliCtx))
	cmd.AddCommand(newProtectULFSetsAddFilterCmd(cliCtx))
	cmd.AddCommand(newProtectULFSetsRemoveFilterCmd(cliCtx))
	cmd.AddCommand(newProtectULFSetsExportCmd(cliCtx))

	return cmd
}

func newProtectULFSetsListCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all unified logging filter sets",
		RunE: func(cmd *cobra.Command, _ []string) error {
			items, err := cliCtx.ProtectClient.ListUnifiedLoggingFilterSets(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(items))
			for _, s := range items {
				rows = append(rows, flattenULFSet(s))
			}
			data, err := json.Marshal(rows)
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}
}

// flattenULFSet converts a UnifiedLoggingFilterSet into a clean map for
// readable table output, reducing nested slices to names/counts.
func flattenULFSet(s jamfprotect.UnifiedLoggingFilterSet) map[string]any {
	filterNames := make([]string, 0, len(s.Filters))
	for _, f := range s.Filters {
		filterNames = append(filterNames, f.Name)
	}
	planNames := make([]string, 0, len(s.Plans))
	for _, p := range s.Plans {
		planNames = append(planNames, p.Name)
	}
	return map[string]any{
		"name":         s.Name,
		"description":  s.Description,
		"filtersCount": len(s.Filters),
		// filters/plans always present: table/csv columns come from row 0
		"filters": strings.Join(filterNames, ", "),
		"plans":   strings.Join(planNames, ", "),
	}
}

func newProtectULFSetsGetCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get <name>",
		Short: "Get a unified logging filter set by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)

			uuid, err := r.ResolveUnifiedLoggingFilterSetUUID(ctx, args[0])
			if err != nil {
				return err
			}
			item, err := cliCtx.ProtectClient.GetUnifiedLoggingFilterSet(ctx, uuid)
			if err != nil {
				return err
			}
			return printResult(cliCtx.Output, item, flattenULFSet(*item))
		},
	}
}

func newProtectULFSetsApplyCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		fromFile string
		yes      bool
		scaffold bool
	)

	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Create or update a unified logging filter set",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if scaffold {
				return printExport(ulfSetExport{})
			}
			ctx := cmd.Context()
			data, err := readInput(fromFile)
			if err != nil {
				return err
			}

			var export ulfSetExport
			if err := unmarshalInput(data, &export); err != nil {
				return fmt.Errorf("parsing input: %w", err)
			}

			if export.Name == "" {
				return fmt.Errorf("input must include a 'name' field")
			}

			r := protect.NewResolver(cliCtx.ProtectClient)

			// Resolve filter names to UUIDs
			input, err := ulfSetExportToInput(ctx, export, r)
			if err != nil {
				return err
			}

			// Check if the set exists by name
			uuid, err := r.ResolveUnifiedLoggingFilterSetUUID(ctx, input.Name)
			if err != nil {
				// Not found — create
				result, err := cliCtx.ProtectClient.CreateUnifiedLoggingFilterSet(ctx, input)
				if err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "Created unified logging filter set %q\n", input.Name)
				return printResult(cliCtx.Output, result, flattenULFSet(result))
			}

			// Found — confirm before replacing
			proceed, err := confirmReplace("unified logging filter set", input.Name, yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			result, err := cliCtx.ProtectClient.UpdateUnifiedLoggingFilterSet(ctx, uuid, input)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Updated unified logging filter set %q\n", input.Name)
			return printResult(cliCtx.Output, result, flattenULFSet(result))
		},
	}

	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON input file (or pipe JSON to stdin)")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt when replacing")
	cmd.Flags().BoolVar(&scaffold, "scaffold", false, "Print an empty JSON template and exit")

	return cmd
}

func newProtectULFSetsDeleteCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:         "delete <name>",
		Short:       "Delete a unified logging filter set",
		Annotations: map[string]string{"jamf:destructive": "true"},
		Args:        cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)

			uuid, err := r.ResolveUnifiedLoggingFilterSetUUID(ctx, args[0])
			if err != nil {
				return err
			}

			proceed, err := confirmDelete("unified logging filter set", args[0], yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			if err := cliCtx.ProtectClient.DeleteUnifiedLoggingFilterSet(ctx, uuid); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Deleted unified logging filter set %q\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	return cmd
}

func newProtectULFSetsAddFilterCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var filterName string
	cmd := &cobra.Command{
		Use:   "add-filter <set-name>",
		Short: "Add a unified logging filter to a set",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)

			setUUID, err := r.ResolveUnifiedLoggingFilterSetUUID(ctx, args[0])
			if err != nil {
				return err
			}
			filterUUID, err := r.ResolveUnifiedLoggingFilterUUID(ctx, filterName)
			if err != nil {
				return err
			}

			set, err := cliCtx.ProtectClient.GetUnifiedLoggingFilterSet(ctx, setUUID)
			if err != nil {
				return err
			}

			// Build the updated filters list, skipping if already present
			uuids := make([]string, 0, len(set.Filters)+1)
			alreadyPresent := false
			for _, f := range set.Filters {
				uuids = append(uuids, f.UUID)
				if f.UUID == filterUUID {
					alreadyPresent = true
				}
			}
			if alreadyPresent {
				fmt.Fprintf(os.Stderr, "Unified logging filter %q is already in set %q\n", filterName, args[0])
				return nil
			}
			uuids = append(uuids, filterUUID)

			input := jamfprotect.UnifiedLoggingFilterSetInput{
				Name:        set.Name,
				Description: set.Description,
				Filters:     uuids,
			}
			result, err := cliCtx.ProtectClient.UpdateUnifiedLoggingFilterSet(ctx, setUUID, input)
			if err != nil {
				return err
			}
			return printResult(cliCtx.Output, result, flattenULFSet(result))
		},
	}
	cmd.Flags().StringVar(&filterName, "filter", "", "Name of the unified logging filter to add")
	_ = cmd.MarkFlagRequired("filter")
	return cmd
}

func newProtectULFSetsRemoveFilterCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var filterName string
	cmd := &cobra.Command{
		Use:   "remove-filter <set-name>",
		Short: "Remove a unified logging filter from a set",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)

			setUUID, err := r.ResolveUnifiedLoggingFilterSetUUID(ctx, args[0])
			if err != nil {
				return err
			}
			filterUUID, err := r.ResolveUnifiedLoggingFilterUUID(ctx, filterName)
			if err != nil {
				return err
			}

			set, err := cliCtx.ProtectClient.GetUnifiedLoggingFilterSet(ctx, setUUID)
			if err != nil {
				return err
			}

			// Build the updated filters list without the target
			uuids := make([]string, 0, len(set.Filters))
			for _, f := range set.Filters {
				if f.UUID != filterUUID {
					uuids = append(uuids, f.UUID)
				}
			}

			input := jamfprotect.UnifiedLoggingFilterSetInput{
				Name:        set.Name,
				Description: set.Description,
				Filters:     uuids,
			}
			result, err := cliCtx.ProtectClient.UpdateUnifiedLoggingFilterSet(ctx, setUUID, input)
			if err != nil {
				return err
			}
			return printResult(cliCtx.Output, result, flattenULFSet(result))
		},
	}
	cmd.Flags().StringVar(&filterName, "filter", "", "Name of the unified logging filter to remove")
	_ = cmd.MarkFlagRequired("filter")
	return cmd
}

func newProtectULFSetsExportCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "export <name>",
		Short: "Export a unified logging filter set as JSON or YAML",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)
			uuid, err := r.ResolveUnifiedLoggingFilterSetUUID(ctx, args[0])
			if err != nil {
				return err
			}
			item, err := cliCtx.ProtectClient.GetUnifiedLoggingFilterSet(ctx, uuid)
			if err != nil {
				return err
			}
			return printExport(ulfSetToExport(item))
		},
	}
}

// ulfSetExport is the human-friendly export/import format for unified logging
// filter sets. Uses filter names instead of UUIDs so files are portable across
// tenants.
type ulfSetExport struct {
	Name        string   `json:"name" yaml:"name"`
	Description string   `json:"description" yaml:"description"`
	Filters     []string `json:"filters" yaml:"filters"`
}

// ulfSetToExport converts a UnifiedLoggingFilterSet API response to the export
// format with names.
func ulfSetToExport(s *jamfprotect.UnifiedLoggingFilterSet) ulfSetExport {
	names := make([]string, len(s.Filters))
	for i, f := range s.Filters {
		names[i] = f.Name
	}
	return ulfSetExport{
		Name:        s.Name,
		Description: s.Description,
		Filters:     names,
	}
}

// ulfSetExportToInput resolves filter names to UUIDs and builds a
// UnifiedLoggingFilterSetInput.
func ulfSetExportToInput(ctx context.Context, e ulfSetExport, r *protect.Resolver) (jamfprotect.UnifiedLoggingFilterSetInput, error) {
	uuids := make([]string, len(e.Filters))
	for i, name := range e.Filters {
		uuid, err := r.ResolveUnifiedLoggingFilterUUID(ctx, name)
		if err != nil {
			return jamfprotect.UnifiedLoggingFilterSetInput{}, fmt.Errorf("resolving unified logging filter %q: %w", name, err)
		}
		uuids[i] = uuid
	}
	return jamfprotect.UnifiedLoggingFilterSetInput{
		Name:        e.Name,
		Description: e.Description,
		Filters:     uuids,
	}, nil
}
