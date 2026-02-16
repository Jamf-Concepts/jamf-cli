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

	"github.com/ktn-jamf/jamfpro-cli/internal/commands/generated"
	"github.com/ktn-jamf/jamfpro-cli/internal/output"
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

	sections := []overviewSection{
		{
			Name: "Instance Info",
			Items: []overviewItem{
				item("Server URL", serverURL),
				item("Jamf Pro Version", get("version")),
				item("Health Status", get("health")),
				item("SLASA Status", get("slasa")),
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
			Name: "Cloud Services (CSA)",
			Items: []overviewItem{
				item("Scopes", get("csa_scopes")),
			},
		},
		{
			Name: "Enrollment Settings",
			Items: []overviewItem{
				item("macOS Enterprise Enrollment", get("enroll_macos_enterprise")),
				item("iOS Enterprise Enrollment", get("enroll_ios_enterprise")),
				item("iOS Personal Enrollment", get("enroll_ios_personal")),
				item("Account-Driven User Enrollment", get("enroll_adue")),
				item("Account-Driven Device (macOS)", get("enroll_adde_macos")),
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
			Name: "LAPS",
			Items: []overviewItem{
				item("Auto Deploy", get("laps_auto_deploy")),
				item("Auto Rotate", get("laps_auto_rotate")),
			},
		},
		{
			Name: "MDM Profile Renewal",
			Items: []overviewItem{
				item("Auto Renew (Computers)", get("mdm_renew_computer")),
				item("Auto Renew (Mobile)", get("mdm_renew_mobile")),
				item("Computer Cert Expiry Limit", get("mdm_cert_computer_days")),
				item("Mobile Cert Expiry Limit", get("mdm_cert_mobile_days")),
			},
		},
		{
			Name: "Certificate Authority",
			Items: []overviewItem{
				getItem("Built-in CA Expires", "ca_expires"),
			},
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
			Name: "Organizational Structure",
			Items: []overviewItem{
				item("Sites", get("sites")),
				item("Buildings", get("buildings")),
				item("Departments", get("departments")),
				item("Categories", get("categories")),
			},
		},
		{
			Name: "Device Groups",
			Items: []overviewItem{
				item("Computer Groups", get("computer_groups")),
				item("Mobile Device Smart Groups", get("md_smart_groups")),
			},
		},
		{
			Name: "Configuration & Deployment",
			Items: []overviewItem{
				item("Scripts", get("scripts")),
				item("eBooks", get("ebooks")),
				item("JCDS Files", get("jcds_files")),
			},
		},
		{
			Name: "Enrollment",
			Items: []overviewItem{
				item("DEP Instances", get("dep_instances")),
				getItem("DEP Token Expires", "dep_token_expires"),
				item("Computer Prestages", get("computer_prestages")),
				item("Mobile Device Prestages", get("md_prestages")),
			},
		},
		{
			Name: "Users & Access",
			Items: []overviewItem{
				item("Static User Groups", get("static_user_groups")),
			},
		},
	}

	return sections, nil
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

	fmt.Fprintln(w, colorize("INSTANCE OVERVIEW", bold))
	fmt.Fprintln(w, strings.Repeat("─", 50))

	for i, section := range sections {
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintln(w, colorize(section.Name, bold))

		for _, item := range section.Items {
			displayValue := item.Value

			switch {
			case item.ColorHint == "red":
				displayValue = colorize("● "+item.Value, red)
			case item.ColorHint == "yellow":
				displayValue = colorize("● "+item.Value, yellow)
			case item.Value == "ok" || item.Value == "ACCEPTED":
				displayValue = colorize("● "+item.Value, green)
			case item.Value == "enabled":
				displayValue = colorize("● "+item.Value, green)
			case item.Value == "disabled":
				displayValue = colorize("○ "+item.Value, dim)
			case item.Value == "offline" || strings.HasPrefix(item.Value, "HTTP"):
				displayValue = colorize("● "+item.Value, red)
			case item.Value == "N/A" || item.Value == "Not configured" || item.Value == "None configured":
				displayValue = colorize(item.Value, dim)
			}

			fmt.Fprintf(w, "  %-28s %s\n", item.Resource, displayValue)
		}
	}
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
