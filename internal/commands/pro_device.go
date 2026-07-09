// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/output"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/blueprints"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/compliancebenchmarks"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/ddmreport"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/devicegroups"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/devices"
)

func newDeviceCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:    "device <name|serial|id>",
		Hidden: true,
		Short:  "Show a comprehensive view of a single device",
		Long: `Display a detailed, aggregated view of a single Jamf Pro device including
identity, hardware, OS, security posture, user info, MDM command history,
and policy execution logs.

The device can be identified by its Jamf Pro ID, serial number, or name.
MDM and policy history are fetched in parallel; partial failures are shown
as warnings on stderr and do not prevent the rest of the report.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sections, err := runDeviceDeepDive(cmd.Context(), cliCtx, args[0])
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
func runDeviceDeepDive(ctx context.Context, cliCtx *registry.CLIContext, identifier string) ([]overviewSection, error) {
	client := cliCtx.Client

	// 1. Resolve device.
	deviceID, deviceName, err := resolveDeviceByIdentifier(ctx, client, identifier)
	if err != nil {
		return nil, err
	}

	// 2. Fetch full detail.
	detail, err := fetchJSON(ctx, client, "/v3/computers-inventory-detail/"+deviceID)
	if err != nil {
		return nil, fmt.Errorf("fetching device detail: %w", err)
	}

	// 3. Extract sections from the detail response.
	general, _ := detail["general"].(map[string]any)
	hardware, _ := detail["hardware"].(map[string]any)
	operatingSystem, _ := detail["operatingSystem"].(map[string]any)
	security, _ := detail["security"].(map[string]any)
	diskEnc, _ := detail["diskEncryption"].(map[string]any)
	userAndLocation, _ := detail["userAndLocation"].(map[string]any)

	// 4. Build core sections.
	sections := []overviewSection{
		buildIdentitySection(deviceID, deviceName, general),
		buildHardwareSection(hardware, operatingSystem),
		buildSecuritySection(security, diskEnc),
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

	// 7. Platform sections (blueprints + compliance) when platform auth is active.
	if cliCtx.PlatformSDKClient != nil {
		serial := strVal(hardware, "serialNumber")
		platformSections := fetchDevicePlatformSections(ctx, cliCtx, serial)
		sections = append(sections, platformSections...)
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

func buildSecuritySection(sec, diskEnc map[string]any) overviewSection {
	sipStatus := strVal(sec, "sipStatus")
	gkStatus := strVal(sec, "gatekeeperStatus")
	fwEnabled := boolVal(sec, "firewallEnabled")
	btAllowed := boolVal(sec, "bootstrapTokenAllowed")
	btEscrow := strVal(sec, "bootstrapTokenEscrowedStatus")

	// v3 moved FileVault from security to diskEncryption section.
	// Use partition state as ground truth for encryption status.
	fvStatus := fileVaultStatus(diskEnc)

	var fvColor string
	if fvStatus != "" && fvStatus != statusFVEncrypted {
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
	filter := fmt.Sprintf(`clientManagementId=="%s"`, managementID)
	path := "/v2/mdm/commands?filter=" + url.QueryEscape(filter) + "&page-size=10&sort=dateCompleted%3Adesc"

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
		// The v2 API field is "commandState" (e.g. ACKNOWLEDGED, PENDING, ERROR),
		// not "status" — reading the wrong key left this column blank.
		state := strVal(cmd, "commandState")
		completed := formatCompletedDate(strVal(cmd, "dateCompleted"))

		var color string
		if strings.EqualFold(state, "Error") || strings.EqualFold(state, "Failed") {
			color = "red"
		}

		items = append(items, overviewItem{
			Resource:  cmdType,
			Value:     historyValue(state, completed),
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
		completed := formatCompletedDate(strVal(entry, "date_completed_utc"))

		var color string
		if strings.EqualFold(status, "Failed") {
			color = "red"
		}

		items = append(items, overviewItem{
			Resource:  name,
			Value:     historyValue(status, completed),
			ColorHint: color,
		})
	}

	return &overviewSection{
		Name:  "Policy History (Last 10)",
		Items: items,
	}
}

// formatCompletedDate normalizes an API completion timestamp to a compact
// "2006-01-02 15:04" form. It returns "" when the value is empty or the
// epoch-zero sentinel APIs use for "not completed" (e.g. 1970-01-01T00:00:00Z
// for a pending MDM command). Unparseable values are returned unchanged so no
// data is silently dropped.
func formatCompletedDate(raw string) string {
	if raw == "" {
		return ""
	}
	// MDM v2 dates are RFC3339 with "Z"; Classic date_completed_utc uses a
	// numeric "-0700" offset without a colon.
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.000-0700",
		"2006-01-02T15:04:05-0700",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, raw); err == nil {
			if t.Year() <= 1970 {
				return ""
			}
			return t.Format("2006-01-02 15:04")
		}
	}
	return raw
}

// historyValue joins a history entry's status/state with its completion date
// for display in a single right-aligned value column, omitting either side
// when empty.
func historyValue(status, completed string) string {
	switch {
	case completed == "":
		return status
	case status == "":
		return completed
	default:
		return status + "  " + completed
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

// fetchDevicePlatformSections returns Platform Blueprints and Compliance sections
// for a device identified by serial number. It resolves the device's platform
// device groups and cross-references them against blueprints and benchmarks.
func fetchDevicePlatformSections(ctx context.Context, cliCtx *registry.CLIContext, serial string) []overviewSection {
	c := cliCtx.PlatformSDKClient
	bp := blueprints.New(c)
	cb := compliancebenchmarks.New(c)
	dg := devicegroups.New(c)
	if serial == "" {
		return nil
	}

	// Resolve serial to platform device ID
	d := devices.New(cliCtx.PlatformSDKClient)
	devID, err := d.ResolveDeviceIDBySerialNumber(ctx, serial)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to resolve platform device: %v\n", err)
		return nil
	}

	// Get device's group memberships
	deviceGroups, err := dg.ListDeviceGroupsForDevice(ctx, devID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to fetch platform device groups: %v\n", err)
		return nil
	}
	groupIDs := make(map[string]bool, len(deviceGroups))
	for _, g := range deviceGroups {
		groupIDs[g.GroupID] = true
	}

	var (
		mu         sync.Mutex
		wg         sync.WaitGroup
		bpSection  *overviewSection
		cbSection  *overviewSection
		ddmSection *overviewSection
	)

	// Fetch blueprints, benchmarks, and DDM report in parallel
	wg.Add(3)

	// DDM declaration report — aggregate by source, show errors
	go func() {
		defer wg.Done()
		declarations, err := ddmreport.New(c).GetDeviceDeclarationReportFiltered(ctx, devID, ddmAllDeclarationsFilter, nil)
		if err != nil {
			return
		}

		// Build lookup maps
		bpNames := make(map[string]string)
		if bps, err := bp.ListBlueprints(ctx, nil, ""); err == nil {
			for _, item := range bps {
				bpNames[item.ID] = item.Name
			}
		}
		type sourceAgg struct {
			kind         string
			successful   int
			unsuccessful int
			pending      int
			total        int
		}
		bySource := make(map[string]*sourceAgg)
		type ddmError struct {
			source string
			reason string
		}
		var errors []ddmError

		for _, d := range declarations {
			source, kind := classifyDeclaration(d.DeclarationIdentifier, bpNames, nil)

			a, ok := bySource[source]
			if !ok {
				a = &sourceAgg{kind: kind}
				bySource[source] = a
			}
			a.total++
			switch d.Status {
			case "SUCCESSFUL":
				a.successful++
			case "PENDING", "AWAITING_SYNC":
				a.pending++
			default:
				a.unsuccessful++
			}

			for _, r := range d.Reasons {
				if ignorableDDMReasonCodes[r.Code] {
					continue
				}
				errors = append(errors, ddmError{source, r.Code + ": " + r.Description})
			}
		}

		if len(bySource) == 0 {
			mu.Lock()
			ddmSection = &overviewSection{Name: "DDM Declarations", Items: []overviewItem{{Resource: "(none)", Value: ""}}}
			mu.Unlock()
			return
		}

		var items []overviewItem
		for name, a := range bySource {
			if a.kind == "standalone" {
				continue
			}
			status := fmt.Sprintf("%d ok", a.successful)
			if a.unsuccessful > 0 {
				status += fmt.Sprintf(", %d failed", a.unsuccessful)
			}
			if a.pending > 0 {
				status += fmt.Sprintf(", %d pending", a.pending)
			}
			items = append(items, overviewItem{Resource: name, Value: status})
		}
		for _, e := range errors {
			items = append(items, overviewItem{})
			items = append(items, overviewItem{Resource: e.source, Value: e.reason})
		}

		mu.Lock()
		ddmSection = &overviewSection{Name: "DDM Declarations", Items: items}
		mu.Unlock()
	}()

	// Blueprints scoped to this device's groups
	go func() {
		defer wg.Done()
		bps, err := bp.ListBlueprints(ctx, nil, "")
		if err != nil {
			return
		}
		var items []overviewItem
		for _, item := range bps {
			detail, err := bp.GetBlueprint(ctx, item.ID)
			if err != nil {
				continue
			}
			if detail.Scope == nil || !scopeOverlaps(detail.Scope.DeviceGroups, groupIDs) {
				continue
			}
			state := ""
			if detail.DeploymentState != nil {
				state = detail.DeploymentState.State
			}
			items = append(items, overviewItem{detail.Name, state, ""})
		}
		if len(items) == 0 {
			items = []overviewItem{{Resource: "(none)", Value: ""}}
		}
		mu.Lock()
		bpSection = &overviewSection{Name: "Platform Blueprints", Items: items}
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		resp, err := cb.ListBenchmarks(ctx)
		if err != nil {
			return
		}
		var items []overviewItem
		for _, b := range resp.Benchmarks {
			bm, err := cb.GetBenchmark(ctx, b.ID)
			if err != nil {
				continue
			}
			if bm.Target == nil || !scopeOverlaps(bm.Target.DeviceGroups, groupIDs) {
				continue
			}
			items = append(items, overviewItem{bm.Title, bm.EnforcementMode, ""})
		}
		if len(items) == 0 {
			items = []overviewItem{{Resource: "(none)", Value: ""}}
		}
		mu.Lock()
		cbSection = &overviewSection{Name: "Platform Compliance", Items: items}
		mu.Unlock()
	}()

	wg.Wait()

	var sections []overviewSection
	if bpSection != nil {
		sections = append(sections, *bpSection)
	}
	if cbSection != nil {
		sections = append(sections, *cbSection)
	}
	if ddmSection != nil {
		sections = append(sections, *ddmSection)
	}
	return sections
}

// scopeOverlaps returns true if any of the given scope IDs are in the target set.
func scopeOverlaps(scopeIDs []string, targetSet map[string]bool) bool {
	for _, id := range scopeIDs {
		if targetSet[id] {
			return true
		}
	}
	return false
}
