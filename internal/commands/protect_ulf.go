// Copyright 2026, Jamf Software LLC

package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/Jamf-Concepts/jamf-cli/internal/protect"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfprotect-go-sdk/jamfprotect"
)

// unifiedLoggingFilterYAML is the community YAML schema for unified logging filters.
type unifiedLoggingFilterYAML struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Predicate   string   `yaml:"predicate"`
	Tags        []string `yaml:"tags"`
	Enabled     bool     `yaml:"enabled"`
}

func newProtectUnifiedLoggingFiltersCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unified-logging-filters",
		Short: "Manage Jamf Protect unified logging filters",
	}

	cmd.AddCommand(newProtectULFListCmd(cliCtx))
	cmd.AddCommand(newProtectULFGetCmd(cliCtx))
	cmd.AddCommand(newProtectULFApplyCmd(cliCtx))
	cmd.AddCommand(newProtectULFDeleteCmd(cliCtx))
	cmd.AddCommand(newProtectULFImportCmd(cliCtx))
	cmd.AddCommand(newProtectULFExportCmd(cliCtx))

	return cmd
}

func newProtectULFListCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all unified logging filters",
		RunE: func(cmd *cobra.Command, _ []string) error {
			filters, err := cliCtx.ProtectClient.ListUnifiedLoggingFilters(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(filters))
			for _, f := range filters {
				rows = append(rows, flattenULF(f))
			}
			data, err := json.Marshal(rows)
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}
}

// flattenULF converts a UnifiedLoggingFilter into a clean map for readable
// table output, reducing the set membership list to names.
func flattenULF(f jamfprotect.UnifiedLoggingFilter) map[string]any {
	m := map[string]any{
		"name":    f.Name,
		"enabled": f.Enabled,
		"filter":  f.Filter,
	}
	if len(f.Sets) > 0 {
		names := make([]string, 0, len(f.Sets))
		for _, s := range f.Sets {
			names = append(names, s.Name)
		}
		m["sets"] = strings.Join(names, ", ")
	}
	return m
}

func newProtectULFGetCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get <name>",
		Short: "Get a unified logging filter by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)

			uuid, err := r.ResolveUnifiedLoggingFilterUUID(ctx, args[0])
			if err != nil {
				return err
			}

			filter, err := cliCtx.ProtectClient.GetUnifiedLoggingFilter(ctx, uuid)
			if err != nil {
				return err
			}
			return printResult(cliCtx.Output, filter, flattenULF(*filter))
		},
	}
}

func newProtectULFApplyCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		fromFile string
		yes      bool
		scaffold bool
	)

	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Create or update a unified logging filter",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if scaffold {
				return printExport(jamfprotect.UnifiedLoggingFilterInput{})
			}
			ctx := cmd.Context()
			data, err := readInput(fromFile)
			if err != nil {
				return err
			}

			var input jamfprotect.UnifiedLoggingFilterInput
			if err := unmarshalInput(data, &input); err != nil {
				return fmt.Errorf("parsing input: %w", err)
			}

			if input.Name == "" {
				return fmt.Errorf("input must include a 'Name' field")
			}

			// Check if unified logging filter exists by name
			r := protect.NewResolver(cliCtx.ProtectClient)
			uuid, err := r.ResolveUnifiedLoggingFilterUUID(ctx, input.Name)
			if err != nil {
				// Not found — create
				result, err := cliCtx.ProtectClient.CreateUnifiedLoggingFilter(ctx, input)
				if err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "Created unified logging filter %q\n", input.Name)
				return printResult(cliCtx.Output, result, flattenULF(result))
			}

			// Found — confirm before replacing
			proceed, err := confirmReplace("unified logging filter", input.Name, yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			result, err := cliCtx.ProtectClient.UpdateUnifiedLoggingFilter(ctx, uuid, input)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Updated unified logging filter %q\n", input.Name)
			return printResult(cliCtx.Output, result, flattenULF(result))
		},
	}

	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON input file (or pipe JSON to stdin)")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt when replacing")
	cmd.Flags().BoolVar(&scaffold, "scaffold", false, "Print an empty JSON template and exit")

	return cmd
}

func newProtectULFDeleteCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:         "delete <name>",
		Short:       "Delete a unified logging filter",
		Annotations: map[string]string{"jamf:destructive": "true"},
		Args:        cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)

			uuid, err := r.ResolveUnifiedLoggingFilterUUID(ctx, args[0])
			if err != nil {
				return err
			}

			proceed, err := confirmDelete("unified logging filter", args[0], yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			if err := cliCtx.ProtectClient.DeleteUnifiedLoggingFilter(ctx, uuid); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Deleted unified logging filter %q\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	return cmd
}

func newProtectULFImportCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		file string
		dir  string
	)

	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import unified logging filters from YAML files",
		Long: `Import unified logging filters from YAML files. Existing filters (matched by
name) are updated; new filters are created.

Use --file for a single YAML file or --dir for a directory of YAML files.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			var files []string
			if file != "" {
				files = append(files, file)
			} else {
				err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
					if err != nil {
						return err
					}
					if !info.IsDir() && (strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml")) {
						files = append(files, path)
					}
					return nil
				})
				if err != nil {
					return fmt.Errorf("walking directory: %w", err)
				}
			}

			if len(files) == 0 {
				return fmt.Errorf("no YAML files found")
			}

			// Build name->UUID map for upsert detection
			existing, err := cliCtx.ProtectClient.ListUnifiedLoggingFilters(ctx)
			if err != nil {
				return fmt.Errorf("listing existing filters: %w", err)
			}
			nameToUUID := make(map[string]string, len(existing))
			for _, f := range existing {
				nameToUUID[f.Name] = f.UUID
			}

			for _, f := range files {
				data, err := os.ReadFile(f)
				if err != nil {
					return fmt.Errorf("reading %s: %w", f, err)
				}

				var uy unifiedLoggingFilterYAML
				if err := yaml.Unmarshal(data, &uy); err != nil {
					return fmt.Errorf("parsing %s: %w", f, err)
				}

				input := ulfYAMLToInput(uy)

				if uuid, ok := nameToUUID[uy.Name]; ok {
					if _, err := cliCtx.ProtectClient.UpdateUnifiedLoggingFilter(ctx, uuid, input); err != nil {
						return fmt.Errorf("updating filter %q from %s: %w", uy.Name, f, err)
					}
					fmt.Fprintf(os.Stderr, "Updated unified logging filter %q\n", uy.Name)
				} else {
					created, err := cliCtx.ProtectClient.CreateUnifiedLoggingFilter(ctx, input)
					if err != nil {
						return fmt.Errorf("creating filter %q from %s: %w", uy.Name, f, err)
					}
					nameToUUID[uy.Name] = created.UUID
					fmt.Fprintf(os.Stderr, "Created unified logging filter %q\n", uy.Name)
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&file, "file", "", "Path to a single YAML file")
	cmd.Flags().StringVar(&dir, "dir", "", "Path to a directory of YAML files")
	cmd.MarkFlagsMutuallyExclusive("file", "dir")
	cmd.MarkFlagsOneRequired("file", "dir")

	return cmd
}

func newProtectULFExportCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "export <name>",
		Short: "Export a unified logging filter to YAML",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)

			uuid, err := r.ResolveUnifiedLoggingFilterUUID(ctx, args[0])
			if err != nil {
				return err
			}

			filter, err := cliCtx.ProtectClient.GetUnifiedLoggingFilter(ctx, uuid)
			if err != nil {
				return err
			}

			uy := ulfToYAML(*filter)

			data, err := yaml.Marshal(uy)
			if err != nil {
				return fmt.Errorf("marshalling YAML: %w", err)
			}

			fmt.Print(string(data))
			return nil
		},
	}
}

// ulfYAMLToInput converts the community YAML schema to an SDK UnifiedLoggingFilterInput.
func ulfYAMLToInput(uy unifiedLoggingFilterYAML) jamfprotect.UnifiedLoggingFilterInput {
	return jamfprotect.UnifiedLoggingFilterInput{
		Name:        uy.Name,
		Description: uy.Description,
		Tags:        uy.Tags,
		Filter:      uy.Predicate,
		Enabled:     uy.Enabled,
	}
}

// ulfToYAML converts an SDK UnifiedLoggingFilter to the community YAML schema.
func ulfToYAML(f jamfprotect.UnifiedLoggingFilter) unifiedLoggingFilterYAML {
	return unifiedLoggingFilterYAML{
		Name:        f.Name,
		Description: f.Description,
		Predicate:   f.Filter,
		Tags:        f.Tags,
		Enabled:     f.Enabled,
	}
}
