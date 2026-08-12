// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/blueprintcomponents"
	jamfclient "github.com/Jamf-Concepts/jamf-cli/internal/client"
	platformgen "github.com/Jamf-Concepts/jamf-cli/internal/commands/platform/generated"
	"github.com/Jamf-Concepts/jamf-cli/internal/commands/pro/generated"
	"github.com/Jamf-Concepts/jamf-cli/internal/platform"
	"github.com/Jamf-Concepts/jamf-cli/internal/profileconvert"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamf-cli/internal/scope"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/blueprints"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/devicegroups"
)

func newBlueprintsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "blueprints",
		Short: "Manage blueprints (Platform API)",
		Long:  "Create, deploy, and manage Jamf Platform blueprints. Requires platform gateway auth.",
	}

	// Generated CRUD and actions: list, get, delete, patch, deploy, undeploy, report
	// skip create — apply covers it with portable format + scope resolution + randomize-IDs
	for _, sub := range platformgen.NewBlueprintsCmd(cliCtx).Commands() {
		if sub.Name() == "create" {
			continue
		}
		cmd.AddCommand(sub)
	}

	// Business logic: portable upsert, export, scope management, clone, components, profile import
	cmd.AddCommand(newBlueprintsApplyCmd(cliCtx))
	cmd.AddCommand(newBlueprintsExportCmd(cliCtx))
	cmd.AddCommand(newBlueprintsCloneCmd(cliCtx))
	cmd.AddCommand(newBlueprintsScopeCmd(cliCtx))
	cmd.AddCommand(newBlueprintsComponentsCmd(cliCtx))
	cmd.AddCommand(newBlueprintsImportProfileCmd(cliCtx))

	return cmd
}

func flattenBlueprintDetail(bp blueprints.BlueprintDetail) map[string]any {
	m := map[string]any{
		"id":      bp.ID,
		"name":    bp.Name,
		"created": bp.Created,
		"updated": bp.Updated,
		"steps":   len(bp.Steps),
	}
	if bp.Description != nil {
		m["description"] = *bp.Description
	}
	if bp.Scope != nil {
		m["scope"] = len(bp.Scope.DeviceGroups)
	} else {
		m["scope"] = 0
	}
	if bp.DeploymentState != nil {
		m["state"] = bp.DeploymentState.State
		if bp.DeploymentState.LastDeployment != nil {
			m["lastDeployed"] = bp.DeploymentState.LastDeployment.Started
		}
	}
	return m
}

// resolveBlueprintID returns the blueprint UUID from the positional [<id>] arg
// or by resolving the --name flag. Exactly one of the two must be provided.
func resolveBlueprintID(ctx context.Context, cliCtx *registry.CLIContext, args []string, nameFlag string) (string, error) {
	if nameFlag != "" {
		if len(args) > 0 {
			return "", fmt.Errorf("specify either <id> or --name, not both")
		}
		return blueprints.New(cliCtx.PlatformSDKClient).ResolveBlueprintIDByName(ctx, nameFlag)
	}
	if len(args) == 0 {
		return "", fmt.Errorf("provide a blueprint <id> or use --name")
	}
	return args[0], nil
}

// blueprintLabel returns a human-readable label for log messages.
func blueprintLabel(args []string, nameFlag string) string {
	if nameFlag != "" {
		return nameFlag
	}
	if len(args) > 0 {
		return args[0]
	}
	return "<unknown>"
}

func newBlueprintsApplyCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		fromFile           string
		yes                bool
		scaffold           bool
		components         []string
		computerGroups     []string
		mobileDeviceGroups []string
	)
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Create or update a blueprint",
		Long: `Create or update a blueprint from JSON or YAML input.

Accepts both the legacy format (scope with UUID strings) and the portable
export format (scope with enriched group objects including names). When the
portable format is detected, group names are resolved to platform UUIDs on
the target instance — enabling cross-instance cloning via export → apply.

Use --computer-group or --mobile-device-group to override the scope from the
input file. This is useful when applying a blueprint from one instance to
another where different groups should be targeted.

Payload identifiers in legacy configuration profile components are
automatically randomized when creating a new blueprint, preventing
collisions when cloning across instances.

Examples:
  # Apply from file (same instance)
  jamf-cli pro bp apply --from-file blueprint.json

  # Cross-instance clone with scope override
  jamf-cli -p source pro bp export MyBlueprint > bp.json
  jamf-cli -p target pro bp apply --from-file bp.json --computer-group "Lab Macs"

  # Multi-instance fan-out
  jamf-cli multi --filter 'school-*' -- pro bp apply --from-file bp.json --yes`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if scaffold {
				bp := blueprintScaffold()
				if len(components) > 0 {
					var comps []blueprints.Component
					for _, c := range components {
						id := resolveComponentIdentifier(c)
						jsonStr, ok := blueprintcomponents.Scaffolds[id]
						if !ok {
							return fmt.Errorf("unknown component: %s\nRun 'jamf-cli pro blueprints components list' to see available components", c)
						}
						comps = append(comps, blueprints.Component{
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

			// Resolve scope override flags to UUIDs
			hasNameOverrides := len(computerGroups) > 0 || len(mobileDeviceGroups) > 0
			if cliCtx.Client == nil && hasNameOverrides {
				return fmt.Errorf("--computer-group/--mobile-device-group requires Jamf Pro authentication for name resolution")
			}
			var scopeOverrideIDs []string
			if hasNameOverrides {
				resolved, err := resolveGroupNames(ctx, cliCtx.Client, computerGroups, "COMPUTER")
				if err != nil {
					return err
				}
				scopeOverrideIDs = append(scopeOverrideIDs, resolved...)
				resolved, err = resolveGroupNames(ctx, cliCtx.Client, mobileDeviceGroups, "MOBILE")
				if err != nil {
					return err
				}
				scopeOverrideIDs = append(scopeOverrideIDs, resolved...)
			}

			data, err := readInput(fromFile)
			if err != nil {
				return err
			}

			createReq, err := parseBlueprintApplyInput(ctx, data, cliCtx.Client, scopeOverrideIDs)
			if err != nil {
				return err
			}

			// Check if a blueprint with this name already exists
			id, resolveErr := blueprints.New(cliCtx.PlatformSDKClient).ResolveBlueprintIDByName(ctx, createReq.Name)
			if resolveErr != nil && !platform.IsNotFound(resolveErr) {
				return resolveErr
			}
			if resolveErr != nil {
				// Not found — create with randomized payload IDs
				createReq.Steps = randomizePayloadIdentifiers(createReq.Steps)
				result, err := blueprints.New(cliCtx.PlatformSDKClient).CreateBlueprint(ctx, createReq)
				if err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "Created blueprint %q (id: %s)\n", createReq.Name, result.ID)
				bp, err := blueprints.New(cliCtx.PlatformSDKClient).GetBlueprint(ctx, result.ID)
				if err != nil {
					return err
				}
				return printResult(cliCtx.Output, bp, flattenBlueprintDetail(*bp))
			}

			// Found — confirm before updating (no payload ID randomization on update)
			proceed, err := confirmReplace("blueprint", createReq.Name, yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			updateReq := blueprintCreateToUpdate(createReq)
			if err := blueprints.New(cliCtx.PlatformSDKClient).UpdateBlueprint(ctx, id, updateReq); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Updated blueprint %q\n", createReq.Name)
			bp, err := blueprints.New(cliCtx.PlatformSDKClient).GetBlueprint(ctx, id)
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
	cmd.Flags().StringSliceVar(&computerGroups, "computer-group", nil, "Override scope with computer group name(s) (repeatable)")
	cmd.Flags().StringSliceVar(&mobileDeviceGroups, "mobile-device-group", nil, "Override scope with mobile device group name(s) (repeatable)")
	_ = cmd.RegisterFlagCompletionFunc("component", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return blueprintcomponents.Identifiers(), cobra.ShellCompDirectiveNoFileComp
	})
	return cmd
}

// blueprintCreateToUpdate converts a create request to an update request
// for merge-patch semantics.
func blueprintCreateToUpdate(c *blueprints.CreateBlueprintRequest) *blueprints.UpdateBlueprintRequest {
	update := &blueprints.UpdateBlueprintRequest{
		Name:        &c.Name,
		Description: c.Description,
		Steps:       &c.Steps,
	}
	if len(c.Scope.DeviceGroups) > 0 {
		update.Scope = &blueprints.BlueprintScope{
			DeviceGroups: c.Scope.DeviceGroups,
		}
	}
	return update
}

func blueprintScaffold() *blueprints.CreateBlueprintRequest {
	stepName := "Step 1"
	return &blueprints.CreateBlueprintRequest{
		Name: "My Blueprint",
		Scope: blueprints.CreateScope{
			DeviceGroups: []string{"<device-group-id>"},
		},
		Steps: []blueprints.BlueprintStep{
			{
				Name: &stepName,
				Components: []blueprints.Component{
					{
						Identifier:    "<component-identifier>",
						Configuration: json.RawMessage(`{}`),
					},
				},
			},
		},
	}
}

// blueprintExportScopeGroup represents a device group in the portable export format.
// Includes both the platform UUID and human-readable metadata so the export
// can be applied to a different Jamf instance where group UUIDs differ.
type blueprintExportScopeGroup struct {
	ID         string `json:"id" yaml:"id"`
	Name       string `json:"name" yaml:"name"`
	DeviceType string `json:"deviceType" yaml:"deviceType"` // COMPUTER, MOBILE_DEVICE, etc.
}

// blueprintExportScope is the scope section of the portable export format.
type blueprintExportScope struct {
	DeviceGroups []blueprintExportScopeGroup `json:"deviceGroups" yaml:"deviceGroups"`
}

// blueprintExport is the portable export format for blueprints.
// Server-generated fields (id, created, updated, deploymentState) are stripped.
// Scope contains enriched group objects (id + name + type) for cross-instance portability.
type blueprintExport struct {
	Name        string                     `json:"name" yaml:"name"`
	Description string                     `json:"description,omitempty" yaml:"description,omitempty"`
	Scope       blueprintExportScope       `json:"scope" yaml:"scope"`
	Steps       []blueprints.BlueprintStep `json:"steps" yaml:"steps"`
}

func blueprintToExport(ctx context.Context, c *jamfplatform.Client, bp *blueprints.BlueprintDetail) blueprintExport {
	var groupIDs []string
	if bp.Scope != nil {
		groupIDs = bp.Scope.DeviceGroups
	}
	groups := reverseResolveGroups(ctx, c, groupIDs)
	desc := ""
	if bp.Description != nil {
		desc = *bp.Description
	}
	return blueprintExport{
		Name:        bp.Name,
		Description: desc,
		Scope:       blueprintExportScope{DeviceGroups: groups},
		Steps:       bp.Steps,
	}
}

func newBlueprintsExportCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var nameFlag string
	cmd := &cobra.Command{
		Use:   "export [<id>]",
		Short: "Export a blueprint as portable JSON or YAML",
		Long: `Export a blueprint in a portable format suitable for cross-instance cloning.

Scope device groups are enriched with their names and types so the export can
be applied to a different Jamf instance where group UUIDs differ. Group names
are resolved on the target instance at apply time.

Cross-instance workflow:
  jamf-cli -p source pro bp export MyBlueprint > bp.json
  jamf-cli -p target pro bp apply --from-file bp.json
  jamf-cli -p target pro bp apply --from-file bp.json --computer-group "Lab Macs"

Multi-instance fan-out:
  jamf-cli -p source pro bp export MyBlueprint > bp.json
  jamf-cli multi --filter 'school-*' -- pro bp apply --from-file bp.json --yes`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			ctx := cmd.Context()
			id, err := resolveBlueprintID(ctx, cliCtx, args, nameFlag)
			if err != nil {
				return err
			}
			bp, err := blueprints.New(cliCtx.PlatformSDKClient).GetBlueprint(ctx, id)
			if err != nil {
				return err
			}
			return printExport(blueprintToExport(ctx, cliCtx.PlatformSDKClient, bp))
		},
	}
	cmd.Flags().StringVar(&nameFlag, "name", "", "Look up blueprint by name")
	return cmd
}

func newBlueprintsScopeCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scope",
		Short: "Manage blueprint scope (device groups)",
	}
	cmd.AddCommand(newBlueprintsScopeListCmd(cliCtx))
	cmd.AddCommand(newBlueprintsScopeAddCmd(cliCtx))
	cmd.AddCommand(newBlueprintsScopeRemoveCmd(cliCtx))
	return cmd
}

func newBlueprintsScopeListCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var nameFlag string
	cmd := &cobra.Command{
		Use:   "list [<id>]",
		Short: "List device groups in a blueprint's scope",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			id, err := resolveBlueprintID(cmd.Context(), cliCtx, args, nameFlag)
			if err != nil {
				return err
			}
			bp, err := blueprints.New(cliCtx.PlatformSDKClient).GetBlueprint(cmd.Context(), id)
			if err != nil {
				return err
			}
			var deviceGroups []string
			if bp.Scope != nil {
				deviceGroups = bp.Scope.DeviceGroups
			}
			rows := make([]map[string]any, 0, len(deviceGroups))
			for _, gid := range deviceGroups {
				rows = append(rows, map[string]any{"deviceGroupId": gid})
			}
			data, err := json.Marshal(rows)
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}
	cmd.Flags().StringVar(&nameFlag, "name", "", "Look up blueprint by name")
	return cmd
}

func newBlueprintsScopeAddCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		nameFlag           string
		groupIDs           []string
		computerGroups     []string
		mobileDeviceGroups []string
	)
	cmd := &cobra.Command{
		Use:   "add [<id>]",
		Short: "Add device groups to a blueprint's scope",
		Long: `Add one or more device groups to a blueprint's scope.

Groups can be specified by platform UUID (--group-id) or by name
(--computer-group, --mobile-device-group). Names are resolved to
platform UUIDs via the /v1/groups API.

Examples:
  jamf-cli pro blueprints scope add <bp-id> --group-id <uuid>
  jamf-cli pro blueprints scope add --name "My Blueprint" --computer-group "All Managed Computers"
  jamf-cli pro blueprints scope add <bp-id> --mobile-device-group "Shared iPads"`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			hasNames := len(computerGroups) > 0 || len(mobileDeviceGroups) > 0
			if cliCtx.Client == nil && hasNames {
				return fmt.Errorf("--computer-group/--mobile-device-group requires Jamf Pro authentication for name resolution")
			}
			ctx := cmd.Context()
			id, err := resolveBlueprintID(ctx, cliCtx, args, nameFlag)
			if err != nil {
				return err
			}

			// Resolve group names to platform UUIDs
			addIDs, err := resolveAllGroupFlags(ctx, cliCtx.Client, groupIDs, computerGroups, mobileDeviceGroups)
			if err != nil {
				return err
			}

			// Get current scope
			bp, err := blueprints.New(cliCtx.PlatformSDKClient).GetBlueprint(ctx, id)
			if err != nil {
				return err
			}

			// Merge — deduplicate
			var currentGroups []string
			if bp.Scope != nil {
				currentGroups = bp.Scope.DeviceGroups
			}
			existing := make(map[string]bool, len(currentGroups))
			for _, gid := range currentGroups {
				existing[gid] = true
			}
			newScope := append([]string{}, currentGroups...)
			var added int
			for _, gid := range addIDs {
				if existing[gid] {
					fmt.Fprintf(os.Stderr, "  group %s already in scope, skipping\n", gid)
					continue
				}
				newScope = append(newScope, gid)
				existing[gid] = true
				added++
			}
			if added == 0 {
				fmt.Fprintln(os.Stderr, "No new groups to add")
				return nil
			}

			updateReq := &blueprints.UpdateBlueprintRequest{
				Scope: &blueprints.BlueprintScope{
					DeviceGroups: newScope,
				},
			}
			if err := blueprints.New(cliCtx.PlatformSDKClient).UpdateBlueprint(ctx, id, updateReq); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Added %d group(s) to blueprint scope\n", added)
			return nil
		},
	}
	cmd.Flags().StringVar(&nameFlag, "name", "", "Look up blueprint by name")
	cmd.Flags().StringSliceVar(&groupIDs, "group-id", nil, "Device group platform UUID (repeatable)")
	cmd.Flags().StringSliceVar(&computerGroups, "computer-group", nil, "Computer group name (repeatable)")
	cmd.Flags().StringSliceVar(&mobileDeviceGroups, "mobile-device-group", nil, "Mobile device group name (repeatable)")
	return cmd
}

func newBlueprintsScopeRemoveCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		nameFlag           string
		groupIDs           []string
		computerGroups     []string
		mobileDeviceGroups []string
	)
	cmd := &cobra.Command{
		Use:   "remove [<id>]",
		Short: "Remove device groups from a blueprint's scope",
		Long: `Remove one or more device groups from a blueprint's scope.

Groups can be specified by platform UUID (--group-id) or by name
(--computer-group, --mobile-device-group).

Examples:
  jamf-cli pro blueprints scope remove <bp-id> --group-id <uuid>
  jamf-cli pro blueprints scope remove --name "My Blueprint" --computer-group "Lab Macs"`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			hasNames := len(computerGroups) > 0 || len(mobileDeviceGroups) > 0
			if cliCtx.Client == nil && hasNames {
				return fmt.Errorf("--computer-group/--mobile-device-group requires Jamf Pro authentication for name resolution")
			}
			ctx := cmd.Context()
			id, err := resolveBlueprintID(ctx, cliCtx, args, nameFlag)
			if err != nil {
				return err
			}

			// Resolve group names to platform UUIDs
			removeIDs, err := resolveAllGroupFlags(ctx, cliCtx.Client, groupIDs, computerGroups, mobileDeviceGroups)
			if err != nil {
				return err
			}

			// Get current scope
			bp, err := blueprints.New(cliCtx.PlatformSDKClient).GetBlueprint(ctx, id)
			if err != nil {
				return err
			}

			// Filter out removed groups
			removeSet := make(map[string]bool, len(removeIDs))
			for _, gid := range removeIDs {
				removeSet[gid] = true
			}
			var currentGroups []string
			if bp.Scope != nil {
				currentGroups = bp.Scope.DeviceGroups
			}
			var newScope []string
			var removed int
			for _, gid := range currentGroups {
				if removeSet[gid] {
					removed++
					continue
				}
				newScope = append(newScope, gid)
			}
			if removed == 0 {
				fmt.Fprintln(os.Stderr, "No matching groups found in scope")
				return nil
			}

			updateReq := &blueprints.UpdateBlueprintRequest{
				Scope: &blueprints.BlueprintScope{
					DeviceGroups: newScope,
				},
			}
			if err := blueprints.New(cliCtx.PlatformSDKClient).UpdateBlueprint(ctx, id, updateReq); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Removed %d group(s) from blueprint scope\n", removed)
			return nil
		},
	}
	cmd.Flags().StringVar(&nameFlag, "name", "", "Look up blueprint by name")
	cmd.Flags().StringSliceVar(&groupIDs, "group-id", nil, "Device group platform UUID (repeatable)")
	cmd.Flags().StringSliceVar(&computerGroups, "computer-group", nil, "Computer group name (repeatable)")
	cmd.Flags().StringSliceVar(&mobileDeviceGroups, "mobile-device-group", nil, "Mobile device group name (repeatable)")
	return cmd
}

// resolveGroupNames resolves a slice of group names to platform UUIDs.
// groupType may be "COMPUTER", "MOBILE", or "" to match any type.
func resolveGroupNames(ctx context.Context, client registry.HTTPClient, names []string, groupType string) ([]string, error) {
	var ids []string
	for _, name := range names {
		id, err := resolveGroupPlatformID(ctx, client, name, groupType)
		if err != nil {
			return nil, fmt.Errorf("resolving group %q: %w", name, err)
		}
		fmt.Fprintf(os.Stderr, "  Resolved group %q → %s\n", name, id)
		ids = append(ids, id)
	}
	return ids, nil
}

