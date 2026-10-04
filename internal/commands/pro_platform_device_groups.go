// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	platformgen "github.com/Jamf-Concepts/jamf-cli/internal/commands/platform/generated"
	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/platform"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/jamf/jamfplatform-go-sdk/jamfplatform"
	"github.com/jamf/jamfplatform-go-sdk/jamfplatform/devicegroups"
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
	for _, sub := range platformgen.NewPlatformDeviceGroupsCmd(cliCtx).Commands() {
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

// pdgListPath returns the list endpoint device-group name lookups filter over.
//
// No scope in the path: it travels as an X-Environment-Id or X-Tenant-Id header
// set by the transport, which is what the generated commands in this namespace
// send. This was missed when the scope moved out of the URL — the old form is
// still routed during the transition window, so it kept working while reading
// the tenant back through Transport().TenantID(), an accessor that answers ""
// for environment scope and "" again for organization scope. Under either it
// built /device-groups/v1/tenant//device-groups, so every --name lookup here
// would have failed for exactly the scope Jamf wants integrations to use.
func pdgListPath(_ *jamfplatform.Client) string {
	return "/device-groups/v1/device-groups"
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

// resolvePDGTarget normalizes --device-type, then resolves the group ID from
// either --name or a positional ID argument.
func resolvePDGTarget(ctx context.Context, cliCtx *registry.CLIContext, args []string, nameFlag, deviceTypeFlag string) (string, error) {
	dt, err := normalizeDeviceTypeFlag(deviceTypeFlag)
	if err != nil {
		return "", err
	}
	if nameFlag != "" {
		return pdgResolveID(ctx, cliCtx.PlatformSDKClient, nameFlag, dt)
	}
	if len(args) == 1 {
		return args[0], nil
	}
	return "", fmt.Errorf("provide a positional ID or --name")
}

// pdgItemPath returns the item-level endpoint for a device group ID.
func pdgItemPath(c *jamfplatform.Client, id string) string {
	return pdgListPath(c) + "/" + url.PathEscape(id)
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
			id, err := resolvePDGTarget(cmd.Context(), cliCtx, args, nameFlag, deviceTypeFlag)
			if err != nil {
				return err
			}
			var result any
			if err := cliCtx.PlatformSDKClient.Transport().DoExpect(cmd.Context(), http.MethodGet, pdgItemPath(cliCtx.PlatformSDKClient, id), nil, http.StatusOK, &result); err != nil {
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
			id, err := resolvePDGTarget(cmd.Context(), cliCtx, args, nameFlag, deviceTypeFlag)
			if err != nil {
				return err
			}
			if err := platform.ConfirmAction("delete", id, yes); err != nil {
				return err
			}
			if err := cliCtx.PlatformSDKClient.Transport().DoExpect(cmd.Context(), http.MethodDelete, pdgItemPath(cliCtx.PlatformSDKClient, id), nil, http.StatusNoContent, nil); err != nil {
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

// pdgPatchContentType is what both device-group PATCHes are sent as. The spec
// declares application/json for each, and the server refuses anything else:
// `Transport().DoExpect` sends a PATCH as application/merge-patch+json when no
// content type is given, and wire-checked 2026-10-04 both endpoints answered
// that with 400 "The request is malformed or the content type is not
// supported" for every body. The SDK's own UpdateDeviceGroup and
// UpdateDeviceGroupMembers name application/json, which is why apply and
// add-members worked while patch and patch-members never did. The transport
// default is tracked upstream as jamf/jamfplatform-go-sdk#85; naming the
// content type here is correct whatever it becomes.
const pdgPatchContentType = "application/json"

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
				return printScaffold(map[string]any{
					"criteria":    []any{},
					"description": "",
					"name":        "",
				})
			}
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			id, err := resolvePDGTarget(cmd.Context(), cliCtx, args, nameFlag, deviceTypeFlag)
			if err != nil {
				return err
			}
			body, err := platform.ReadBody(bodyFile, setFlags)
			if err != nil {
				return err
			}
			path := pdgItemPath(cliCtx.PlatformSDKClient, id)
			if cliCtx.DryRun {
				return platform.ReportDryRun(cmd.ErrOrStderr(), http.MethodPatch, path, body)
			}
			if err := cliCtx.PlatformSDKClient.Transport().DoWithContentType(cmd.Context(), http.MethodPatch, path, body, pdgPatchContentType, http.StatusNoContent, nil); err != nil {
				return fmt.Errorf("patch: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&nameFlag, "name", "", "Resolve target by name instead of ID")
	cmd.Flags().StringVar(&deviceTypeFlag, "device-type", "", "Narrow --name lookup by device type: COMPUTER or MOBILE")
	cmd.Flags().StringVar(&bodyFile, "from-file", "", "Path to JSON input file (or pipe JSON to stdin)")
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
			id, err := resolvePDGTarget(cmd.Context(), cliCtx, args, nameFlag, deviceTypeFlag)
			if err != nil {
				return err
			}
			path := pdgItemPath(cliCtx.PlatformSDKClient, id) + "/members"
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
				return printScaffold(map[string]any{
					"added":   []any{},
					"removed": []any{},
				})
			}
			if err := requirePlatformClient(cliCtx); err != nil {
				return err
			}
			id, err := resolvePDGTarget(cmd.Context(), cliCtx, args, nameFlag, deviceTypeFlag)
			if err != nil {
				return err
			}
			path := pdgItemPath(cliCtx.PlatformSDKClient, id) + "/members"
			body, err := platform.ReadBody(bodyFile, setFlags)
			if err != nil {
				return err
			}
			if cliCtx.DryRun {
				return platform.ReportDryRun(cmd.ErrOrStderr(), http.MethodPatch, path, body)
			}
			if err := cliCtx.PlatformSDKClient.Transport().DoWithContentType(cmd.Context(), http.MethodPatch, path, body, pdgPatchContentType, http.StatusNoContent, nil); err != nil {
				return fmt.Errorf("patch-members: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&nameFlag, "name", "", "Resolve target by name instead of ID")
	cmd.Flags().StringVar(&deviceTypeFlag, "device-type", "", "Narrow --name lookup by device type: COMPUTER or MOBILE")
	cmd.Flags().StringVar(&bodyFile, "from-file", "", "Path to JSON input file (or pipe JSON to stdin)")
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
	return newPDGMemberChangeCmd(cliCtx, true)
}

func newPDGRemoveMembersCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return newPDGMemberChangeCmd(cliCtx, false)
}

// newPDGMemberChangeCmd builds add-members / remove-members.
//
// The server answers 204 whether or not anything changed — re-adding a member
// and removing a device that is not one are both silent no-ops — so the
// membership is read first and the report counts what actually changes rather
// than echoing the number of IDs given. An ID the server does not know, or a
// device of the other type, fails the whole request with INVALID_DEVICE.
func newPDGMemberChangeCmd(cliCtx *registry.CLIContext, add bool) *cobra.Command {
	var ids []string
	var deviceTypeFlag string
	verb, short, flagHelp := "add-members", "Add devices to a static group", "Device ID to add (repeatable)"
	if !add {
		verb, short, flagHelp = "remove-members", "Remove devices from a static group", "Device ID to remove (repeatable)"
	}
	cmd := &cobra.Command{
		Use:   verb + " <name>",
		Short: short,
		Long: short + `.

Device IDs are Jamf Platform device IDs, which are each device's Jamf Pro
management ID. The group's current members are read first, so the summary
reports what changed: a device already in the requested state is counted as
unchanged.`,
		Args: cobra.ExactArgs(1),
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
			for _, id := range ids {
				if !platformDeviceIDShape.MatchString(strings.TrimSpace(id)) {
					return &exitcode.Error{
						Code:    exitcode.Usage,
						Message: fmt.Sprintf("--id %q is not a Platform device ID", id),
						Hint:    "a Platform device ID is a UUID: the device's Jamf Pro management ID, not its numeric inventory ID",
					}
				}
			}
			ctx := cmd.Context()
			stderr := cmd.ErrOrStderr()
			groupID, err := pdgResolveID(ctx, cliCtx.PlatformSDKClient, args[0], dt)
			if err != nil {
				return err
			}
			dg := devicegroups.New(cliCtx.PlatformSDKClient)
			current, err := dg.ListDeviceGroupMembers(ctx, groupID)
			if err != nil {
				return fmt.Errorf("reading the members of %q: %w", args[0], err)
			}
			change, unchanged := pdgPartitionMembers(current, ids, add)

			// Every requested ID is sent, not only the ones that change the
			// group: re-adding a member and removing a non-member are both
			// no-ops on the wire, but an ID that names no device answers
			// INVALID_DEVICE only if it is sent. Filtering it out would read a
			// typo as "not a member, left alone". The partition is for the
			// report.
			send := slices.Concat(change, unchanged)
			patch := &devicegroups.DeviceGroupMemberPatchRepresentationV1{}
			if add {
				patch.Added = &send
			} else {
				patch.Removed = &send
			}
			if cliCtx.DryRun {
				_, _ = fmt.Fprintf(stderr, "[dry-run] %d to change, %d unchanged\n", len(change), len(unchanged))
				return platform.ReportDryRun(stderr, http.MethodPatch, pdgItemPath(cliCtx.PlatformSDKClient, groupID)+"/members", patch)
			}
			if err := dg.UpdateDeviceGroupMembers(ctx, groupID, patch); err != nil {
				return err
			}
			done, state := "Added", "already members"
			if !add {
				done, state = "Removed", "not members"
			}
			_, _ = fmt.Fprintf(stderr, "%s %d device(s) %s group %q", done, len(change), directionWord(add), args[0])
			if len(unchanged) > 0 {
				_, _ = fmt.Fprintf(stderr, "; %d %s, left alone", len(unchanged), state)
			}
			_, _ = fmt.Fprintln(stderr)
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&ids, "id", nil, flagHelp)
	cmd.Flags().StringVar(&deviceTypeFlag, "device-type", "", "Narrow name lookup by device type: COMPUTER or MOBILE")
	return cmd
}

// platformDeviceIDShape is a Platform device ID: the Jamf Pro management ID.
var platformDeviceIDShape = regexp.MustCompile(`^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$`)

// pdgPartitionMembers splits requested IDs into those that would change the
// group and those already in the requested state, de-duplicated and compared
// case-insensitively (device IDs are UUIDs).
func pdgPartitionMembers(current, requested []string, add bool) (change, unchanged []string) {
	member := make(map[string]bool, len(current))
	for _, id := range current {
		member[strings.ToLower(id)] = true
	}
	seen := map[string]bool{}
	change, unchanged = []string{}, []string{}
	for _, id := range requested {
		key := strings.ToLower(strings.TrimSpace(id))
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		if member[key] == add {
			unchanged = append(unchanged, id)
		} else {
			change = append(change, id)
		}
	}
	return change, unchanged
}
