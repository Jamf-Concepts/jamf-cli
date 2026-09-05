// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type mdmHealthReport struct {
	Summary        mdmHealthSummary
	Failures       []mdmFailureSummary
	DeviceFailures []mdmDeviceSummary
	DevicePending  []mdmDeviceSummary
}

type mdmHealthSummary struct {
	TotalErrors        int
	UniqueProfiles     int
	UniqueDevices      int
	DevicesHighFailure int
	DevicesHighPending int
	Days               int
	CommandType        string
}

type mdmDeviceSummary struct {
	ManagementID string
	DeviceType   string
	Name         string
	Serial       string
	OSVersion    string
	Username     string
	Count        int
}

type mdmFailureSummary struct {
	Name       string // profile identifier or app name
	ID         string // profileId
	DeviceType string // COMPUTER, MOBILE_DEVICE, or MIXED
	Errors     int
	Devices    int
	LastError  string
	TopError   string // most common error description
}

// mdmCommandResult is a single MDM command from /v2/mdm/commands.
type mdmCommandResult struct {
	UUID              string
	CommandType       string
	Status            string
	DateCompleted     time.Time
	ProfileID         string
	ProfileIdentifier string
	ClientMgmtID      string
	ClientType        string
	ErrorDescription  string
}

// ---------------------------------------------------------------------------
// Commands
// ---------------------------------------------------------------------------

func newReportProfileStatusCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var days int

	cmd := &cobra.Command{
		Use:   "profile-status",
		Short: "Configuration profile deployment health",
		Long: `Reports on configuration profile deployment failures from MDM command history.

Queries the MDM commands API for failed InstallProfile commands and aggregates
by profile. This is a single paginated API call — no per-device fetching.

Examples:
  # Profile failures in the last 30 days
  jamf-cli pro report profile-status

  # Narrow the window
  jamf-cli pro report profile-status --days 7

With no -o flag, this report writes a table. Then --out-file receives that
table, not JSON. Use -o json to write structured data to the file.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("output") {
				outputFmt = "table"
			}
			return runMDMHealthReport(cmd.Context(), cliCtx, "INSTALL_PROFILE", "profile", days)
		},
	}

	cmd.Flags().IntVar(&days, "days", 30, "look back N days for failures")
	return cmd
}

func newReportAppStatusCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var days int

	cmd := &cobra.Command{
		Use:   "app-status",
		Short: "Managed app deployment health",
		Long: `Reports on managed app deployment failures from MDM command history.

Queries the MDM commands API for failed InstallApplication and
InstallEnterpriseApplication commands and aggregates by app.

Examples:
  # App failures in the last 30 days
  jamf-cli pro report app-status

  # Narrow the window
  jamf-cli pro report app-status --days 7

With no -o flag, this report writes a table. Then --out-file receives that
table, not JSON. Use -o json to write structured data to the file.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("output") {
				outputFmt = "table"
			}
			return runMDMHealthReport(cmd.Context(), cliCtx, "INSTALL_APPLICATION,INSTALL_ENTERPRISE_APPLICATION", "app", days)
		},
	}

	cmd.Flags().IntVar(&days, "days", 30, "look back N days for failures")
	return cmd
}

// ---------------------------------------------------------------------------
// Core logic — shared between profile-status and app-status
// ---------------------------------------------------------------------------

