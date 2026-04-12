// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/blueprintcomponents"
	"github.com/Jamf-Concepts/jamf-cli/internal/platform"
	"github.com/Jamf-Concepts/jamf-cli/internal/profileconvert"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamf-cli/internal/scope"
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
	cmd.AddCommand(newBlueprintsCloneCmd(cliCtx))
	cmd.AddCommand(newBlueprintsComponentsCmd(cliCtx))
	cmd.AddCommand(newBlueprintsImportProfileCmd(cliCtx))

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
		fromFile   string
		yes        bool
		scaffold   bool
		components []string
	)
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Create or update a blueprint",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if scaffold {
				bp := blueprintScaffold()
				if len(components) > 0 {
					var comps []jamfplatform.BlueprintComponentV1
					for _, c := range components {
						id := resolveComponentIdentifier(c)
						jsonStr, ok := blueprintcomponents.Scaffolds[id]
						if !ok {
							return fmt.Errorf("unknown component: %s\nRun 'jamf-cli pro blueprints components list' to see available components", c)
						}
						comps = append(comps, jamfplatform.BlueprintComponentV1{
							Identifier:    id,
							Configuration: json.RawMessage(jsonStr),
						})
					}
					bp.Steps[0].Components = comps
				}
				return printScaffold(bp)
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
	cmd.Flags().StringSliceVar(&components, "component", nil, "Component identifier(s) to include in scaffold (repeatable, use with --scaffold)")
	_ = cmd.RegisterFlagCompletionFunc("component", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return blueprintcomponents.Identifiers(), cobra.ShellCompDirectiveNoFileComp
	})
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
	cmd.AddCommand(newBlueprintsComponentsScaffoldCmd())
	cmd.AddCommand(newBlueprintsComponentsConfigProfileCmd(cliCtx))
	cmd.AddCommand(newBlueprintsComponentsConfigProfilePlistCmd())
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

func newBlueprintsCloneCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var scopeGroups []string
	cmd := &cobra.Command{
		Use:   "clone <source-name> <new-name>",
		Short: "Clone a blueprint with a new name",
		Long: `Creates a copy of an existing blueprint with a new name.
The source scope is copied by default. Use --scope to override device group targets.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			ctx := cmd.Context()
			pc := cliCtx.PlatformClient

			source, err := pc.GetBlueprintByName(ctx, args[0])
			if err != nil {
				return fmt.Errorf("source blueprint: %w", err)
			}

			groups := source.Scope.DeviceGroups
			if len(scopeGroups) > 0 {
				groups = scopeGroups
			}
			steps := randomizePayloadIdentifiers(source.Steps)

			createReq := &jamfplatform.BlueprintCreateRequestV1{
				Name:        args[1],
				Description: source.Description,
				Scope: jamfplatform.BlueprintCreateScopeV1{
					DeviceGroups: groups,
				},
				Steps: steps,
			}

			result, err := pc.CreateBlueprint(ctx, createReq)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Cloned blueprint %q → %q (id: %s)\n", args[0], args[1], result.ID)

			bp, err := pc.GetBlueprint(ctx, result.ID)
			if err != nil {
				return err
			}
			return printResult(cliCtx.Output, bp, flattenBlueprintDetail(*bp))
		},
	}
	cmd.Flags().StringSliceVar(&scopeGroups, "scope", nil, "Override device group IDs for the clone")
	return cmd
}

func newBlueprintsComponentsScaffoldCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "scaffold <identifier>",
		Short: "Print example configuration JSON for a component",
		Long: `Print a JSON configuration scaffold for a blueprint component.

Accepts full identifiers (com.jamf.ddm.software-update-settings) or
short names (software-update-settings).

The output can be used as the "configuration" field when building a
blueprint component in a step.

Examples:
  jamf-cli pro blueprints components scaffold software-update-settings
  jamf-cli pro blueprints components scaffold com.jamf.ddm.passcode-settings`,
		Args: cobra.ExactArgs(1),
		ValidArgsFunction: func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
			return blueprintcomponents.Identifiers(), cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(_ *cobra.Command, args []string) error {
			identifier := resolveComponentIdentifier(args[0])
			scaffold, ok := blueprintcomponents.Scaffolds[identifier]
			if !ok {
				return fmt.Errorf("unknown component identifier: %s\nRun 'jamf-cli pro blueprints components list' to see available components", args[0])
			}
			// Output as a complete component block ready for use in a blueprint step
			block := map[string]any{
				"identifier":    identifier,
				"configuration": json.RawMessage(scaffold),
			}
			return printScaffold(block)
		},
	}
}

// resolveComponentIdentifier resolves a component identifier from user input.
// It accepts full identifiers, short names from the generated map, or bare
// slugs that get auto-prefixed with "com.jamf.ddm.".
func resolveComponentIdentifier(input string) string {
	// Already a full identifier
	if _, ok := blueprintcomponents.Scaffolds[input]; ok {
		return input
	}
	// Try short name lookup
	if full, ok := blueprintcomponents.ShortNames[input]; ok {
		return full
	}
	// Try auto-prefixing for dotless input
	if !strings.Contains(input, ".") {
		candidate := "com.jamf.ddm." + input
		if _, ok := blueprintcomponents.Scaffolds[candidate]; ok {
			return candidate
		}
	}
	return input // return as-is, will fail with "unknown" error
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

func newBlueprintsComponentsConfigProfileCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		fromFile      string
		profileName   string
		profileType   string
		stripDefaults bool
	)
	cmd := &cobra.Command{
		Use:   "configuration-profile",
		Short: "Convert a mobileconfig to a blueprint component",
		Long: `Convert a configuration profile (.mobileconfig) to a com.jamf.ddm-configuration-profile
blueprint component. The profile display name is used as the component label.

Input can be a local file or piped from stdin:
  jamf-cli pro blueprints components configuration-profile --from-file profile.mobileconfig
  cat profile.mobileconfig | jamf-cli pro blueprints components configuration-profile

Or download an existing profile from Jamf Pro by name:
  jamf-cli pro blueprints components configuration-profile --name "My Restrictions"
  jamf-cli pro blueprints components configuration-profile --name "Managed Restrictions" --type mobile

Only preference domains that Apple supports for declarative management can be
used. Unsupported payload types will trigger a warning but are still included
in the output — the API will validate and reject if necessary.

Use --strip-defaults to remove keys that are set to their Apple default values.
This is useful for profiles created by Jamf Pro's UI which sets every key even
when the administrator only intended to manage a few settings.

Supported payloads: https://github.com/apple/device-management/tree/release/mdm/profiles`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var data []byte
			var err error

			if profileName != "" {
				// Download from Jamf Pro classic API
				data, err = downloadClassicProfile(cmd, cliCtx, profileName, profileType)
				if err != nil {
					return err
				}
			} else {
				data, err = readInput(fromFile)
				if err != nil {
					return err
				}
			}

			config, warnings, err := profileconvert.ConvertMobileconfig(data, false)
			if err != nil {
				return err
			}

			if stripDefaults {
				fetcher := profileconvert.NewSchemaFetcher(nil)
				var msgs []string
				config, msgs = profileconvert.StripConfigDefaults(config, fetcher)
				for _, m := range msgs {
					fmt.Fprintf(os.Stderr, "  %s\n", m)
				}
				if err := profileconvert.ConfigHasPayloads(config); err != nil {
					return err
				}
			}

			for _, w := range warnings {
				fmt.Fprintf(os.Stderr, "Warning: %s\n", w)
			}
			fmt.Fprintln(os.Stderr, profileconvert.ConflictWarning)

			component, err := profileconvert.FormatComponentJSON(config)
			if err != nil {
				return err
			}

			fmt.Println(string(component))
			return nil
		},
	}
	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to .mobileconfig file (or pipe to stdin)")
	cmd.Flags().StringVar(&profileName, "name", "", "Download profile from Jamf Pro by name (requires Pro auth)")
	cmd.Flags().StringVar(&profileType, "type", "computer", "Profile type when using --name: computer or mobile")
	cmd.Flags().BoolVar(&stripDefaults, "strip-defaults", false, "Remove keys set to Apple's default values (fetches schemas from GitHub)")
	cmd.MarkFlagsMutuallyExclusive("from-file", "name")
	_ = cmd.RegisterFlagCompletionFunc("type", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"computer", "mobile"}, cobra.ShellCompDirectiveNoFileComp
	})
	return cmd
}

