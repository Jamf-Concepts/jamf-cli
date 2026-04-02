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

// analyticYAML is the community YAML schema for analytics.
type analyticYAML struct {
	Name             string                `yaml:"name"`
	LongDescription  string                `yaml:"longDescription"`
	Level            int64                 `yaml:"level"`
	InputType        string                `yaml:"inputType"`
	Tags             []string              `yaml:"tags"`
	SnapshotFiles    []string              `yaml:"snapshotFiles"`
	Filter           string                `yaml:"filter"`
	Actions          []analyticActionYAML  `yaml:"actions"`
	Context          []analyticContextYAML `yaml:"context"`
	Categories       []string              `yaml:"categories"`
	Severity         string                `yaml:"severity"`
	ShortDescription string                `yaml:"shortDescription"`
	Remediation      string                `yaml:"remediation,omitempty"`
}

// analyticActionYAML represents an action in the YAML schema.
type analyticActionYAML struct {
	Name       string `yaml:"name"`
	Parameters string `yaml:"parameters,omitempty"`
}

// analyticContextYAML represents a context entry in the YAML schema.
type analyticContextYAML struct {
	Name  string   `yaml:"name"`
	Type  string   `yaml:"type"`
	Exprs []string `yaml:"exprs"`
}

func newProtectAnalyticsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "analytics",
		Short: "Manage Jamf Protect analytics",
	}

	cmd.AddCommand(newProtectAnalyticsListCmd(cliCtx))
	cmd.AddCommand(newProtectAnalyticsGetCmd(cliCtx))
	cmd.AddCommand(newProtectAnalyticsApplyCmd(cliCtx))
	cmd.AddCommand(newProtectAnalyticsDeleteCmd(cliCtx))
	cmd.AddCommand(newProtectAnalyticsImportCmd(cliCtx))
	cmd.AddCommand(newProtectAnalyticsExportCmd(cliCtx))

	return cmd
}

func newProtectAnalyticsListCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all analytics",
		RunE: func(cmd *cobra.Command, _ []string) error {
			analytics, err := cliCtx.ProtectClient.ListAnalytics(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(analytics))
			for _, a := range analytics {
				rows = append(rows, flattenAnalytic(a))
			}
			data, err := json.Marshal(rows)
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}
}

func newProtectAnalyticsGetCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get <name>",
		Short: "Get an analytic by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)

			uuid, err := r.ResolveAnalyticUUID(ctx, args[0])
			if err != nil {
				return err
			}

			analytic, err := cliCtx.ProtectClient.GetAnalytic(ctx, uuid)
			if err != nil {
				return err
			}
			return printProtectResult(cliCtx.Output, analytic, flattenAnalytic(*analytic))
		},
	}
}

func newProtectAnalyticsApplyCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		fromFile string
		yes      bool
	)

	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Create or update an analytic",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			data, err := readProtectInput(fromFile)
			if err != nil {
				return err
			}

			var input jamfprotect.AnalyticInput
			if err := unmarshalProtectInput(data, &input); err != nil {
				return fmt.Errorf("parsing input: %w", err)
			}

			if input.Name == "" {
				return fmt.Errorf("input must include a 'Name' field")
			}

			// Check if analytic exists by name
			r := protect.NewResolver(cliCtx.ProtectClient)
			uuid, err := r.ResolveAnalyticUUID(ctx, input.Name)
			if err != nil {
				// Not found — create
				result, err := cliCtx.ProtectClient.CreateAnalytic(ctx, input)
				if err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "Created analytic %q\n", input.Name)
				return printProtectResult(cliCtx.Output, result, flattenAnalytic(result))
			}

			// Found — confirm before replacing
			proceed, err := confirmProtectReplace("analytic", input.Name, yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			result, err := cliCtx.ProtectClient.UpdateAnalytic(ctx, uuid, input)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Updated analytic %q\n", input.Name)
			return printProtectResult(cliCtx.Output, result, flattenAnalytic(result))
		},
	}

	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON input file (or pipe JSON to stdin)")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt when replacing")

	return cmd
}

func newProtectAnalyticsDeleteCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete an analytic",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)

			uuid, err := r.ResolveAnalyticUUID(ctx, args[0])
			if err != nil {
				return err
			}

			proceed, err := confirmProtectDelete("analytic", args[0], yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			if err := cliCtx.ProtectClient.DeleteAnalytic(ctx, uuid); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Deleted analytic %q\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	return cmd
}

func newProtectAnalyticsImportCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		file string
		dir  string
	)

	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import analytics from YAML files",
		Long: `Import analytics from YAML files. Existing analytics (matched by name) are
updated; new analytics are created.

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
			existing, err := cliCtx.ProtectClient.ListAnalytics(ctx)
			if err != nil {
				return fmt.Errorf("listing existing analytics: %w", err)
			}
			nameToUUID := make(map[string]string, len(existing))
			for _, a := range existing {
				nameToUUID[a.Name] = a.UUID
			}

			for _, f := range files {
				data, err := os.ReadFile(f)
				if err != nil {
					return fmt.Errorf("reading %s: %w", f, err)
				}

				var ay analyticYAML
				if err := yaml.Unmarshal(data, &ay); err != nil {
					return fmt.Errorf("parsing %s: %w", f, err)
				}

				input := analyticYAMLToInput(ay)

				if uuid, ok := nameToUUID[ay.Name]; ok {
					if _, err := cliCtx.ProtectClient.UpdateAnalytic(ctx, uuid, input); err != nil {
						return fmt.Errorf("updating analytic %q from %s: %w", ay.Name, f, err)
					}
					fmt.Fprintf(os.Stderr, "Updated analytic %q\n", ay.Name)
				} else {
					created, err := cliCtx.ProtectClient.CreateAnalytic(ctx, input)
					if err != nil {
						return fmt.Errorf("creating analytic %q from %s: %w", ay.Name, f, err)
					}
					nameToUUID[ay.Name] = created.UUID
					fmt.Fprintf(os.Stderr, "Created analytic %q\n", ay.Name)
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

func newProtectAnalyticsExportCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "export <name>",
		Short: "Export an analytic to YAML",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)

			uuid, err := r.ResolveAnalyticUUID(ctx, args[0])
			if err != nil {
				return err
			}

			analytic, err := cliCtx.ProtectClient.GetAnalytic(ctx, uuid)
			if err != nil {
				return err
			}

			ay := analyticToYAML(*analytic)

			data, err := yaml.Marshal(ay)
			if err != nil {
				return fmt.Errorf("marshalling YAML: %w", err)
			}

			fmt.Print(string(data))
			return nil
		},
	}
}

// analyticYAMLToInput converts the community YAML schema to an SDK AnalyticInput.
func analyticYAMLToInput(ay analyticYAML) jamfprotect.AnalyticInput {
	analyticActions := make([]jamfprotect.AnalyticActionInput, 0, len(ay.Actions))
	for _, a := range ay.Actions {
		params := a.Parameters
		if params == "" {
			params = "{}"
		}
		analyticActions = append(analyticActions, jamfprotect.AnalyticActionInput{
			Name:       a.Name,
			Parameters: params,
		})
	}

	contexts := make([]jamfprotect.AnalyticContextInput, 0, len(ay.Context))
	for _, c := range ay.Context {
		contexts = append(contexts, jamfprotect.AnalyticContextInput{
			Name:  c.Name,
			Type:  c.Type,
			Exprs: c.Exprs,
		})
	}

	tags := ay.Tags
	if tags == nil {
		tags = []string{}
	}
	categories := ay.Categories
	if categories == nil {
		categories = []string{}
	}
	snapshotFiles := ay.SnapshotFiles
	if snapshotFiles == nil {
		snapshotFiles = []string{}
	}

	return jamfprotect.AnalyticInput{
		Name:            ay.Name,
		InputType:       ay.InputType,
		Description:     ay.ShortDescription,
		Actions:         nil,
		AnalyticActions: analyticActions,
		Tags:            tags,
		Categories:      categories,
		Filter:          ay.Filter,
		Context:         contexts,
		Level:           ay.Level,
		Severity:        ay.Severity,
		SnapshotFiles:   snapshotFiles,
	}
}

// flattenAnalytic converts an Analytic to a clean map for list table output.
func flattenAnalytic(a jamfprotect.Analytic) map[string]any {
	m := map[string]any{
		"name":      a.Name,
		"severity":  a.Severity,
		"inputType": a.InputType,
		"jamf":      a.Jamf,
	}
	if len(a.Categories) > 0 {
		m["categories"] = strings.Join(a.Categories, ", ")
	}
	return m
}

// analyticToYAML converts an SDK Analytic to the community YAML schema.
func analyticToYAML(a jamfprotect.Analytic) analyticYAML {
	var actions []analyticActionYAML
	for _, aa := range a.AnalyticActions {
		actions = append(actions, analyticActionYAML{
			Name:       aa.Name,
			Parameters: aa.Parameters,
		})
	}

	var contexts []analyticContextYAML
	for _, c := range a.Context {
		contexts = append(contexts, analyticContextYAML{
			Name:  c.Name,
			Type:  c.Type,
			Exprs: c.Exprs,
		})
	}

	return analyticYAML{
		Name:             a.Name,
		LongDescription:  a.LongDescription,
		Level:            a.Level,
		InputType:        a.InputType,
		Tags:             a.Tags,
		SnapshotFiles:    a.SnapshotFiles,
		Filter:           a.Filter,
		Actions:          actions,
		Context:          contexts,
		Categories:       a.Categories,
		Severity:         a.Severity,
		ShortDescription: a.Description,
		Remediation:      a.Remediation,
	}
}
