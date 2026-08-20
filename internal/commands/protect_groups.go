// Copyright 2026, Jamf Software LLC

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

func newProtectGroupsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "groups",
		Short: "Manage Jamf Protect groups",
	}

	cmd.AddCommand(newProtectGroupsListCmd(cliCtx))
	cmd.AddCommand(newProtectGroupsGetCmd(cliCtx))
	cmd.AddCommand(newProtectGroupsApplyCmd(cliCtx))
	cmd.AddCommand(newProtectGroupsDeleteCmd(cliCtx))
	cmd.AddCommand(newProtectGroupsExportCmd(cliCtx))

	return cmd
}

func newProtectGroupsListCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all groups",
		RunE: func(cmd *cobra.Command, _ []string) error {
			items, err := cliCtx.ProtectClient.ListGroups(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(items))
			for _, g := range items {
				rows = append(rows, flattenGroup(g))
			}
			data, err := json.Marshal(rows)
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}
}

// flattenGroup converts a Group into a clean map for readable table output,
// reducing nested objects to names.
func flattenGroup(g jamfprotect.Group) map[string]any {
	m := map[string]any{
		"name":        g.Name,
		"accessGroup": g.AccessGroup,
		"created":     g.Created,
		"updated":     g.Updated,
	}
	if g.Connection != nil {
		m["connection"] = g.Connection.Name
	}
	if len(g.AssignedRoles) > 0 {
		names := make([]string, 0, len(g.AssignedRoles))
		for _, r := range g.AssignedRoles {
			names = append(names, r.Name)
		}
		m["assignedRoles"] = strings.Join(names, ", ")
	}
	return m
}

func newProtectGroupsGetCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get <name>",
		Short: "Get a group by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)

			id, err := r.ResolveGroupID(ctx, args[0])
			if err != nil {
				return err
			}

			item, err := cliCtx.ProtectClient.GetGroup(ctx, id)
			if err != nil {
				return err
			}
			return printResult(cliCtx.Output, item, flattenGroup(*item))
		},
	}
}

// protectGroupUpdateSatisfied decides whether an existing group can be updated
// with this document, working around the one asymmetry between createGroup and
// updateGroup.
//
// createGroup accepts accessGroup: true on a connection-less local group;
// updateGroup refuses it outright — "Local groups cannot be designated as access
// groups" — *even when true is already the stored value*. Established on the wire:
// create with true succeeds and stores true, the identical document re-applied
// fails, and turning it off via update works but cannot be turned back on.
//
// satisfied reports that the target already holds the desired state, so the update
// must be skipped rather than attempted. An error means the change is impossible
// through update and says what to do about it. Shared by 'groups apply' and
// 'protect restore' so the same document behaves the same way through both.
func protectGroupUpdateSatisfied(existing *jamfprotect.Group, input jamfprotect.GroupInput) (bool, error) {
	isLocal := input.ConnectionID == nil || *input.ConnectionID == ""
	if !input.AccessGroup || !isLocal {
		return false, nil
	}
	if existing.AccessGroup {
		// Desired state already holds; updateGroup could only refuse it.
		return true, nil
	}
	return false, fmt.Errorf("group %q asks for accessGroup: true, but the target's copy has it disabled and "+
		"updateGroup refuses to designate a connection-less local group as an access group — the server allows it "+
		"only at create. Delete the group in the target and apply again, or clear accessGroup in the document", input.Name)
}

func newProtectGroupsApplyCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		fromFile string
		yes      bool
		scaffold bool
	)

	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Create or update a group",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if scaffold {
				// The export shape, not the SDK input shape: 'export' emits names now,
				// and a scaffold teaching roleids/connectionid teaches the form this
				// command only still accepts for backward compatibility.
				return printExport(groupExport{Roles: []string{}})
			}
			ctx := cmd.Context()
			data, err := readInput(fromFile)
			if err != nil {
				return err
			}
			r := protect.NewResolver(cliCtx.ProtectClient)
			input, err := groupInputFromDocument(ctx, data, r)
			if err != nil {
				return fmt.Errorf("parsing input file: %w", err)
			}

			if input.Name == "" {
				return fmt.Errorf("input must include a 'name' field")
			}

			// Check if group exists by name
			id, err := r.ResolveGroupID(ctx, input.Name)
			if err != nil {
				// Not found — create
				result, err := cliCtx.ProtectClient.CreateGroup(ctx, input)
				if err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "Created group %q\n", input.Name)
				return printResult(cliCtx.Output, result, flattenGroup(result))
			}

			// Found — confirm before replacing
			proceed, err := confirmReplace("group", input.Name, yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			// Same guard 'protect restore' applies. Without it, re-applying a local
			// access group's own export failed with the raw server message that says
			// nothing actionable.
			existing, err := cliCtx.ProtectClient.GetGroup(ctx, id)
			if err != nil {
				return err
			}
			satisfied, err := protectGroupUpdateSatisfied(existing, input)
			if err != nil {
				return err
			}
			if satisfied {
				fmt.Fprintf(os.Stderr, "Group %q is already an access group; nothing to update "+
					"(updateGroup cannot re-send that flag for a local group)\n", input.Name)
				return printResult(cliCtx.Output, *existing, flattenGroup(*existing))
			}

			result, err := cliCtx.ProtectClient.UpdateGroup(ctx, id, input)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Updated group %q\n", input.Name)
			return printResult(cliCtx.Output, result, flattenGroup(result))
		},
	}

	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON input file (or pipe JSON to stdin)")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt when replacing")
	cmd.Flags().BoolVar(&scaffold, "scaffold", false, "Print an empty JSON template and exit")

	return cmd
}

func newProtectGroupsDeleteCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:         "delete <name>",
		Short:       "Delete a group",
		Annotations: map[string]string{"jamf:destructive": "true"},
		Args:        cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)

			id, err := r.ResolveGroupID(ctx, args[0])
			if err != nil {
				return err
			}

			proceed, err := confirmDelete("group", args[0], yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			if err := cliCtx.ProtectClient.DeleteGroup(ctx, id); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Deleted group %q\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	return cmd
}

func newProtectGroupsExportCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "export <name>",
		Short: "Export a group as JSON or YAML",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := protect.NewResolver(cliCtx.ProtectClient)
			id, err := r.ResolveGroupID(ctx, args[0])
			if err != nil {
				return err
			}
			item, err := cliCtx.ProtectClient.GetGroup(ctx, id)
			if err != nil {
				return err
			}
			return printExport(groupToExport(item))
		},
	}
}
