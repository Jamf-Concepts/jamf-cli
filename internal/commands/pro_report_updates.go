// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/output"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// Error-class statuses for managed software updates.
var updateErrorStatuses = map[string]bool{
	"ERROR":                          true,
	"DOWNLOAD_FAILED":                true,
	"INSTALL_FAILED":                 true,
	"DOWNLOAD_INSUFFICIENT_SPACE":    true,
	"DOWNLOAD_INSUFFICIENT_POWER":    true,
	"DOWNLOAD_INSUFFICIENT_NETWORK":  true,
	"INSTALL_INSUFFICIENT_SPACE":     true,
	"INSTALL_INSUFFICIENT_POWER":     true,
	"INSTALL_PHONE_CALL_IN_PROGRESS": true,
	"DOWNLOAD_REQUIRES_COMPUTER":     true,
}

// updateDeviceMeta holds inventory data keyed by Jamf Pro device ID.
type updateDeviceMeta struct {
	name, serial, osVersion, username, deviceType string
}

func newReportUpdateStatusCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-status",
		Short: "Managed software update deployment status",
		Long: `Reports on managed software update statuses across the fleet.

Queries /v1/managed-software-updates/update-statuses for all active
update statuses and aggregates by status type. Flags devices in error
states with device details (name, serial, OS version, username).

Examples:
  # Update deployment status
  jamf-cli pro report update-status

  # JSON output for scripting
  jamf-cli pro report update-status -o json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("output") {
				outputFmt = "table"
			}
			return runReportUpdateStatus(cmd.Context(), cliCtx.Client)
		},
	}

	return cmd
}

func runReportUpdateStatus(ctx context.Context, client registry.HTTPClient) error {
	// Fetch both update statuses and update plans
	fmt.Fprintf(os.Stderr, "Fetching managed software update statuses...\n")

	results, err := FetchAllPaginated(ctx, client, "/v1/managed-software-updates/update-statuses", 2000)
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: failed to fetch update statuses: %v\n", err)
	}

	fmt.Fprintf(os.Stderr, "Fetching managed software update plans...\n")
	plans, plansErr := FetchAllPaginated(ctx, client, "/v1/managed-software-updates/plans", 2000)
	if plansErr != nil {
		fmt.Fprintf(os.Stderr, "WARNING: failed to fetch update plans: %v\n", plansErr)
	}

	if len(results) == 0 && len(plans) == 0 {
		fmt.Fprintln(os.Stderr, "No managed software update data found.")
		return nil
	}

	// Aggregate by status and collect error devices
	statusCounts := make(map[string]int)
	type errorDevice struct {
		deviceID   string
		deviceType string // COMPUTER, MOBILE_DEVICE, APPLE_TV
		status     string
		productKey string
		updated    string
	}
	var errors []errorDevice

	for _, r := range results {
		status := strVal(r, "status")
		if status == "" {
			status = "UNKNOWN"
		}
		statusCounts[status]++

		if updateErrorStatuses[status] {
			device, _ := r["device"].(map[string]any)
			deviceID := ""
			deviceType := ""
			if device != nil {
				deviceID = strVal(device, "deviceId")
				deviceType = strVal(device, "objectType")
			}
			errors = append(errors, errorDevice{
				deviceID:   deviceID,
				deviceType: deviceType,
				status:     status,
				productKey: strVal(r, "productKey"),
				updated:    strVal(r, "updated"),
			})
		}
	}

	// Build summary rows sorted by count
	type statusRow struct {
		status string
		count  int
	}
	var summaryRows []statusRow
	for s, c := range statusCounts {
		summaryRows = append(summaryRows, statusRow{s, c})
	}
	sort.Slice(summaryRows, func(i, j int) bool {
		return summaryRows[i].count > summaryRows[j].count
	})

	summaryMaps := make([]map[string]any, len(summaryRows))
	for i, r := range summaryRows {
		summaryMaps[i] = map[string]any{
			"status": r.status,
			"count":  r.count,
		}
	}

	// Enrich error devices with inventory data if there are any
	var errorRows []map[string]any
	if len(errors) > 0 {
		lookup := fetchUpdateDeviceLookup(ctx, client)
		for _, e := range errors {
			meta := lookup[e.deviceID]
			dt := meta.deviceType
			if dt == "" {
				switch e.deviceType {
				case "COMPUTER":
					dt = "Computer"
				case "MOBILE_DEVICE":
					dt = "Mobile"
				case "APPLE_TV":
					dt = "Apple TV"
				default:
					dt = e.deviceType
				}
			}
			errorRows = append(errorRows, map[string]any{
				"name":        meta.name,
				"serial":      meta.serial,
				"device_type": dt,
				"os_version":  meta.osVersion,
				"username":    meta.username,
				"status":      e.status,
				"product_key": e.productKey,
				"updated":     e.updated,
			})
		}
	}

	// Aggregate plans by state
	planStateCounts := make(map[string]int)
	type failedPlan struct {
		planUUID   string
		deviceID   string
		deviceType string
		state      string
		errors     string
		action     string
		version    string
	}
	var failedPlans []failedPlan

	for _, p := range plans {
		status, _ := p["status"].(map[string]any)
		state := "UNKNOWN"
		if status != nil {
			state = strVal(status, "state")
			if state == "" {
				state = "UNKNOWN"
			}
		}
		planStateCounts[state]++

		if state == "PlanFailed" {
			device, _ := p["device"].(map[string]any)
			deviceID := ""
			deviceType := ""
			if device != nil {
				deviceID = strVal(device, "deviceId")
				deviceType = strVal(device, "objectType")
			}
			var errReasons string
			if status != nil {
				if reasons, ok := status["errorReasons"].([]any); ok {
					var parts []string
					for _, r := range reasons {
						if s, ok := r.(string); ok {
							parts = append(parts, s)
						}
					}
					errReasons = strings.Join(parts, ", ")
				}
			}
			failedPlans = append(failedPlans, failedPlan{
				planUUID:   strVal(p, "planUuid"),
				deviceID:   deviceID,
				deviceType: deviceType,
				state:      state,
				errors:     errReasons,
				action:     strVal(p, "updateAction"),
				version:    strVal(p, "versionType"),
			})
		}
	}

	// Build plan state summary
	type planStateRow struct {
		state string
		count int
	}
	var planStateRows []planStateRow
	for s, c := range planStateCounts {
		planStateRows = append(planStateRows, planStateRow{s, c})
	}
	sort.Slice(planStateRows, func(i, j int) bool {
		return planStateRows[i].count > planStateRows[j].count
	})
	planStateMaps := make([]map[string]any, len(planStateRows))
	for i, r := range planStateRows {
		planStateMaps[i] = map[string]any{"state": r.state, "count": r.count}
	}

	// Fetch last event for each failed plan to add context
	lastEvents := make(map[string]string) // planUUID -> last event type
	for _, fp := range failedPlans {
		if fp.planUUID == "" {
			continue
		}
		eventsData, err := fetchJSON(ctx, client, fmt.Sprintf("/v1/managed-software-updates/plans/%s/events", fp.planUUID))
		if err != nil {
			continue
		}
		eventsJSON, _ := eventsData["events"].(string)
		if eventsJSON == "" {
			continue
		}
		lastEvent := extractLastEventType(eventsJSON)
		if lastEvent != "" {
			lastEvents[fp.planUUID] = lastEvent
		}
	}

	// Enrich failed plans with device details
	var failedPlanRows []map[string]any
	if len(failedPlans) > 0 {
		lookup := fetchUpdateDeviceLookup(ctx, client)
		for _, fp := range failedPlans {
			meta := lookup[fp.deviceID]
			dt := meta.deviceType
			if dt == "" {
				switch fp.deviceType {
				case "COMPUTER":
					dt = "Computer"
				case "MOBILE_DEVICE":
					dt = "Mobile"
				default:
					dt = fp.deviceType
				}
			}
			lastEvt := lastEvents[fp.planUUID]
			failedPlanRows = append(failedPlanRows, map[string]any{
				"name":        meta.name,
				"serial":      meta.serial,
				"device_type": dt,
				"os_version":  meta.osVersion,
				"username":    meta.username,
				"action":      fp.action,
				"version":     fp.version,
				"error":       fp.errors,
				"last_event":  lastEvt,
			})
		}
	}

	formatter := output.New(outputFmt, noColor, wide)

	if outputFmt == "json" || outputFmt == "yaml" {
		combined := map[string]any{
			"total":              len(results),
			"status_summary":     summaryMaps,
			"error_devices":      errorRows,
			"plan_total":         len(plans),
			"plan_state_summary": planStateMaps,
			"failed_plans":       failedPlanRows,
		}
		return formatter.Print([]map[string]any{combined})
	}

	// Table: update status summary
	if len(summaryMaps) > 0 {
		fmt.Printf("── Managed Software Update Status (%d total) ──\n", len(results))
		if err := formatter.Print(summaryMaps); err != nil {
			return err
		}
	}

	// Table: error devices
	if len(errorRows) > 0 {
		fmt.Printf("\n── Devices With Update Errors (%d) ──\n", len(errorRows))
		if err := formatter.Print(errorRows); err != nil {
			return err
		}
	}

	// Table: plan state summary
	if len(planStateMaps) > 0 {
		fmt.Printf("\n── Update Plan Status (%d total) ──\n", len(plans))
		if err := formatter.Print(planStateMaps); err != nil {
			return err
		}
	}

	// Table: failed plans
	if len(failedPlanRows) > 0 {
		fmt.Printf("\n── Failed Update Plans (%d) ──\n", len(failedPlanRows))
		if err := formatter.Print(failedPlanRows); err != nil {
			return err
		}
	}

	return nil
}

// fetchUpdateDeviceLookup builds a Jamf Pro device ID → meta lookup from
// computer and mobile device inventory.
func fetchUpdateDeviceLookup(ctx context.Context, client registry.HTTPClient) map[string]updateDeviceMeta {
	lookup := make(map[string]updateDeviceMeta)

	// Computers (sectioned format for OS version and username)
	computers, err := FetchAllPaginated(ctx, client,
		"/v3/computers-inventory?section=GENERAL&section=HARDWARE&section=OPERATING_SYSTEM&section=USER_AND_LOCATION", 2000)
	if err == nil {
		for _, c := range computers {
			id := extractID(c)
			if id == "" {
				continue
			}
			general, _ := c["general"].(map[string]any)
			hardware, _ := c["hardware"].(map[string]any)
			osInfo, _ := c["operatingSystem"].(map[string]any)
			userLoc, _ := c["userAndLocation"].(map[string]any)
			lookup[id] = updateDeviceMeta{
				name:       strVal(general, "name"),
				serial:     strVal(hardware, "serialNumber"),
				osVersion:  strVal(osInfo, "version"),
				username:   strVal(userLoc, "username"),
				deviceType: "Computer",
			}
		}
	}

	// Mobile devices (detail for OS version)
	mobileSerials := make(map[string]string)
	mobileFlat, err := FetchAllPaginated(ctx, client, "/v2/mobile-devices", 2000)
	if err == nil {
		for _, m := range mobileFlat {
			id := extractID(m)
			if id != "" {
				mobileSerials[id] = strVal(m, "serialNumber")
			}
		}
	}

	mobileDetail, err := FetchAllPaginated(ctx, client, "/v2/mobile-devices/detail?section=GENERAL", 2000)
	if err == nil {
		for _, m := range mobileDetail {
			mobileID := extractField(m, "mobileDeviceId")
			if mobileID == "" {
				continue
			}
			general, _ := m["general"].(map[string]any)
			lookup[mobileID] = updateDeviceMeta{
				name:       strVal(general, "displayName"),
				serial:     mobileSerials[mobileID],
				osVersion:  strVal(general, "osVersion"),
				deviceType: "Mobile",
			}
		}
	}

	return lookup
}

// extractLastEventType parses the events JSON string (which is a JSON object
// containing an "events" array) and returns the "type" field of the last event.
func extractLastEventType(eventsJSON string) string {
	var wrapper struct {
		Events []struct {
			Type string `json:"type"`
		} `json:"events"`
	}
	if err := json.Unmarshal([]byte(eventsJSON), &wrapper); err != nil {
		return ""
	}
	if len(wrapper.Events) == 0 {
		return ""
	}
	return strings.TrimPrefix(wrapper.Events[len(wrapper.Events)-1].Type, ".")
}
