// Copyright 2026, Jamf Software LLC

package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/Jamf-Concepts/jamf-cli/internal/output"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamf-cli/internal/resolve"
)

// deviceTarget holds the mutually exclusive targeting flags shared by all
// device action commands.
type deviceTarget struct {
	serial   string
	name     string
	id       string
	group    string
	fromFile string
}

// addFlags registers --serial, --name, --id, --group, --from-file on the
// command and marks them mutually exclusive.
func (dt *deviceTarget) addFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&dt.serial, "serial", "", "device serial number")
	cmd.Flags().StringVar(&dt.name, "name", "", "device name")
	cmd.Flags().StringVar(&dt.id, "id", "", "device numeric ID")
	cmd.Flags().StringVar(&dt.group, "group", "", "target all members of a device group")
	cmd.Flags().StringVar(&dt.fromFile, "from-file", "", "file containing one serial or ID per line")
	cmd.MarkFlagsMutuallyExclusive("serial", "name", "id", "group", "from-file")
}

// addFlushFlags registers --serial, --name, --id, --group (but not --from-file)
// with device-type-specific help text and marks them mutually exclusive.
// Used by flush-style commands that operate on a single device or group but
// don't support bulk file input.
func (dt *deviceTarget) addFlushFlags(cmd *cobra.Command, deviceType string) {
	cmd.Flags().StringVar(&dt.serial, "serial", "", deviceType+" serial number")
	cmd.Flags().StringVar(&dt.name, "name", "", deviceType+" name")
	cmd.Flags().StringVar(&dt.id, "id", "", deviceType+" numeric ID")
	cmd.Flags().StringVar(&dt.group, "group", "", "target all members of a "+deviceType+" group (smart or static)")
	cmd.MarkFlagsMutuallyExclusive("serial", "name", "id", "group")
}

// validate ensures exactly one targeting flag is set.
func (dt *deviceTarget) validate() error {
	set := 0
	for _, v := range []string{dt.serial, dt.name, dt.id, dt.group, dt.fromFile} {
		if v != "" {
			set++
		}
	}
	if set == 0 {
		return fmt.Errorf("one of --serial, --name, --id, --group, or --from-file is required")
	}
	return nil
}

// isBulk returns true when the target resolves to multiple devices.
func (dt *deviceTarget) isBulk() bool {
	return dt.group != "" || dt.fromFile != ""
}

// resolveComputersCmd resolves the target to one or more computers using a cobra command's context.
func (dt *deviceTarget) resolveComputersCmd(cmd *cobra.Command, client registry.HTTPClient) ([]*resolve.DeviceIdentifiers, error) {
	ctx := cmd.Context()
	switch {
	case dt.group != "":
		return resolve.ResolveComputerGroup(ctx, client, dt.group)
	case dt.fromFile != "":
		return resolve.ResolveComputersFromFile(ctx, client, dt.fromFile)
	default:
		d, err := resolve.ResolveComputer(ctx, client, dt.serial, dt.name, dt.id)
		if err != nil {
			return nil, err
		}
		return []*resolve.DeviceIdentifiers{d}, nil
	}
}

// resolveMobileDevicesCmd resolves the target to one or more mobile devices.
func (dt *deviceTarget) resolveMobileDevicesCmd(cmd *cobra.Command, client registry.HTTPClient) ([]*resolve.DeviceIdentifiers, error) {
	ctx := cmd.Context()
	switch {
	case dt.group != "":
		return resolve.ResolveMobileDeviceGroup(ctx, client, dt.group)
	case dt.fromFile != "":
		return resolve.ResolveMobileDevicesFromFile(ctx, client, dt.fromFile)
	default:
		d, err := resolve.ResolveMobileDevice(ctx, client, dt.serial, dt.name, dt.id)
		if err != nil {
			return nil, err
		}
		return []*resolve.DeviceIdentifiers{d}, nil
	}
}

// --- Computer action commands ---

func newComputerEraseCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		dt                 deviceTarget
		yes                bool
		confirmDestructive bool
		scaffold           bool
		bodyFile           string
	)

	cmd := &cobra.Command{
		Use:         "erase",
		Short:       "Erase a computer",
		Annotations: map[string]string{"jamf:destructive": "true"},
		Long: `Erase a computer by serial number, name, or ID, or target a group.

This is a destructive operation that wipes the device. An optional request
body can provide a Find My PIN (6 digits) via stdin or --body-file.`,
		Example: `  # Erase by serial number
  jamf-cli pro comp erase --serial C02X1234 --yes

  # Erase with Find My PIN
  echo '{"pin":"123456"}' | jamf-cli pro comp erase --serial C02X1234 --yes

  # Erase all devices in a group
  jamf-cli pro comp erase --group "Decommissioned" --yes

  # Dry-run preview
  jamf-cli pro comp erase --serial C02X1234 --dry-run

  # Print JSON template for request body
  jamf-cli pro comp erase --scaffold`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if scaffold {
				fmt.Println(`{
  "pin": "123456"
}`)
				return nil
			}
			return runDeviceAction(cmd, cliCtx, &dt, yes, confirmDestructive, deviceActionConfig{
				actionName:  "erase",
				deviceType:  "computer",
				destructive: true,
				bodyFile:    bodyFile,
				execSingle: func(d *resolve.DeviceIdentifiers, body io.Reader) error {
					path := fmt.Sprintf("/v1/computer-inventory/%s/erase", url.PathEscape(d.ID))
					return doPostAction(cmd, cliCtx, path, body)
				},
			})
		},
	}

	dt.addFlags(cmd)
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation prompt")
	cmd.Flags().BoolVar(&confirmDestructive, "confirm-destructive", false, "required for bulk destructive operations")
	cmd.Flags().BoolVar(&scaffold, "scaffold", false, "print a JSON template for the request body and exit")
	cmd.Flags().StringVar(&bodyFile, "body-file", "", "read request body from file (e.g., erase PIN)")

	return cmd
}

func newComputerRemoveMDMCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		dt                 deviceTarget
		yes                bool
		confirmDestructive bool
	)

	cmd := &cobra.Command{
		Use:         "remove-mdm",
		Short:       "Remove the MDM profile from a computer",
		Annotations: map[string]string{"jamf:destructive": "true"},
		Long:        "Remove the MDM profile (unmanage) from a computer by serial number, name, or ID.",
		Example: `  jamf-cli pro comp remove-mdm --serial C02X1234 --yes
  jamf-cli pro comp remove-mdm --group "Offboarding" --yes --confirm-destructive`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDeviceAction(cmd, cliCtx, &dt, yes, confirmDestructive, deviceActionConfig{
				actionName:  "remove-mdm",
				deviceType:  "computer",
				destructive: true,
				execSingle: func(d *resolve.DeviceIdentifiers, _ io.Reader) error {
					path := fmt.Sprintf("/v1/computer-inventory/%s/remove-mdm-profile", url.PathEscape(d.ID))
					return doPostAction(cmd, cliCtx, path, nil)
				},
			})
		},
	}

	dt.addFlags(cmd)
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation prompt")
	cmd.Flags().BoolVar(&confirmDestructive, "confirm-destructive", false, "required for bulk destructive operations")
	return cmd
}

func newComputerRedeployFrameworkCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		dt  deviceTarget
		yes bool
	)

	cmd := &cobra.Command{
		Use:   "redeploy-framework",
		Short: "Redeploy the Jamf management framework",
		Long:  "Redeploy the Jamf management framework on a computer by serial number, name, or ID.",
		Example: `  jamf-cli pro comp redeploy-framework --serial C02X1234
  jamf-cli pro comp redeploy-framework --group "Lab Macs" --yes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDeviceAction(cmd, cliCtx, &dt, yes, false, deviceActionConfig{
				actionName: "redeploy-framework",
				deviceType: "computer",
				execSingle: func(d *resolve.DeviceIdentifiers, _ io.Reader) error {
					path := fmt.Sprintf("/v1/jamf-management-framework/redeploy/%s", url.PathEscape(d.ID))
					return doPostAction(cmd, cliCtx, path, nil)
				},
			})
		},
	}

	dt.addFlags(cmd)
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation for bulk operations")
	return cmd
}

func newComputerBlankPushCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		dt  deviceTarget
		yes bool
	)

	cmd := &cobra.Command{
		Use:   "blank-push",
		Short: "Send a blank push notification to a computer",
		Long:  "Send a blank push notification to a computer or group. This triggers the device to check in.",
		Example: `  jamf-cli pro comp blank-push --serial C02X1234
  jamf-cli pro comp blank-push --group "All Macs" --yes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDeviceAction(cmd, cliCtx, &dt, yes, false, deviceActionConfig{
				actionName:          "blank-push",
				deviceType:          "computer",
				batchByManagementID: true,
				execBatch: func(devices []*resolve.DeviceIdentifiers) error {
					ids := make([]string, 0, len(devices))
					for _, d := range devices {
						if d.ManagementID == "" {
							return fmt.Errorf("computer %s has no managementId — cannot send blank push", resolve.FormatDeviceDesc(d))
						}
						ids = append(ids, d.ManagementID)
					}
					body := map[string]any{"clientManagementIds": ids}
					data, _ := json.Marshal(body)
					return doPostAction(cmd, cliCtx, "/v2/mdm/blank-push", strings.NewReader(string(data)))
				},
			})
		},
	}

	dt.addFlags(cmd)
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation for bulk operations")
	return cmd
}

func newComputerDDMSyncCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		dt  deviceTarget
		yes bool
	)

	cmd := &cobra.Command{
		Use:   "ddm-sync",
		Short: "Force a Declarative Device Management sync",
		Long:  "Force a DDM sync on a computer by serial number, name, or ID.",
		Example: `  jamf-cli pro comp ddm-sync --serial C02X1234
  jamf-cli pro comp ddm-sync --from-file devices.txt --yes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDeviceAction(cmd, cliCtx, &dt, yes, false, deviceActionConfig{
				actionName: "ddm-sync",
				deviceType: "computer",
				execSingle: func(d *resolve.DeviceIdentifiers, _ io.Reader) error {
					if d.ManagementID == "" {
						return fmt.Errorf("computer %s has no managementId — cannot send DDM sync", resolve.FormatDeviceDesc(d))
					}
					path := fmt.Sprintf("/v1/ddm/%s/sync", url.PathEscape(d.ManagementID))
					return doPostAction(cmd, cliCtx, path, nil)
				},
			})
		},
	}

	dt.addFlags(cmd)
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation for bulk operations")
	return cmd
}

func newComputerRenewMDMCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		dt  deviceTarget
		yes bool
	)

	cmd := &cobra.Command{
		Use:   "renew-mdm",
		Short: "Renew the MDM profile on a computer",
		Long:  "Renew the MDM profile on a computer or group.",
		Example: `  jamf-cli pro comp renew-mdm --serial C02X1234
  jamf-cli pro comp renew-mdm --group "All Macs" --yes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDeviceAction(cmd, cliCtx, &dt, yes, false, deviceActionConfig{
				actionName:  "renew-mdm",
				deviceType:  "computer",
				batchByUDID: true,
				execBatch: func(devices []*resolve.DeviceIdentifiers) error {
					udids := make([]string, 0, len(devices))
					for _, d := range devices {
						if d.UDID == "" {
							return fmt.Errorf("computer %s has no UDID — cannot renew MDM profile", resolve.FormatDeviceDesc(d))
						}
						udids = append(udids, d.UDID)
					}
					body := map[string]any{"udids": udids}
					data, _ := json.Marshal(body)
					return doPostAction(cmd, cliCtx, "/v1/mdm/renew-profile", strings.NewReader(string(data)))
				},
			})
		},
	}

	dt.addFlags(cmd)
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation for bulk operations")
	return cmd
}

// --- Mobile device action commands ---

func newMobileEraseCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		dt                 deviceTarget
		yes                bool
		confirmDestructive bool
		scaffold           bool
		bodyFile           string
	)

	cmd := &cobra.Command{
		Use:         "erase",
		Short:       "Erase a mobile device",
		Annotations: map[string]string{"jamf:destructive": "true"},
		Long: `Erase a mobile device by serial number, name, or ID, or target a group.

This is a destructive operation. An optional request body can configure
erase behavior (preserveDataPlan, clearActivationLock, etc.).`,
		Example: `  jamf-cli pro md erase --serial F4GH5678 --yes
  jamf-cli pro md erase --group "Old iPads" --yes
  jamf-cli pro md erase --scaffold`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if scaffold {
				fmt.Println(`{
  "preserveDataPlan": false,
  "disallowProximitySetup": false,
  "clearActivationLock": false,
  "returnToService": false
}`)
				return nil
			}
			return runMobileAction(cmd, cliCtx, &dt, yes, confirmDestructive, deviceActionConfig{
				actionName:  "erase",
				deviceType:  "mobile device",
				destructive: true,
				bodyFile:    bodyFile,
				execSingle: func(d *resolve.DeviceIdentifiers, body io.Reader) error {
					path := fmt.Sprintf("/v2/mobile-devices/%s/erase", url.PathEscape(d.ID))
					return doPostAction(cmd, cliCtx, path, body)
				},
			})
		},
	}

	dt.addFlags(cmd)
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation prompt")
	cmd.Flags().BoolVar(&confirmDestructive, "confirm-destructive", false, "required for bulk destructive operations")
	cmd.Flags().BoolVar(&scaffold, "scaffold", false, "print a JSON template for the request body and exit")
	cmd.Flags().StringVar(&bodyFile, "body-file", "", "read request body from file")

	return cmd
}

func newMobileUnmanageCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		dt                 deviceTarget
		yes                bool
		confirmDestructive bool
	)

	cmd := &cobra.Command{
		Use:   "unmanage",
		Short: "Remove management from a mobile device",
		Long:  "Remove management (unmanage) from a mobile device by serial number, name, or ID.",
		Example: `  jamf-cli pro md unmanage --serial F4GH5678 --yes
  jamf-cli pro md unmanage --group "Retired iPads" --yes --confirm-destructive`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMobileAction(cmd, cliCtx, &dt, yes, confirmDestructive, deviceActionConfig{
				actionName:  "unmanage",
				deviceType:  "mobile device",
				destructive: true,
				execSingle: func(d *resolve.DeviceIdentifiers, _ io.Reader) error {
					path := fmt.Sprintf("/v2/mobile-devices/%s/unmanage", url.PathEscape(d.ID))
					return doPostAction(cmd, cliCtx, path, nil)
				},
			})
		},
	}

	dt.addFlags(cmd)
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation prompt")
	cmd.Flags().BoolVar(&confirmDestructive, "confirm-destructive", false, "required for bulk destructive operations")
	return cmd
}

