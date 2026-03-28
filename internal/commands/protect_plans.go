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

func newProtectPlansCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plans",
		Short: "Manage Jamf Protect plans",
	}

	cmd.AddCommand(newProtectPlansListCmd(cliCtx))
	cmd.AddCommand(newProtectPlansGetCmd(cliCtx))
	cmd.AddCommand(newProtectPlansCreateCmd(cliCtx))
	cmd.AddCommand(newProtectPlansUpdateCmd(cliCtx))
	cmd.AddCommand(newProtectPlansDeleteCmd(cliCtx))
	cmd.AddCommand(newProtectPlansConfigProfileCmd(cliCtx))

	return cmd
}

func newProtectPlansListCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all plans",
		RunE: func(cmd *cobra.Command, _ []string) error {
			plans, err := cliCtx.ProtectClient.ListPlans(cmd.Context())
			if err != nil {
				return err
			}
			return protect.PrintList(cliCtx.Output, plans)
		},
	}
}

func newProtectPlansGetCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get <name>",
		Short: "Get a plan by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)

			id, err := r.ResolvePlanID(ctx, args[0])
			if err != nil {
				return err
			}

			plan, err := cliCtx.ProtectClient.GetPlan(ctx, id)
			if err != nil {
				return err
			}
			return protect.PrintOne(cliCtx.Output, plan)
		},
	}
}

func newProtectPlansCreateCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		fromFile string
		scaffold bool
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a plan",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if scaffold {
				fmt.Println(planScaffoldJSON)
				return nil
			}

			data, err := readProtectInput(fromFile)
			if err != nil {
				return err
			}

			var input jamfprotect.PlanInput
			if err := json.Unmarshal(data, &input); err != nil {
				return fmt.Errorf("parsing input: %w", err)
			}

			plan, err := cliCtx.ProtectClient.CreatePlan(cmd.Context(), input)
			if err != nil {
				return err
			}
			return protect.PrintOne(cliCtx.Output, plan)
		},
	}

	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON input file (or pipe JSON to stdin)")
	cmd.Flags().BoolVar(&scaffold, "scaffold", false, "Print a JSON template for the request body and exit")
	cmd.MarkFlagsMutuallyExclusive("from-file", "scaffold")

	return cmd
}

func newProtectPlansUpdateCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var fromFile string

	cmd := &cobra.Command{
		Use:   "update <name>",
		Short: "Update a plan",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)

			id, err := r.ResolvePlanID(ctx, args[0])
			if err != nil {
				return err
			}

			data, err := readProtectInput(fromFile)
			if err != nil {
				return err
			}

			var input jamfprotect.PlanInput
			if err := json.Unmarshal(data, &input); err != nil {
				return fmt.Errorf("parsing input: %w", err)
			}

			plan, err := cliCtx.ProtectClient.UpdatePlan(ctx, id, input)
			if err != nil {
				return err
			}
			return protect.PrintOne(cliCtx.Output, plan)
		},
	}

	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON input file (or pipe JSON to stdin)")

	return cmd
}

func newProtectPlansDeleteCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a plan",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)

			id, err := r.ResolvePlanID(ctx, args[0])
			if err != nil {
				return err
			}

			proceed, err := confirmProtectDelete("plan", args[0], yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			if err := cliCtx.ProtectClient.DeletePlan(ctx, id); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Deleted plan %q\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	return cmd
}

func newProtectPlansConfigProfileCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "config-profile <name>",
		Short: "Get the configuration profile for a plan",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)

			id, err := r.ResolvePlanID(ctx, args[0])
			if err != nil {
				return err
			}

			profile, err := cliCtx.ProtectClient.GetPlansConfigProfile(ctx, id, nil)
			if err != nil {
				return err
			}
			fmt.Println(profile)
			return nil
		},
	}
}

const planScaffoldJSON = `{
  "Name": "",
  "Description": "",
  "LogLevel": null,
  "ActionConfigs": "",
  "ExceptionSets": [],
  "Telemetry": null,
  "TelemetryV2": null,
  "AnalyticSets": [
    {
      "Type": "",
      "UUID": ""
    }
  ],
  "USBControlSet": null,
  "CommsConfig": {
    "FQDN": "",
    "Protocol": ""
  },
  "InfoSync": {
    "Attrs": [],
    "InsightsSyncInterval": 0
  },
  "AutoUpdate": false,
  "SignaturesFeedConfig": {
    "Mode": ""
  }
}`