// classicProfilePath returns the Classic API path for a profile by name.
// profileType must be "computer" or "mobile".
func classicProfilePath(profileType, name string) string {
	if profileType == "mobile" {
		return "/JSSResource/mobiledeviceconfigurationprofiles/name/" + name
	}
	return "/JSSResource/osxconfigurationprofiles/name/" + name
}

// downloadClassicProfile fetches a configuration profile from the Jamf Pro
// Classic API and extracts the mobileconfig XML from the response.
// profileType must be "computer" or "mobile".
func downloadClassicProfile(cmd *cobra.Command, cliCtx *registry.CLIContext, name, profileType string) ([]byte, error) {
	if cliCtx.Client == nil {
		return nil, fmt.Errorf("--name requires Jamf Pro authentication (use 'jamf-cli pro setup' or set JAMF_* env vars)")
	}

	path := classicProfilePath(profileType, name)
	resp, err := cliCtx.Client.Do(cmd.Context(), "GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("fetching profile %q: %w", name, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := readResponseBody(resp)
	if err != nil {
		return nil, fmt.Errorf("reading profile response: %w", err)
	}

	// Extract the <payloads> content from the Classic API XML response
	mobileconfig := extractPayloadsFromXML(string(body))
	if mobileconfig == "" {
		return nil, fmt.Errorf("no <payloads> found in profile %q response", name)
	}

	fmt.Fprintf(os.Stderr, "Downloaded profile %q from Jamf Pro\n", name)
	return []byte(mobileconfig), nil
}

func newBlueprintsComponentsConfigProfilePlistCmd() *cobra.Command {
	var (
		fromFile      string
		payloadType   string
		displayName   string
		stripDefaults bool
	)
	cmd := &cobra.Command{
		Use:   "configuration-profile-plist",
		Short: "Convert a preference domain plist to a blueprint component",
		Long: `Convert a raw preference domain plist to a com.jamf.ddm-configuration-profile
blueprint component.

The plist should contain only preference domain keys (no Apple payload metadata).
You must specify the payload type (preference domain identifier).

Use --strip-defaults to remove keys that are set to their Apple default values.

Examples:
  jamf-cli pro blueprints components configuration-profile-plist \
    --from-file com.apple.dock.plist --payload-type com.apple.dock

  cat prefs.plist | jamf-cli pro blueprints components configuration-profile-plist \
    --payload-type com.apple.screensaver --display-name "Screensaver Settings"`,
		RunE: func(_ *cobra.Command, _ []string) error {
			data, err := readInput(fromFile)
			if err != nil {
				return err
			}

			config, warnings, err := profileconvert.ConvertPlist(data, payloadType, displayName)
			if err != nil {
				return err
			}

			if stripDefaults {
				fetcher := profileconvert.NewSchemaFetcher(nil)
				var msgs []string
				config, msgs = profileconvert.StripConfigDefaults(config, fetcher)
				for _, m := range msgs {
					fmt.Fprintf(os.Stderr, "  %s\n", m)
				}
				if err := profileconvert.ConfigHasPayloads(config); err != nil {
					return err
				}
			}

			for _, w := range warnings {
				fmt.Fprintf(os.Stderr, "Warning: %s\n", w)
			}
			fmt.Fprintln(os.Stderr, profileconvert.ConflictWarning)

			component, err := profileconvert.FormatComponentJSON(config)
			if err != nil {
				return err
			}

			fmt.Println(string(component))
			return nil
		},
	}
	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to plist file (or pipe to stdin)")
	cmd.Flags().StringVar(&payloadType, "payload-type", "", "Apple preference domain (e.g. com.apple.dock)")
	cmd.Flags().StringVar(&displayName, "display-name", "", "Display name for the component (defaults to payload type)")
	cmd.Flags().BoolVar(&stripDefaults, "strip-defaults", false, "Remove keys set to Apple's default values (fetches schemas from GitHub)")
	_ = cmd.MarkFlagRequired("payload-type")
	_ = cmd.RegisterFlagCompletionFunc("payload-type", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return profileconvert.SupportedPayloadTypesList(), cobra.ShellCompDirectiveNoFileComp
	})
	return cmd
}

func newBlueprintsImportProfileCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		blueprintName     string
		profileType       string
		filterUnsupported bool
		stripDefaults     bool
	)
	cmd := &cobra.Command{
		Use:   "import-profile <profile-name>",
		Short: "Import a Classic configuration profile as a blueprint",
		Long: `Download a configuration profile from Jamf Pro, convert it to a
com.jamf.ddm-configuration-profile blueprint component, resolve target scope
groups to platform device group UUIDs, and create the blueprint in one step.

Use --type to specify the profile type: "computer" (default) for macOS configuration
profiles or "mobile" for mobile device configuration profiles. Profiles can share
names across types, so the flag is required when ambiguous.

The blueprint name defaults to the profile's display name (override with --blueprint-name).

Use --strip-defaults to remove keys that are set to their Apple default values.
This is useful for profiles created by Jamf Pro's UI which sets every key even
when the administrator only intended to manage a few settings.

Scope handling:
  Only target computer groups and mobile device groups are carried over to the
  blueprint scope. Individual computer/device assignments, buildings, departments,
  limitations, and exclusions are NOT imported — blueprints only support device
  group scoping. You will be warned about any scope elements that are dropped.

Examples:
  jamf-cli pro blueprints import-profile "My Restrictions"
  jamf-cli pro blueprints import-profile "Managed Restrictions" --type mobile
  jamf-cli pro blueprints import-profile "FileVault Settings" --blueprint-name "FV Blueprint"
  jamf-cli pro blueprints import-profile "My Restrictions" --strip-defaults --filter-unsupported`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			if cliCtx.Client == nil {
				return fmt.Errorf("import-profile requires Jamf Pro authentication")
			}
			ctx := cmd.Context()

			// Step 1: Download the Classic API profile
			fmt.Fprintf(os.Stderr, "Downloading %s profile %q from Jamf Pro...\n", profileType, args[0])
			profilePath := classicProfilePath(profileType, args[0])
			resp, err := cliCtx.Client.Do(ctx, "GET", profilePath, nil)
			if err != nil {
				return fmt.Errorf("fetching profile: %w", err)
			}
			defer func() { _ = resp.Body.Close() }()

			body, err := readResponseBody(resp)
			if err != nil {
				return fmt.Errorf("reading profile response: %w", err)
			}

			// Step 2: Extract mobileconfig from <payloads>
			mobileconfig := extractPayloadsFromXML(string(body))
			if mobileconfig == "" {
				return fmt.Errorf("no <payloads> found in profile %q (type=%s)", args[0], profileType)
			}

			// Step 3: Convert mobileconfig to component configuration
			config, warnings, err := profileconvert.ConvertMobileconfig([]byte(mobileconfig), filterUnsupported)
			if err != nil {
				return fmt.Errorf("converting profile: %w", err)
			}
			for _, w := range warnings {
				fmt.Fprintf(os.Stderr, "Warning: %s\n", w)
			}

			// Validate/strip using Apple schemas — always validate for
			// import-profile since we're uploading directly to the API.
			fetcher := profileconvert.NewSchemaFetcher(nil)
			if stripDefaults {
				var msgs []string
				config, msgs = profileconvert.StripConfigDefaults(config, fetcher)
				for _, m := range msgs {
					fmt.Fprintf(os.Stderr, "  %s\n", m)
				}
			} else {
				var msgs []string
				config, msgs = profileconvert.ValidatePayloads(config, fetcher)
				for _, m := range msgs {
					fmt.Fprintf(os.Stderr, "  %s\n", m)
				}
			}
			if err := profileconvert.ConfigHasPayloads(config); err != nil {
				return err
			}

			types := profileconvert.PayloadTypeSummary([]byte(mobileconfig))
			fmt.Fprintf(os.Stderr, "Converted %d payload(s): %s\n", len(types), strings.Join(types, ", "))

			// Step 4: Extract scope from Classic API XML and resolve to platform UUIDs
			scopeGroups, scopeWarnings := extractAndResolveScope(ctx, cliCtx.Client, body)
			for _, w := range scopeWarnings {
				fmt.Fprintf(os.Stderr, "Warning: %s\n", w)
			}
			if len(scopeGroups) > 0 {
				fmt.Fprintf(os.Stderr, "Resolved %d scope group(s) to platform UUIDs\n", len(scopeGroups))
			} else {
				fmt.Fprintln(os.Stderr, "No scope groups resolved — blueprint will have empty scope")
			}

			// Step 5: Build and create the blueprint
			name := blueprintName
			if name == "" {
				name = profileconvert.ProfileDisplayName([]byte(mobileconfig))
			}
			if name == "" {
				name = args[0]
			}

			createReq := &jamfplatform.BlueprintCreateRequestV1{
				Name: name,
				Scope: jamfplatform.BlueprintCreateScopeV1{
					DeviceGroups: scopeGroups,
				},
				Steps: []jamfplatform.BlueprintStepV1{
					{
						Name: "Step 1",
						Components: []jamfplatform.BlueprintComponentV1{
							{
								Identifier:    "com.jamf.ddm-configuration-profile",
								Configuration: config,
							},
						},
					},
				},
			}

			fmt.Fprintln(os.Stderr, profileconvert.ConflictWarning)

			result, err := cliCtx.PlatformClient.CreateBlueprint(ctx, createReq)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Created blueprint %q (id: %s)\n", name, result.ID)

			bp, err := cliCtx.PlatformClient.GetBlueprint(ctx, result.ID)
			if err != nil {
				return err
			}
			return printResult(cliCtx.Output, bp, flattenBlueprintDetail(*bp))
		},
	}
	cmd.Flags().StringVar(&blueprintName, "blueprint-name", "", "Override the blueprint name (defaults to profile display name)")
	cmd.Flags().StringVar(&profileType, "type", "computer", "Profile type: computer (macOS) or mobile (iOS/iPadOS/tvOS)")
	cmd.Flags().BoolVar(&filterUnsupported, "filter-unsupported", false, "Remove payload types not supported by the platform API instead of passing them through")
	cmd.Flags().BoolVar(&stripDefaults, "strip-defaults", false, "Remove keys set to Apple's default values (fetches schemas from GitHub)")
	_ = cmd.RegisterFlagCompletionFunc("type", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"computer", "mobile"}, cobra.ShellCompDirectiveNoFileComp
	})
	return cmd
}

