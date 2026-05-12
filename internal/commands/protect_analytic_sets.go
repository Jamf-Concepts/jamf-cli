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

func newProtectAnalyticSetsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "analytic-sets",
		Short: "Manage Jamf Protect analytic sets",
	}

	cmd.AddCommand(newProtectAnalyticSetsListCmd(cliCtx))
	cmd.AddCommand(newProtectAnalyticSetsGetCmd(cliCtx))
	cmd.AddCommand(newProtectAnalyticSetsApplyCmd(cliCtx))
	cmd.AddCommand(newProtectAnalyticSetsDeleteCmd(cliCtx))
	cmd.AddCommand(newProtectAnalyticSetsAddAnalyticCmd(cliCtx))
	cmd.AddCommand(newProtectAnalyticSetsRemoveAnalyticCmd(cliCtx))
	cmd.AddCommand(newProtectAnalyticSetsExportCmd(cliCtx))

	return cmd
}

func newProtectAnalyticSetsListCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all analytic sets",
		RunE: func(cmd *cobra.Command, _ []string) error {
			items, err := cliCtx.ProtectClient.ListAnalyticSets(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(items))
			for _, s := range items {
				rows = append(rows, flattenAnalyticSet(s))
			}
			data, err := json.Marshal(rows)
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}
}

// flattenAnalyticSet converts an AnalyticSet into a clean map for readable
// table output, reducing nested slices to names/counts.
func flattenAnalyticSet(s jamfprotect.AnalyticSet) map[string]any {
	m := map[string]any{
		"name":           s.Name,
		"description":    s.Description,
		"analyticsCount": len(s.Analytics),
		"managed":        s.Managed,
		"types":          strings.Join(s.Types, ", "),
	}
	if len(s.Plans) > 0 {
		names := make([]string, 0, len(s.Plans))
		for _, p := range s.Plans {
			names = append(names, p.Name)
		}
		m["plans"] = strings.Join(names, ", ")
	}
	return m
}

func newProtectAnalyticSetsGetCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get <name>",
		Short: "Get an analytic set by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r := protect.NewResolver(cliCtx.ProtectClient)
			uuid, err := r.ResolveAnalyticSetUUID(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			item, err := cliCtx.ProtectClient.GetAnalyticSet(cmd.Context(), uuid)
			if err != nil {
				return err
			}
			return printResult(cliCtx.Output, item, flattenAnalyticSet(*item))
		},
	}
}

func newProtectAnalyticSetsApplyCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		fromFile string
		yes      bool
		scaffold bool
	)
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Create or update an analytic set",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if scaffold {
				return printExport(analyticSetExport{})
			}
			ctx := cmd.Context()
			data, err := readInput(fromFile)
			if err != nil {
				return err
			}
			var export analyticSetExport
			if err := unmarshalInput(data, &export); err != nil {
				return fmt.Errorf("parsing input: %w", err)
			}

			if export.Name == "" {
				return fmt.Errorf("input must include a 'name' field")
			}

			r := protect.NewResolver(cliCtx.ProtectClient)

			// Resolve analytic names to UUIDs
			input, err := analyticSetExportToInput(ctx, export, r)
			if err != nil {
				return err
			}

			// Check if analytic set exists by name
			uuid, err := r.ResolveAnalyticSetUUID(ctx, input.Name)
			if err != nil {
				// Not found — create
				result, err := cliCtx.ProtectClient.CreateAnalyticSet(ctx, input)
				if err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "Created analytic set %q\n", input.Name)
				return printResult(cliCtx.Output, result, flattenAnalyticSet(result))
			}

			// Found — confirm before replacing
			proceed, err := confirmReplace("analytic set", input.Name, yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			result, err := cliCtx.ProtectClient.UpdateAnalyticSet(ctx, uuid, input)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Updated analytic set %q\n", input.Name)
			return printResult(cliCtx.Output, result, flattenAnalyticSet(result))
		},
	}
	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON input file (or pipe JSON to stdin)")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt when replacing")
	cmd.Flags().BoolVar(&scaffold, "scaffold", false, "Print an empty JSON template and exit")
	return cmd
}

func newProtectAnalyticSetsDeleteCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:         "delete <name>",
		Short:       "Delete an analytic set",
		Annotations: map[string]string{"jamf:destructive": "true"},
		Args:        cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r := protect.NewResolver(cliCtx.ProtectClient)
			uuid, err := r.ResolveAnalyticSetUUID(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			proceed, err := confirmDelete("analytic set", args[0], yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			if err := cliCtx.ProtectClient.DeleteAnalyticSet(cmd.Context(), uuid); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Deleted analytic set %q\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	return cmd
}

func newProtectAnalyticSetsAddAnalyticCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var analyticName string
	cmd := &cobra.Command{
		Use:   "add-analytic <set-name>",
		Short: "Add an analytic to a set",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)

			setUUID, err := r.ResolveAnalyticSetUUID(ctx, args[0])
			if err != nil {
				return err
			}
			analyticUUID, err := r.ResolveAnalyticUUID(ctx, analyticName)
			if err != nil {
				return err
			}

			set, err := cliCtx.ProtectClient.GetAnalyticSet(ctx, setUUID)
			if err != nil {
				return err
			}

			// Build the updated analytics list, skipping if already present
			uuids := make([]string, 0, len(set.Analytics)+1)
			alreadyPresent := false
			for _, a := range set.Analytics {
				uuids = append(uuids, a.UUID)
				if a.UUID == analyticUUID {
					alreadyPresent = true
				}
			}
			if alreadyPresent {
				fmt.Fprintf(os.Stderr, "Analytic %q is already in set %q\n", analyticName, args[0])
				return nil
			}
			uuids = append(uuids, analyticUUID)

			input := jamfprotect.AnalyticSetInput{
				Name:        set.Name,
				Description: set.Description,
				Types:       set.Types,
				Analytics:   uuids,
			}
			result, err := cliCtx.ProtectClient.UpdateAnalyticSet(ctx, setUUID, input)
			if err != nil {
				return err
			}
			return printResult(cliCtx.Output, result, flattenAnalyticSet(result))
		},
	}
	cmd.Flags().StringVar(&analyticName, "analytic", "", "Name of the analytic to add")
	_ = cmd.MarkFlagRequired("analytic")
	return cmd
}

func newProtectAnalyticSetsRemoveAnalyticCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var analyticName string
	cmd := &cobra.Command{
		Use:   "remove-analytic <set-name>",
		Short: "Remove an analytic from a set",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)

			setUUID, err := r.ResolveAnalyticSetUUID(ctx, args[0])
			if err != nil {
				return err
			}
			analyticUUID, err := r.ResolveAnalyticUUID(ctx, analyticName)
			if err != nil {
				return err
			}

			set, err := cliCtx.ProtectClient.GetAnalyticSet(ctx, setUUID)
			if err != nil {
				return err
			}

			// Build the updated analytics list without the target
			uuids := make([]string, 0, len(set.Analytics))
			for _, a := range set.Analytics {
				if a.UUID != analyticUUID {
					uuids = append(uuids, a.UUID)
				}
			}

			input := jamfprotect.AnalyticSetInput{
				Name:        set.Name,
				Description: set.Description,
				Types:       set.Types,
				Analytics:   uuids,
			}
			result, err := cliCtx.ProtectClient.UpdateAnalyticSet(ctx, setUUID, input)
			if err != nil {
				return err
			}
			return printResult(cliCtx.Output, result, flattenAnalyticSet(result))
		},
	}
	cmd.Flags().StringVar(&analyticName, "analytic", "", "Name of the analytic to remove")
	_ = cmd.MarkFlagRequired("analytic")
	return cmd
}

func newProtectAnalyticSetsExportCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "export <name>",
		Short: "Export an analytic set as JSON or YAML",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)
			uuid, err := r.ResolveAnalyticSetUUID(ctx, args[0])
			if err != nil {
				return err
			}
			item, err := cliCtx.ProtectClient.GetAnalyticSet(ctx, uuid)
			if err != nil {
				return err
			}
			return printExport(analyticSetToExport(item))
		},
	}
}

// analyticSetExport is the human-friendly export/import format for analytic sets.
// Uses analytic names instead of UUIDs so files are portable across tenants.
type analyticSetExport struct {
	Name        string   `json:"name" yaml:"name"`
	Description string   `json:"description" yaml:"description"`
	Types       []string `json:"types" yaml:"types"`
	Analytics   []string `json:"analytics" yaml:"analytics"`
}

// analyticSetToExport converts an AnalyticSet API response to the export format with names.
func analyticSetToExport(s *jamfprotect.AnalyticSet) analyticSetExport {
	names := make([]string, len(s.Analytics))
	for i, a := range s.Analytics {
		names[i] = a.Name
	}
	return analyticSetExport{
		Name:        s.Name,
		Description: s.Description,
		Types:       s.Types,
		Analytics:   names,
	}
}

// analyticSetExportToInput resolves analytic names to UUIDs and builds an AnalyticSetInput.
func analyticSetExportToInput(ctx context.Context, e analyticSetExport, r *protect.Resolver) (jamfprotect.AnalyticSetInput, error) {
	uuids := make([]string, len(e.Analytics))
	for i, name := range e.Analytics {
		uuid, err := r.ResolveAnalyticUUID(ctx, name)
		if err != nil {
			return jamfprotect.AnalyticSetInput{}, fmt.Errorf("resolving analytic %q: %w", name, err)
		}
		uuids[i] = uuid
	}
	return jamfprotect.AnalyticSetInput{
		Name:        e.Name,
		Description: e.Description,
		Types:       e.Types,
		Analytics:   uuids,
	}, nil
}