// --- Shared action execution engine ---

type deviceActionConfig struct {
	actionName          string
	deviceType          string
	destructive         bool
	bodyFile            string
	batchByManagementID bool // blank-push: send all managementIDs in one request
	batchByUDID         bool // renew-mdm: send all UDIDs in one request
	execSingle          func(d *resolve.DeviceIdentifiers, body io.Reader) error
	execBatch           func(devices []*resolve.DeviceIdentifiers) error
}

func runDeviceAction(cmd *cobra.Command, cliCtx *registry.CLIContext, dt *deviceTarget, yes, confirmDestructive bool, cfg deviceActionConfig) error {
	if err := dt.validate(); err != nil {
		return err
	}
	devices, err := dt.resolveComputersCmd(cmd, cliCtx.Client)
	if err != nil {
		return err
	}
	return executeAction(cmd, dt, devices, yes, confirmDestructive, cfg)
}

func runMobileAction(cmd *cobra.Command, cliCtx *registry.CLIContext, dt *deviceTarget, yes, confirmDestructive bool, cfg deviceActionConfig) error {
	if err := dt.validate(); err != nil {
		return err
	}
	devices, err := dt.resolveMobileDevicesCmd(cmd, cliCtx.Client)
	if err != nil {
		return err
	}
	return executeAction(cmd, dt, devices, yes, confirmDestructive, cfg)
}

func executeAction(cmd *cobra.Command, dt *deviceTarget, devices []*resolve.DeviceIdentifiers, yes, confirmDestructive bool, cfg deviceActionConfig) error {
	stderr := cmd.ErrOrStderr()

	// Reject --body-file with bulk targeting (body can't be reused per device
	// and the semantics are unclear).
	if cfg.bodyFile != "" && dt.isBulk() {
		return fmt.Errorf("--body-file cannot be used with --group or --from-file")
	}

	// Dry-run: print what would happen.
	if dryRun {
		for _, d := range devices {
			_, _ = fmt.Fprintf(stderr, "[dry-run] Would %s %s %s\n", cfg.actionName, cfg.deviceType, resolve.FormatDeviceDesc(d))
		}
		return nil
	}

	// Bulk targeting: show preview table.
	if dt.isBulk() {
		rows := make([]map[string]any, len(devices))
		for i, d := range devices {
			rows[i] = map[string]any{
				"id":     d.ID,
				"name":   d.Name,
				"serial": d.SerialNumber,
				"action": cfg.actionName,
			}
		}
		formatter := output.New("table", noColor, wide)
		_ = formatter.Print(rows)

		if cfg.destructive && !confirmDestructive {
			_, _ = fmt.Fprintf(stderr, "\n⚠️  This will %s %d %ss. Both --yes and --confirm-destructive are required.\n", cfg.actionName, len(devices), cfg.deviceType)
			return nil
		}
		if !yes {
			_, _ = fmt.Fprintf(stderr, "\nThis will %s %d %ss. Use --yes to execute.\n", cfg.actionName, len(devices), cfg.deviceType)
			return nil
		}
	}

	// Single-device destructive confirmation.
	if cfg.destructive && len(devices) == 1 && !dt.isBulk() {
		if !yes {
			isNoInput, _ := cmd.Flags().GetBool("no-input")
			if isNoInput {
				return fmt.Errorf("destructive operation requires --yes when --no-input is set")
			}
			_, _ = fmt.Fprintf(stderr, "⚠️  This will %s %s %s. Type 'yes' to confirm: ", cfg.actionName, cfg.deviceType, resolve.FormatDeviceDesc(devices[0]))
			var confirm string
			_, _ = fmt.Scanln(&confirm)
			if confirm != "yes" {
				return fmt.Errorf("aborted")
			}
		}
	}

	// Batch endpoints (blank-push, renew-mdm): send all IDs in one request.
	if (cfg.batchByManagementID || cfg.batchByUDID) && cfg.execBatch != nil && len(devices) > 0 {
		return cfg.execBatch(devices)
	}

	// Read optional body (for erase commands).
	body, err := readActionBody(cfg.bodyFile)
	if err != nil {
		return err
	}

	// Execute per-device.
	if len(devices) == 1 {
		return cfg.execSingle(devices[0], body)
	}

	// Bulk per-device execution with progress logging.
	_, _ = fmt.Fprintf(stderr, "Sending %s to %d %ss...\n", cfg.actionName, len(devices), cfg.deviceType)
	var successCount, failCount int
	for _, d := range devices {
		if err := cfg.execSingle(d, nil); err != nil {
			_, _ = fmt.Fprintf(stderr, "[%s] %-40s ERROR: %v\n", cfg.actionName, resolve.FormatDeviceDesc(d), err)
			failCount++
		} else {
			_, _ = fmt.Fprintf(stderr, "[%s] %-40s ok\n", cfg.actionName, resolve.FormatDeviceDesc(d))
			successCount++
		}
	}
	_, _ = fmt.Fprintf(stderr, "%s complete: %d succeeded, %d failed.\n", cfg.actionName, successCount, failCount)
	if failCount > 0 {
		return fmt.Errorf("%d of %d %s operations failed", failCount, successCount+failCount, cfg.actionName)
	}
	return nil
}

// doPostAction sends a POST request and prints the response.
func doPostAction(cmd *cobra.Command, cliCtx *registry.CLIContext, path string, body io.Reader) error {
	resp, err := cliCtx.Client.Do(cmd.Context(), "POST", path, body)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	return cliCtx.Output.PrintResponse(resp)
}

// readActionBody reads an optional request body from --body-file or stdin.
// Returns nil if no input is available (body is optional for most actions).
func readActionBody(bodyFile string) (io.Reader, error) {
	if bodyFile != "" {
		data, err := os.ReadFile(bodyFile)
		if err != nil {
			return nil, fmt.Errorf("reading body file: %w", err)
		}
		return strings.NewReader(string(data)), nil
	}
	// Check if stdin has piped data.
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		return os.Stdin, nil
	}
	return nil, nil
}

// --- Modern API computer MDM commands ---