// classicProfileScope is a minimal struct for unmarshalling the scope section
// from a Classic API configuration profile XML response.
type classicProfileScope struct {
	XMLName            xml.Name              `xml:"scope"`
	AllComputers       bool                  `xml:"all_computers"`
	AllMobileDevices   bool                  `xml:"all_mobile_devices"`
	Computers          scope.ScopeItemSlice  `xml:"computers"`
	MobileDevices      scope.ScopeItemSlice  `xml:"mobile_devices"`
	ComputerGroups     scope.ScopeItemSlice  `xml:"computer_groups"`
	MobileDeviceGroups scope.ScopeItemSlice  `xml:"mobile_device_groups"`
	Buildings          scope.ScopeItemSlice  `xml:"buildings"`
	Departments        scope.ScopeItemSlice  `xml:"departments"`
	Limitations        *scope.LimitationsXML `xml:"limitations,omitempty"`
	Exclusions         *scope.ExclusionsXML  `xml:"exclusions,omitempty"`
}

// extractAndResolveScope parses the <scope> section from a Classic API profile
// response, resolves target computer/mobile device group names to platform UUIDs,
// and returns warnings for any scope elements that can't be imported.
func extractAndResolveScope(ctx context.Context, client registry.HTTPClient, xmlBody []byte) ([]string, []string) {
	var warnings []string

	// Find and parse the <scope> section
	xmlStr := string(xmlBody)
	scopeStart := strings.Index(xmlStr, "<scope>")
	if scopeStart == -1 {
		warnings = append(warnings, "no <scope> section found in profile")
		return nil, warnings
	}
	scopeEnd := strings.Index(xmlStr[scopeStart:], "</scope>")
	if scopeEnd == -1 {
		warnings = append(warnings, "malformed <scope> section in profile")
		return nil, warnings
	}
	scopeXML := xmlStr[scopeStart : scopeStart+scopeEnd+len("</scope>")]

	var s classicProfileScope
	if err := xml.Unmarshal([]byte(scopeXML), &s); err != nil {
		warnings = append(warnings, fmt.Sprintf("failed to parse scope: %v", err))
		return nil, warnings
	}

	// Warn about scope elements that blueprints don't support
	if s.AllComputers {
		warnings = append(warnings, "profile is scoped to 'All Computers' — blueprint scope requires explicit device groups")
	}
	if s.AllMobileDevices {
		warnings = append(warnings, "profile is scoped to 'All Mobile Devices' — blueprint scope requires explicit device groups")
	}
	if len(s.Computers.Items) > 0 {
		warnings = append(warnings, fmt.Sprintf("%d individual computer assignment(s) dropped — blueprints only support device group scoping", len(s.Computers.Items)))
	}
	if len(s.MobileDevices.Items) > 0 {
		warnings = append(warnings, fmt.Sprintf("%d individual mobile device assignment(s) dropped — blueprints only support device group scoping", len(s.MobileDevices.Items)))
	}
	if len(s.Buildings.Items) > 0 {
		warnings = append(warnings, fmt.Sprintf("%d building scope(s) dropped — not supported in blueprints", len(s.Buildings.Items)))
	}
	if len(s.Departments.Items) > 0 {
		warnings = append(warnings, fmt.Sprintf("%d department scope(s) dropped — not supported in blueprints", len(s.Departments.Items)))
	}
	if s.Limitations != nil {
		total := len(s.Limitations.Users.Items) + len(s.Limitations.UserGroups.Items) +
			len(s.Limitations.NetworkSegments.Items) + len(s.Limitations.ComputerGroups.Items)
		if total > 0 {
			warnings = append(warnings, fmt.Sprintf("%d scope limitation(s) dropped — not supported in blueprints", total))
		}
	}
	if s.Exclusions != nil {
		total := len(s.Exclusions.Computers.Items) + len(s.Exclusions.ComputerGroups.Items) +
			len(s.Exclusions.Buildings.Items) + len(s.Exclusions.Departments.Items) +
			len(s.Exclusions.Users.Items) + len(s.Exclusions.UserGroups.Items) +
			len(s.Exclusions.NetworkSegments.Items)
		if total > 0 {
			warnings = append(warnings, fmt.Sprintf("%d scope exclusion(s) dropped — not supported in blueprints", total))
		}
	}

	// Collect group names to resolve
	var groupNames []string
	for _, g := range s.ComputerGroups.Items {
		groupNames = append(groupNames, g.Name)
	}
	for _, g := range s.MobileDeviceGroups.Items {
		groupNames = append(groupNames, g.Name)
	}

	if len(groupNames) == 0 {
		return nil, warnings
	}

	// Resolve each group name to a platform UUID via /v1/groups
	var platformIDs []string
	for _, name := range groupNames {
		id, err := resolveGroupPlatformID(ctx, client, name)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("could not resolve group %q to platform UUID: %v", name, err))
			continue
		}
		platformIDs = append(platformIDs, id)
		fmt.Fprintf(os.Stderr, "  Resolved group %q → %s\n", name, id)
	}

	return platformIDs, warnings
}