// resolveAllGroupFlags resolves --group-id, --computer-group, and --mobile-device-group
// flags into a single slice of platform UUIDs. Returns an error if none are provided.
func resolveAllGroupFlags(ctx context.Context, httpClient registry.HTTPClient, groupIDs, computerGroups, mobileDeviceGroups []string) ([]string, error) {
	ids := append([]string{}, groupIDs...)
	resolved, err := resolveGroupNames(ctx, httpClient, computerGroups, "COMPUTER")
	if err != nil {
		return nil, err
	}
	ids = append(ids, resolved...)
	resolved, err = resolveGroupNames(ctx, httpClient, mobileDeviceGroups, "MOBILE")
	if err != nil {
		return nil, err
	}
	ids = append(ids, resolved...)
	if len(ids) == 0 {
		return nil, fmt.Errorf("specify at least one --group-id, --computer-group, or --mobile-device-group")
	}
	return ids, nil
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
			comps, err := blueprints.New(cliCtx.PlatformSDKClient).ListBlueprintComponents(cmd.Context())
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
			bpClient := blueprints.New(cliCtx.PlatformSDKClient)

			sourceID, err := bpClient.ResolveBlueprintIDByName(ctx, args[0])
			if err != nil {
				return fmt.Errorf("source blueprint: %w", err)
			}
			source, err := bpClient.GetBlueprint(ctx, sourceID)
			if err != nil {
				return fmt.Errorf("source blueprint: %w", err)
			}

			var groups []string
			if source.Scope != nil {
				groups = source.Scope.DeviceGroups
			}
			if len(scopeGroups) > 0 {
				groups = scopeGroups
			}
			steps := randomizePayloadIdentifiers(source.Steps)

			createReq := &blueprints.CreateBlueprintRequest{
				Name:        args[1],
				Description: source.Description,
				Scope: blueprints.CreateScope{
					DeviceGroups: groups,
				},
				Steps: steps,
			}

			result, err := bpClient.CreateBlueprint(ctx, createReq)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Cloned blueprint %q → %q (id: %s)\n", args[0], args[1], result.ID)

			bp, err := bpClient.GetBlueprint(ctx, result.ID)
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
			comp, err := blueprints.New(cliCtx.PlatformSDKClient).GetBlueprintComponent(cmd.Context(), args[0])
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
		profileID     string
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

Or download an existing profile from Jamf Pro by ID or name:
  jamf-cli pro blueprints components configuration-profile --id 42
  jamf-cli pro blueprints components configuration-profile --name "My Restrictions"
  jamf-cli pro blueprints components configuration-profile --name "Managed Restrictions" --type mobile

Jamf Pro allows duplicate profile display names: when a name matches more than
one profile you are prompted to pick one, or with --no-input the command errors
listing the matching IDs. Use --id to skip the lookup entirely.

Only preference domains that Apple supports for declarative management can be
used. Unsupported payload types will trigger a warning but are still included
in the output — the API will validate and reject if necessary.

Use --strip-defaults to remove keys that are set to their Apple default values.
This is useful for profiles created by Jamf Pro's UI which sets every key even
when the administrator only intended to manage a few settings.

Supported payloads: https://github.com/apple/device-management/tree/release/mdm/profiles`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if profileType != "computer" && profileType != "mobile" {
				return fmt.Errorf("--type must be 'computer' or 'mobile', got %q", profileType)
			}

			var data []byte
			var err error

			if profileName != "" || profileID != "" {
				if profileID != "" && !isClassicID(profileID) {
					return fmt.Errorf("--id must be a numeric Classic API ID, got %q (use --name for a display name)", profileID)
				}
				// Download from Jamf Pro classic API
				data, err = downloadClassicProfile(cmd, cliCtx, profileID, profileName, profileType)
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
	cmd.Flags().StringVar(&profileID, "id", "", "Download profile from Jamf Pro by ID (requires Pro auth)")
	cmd.Flags().StringVar(&profileType, "type", "computer", "Profile type when using --id/--name: computer or mobile")
	cmd.Flags().BoolVar(&stripDefaults, "strip-defaults", false, "Remove keys set to Apple's default values (fetches schemas from GitHub)")
	cmd.MarkFlagsMutuallyExclusive("from-file", "name", "id")
	_ = cmd.RegisterFlagCompletionFunc("type", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"computer", "mobile"}, cobra.ShellCompDirectiveNoFileComp
	})
	return cmd
}

// classicProfileCollection returns the Classic API collection segment for a
// configuration profile type. profileType must be "computer" or "mobile".
func classicProfileCollection(profileType string) string {
	if profileType == "mobile" {
		return "mobiledeviceconfigurationprofiles"
	}
	return "osxconfigurationprofiles"
}

// classicProfilePath returns the Classic API path for a profile by numeric ID.
// profileType must be "computer" or "mobile".
func classicProfilePath(profileType, id string) string {
	return "/JSSResource/" + classicProfileCollection(profileType) + "/id/" + url.PathEscape(id)
}

// otherProfileType returns the profile type that profileType is not, used to
// hint when a name only exists under the other type.
func otherProfileType(profileType string) string {
	if profileType == "mobile" {
		return "computer"
	}
	return "mobile"
}

