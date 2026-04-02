// Copyright 2026, Jamf Software LLC

package commands

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/protect"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfprotect-go-sdk/jamfprotect"
)

func newProtectTelemetryCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "telemetry",
		Short: "Manage Jamf Protect telemetry configurations",
	}

	cmd.AddCommand(newProtectTelemetryListCmd(cliCtx))
	cmd.AddCommand(newProtectTelemetryGetCmd(cliCtx))
	cmd.AddCommand(newProtectTelemetryApplyCmd(cliCtx))
	cmd.AddCommand(newProtectTelemetryDeleteCmd(cliCtx))
	cmd.AddCommand(newProtectTelemetryExportCmd(cliCtx))

	return cmd
}

func newProtectTelemetryListCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all telemetry configurations",
		RunE: func(cmd *cobra.Command, _ []string) error {
			items, err := cliCtx.ProtectClient.ListTelemetriesV2(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(items))
			for _, t := range items {
				rows = append(rows, flattenTelemetry(t))
			}
			data, err := json.Marshal(rows)
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}
}

// flattenTelemetry converts a TelemetryV2 into a clean map for readable
// table output, reducing nested slices to comma-separated strings.
func flattenTelemetry(t jamfprotect.TelemetryV2) map[string]any {
	return map[string]any{
		"name":               t.Name,
		"logFileCollection":  t.LogFileCollection,
		"performanceMetrics": t.PerformanceMetrics,
		"fileHashing":        t.FileHashing,
	}
}

func newProtectTelemetryGetCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get <name>",
		Short: "Get a telemetry configuration by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r := protect.NewResolver(cliCtx.ProtectClient)
			id, err := r.ResolveTelemetryV2ID(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			item, err := cliCtx.ProtectClient.GetTelemetryV2(cmd.Context(), id)
			if err != nil {
				return err
			}
			return printProtectResult(cliCtx.Output, item, flattenTelemetry(*item))
		},
	}
}

func newProtectTelemetryApplyCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		fromFile string
		yes      bool
	)
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Create or update a telemetry configuration",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			data, err := readProtectInput(fromFile)
			if err != nil {
				return err
			}
			var input jamfprotect.TelemetryV2Input
			if err := unmarshalProtectInput(data, &input); err != nil {
				return fmt.Errorf("parsing input JSON: %w", err)
			}

			if input.Name == "" {
				return fmt.Errorf("input must include a 'Name' field")
			}

			// Check if telemetry configuration exists by name
			r := protect.NewResolver(cliCtx.ProtectClient)
			id, err := r.ResolveTelemetryV2ID(ctx, input.Name)
			if err != nil {
				// Not found — create
				result, err := cliCtx.ProtectClient.CreateTelemetryV2(ctx, input)
				if err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "Created telemetry configuration %q\n", input.Name)
				return printProtectResult(cliCtx.Output, result, flattenTelemetry(result))
			}

			// Found — confirm before replacing
			proceed, err := confirmProtectReplace("telemetry config", input.Name, yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			result, err := cliCtx.ProtectClient.UpdateTelemetryV2(ctx, id, input)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Updated telemetry configuration %q\n", input.Name)
			return printProtectResult(cliCtx.Output, result, flattenTelemetry(result))
		},
	}
	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON input file (or pipe JSON to stdin)")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt when replacing")
	return cmd
}

func newProtectTelemetryDeleteCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a telemetry configuration",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r := protect.NewResolver(cliCtx.ProtectClient)
			id, err := r.ResolveTelemetryV2ID(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			proceed, err := confirmProtectDelete("telemetry configuration", args[0], yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			if err := cliCtx.ProtectClient.DeleteTelemetryV2(cmd.Context(), id); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Deleted telemetry configuration %q\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	return cmd
}

func newProtectTelemetryExportCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "export <name>",
		Short: "Export a telemetry configuration as JSON or YAML",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)
			id, err := r.ResolveTelemetryV2ID(ctx, args[0])
			if err != nil {
				return err
			}
			item, err := cliCtx.ProtectClient.GetTelemetryV2(ctx, id)
			if err != nil {
				return err
			}
			return printProtectExport(telemetryToInput(item))
		},
	}
}

// telemetryToInput converts a TelemetryV2 response to a TelemetryV2Input, stripping server-only fields.
func telemetryToInput(t *jamfprotect.TelemetryV2) jamfprotect.TelemetryV2Input {
	return jamfprotect.TelemetryV2Input{
		Name:               t.Name,
		Description:        t.Description,
		LogFiles:           t.LogFiles,
		LogFileCollection:  t.LogFileCollection,
		PerformanceMetrics: t.PerformanceMetrics,
		Events:             t.Events,
		FileHashing:        t.FileHashing,
	}
}