func runMDMHealthReport(ctx context.Context, cliCtx *registry.CLIContext, commandTypes, label string, days int) error {
	client := cliCtx.Client
	cutoff := time.Now().AddDate(0, 0, -days)
	cutoffStr := cutoff.UTC().Format("2006-01-02T15:04:05.000Z")

	fmt.Fprintf(os.Stderr, "Fetching failed %s commands (last %d days)...\n", label, days)

	// Query each command type separately — RSQL OR syntax is unreliable
	// across Jamf Pro versions.
	var results []map[string]any
	for _, ct := range splitCommaSeparated(commandTypes) {
		filter := fmt.Sprintf("command==%s;status==Error;dateCompleted=ge=%s", ct, cutoffStr)
		path := "/v2/mdm/commands?filter=" + url.QueryEscape(filter) + "&sort=dateCompleted%3Adesc"
		batch, err := FetchAllPaginated(ctx, client, path, 200)
		if err != nil {
			return fmt.Errorf("fetching %s commands: %w", ct, err)
		}
		results = append(results, batch...)
	}

	// Parse results
	parsed := make([]mdmCommandResult, 0, len(results))
	for _, m := range results {
		parsed = append(parsed, parseMDMCommand(m))
	}

	// Build name lookup from Classic API
	var nameLookup map[string]string
	if len(parsed) > 0 {
		switch label {
		case "profile":
			nameLookup = fetchProfileNameLookup(ctx, client)
		case "app":
			nameLookup = fetchAppNameLookup(ctx, client)
		}
	}

	// Aggregate failures by profile/app
	report := aggregateMDMFailures(parsed, nameLookup, label, days)

	// Per-device failure aggregation (devices with >5 errors)
	rawDeviceFailures := aggregateByDevice(parsed, 5, nil)

	// Fetch pending commands for the same command types (within date window)
	fmt.Fprintf(os.Stderr, "Fetching pending %s commands...\n", label)
	var pendingResults []map[string]any
	for _, ct := range splitCommaSeparated(commandTypes) {
		filter := fmt.Sprintf("command==%s;status==Pending;dateSent=ge=%s", ct, cutoffStr)
		path := "/v2/mdm/commands?filter=" + url.QueryEscape(filter)
		batch, err := FetchAllPaginated(ctx, client, path, 200)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: failed to fetch pending commands: %v\n", err)
			break
		}
		pendingResults = append(pendingResults, batch...)
	}

	var pendingParsed []mdmCommandResult
	for _, m := range pendingResults {
		pendingParsed = append(pendingParsed, parseMDMCommand(m))
	}
	rawDevicePending := aggregateByDevice(pendingParsed, 10, nil)

	// Only fetch device inventory if we have flagged devices to enrich
	if len(rawDeviceFailures) > 0 || len(rawDevicePending) > 0 {
		deviceLookup := fetchDeviceLookup(ctx, client)
		report.DeviceFailures = enrichDeviceSummaries(rawDeviceFailures, deviceLookup)
		report.DevicePending = enrichDeviceSummaries(rawDevicePending, deviceLookup)
	}
	report.Summary.DevicesHighFailure = len(report.DeviceFailures)
	report.Summary.DevicesHighPending = len(report.DevicePending)

	return printMDMHealthReport(cliCtx, report, label)
}

func parseMDMCommand(m map[string]any) mdmCommandResult {
	var dateCompleted time.Time
	if ds := strVal(m, "dateCompleted"); ds != "" {
		dateCompleted, _ = time.Parse(time.RFC3339, ds)
		if dateCompleted.IsZero() {
			dateCompleted, _ = time.Parse("2006-01-02T15:04:05.999Z", ds)
		}
	}

	profileID := extractField(m, "profileId")

	// Profile identifier may be at top level or nested
	profileIdentifier := strVal(m, "profileIdentifier")

	// Client info
	clientMgmtID := ""
	clientType := ""
	if client, ok := m["client"].(map[string]any); ok {
		clientMgmtID = strVal(client, "managementId")
		clientType = strVal(client, "clientType")
	}

	// Error description
	errorDesc := ""
	if cmdErr, ok := m["commandError"].(map[string]any); ok {
		errorDesc = strVal(cmdErr, "errorLocalizedDescription")
		if errorDesc == "" {
			errorDesc = strVal(cmdErr, "errorEnglishDescription")
		}
		if errorDesc == "" {
			errorDesc = strVal(cmdErr, "errorDomain")
		}
	}

	return mdmCommandResult{
		UUID:              strVal(m, "uuid"),
		CommandType:       strVal(m, "commandType"),
		Status:            strVal(m, "commandState"),
		DateCompleted:     dateCompleted,
		ProfileID:         profileID,
		ProfileIdentifier: profileIdentifier,
		ClientMgmtID:      clientMgmtID,
		ClientType:        clientType,
		ErrorDescription:  errorDesc,
	}
}