// isClassicID reports whether s is a Classic API numeric ID rather than a name.
func isClassicID(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// findClassicProfileByName resolves a configuration profile name to its Classic
// API ID, reusing the generated classic commands' list-based resolver so name
// lookups behave the same here as in `pro classic-macos-config-profiles`:
// case-insensitive match, and duplicate names either prompt for a pick or (with
// --no-input) error listing the colliding IDs. Returns "" when nothing matches.
//
// Listing beats the /JSSResource/.../name/{name} shortcut: Jamf Pro allows
// duplicate profile display names, and the name endpoint silently returns
// whichever one the server picks. Listing also sidesteps the name path's known
// limits — "/" in a name is rejected outright and "+" is dropped by the platform
// gateway (see docs/solutions/conventions/classic-api-name-path-encoding).
// allowPrompt is false for speculative lookups (the wrong-`--type` hint below),
// which must never stop to ask the user about a profile they didn't name.
//
// The resolver's own collision wording ("use update with a specific ID") is
// written for the generated apply/update paths and doesn't fit here — nothing
// is being replaced, so we discard it and phrase our own remedy (re-run with
// one of the IDs) via the typed *generated.ClassicNameCollisionError.
func findClassicProfileByName(ctx context.Context, client registry.HTTPClient, profileType, name string, allowPrompt bool) (string, error) {
	collection := classicProfileCollection(profileType)
	wrapperKey := "os_x_configuration_profiles"
	if profileType == "mobile" {
		wrapperKey = "configuration_profiles"
	}
	id, err := generated.ResolveClassicNameToID(ctx, client, collection, wrapperKey, name, "use", noInput || !allowPrompt)
	if err != nil {
		var collision *generated.ClassicNameCollisionError
		if errors.As(err, &collision) {
			return "", fmt.Errorf("%w; re-run with one of these IDs instead of a name", collision)
		}
		return "", err
	}
	return id, nil
}

// resolveClassicProfileID turns the identifier a user supplied into a Classic
// API profile ID. A numeric positional argument is used as the ID directly;
// anything else is treated as a display name, as is an explicit --name. The
// returned name is empty when the lookup was by ID.
//
// Duplicate display names are what makes ID lookup necessary (issue #315): the
// name resolver prompts for the intended profile, or errors listing the
// colliding IDs under --no-input.
func resolveClassicProfileID(ctx context.Context, client registry.HTTPClient, profileType, arg, nameFlag string) (id, name string, err error) {
	switch {
	case nameFlag != "" && arg != "":
		return "", "", fmt.Errorf("specify either <id> or --name, not both")
	case nameFlag == "" && arg == "":
		return "", "", fmt.Errorf("provide a profile <id> or use --name")
	case nameFlag == "" && isClassicID(arg):
		return arg, "", nil
	}

	name = nameFlag
	if name == "" {
		name = arg
	}

	id, err = findClassicProfileByName(ctx, client, profileType, name, true)
	if err != nil {
		return "", "", err
	}
	if id != "" {
		return id, name, nil
	}

	// A name that only exists under the other type is a --type mistake.
	other := otherProfileType(profileType)
	if otherID, otherErr := findClassicProfileByName(ctx, client, other, name, false); otherErr == nil && otherID != "" {
		return "", "", fmt.Errorf("no %s configuration profile named %q — a %s profile matches; re-run with --type %s",
			profileType, name, other, other)
	}
	return "", "", fmt.Errorf("no %s configuration profile named %q found", profileType, name)
}

// classicProfileLabel describes a resolved profile for log and error messages.
func classicProfileLabel(id, name string) string {
	if name != "" {
		return fmt.Sprintf("%q (id %s)", name, id)
	}
	return "id " + id
}

// fetchClassicProfile resolves a profile identifier (numeric ID, or a name via
// the positional argument or --name) and returns the raw Classic API XML body
// alongside the resolved ID and name. profileType must be "computer" or "mobile".
func fetchClassicProfile(ctx context.Context, cliCtx *registry.CLIContext, profileType, arg, nameFlag string) (body []byte, id, name string, err error) {
	if cliCtx.Client == nil {
		return nil, "", "", fmt.Errorf("downloading a profile requires Jamf Pro authentication (use 'jamf-cli pro setup' or set JAMF_* env vars)")
	}

	id, name, err = resolveClassicProfileID(ctx, cliCtx.Client, profileType, arg, nameFlag)
	if err != nil {
		return nil, "", "", err
	}
	label := classicProfileLabel(id, name)

	resp, err := cliCtx.Client.Do(ctx, "GET", classicProfilePath(profileType, id), nil)
	if err != nil {
		return nil, "", "", fmt.Errorf("fetching %s profile %s: %w", profileType, label, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, "", "", fmt.Errorf("%s profile %s not found (HTTP %d)", profileType, label, resp.StatusCode)
	}

	body, err = jamfclient.ReadResponseBody(resp)
	if err != nil {
		return nil, "", "", fmt.Errorf("reading profile response: %w", err)
	}
	return body, id, name, nil
}

// downloadClassicProfile fetches a configuration profile from the Jamf Pro
// Classic API and extracts the mobileconfig XML from the response.
// profileType must be "computer" or "mobile".
func downloadClassicProfile(cmd *cobra.Command, cliCtx *registry.CLIContext, arg, nameFlag, profileType string) ([]byte, error) {
	body, id, name, err := fetchClassicProfile(cmd.Context(), cliCtx, profileType, arg, nameFlag)
	if err != nil {
		return nil, err
	}
	label := classicProfileLabel(id, name)

	// Extract the <payloads> content from the Classic API XML response
	mobileconfig := extractPayloadsFromXML(string(body))
	if mobileconfig == "" {
		return nil, fmt.Errorf("no <payloads> found in %s profile %s response", profileType, label)
	}

	fmt.Fprintf(os.Stderr, "Downloaded %s profile %s from Jamf Pro\n", profileType, label)
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
	return cmd
}

// warnAPIOnlyPayloads inspects a configuration-profile component's config and,
// if it contains payload types the Jamf Pro UI cannot manage directly, prints
// an advisory naming them. Those payloads show as read-only "Legacy payload"
// items in the UI and can only be edited through the blueprints API. Payload
// types the UI can manage (UIManageablePayloadTypes) produce no warning.
func warnAPIOnlyPayloads(config json.RawMessage) {
	apiOnly := profileconvert.APIOnlyPayloadTypes(config)
	if len(apiOnly) == 0 {
		return
	}
	fmt.Fprintln(os.Stderr, "Note: the following payload(s) can only be managed through the blueprints API — "+
		"they show as read-only \"Legacy payload\" items in the Jamf Pro UI and cannot be edited there:")
	for _, pt := range apiOnly {
		fmt.Fprintf(os.Stderr, "  - %s\n", pt)
	}
}

func newBlueprintsImportProfileCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		blueprintName      string
		profileName        string
		profileType        string
		includeUnsupported bool
		stripDefaults      bool
		noConvert          bool
		computerGroups     []string
		mobileDeviceGroups []string
	)
	cmd := &cobra.Command{
		Use:   "import-profile [<id>]",
		Short: "Import a Classic configuration profile as a blueprint",
		Long: `Download a configuration profile from Jamf Pro, convert its payloads
to native DDM blueprint components where possible, and create the blueprint.

Payloads are automatically promoted to native DDM components when a mapping
exists. Currently supported:

  com.apple.mobiledevice.passwordpolicy  ->  passcode-settings
  com.apple.applicationaccess (safari*)  ->  safari-settings (macOS/iOS 26+)
  com.apple.applicationaccess (deferral) ->  software-update-settings (Deferrals)
  com.apple.applicationaccess (RSR)      ->  software-update-settings (RapidSecurityResponse)
  com.apple.SoftwareUpdate               ->  software-update-settings (AutomaticActions, Beta, etc.)

Payloads without a DDM mapping are wrapped in a com.jamf.ddm-configuration-profile
component. A single profile with mixed payloads produces multiple components.
Some of these configuration-profile ("legacy payload") components can only be
managed through the blueprints API — they appear as read-only "Legacy payload"
items in the Jamf Pro UI and cannot be edited there.

The configuration-profile component takes a fixed set of payload types as standalone
payloads — not every Apple payload, despite what the API reference says. Types outside
that set (com.apple.MCX, com.apple.Safari, com.apple.SoftwareUpdate, com.apple.Terminal,
com.apple.systemuiserver, third-party preference domains, ...) are delivered as
Application & Custom Settings (MCX) instead, which the API accepts for any domain and
which is their correct legacy delivery. A separate set is disabled outright by
blueprints (certificates, VPN, SSO, web clips, fonts, etc.); those are skipped with a
warning. Use --include-unsupported to send the disabled ones anyway (the API rejects them).

Application & Custom Settings (MCX) payloads are unwrapped only when their inner
preference domain is one the API accepts standalone, or one a DDM converter can
consume. Everything else stays wrapped as opaque Custom Settings.

Identify the profile by its Classic API ID (positional) or by display name
(--name). A non-numeric positional argument is treated as a display name, so
existing name-based invocations keep working — but a profile whose display name
is entirely digits (e.g. "2024") is resolved as an ID; use --name for those.
Jamf Pro allows duplicate profile display names: when a name matches more than
one profile you are prompted to pick one, or with --no-input the command errors
listing the matching IDs so you can re-run against a specific ID.

Use --type to specify the profile type: "computer" (default) for macOS configuration
profiles or "mobile" for mobile device configuration profiles. Profiles can share
names across types, so the flag is required when ambiguous.

The blueprint name defaults to the profile's display name (override with --blueprint-name).

Use --strip-defaults to remove keys that are set to their Apple default values.
This is useful for profiles created by Jamf Pro's UI which sets every key even
when the administrator only intended to manage a few settings. Default stripping
applies only to the configuration-profile wrapper, not native DDM components.

Scope handling:
  Only target computer groups and mobile device groups are carried over to the
  blueprint scope. Individual computer/device assignments, buildings, departments,
  limitations, and exclusions are NOT imported — blueprints only support device
  group scoping. You will be warned about any scope elements that are dropped.

  Because blueprints require at least one device group, a profile whose scope has
  no groups (e.g. scoped to all computers or to individual devices) cannot be
  imported as-is: the command errors before calling the API. Use --computer-group
  or --mobile-device-group to set the scope explicitly. These flags also override
  the profile's own scope when you want to target different groups.

Examples:
  jamf-cli pro blueprints import-profile 42
  jamf-cli pro blueprints import-profile --name "Passcode Policy"
  jamf-cli pro blueprints import-profile "My Restrictions"
  jamf-cli pro blueprints import-profile --name "Managed Restrictions" --type mobile
  jamf-cli pro blueprints import-profile 42 --blueprint-name "FV Blueprint"
  jamf-cli pro blueprints import-profile "My Restrictions" --strip-defaults
  jamf-cli pro blueprints import-profile "Software Update" --computer-group "All Managed"
  jamf-cli pro blueprints import-profile "My Restrictions" --include-unsupported`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if profileType != "computer" && profileType != "mobile" {
				return fmt.Errorf("--type must be 'computer' or 'mobile', got %q", profileType)
			}
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			if cliCtx.Client == nil {
				return fmt.Errorf("import-profile requires Jamf Pro authentication")
			}
			ctx := cmd.Context()

			var arg string
			if len(args) > 0 {
				arg = args[0]
			}

			// Step 1: Resolve the identifier and download the Classic API profile
			body, profileID, resolvedName, err := fetchClassicProfile(ctx, cliCtx, profileType, arg, profileName)
			if err != nil {
				return err
			}
			profileLabel := classicProfileLabel(profileID, resolvedName)
			fmt.Fprintf(os.Stderr, "Downloaded %s profile %s from Jamf Pro\n", profileType, profileLabel)

			// Step 2: Extract mobileconfig from <payloads>
			mobileconfig := extractPayloadsFromXML(string(body))
			if mobileconfig == "" {
				return fmt.Errorf("no <payloads> found in %s profile %s", profileType, profileLabel)
			}

			// Step 3: Convert mobileconfig to blueprint components.
			displayName := profileconvert.ProfileDisplayName([]byte(mobileconfig))
			var components []blueprints.Component

			if noConvert {
				// Legacy mode: wrap all payloads in a single configuration-profile component.
				config, warnings, err := profileconvert.ConvertMobileconfig([]byte(mobileconfig), !includeUnsupported)
				if err != nil {
					return fmt.Errorf("converting profile: %w", err)
				}
				for _, w := range warnings {
					fmt.Fprintf(os.Stderr, "Warning: %s\n", w)
				}
				if stripDefaults {
					fetcher := profileconvert.NewSchemaFetcher(nil)
					var msgs []string
					config, msgs = profileconvert.StripConfigDefaults(config, fetcher)
					for _, m := range msgs {
						fmt.Fprintf(os.Stderr, "  %s\n", m)
					}
				}
				if err := profileconvert.ConfigHasPayloads(config); err != nil {
					return fmt.Errorf("no payloads remain after processing")
				}
				types := profileconvert.PayloadTypeSummary([]byte(mobileconfig))
				fmt.Fprintf(os.Stderr, "Processed %d payload(s) (legacy mode — no DDM conversion)\n", len(types))
				warnAPIOnlyPayloads(config)
				components = append(components, blueprints.Component{
					Identifier:    "com.jamf.ddm-configuration-profile",
					Configuration: config,
				})
			} else {
				// DDM mode: promote compatible payloads to native DDM components.
				var ddmFetcher *profileconvert.SchemaFetcher
				if stripDefaults {
					ddmFetcher = profileconvert.NewSchemaFetcher(nil)
				}
				ddmResult, err := profileconvert.ConvertToDDMComponents([]byte(mobileconfig), !includeUnsupported, ddmFetcher)
				if err != nil {
					return fmt.Errorf("converting profile: %w", err)
				}
				for _, w := range ddmResult.Warnings {
					fmt.Fprintf(os.Stderr, "Warning: %s\n", w)
				}

				// Validate/strip the configuration-profile component (if any)
				if ddmResult.ProfileConfig != nil {
					fetcher := profileconvert.NewSchemaFetcher(nil)
					if stripDefaults {
						var msgs []string
						ddmResult.ProfileConfig, msgs = profileconvert.StripConfigDefaults(ddmResult.ProfileConfig, fetcher)
						for _, m := range msgs {
							fmt.Fprintf(os.Stderr, "  %s\n", m)
						}
					} else {
						var msgs []string
						ddmResult.ProfileConfig, msgs = profileconvert.ValidatePayloads(ddmResult.ProfileConfig, fetcher)
						for _, m := range msgs {
							fmt.Fprintf(os.Stderr, "  %s\n", m)
						}
					}
					if err := profileconvert.ConfigHasPayloads(ddmResult.ProfileConfig); err != nil {
						ddmResult.ProfileConfig = nil
					}
				}

				// Print conversion summary
				types := profileconvert.PayloadTypeSummary([]byte(mobileconfig))
				fmt.Fprintf(os.Stderr, "Processed %d payload(s)\n", len(types))
				for _, c := range ddmResult.Conversions {
					fmt.Fprintf(os.Stderr, "  %s (native DDM)\n", c)
				}
				if ddmResult.ProfileConfig != nil {
					fmt.Fprintln(os.Stderr, "  remaining payloads wrapped in configuration-profile component")
					warnAPIOnlyPayloads(ddmResult.ProfileConfig)
				}

				for _, nc := range ddmResult.NativeComponents {
					components = append(components, blueprints.Component{
						Identifier:    nc.Identifier,
						Configuration: nc.Configuration,
					})
				}
				if ddmResult.ProfileConfig != nil {
					components = append(components, blueprints.Component{
						Identifier:    "com.jamf.ddm-configuration-profile",
						Configuration: ddmResult.ProfileConfig,
					})
				}
			}

			if len(components) == 0 {
				return fmt.Errorf("no components produced — all payloads were stripped or unsupported")
			}

			// Step 4: Determine scope. --computer-group/--mobile-device-group override
			// the profile's own scope; otherwise carry over the profile's target groups.
			var scopeGroups []string
			if len(computerGroups) > 0 || len(mobileDeviceGroups) > 0 {
				scopeGroups, err = resolveAllGroupFlags(ctx, cliCtx.Client, nil, computerGroups, mobileDeviceGroups)
				if err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "Scope overridden with %d group(s) from flags\n", len(scopeGroups))
			} else {
				var scopeWarnings []string
				scopeGroups, scopeWarnings = extractAndResolveScope(ctx, cliCtx.Client, body)
				for _, w := range scopeWarnings {
					fmt.Fprintf(os.Stderr, "Warning: %s\n", w)
				}
				if len(scopeGroups) > 0 {
					fmt.Fprintf(os.Stderr, "Resolved %d scope group(s) to platform UUIDs\n", len(scopeGroups))
				}
			}

			// Blueprints require a non-empty device-group scope. Fail fast with an
			// actionable message instead of letting CreateBlueprint 400.
			if len(scopeGroups) == 0 {
				return fmt.Errorf("%s profile %s has no device-group scope that blueprints can use — "+
					"re-run with --computer-group \"<name>\" (or --mobile-device-group) to set the scope.\n"+
					"Blueprints only support device-group scoping; all-computers, individual-device, "+
					"building, and department scopes are not carried over", profileType, profileLabel)
			}

			// Step 5: Build and create the blueprint
			name := blueprintName
			if name == "" {
				name = displayName
			}
			if name == "" {
				name = resolvedName
			}
			if name == "" {
				name = "Imported profile " + profileID
			}

			importStepName := "Step 1"
			createReq := &blueprints.CreateBlueprintRequest{
				Name: name,
				Scope: blueprints.CreateScope{
					DeviceGroups: scopeGroups,
				},
				Steps: []blueprints.BlueprintStep{
					{
						Name:       &importStepName,
						Components: components,
					},
				},
			}

			fmt.Fprintln(os.Stderr, profileconvert.ConflictWarning)

			result, err := blueprints.New(cliCtx.PlatformSDKClient).CreateBlueprint(ctx, createReq)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Created blueprint %q (id: %s)\n", name, result.ID)

			bp, err := blueprints.New(cliCtx.PlatformSDKClient).GetBlueprint(ctx, result.ID)
			if err != nil {
				return err
			}
			return printResult(cliCtx.Output, bp, flattenBlueprintDetail(*bp))
		},
	}
	cmd.Flags().StringVar(&blueprintName, "blueprint-name", "", "Override the blueprint name (defaults to profile display name)")
	cmd.Flags().StringVar(&profileName, "name", "", "Look up the configuration profile by display name instead of <id>")
	cmd.Flags().StringVar(&profileType, "type", "computer", "Profile type: computer (macOS) or mobile (iOS/iPadOS/tvOS)")
	cmd.Flags().BoolVar(&noConvert, "legacy", false, "Wrap all payloads in a single configuration-profile component without DDM conversion")
	cmd.Flags().BoolVar(&includeUnsupported, "include-unsupported", false, "Send payloads blueprints disables anyway (the API will reject them; by default they are skipped)")
	cmd.Flags().BoolVar(&stripDefaults, "strip-defaults", false, "Remove keys set to Apple's default values (fetches schemas from GitHub)")
	cmd.Flags().StringSliceVar(&computerGroups, "computer-group", nil, "Set the blueprint scope to these computer group name(s), overriding the profile's scope (repeatable)")
	cmd.Flags().StringSliceVar(&mobileDeviceGroups, "mobile-device-group", nil, "Set the blueprint scope to these mobile device group name(s), overriding the profile's scope (repeatable)")
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
	scopeEnd := strings.LastIndex(xmlStr[scopeStart:], "</scope>")
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

	// Collect group names to resolve, tagged by type
	type groupRef struct {
		name      string
		groupType string
	}
	var refs []groupRef
	for _, g := range s.ComputerGroups.Items {
		refs = append(refs, groupRef{g.Name, "COMPUTER"})
	}
	for _, g := range s.MobileDeviceGroups.Items {
		refs = append(refs, groupRef{g.Name, "MOBILE"})
	}

	if len(refs) == 0 {
		return nil, warnings
	}

	// Resolve each group name to a platform UUID via /v1/groups
	var platformIDs []string
	for _, ref := range refs {
		id, err := resolveGroupPlatformID(ctx, client, ref.name, ref.groupType)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("could not resolve group %q (%s) to platform UUID: %v", ref.name, ref.groupType, err))
			continue
		}
		platformIDs = append(platformIDs, id)
		fmt.Fprintf(os.Stderr, "  Resolved group %q (%s) → %s\n", ref.name, ref.groupType, id)
	}

	return platformIDs, warnings
}

