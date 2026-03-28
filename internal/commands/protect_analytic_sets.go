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
			data, _ := json.Marshal(rows)
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
			return protect.PrintOne(cliCtx.Output, item)
		},
	}
}

func newProtectAnalyticSetsApplyCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		fromFile string
		yes      bool
	)
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Create or update an analytic set",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			data, err := readProtectInput(fromFile)
			if err != nil {
				return err
			}
			var input jamfprotect.AnalyticSetInput
			if err := unmarshalProtectInput(data, &input); err != nil {
				return fmt.Errorf("parsing input JSON: %w", err)
			}

			if input.Name == "" {
				return fmt.Errorf("input must include a 'Name' field")
			}

			// Check if analytic set exists by name
			r := protect.NewResolver(cliCtx.ProtectClient)
			uuid, err := r.ResolveAnalyticSetUUID(ctx, input.Name)

			if err != nil {
				// Not found — create
				result, err := cliCtx.ProtectClient.CreateAnalyticSet(ctx, input)
				if err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "Created analytic set %q\n", input.Name)
				return protect.PrintOne(cliCtx.Output, result)
			}

			// Found — confirm before replacing
			proceed, err := confirmProtectReplace("analytic set", input.Name, yes)
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
			return protect.PrintOne(cliCtx.Output, result)
		},
	}
	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON input file (or pipe JSON to stdin)")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt when replacing")
	return cmd
}

func newProtectAnalyticSetsDeleteCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete an analytic set",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r := protect.NewResolver(cliCtx.ProtectClient)
			uuid, err := r.ResolveAnalyticSetUUID(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			proceed, err := confirmProtectDelete("analytic set", args[0], yes)
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
			return protect.PrintOne(cliCtx.Output, result)
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
			return protect.PrintOne(cliCtx.Output, result)
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
			return printProtectExport(analyticSetToInput(item))
		},
	}
}

// analyticSetToInput converts an AnalyticSet response to an AnalyticSetInput, stripping server-only fields.
func analyticSetToInput(s *jamfprotect.AnalyticSet) jamfprotect.AnalyticSetInput {
	uuids := make([]string, len(s.Analytics))
	for i, a := range s.Analytics {
		uuids[i] = a.UUID
	}
	return jamfprotect.AnalyticSetInput{
		Name:        s.Name,
		Description: s.Description,
		Types:       s.Types,
		Analytics:   uuids,
	}
}