// resolveGroupPlatformID queries /v1/groups with an RSQL filter to find the
// platform UUID for a group by name.
func resolveGroupPlatformID(ctx context.Context, client registry.HTTPClient, groupName string) (string, error) {
	filter := fmt.Sprintf(`groupName=="%s"`, groupName)
	path := "/v1/groups?page-size=1&filter=" + url.QueryEscape(filter)

	resp, err := client.Do(ctx, "GET", path, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := readResponseBody(resp)
	if err != nil {
		return "", err
	}

	var result struct {
		Results []struct {
			GroupPlatformID string `json:"groupPlatformId"`
			GroupName       string `json:"groupName"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parsing groups response: %w", err)
	}

	if len(result.Results) == 0 {
		return "", fmt.Errorf("no group found with name %q", groupName)
	}

	return result.Results[0].GroupPlatformID, nil
}

// extractPayloadsFromXML extracts the content between <payloads> tags from
// Classic API XML. The content may be CDATA-wrapped or XML entity-encoded.
func extractPayloadsFromXML(xmlStr string) string {
	start := strings.Index(xmlStr, "<payloads>")
	if start == -1 {
		return ""
	}
	start += len("<payloads>")
	end := strings.Index(xmlStr[start:], "</payloads>")
	if end == -1 {
		return ""
	}
	content := strings.TrimSpace(xmlStr[start : start+end])
	// Classic API wraps in CDATA sometimes
	content = strings.TrimPrefix(content, "<![CDATA[")
	content = strings.TrimSuffix(content, "]]>")
	content = strings.TrimSpace(content)
	// Classic API may entity-encode the XML instead of using CDATA
	if strings.Contains(content, "&lt;") {
		content = html.UnescapeString(content)
	}
	return content
}

// readResponseBody reads the full body from an HTTP response.
func readResponseBody(resp *http.Response) ([]byte, error) {
	return io.ReadAll(io.LimitReader(resp.Body, 10<<20))
}

// randomizePayloadIdentifiers walks through blueprint steps and replaces
// payloadIdentifier values in legacy configuration profile components
// (com.jamf.ddm-configuration-profile) with fresh UUIDs.
func randomizePayloadIdentifiers(steps []jamfplatform.BlueprintStepV1) []jamfplatform.BlueprintStepV1 {
	out := make([]jamfplatform.BlueprintStepV1, len(steps))
	for i, step := range steps {
		comps := make([]jamfplatform.BlueprintComponentV1, len(step.Components))
		for j, comp := range step.Components {
			comps[j] = comp
			if len(comp.Configuration) == 0 {
				continue
			}
			var config map[string]any
			if err := json.Unmarshal(comp.Configuration, &config); err != nil {
				continue
			}
			if randomizeMapPayloadIDs(config) {
				if data, err := json.Marshal(config); err == nil {
					comps[j].Configuration = data
				}
			}
		}
		out[i] = jamfplatform.BlueprintStepV1{
			Name:                step.Name,
			Components:          comps,
			ActivationPredicate: step.ActivationPredicate,
		}
	}
	return out
}

// randomizeMapPayloadIDs recursively walks a JSON map and replaces any
// "payloadIdentifier" string values with a fresh UUID. Returns true if
// any replacement was made.
func randomizeMapPayloadIDs(m map[string]any) bool {
	changed := false
	for k, v := range m {
		switch val := v.(type) {
		case map[string]any:
			if randomizeMapPayloadIDs(val) {
				changed = true
			}
		case []any:
			for _, item := range val {
				if sub, ok := item.(map[string]any); ok {
					if randomizeMapPayloadIDs(sub) {
						changed = true
					}
				}
			}
		case string:
			if k == "payloadIdentifier" {
				m[k] = newUUID()
				changed = true
			}
		}
	}
	return changed
}

// newUUID generates a random UUID v4 string.
func newUUID() string {
	var u [16]byte
	_, _ = rand.Read(u[:])
	u[6] = (u[6] & 0x0f) | 0x40 // version 4
	u[8] = (u[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:16])
}
