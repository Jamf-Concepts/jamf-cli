// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	platformgen "github.com/Jamf-Concepts/jamf-cli/internal/commands/platform/generated"
	"github.com/Jamf-Concepts/jamf-cli/internal/platform"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/devicegroups"
)

func newPlatformDeviceGroupsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "platform-device-groups",
		Short: "Manage device groups (Platform API)",
		Long:  "Create and manage unified device groups via the Jamf Platform API. Requires platform gateway auth.",
	}

	// Generated CRUD: list, create. Skip --name-using ops; replaced below with
	// handwritten versions that add --device-type for COMPUTER/MOBILE disambiguation.
	needsType := map[string]bool{
		"get": true, "delete": true, "patch": true, "members": true, "patch-members": true,
	}
	for _, sub := range platformgen.NewDeviceGroupsCmd(cliCtx).Commands() {
		if needsType[sub.Name()] {
			continue
		}
		cmd.AddCommand(sub)
	}

	// Name-based CRUD with --device-type disambiguation
	cmd.AddCommand(newPDGGetCmd(cliCtx))
	cmd.AddCommand(newPDGDeleteCmd(cliCtx))
	cmd.AddCommand(newPDGPatchCmd(cliCtx))
	cmd.AddCommand(newPDGMembersCmd(cliCtx))
	cmd.AddCommand(newPDGPatchMembersCmd(cliCtx))

	// Business logic: upsert and ergonomic member mutations
	cmd.AddCommand(newPDGApplyCmd(cliCtx))
	cmd.AddCommand(newPDGAddMembersCmd(cliCtx))
	cmd.AddCommand(newPDGRemoveMembersCmd(cliCtx))

	return cmd
}

// pdgListPath returns the tenant-prefixed list endpoint for device groups.
func pdgListPath(c *jamfplatform.Client) string {
	return "/api/device-groups/v1/tenant/" + url.PathEscape(c.Transport().TenantID()) + "/device-groups"
}

// pdgResolveID resolves a device group name to its ID, optionally filtering by
// deviceType ("COMPUTER" or "MOBILE"). When deviceType is empty the lookup
// searches all groups; if two groups share a name the call errors with a hint
// to add --device-type.
func pdgResolveID(ctx context.Context, c *jamfplatform.Client, name, deviceType string) (string, error) {
	filter := ""
	if deviceType != "" {
		filter = fmt.Sprintf(`deviceType=="%s"`, deviceType)
	}
	return platform.ResolveIDByNameFiltered(ctx, c, pdgListPath(c), name, filter)
}

// normalizeDeviceTypeFlag uppercases the value and validates it is COMPUTER,
// MOBILE, or empty. Returns the normalized value and an error if invalid.
func normalizeDeviceTypeFlag(t string) (string, error) {
	upper := strings.ToUpper(t)
	if upper != "" && upper != "COMPUTER" && upper != "MOBILE" {
		return "", fmt.Errorf("--device-type must be COMPUTER or MOBILE (got %q)", t)
	}
	return upper, nil
}

func newPDGGetCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var nameFlag, deviceTypeFlag string
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Get a device group by ID",
		Long:  "Retrieve a specific device group by its ID",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			dt, err := normalizeDeviceTypeFlag(deviceTypeFlag)
			if err != nil {
				return err
			}
			var resolvedID string
			if nameFlag != "" {
				id, err := pdgResolveID(cmd.Context(), cliCtx.PlatformSDKClient, nameFlag, dt)
				if err != nil {
					return err
				}
				resolvedID = id
			} else if len(args) == 1 {
				resolvedID = args[0]
			} else {
				return fmt.Errorf("provide a positional ID or --name")
			}
			path := "/api/device-groups/v1/tenant/" + url.PathEscape(cliCtx.PlatformSDKClient.Transport().TenantID()) + "/device-groups/" + url.PathEscape(resolvedID)
			var result any
			if err := cliCtx.PlatformSDKClient.Transport().DoExpect(cmd.Context(), http.MethodGet, path, nil, http.StatusOK, &result); err != nil {
				return fmt.Errorf("get: %w", err)
			}
			if result == nil {
				return nil
			}
			b, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return err
			}
			return cliCtx.Output.PrintRaw(b)
		},
	}
	cmd.Flags().StringVar(&nameFlag, "name", "", "Resolve target by name instead of ID")
	cmd.Flags().StringVar(&deviceTypeFlag, "device-type", "", "Narrow --name lookup by device type: COMPUTER or MOBILE")
	return cmd
}

func newPDGDeleteCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var nameFlag, deviceTypeFlag string
	var yes bool
	cmd := &cobra.Command{
		Use:         "delete <id>",
		Short:       "Delete a device group",
		Long:        "Delete an existing device group",
		Annotations: map[string]string{"jamf:destructive": "true"},
		Args:        cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			dt, err := normalizeDeviceTypeFlag(deviceTypeFlag)
			if err != nil {
				return err
			}
			var resolvedID string
			if nameFlag != "" {
				id, err := pdgResolveID(cmd.Context(), cliCtx.PlatformSDKClient, nameFlag, dt)
				if err != nil {
					return err
				}
				resolvedID = id
			} else if len(args) == 1 {
				resolvedID = args[0]
			} else {
				return fmt.Errorf("provide a positional ID or --name")
			}
			if err := platform.ConfirmAction("delete", resolvedID, yes); err != nil {
				return err
			}
			path := "/api/device-groups/v1/tenant/" + url.PathEscape(cliCtx.PlatformSDKClient.Transport().TenantID()) + "/device-groups/" + url.PathEscape(resolvedID)
			if err := cliCtx.PlatformSDKClient.Transport().DoExpect(cmd.Context(), http.MethodDelete, path, nil, http.StatusNoContent, nil); err != nil {
				return fmt.Errorf("delete: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&nameFlag, "name", "", "Resolve target by name instead of ID")
	cmd.Flags().StringVar(&deviceTypeFlag, "device-type", "", "Narrow --name lookup by device type: COMPUTER or MOBILE")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	return cmd
}

func newPDGPatchCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var nameFlag, deviceTypeFlag, bodyFile string
	var setFlags []string
	var scaffoldFlag bool
	cmd := &cobra.Command{
		Use:   "patch <id>",
		Short: "Update a device group",
		Long:  "Update an existing device group",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if scaffoldFlag {
				fmt.Println("{\n  \"criteria\": [],\n  \"description\": \"\",\n  \"name\": \"\"\n}")
				return nil
			}
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			dt, err := normalizeDeviceTypeFlag(deviceTypeFlag)
			if err != nil {
				return err
			}
			var resolvedID string
			if nameFlag != "" {
				id, err := pdgResolveID(cmd.Context(), cliCtx.PlatformSDKClient, nameFlag, dt)
				if err != nil {
					return err
				}
				resolvedID = id
			} else if len(args) == 1 {
				resolvedID = args[0]
			} else {
				return fmt.Errorf("provide a positional ID or --name")
			}
			path := "/api/device-groups/v1/tenant/" + url.PathEscape(cliCtx.PlatformSDKClient.Transport().TenantID()) + "/device-groups/" + url.PathEscape(resolvedID)
			body, err := platform.ReadBody(bodyFile, setFlags)
			if err != nil {
				return err
			}
			if err := cliCtx.PlatformSDKClient.Transport().DoExpect(cmd.Context(), http.MethodPatch, path, body, http.StatusNoContent, nil); err != nil {
				return fmt.Errorf("patch: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&nameFlag, "name", "", "Resolve target by name instead of ID")
	cmd.Flags().StringVar(&deviceTypeFlag, "device-type", "", "Narrow --name lookup by device type: COMPUTER or MOBILE")
	cmd.Flags().StringVar(&bodyFile, "file", "", "Path to JSON file containing the request body")
	cmd.Flags().StringArrayVar(&setFlags, "set", nil, "Override body values (key=value, repeatable, supports nested.keys)")
	cmd.Flags().BoolVar(&scaffoldFlag, "scaffold", false, "Print an example request body and exit")
	return cmd
}

func newPDGMembersCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var nameFlag, deviceTypeFlag string
	cmd := &cobra.Command{
		Use:   "members <id>",
		Short: "Get group members",
		Long:  "Retrieve all members of a device group",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			dt, err := normalizeDeviceTypeFlag(deviceTypeFlag)
			if err != nil {
				return err
			}
			var resolvedID string
			if nameFlag != "" {
				id, err := pdgResolveID(cmd.Context(), cliCtx.PlatformSDKClient, nameFlag, dt)
				if err != nil {
					return err
				}
				resolvedID = id
			} else if len(args) == 1 {
				resolvedID = args[0]
			} else {
				return fmt.Errorf("provide a positional ID or --name")
			}
			path := "/api/device-groups/v1/tenant/" + url.PathEscape(cliCtx.PlatformSDKClient.Transport().TenantID()) + "/device-groups/" + url.PathEscape(resolvedID) + "/members"
			var result any
			if err := cliCtx.PlatformSDKClient.Transport().DoExpect(cmd.Context(), http.MethodGet, path, nil, http.StatusOK, &result); err != nil {
				return fmt.Errorf("members: %w", err)
			}
			if result == nil {
				return nil
			}
			b, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return err
			}
			return cliCtx.Output.PrintRaw(b)
		},
	}
	cmd.Flags().StringVar(&nameFlag, "name", "", "Resolve target by name instead of ID")
	cmd.Flags().StringVar(&deviceTypeFlag, "device-type", "", "Narrow --name lookup by device type: COMPUTER or MOBILE")
	return cmd
}

func newPDGPatchMembersCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var nameFlag, deviceTypeFlag, bodyFile string
	var setFlags []string
	var scaffoldFlag bool
	cmd := &cobra.Command{
		Use:   "patch-members <id>",
		Short: "Update device group members",
		Long:  "Add devices to or remove devices from a static device group. Cannot be used with smart groups.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if scaffoldFlag {
				fmt.Println("{\n  \"added\": [],\n  \"removed\": []\n}")
				return nil
			}
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			dt, err := normalizeDeviceTypeFlag(deviceTypeFlag)
			if err != nil {
				return err
			}
			var resolvedID string
			if nameFlag != "" {
				id, err := pdgResolveID(cmd.Context(), cliCtx.PlatformSDKClient, nameFlag, dt)
				if err != nil {
					return err
				}
				resolvedID = id
			} else if len(args) == 1 {
				resolvedID = args[0]
			} else {
				return fmt.Errorf("provide a positional ID or --name")
			}
			path := "/api/device-groups/v1/tenant/" + url.PathEscape(cliCtx.PlatformSDKClient.Transport().TenantID()) + "/device-groups/" + url.PathEscape(resolvedID) + "/members"
			body, err := platform.ReadBody(bodyFile, setFlags)
			if err != nil {
				return err
			}
			if err := cliCtx.PlatformSDKClient.Transport().DoExpect(cmd.Context(), http.MethodPatch, path, body, http.StatusNoContent, nil); err != nil {
				return fmt.Errorf("patch-members: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&nameFlag, "name", "", "Resolve target by name instead of ID")
	cmd.Flags().StringVar(&deviceTypeFlag, "device-type", "", "Narrow --name lookup by device type: COMPUTER or MOBILE")
	cmd.Flags().StringVar(&bodyFile, "file", "", "Path to JSON file containing the request body")
	cmd.Flags().StringArrayVar(&setFlags, "set", nil, "Override body values (key=value, repeatable, supports nested.keys)")
	cmd.Flags().BoolVar(&scaffoldFlag, "scaffold", false, "Print an example request body and exit")
	return cmd
}

func newPDGApplyCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		fromFile string
		yes      bool
		scaffold bool
	)
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Create or update a device group",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if scaffold {
				return printScaffold(deviceGroupScaffold())
			}
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			ctx := cmd.Context()

			data, err := readInput(fromFile)
			if err != nil {
				return err
			}

			var createReq devicegroups.DeviceGroupCreateRepresentationV1
			if err := unmarshalInput(data, &createReq); err != nil {
				return fmt.Errorf("parsing input: %w", err)
			}
			if createReq.Name == "" {
				return fmt.Errorf("input must include a 'name' field")
			}

			// Use deviceType from the input JSON to disambiguate when a COMPUTER
			// and MOBILE group share the same name.
			id, resolveErr := pdgResolveID(ctx, cliCtx.PlatformSDKClient, createReq.Name, string(createReq.DeviceType))
			if resolveErr != nil && !platform.IsNotFound(resolveErr) {
				return resolveErr
			}
			if resolveErr != nil {
				// Not found — create
				result, err := devicegroups.New(cliCtx.PlatformSDKClient).CreateDeviceGroup(ctx, &createReq)
				if err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "Created device group %q (id: %s)\n", createReq.Name, result.ID)
				group, err := devicegroups.New(cliCtx.PlatformSDKClient).GetDeviceGroup(ctx, result.ID)
				if err != nil {
					return err
				}
				return platform.PrintOne(cliCtx.Output, group)
			}

			// Found — confirm before updating
			proceed, err := confirmReplace("device group", createReq.Name, yes)
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			updateReq := &devicegroups.DeviceGroupUpdateRepresentationV1{
				Name:        &createReq.Name,
				Description: createReq.Description,
				Criteria:    createReq.Criteria,
			}
			if err := devicegroups.New(cliCtx.PlatformSDKClient).UpdateDeviceGroup(ctx, id, updateReq); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Updated device group %q\n", createReq.Name)
			group, err := devicegroups.New(cliCtx.PlatformSDKClient).GetDeviceGroup(ctx, id)
			if err != nil {
				return err
			}
			return platform.PrintOne(cliCtx.Output, group)
		},
	}
	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON input file (or pipe JSON to stdin)")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt when replacing")
	cmd.Flags().BoolVar(&scaffold, "scaffold", false, "Print a JSON template for the input format")
	return cmd
}

