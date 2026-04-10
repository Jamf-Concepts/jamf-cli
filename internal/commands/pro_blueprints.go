// Copyright 2026, Jamf Software LLC

package commands

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/platform"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform"
)

func newBlueprintsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "blueprints",
		Short: "Manage blueprints (Platform API)",
		Long:  "Create, deploy, and manage Jamf Platform blueprints. Requires platform gateway auth.",
	}

	cmd.AddCommand(newBlueprintsListCmd(cliCtx))
	cmd.AddCommand(newBlueprintsGetCmd(cliCtx))
	cmd.AddCommand(newBlueprintsApplyCmd(cliCtx))
	cmd.AddCommand(newBlueprintsDeleteCmd(cliCtx))
	cmd.AddCommand(newBlueprintsExportCmd(cliCtx))
	cmd.AddCommand(newBlueprintsDeployCmd(cliCtx))
	cmd.AddCommand(newBlueprintsUndeployCmd(cliCtx))
	cmd.AddCommand(newBlueprintsReportCmd(cliCtx))
	cmd.AddCommand(newBlueprintsComponentsCmd(cliCtx))

	return cmd
}

func newBlueprintsListCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		sortFields []string
		search     string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all blueprints",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			bps, err := cliCtx.PlatformClient.ListBlueprints(cmd.Context(), sortFields, search)
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(bps))
			for _, bp := range bps {
				rows = append(rows, flattenBlueprintOverview(bp))
			}
			data, err := json.Marshal(rows)
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}
	cmd.Flags().StringSliceVar(&sortFields, "sort", nil, "Sort fields (e.g. name:asc)")
	cmd.Flags().StringVar(&search, "search", "", "Search filter")
	return cmd
}

func flattenBlueprintOverview(bp jamfplatform.BlueprintOverviewV1) map[string]any {
	m := map[string]any{
		"id":          bp.ID,
		"name":        bp.Name,
		"description": bp.Description,
		"created":     bp.Created,
		"updated":     bp.Updated,
		"state":       bp.DeploymentState.State,
	}
	if bp.DeploymentState.LastDeployment != nil {
		m["lastDeployed"] = bp.DeploymentState.LastDeployment.Started
	}
	return m
}

func flattenBlueprintDetail(bp jamfplatform.BlueprintDetailV1) map[string]any {
	m := map[string]any{
		"id":          bp.ID,
		"name":        bp.Name,
		"description": bp.Description,
		"created":     bp.Created,
		"updated":     bp.Updated,
		"state":       bp.DeploymentState.State,
		"steps":       len(bp.Steps),
		"scope":       len(bp.Scope.DeviceGroups),
	}
	if bp.DeploymentState.LastDeployment != nil {
		m["lastDeployed"] = bp.DeploymentState.LastDeployment.Started
	}
	return m
}

func newBlueprintsGetCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get <name>",
		Short: "Get a blueprint by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			bp, err := cliCtx.PlatformClient.GetBlueprintByName(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return printResult(cliCtx.Output, bp, flattenBlueprintDetail(*bp))
		},
	}
}

func newBlueprintsApplyCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		fromFile string
		yes      bool
		scaffold bool
	)
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Create or update a blueprint",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if scaffold {
				return printScaffold(blueprintScaffold())
			}
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			ctx := cmd.Context()

			data, err := readInput(fromFile)
			if err != nil {
				return err
			}

			var createReq jamfplatform.BlueprintCreateRequestV1
			if err := unmarshalInput(data, &createReq); err != nil {
				return fmt.Errorf("parsing input: %w", err)
			}
			if createReq.Name == "" {
				return fmt.Errorf("input must include a 'name' field")
			}

			// Check if a blueprint with this name already exists
			r := platform.NewResolver(cliCtx.PlatformClient)
			id, resolveErr := r.ResolveBlueprintID(ctx, createReq.Name)
			if resolveErr != nil {
				// Not found — create
				result, err := cliCtx.PlatformClient.CreateBlueprint(ctx, &createReq)
				if err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "Created blueprint %q (id: %s)\n", createReq.Name, result.ID)
				// Fetch the full blueprint to display
				bp, err := cliCtx.PlatformClient.GetBlueprint(ctx, result.ID)
				if err != nil {
					return err
				}
				return printResult(cliCtx.Output, bp, flattenBlueprintDetail(*bp))
			}

			// Found — confirm before updating
			proceed, err := confirmReplace("blueprint", createReq.Name, yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			updateReq := blueprintCreateToUpdate(&createReq)
			if err := cliCtx.PlatformClient.UpdateBlueprint(ctx, id, updateReq); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Updated blueprint %q\n", createReq.Name)
			bp, err := cliCtx.PlatformClient.GetBlueprint(ctx, id)
			if err != nil {
				return err
			}
			return printResult(cliCtx.Output, bp, flattenBlueprintDetail(*bp))
		},
	}
	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON input file (or pipe JSON to stdin)")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt when replacing")
	cmd.Flags().BoolVar(&scaffold, "scaffold", false, "Print a JSON template for the input format")
	return cmd
}

// blueprintCreateToUpdate converts a create request to an update request
// for merge-patch semantics.
func blueprintCreateToUpdate(c *jamfplatform.BlueprintCreateRequestV1) *jamfplatform.BlueprintUpdateRequestV1 {
	return &jamfplatform.BlueprintUpdateRequestV1{
		Name:        &c.Name,
		Description: &c.Description,
		Scope: &jamfplatform.BlueprintUpdateScopeV1{
			DeviceGroups: c.Scope.DeviceGroups,
		},
		Steps: c.Steps,
	}
}

