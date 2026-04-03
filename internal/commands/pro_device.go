// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/output"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

func newDeviceCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "device <name|serial|id>",
		Short: "Show a comprehensive view of a single device",
		Long: `Display a detailed, aggregated view of a single Jamf Pro device including
identity, hardware, OS, security posture, user info, MDM command history,
and policy execution logs.

The device can be identified by its Jamf Pro ID, serial number, or name.
MDM and policy history are fetched in parallel; partial failures are shown
as warnings on stderr and do not prevent the rest of the report.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sections, err := runDeviceDeepDive(cmd.Context(), cliCtx.Client, args[0])
			if err != nil {
				return err
			}

			if !cmd.Flags().Changed("output") || outputFmt == "table" {
				printOverviewTable(cmd.OutOrStdout(), sections, !noColor, "DEVICE DETAIL")
				return nil
			}

			rows := overviewToRows(sections)
			formatter := output.New(outputFmt, noColor, wide)
			return formatter.Print(rows)
		},
	}
}

// runDeviceDeepDive resolves a device and fetches a comprehensive view.
func runDeviceDeepDive(ctx context.Context, client registry.HTTPClient, identifier string) ([]overviewSection, error) {
	// 1. Resolve device.
	deviceID, deviceName, err := resolveDeviceByIdentifier(ctx, client, identifier)
	if err != nil {
		return nil, err
	}

	// 2. Fetch full detail.
	detail, err := fetchJSON(ctx, client, "/v1/computers-inventory-detail/"+deviceID)
	if err != nil {
		return nil, fmt.Errorf("fetching device detail: %w", err)
	}

	// 3. Extract sections from the detail response.
	general, _ := detail["general"].(map[string]any)
	hardware, _ := detail["hardware"].(map[string]any)
	operatingSystem, _ := detail["operatingSystem"].(map[string]any)
	security, _ := detail["security"].(map[string]any)
	userAndLocation, _ := detail["userAndLocation"].(map[string]any)

	// 4. Build core sections.
	sections := []overviewSection{
		buildIdentitySection(deviceID, deviceName, general),
		buildHardwareSection(hardware, operatingSystem),
		buildSecuritySection(security),
		buildUserLocationSection(userAndLocation),
	}

	// 5. Fetch history in parallel.
	var (
		mu         sync.Mutex
		wg         sync.WaitGroup
		mdmSection *overviewSection
		polSection *overviewSection
	)

	wg.Add(2)

	// The MDM commands API filters on managementId (a UUID), not the numeric Jamf ID.
	managementID := strVal(general, "managementId")

	go func() {
		defer wg.Done()
		s := fetchMDMHistory(ctx, client, managementID)
		mu.Lock()
		mdmSection = s
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		s := fetchPolicyHistory(ctx, client, deviceID)
		mu.Lock()
		polSection = s
		mu.Unlock()
	}()

	wg.Wait()

	// 6. Append non-nil history sections.
	if mdmSection != nil {
		sections = append(sections, *mdmSection)
	}
	if polSection != nil {
		sections = append(sections, *polSection)
	}

	return sections, nil
}

func buildIdentitySection(id, name string, general map[string]any) overviewSection {
	return overviewSection{
		Name: "Identity",
		Items: []overviewItem{
			{Resource: "Name", Value: name},
			{Resource: "Jamf ID", Value: id},
			{Resource: "Platform", Value: strVal(general, "platform")},
			{Resource: "Last IP", Value: strVal(general, "lastIpAddress")},
			{Resource: "Managed", Value: nestedBoolStr(general, "remoteManagement", "managed")},
			{Resource: "MDM Capable", Value: nestedBoolStr(general, "mdmCapable", "capable")},
			{Resource: "Supervised", Value: boolDisplay(boolVal(general, "supervised"))},
			{Resource: "DEP Enrolled", Value: boolDisplay(boolVal(general, "enrolledViaAutomatedDeviceEnrollment"))},
			{Resource: "Last Contact", Value: strVal(general, "lastContactTime")},
		},
	}
}

func buildHardwareSection(hw, os map[string]any) overviewSection {
	ramMB := numStr(hw, "totalRamMegabytes")
	ramDisplay := ramMB + " MB"
	if ramMB == "" {
		ramDisplay = ""
	}

	return overviewSection{
		Name: "Hardware & OS",
		Items: []overviewItem{
			{Resource: "Model", Value: strVal(hw, "model")},
			{Resource: "Serial Number", Value: strVal(hw, "serialNumber")},
			{Resource: "Processor", Value: strVal(hw, "processorType")},
			{Resource: "RAM", Value: ramDisplay},
			{Resource: "Make", Value: strVal(hw, "make")},
			{},
			{Resource: "OS", Value: strVal(os, "name") + " " + strVal(os, "version")},
			{Resource: "Build", Value: strVal(os, "build")},
		},
	}
}

func buildSecuritySection(sec map[string]any) overviewSection {
	sipStatus := strVal(sec, "sipStatus")
	gkStatus := strVal(sec, "gatekeeperStatus")
	fwEnabled := boolVal(sec, "firewallEnabled")
	btAllowed := boolVal(sec, "bootstrapTokenAllowed")
	btEscrow := strVal(sec, "bootstrapTokenEscrowedStatus")

	// FileVault: look for diskEncryptionStatus or fall back to common field names.
	fvStatus := strVal(sec, "fileVaultStatus")
	if fvStatus == "" {
		fvStatus = strVal(sec, "diskEncryptionStatus")
	}

	var fvColor string
	if fvStatus != "" && fvStatus != statusFVAllEncrypted && fvStatus != statusFVBootEncrypted {
		fvColor = "red"
	}

	var gkColor string
	if gkStatus == statusGKDisabled || gkStatus == statusGKDisabledAlt {
		gkColor = "red"
	}

	return overviewSection{
		Name: "Security",
		Items: []overviewItem{
			{Resource: "SIP", Value: sipStatus},
			{Resource: "Gatekeeper", Value: gkStatus, ColorHint: gkColor},
			{Resource: "Firewall", Value: boolDisplay(fwEnabled)},
			{Resource: "Bootstrap Token Allowed", Value: boolDisplay(btAllowed)},
			{Resource: "Bootstrap Token Escrowed", Value: btEscrow},
			{Resource: "FileVault", Value: fvStatus, ColorHint: fvColor},
		},
	}
}

func buildUserLocationSection(ul map[string]any) overviewSection {
	return overviewSection{
		Name: "User & Location",
		Items: []overviewItem{
			{Resource: "Username", Value: strVal(ul, "username")},
			{Resource: "Full Name", Value: strVal(ul, "realname")},
			{Resource: "Email", Value: strVal(ul, "email")},
			{Resource: "Department", Value: strVal(ul, "department")},
			{Resource: "Building", Value: strVal(ul, "building")},
		},
	}
}

// fetchMDMHistory fetches recent MDM commands for a device.
// Returns nil on error (logs warning to stderr).
func fetchMDMHistory(ctx context.Context, client registry.HTTPClient, managementID string) *overviewSection {
	if managementID == "" {
		return nil
	}
	filter := fmt.Sprintf("clientManagementId==%s", managementID)
	path := "/v2/mdm/commands?filter=" + url.QueryEscape(filter) + "&page-size=10&sort=completedDateTime%3Adesc"

	data, err := fetchJSON(ctx, client, path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to fetch MDM history: %v\n", err)
		return nil
	}

	results, _ := data["results"].([]any)
	if len(results) == 0 {
		return &overviewSection{
			Name:  "MDM Command History (Last 10)",
			Items: []overviewItem{{Resource: "(none)", Value: ""}},
		}
	}

	items := make([]overviewItem, 0, len(results))
	for _, r := range results {
		cmd, ok := r.(map[string]any)
		if !ok {
			continue
		}
		cmdType := strVal(cmd, "commandType")
		status := strVal(cmd, "status")

		var color string
		if strings.EqualFold(status, "Error") || strings.EqualFold(status, "Failed") {
			color = "red"
		}

		items = append(items, overviewItem{
			Resource:  cmdType,
			Value:     status,
			ColorHint: color,
		})
	}

	return &overviewSection{
		Name:  "MDM Command History (Last 10)",
		Items: items,
	}
}

// fetchPolicyHistory fetches recent policy execution logs from the Classic API.
// Returns nil on error (logs warning to stderr).
func fetchPolicyHistory(ctx context.Context, client registry.HTTPClient, deviceID string) *overviewSection {
	path := "/JSSResource/computerhistory/id/" + deviceID

	data, err := fetchJSON(ctx, client, path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to fetch policy history: %v\n", err)
		return nil
	}

	// Classic API wraps in {"computer_history": {...}}
	inner := unwrapClassicDetail(data)

	// Extract policy_logs
	var logs []any
	if pl, ok := inner["policy_logs"].(map[string]any); ok {
		if arr, ok := pl["policy_log"].([]any); ok {
			logs = arr
		}
	} else if arr, ok := inner["policy_logs"].([]any); ok {
		logs = arr
	}

	if len(logs) == 0 {
		return &overviewSection{
			Name:  "Policy History (Last 10)",
			Items: []overviewItem{{Resource: "(none)", Value: ""}},
		}
	}

	// Take last 10
	start := 0
	if len(logs) > 10 {
		start = len(logs) - 10
	}
	logs = logs[start:]

	items := make([]overviewItem, 0, len(logs))
	for _, l := range logs {
		entry, ok := l.(map[string]any)
		if !ok {
			continue
		}
		name := strVal(entry, "policy_name")
		status := strVal(entry, "status")

		var color string
		if strings.EqualFold(status, "Failed") {
			color = "red"
		}

		items = append(items, overviewItem{
			Resource:  name,
			Value:     status,
			ColorHint: color,
		})
	}

	return &overviewSection{
		Name:  "Policy History (Last 10)",
		Items: items,
	}
}

// numStr extracts a numeric value as a string. Handles both float64 (JSON default) and string.
func numStr(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	switch v := m[key].(type) {
	case float64:
		return fmt.Sprintf("%d", int(v))
	case string:
		return v
	default:
		return ""
	}
}

// nestedBoolStr extracts a bool from a nested map (e.g., general["remoteManagement"]["managed"])
// and returns "Yes"/"No"/"Unknown".
func nestedBoolStr(m map[string]any, outerKey, innerKey string) string {
	if m == nil {
		return "Unknown"
	}
	outer, ok := m[outerKey].(map[string]any)
	if !ok {
		return "Unknown"
	}
	v, ok := outer[innerKey].(bool)
	if !ok {
		return "Unknown"
	}
	return boolDisplay(v)
}

// boolDisplay converts a bool to a human-friendly "Yes"/"No".
func boolDisplay(v bool) string {
	if v {
		return "Yes"
	}
	return "No"
}