// fetchProfileNameLookup fetches macOS and mobile config profile lists and
// builds an id->name lookup. Returns empty map on error (non-fatal).
func fetchProfileNameLookup(ctx context.Context, client registry.HTTPClient) map[string]string {
	lookup := make(map[string]string)

	// macOS config profiles
	macProfiles, err := FetchClassicList(ctx, client, "/JSSResource/osxconfigurationprofiles", "os_x_configuration_profiles")
	if err == nil {
		for _, r := range macProfiles {
			if m, ok := r.(map[string]any); ok {
				lookup[extractID(m)] = extractField(m, "name")
			}
		}
	}

	// Mobile device config profiles
	mobileProfiles, err := FetchClassicList(ctx, client, "/JSSResource/mobiledeviceconfigurationprofiles", "configuration_profiles")
	if err == nil {
		for _, r := range mobileProfiles {
			if m, ok := r.(map[string]any); ok {
				lookup[extractID(m)] = extractField(m, "name")
			}
		}
	}

	return lookup
}

// fetchAppNameLookup fetches mobile device apps and Mac apps from the Classic
// API and builds an id->name lookup. Returns empty map on error (non-fatal).
func fetchAppNameLookup(ctx context.Context, client registry.HTTPClient) map[string]string {
	lookup := make(map[string]string)

	// Mobile device apps
	mobileApps, err := FetchClassicList(ctx, client, "/JSSResource/mobiledeviceapplications", "mobile_device_applications")
	if err == nil {
		for _, r := range mobileApps {
			if m, ok := r.(map[string]any); ok {
				lookup[extractID(m)] = extractField(m, "name")
			}
		}
	}

	// Mac App Store apps
	macApps, err := FetchClassicList(ctx, client, "/JSSResource/macapplications", "mac_applications")
	if err == nil {
		for _, r := range macApps {
			if m, ok := r.(map[string]any); ok {
				lookup[extractID(m)] = extractField(m, "name")
			}
		}
	}

	return lookup
}

// mdmDeviceMeta holds inventory data for a device, keyed by managementId.
type mdmDeviceMeta struct {
	name, serial, osVersion, username string
}

// fetchDeviceLookup fetches computer and mobile device inventory lists and
// builds a managementId -> device meta lookup.
func fetchDeviceLookup(ctx context.Context, client registry.HTTPClient) map[string]mdmDeviceMeta {
	lookup := make(map[string]mdmDeviceMeta)

	// Computers
	computers, err := FetchAllPaginated(ctx, client, "/v4/computers-inventory?section=GENERAL&section=HARDWARE&section=OPERATING_SYSTEM&section=USER_AND_LOCATION", 2000)
	if err == nil {
		for _, c := range computers {
			mgmtID := strVal(c, "managementId")
			if mgmtID == "" {
				// Sectioned format: managementId might be top-level
				general, _ := c["general"].(map[string]any)
				mgmtID = strVal(general, "managementId")
			}
			if mgmtID == "" {
				continue
			}
			general, _ := c["general"].(map[string]any)
			hardware, _ := c["hardware"].(map[string]any)
			osInfo, _ := c["operatingSystem"].(map[string]any)
			userLoc, _ := c["userAndLocation"].(map[string]any)
			lookup[mgmtID] = mdmDeviceMeta{
				name:      strVal(general, "name"),
				serial:    strVal(hardware, "serialNumber"),
				osVersion: strVal(osInfo, "version"),
				username:  strVal(userLoc, "username"),
			}
		}
	}

	// Mobile devices: flat list for serial/name, detail for OS version
	// Build serial lookup from flat list first
	mobileSerials := make(map[string]string) // managementId -> serial
	mobileFlat, err := FetchAllPaginated(ctx, client, "/v2/mobile-devices", 2000)
	if err == nil {
		for _, m := range mobileFlat {
			mgmtID := strVal(m, "managementId")
			if mgmtID != "" {
				mobileSerials[mgmtID] = strVal(m, "serialNumber")
			}
		}
	}

	// Detail endpoint for OS version and display name
	mobiles, err := FetchAllPaginated(ctx, client, "/v2/mobile-devices/detail?section=GENERAL", 2000)
	if err == nil {
		for _, m := range mobiles {
			general, _ := m["general"].(map[string]any)
			mgmtID := strVal(general, "managementId")
			if mgmtID == "" {
				continue
			}
			lookup[mgmtID] = mdmDeviceMeta{
				name:      strVal(general, "displayName"),
				serial:    mobileSerials[mgmtID],
				osVersion: strVal(general, "osVersion"),
			}
		}
	}

	return lookup
}

