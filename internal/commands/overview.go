package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/jamf/jamfpro-cli/internal/commands/generated"
	"github.com/jamf/jamfpro-cli/internal/output"
)

type overviewSection struct {
	Name  string
	Items []overviewItem
}

type overviewItem struct {
	Resource  string
	Value     string
	ColorHint string // optional: "red", "yellow" for expiration coloring
}

type fetchResult struct {
	Key   string
	Value string
	Err   error
}

// fetchJSON performs a GET request and returns the parsed JSON object.
func fetchJSON(ctx context.Context, client generated.HTTPClient, path string) (map[string]interface{}, error) {
	resp, err := client.Do(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// fetchPaginatedCount appends ?page-size=1 to the path and returns the totalCount.
func fetchPaginatedCount(ctx context.Context, client generated.HTTPClient, path string) (string, error) {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	data, err := fetchJSON(ctx, client, path+sep+"page-size=1")
	if err != nil {
		return "", err
	}

	if tc, ok := data["totalCount"]; ok {
		return formatCount(tc), nil
	}
	return "0", nil
}

// fetchArrayCount performs a GET and returns the length of the JSON array.
func fetchArrayCount(ctx context.Context, client generated.HTTPClient, path string) (string, error) {
	resp, err := client.Do(ctx, "GET", path, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var arr []json.RawMessage
	if err := json.Unmarshal(body, &arr); err != nil {
		return "", err
	}
	return formatCount(float64(len(arr))), nil
}

// fetchClassicCount performs a GET on a Classic API list endpoint and returns the
// count of items. Classic endpoints wrap arrays as {"key": [...]}, so the caller
// provides the wrapper key (e.g. "policies", "packages").
func fetchClassicCount(ctx context.Context, client generated.HTTPClient, path, wrapperKey string) (string, error) {
	resp, err := client.Do(ctx, "GET", path, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(body, &wrapper); err != nil {
		return "", err
	}
	inner, ok := wrapper[wrapperKey]
	if !ok {
		return "0", nil
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(inner, &arr); err != nil {
		return "0", nil
	}
	return formatCount(float64(len(arr))), nil
}

// fetchClassicNestedSize performs a GET on a Classic API endpoint that returns
// a nested structure like {"computer_commands": {"computer_command": [...], "size": N}}.
// It extracts the "size" field from the inner object.
func fetchClassicNestedSize(ctx context.Context, client generated.HTTPClient, path, outerKey string) (string, error) {
	data, err := fetchJSON(ctx, client, path)
	if err != nil {
		return "", err
	}
	inner, ok := data[outerKey].(map[string]interface{})
	if !ok {
		return "0", nil
	}
	if size, ok := inner["size"]; ok {
		return formatCount(size), nil
	}
	return "0", nil
}

// formatCount converts a numeric value to a comma-formatted string.
func formatCount(v interface{}) string {
	var n int64
	switch val := v.(type) {
	case float64:
		n = int64(val)
	case int:
		n = int64(val)
	case int64:
		n = val
	default:
		return fmt.Sprintf("%v", v)
	}
	return commaFormat(n)
}

// commaFormat inserts commas into an integer for display.
func commaFormat(n int64) string {
	s := strconv.FormatInt(n, 10)
	if len(s) <= 3 {
		return s
	}
	var result strings.Builder
	offset := len(s) % 3
	if offset > 0 {
		result.WriteString(s[:offset])
	}
	for i := offset; i < len(s); i += 3 {
		if result.Len() > 0 {
			result.WriteByte(',')
		}
		result.WriteString(s[i : i+3])
	}
	return result.String()
}

// enabledDisabled converts a boolean interface value to "enabled" or "disabled".
func enabledDisabled(v interface{}) string {
	if b, ok := v.(bool); ok && b {
		return "enabled"
	}
	return "disabled"
}

// formatEpochDate converts Unix epoch seconds to a human-readable date string.
func formatEpochDate(epoch float64) string {
	t := time.Unix(int64(epoch), 0).UTC()
	return t.Format("Jan 02, 2006")
}

// formatExpirationDate formats a date string and adds proximity context.
// Returns the formatted date and a color hint: "red", "yellow", or "".
func formatExpirationDate(dateStr string, now time.Time) (string, string) {
	t, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return dateStr, ""
	}
	days := int(t.Sub(now).Hours() / 24)
	formatted := t.Format("Jan 02, 2006")
	switch {
	case days < 0:
		return formatted + " (expired)", "red"
	case days < 30:
		return fmt.Sprintf("%s (%d days)", formatted, days), "red"
	case days < 90:
		return fmt.Sprintf("%s (%d days)", formatted, days), "yellow"
	default:
		return formatted, ""
	}
}

// formatEpochExpiration formats an epoch timestamp and adds proximity context.
func formatEpochExpiration(epoch float64, now time.Time) (string, string) {
	t := time.Unix(int64(epoch), 0).UTC()
	days := int(t.Sub(now).Hours() / 24)
	formatted := t.Format("Jan 02, 2006")
	switch {
	case days < 0:
		return formatted + " (expired)", "red"
	case days < 30:
		return fmt.Sprintf("%s (%d days)", formatted, days), "red"
	case days < 90:
		return fmt.Sprintf("%s (%d days)", formatted, days), "yellow"
	default:
		return formatted, ""
	}
}

// runOverview executes all API calls in parallel and prints the overview.
func runOverview(ctx context.Context, cliCtx *generated.CLIContext) ([]overviewSection, error) {
	client := cliCtx.Client

	var mu sync.Mutex
	results := make(map[string]string)
	colorHints := make(map[string]string)
	var wg sync.WaitGroup

	// Helper to send a single fetch result.
	send := func(key, value string, err error) {
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			results[key] = "N/A"
		} else {
			results[key] = value
		}
	}

	// sendWithColor sends a result with an optional color hint for expiration dates.
	sendWithColor := func(key, value, color string, err error) {
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			results[key] = "N/A"
		} else {
			results[key] = value
			if color != "" {
				colorHints[key] = color
			}
		}
	}

	// 1. Instance Info: version
	wg.Add(1)
	go func() {
		defer wg.Done()
		data, err := fetchJSON(ctx, client, "/v1/jamf-pro-version")
		if err != nil {
			send("version", "", err)
			return
		}
		if v, ok := data["version"].(string); ok {
			send("version", v, nil)
		} else {
			send("version", "N/A", nil)
		}
	}()

	// 1. Instance Info: health
	wg.Add(1)
	go func() {
		defer wg.Done()
		h := checkHealth(serverURL)
		send("health", h.Status, nil)
		if h.Healthy {
			send("health_ok", "true", nil)
		} else {
			send("health_ok", "false", nil)
		}
	}()

	// 1. Instance Info: SLASA
	wg.Add(1)
	go func() {
		defer wg.Done()
		data, err := fetchJSON(ctx, client, "/v1/slasa")
		if err != nil {
			send("slasa", "", err)
			return
		}
		if s, ok := data["slasaAcceptanceStatus"].(string); ok {
			send("slasa", s, nil)
		} else {
			send("slasa", "N/A", nil)
		}
	}()

	// 2. Jamf Pro Features (single call)
	wg.Add(1)
	go func() {
		defer wg.Done()
		data, err := fetchJSON(ctx, client, "/v2/jamf-pro-information")
		if err != nil {
			for _, k := range []string{"vpp", "dep", "cloud_deploy", "patch", "sso", "smtp"} {
				send(k, "", err)
			}
			return
		}
		send("vpp", enabledDisabled(data["vppTokenEnabled"]), nil)
		send("dep", enabledDisabled(data["depAccountEnabled"]), nil)
		send("cloud_deploy", enabledDisabled(data["cloudDeploymentsEnabled"]), nil)
		send("patch", enabledDisabled(data["patchEnabled"]), nil)
		send("sso", enabledDisabled(data["ssoSamlEnabled"]), nil)
		send("smtp", enabledDisabled(data["smtpEnabled"]), nil)
	}()

	// 3. CSA Scopes
	wg.Add(1)
	go func() {
		defer wg.Done()
		resp, err := client.Do(ctx, "GET", "/v1/csa/token", nil)
		if err != nil {
			send("csa_scopes", "", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusNotFound {
			send("csa_scopes", "Not configured", nil)
			return
		}
		if resp.StatusCode != http.StatusOK {
			send("csa_scopes", "", fmt.Errorf("HTTP %d", resp.StatusCode))
			return
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			send("csa_scopes", "", err)
			return
		}

		var data map[string]interface{}
		if err := json.Unmarshal(body, &data); err != nil {
			send("csa_scopes", "", err)
			return
		}

		if scopes, ok := data["scopes"].([]interface{}); ok {
			send("csa_scopes", fmt.Sprintf("%d scopes", len(scopes)), nil)
		} else {
			send("csa_scopes", "N/A", nil)
		}
	}()

	// 4. Client Check-In (single call)
	wg.Add(1)
	go func() {
		defer wg.Done()
		data, err := fetchJSON(ctx, client, "/v3/check-in")
		if err != nil {
			for _, k := range []string{"checkin_freq", "create_hooks", "startup_script", "local_config"} {
				send(k, "", err)
			}
			return
		}
		if freq, ok := data["checkInFrequency"].(float64); ok {
			send("checkin_freq", fmt.Sprintf("%d min", int(freq)), nil)
		} else {
			send("checkin_freq", "N/A", nil)
		}
		send("create_hooks", enabledDisabled(data["createHooks"]), nil)
		send("startup_script", enabledDisabled(data["createStartupScript"]), nil)
		send("local_config", enabledDisabled(data["enableLocalConfigurationProfiles"]), nil)
	}()

	// Enrollment Settings (single call → 5 booleans)
	wg.Add(1)
	go func() {
		defer wg.Done()
		data, err := fetchJSON(ctx, client, "/v4/enrollment")
		if err != nil {
			for _, k := range []string{"enroll_macos_enterprise", "enroll_ios_enterprise", "enroll_ios_personal", "enroll_adue", "enroll_adde_macos"} {
				send(k, "", err)
			}
			return
		}
		send("enroll_macos_enterprise", enabledDisabled(data["macOsEnterpriseEnrollmentEnabled"]), nil)
		send("enroll_ios_enterprise", enabledDisabled(data["iosEnterpriseEnrollmentEnabled"]), nil)
		send("enroll_ios_personal", enabledDisabled(data["iosPersonalEnrollmentEnabled"]), nil)
		send("enroll_adue", enabledDisabled(data["accountDrivenUserEnrollmentEnabled"]), nil)
		send("enroll_adde_macos", enabledDisabled(data["accountDrivenDeviceMacosEnrollmentEnabled"]), nil)
	}()

	// Self Service (single call → nested fields)
	wg.Add(1)
	go func() {
		defer wg.Done()
		data, err := fetchJSON(ctx, client, "/v1/self-service/settings")
		if err != nil {
			for _, k := range []string{"ss_install_auto", "ss_login_required", "ss_notifications"} {
				send(k, "", err)
			}
			return
		}
		if install, ok := data["installSettings"].(map[string]interface{}); ok {
			send("ss_install_auto", enabledDisabled(install["installAutomatically"]), nil)
		} else {
			send("ss_install_auto", "N/A", nil)
		}
		if login, ok := data["loginSettings"].(map[string]interface{}); ok {
			if level, ok := login["userLoginLevel"].(string); ok {
				send("ss_login_required", level, nil)
			} else {
				send("ss_login_required", "N/A", nil)
			}
		} else {
			send("ss_login_required", "N/A", nil)
		}
		if config, ok := data["configurationSettings"].(map[string]interface{}); ok {
			send("ss_notifications", enabledDisabled(config["notificationsEnabled"]), nil)
		} else {
			send("ss_notifications", "N/A", nil)
		}
	}()

	// LAPS (single call → 2 booleans)
	wg.Add(1)
	go func() {
		defer wg.Done()
		data, err := fetchJSON(ctx, client, "/v2/local-admin-password/settings")
		if err != nil {
			for _, k := range []string{"laps_auto_deploy", "laps_auto_rotate"} {
				send(k, "", err)
			}
			return
		}
		send("laps_auto_deploy", enabledDisabled(data["autoDeployEnabled"]), nil)
		send("laps_auto_rotate", enabledDisabled(data["autoRotateEnabled"]), nil)
	}()

	// MDM Profile Renewal (single call → 2 booleans + 2 values)
	wg.Add(1)
	go func() {
		defer wg.Done()
		data, err := fetchJSON(ctx, client, "/v1/device-communication-settings")
		if err != nil {
			for _, k := range []string{"mdm_renew_computer", "mdm_renew_mobile", "mdm_cert_computer_days", "mdm_cert_mobile_days"} {
				send(k, "", err)
			}
			return
		}
		send("mdm_renew_computer", enabledDisabled(data["autoRenewComputerMdmProfileWhenDeviceIdentityCertExpiring"]), nil)
		send("mdm_renew_mobile", enabledDisabled(data["autoRenewMobileDeviceMdmProfileWhenDeviceIdentityCertExpiring"]), nil)
		if days, ok := data["mdmProfileComputerExpirationLimitInDays"].(float64); ok {
			send("mdm_cert_computer_days", fmt.Sprintf("%d days", int(days)), nil)
		} else {
			send("mdm_cert_computer_days", "N/A", nil)
		}
		if days, ok := data["mdmProfileMobileDeviceExpirationLimitInDays"].(float64); ok {
			send("mdm_cert_mobile_days", fmt.Sprintf("%d days", int(days)), nil)
		} else {
			send("mdm_cert_mobile_days", "N/A", nil)
		}
	}()

	// Certificate Authority (single call → expiration date)
	wg.Add(1)
	go func() {
		defer wg.Done()
		data, err := fetchJSON(ctx, client, "/v1/pki/certificate-authority/active")
		if err != nil {
			send("ca_expires", "", err)
			return
		}
		if notAfter, ok := data["notAfter"].(float64); ok {
			formatted, color := formatEpochExpiration(notAfter, time.Now())
			sendWithColor("ca_expires", formatted, color, nil)
		} else {
			send("ca_expires", "N/A", nil)
		}
	}()

	// 5. Inventory Summary (single call)
	wg.Add(1)
	go func() {
		defer wg.Done()
		data, err := fetchJSON(ctx, client, "/v1/inventory-information")
		if err != nil {
			for _, k := range []string{"managed_computers", "unmanaged_computers", "managed_devices", "unmanaged_devices"} {
				send(k, "", err)
			}
			return
		}
		send("managed_computers", formatCount(data["managedComputers"]), nil)
		send("unmanaged_computers", formatCount(data["unmanagedComputers"]), nil)
		send("managed_devices", formatCount(data["managedDevices"]), nil)
		send("unmanaged_devices", formatCount(data["unmanagedDevices"]), nil)
	}()

	// 6. Organizational Structure
	wg.Add(1)
	go func() {
		defer wg.Done()
		v, err := fetchArrayCount(ctx, client, "/v1/sites")
		send("sites", v, err)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		v, err := fetchPaginatedCount(ctx, client, "/v1/buildings")
		send("buildings", v, err)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		v, err := fetchPaginatedCount(ctx, client, "/v1/departments")
		send("departments", v, err)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		v, err := fetchPaginatedCount(ctx, client, "/v1/categories")
		send("categories", v, err)
	}()

	// 7. Device Groups
	wg.Add(1)
	go func() {
		defer wg.Done()
		v, err := fetchArrayCount(ctx, client, "/v1/computer-groups")
		send("computer_groups", v, err)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		v, err := fetchPaginatedCount(ctx, client, "/v1/mobile-device-groups/smart-groups")
		send("md_smart_groups", v, err)
	}()

	// 8. Configuration & Deployment
	wg.Add(1)
	go func() {
		defer wg.Done()
		v, err := fetchPaginatedCount(ctx, client, "/v1/scripts")
		send("scripts", v, err)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		v, err := fetchPaginatedCount(ctx, client, "/v1/ebooks")
		send("ebooks", v, err)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		v, err := fetchArrayCount(ctx, client, "/v1/jcds/files")
		send("jcds_files", v, err)
	}()

	// 9. Enrollment (fetch full results for count + nearest token expiration)
	wg.Add(1)
	go func() {
		defer wg.Done()
		data, err := fetchJSON(ctx, client, "/v1/device-enrollments")
		if err != nil {
			send("dep_instances", "", err)
			send("dep_token_expires", "", err)
			return
		}
		if tc, ok := data["totalCount"]; ok {
			send("dep_instances", formatCount(tc), nil)
		} else {
			send("dep_instances", "0", nil)
		}

		// Find earliest tokenExpirationDate from results
		results, _ := data["results"].([]interface{})
		var earliest string
		for _, r := range results {
			item, ok := r.(map[string]interface{})
			if !ok {
				continue
			}
			dateStr, ok := item["tokenExpirationDate"].(string)
			if !ok || dateStr == "" {
				continue
			}
			if earliest == "" || dateStr < earliest {
				earliest = dateStr
			}
		}
		if earliest != "" {
			formatted, color := formatExpirationDate(earliest, time.Now())
			sendWithColor("dep_token_expires", formatted, color, nil)
		} else {
			send("dep_token_expires", "None configured", nil)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		v, err := fetchPaginatedCount(ctx, client, "/v3/computer-prestages")
		send("computer_prestages", v, err)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		v, err := fetchPaginatedCount(ctx, client, "/v3/mobile-device-prestages")
		send("md_prestages", v, err)
	}()

	// 10. Users & Access
	wg.Add(1)
	go func() {
		defer wg.Done()
		v, err := fetchArrayCount(ctx, client, "/v1/static-user-groups")
		send("static_user_groups", v, err)
	}()

	// 11. Notifications/Alerts
	wg.Add(1)
	go func() {
		defer wg.Done()
		resp, err := client.Do(ctx, "GET", "/v1/notifications", nil)
		if err != nil {
			send("alerts", "", err)
			send("alert_detail", "", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			send("alerts", "", fmt.Errorf("HTTP %d", resp.StatusCode))
			return
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			send("alerts", "", err)
			return
		}

		var alerts []map[string]interface{}
		if err := json.Unmarshal(body, &alerts); err != nil {
			send("alerts", "", err)
			return
		}

		count := len(alerts)
		if count == 0 {
			send("alerts", "None", nil)
			sendWithColor("alert_detail", "", "", nil)
			return
		}

		sendWithColor("alerts", fmt.Sprintf("%d active", count), "red", nil)
		var types []string
		for _, a := range alerts {
			if t, ok := a["type"].(string); ok {
				types = append(types, t)
			}
		}
		send("alert_detail", strings.Join(types, ", "), nil)
	}()

	// 12. Pending MDM Commands (Classic API)
	wg.Add(1)
	go func() {
		defer wg.Done()
		v, err := fetchClassicNestedSize(ctx, client, "/JSSResource/computercommands", "computer_commands")
		send("pending_computer_cmds", v, err)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		v, err := fetchClassicNestedSize(ctx, client, "/JSSResource/mobiledevicecommands", "mobile_device_commands")
		send("pending_mobile_cmds", v, err)
	}()

	// 12b. Failed MDM Commands (Modern API v2 with RSQL filter)
	wg.Add(1)
	go func() {
		defer wg.Done()
		v, err := fetchPaginatedCount(ctx, client, "/v2/mdm/commands?filter=status%3D%3DError")
		send("failed_cmds", v, err)
	}()

	// 13. Configuration Management (Classic API)
	wg.Add(1)
	go func() {
		defer wg.Done()
		v, err := fetchClassicCount(ctx, client, "/JSSResource/policies", "policies")
		send("policies", v, err)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		v, err := fetchClassicCount(ctx, client, "/JSSResource/osxconfigurationprofiles", "os_x_configuration_profiles")
		send("macos_profiles", v, err)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		v, err := fetchClassicCount(ctx, client, "/JSSResource/mobiledeviceconfigurationprofiles", "configuration_profiles")
		send("ios_profiles", v, err)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		v, err := fetchClassicCount(ctx, client, "/JSSResource/packages", "packages")
		send("packages", v, err)
	}()

	// 14. Patch Management (Classic API)
	wg.Add(1)
	go func() {
		defer wg.Done()
		v, err := fetchClassicCount(ctx, client, "/JSSResource/patchsoftwaretitles", "patch_software_titles")
		send("patch_titles", v, err)
	}()

	// 15. Integrations
	wg.Add(1)
	go func() {
		defer wg.Done()
		v, err := fetchClassicCount(ctx, client, "/JSSResource/webhooks", "webhooks")
		send("webhooks", v, err)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		v, err := fetchArrayCount(ctx, client, "/ldap/servers")
		send("ldap_servers", v, err)
	}()

	// 16. DEP Sync Status (latest sync for first enrollment instance)
	wg.Add(1)
	go func() {
		defer wg.Done()
		// First get enrollment instances to find the first ID
		data, err := fetchJSON(ctx, client, "/v1/device-enrollments")
		if err != nil {
			send("dep_sync_status", "", err)
			return
		}
		results, _ := data["results"].([]interface{})
		if len(results) == 0 {
			send("dep_sync_status", "No DEP instances", nil)
			return
		}
		// Get the first instance ID
		first, ok := results[0].(map[string]interface{})
		if !ok {
			send("dep_sync_status", "N/A", nil)
			return
		}
		instanceID := ""
		if id, ok := first["id"].(string); ok {
			instanceID = id
		} else if id, ok := first["id"].(float64); ok {
			instanceID = strconv.Itoa(int(id))
		}
		if instanceID == "" {
			send("dep_sync_status", "N/A", nil)
			return
		}
		// Fetch latest sync state
		syncPath := fmt.Sprintf("/v1/device-enrollments/%s/syncs/latest", instanceID)
		syncData, err := fetchJSON(ctx, client, syncPath)
		if err != nil {
			send("dep_sync_status", "", err)
			return
		}
		state, _ := syncData["syncState"].(string)
		ts, _ := syncData["timestamp"].(string)
		if state == "" {
			send("dep_sync_status", "N/A", nil)
			return
		}
		// Parse timestamp for display
		display := state
		if ts != "" {
			if t, err := time.Parse("2006-01-02T15:04:05.999", ts); err == nil {
				display = fmt.Sprintf("%s (%s)", state, t.Format("Jan 02 15:04 UTC"))
			}
		}
		if state == "SUCCESSFUL" {
			send("dep_sync_status", display, nil)
		} else {
			sendWithColor("dep_sync_status", display, "yellow", nil)
		}
	}()

	wg.Wait()

	// Get results (with "N/A" fallback)
	get := func(key string) string {
		if v, ok := results[key]; ok {
			return v
		}
		return "N/A"
	}

	// getItem returns an overviewItem with optional color hint from expiration dates.
	getItem := func(resource, key string) overviewItem {
		return overviewItem{resource, get(key), colorHints[key]}
	}

	// item creates a plain overviewItem with no color hint.
	item := func(resource, value string) overviewItem {
		return overviewItem{resource, value, ""}
	}

	// Build notifications section: list each alert type on its own line
	notifItems := []overviewItem{
		getItem("Active Alerts", "alerts"),
	}
	if detail := get("alert_detail"); detail != "" && detail != "N/A" {
		types := strings.Split(detail, ", ")
		for i, t := range types {
			label := ""
			if i == 0 {
				label = "Alert Types"
			}
			notifItems = append(notifItems, overviewItem{label, t, "red"})
		}
	}

	// Color failed commands red if count > 0
	failedCmdsItem := overviewItem{"Failed Commands", get("failed_cmds"), ""}
	if v := get("failed_cmds"); v != "0" && v != "N/A" {
		failedCmdsItem.ColorHint = "red"
	}

	sections := []overviewSection{
		{
			Name: "Instance Info",
			Items: append([]overviewItem{
				item("Server URL", serverURL),
				item("Jamf Pro Version", get("version")),
				item("Health Status", get("health")),
				item("SLASA Status", get("slasa")),
				item("CSA Scopes", get("csa_scopes")),
				{}, // blank separator
			}, notifItems...),
		},
		{
			Name: "Inventory Summary",
			Items: []overviewItem{
				item("Managed Computers", get("managed_computers")),
				item("Unmanaged Computers", get("unmanaged_computers")),
				item("Managed Devices", get("managed_devices")),
				item("Unmanaged Devices", get("unmanaged_devices")),
			},
		},
		{
			Name: "Jamf Pro Features",
			Items: []overviewItem{
				item("VPP Token", get("vpp")),
				item("DEP Account", get("dep")),
				item("Cloud Deployments", get("cloud_deploy")),
				item("Patch Management", get("patch")),
				item("SSO (SAML)", get("sso")),
				item("SMTP", get("smtp")),
			},
		},
		{
			Name: "Enrollment",
			Items: []overviewItem{
				item("macOS Enterprise Enrollment", get("enroll_macos_enterprise")),
				item("iOS Enterprise Enrollment", get("enroll_ios_enterprise")),
				item("iOS Personal Enrollment", get("enroll_ios_personal")),
				item("Account-Driven User Enrollment", get("enroll_adue")),
				item("Account-Driven Device (macOS)", get("enroll_adde_macos")),
				{}, // blank separator
				item("DEP Instances", get("dep_instances")),
				getItem("DEP Token Expires", "dep_token_expires"),
				item("DEP Sync Status", get("dep_sync_status")),
				item("Computer Prestages", get("computer_prestages")),
				item("Mobile Device Prestages", get("md_prestages")),
			},
		},
		{
			Name: "Client Check-In",
			Items: []overviewItem{
				item("Check-In Frequency", get("checkin_freq")),
				item("Create Hooks", get("create_hooks")),
				item("Startup Script", get("startup_script")),
				item("Local Config Profiles", get("local_config")),
			},
		},
		{
			Name: "Security & MDM",
			Items: []overviewItem{
				item("LAPS Auto Deploy", get("laps_auto_deploy")),
				item("LAPS Auto Rotate", get("laps_auto_rotate")),
				item("MDM Auto Renew (Computers)", get("mdm_renew_computer")),
				item("MDM Auto Renew (Mobile)", get("mdm_renew_mobile")),
				item("Computer Cert Expiry Limit", get("mdm_cert_computer_days")),
				item("Mobile Cert Expiry Limit", get("mdm_cert_mobile_days")),
				getItem("Built-in CA Expires", "ca_expires"),
				item("Pending Computer Commands", get("pending_computer_cmds")),
				item("Pending Mobile Commands", get("pending_mobile_cmds")),
				failedCmdsItem,
			},
		},
		{
			Name: "Organization",
			Items: []overviewItem{
				item("Sites", get("sites")),
				item("Buildings", get("buildings")),
				item("Departments", get("departments")),
				item("Categories", get("categories")),
				item("Computer Groups", get("computer_groups")),
				item("Mobile Device Smart Groups", get("md_smart_groups")),
				item("Static User Groups", get("static_user_groups")),
			},
		},
		{
			Name: "Configuration & Deployment",
			Items: []overviewItem{
				item("Policies", get("policies")),
				item("macOS Config Profiles", get("macos_profiles")),
				item("iOS Config Profiles", get("ios_profiles")),
				item("Packages", get("packages")),
				item("Scripts", get("scripts")),
				item("eBooks", get("ebooks")),
				item("JCDS Files", get("jcds_files")),
				item("Patch Titles", get("patch_titles")),
				item("LDAP/IdP Servers", get("ldap_servers")),
				item("Webhooks", get("webhooks")),
			},
		},
		{
			Name: "Self Service",
			Items: []overviewItem{
				item("Install Automatically", get("ss_install_auto")),
				item("Login Required", get("ss_login_required")),
				item("Notifications", get("ss_notifications")),
			},
		},
	}

	return sections, nil
}

// isNumericValue returns true if the string looks like a count (digits and commas only).
func isNumericValue(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c != ',' && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

// printOverviewTable renders a grouped overview table with ANSI colors.
func printOverviewTable(w io.Writer, sections []overviewSection, useColor bool) {
	colorize := func(text, code string) string {
		if !useColor {
			return text
		}
		return code + text + "\033[0m"
	}

	bold := "\033[1m"
	green := "\033[32m"
	yellow := "\033[33m"
	red := "\033[31m"
	dim := "\033[2m"

	const labelWidth = 30
	const totalWidth = 62

	// Title
	fmt.Fprintln(w)
	fmt.Fprintln(w, colorize("  INSTANCE OVERVIEW", bold))
	fmt.Fprintln(w, colorize("  "+strings.Repeat("━", totalWidth), dim))

	for _, section := range sections {
		// Section header with dim separator
		fmt.Fprintln(w)
		fmt.Fprintf(w, "  %s\n", colorize(section.Name, bold))
		fmt.Fprintln(w, colorize("  "+strings.Repeat("─", totalWidth), dim))

		for _, item := range section.Items {
			// Empty item = blank separator line within a section
			if item.Resource == "" && item.Value == "" {
				fmt.Fprintln(w)
				continue
			}

			displayValue := item.Value
			visibleLen := len(item.Value)

			switch {
			case item.ColorHint == "red":
				displayValue = colorize("● "+item.Value, red)
				visibleLen += 2
			case item.ColorHint == "yellow":
				displayValue = colorize("● "+item.Value, yellow)
				visibleLen += 2
			case item.Value == "ok" || item.Value == "ACCEPTED" || item.Value == "None":
				displayValue = colorize("● "+item.Value, green)
				visibleLen += 2
			case item.Value == "enabled":
				displayValue = colorize("● "+item.Value, green)
				visibleLen += 2
			case strings.HasPrefix(item.Value, "SUCCESSFUL"):
				displayValue = colorize("● "+item.Value, green)
				visibleLen += 2
			case item.Value == "disabled":
				displayValue = colorize("○ "+item.Value, dim)
				visibleLen += 2
			case item.Value == "offline" || strings.HasPrefix(item.Value, "HTTP"):
				displayValue = colorize("● "+item.Value, red)
				visibleLen += 2
			case item.Value == "N/A" || item.Value == "Not configured" || item.Value == "None configured":
				displayValue = colorize(item.Value, dim)
			}

			// Right-align values that fit; left-align long values (e.g. alert types)
			padding := totalWidth - labelWidth - visibleLen
			if padding >= 1 {
				fmt.Fprintf(w, "  %-*s%*s%s\n", labelWidth, item.Resource, padding, "", displayValue)
			} else {
				fmt.Fprintf(w, "  %-*s %s\n", labelWidth, item.Resource, displayValue)
			}
		}
	}
	fmt.Fprintln(w)
}

// overviewToRows flattens sections into []map[string]interface{} for structured output.
func overviewToRows(sections []overviewSection) []map[string]interface{} {
	var rows []map[string]interface{}
	for _, section := range sections {
		for _, item := range section.Items {
			row := map[string]interface{}{
				"section":  section.Name,
				"resource": item.Resource,
				"value":    item.Value,
			}
			if item.ColorHint != "" {
				row["status"] = item.ColorHint
			}
			rows = append(rows, row)
		}
	}
	return rows
}

func newOverviewCmd(cliCtx *generated.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "overview",
		Short: "Show a summary of the Jamf Pro instance",
		Long: `Display a grouped summary of your Jamf Pro instance including server info,
feature flags, inventory counts, organizational structure, and more.

Makes parallel API calls for fast results. Items that fail to load show "N/A".`,
		RunE: func(cmd *cobra.Command, args []string) error {
			sections, err := runOverview(cmd.Context(), cliCtx)
			if err != nil {
				return err
			}

			// Table output uses custom grouped rendering
			if outputFmt == "" || outputFmt == "table" {
				printOverviewTable(cmd.OutOrStdout(), sections, !noColor)
				return nil
			}

			// Structured formats delegate to the standard formatter
			rows := overviewToRows(sections)
			formatter := output.New(outputFmt, noColor, wide)
			return formatter.Print(rows)
		},
	}
}
