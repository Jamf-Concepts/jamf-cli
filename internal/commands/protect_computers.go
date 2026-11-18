// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/listfile"
	"github.com/Jamf-Concepts/jamf-cli/internal/platform"
	"github.com/Jamf-Concepts/jamf-cli/internal/protect"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamf-cli/internal/resolve"
	"github.com/Jamf-Concepts/jamfprotect-go-sdk/jamfprotect"
)

func newProtectComputersCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "computers",
		Short: "Manage Jamf Protect computers",
	}

	cmd.AddCommand(newProtectComputersListCmd(cliCtx))
	cmd.AddCommand(newProtectComputersGetCmd(cliCtx))
	cmd.AddCommand(newProtectComputersDeleteCmd(cliCtx))
	cmd.AddCommand(newProtectComputersSetPlanCmd(cliCtx))
	cmd.AddCommand(newProtectComputersUpdateCmd(cliCtx))

	return cmd
}

func newProtectComputersListCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all computers",
		RunE: func(cmd *cobra.Command, _ []string) error {
			computers, err := cliCtx.ProtectClient.ListComputers(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(computers))
			for _, c := range computers {
				rows = append(rows, flattenComputer(c))
			}
			data, err := json.Marshal(rows)
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}
}

func newProtectComputersGetCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var listFile string
	cmd := &cobra.Command{
		Use:   "get [<uuid|serial|hostname>]",
		Short: "Get a computer by UUID, serial number, or hostname",
		Long: `Get a computer by UUID, serial number, or hostname, or every computer a
--list-file names, one per line.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)

			if listFile != "" {
				if len(args) > 0 {
					return fmt.Errorf("--list-file names the computers to get; do not pass a <uuid|serial|hostname> as well")
				}
				targets, skipped, err := resolve.ResolveEachFile(ctx, "computer", listFile, func(ctx context.Context, v string) (string, string, error) {
					uuid, err := r.ResolveComputerUUID(ctx, v)
					return uuid, "", err
				})
				if err != nil {
					return err
				}
				return platform.PrintListRecords(cmd, cliCtx.Output, targets, skipped, "protect computers entries", func(ctx context.Context, uuid string) (any, error) {
					return cliCtx.ProtectClient.GetComputer(ctx, uuid)
				})
			}
			if len(args) == 0 {
				return fmt.Errorf("provide a <uuid|serial|hostname> or --list-file")
			}
			uuid, err := r.ResolveComputerUUID(ctx, args[0])
			if err != nil {
				return err
			}

			computer, err := cliCtx.ProtectClient.GetComputer(ctx, uuid)
			if err != nil {
				return err
			}
			return protect.PrintOne(cliCtx.Output, computer)
		},
	}
	listfile.AddFlag(cmd, &listFile, "computers to get: UUID, serial number or hostname")
	return cmd
}

func newProtectComputersDeleteCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:         "delete <uuid|serial|hostname>",
		Short:       "Delete a computer by UUID, serial number, or hostname",
		Annotations: map[string]string{"jamf:destructive": "true"},
		Args:        cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)
			computer, err := r.ResolveComputer(ctx, args[0])
			if err != nil {
				return err
			}
			proceed, err := confirmDelete("computer", protect.DescribeComputer(computer), yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}
			return cliCtx.ProtectClient.DeleteComputer(ctx, *computer.UUID)
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")

	return cmd
}

func newProtectComputersSetPlanCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "set-plan <uuid|serial|hostname> <plan-name>",
		Short: "Assign a plan to a computer by UUID, serial number, or hostname",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)
			computerUUID, err := r.ResolveComputerUUID(ctx, args[0])
			if err != nil {
				return err
			}
			planID, err := r.ResolvePlanID(ctx, args[1])
			if err != nil {
				return err
			}
			computer, err := cliCtx.ProtectClient.SetComputerPlan(ctx, computerUUID, planID)
			if err != nil {
				return err
			}
			return protect.PrintOne(cliCtx.Output, computer)
		},
	}
}

func newProtectComputersUpdateCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var label string
	var tags []string

	cmd := &cobra.Command{
		Use:   "update <uuid|serial|hostname>",
		Short: "Update a computer's label and/or tags",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("label") && !cmd.Flags().Changed("tags") {
				return fmt.Errorf("at least one of --label or --tags must be specified")
			}
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)
			uuid, err := r.ResolveComputerUUID(ctx, args[0])
			if err != nil {
				return err
			}
			input := jamfprotect.ComputerUpdateInput{}
			if cmd.Flags().Changed("label") {
				input.Label = &label
			}
			if cmd.Flags().Changed("tags") {
				input.Tags = tags
			}
			computer, err := cliCtx.ProtectClient.UpdateComputer(ctx, uuid, input)
			if err != nil {
				return err
			}
			return protect.PrintOne(cliCtx.Output, computer)
		},
	}

	cmd.Flags().StringVar(&label, "label", "", "New label for the computer")
	cmd.Flags().StringArrayVar(&tags, "tags", nil, "Tags to set (replaces existing tags)")

	return cmd
}

// flattenComputer converts a Computer with pointer fields into a clean map,
// omitting nil fields and surfacing the plan name for readable output.
func flattenComputer(c jamfprotect.Computer) map[string]any {
	m := make(map[string]any)
	setStr := func(k string, v *string) {
		if v != nil {
			m[k] = *v
		}
	}
	setInt := func(k string, v *int64) {
		if v != nil {
			m[k] = *v
		}
	}

	setStr("hostname", c.HostName)
	setStr("serial", c.Serial)
	setStr("osString", c.OSString)
	setStr("modelName", c.ModelName)
	setStr("connectionStatus", c.ConnectionStatus)
	setStr("checkin", c.Checkin)
	setStr("version", c.Version)
	setStr("fullDiskAccess", c.FullDiskAccess)
	setStr("lastConnection", c.LastConnection)
	setStr("lastConnectionIp", c.LastConnectionIP)
	setStr("created", c.Created)
	setStr("updated", c.Updated)
	setStr("uuid", c.UUID)
	setStr("arch", c.Arch)
	setStr("kernelVersion", c.KernelVersion)
	setStr("lastDisconnection", c.LastDisconnection)
	setStr("lastDisconnectionReason", c.LastDisconnectionReason)
	setInt("osMajor", c.OSMajor)
	setInt("osMinor", c.OSMinor)
	setInt("osPatch", c.OSPatch)
	if c.MemorySize != nil {
		m["memorySize"] = int64(*c.MemorySize)
	}
	setInt("signaturesVersion", c.SignaturesVersion)
	setInt("insightsStatsFail", c.InsightsStatsFail)
	setStr("insightsUpdated", c.InsightsUpdated)
	if c.WebProtectionActive != nil {
		m["webProtectionActive"] = *c.WebProtectionActive
	}
	if c.Plan != nil && c.Plan.Name != nil {
		m["plan"] = *c.Plan.Name
	}
	if c.Tags != nil {
		m["tags"] = fmt.Sprintf("%v", *c.Tags)
	}

	return m
}