func deviceGroupScaffold() *devicegroups.DeviceGroupCreateRepresentationV1 {
	desc := ""
	return &devicegroups.DeviceGroupCreateRepresentationV1{
		Name:        "My Device Group",
		Description: &desc,
		DeviceType:  "COMPUTER",
		GroupType:   "STATIC",
		Members:     &[]string{"<device-id>"},
	}
}

func newPDGAddMembersCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var ids []string
	var deviceTypeFlag string
	cmd := &cobra.Command{
		Use:   "add-members <name>",
		Short: "Add devices to a static group",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			dt, err := normalizeDeviceTypeFlag(deviceTypeFlag)
			if err != nil {
				return err
			}
			if len(ids) == 0 {
				return fmt.Errorf("at least one --id is required")
			}
			ctx := cmd.Context()
			groupID, err := pdgResolveID(ctx, cliCtx.PlatformSDKClient, args[0], dt)
			if err != nil {
				return err
			}
			patch := &devicegroups.DeviceGroupMemberPatchRepresentationV1{
				Added: &ids,
			}
			if err := devicegroups.New(cliCtx.PlatformSDKClient).UpdateDeviceGroupMembers(ctx, groupID, patch); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Added %d device(s) to group %q\n", len(ids), args[0])
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&ids, "id", nil, "Device ID to add (repeatable)")
	cmd.Flags().StringVar(&deviceTypeFlag, "device-type", "", "Narrow name lookup by device type: COMPUTER or MOBILE")
	return cmd
}

func newPDGRemoveMembersCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var ids []string
	var deviceTypeFlag string
	cmd := &cobra.Command{
		Use:   "remove-members <name>",
		Short: "Remove devices from a static group",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			dt, err := normalizeDeviceTypeFlag(deviceTypeFlag)
			if err != nil {
				return err
			}
			if len(ids) == 0 {
				return fmt.Errorf("at least one --id is required")
			}
			ctx := cmd.Context()
			groupID, err := pdgResolveID(ctx, cliCtx.PlatformSDKClient, args[0], dt)
			if err != nil {
				return err
			}
			patch := &devicegroups.DeviceGroupMemberPatchRepresentationV1{
				Removed: &ids,
			}
			if err := devicegroups.New(cliCtx.PlatformSDKClient).UpdateDeviceGroupMembers(ctx, groupID, patch); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Removed %d device(s) from group %q\n", len(ids), args[0])
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&ids, "id", nil, "Device ID to remove (repeatable)")
	cmd.Flags().StringVar(&deviceTypeFlag, "device-type", "", "Narrow name lookup by device type: COMPUTER or MOBILE")
	return cmd
}

// guards against unused-import errors
var (
	_ = strings.Replace
	_ = strconv.Itoa
)
