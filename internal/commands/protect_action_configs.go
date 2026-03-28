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

func newProtectActionConfigsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "action-configs",
		Short: "Manage Jamf Protect action configurations",
	}

	cmd.AddCommand(newProtectActionConfigsListCmd(cliCtx))
	cmd.AddCommand(newProtectActionConfigsGetCmd(cliCtx))
	cmd.AddCommand(newProtectActionConfigsCreateCmd(cliCtx))
	cmd.AddCommand(newProtectActionConfigsUpdateCmd(cliCtx))
	cmd.AddCommand(newProtectActionConfigsDeleteCmd(cliCtx))

	return cmd
}

func newProtectActionConfigsListCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all action configurations",
		RunE: func(cmd *cobra.Command, _ []string) error {
			items, err := cliCtx.ProtectClient.ListActionConfigs(cmd.Context())
			if err != nil {
				return err
			}
			return protect.PrintList(cliCtx.Output, items)
		},
	}
}

func newProtectActionConfigsGetCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get <name>",
		Short: "Get an action configuration by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r := protect.NewResolver(cliCtx.ProtectClient)
			id, err := r.ResolveActionConfigID(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			item, err := cliCtx.ProtectClient.GetActionConfig(cmd.Context(), id)
			if err != nil {
				return err
			}
			return protect.PrintOne(cliCtx.Output, item)
		},
	}
}

func newProtectActionConfigsCreateCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var fromFile string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an action configuration",
		RunE: func(cmd *cobra.Command, _ []string) error {
			data, err := readProtectInput(fromFile)
			if err != nil {
				return err
			}
			var input jamfprotect.ActionConfigInput
			if err := json.Unmarshal(data, &input); err != nil {
				return fmt.Errorf("parsing input JSON: %w", err)
			}
			result, err := cliCtx.ProtectClient.CreateActionConfig(cmd.Context(), input)
			if err != nil {
				return err
			}
			return protect.PrintOne(cliCtx.Output, result)
		},
	}
	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON input file (or pipe JSON to stdin)")
	return cmd
}

func newProtectActionConfigsUpdateCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var fromFile string
	cmd := &cobra.Command{
		Use:   "update <name>",
		Short: "Update an action configuration",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r := protect.NewResolver(cliCtx.ProtectClient)
			id, err := r.ResolveActionConfigID(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			data, err := readProtectInput(fromFile)
			if err != nil {
				return err
			}
			var input jamfprotect.ActionConfigInput
			if err := json.Unmarshal(data, &input); err != nil {
				return fmt.Errorf("parsing input JSON: %w", err)
			}
			result, err := cliCtx.ProtectClient.UpdateActionConfig(cmd.Context(), id, input)
			if err != nil {
				return err
			}
			return protect.PrintOne(cliCtx.Output, result)
		},
	}
	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON input file (or pipe JSON to stdin)")
	return cmd
}

func newProtectActionConfigsDeleteCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete an action configuration",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r := protect.NewResolver(cliCtx.ProtectClient)
			id, err := r.ResolveActionConfigID(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			proceed, err := confirmProtectDelete("action configuration", args[0], yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			if err := cliCtx.ProtectClient.DeleteActionConfig(cmd.Context(), id); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Deleted action configuration %q\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	return cmd
}