// sendComputerModernMDMCommand sends a single MDM command to a computer via
// POST /v2/mdm/commands using the management ID (UUID).
func sendComputerModernMDMCommand(cmd *cobra.Command, cliCtx *registry.CLIContext, d *resolve.DeviceIdentifiers, commandData map[string]any) error {
	if d.ManagementID == "" {
		return fmt.Errorf("computer %s has no managementId — cannot send MDM command", resolve.FormatDeviceDesc(d))
	}
	body := map[string]any{
		"clientData": []map[string]any{
			{"managementId": d.ManagementID},
		},
		"commandData": commandData,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return doPostAction(cmd, cliCtx, "/v2/mdm/commands", strings.NewReader(string(data)))
}

// newModernComputerMDMCmd creates a computer subcommand that sends a
// modern API MDM command via POST /v2/mdm/commands with no additional body fields.
func newModernComputerMDMCmd(cliCtx *registry.CLIContext, name, commandType, short, long, example string, destructive bool) *cobra.Command {
	var (
		dt                 deviceTarget
		yes                bool
		confirmDestructive bool
	)
	cmd := &cobra.Command{
		Use:     name,
		Short:   short,
		Long:    long,
		Example: example,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDeviceAction(cmd, cliCtx, &dt, yes, confirmDestructive, deviceActionConfig{
				actionName:  name,
				deviceType:  "computer",
				destructive: destructive,
				execSingle: func(d *resolve.DeviceIdentifiers, _ io.Reader) error {
					return sendComputerModernMDMCommand(cmd, cliCtx, d, map[string]any{
						"commandType": commandType,
					})
				},
			})
		},
	}
	dt.addFlags(cmd)
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation prompt")
	if destructive {
		cmd.Flags().BoolVar(&confirmDestructive, "confirm-destructive", false, "required for bulk destructive operations")
	}
	return cmd
}

func newComputerLockCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return newModernComputerMDMCmd(
		cliCtx, "lock", "DEVICE_LOCK",
		"Lock a computer",
		"Lock a computer by serial number, name, or ID. This is a destructive operation.",
		`  jamf-cli pro comp lock --serial C02X1234 --yes
  jamf-cli pro comp lock --group "Lost Devices" --yes --confirm-destructive`,
		true,
	)
}

func newComputerEnableRemoteDesktopCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return newModernComputerMDMCmd(
		cliCtx, "enable-remote-desktop", "ENABLE_REMOTE_DESKTOP",
		"Enable Remote Desktop on a computer",
		"Enable the Remote Desktop agent on a computer by serial number, name, or ID.",
		`  jamf-cli pro comp enable-remote-desktop --serial C02X1234
  jamf-cli pro comp enable-remote-desktop --group "Lab Macs" --yes`,
		false,
	)
}

func newComputerDisableRemoteDesktopCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return newModernComputerMDMCmd(
		cliCtx, "disable-remote-desktop", "DISABLE_REMOTE_DESKTOP",
		"Disable Remote Desktop on a computer",
		"Disable the Remote Desktop agent on a computer by serial number, name, or ID.",
		`  jamf-cli pro comp disable-remote-desktop --serial C02X1234
  jamf-cli pro comp disable-remote-desktop --group "Lab Macs" --yes`,
		false,
	)
}

func newComputerRestartCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		dt                 deviceTarget
		yes                bool
		rebuildKernelCache bool
	)
	cmd := &cobra.Command{
		Use:   "restart",
		Short: "Restart a computer",
		Long:  "Restart a supervised computer by serial number, name, or ID.",
		Example: `  jamf-cli pro comp restart --serial C02X1234
  jamf-cli pro comp restart --serial C02X1234 --rebuild-kernel-cache
  jamf-cli pro comp restart --group "Lab Macs" --yes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDeviceAction(cmd, cliCtx, &dt, yes, false, deviceActionConfig{
				actionName: "restart",
				deviceType: "computer",
				execSingle: func(d *resolve.DeviceIdentifiers, _ io.Reader) error {
					commandData := map[string]any{"commandType": "RESTART_DEVICE"}
					if rebuildKernelCache {
						commandData["rebuildKernelCache"] = true
					}
					return sendComputerModernMDMCommand(cmd, cliCtx, d, commandData)
				},
			})
		},
	}
	dt.addFlags(cmd)
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation for bulk operations")
	cmd.Flags().BoolVar(&rebuildKernelCache, "rebuild-kernel-cache", false, "rebuild the kernel cache before restarting")
	return cmd
}

func newComputerShutdownCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return newModernComputerMDMCmd(
		cliCtx, "shutdown", "SHUT_DOWN_DEVICE",
		"Shut down a computer",
		"Shut down a supervised computer by serial number, name, or ID.",
		`  jamf-cli pro comp shutdown --serial C02X1234
  jamf-cli pro comp shutdown --group "Lab Macs" --yes`,
		false,
	)
}

func newComputerSetRecoveryLockCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		dt                 deviceTarget
		yes                bool
		confirmDestructive bool
		newPassword        string
	)
	cmd := &cobra.Command{
		Use:   "set-recovery-lock",
		Short: "Set or clear the Recovery Lock password on a computer",
		Long: `Set the Recovery Lock password on an Apple Silicon or Apple T2 computer.
Omit --new-password or pass an empty string to clear the existing password.

This is a destructive operation: setting an unknown password can permanently
lock a user out of their machine.`,
		Example: `  # Set a recovery lock password
  jamf-cli pro comp set-recovery-lock --serial C02X1234 --new-password "S3cur3P@ss" --yes

  # Clear the recovery lock password
  jamf-cli pro comp set-recovery-lock --serial C02X1234 --yes

  # Bulk (requires both --yes and --confirm-destructive)
  jamf-cli pro comp set-recovery-lock --group "Lab Macs" --new-password "S3cur3" --yes --confirm-destructive`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDeviceAction(cmd, cliCtx, &dt, yes, confirmDestructive, deviceActionConfig{
				actionName:  "set-recovery-lock",
				deviceType:  "computer",
				destructive: true,
				execSingle: func(d *resolve.DeviceIdentifiers, _ io.Reader) error {
					return sendComputerModernMDMCommand(cmd, cliCtx, d, map[string]any{
						"commandType": "SET_RECOVERY_LOCK",
						"newPassword": newPassword,
					})
				},
			})
		},
	}
	dt.addFlags(cmd)
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation prompt")
	cmd.Flags().BoolVar(&confirmDestructive, "confirm-destructive", false, "required for bulk destructive operations")
	cmd.Flags().StringVar(&newPassword, "new-password", "", "Recovery Lock password (omit to clear)")
	return cmd
}

// --- Modern API mobile device MDM commands ---

// sendMobileModernMDMCommand sends a single MDM command to a mobile device via
// POST /v2/mdm/commands using the management ID (UUID).
func sendMobileModernMDMCommand(cmd *cobra.Command, cliCtx *registry.CLIContext, d *resolve.DeviceIdentifiers, commandData map[string]any) error {
	if d.ManagementID == "" {
		return fmt.Errorf("mobile device %s has no managementId — cannot send MDM command", resolve.FormatDeviceDesc(d))
	}
	body := map[string]any{
		"clientData": []map[string]any{
			{"managementId": d.ManagementID},
		},
		"commandData": commandData,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return doPostAction(cmd, cliCtx, "/v2/mdm/commands", strings.NewReader(string(data)))
}

// newModernMobileMDMCmd creates a mobile-device subcommand that sends a
// modern API MDM command via POST /v2/mdm/commands with no additional body fields.
func newModernMobileMDMCmd(cliCtx *registry.CLIContext, name, commandType, short, long, example string, destructive bool) *cobra.Command {
	var (
		dt                 deviceTarget
		yes                bool
		confirmDestructive bool
	)
	cmd := &cobra.Command{
		Use:     name,
		Short:   short,
		Long:    long,
		Example: example,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMobileAction(cmd, cliCtx, &dt, yes, confirmDestructive, deviceActionConfig{
				actionName:  name,
				deviceType:  "mobile device",
				destructive: destructive,
				execSingle: func(d *resolve.DeviceIdentifiers, _ io.Reader) error {
					return sendMobileModernMDMCommand(cmd, cliCtx, d, map[string]any{
						"commandType": commandType,
					})
				},
			})
		},
	}
	dt.addFlags(cmd)
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation prompt")
	if destructive {
		cmd.Flags().BoolVar(&confirmDestructive, "confirm-destructive", false, "required for bulk destructive operations")
	}
	return cmd
}

func newMobileRestartCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return newModernMobileMDMCmd(
		cliCtx, "restart", "RESTART_DEVICE",
		"Restart a mobile device",
		"Restart a mobile device by serial number, name, or ID.",
		`  jamf-cli pro md restart --serial F4GH5678
  jamf-cli pro md restart --group "Lab iPads" --yes`,
		false,
	)
}

func newMobileShutdownCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return newModernMobileMDMCmd(
		cliCtx, "shutdown", "SHUT_DOWN_DEVICE",
		"Shut down a mobile device",
		"Shut down a mobile device by serial number, name, or ID.",
		`  jamf-cli pro md shutdown --serial F4GH5678`,
		false,
	)
}

// newMobileUpdateInventoryCmd uses the Classic API — UpdateInventory has no modern equivalent.
func newMobileUpdateInventoryCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		dt  deviceTarget
		yes bool
	)
	cmd := &cobra.Command{
		Use:   "update-inventory",
		Short: "Request an inventory update from a mobile device",
		Long:  "Request a mobile device to submit an updated inventory report.",
		Example: `  jamf-cli pro md update-inventory --serial F4GH5678
  jamf-cli pro md update-inventory --group "All iPads" --yes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMobileAction(cmd, cliCtx, &dt, yes, false, deviceActionConfig{
				actionName: "update-inventory",
				deviceType: "mobile device",
				execSingle: func(d *resolve.DeviceIdentifiers, _ io.Reader) error {
					path := fmt.Sprintf("/JSSResource/mobiledevicecommands/command/UpdateInventory/id/%s", url.PathEscape(d.ID))
					resp, err := cliCtx.Client.Do(cmd.Context(), "POST", path, nil)
					if err != nil {
						return err
					}
					defer func() { _ = resp.Body.Close() }()
					if resp.StatusCode < 200 || resp.StatusCode >= 300 {
						body, _ := io.ReadAll(resp.Body)
						return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
					}
					return nil
				},
			})
		},
	}
	dt.addFlags(cmd)
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation for bulk operations")
	return cmd
}

func newMobileLockCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		dt                 deviceTarget
		yes                bool
		confirmDestructive bool
		message            string
		phoneNumber        string
		pin                string
	)
	cmd := &cobra.Command{
		Use:   "lock",
		Short: "Lock a mobile device",
		Long:  "Lock a supervised mobile device by serial number, name, or ID. This is a destructive operation.",
		Example: `  jamf-cli pro md lock --serial F4GH5678 --yes
  jamf-cli pro md lock --serial F4GH5678 --message "Call IT" --phone-number "555-1234" --yes
  jamf-cli pro md lock --group "Lost Devices" --yes --confirm-destructive`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMobileAction(cmd, cliCtx, &dt, yes, confirmDestructive, deviceActionConfig{
				actionName:  "lock",
				deviceType:  "mobile device",
				destructive: true,
				execSingle: func(d *resolve.DeviceIdentifiers, _ io.Reader) error {
					commandData := map[string]any{"commandType": "DEVICE_LOCK"}
					if message != "" {
						commandData["message"] = message
					}
					if phoneNumber != "" {
						commandData["phoneNumber"] = phoneNumber
					}
					if pin != "" {
						commandData["pin"] = pin
					}
					return sendMobileModernMDMCommand(cmd, cliCtx, d, commandData)
				},
			})
		},
	}
	dt.addFlags(cmd)
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation prompt")
	cmd.Flags().BoolVar(&confirmDestructive, "confirm-destructive", false, "required for bulk destructive operations")
	cmd.Flags().StringVar(&message, "message", "", "message to display on the locked screen")
	cmd.Flags().StringVar(&phoneNumber, "phone-number", "", "phone number to display on the locked screen")
	cmd.Flags().StringVar(&pin, "pin", "", "6-digit PIN required to unlock (supervised devices)")
	return cmd
}

func newMobileClearPasscodeCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		dt                 deviceTarget
		yes                bool
		confirmDestructive bool
		unlockToken        string
	)
	cmd := &cobra.Command{
		Use:     "clear-passcode",
		Short:   "Clear the passcode on a mobile device",
		Long:    "Clear the passcode on a supervised mobile device by serial number, name, or ID.",
		Example: `  jamf-cli pro md clear-passcode --serial F4GH5678 --unlock-token VU5MT0NLVE9LRU4= --yes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMobileAction(cmd, cliCtx, &dt, yes, confirmDestructive, deviceActionConfig{
				actionName:  "clear-passcode",
				deviceType:  "mobile device",
				destructive: true,
				execSingle: func(d *resolve.DeviceIdentifiers, _ io.Reader) error {
					return sendMobileModernMDMCommand(cmd, cliCtx, d, map[string]any{
						"commandType": "CLEAR_PASSCODE",
						"unlockToken": unlockToken,
					})
				},
			})
		},
	}
	dt.addFlags(cmd)
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation prompt")
	cmd.Flags().BoolVar(&confirmDestructive, "confirm-destructive", false, "required for bulk destructive operations")
	cmd.Flags().StringVar(&unlockToken, "unlock-token", "", "base64-encoded unlock token (required for supervised devices)")
	_ = cmd.MarkFlagRequired("unlock-token")
	return cmd
}

func newMobileEnableLostModeCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		dt                 deviceTarget
		yes                bool
		confirmDestructive bool
		message            string
		phone              string
		footnote           string
	)
	cmd := &cobra.Command{
		Use:   "enable-lost-mode",
		Short: "Enable Lost Mode on a supervised mobile device",
		Long:  "Enable Lost Mode on a supervised mobile device. At least one of --message or --phone is required.",
		Example: `  jamf-cli pro md enable-lost-mode --serial F4GH5678 --message "Lost device" --phone "555-1234" --yes
  jamf-cli pro md enable-lost-mode --group "Lost iPads" --message "Contact IT" --yes --confirm-destructive`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if message == "" && phone == "" {
				return fmt.Errorf("at least one of --message or --phone is required")
			}
			return runMobileAction(cmd, cliCtx, &dt, yes, confirmDestructive, deviceActionConfig{
				actionName:  "enable-lost-mode",
				deviceType:  "mobile device",
				destructive: true,
				execSingle: func(d *resolve.DeviceIdentifiers, _ io.Reader) error {
					commandData := map[string]any{"commandType": "ENABLE_LOST_MODE"}
					if message != "" {
						commandData["lostModeMessage"] = message
					}
					if phone != "" {
						commandData["lostModePhone"] = phone
					}
					if footnote != "" {
						commandData["lostModeFootnote"] = footnote
					}
					return sendMobileModernMDMCommand(cmd, cliCtx, d, commandData)
				},
			})
		},
	}
	dt.addFlags(cmd)
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation prompt")
	cmd.Flags().BoolVar(&confirmDestructive, "confirm-destructive", false, "required for bulk destructive operations")
	cmd.Flags().StringVar(&message, "message", "", "message to display in Lost Mode")
	cmd.Flags().StringVar(&phone, "phone", "", "phone number to display in Lost Mode")
	cmd.Flags().StringVar(&footnote, "footnote", "", "footnote to display in Lost Mode")
	return cmd
}

func newMobileDisableLostModeCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return newModernMobileMDMCmd(
		cliCtx, "disable-lost-mode", "DISABLE_LOST_MODE",
		"Disable Lost Mode on a mobile device",
		"Disable Lost Mode on a supervised mobile device that is currently in Lost Mode.",
		`  jamf-cli pro md disable-lost-mode --serial F4GH5678 --yes
  jamf-cli pro md disable-lost-mode --group "Recovered iPads" --yes --confirm-destructive`,
		true,
	)
}

func newMobilePlayLostModeSoundCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return newModernMobileMDMCmd(
		cliCtx, "play-lost-mode-sound", "PLAY_LOST_MODE_SOUND",
		"Play a sound on a device in Lost Mode",
		"Play a sound on a supervised mobile device that is currently in Lost Mode.",
		`  jamf-cli pro md play-lost-mode-sound --serial F4GH5678
  jamf-cli pro md play-lost-mode-sound --group "Lost Devices" --yes`,
		false,
	)
}

func newMobileClearRestrictionsPasswordCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return newModernMobileMDMCmd(
		cliCtx, "clear-restrictions-password", "CLEAR_RESTRICTIONS_PASSWORD",
		"Clear the restrictions password on a mobile device",
		"Clear the restrictions password on a supervised mobile device.",
		`  jamf-cli pro md clear-restrictions-password --serial F4GH5678
  jamf-cli pro md clear-restrictions-password --group "Managed iPads" --yes`,
		false,
	)
}

// --- SETTINGS command (mobile + computer) ---

func newMobileSettingsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		dt               deviceTarget
		yes              bool
		defaultBrowser   string
		defaultCalling   string
		defaultMessaging string
		bluetooth        string
		deviceName       string
		dataRoaming      string
		voiceRoaming     string
		personalHotspot  string
		timeZone         string
		updateCadence    string
	)
	cmd := &cobra.Command{
		Use:   "settings",
		Short: "Send a Settings command to a mobile device",
		Long: `Send an MDM Settings command to configure device settings.

At least one setting flag must be provided. Boolean settings accept "true" or "false".
Default application flags accept an app bundle identifier.`,
		Example: `  # Set default browser
  jamf-cli pro md settings --serial F4GH5678 --default-browser com.google.chrome.ios

  # Reset default apps to Apple defaults
  jamf-cli pro md settings --serial F4GH5678 --default-browser com.apple.mobilesafari

  # Toggle bluetooth and personal hotspot
  jamf-cli pro md settings --serial F4GH5678 --bluetooth false --personal-hotspot false

  # Set device name and timezone
  jamf-cli pro md settings --serial F4GH5678 --device-name "Reception iPad" --time-zone "America/New_York"

  # Bulk via group
  jamf-cli pro md settings --group "Lobby iPads" --default-browser com.google.chrome.ios --yes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			commandData := map[string]any{"commandType": "SETTINGS"}
			set := 0
			if defaultBrowser != "" || defaultCalling != "" || defaultMessaging != "" {
				da := map[string]any{}
				if defaultBrowser != "" {
					da["webBrowser"] = defaultBrowser
				}
				if defaultCalling != "" {
					da["calling"] = defaultCalling
				}
				if defaultMessaging != "" {
					da["messaging"] = defaultMessaging
				}
				commandData["defaultApplications"] = da
				set++
			}
			if cmd.Flags().Changed("bluetooth") {
				commandData["bluetooth"] = bluetooth == "true"
				set++
			}
			if deviceName != "" {
				commandData["deviceName"] = deviceName
				set++
			}
			if cmd.Flags().Changed("data-roaming") {
				if dataRoaming == "true" {
					commandData["dataRoaming"] = "ENABLE_DATA_ROAMING"
				} else {
					commandData["dataRoaming"] = "DISABLE_DATA_ROAMING"
				}
				set++
			}
			if cmd.Flags().Changed("voice-roaming") {
				if voiceRoaming == "true" {
					commandData["voiceRoaming"] = "ENABLE_VOICE_ROAMING"
				} else {
					commandData["voiceRoaming"] = "DISABLE_VOICE_ROAMING"
				}
				set++
			}
			if cmd.Flags().Changed("personal-hotspot") {
				if personalHotspot == "true" {
					commandData["personalHotspot"] = "ENABLE_PERSONAL_HOTSPOT"
				} else {
					commandData["personalHotspot"] = "DISABLE_PERSONAL_HOTSPOT"
				}
				set++
			}
			if timeZone != "" {
				commandData["timeZone"] = timeZone
				set++
			}
			if updateCadence != "" {
				commandData["softwareUpdateSettings"] = map[string]any{
					"recommendationCadence": updateCadence,
				}
				set++
			}
			if set == 0 {
				return fmt.Errorf("at least one setting flag is required")
			}
			return runMobileAction(cmd, cliCtx, &dt, yes, false, deviceActionConfig{
				actionName: "settings",
				deviceType: "mobile device",
				execSingle: func(d *resolve.DeviceIdentifiers, _ io.Reader) error {
					return sendMobileModernMDMCommand(cmd, cliCtx, d, commandData)
				},
			})
		},
	}
	dt.addFlags(cmd)
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation for bulk operations")
	cmd.Flags().StringVar(&defaultBrowser, "default-browser", "", "bundle ID of default web browser")
	cmd.Flags().StringVar(&defaultCalling, "default-calling", "", "bundle ID of default calling app")
	cmd.Flags().StringVar(&defaultMessaging, "default-messaging", "", "bundle ID of default messaging app")
	cmd.Flags().StringVar(&bluetooth, "bluetooth", "", "enable or disable bluetooth (true/false)")
	cmd.Flags().StringVar(&deviceName, "device-name", "", "set the device name")
	cmd.Flags().StringVar(&dataRoaming, "data-roaming", "", "enable or disable data roaming (true/false)")
	cmd.Flags().StringVar(&voiceRoaming, "voice-roaming", "", "enable or disable voice roaming (true/false)")
	cmd.Flags().StringVar(&personalHotspot, "personal-hotspot", "", "enable or disable personal hotspot (true/false)")
	cmd.Flags().StringVar(&timeZone, "time-zone", "", "IANA time zone (e.g. America/New_York)")
	cmd.Flags().StringVar(&updateCadence, "software-update-cadence", "", "ALLOW_ALL_UPDATES, ONLY_ALLOW_LEAST_CURRENT_UPDATE, or ONLY_ALLOW_MOST_CURRENT_UPDATE")
	return cmd
}

func newComputerSettingsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		dt            deviceTarget
		yes           bool
		bluetooth     string
		timeZone      string
		updateCadence string
	)
	cmd := &cobra.Command{
		Use:   "settings",
		Short: "Send a Settings command to a computer",
		Long: `Send an MDM Settings command to configure computer settings.

At least one setting flag must be provided. Boolean settings accept "true" or "false".`,
		Example: `  jamf-cli pro comp settings --serial C02X1234 --bluetooth false
  jamf-cli pro comp settings --serial C02X1234 --time-zone "Europe/London"
  jamf-cli pro comp settings --group "Lab Macs" --software-update-cadence ONLY_ALLOW_MOST_CURRENT_UPDATE --yes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			commandData := map[string]any{"commandType": "SETTINGS"}
			set := 0
			if cmd.Flags().Changed("bluetooth") {
				commandData["bluetooth"] = bluetooth == "true"
				set++
			}
			if timeZone != "" {
				commandData["timeZone"] = timeZone
				set++
			}
			if updateCadence != "" {
				commandData["softwareUpdateSettings"] = map[string]any{
					"recommendationCadence": updateCadence,
				}
				set++
			}
			if set == 0 {
				return fmt.Errorf("at least one setting flag is required")
			}
			return runDeviceAction(cmd, cliCtx, &dt, yes, false, deviceActionConfig{
				actionName: "settings",
				deviceType: "computer",
				execSingle: func(d *resolve.DeviceIdentifiers, _ io.Reader) error {
					return sendComputerModernMDMCommand(cmd, cliCtx, d, commandData)
				},
			})
		},
	}
	dt.addFlags(cmd)
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation for bulk operations")
	cmd.Flags().StringVar(&bluetooth, "bluetooth", "", "enable or disable bluetooth (true/false)")
	cmd.Flags().StringVar(&timeZone, "time-zone", "", "IANA time zone (e.g. America/New_York)")
	cmd.Flags().StringVar(&updateCadence, "software-update-cadence", "", "ALLOW_ALL_UPDATES, ONLY_ALLOW_LEAST_CURRENT_UPDATE, or ONLY_ALLOW_MOST_CURRENT_UPDATE")
	return cmd
}

// --- Additional mobile action commands ---

func newMobileRequestMirroringCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		dt              deviceTarget
		yes             bool
		destinationID   string
		destinationName string
		scanTime        int
	)
	cmd := &cobra.Command{
		Use:   "request-mirroring",
		Short: "Request AirPlay mirroring to a destination",
		Long: `Request a mobile device to start AirPlay mirroring to a specified destination.
One of --destination-id or --destination-name is required.`,
		Example: `  jamf-cli pro md request-mirroring --serial F4GH5678 --destination-name "Conference Room TV"
  jamf-cli pro md request-mirroring --serial F4GH5678 --destination-id "AA:BB:CC:DD:EE:FF"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if destinationID == "" && destinationName == "" {
				return fmt.Errorf("one of --destination-id or --destination-name is required")
			}
			commandData := map[string]any{"commandType": "REQUEST_MIRRORING"}
			if destinationID != "" {
				commandData["destinationDeviceId"] = destinationID
			}
			if destinationName != "" {
				commandData["destinationName"] = destinationName
			}
			if cmd.Flags().Changed("scan-time") {
				commandData["scanTime"] = scanTime
			}
			return runMobileAction(cmd, cliCtx, &dt, yes, false, deviceActionConfig{
				actionName: "request-mirroring",
				deviceType: "mobile device",
				execSingle: func(d *resolve.DeviceIdentifiers, _ io.Reader) error {
					return sendMobileModernMDMCommand(cmd, cliCtx, d, commandData)
				},
			})
		},
	}
	dt.addFlags(cmd)
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation for bulk operations")
	cmd.Flags().StringVar(&destinationID, "destination-id", "", "hardware address of the AirPlay destination (e.g. AA:BB:CC:DD:EE:FF)")
	cmd.Flags().StringVar(&destinationName, "destination-name", "", "name of the AirPlay destination")
	cmd.Flags().IntVar(&scanTime, "scan-time", 0, "seconds to scan for the destination device")
	cmd.MarkFlagsMutuallyExclusive("destination-id", "destination-name")
	return cmd
}

func newMobileStopMirroringCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return newModernMobileMDMCmd(
		cliCtx, "stop-mirroring", "STOP_MIRRORING",
		"Stop AirPlay mirroring on a mobile device",
		"Stop an active AirPlay mirroring session on a mobile device.",
		`  jamf-cli pro md stop-mirroring --serial F4GH5678`,
		false,
	)
}

func newMobileRefreshCellularPlansCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		dt            deviceTarget
		yes           bool
		esimServerURL string
	)
	cmd := &cobra.Command{
		Use:     "refresh-cellular-plans",
		Short:   "Refresh eSIM cellular plans on a mobile device",
		Long:    "Send a command to refresh cellular plans from an eSIM server.",
		Example: `  jamf-cli pro md refresh-cellular-plans --serial F4GH5678 --esim-server-url "https://esim.example.com"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMobileAction(cmd, cliCtx, &dt, yes, false, deviceActionConfig{
				actionName: "refresh-cellular-plans",
				deviceType: "mobile device",
				execSingle: func(d *resolve.DeviceIdentifiers, _ io.Reader) error {
					return sendMobileModernMDMCommand(cmd, cliCtx, d, map[string]any{
						"commandType":   "REFRESH_CELLULAR_PLANS",
						"esimServerUrl": esimServerURL,
					})
				},
			})
		},
	}
	dt.addFlags(cmd)
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation for bulk operations")
	cmd.Flags().StringVar(&esimServerURL, "esim-server-url", "", "URL of the eSIM server (required)")
	_ = cmd.MarkFlagRequired("esim-server-url")
	return cmd
}

func newMobileApplyRedemptionCodeCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		dt             deviceTarget
		yes            bool
		appIdentifier  string
		redemptionCode string
	)
	cmd := &cobra.Command{
		Use:     "apply-redemption-code",
		Short:   "Apply a redemption code for a managed app",
		Long:    "Redeem an App Store code on a managed mobile device for a pending app installation.",
		Example: `  jamf-cli pro md apply-redemption-code --serial F4GH5678 --app com.example.app --code SB56LT7YX8RH`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMobileAction(cmd, cliCtx, &dt, yes, false, deviceActionConfig{
				actionName: "apply-redemption-code",
				deviceType: "mobile device",
				execSingle: func(d *resolve.DeviceIdentifiers, _ io.Reader) error {
					return sendMobileModernMDMCommand(cmd, cliCtx, d, map[string]any{
						"commandType":    "APPLY_REDEMPTION_CODE",
						"identifier":     appIdentifier,
						"redemptionCode": redemptionCode,
					})
				},
			})
		},
	}
	dt.addFlags(cmd)
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation for bulk operations")
	cmd.Flags().StringVar(&appIdentifier, "app", "", "bundle identifier of the app (required)")
	cmd.Flags().StringVar(&redemptionCode, "code", "", "redemption code (required)")
	_ = cmd.MarkFlagRequired("app")
	_ = cmd.MarkFlagRequired("code")
	return cmd
}

// --- Shared iPad / multi-user commands ---

func newMobileDeleteUserCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		dt                 deviceTarget
		yes                bool
		confirmDestructive bool
		userName           string
		forceDeletion      bool
		deleteAll          bool
	)
	cmd := &cobra.Command{
		Use:   "delete-user",
		Short: "Delete a user from a Shared iPad",
		Long: `Delete a user account from a Shared iPad.
Specify --user-name to delete a single user, or --all to delete all users.
This is a destructive operation.`,
		Example: `  jamf-cli pro md delete-user --serial F4GH5678 --user-name "student01" --yes
  jamf-cli pro md delete-user --serial F4GH5678 --all --force --yes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if userName == "" && !deleteAll {
				return fmt.Errorf("one of --user-name or --all is required")
			}
			commandData := map[string]any{"commandType": "DELETE_USER"}
			if userName != "" {
				commandData["userName"] = userName
			}
			if forceDeletion {
				commandData["forceDeletion"] = true
			}
			if deleteAll {
				commandData["deleteAllUsers"] = true
			}
			return runMobileAction(cmd, cliCtx, &dt, yes, confirmDestructive, deviceActionConfig{
				actionName:  "delete-user",
				deviceType:  "mobile device",
				destructive: true,
				execSingle: func(d *resolve.DeviceIdentifiers, _ io.Reader) error {
					return sendMobileModernMDMCommand(cmd, cliCtx, d, commandData)
				},
			})
		},
	}
	dt.addFlags(cmd)
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation prompt")
	cmd.Flags().BoolVar(&confirmDestructive, "confirm-destructive", false, "required for bulk destructive operations")
	cmd.Flags().StringVar(&userName, "user-name", "", "username of the user to delete")
	cmd.Flags().BoolVar(&forceDeletion, "force", false, "force deletion even if the user is logged in")
	cmd.Flags().BoolVar(&deleteAll, "all", false, "delete all users from the device")
	cmd.MarkFlagsMutuallyExclusive("user-name", "all")
	return cmd
}

func newMobileLogOutUserCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return newModernMobileMDMCmd(
		cliCtx, "log-out-user", "LOG_OUT_USER",
		"Log out the current user on a Shared iPad",
		"Log out the currently signed-in user on a Shared iPad.",
		`  jamf-cli pro md log-out-user --serial F4GH5678
  jamf-cli pro md log-out-user --group "Shared iPads" --yes`,
		false,
	)
}

func newMobileUnlockUserAccountCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		dt       deviceTarget
		yes      bool
		userName string
	)
	cmd := &cobra.Command{
		Use:     "unlock-user-account",
		Short:   "Unlock a user account on a Shared iPad",
		Long:    "Unlock a locked user account on a Shared iPad.",
		Example: `  jamf-cli pro md unlock-user-account --serial F4GH5678 --user-name "student01"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMobileAction(cmd, cliCtx, &dt, yes, false, deviceActionConfig{
				actionName: "unlock-user-account",
				deviceType: "mobile device",
				execSingle: func(d *resolve.DeviceIdentifiers, _ io.Reader) error {
					return sendMobileModernMDMCommand(cmd, cliCtx, d, map[string]any{
						"commandType": "UNLOCK_USER_ACCOUNT",
						"userName":    userName,
					})
				},
			})
		},
	}
	dt.addFlags(cmd)
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation for bulk operations")
	cmd.Flags().StringVar(&userName, "user-name", "", "username of the account to unlock (required)")
	_ = cmd.MarkFlagRequired("user-name")
	return cmd
}

// --- Additional computer action commands ---

// resolveAdminAccountGUID looks up the LAPS-capable admin accounts for a device
// and returns the GUID of the MDM-created account (or the account matching userName).
func resolveAdminAccountGUID(cmd *cobra.Command, client registry.HTTPClient, managementID, userName string) (string, error) {
	resp, err := client.Do(cmd.Context(), "GET", fmt.Sprintf("/v2/local-admin-password/%s/accounts", url.PathEscape(managementID)), nil)
	if err != nil {
		return "", fmt.Errorf("fetching LAPS accounts: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("fetching LAPS accounts: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var data struct {
		Results []struct {
			GUID       string `json:"guid"`
			Username   string `json:"username"`
			UserSource string `json:"userSource"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return "", fmt.Errorf("parsing LAPS accounts: %w", err)
	}
	if len(data.Results) == 0 {
		return "", fmt.Errorf("no LAPS-capable admin accounts found on this device")
	}

	// If user specified a username, match it.
	if userName != "" {
		for _, a := range data.Results {
			if strings.EqualFold(a.Username, userName) {
				return a.GUID, nil
			}
		}
		return "", fmt.Errorf("no LAPS account found with username %q", userName)
	}

	// Default: prefer the MDM-created account.
	for _, a := range data.Results {
		if a.UserSource == "MDM" {
			return a.GUID, nil
		}
	}
	// Fall back to the first account if no MDM source found.
	return data.Results[0].GUID, nil
}

func newComputerSetAutoAdminPasswordCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		dt                 deviceTarget
		yes                bool
		confirmDestructive bool
		userName           string
		passwordFile       string
	)
	cmd := &cobra.Command{
		Use:   "set-auto-admin-password",
		Short: "Set the auto-admin password on a computer",
		Long: `Set the password for a local administrator account created during DEP enrollment.

The account GUID is resolved automatically from the device's LAPS-capable accounts
(defaults to the MDM-created account). Use --user-name to target a specific account.

The password is read from a file (--password-file) or prompted interactively.
It is never accepted as a flag value.`,
		Example: `  # Interactive password prompt (auto-resolves MDM admin account)
  jamf-cli pro comp set-auto-admin-password --serial C02X1234 --yes

  # Target a specific admin username
  jamf-cli pro comp set-auto-admin-password --serial C02X1234 --user-name "jamfadmin" --yes

  # Password from file (CI/CD)
  jamf-cli pro comp set-auto-admin-password --serial C02X1234 --password-file /tmp/pw.txt --yes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			var password string
			if passwordFile != "" {
				data, err := os.ReadFile(passwordFile)
				if err != nil {
					return fmt.Errorf("reading password file: %w", err)
				}
				password = strings.TrimRight(string(data), "\r\n")
			} else if noInput {
				return fmt.Errorf("--password-file is required when --no-input is set")
			} else {
				fmt.Fprint(os.Stderr, "Enter new admin password: ")
				passBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
				fmt.Fprintln(os.Stderr)
				if err != nil {
					return fmt.Errorf("reading password: %w", err)
				}
				password = string(passBytes)
			}
			if password == "" {
				return fmt.Errorf("password must not be empty")
			}
			return runDeviceAction(cmd, cliCtx, &dt, yes, confirmDestructive, deviceActionConfig{
				actionName:  "set-auto-admin-password",
				deviceType:  "computer",
				destructive: true,
				execSingle: func(d *resolve.DeviceIdentifiers, _ io.Reader) error {
					guid, err := resolveAdminAccountGUID(cmd, cliCtx.Client, d.ManagementID, userName)
					if err != nil {
						return fmt.Errorf("device %s: %w", resolve.FormatDeviceDesc(d), err)
					}
					return sendComputerModernMDMCommand(cmd, cliCtx, d, map[string]any{
						"commandType": "SET_AUTO_ADMIN_PASSWORD",
						"guid":        guid,
						"password":    password,
					})
				},
			})
		},
	}
	dt.addFlags(cmd)
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation prompt")
	cmd.Flags().BoolVar(&confirmDestructive, "confirm-destructive", false, "required for bulk destructive operations")
	cmd.Flags().StringVar(&userName, "user-name", "", "username of the admin account (default: MDM-created account)")
	cmd.Flags().StringVar(&passwordFile, "password-file", "", "file containing the new password")
	return cmd
}
