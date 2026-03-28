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

func newProtectTelemetryCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "telemetry",
		Short: "Manage Jamf Protect telemetry configurations",
	}

	cmd.AddCommand(newProtectTelemetryListCmd(cliCtx))
	cmd.AddCommand(newProtectTelemetryGetCmd(cliCtx))
	cmd.AddCommand(newProtectTelemetryCreateCmd(cliCtx))
	cmd.AddCommand(newProtectTelemetryUpdateCmd(cliCtx))
	cmd.AddCommand(newProtectTelemetryDeleteCmd(cliCtx))

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
			data, _ := json.Marshal(rows)
			return cliCtx.Output.PrintRaw(data)
		},
	}
}

// flattenTelemetry converts a TelemetryV2 into a clean map for readable
// table output, reducing nested slices to comma-separated strings.
func flattenTelemetry(t jamfprotect.TelemetryV2) map[string]any {
	m := map[string]any{
		"name":               t.Name,
		"description":        t.Description,
		"logFileCollection":  t.LogFileCollection,
		"performanceMetrics": t.PerformanceMetrics,
		"fileHashing":        t.FileHashing,
		"created":            t.Created,
		"updated":            t.Updated,
	}
	if len(t.Plans) > 0 {
		names := make([]string, 0, len(t.Plans))
		for _, p := range t.Plans {
			names = append(names, p.Name)
		}
		m["plans"] = strings.Join(names, ", ")
	}
	if len(t.Events) > 0 {
		m["events"] = strings.Join(t.Events, ", ")
	}
	if len(t.LogFiles) > 0 {
		m["logFiles"] = strings.Join(t.LogFiles, ", ")
	}
	return m
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
			return protect.PrintOne(cliCtx.Output, item)
		},
	}
}

func newProtectTelemetryCreateCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var fromFile string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a telemetry configuration",
		RunE: func(cmd *cobra.Command, _ []string) error {
			data, err := readProtectInput(fromFile)
			if err != nil {
				return err
			}
			var input jamfprotect.TelemetryV2Input
			if err := json.Unmarshal(data, &input); err != nil {
				return fmt.Errorf("parsing input JSON: %w", err)
			}
			result, err := cliCtx.ProtectClient.CreateTelemetryV2(cmd.Context(), input)
			if err != nil {
				return err
			}
			return protect.PrintOne(cliCtx.Output, result)
		},
	}
	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON input file (or pipe JSON to stdin)")
	return cmd
}

func newProtectTelemetryUpdateCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var fromFile string
	cmd := &cobra.Command{
		Use:   "update <name>",
		Short: "Update a telemetry configuration",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r := protect.NewResolver(cliCtx.ProtectClient)
			id, err := r.ResolveTelemetryV2ID(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			data, err := readProtectInput(fromFile)
			if err != nil {
				return err
			}
			var input jamfprotect.TelemetryV2Input
			if err := json.Unmarshal(data, &input); err != nil {
				return fmt.Errorf("parsing input JSON: %w", err)
			}
			result, err := cliCtx.ProtectClient.UpdateTelemetryV2(cmd.Context(), id, input)
			if err != nil {
				return err
			}
			return protect.PrintOne(cliCtx.Output, result)
		},
	}
	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON input file (or pipe JSON to stdin)")
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