// resolveGroupPlatformID queries /v1/groups with an RSQL filter to find the
// platform UUID for a group by name. groupType may be "COMPUTER", "MOBILE",
// or "" to match any type.
func resolveGroupPlatformID(ctx context.Context, client registry.HTTPClient, groupName, groupType string) (string, error) {
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `*`, `\*`, `(`, `\(`, `)`, `\)`, `;`, `\;`).Replace(groupName)
	filter := fmt.Sprintf(`groupName=="%s"`, escaped)
	if groupType != "" {
		filter += fmt.Sprintf(` and groupType=="%s"`, groupType)
	}
	path := "/v1/groups?page-size=1&filter=" + url.QueryEscape(filter)

	resp, err := client.Do(ctx, "GET", path, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := jamfclient.ReadResponseBody(resp)
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
		if groupType != "" {
			return "", fmt.Errorf("no %s group found with name %q", groupType, groupName)
		}
		return "", fmt.Errorf("no group found with name %q", groupName)
	}

	return result.Results[0].GroupPlatformID, nil
}

// extractPayloadsFromXML extracts the content between <payloads> tags from
// Classic API XML. The content may be CDATA-wrapped or XML entity-encoded.
// Uses string indexing rather than xml.Decoder because the Classic API response
// shape is well-known and the payload content itself is opaque (plist XML that
// we don't want a decoder to interpret).
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
	// Classic API may entity-encode the XML instead of using CDATA.
	// html.UnescapeString handles the standard XML entities (&lt; &gt; &amp;
	// &quot; &#34;) that the Classic API produces. It also handles HTML5 named
	// entities, but those never appear in Classic API output.
	if strings.Contains(content, "&lt;") {
		content = html.UnescapeString(content)
	}
	return content
}