// aggregateByDevice groups commands by device management ID and returns
// devices with count >= threshold, sorted by count descending.
func aggregateByDevice(results []mdmCommandResult, threshold int, _ map[string]mdmDeviceMeta) []mdmDeviceSummary {
	type acc struct {
		count      int
		deviceType string
	}
	byDevice := make(map[string]*acc)
	for _, r := range results {
		if r.ClientMgmtID == "" {
			continue
		}
		a, ok := byDevice[r.ClientMgmtID]
		if !ok {
			a = &acc{}
			byDevice[r.ClientMgmtID] = a
		}
		a.count++
		if r.ClientType != "" {
			a.deviceType = r.ClientType
		}
	}

	var summaries []mdmDeviceSummary
	for mgmtID, a := range byDevice {
		if a.count < threshold {
			continue
		}
		dt := normalizeDeviceType(a.deviceType)
		summaries = append(summaries, mdmDeviceSummary{
			ManagementID: mgmtID,
			DeviceType:   dt,
			Count:        a.count,
		})
	}

	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].Count > summaries[j].Count
	})

	return summaries
}

// enrichDeviceSummaries populates device details from the inventory lookup.
func enrichDeviceSummaries(summaries []mdmDeviceSummary, lookup map[string]mdmDeviceMeta) []mdmDeviceSummary {
	for i, s := range summaries {
		meta := lookup[s.ManagementID]
		summaries[i].Name = meta.name
		summaries[i].Serial = meta.serial
		summaries[i].OSVersion = meta.osVersion
		summaries[i].Username = meta.username
	}
	return summaries
}

func aggregateMDMFailures(results []mdmCommandResult, nameLookup map[string]string, label string, days int) *mdmHealthReport {
	type accumulator struct {
		errors      int
		devices     map[string]bool
		clientTypes map[string]bool // track COMPUTER vs MOBILE_DEVICE
		lastError   time.Time
		errorCounts map[string]int // error description -> count
	}

	// Group by profile identifier (or profileId as fallback)
	byName := make(map[string]*accumulator)
	nameToID := make(map[string]string)

	for _, r := range results {
		key := r.ProfileIdentifier
		if key == "" {
			key = r.ProfileID
		}
		if key == "" {
			key = r.CommandType // fallback for apps
		}

		acc, ok := byName[key]
		if !ok {
			acc = &accumulator{
				devices:     make(map[string]bool),
				clientTypes: make(map[string]bool),
				errorCounts: make(map[string]int),
			}
			byName[key] = acc
		}
		if r.ProfileID != "" {
			nameToID[key] = r.ProfileID
		}

		acc.errors++
		if r.ClientMgmtID != "" {
			acc.devices[r.ClientMgmtID] = true
		}
		if r.ClientType != "" {
			acc.clientTypes[r.ClientType] = true
		}
		if r.DateCompleted.After(acc.lastError) {
			acc.lastError = r.DateCompleted
		}
		if r.ErrorDescription != "" {
			acc.errorCounts[r.ErrorDescription]++
		}
	}

	var failures []mdmFailureSummary
	uniqueDevices := make(map[string]bool)

	for name, acc := range byName {
		lastErr := ""
		if !acc.lastError.IsZero() {
			lastErr = acc.lastError.Format("2006-01-02")
		}

		// Find most common error
		topError := ""
		topCount := 0
		for desc, count := range acc.errorCounts {
			if count > topCount {
				topError = desc
				topCount = count
			}
		}

		displayName := name
		profileID := nameToID[name]
		if profileID == "" {
			profileID = name
		}
		if nameLookup != nil {
			if resolved, ok := nameLookup[profileID]; ok && resolved != "" {
				displayName = resolved
			}
		}

		// Resolve device type: if only one type, use it; if mixed, say "Mixed"
		deviceType := "Unknown"
		switch len(acc.clientTypes) {
		case 1:
			for ct := range acc.clientTypes {
				deviceType = normalizeDeviceType(ct)
			}
		case 0:
			// no client type info
		default:
			deviceType = "Mixed"
		}

		failures = append(failures, mdmFailureSummary{
			Name:       displayName,
			ID:         profileID,
			DeviceType: deviceType,
			Errors:     acc.errors,
			Devices:    len(acc.devices),
			LastError:  lastErr,
			TopError:   topError,
		})

		for d := range acc.devices {
			uniqueDevices[d] = true
		}
	}

	sort.Slice(failures, func(i, j int) bool {
		if failures[i].DeviceType != failures[j].DeviceType {
			return failures[i].DeviceType < failures[j].DeviceType
		}
		return failures[i].Errors > failures[j].Errors
	})

	return &mdmHealthReport{
		Summary: mdmHealthSummary{
			TotalErrors:    len(results),
			UniqueProfiles: len(failures),
			UniqueDevices:  len(uniqueDevices),
			Days:           days,
			CommandType:    label,
		},
		Failures: failures,
	}
}