func blueprintScaffold() *jamfplatform.BlueprintCreateRequestV1 {
	return &jamfplatform.BlueprintCreateRequestV1{
		Name:        "My Blueprint",
		Description: "",
		Scope: jamfplatform.BlueprintCreateScopeV1{
			DeviceGroups: []string{"<device-group-id>"},
		},
		Steps: []jamfplatform.BlueprintStepV1{
			{
				Name: "Step 1",
				Components: []jamfplatform.BlueprintComponentV1{
					{
						Identifier:    "<component-identifier>",
						Configuration: json.RawMessage(`{}`),
					},
				},
			},
		},
	}
}

func newBlueprintsDeleteCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a blueprint",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			ctx := cmd.Context()
			r := platform.NewResolver(cliCtx.PlatformClient)
			id, err := r.ResolveBlueprintID(ctx, args[0])
			if err != nil {
				return err
			}
			proceed, err := confirmDelete("blueprint", args[0], yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}
			if err := cliCtx.PlatformClient.DeleteBlueprint(ctx, id); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Deleted blueprint %q\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	return cmd
}

// blueprintExport is the portable export format for blueprints.
// Server-generated fields (id, created, updated, deploymentState) are stripped.
type blueprintExport struct {
	Name        string                              `json:"name" yaml:"name"`
	Description string                              `json:"description,omitempty" yaml:"description,omitempty"`
	Scope       jamfplatform.BlueprintCreateScopeV1 `json:"scope" yaml:"scope"`
	Steps       []jamfplatform.BlueprintStepV1      `json:"steps" yaml:"steps"`
}

func blueprintToExport(bp *jamfplatform.BlueprintDetailV1) blueprintExport {
	return blueprintExport{
		Name:        bp.Name,
		Description: bp.Description,
		Scope: jamfplatform.BlueprintCreateScopeV1{
			DeviceGroups: bp.Scope.DeviceGroups,
		},
		Steps: bp.Steps,
	}
}

func newBlueprintsExportCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "export <name>",
		Short: "Export a blueprint as JSON or YAML",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			bp, err := cliCtx.PlatformClient.GetBlueprintByName(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return printExport(blueprintToExport(bp))
		},
	}
}

func newBlueprintsDeployCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "deploy <name>",
		Short: "Deploy a blueprint",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			ctx := cmd.Context()
			r := platform.NewResolver(cliCtx.PlatformClient)
			id, err := r.ResolveBlueprintID(ctx, args[0])
			if err != nil {
				return err
			}
			if !yes {
				if dryRun {
					fmt.Fprintf(os.Stderr, "[dry-run] Would deploy blueprint %q\n", args[0])
					return nil
				}
				if noInput {
					return fmt.Errorf("deploy requires --yes when --no-input is set")
				}
				fmt.Fprintf(os.Stderr, "This will deploy blueprint %q. Type 'yes' to confirm: ", args[0])
				var confirm string
				if _, err := fmt.Scanln(&confirm); err != nil {
					return fmt.Errorf("reading confirmation: %w", err)
				}
				if confirm != "yes" {
					return fmt.Errorf("aborted")
				}
			}
			if err := cliCtx.PlatformClient.DeployBlueprint(ctx, id); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Deployment started for blueprint %q\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	return cmd
}

func newBlueprintsUndeployCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "undeploy <name>",
		Short: "Undeploy a blueprint",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			ctx := cmd.Context()
			r := platform.NewResolver(cliCtx.PlatformClient)
			id, err := r.ResolveBlueprintID(ctx, args[0])
			if err != nil {
				return err
			}
			if !yes {
				if dryRun {
					fmt.Fprintf(os.Stderr, "[dry-run] Would undeploy blueprint %q\n", args[0])
					return nil
				}
				if noInput {
					return fmt.Errorf("undeploy requires --yes when --no-input is set")
				}
				fmt.Fprintf(os.Stderr, "This will undeploy blueprint %q. Type 'yes' to confirm: ", args[0])
				var confirm string
				if _, err := fmt.Scanln(&confirm); err != nil {
					return fmt.Errorf("reading confirmation: %w", err)
				}
				if confirm != "yes" {
					return fmt.Errorf("aborted")
				}
			}
			if err := cliCtx.PlatformClient.UndeployBlueprint(ctx, id); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Undeployment started for blueprint %q\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	return cmd
}

func newBlueprintsReportCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "report <name>",
		Short: "Get deployment status report for a blueprint",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			ctx := cmd.Context()
			r := platform.NewResolver(cliCtx.PlatformClient)
			id, err := r.ResolveBlueprintID(ctx, args[0])
			if err != nil {
				return err
			}
			report, err := cliCtx.PlatformClient.GetBlueprintReport(ctx, id)
			if err != nil {
				return err
			}
			m := map[string]any{
				"blueprint": args[0],
				"succeeded": report.Succeeded,
				"failed":    report.Failed,
				"pending":   report.Pending,
			}
			data, err := json.Marshal(m)
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}
}

func newBlueprintsComponentsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "components",
		Short: "Manage blueprint components",
	}
	cmd.AddCommand(newBlueprintsComponentsListCmd(cliCtx))
	cmd.AddCommand(newBlueprintsComponentsGetCmd(cliCtx))
	return cmd
}

func newBlueprintsComponentsListCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available blueprint components",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			comps, err := cliCtx.PlatformClient.ListBlueprintComponents(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(comps))
			for _, c := range comps {
				rows = append(rows, map[string]any{
					"identifier":  c.Identifier,
					"name":        c.Name,
					"description": c.Description,
				})
			}
			data, err := json.Marshal(rows)
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}
}

func newBlueprintsComponentsGetCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get <identifier>",
		Short: "Get a blueprint component by identifier",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			comp, err := cliCtx.PlatformClient.GetBlueprintComponent(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return platform.PrintOne(cliCtx.Output, comp)
		},
	}
}