// randomizePayloadIdentifiers walks through blueprint steps and replaces
// payloadIdentifier values in legacy configuration profile components
// (com.jamf.ddm-configuration-profile) with fresh UUIDs.
func randomizePayloadIdentifiers(steps []blueprints.BlueprintStep) []blueprints.BlueprintStep {
	out := make([]blueprints.BlueprintStep, len(steps))
	for i, step := range steps {
		comps := make([]blueprints.Component, len(step.Components))
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
		out[i] = blueprints.BlueprintStep{
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

// reverseResolveGroups maps platform device group UUIDs to enriched metadata
// (name, device type) by fetching the full device groups list. Groups that
// cannot be found (deleted, etc.) are included with their UUID and empty metadata.
func reverseResolveGroups(ctx context.Context, c *jamfplatform.Client, ids []string) []blueprintExportScopeGroup {
	if len(ids) == 0 {
		return nil
	}

	allGroups, err := devicegroups.New(c).ListDeviceGroups(ctx, nil, "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "  Warning: could not list device groups (%v), export will contain UUIDs only (not portable)\n", err)
	}
	groupMap := make(map[string]devicegroups.DeviceGroupListReadRepresentationV1, len(allGroups))
	for _, g := range allGroups {
		groupMap[g.ID] = g
	}

	result := make([]blueprintExportScopeGroup, 0, len(ids))
	for _, id := range ids {
		if g, ok := groupMap[id]; ok {
			result = append(result, blueprintExportScopeGroup{
				ID:         id,
				Name:       g.Name,
				DeviceType: g.DeviceType,
			})
		} else {
			fmt.Fprintf(os.Stderr, "  Warning: group %s not found, including UUID only\n", id)
			result = append(result, blueprintExportScopeGroup{ID: id})
		}
	}
	return result
}

// resolvePortableScopeGroups takes enriched scope groups from a portable export
// and resolves group names to platform UUIDs on the target instance.
// Falls back to the embedded UUID if a group has no name (e.g., deleted on source).
func resolvePortableScopeGroups(ctx context.Context, client registry.HTTPClient, groups []blueprintExportScopeGroup) ([]string, error) {
	var ids []string
	for _, g := range groups {
		if g.Name == "" {
			if g.ID != "" {
				ids = append(ids, g.ID)
			}
			continue
		}
		if client == nil {
			return nil, fmt.Errorf("group name resolution requires Jamf Pro authentication")
		}
		groupType := deviceTypeToGroupType(g.DeviceType)
		id, err := resolveGroupPlatformID(ctx, client, g.Name, groupType)
		if err != nil {
			return nil, fmt.Errorf("resolving group %q: %w", g.Name, err)
		}
		fmt.Fprintf(os.Stderr, "  Resolved group %q → %s\n", g.Name, id)
		ids = append(ids, id)
	}
	return ids, nil
}

// deviceTypeToGroupType maps the Platform SDK DeviceType value to the
// /v1/groups API groupType filter value.
func deviceTypeToGroupType(deviceType string) string {
	switch {
	case strings.HasPrefix(deviceType, "COMPUTER"):
		return "COMPUTER"
	case strings.HasPrefix(deviceType, "MOBILE"):
		return "MOBILE"
	default:
		return "" // no type filter
	}
}

// parseBlueprintApplyInput parses raw JSON/YAML blueprint input, handling both
// the old format (scope with UUID strings) and the portable format (scope with
// enriched group objects). When the portable format is detected, group names
// are resolved to UUIDs on the target instance. scopeOverrideIDs, if non-empty,
// replaces the file's scope entirely.
func parseBlueprintApplyInput(ctx context.Context, data []byte, client registry.HTTPClient, scopeOverrideIDs []string) (*blueprints.CreateBlueprintRequest, error) {
	// Probe the scope format via a generic unmarshal to avoid type coercion
	// issues between JSON objects and YAML strings.
	portable := isPortableScopeFormat(data)

	if !portable {
		var req blueprints.CreateBlueprintRequest
		if err := unmarshalInput(data, &req); err != nil {
			return nil, fmt.Errorf("parsing input: %w", err)
		}
		if req.Name == "" {
			return nil, fmt.Errorf("input must include a 'name' field")
		}
		if len(scopeOverrideIDs) > 0 {
			req.Scope.DeviceGroups = scopeOverrideIDs
		}
		return &req, nil
	}

	// Portable format: scope.deviceGroups contains group objects
	var exp blueprintExport
	if err := unmarshalInput(data, &exp); err != nil {
		return nil, fmt.Errorf("parsing portable format: %w", err)
	}
	if exp.Name == "" {
		return nil, fmt.Errorf("input must include a 'name' field")
	}

	var groupIDs []string
	if len(scopeOverrideIDs) > 0 {
		groupIDs = scopeOverrideIDs
	} else if len(exp.Scope.DeviceGroups) > 0 {
		var err error
		groupIDs, err = resolvePortableScopeGroups(ctx, client, exp.Scope.DeviceGroups)
		if err != nil {
			return nil, err
		}
	}

	req := &blueprints.CreateBlueprintRequest{
		Name: exp.Name,
		Scope: blueprints.CreateScope{
			DeviceGroups: groupIDs,
		},
		Steps: exp.Steps,
	}
	if exp.Description != "" {
		req.Description = &exp.Description
	}
	return req, nil
}

// isPortableScopeFormat probes raw JSON/YAML data to detect whether the
// scope.deviceGroups field contains objects (portable format) or strings
// (legacy UUID format). Uses a generic unmarshal so that type detection
// works for both JSON and YAML inputs.
func isPortableScopeFormat(data []byte) bool {
	var raw map[string]any
	if err := unmarshalInput(data, &raw); err != nil {
		return false
	}
	scopeMap, ok := raw["scope"].(map[string]any)
	if !ok {
		return false
	}
	groups, ok := scopeMap["deviceGroups"].([]any)
	if !ok || len(groups) == 0 {
		return false
	}
	_, isMap := groups[0].(map[string]any)
	return isMap
}

// newUUID generates a random UUID v4 string.
func newUUID() string {
	var u [16]byte
	if _, err := rand.Read(u[:]); err != nil {
		panic("crypto/rand: " + err.Error())
	}
	u[6] = (u[6] & 0x0f) | 0x40 // version 4
	u[8] = (u[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:16])
}