// ---------------------------------------------------------------------------
// Output
// ---------------------------------------------------------------------------

func printMDMHealthReport(cliCtx *registry.CLIContext, report *mdmHealthReport, label string) error {
	out := writerFor(cliCtx)

	if outputFmt == "json" || outputFmt == "yaml" {
		combined := map[string]any{
			"summary":         mdmSummaryToMap(report.Summary),
			"failures":        mdmFailuresToRows(report.Failures),
			"device_failures": mdmDevicesToRows(report.DeviceFailures),
			"device_pending":  mdmDevicesToRows(report.DevicePending),
		}
		return printRows(cliCtx, []map[string]any{combined})
	}

	// Table: summary
	if err := printSection(cliCtx, fmt.Sprintf("── %s Deployment Health Summary ──\n", capitalise(label)), []map[string]any{mdmSummaryToMap(report.Summary)}); err != nil {
		return err
	}

	// Table: failures by profile/app
	if len(report.Failures) > 0 {
		if err := printSection(cliCtx, fmt.Sprintf("\n── Failed %ss (last %d days) ──\n", capitalise(label), report.Summary.Days), mdmFailuresToRows(report.Failures)); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(os.Stderr, "\nNo %s failures found in the last %d days.\n", label, report.Summary.Days)
	}

	// Table: devices with high failure count
	if len(report.DeviceFailures) > 0 {
		_, _ = fmt.Fprint(out, "\n── Devices With High Failure Count (>5 errors) ──\n")
		if err := printRows(cliCtx, mdmDevicesToRows(report.DeviceFailures)); err != nil {
			return err
		}
	}

	// Table: devices with high pending count
	if len(report.DevicePending) > 0 {
		_, _ = fmt.Fprint(out, "\n── Devices With Command Backlog (>10 pending) ──\n")
		if err := printRows(cliCtx, mdmDevicesToRows(report.DevicePending)); err != nil {
			return err
		}
	}

	return nil
}

func mdmSummaryToMap(s mdmHealthSummary) map[string]any {
	m := map[string]any{
		"total_errors":                  s.TotalErrors,
		"unique_" + s.CommandType + "s": s.UniqueProfiles,
		"unique_devices":                s.UniqueDevices,
		"days":                          s.Days,
	}
	if s.DevicesHighFailure > 0 {
		m["devices_high_failure"] = s.DevicesHighFailure
	}
	if s.DevicesHighPending > 0 {
		m["devices_high_pending"] = s.DevicesHighPending
	}
	return m
}

func mdmFailuresToRows(failures []mdmFailureSummary) []map[string]any {
	rows := make([]map[string]any, len(failures))
	for i, f := range failures {
		rows[i] = map[string]any{
			"device_type": f.DeviceType,
			"name":        f.Name,
			"id":          f.ID,
			"errors":      f.Errors,
			"devices":     f.Devices,
			"last_error":  f.LastError,
			"top_error":   f.TopError,
		}
	}
	return rows
}

func mdmDevicesToRows(devices []mdmDeviceSummary) []map[string]any {
	rows := make([]map[string]any, len(devices))
	for i, d := range devices {
		rows[i] = map[string]any{
			"name":        d.Name,
			"serial":      d.Serial,
			"device_type": d.DeviceType,
			"os_version":  d.OSVersion,
			"username":    d.Username,
			"count":       d.Count,
		}
	}
	return rows
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func splitCommaSeparated(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

func capitalise(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
