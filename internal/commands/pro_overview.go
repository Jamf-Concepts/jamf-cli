// Copyright 2026, Jamf Software LLC

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

	"github.com/Jamf-Concepts/jamf-cli/internal/xmlconv"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/blueprints"
	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform/compliancebenchmarks"
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

// tokenExpiry holds per-instance token expiry data for multi-row display.
type tokenExpiry struct {
	Name  string
	Value string
	Color string
}

// fetchJSON performs a GET request and returns the parsed JSON object.
func fetchJSON(ctx context.Context, client registry.HTTPClient, path string) (map[string]any, error) {
	resp, err := client.Do(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // 10 MB limit
	if err != nil {
		return nil, err
	}

	// Classic API returns XML — convert to map
	if xmlconv.IsXML(body) {
		return xmlconv.ToMap(body)
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// fetchPaginatedCount appends ?page-size=1 to the path and returns the totalCount.
func fetchPaginatedCount(ctx context.Context, client registry.HTTPClient, path string) (string, error) {
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
func fetchArrayCount(ctx context.Context, client registry.HTTPClient, path string) (string, error) {
	resp, err := client.Do(ctx, "GET", path, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return "", err
	}

	var arr []json.RawMessage
	if err := json.Unmarshal(body, &arr); err != nil {
		return "", err
	}
	return formatCount(float64(len(arr))), nil
}

// fetchCDPFileCount paginates through the cloud-distribution-point files endpoint
// to get an accurate total. The endpoint's totalCount field is unreliable
// (reflects page size, not actual total), so we count by page until exhausted.
func fetchCDPFileCount(ctx context.Context, client registry.HTTPClient) (string, error) {
	const (
		pageSize = 100
		maxPages = 1000
	)
	total := 0
	for page := range maxPages {
		path := fmt.Sprintf("/v1/cloud-distribution-point/files?page=%d&page-size=%d", page, pageSize)
		data, err := fetchJSON(ctx, client, path)
		if err != nil {
			return "", err
		}
		results, ok := data["results"].([]any)
		if !ok {
			return "", fmt.Errorf("unexpected response: missing results array")
		}
		total += len(results)
		if len(results) < pageSize {
			break
		}
	}
	return formatCount(float64(total)), nil
}

// fetchClassicCount performs a GET on a Classic API list endpoint and returns the
// count of items. Classic API returns XML; JSON is handled as a fallback.
func fetchClassicCount(ctx context.Context, client registry.HTTPClient, path, wrapperKey string) (string, error) {
	resp, err := client.Do(ctx, "GET", path, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return "", err
	}

	if xmlconv.IsXML(body) {
		count, err := xmlconv.CountListItems(body)
		if err != nil {
			return "", err
		}
		return formatCount(float64(count)), nil
	}

	// JSON fallback
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

// fetchClassicNestedSize performs a GET on a Classic API list endpoint and returns
// the number of items. For XML responses it counts child elements directly;
// for JSON it falls back to extracting the "size" field from the nested structure.
func fetchClassicNestedSize(ctx context.Context, client registry.HTTPClient, path, outerKey string) (string, error) {
	resp, err := client.Do(ctx, "GET", path, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return "", err
	}

	if xmlconv.IsXML(body) {
		count, err := xmlconv.CountListItems(body)
		if err != nil {
			return "", err
		}
		return formatCount(float64(count)), nil
	}

	// JSON fallback
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return "", err
	}
	inner, ok := data[outerKey].(map[string]any)
	if !ok {
		return "0", nil
	}
	if size, ok := inner["size"]; ok {
		return formatCount(size), nil
	}
	return "0", nil
}

// formatCount converts a numeric value to a comma-formatted string.
func formatCount(v any) string {
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
func enabledDisabled(v any) string {
	if b, ok := v.(bool); ok && b {
		return "enabled"
	}
	return "disabled"
}

// friendlyAlertTypes maps Jamf Pro notification type enums to human-readable labels.
var friendlyAlertTypes = map[string]string{
	"APNS_CONNECTION_FAILURE":                                     "APNs connection failure",
	"APNS_CERT_REVOKED":                                           "APNs certificate revoked",
	"CLOUD_LDAP_CERT_EXPIRED":                                     "Cloud LDAP certificate expired",
	"CLOUD_LDAP_CERT_WILL_EXPIRE":                                 "Cloud LDAP certificate expiring",
	"SSO_CERT_EXPIRED":                                            "SSO certificate expired",
	"SSO_IDP_CERT_EXPIRED":                                        "SSO IdP certificate expired",
	"SSO_CERT_WILL_EXPIRE":                                        "SSO certificate expiring",
	"SSO_IDP_CERT_WILL_EXPIRE":                                    "SSO IdP certificate expiring",
	"TOMCAT_SSL_CERT_EXPIRED":                                     "SSL certificate expired",
	"TOMCAT_SSL_CERT_WILL_EXPIRE":                                 "SSL certificate expiring",
	"GSX_CERT_EXPIRED":                                            "GSX certificate expired",
	"GSX_CERT_WILL_EXPIRE":                                        "GSX certificate expiring",
	"INVALID_REFERENCES_SCRIPTS":                                  "Scripts with invalid references",
	"INVALID_REFERENCES_EXT_ATTR":                                 "Extension attributes with invalid references",
	"INVALID_REFERENCES_POLICIES":                                 "Policies with invalid references",
	"VPP_ACCOUNT_EXPIRED":                                         "Volume Purchasing token expired",
	"VPP_ACCOUNT_WILL_EXPIRE":                                     "Volume Purchasing token expiring",
	"VPP_TOKEN_REVOKED":                                           "Volume Purchasing token revoked",
	"DEP_INSTANCE_EXPIRED":                                        "Automated Device Enrollment token expired",
	"DEP_INSTANCE_WILL_EXPIRE":                                    "Automated Device Enrollment token expiring",
	"PUSH_PROXY_CERT_EXPIRED":                                     "Push proxy certificate expired",
	"PUSH_CERT_EXPIRED":                                           "Push certificate expired",
	"PUSH_CERT_WILL_EXPIRE":                                       "Push certificate expiring",
	"FREQUENT_INVENTORY_COLLECTION_POLICY":                        "Frequent inventory collection policy",
	"POLICY_MANAGEMENT_ACCOUNT_PAYLOAD_SECURITY_SINGLE":           "Management account security issue (single policy)",
	"POLICY_MANAGEMENT_ACCOUNT_PAYLOAD_SECURITY_MULTIPLE":         "Management account security issue (multiple policies)",
	"USER_INITIATED_ENROLLMENT_MANAGEMENT_ACCOUNT_SECURITY_ISSUE": "User enrollment management account security issue",
	"PATCH_UPDATE":                                                "Patch update available",
	"PATCH_EXTENTION_ATTRIBUTE":                                   "Patch extension attribute issue",
	"HCL_ERROR":                                                   "Healthcare Listener error",
	"HCL_BIND_ERROR":                                              "Healthcare Listener bind error",
	"JIM_ERROR":                                                   "Jamf Infrastructure Manager error",
	"EXCEEDED_LICENSE_COUNT":                                      "Device count exceeds license",
	"MII_INVENTORY_UPLOAD_FAILED_NOTIFICATION":                    "Inventory upload failed",
	"MII_HEARTBEAT_FAILED_NOTIFICATION":                           "Infrastructure heartbeat failed",
	"MII_UNATHORIZED_RESPONSE_NOTIFICATION":                       "Infrastructure unauthorized response",
	"MDM_EXTERNAL_SIGNING_CERTIFICATE_EXPIRED":                    "MDM signing certificate expired",
	"MDM_EXTERNAL_SIGNING_CERTIFICATE_EXPIRING":                   "MDM signing certificate expiring",
	"MDM_EXTERNAL_SIGNING_CERTIFICATE_EXPIRING_TODAY":             "MDM signing certificate expires today",
	"INSECURE_LDAP":                                               "Insecure LDAP connection",
	"LDAP_CONNECTION_CHECK_THROUGH_JIM_SUCCESSFUL":                "LDAP connection check successful (via JIM)",
	"LDAP_CONNECTION_CHECK_THROUGH_JIM_FAILED":                    "LDAP connection check failed (via JIM)",
	"DEVICE_ENROLLMENT_PROGRAM_T_C_NOT_SIGNED":                    "Apple Business Manager T&C not signed",
	"APPLE_SCHOOL_MANAGER_T_C_NOT_SIGNED":                         "Apple School Manager T&C not signed",
	"USER_MAID_MISMATCH_ERROR":                                    "User identity mismatch",
	"USER_MAID_ROSTER_DUPLICATE_ERROR":                            "Duplicate user in roster",
	"USER_MAID_DUPLICATE_ERROR":                                   "Duplicate user identity",
	"BUILT_IN_CA_EXPIRING":                                        "Built-in CA expiring",
	"BUILT_IN_CA_EXPIRED":                                         "Built-in CA expired",
	"BUILT_IN_CA_RENEWAL_SUCCESS":                                 "Built-in CA renewal succeeded",
	"BUILT_IN_CA_RENEWAL_FAILED":                                  "Built-in CA renewal failed",
	"JAMF_PROTECT_UPDATE":                                         "Jamf Protect update available",
	"JAMF_CONNECT_UPDATE":                                         "Jamf Connect update available",
	"JAMF_CONNECT_MAJOR_UPDATE":                                   "Jamf Connect major update available",
	"JAMF_PROTECT_CONNECTION_ISSUE":                               "Jamf Protect connection issue",
	"DEVICE_COMPLIANCE_CONNECTION_ERROR":                          "Device compliance connection error",
	"CONDITIONAL_ACCESS_CONNECTION_ERROR":                         "Conditional access connection error",
	"NO_LONGER_DEVICE_ASSIGNABLE":                                 "App no longer device-assignable",
	"BEYOND_CORP_CONNECTION_ERROR":                                "BeyondCorp connection error",
	"APP_INSTALLERS_NEW_APP_VERSION_DEPLOYMENT_STARTED":           "App Installer deployment started",
	"APP_INSTALLERS_NEW_APP_VERSION_AVAILABLE":                    "App Installer update available",
	"APP_INSTALLERS_APP_VERSION_REMOVED":                          "App Installer version removed",
	"APP_INSTALLERS_DEPLOYMENT_INSTALLATION_FAILED":               "App Installer deployment failed",
	"APP_INSTALLERS_APP_TITLE_REMOVED":                            "App Installer title removed",
	"SAML_RESPONSE_ASSERTION_SIGNING_REQUIRED":                    "SAML assertion signing required",
	"SMTP_GOOGLE_MAIL_DEFAULT_EMAIL_UNAUTHENTICATED":              "SMTP Google Mail unauthenticated",
	"CAN_ENABLE_PASSWORD_RESET_CLOUD_ENVIRONMENT":                 "Password reset available for cloud",
	"USERS_HAVE_DUPLICATED_EMAIL_ADDRESSES":                       "Users with duplicate email addresses",
	"DIRECTORY_CACHE_AWAITING_SYNC":                               "Directory cache awaiting sync",
	"PSSO_EXTERNAL_URL_UNAVAILABLE":                               "Platform SSO external URL unavailable",
}

// friendlyAlertType converts a Jamf Pro notification type enum to a
// human-readable label. Unknown types are returned as-is.
func friendlyAlertType(t string) string {
	if f, ok := friendlyAlertTypes[t]; ok {
		return f
	}
	return t
}

// formatExpirationDate formats a date string and adds proximity context.
// Returns the formatted date and a color hint: "red", "yellow", or "".
func formatExpirationDate(dateStr string, now time.Time) (string, string) {
	var t time.Time
	var err error
	for _, layout := range []string{
		"2006-01-02",
		time.RFC3339,
		"2006-01-02T15:04:05.999Z",
		"2006-01-02T15:04:05.000Z0700",
	} {
		t, err = time.Parse(layout, dateStr)
		if err == nil {
			break
		}
	}
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
func runOverview(ctx context.Context, cliCtx *registry.CLIContext) ([]overviewSection, error) {
	client := cliCtx.Client

	var mu sync.Mutex
	results := make(map[string]string)
	colorHints := make(map[string]string)
	var adeTokenItems []tokenExpiry // per-ADE-instance token expiry rows
	var vppTokenItems []tokenExpiry // per-VPP-location token expiry rows
	var wg sync.WaitGroup
	sem := make(chan struct{}, 10) // cap concurrent API calls

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
	wg.Go(func() {
		sem <- struct{}{}
		defer func() { <-sem }()
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
	})

	// 1. Instance Info: actual Pro server URL (needed for health check + display)
	wg.Go(func() {
		sem <- struct{}{}
		defer func() { <-sem }()
		data, err := fetchJSON(ctx, client, "/v1/jamf-pro-server-url")
		if err != nil {
			send("pro_url", "", err)
			return
		}
		if u, ok := data["url"].(string); ok && u != "" {
			send("pro_url", u, nil)
		} else {
			send("pro_url", "", nil)
		}
	})

	// 1. Instance Info: SLASA
	wg.Go(func() {
		sem <- struct{}{}
		defer func() { <-sem }()
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
	})

	// 2. Jamf Pro Features (single call)
	wg.Go(func() {
		sem <- struct{}{}
		defer func() { <-sem }()
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
	})

	// 3. CSA Scopes
	wg.Go(func() {
		sem <- struct{}{}
		defer func() { <-sem }()
		resp, err := client.Do(ctx, "GET", "/v1/csa/token", nil)
		if err != nil {
			send("csa_scopes", "", err)
			return
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode == http.StatusNotFound {
			send("csa_scopes", "Not configured", nil)
			return
		}
		if resp.StatusCode != http.StatusOK {
			send("csa_scopes", "", fmt.Errorf("HTTP %d", resp.StatusCode))
			return
		}

		body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
		if err != nil {
			send("csa_scopes", "", err)
			return
		}

		var data map[string]any
		if err := json.Unmarshal(body, &data); err != nil {
			send("csa_scopes", "", err)
			return
		}

		if scopes, ok := data["scopes"].([]any); ok {
			send("csa_scopes", fmt.Sprintf("%d scopes", len(scopes)), nil)
		} else {
			send("csa_scopes", "N/A", nil)
		}
	})

	// 4. Client Check-In (single call)
	wg.Go(func() {
		sem <- struct{}{}
		defer func() { <-sem }()
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
	})

	// Enrollment Settings (single call → 5 booleans)
	wg.Go(func() {
		sem <- struct{}{}
		defer func() { <-sem }()
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
	})

	// Self Service (single call → nested fields)
	wg.Go(func() {
		sem <- struct{}{}
		defer func() { <-sem }()
		data, err := fetchJSON(ctx, client, "/v1/self-service/settings")
		if err != nil {
			for _, k := range []string{"ss_install_auto", "ss_login_required", "ss_notifications"} {
				send(k, "", err)
			}
			return
		}
		if install, ok := data["installSettings"].(map[string]any); ok {
			send("ss_install_auto", enabledDisabled(install["installAutomatically"]), nil)
		} else {
			send("ss_install_auto", "N/A", nil)
		}
		if login, ok := data["loginSettings"].(map[string]any); ok {
			if level, ok := login["userLoginLevel"].(string); ok {
				send("ss_login_required", level, nil)
			} else {
				send("ss_login_required", "N/A", nil)
			}
		} else {
			send("ss_login_required", "N/A", nil)
		}
		if config, ok := data["configurationSettings"].(map[string]any); ok {
			send("ss_notifications", enabledDisabled(config["notificationsEnabled"]), nil)
		} else {
			send("ss_notifications", "N/A", nil)
		}
	})

	// LAPS (single call → 2 booleans)
	wg.Go(func() {
		sem <- struct{}{}
		defer func() { <-sem }()
		data, err := fetchJSON(ctx, client, "/v2/local-admin-password/settings")
		if err != nil {
			for _, k := range []string{"laps_auto_deploy", "laps_auto_rotate"} {
				send(k, "", err)
			}
			return
		}
		send("laps_auto_deploy", enabledDisabled(data["autoDeployEnabled"]), nil)
		send("laps_auto_rotate", enabledDisabled(data["autoRotateEnabled"]), nil)
	})

	// MDM Profile Renewal (single call → 2 booleans + 2 values)
	wg.Go(func() {
		sem <- struct{}{}
		defer func() { <-sem }()
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
	})

	// Certificate Authority (single call → expiration date)
	wg.Go(func() {
		sem <- struct{}{}
		defer func() { <-sem }()
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
	})

	// 5. Inventory Summary (single call)
	wg.Go(func() {
		sem <- struct{}{}
		defer func() { <-sem }()
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
	})

	// 6. Organizational Structure
	wg.Go(func() {
		sem <- struct{}{}
		defer func() { <-sem }()
		v, err := fetchArrayCount(ctx, client, "/v1/sites")
		send("sites", v, err)
	})

	wg.Go(func() {
		sem <- struct{}{}
		defer func() { <-sem }()
		v, err := fetchPaginatedCount(ctx, client, "/v1/buildings")
		send("buildings", v, err)
	})

	wg.Go(func() {
		sem <- struct{}{}
		defer func() { <-sem }()
		v, err := fetchPaginatedCount(ctx, client, "/v1/departments")
		send("departments", v, err)
	})

	wg.Go(func() {
		sem <- struct{}{}
		defer func() { <-sem }()
		v, err := fetchPaginatedCount(ctx, client, "/v1/categories")
		send("categories", v, err)
	})

	// 7. Device Groups
	wg.Go(func() {
		sem <- struct{}{}
		defer func() { <-sem }()
		v, err := fetchPaginatedCount(ctx, client, "/v3/computer-groups/smart-groups")
		send("computer_smart_groups", v, err)
	})

	wg.Go(func() {
		sem <- struct{}{}
		defer func() { <-sem }()
		v, err := fetchPaginatedCount(ctx, client, "/v3/computer-groups/static-groups")
		send("computer_static_groups", v, err)
	})

	wg.Go(func() {
		sem <- struct{}{}
		defer func() { <-sem }()
		v, err := fetchPaginatedCount(ctx, client, "/v2/mobile-device-groups/smart-groups")
		send("md_smart_groups", v, err)
	})

	wg.Go(func() {
		sem <- struct{}{}
		defer func() { <-sem }()
		v, err := fetchPaginatedCount(ctx, client, "/v2/mobile-device-groups/static-groups")
		send("md_static_groups", v, err)
	})

	// 8. Configuration & Deployment
	wg.Go(func() {
		sem <- struct{}{}
		defer func() { <-sem }()
		v, err := fetchPaginatedCount(ctx, client, "/v1/scripts")
		send("scripts", v, err)
	})

	wg.Go(func() {
		sem <- struct{}{}
		defer func() { <-sem }()
		v, err := fetchPaginatedCount(ctx, client, "/v1/ebooks")
		send("ebooks", v, err)
	})

	wg.Go(func() {
		sem <- struct{}{}
		defer func() { <-sem }()
		v, err := fetchCDPFileCount(ctx, client)
		send("jcds_files", v, err)
	})

	wg.Go(func() {
		sem <- struct{}{}
		defer func() { <-sem }()
		v, err := fetchPaginatedCount(ctx, client, "/v1/app-installers/titles")
		send("app_installers", v, err)
	})

	// 9. Enrollment (fetch full results for count + per-instance token expiration)
	wg.Go(func() {
		sem <- struct{}{}
		defer func() { <-sem }()
		data, err := fetchJSON(ctx, client, "/v1/device-enrollments")
		if err != nil {
			send("ade_instances", "", err)
			return
		}
		if tc, ok := data["totalCount"]; ok {
			send("ade_instances", formatCount(tc), nil)
		} else {
			send("ade_instances", "0", nil)
		}

		// Build per-instance token expiry rows
		items, _ := data["results"].([]any)
		now := time.Now()
		mu.Lock()
		for _, r := range items {
			item, ok := r.(map[string]any)
			if !ok {
				continue
			}
			dateStr, _ := item["tokenExpirationDate"].(string)
			name, _ := item["name"].(string)
			if name == "" {
				name = "Unnamed"
			}
			if dateStr == "" {
				adeTokenItems = append(adeTokenItems, tokenExpiry{Name: name, Value: "No token", Color: ""})
				continue
			}
			formatted, color := formatExpirationDate(dateStr, now)
			adeTokenItems = append(adeTokenItems, tokenExpiry{Name: name, Value: formatted, Color: color})
		}
		mu.Unlock()
	})

	wg.Go(func() {
		sem <- struct{}{}
		defer func() { <-sem }()
		v, err := fetchPaginatedCount(ctx, client, "/v3/computer-prestages")
		send("computer_prestages", v, err)
	})

	wg.Go(func() {
		sem <- struct{}{}
		defer func() { <-sem }()
		v, err := fetchPaginatedCount(ctx, client, "/v3/mobile-device-prestages")
		send("md_prestages", v, err)
	})

	// VPP / Volume Purchasing Locations (count + per-instance token expiry)
	wg.Go(func() {
		sem <- struct{}{}
		defer func() { <-sem }()
		data, err := fetchJSON(ctx, client, "/v1/volume-purchasing-locations?page-size=100")
		if err != nil {
			send("vpp_locations", "", err)
			return
		}
		if tc, ok := data["totalCount"]; ok {
			send("vpp_locations", formatCount(tc), nil)
		} else {
			send("vpp_locations", "0", nil)
		}

		items, _ := data["results"].([]any)
		now := time.Now()
		mu.Lock()
		for _, r := range items {
			item, ok := r.(map[string]any)
			if !ok {
				continue
			}
			name, _ := item["name"].(string)
			if name == "" {
				name, _ = item["locationName"].(string)
			}
			if name == "" {
				name = "Unnamed"
			}
			dateStr, _ := item["tokenExpiration"].(string)
			if dateStr == "" {
				vppTokenItems = append(vppTokenItems, tokenExpiry{Name: name, Value: "No token", Color: ""})
				continue
			}
			formatted, color := formatExpirationDate(dateStr, now)
			vppTokenItems = append(vppTokenItems, tokenExpiry{Name: name, Value: formatted, Color: color})
		}
		mu.Unlock()
	})

	// 10. Users & Access
	wg.Go(func() {
		sem <- struct{}{}
		defer func() { <-sem }()
		v, err := fetchArrayCount(ctx, client, "/v1/static-user-groups")
		send("static_user_groups", v, err)
	})

	// 11. Notifications/Alerts
	wg.Go(func() {
		sem <- struct{}{}
		defer func() { <-sem }()
		resp, err := client.Do(ctx, "GET", "/v1/notifications", nil)
		if err != nil {
			send("alerts", "", err)
			send("alert_detail", "", err)
			return
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			send("alerts", "", fmt.Errorf("HTTP %d", resp.StatusCode))
			return
		}

		body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
		if err != nil {
			send("alerts", "", err)
			return
		}

		var alerts []map[string]any
		if err := json.Unmarshal(body, &alerts); err != nil {
			send("alerts", "", err)
			return
		}

		count := len(alerts)
		if count == 0 {
			send("alerts", "None", nil)
			sendWithColor("alert_detail", "", "", nil)
			send("apns_cert", "OK", nil)
			return
		}

		sendWithColor("alerts", fmt.Sprintf("%d active", count), "red", nil)
		var types []string
		for _, a := range alerts {
			if t, ok := a["type"].(string); ok {
				types = append(types, friendlyAlertType(t))
			}
		}
		send("alert_detail", strings.Join(types, ", "), nil)

		// Extract APNs certificate status from notifications
		for _, a := range alerts {
			t, _ := a["type"].(string)
			switch t {
			case "PUSH_CERT_EXPIRED":
				sendWithColor("apns_cert", "Expired", "red", nil)
				return
			case "PUSH_CERT_WILL_EXPIRE":
				sendWithColor("apns_cert", "Expiring soon", "yellow", nil)
				return
			}
		}
		send("apns_cert", "OK", nil)
	})

	// Admin SSO (from /v3/sso)
	wg.Go(func() {
		sem <- struct{}{}
		defer func() { <-sem }()
		data, err := fetchJSON(ctx, client, "/v3/sso")
		if err != nil {
			send("admin_sso", "", err)
			return
		}
		enabled, _ := data["ssoEnabled"].(bool)
		configType, _ := data["configurationType"].(string)
		// Admin SSO requires OIDC — SAML-only is regular SSO, not Admin SSO.
		if !enabled || (configType != "OIDC" && configType != "OIDC_WITH_SAML") {
			send("admin_sso", "disabled", nil)
			return
		}
		if configType == "OIDC_WITH_SAML" {
			send("admin_sso", "enabled (OIDC + SAML)", nil)
		} else {
			send("admin_sso", "enabled (OIDC)", nil)
		}
	})

	// 13. Configuration Management (Classic API)
	wg.Go(func() {
		sem <- struct{}{}
		defer func() { <-sem }()
		v, err := fetchClassicCount(ctx, client, "/JSSResource/policies", "policies")
		send("policies", v, err)
	})

	wg.Go(func() {
		sem <- struct{}{}
		defer func() { <-sem }()
		v, err := fetchClassicCount(ctx, client, "/JSSResource/osxconfigurationprofiles", "os_x_configuration_profiles")
		send("macos_profiles", v, err)
	})

	wg.Go(func() {
		sem <- struct{}{}
		defer func() { <-sem }()
		v, err := fetchClassicCount(ctx, client, "/JSSResource/mobiledeviceconfigurationprofiles", "configuration_profiles")
		send("ios_profiles", v, err)
	})

	wg.Go(func() {
		sem <- struct{}{}
		defer func() { <-sem }()
		v, err := fetchClassicCount(ctx, client, "/JSSResource/packages", "packages")
		send("packages", v, err)
	})

	// 14. Patch Management. The Pro API's patch software title configurations,
	// not Classic's /patchsoftwaretitles: capi v1993 withdrew every read on that
	// resource, so the Classic count is refused on a gateway profile while the
	// v3 Pro list is published. Same objects, and the list is a plain array
	// rather than a paged envelope, hence fetchArrayCount.
	wg.Go(func() {
		sem <- struct{}{}
		defer func() { <-sem }()
		v, err := fetchArrayCount(ctx, client, "/v3/patch-software-title-configurations")
		send("patch_titles", v, err)
	})

	// 15. Integrations
	wg.Go(func() {
		sem <- struct{}{}
		defer func() { <-sem }()
		v, err := fetchClassicCount(ctx, client, "/JSSResource/webhooks", "webhooks")
		send("webhooks", v, err)
	})

	wg.Go(func() {
		sem <- struct{}{}
		defer func() { <-sem }()
		v, err := fetchArrayCount(ctx, client, "/ldap/servers")
		send("ldap_servers", v, err)
	})

	// 16. ADE Sync Status (latest sync for first enrollment instance)
	wg.Go(func() {
		sem <- struct{}{}
		defer func() { <-sem }()
		// First get enrollment instances to find the first ID
		data, err := fetchJSON(ctx, client, "/v1/device-enrollments")
		if err != nil {
			send("ade_sync_status", "", err)
			return
		}
		depResults, _ := data["results"].([]any)
		if len(depResults) == 0 {
			send("ade_sync_status", "No ADE instances", nil)
			return
		}
		// Get the first instance ID
		first, ok := depResults[0].(map[string]any)
		if !ok {
			send("ade_sync_status", "N/A", nil)
			return
		}
		instanceID := ""
		if id, ok := first["id"].(string); ok {
			instanceID = id
		} else if id, ok := first["id"].(float64); ok {
			instanceID = strconv.Itoa(int(id))
		}
		if instanceID == "" {
			send("ade_sync_status", "N/A", nil)
			return
		}
		// Fetch latest sync state
		syncPath := fmt.Sprintf("/v1/device-enrollments/%s/syncs/latest", instanceID)
		syncData, err := fetchJSON(ctx, client, syncPath)
		if err != nil {
			send("ade_sync_status", "", err)
			return
		}
		state, _ := syncData["syncState"].(string)
		ts, _ := syncData["timestamp"].(string)
		if state == "" {
			send("ade_sync_status", "N/A", nil)
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
			send("ade_sync_status", display, nil)
		} else {
			sendWithColor("ade_sync_status", display, "yellow", nil)
		}
	})

	wg.Wait()

	// ── Health check (runs after wg.Wait so we can use the fetched pro URL) ──
	// Use the actual Pro instance URL for the health check (not the gateway URL).
	healthURL := serverURL
	if u, ok := results["pro_url"]; ok && u != "" {
		healthURL = u
	}
	h := checkHealth(healthURL)
	results["health"] = h.Status
	if h.Healthy {
		results["health_ok"] = "true"
	} else {
		results["health_ok"] = "false"
	}

	// ── Platform API metrics (only when platform auth is active) ──────────
	var platformSection *overviewSection
	if cliCtx.PlatformSDKClient != nil {
		platformSection = fetchPlatformOverview(ctx, cliCtx)
	}

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

	// Build alert items: list each alert type on its own line
	alertItems := []overviewItem{
		getItem("Active Alerts", "alerts"),
	}
	if detail := get("alert_detail"); detail != "" && detail != "N/A" {
		types := strings.Split(detail, ", ")
		for i, t := range types {
			label := ""
			if i == 0 {
				label = "Alert Types"
			}
			alertItems = append(alertItems, overviewItem{label, t, "red"})
		}
	}

	// Build Configuration section — skip items with count = 0
	var configItems []overviewItem
	for _, pair := range []struct {
		label, key string
	}{
		{"Policies", "policies"},
		{"macOS Config Profiles", "macos_profiles"},
		{"iOS Config Profiles", "ios_profiles"},
		{"Packages", "packages"},
		{"App Installers", "app_installers"},
		{"Scripts", "scripts"},
		{"eBooks", "ebooks"},
		{"JCDS Files", "jcds_files"},
		{"Patch Titles", "patch_titles"},
		{"LDAP/IdP Servers", "ldap_servers"},
		{"Webhooks", "webhooks"},
	} {
		v := get(pair.key)
		if v != "0" {
			configItems = append(configItems, item(pair.label, v))
		}
	}

	// Build Features section — only show enabled features
	var featureItems []overviewItem
	for _, pair := range []struct {
		label, key string
	}{
		{"Volume Purchasing", "vpp"},
		{"Automated Device Enrollment", "dep"},
		{"Cloud Distribution", "cloud_deploy"},
		{"Patch Management", "patch"},
		{"SSO (SAML)", "sso"},
		{"SMTP", "smtp"},
	} {
		v := get(pair.key)
		if v == "enabled" {
			featureItems = append(featureItems, item(pair.label, v))
		}
	}
	// Admin SSO shows its configuration type, not just enabled/disabled
	if v := get("admin_sso"); v != "N/A" && v != "disabled" {
		featureItems = append(featureItems, item("Admin SSO", v))
	}

	// Build Health & Alerts items
	healthItems := []overviewItem{item("Health Status", get("health"))}
	healthItems = append(healthItems, alertItems...)

	sections := []overviewSection{
		{
			Name:  "Health & Alerts",
			Items: healthItems,
		},
		{
			Name:  "Instance",
			Items: buildInstanceItems(get, item),
		},
		{
			Name: "Fleet",
			Items: []overviewItem{
				item("Managed Computers", get("managed_computers")),
				item("Unmanaged Computers", get("unmanaged_computers")),
				item("Check-In Frequency", get("checkin_freq")),
				item("Managed Devices", get("managed_devices")),
				item("Unmanaged Devices", get("unmanaged_devices")),
			},
		},
		{
			Name:  "Enrollment & Certificates",
			Items: buildEnrollmentItems(get, getItem, item, adeTokenItems, vppTokenItems),
		},
		{
			Name:  "Configuration",
			Items: configItems,
		},
		{
			Name: "Organization",
			Items: []overviewItem{
				item("Sites", get("sites")),
				item("Buildings", get("buildings")),
				item("Departments", get("departments")),
				item("Categories", get("categories")),
				item("Computer Smart Groups", get("computer_smart_groups")),
				item("Computer Static Groups", get("computer_static_groups")),
				item("Mobile Smart Groups", get("md_smart_groups")),
				item("Mobile Static Groups", get("md_static_groups")),
				item("Static User Groups", get("static_user_groups")),
			},
		},
		{
			Name:  "Features",
			Items: featureItems,
		},
	}

	if platformSection != nil {
		sections = append(sections, *platformSection)
	}

	return sections, nil
}

// buildInstanceItems assembles the Instance section.
// When the Pro server URL was fetched from the API, it is shown as "Server URL".
// When platform gateway auth is active, the gateway URL is also displayed.
func buildInstanceItems(get func(string) string, item func(string, string) overviewItem) []overviewItem {
	// Prefer the URL reported by the Jamf Pro API over the configured serverURL,
	// which may be a gateway address that doesn't represent the instance itself.
	displayURL := serverURL
	if u := get("pro_url"); u != "N/A" && u != "" {
		displayURL = u
	}

	items := []overviewItem{
		item("Server URL", displayURL),
		item("Jamf Pro Version", get("version")),
	}

	// When using platform gateway auth, also show the gateway URL.
	// Normalize trailing slashes — the API may return a URL with one.
	if strings.TrimRight(displayURL, "/") != strings.TrimRight(serverURL, "/") {
		items = append(items, item("Gateway URL", serverURL))
	}

	return items
}

// buildEnrollmentItems assembles the Enrollment & Certificates section,
// including per-instance ADE and VPP token expiry rows.
func buildEnrollmentItems(
	get func(string) string,
	getItem func(string, string) overviewItem,
	item func(string, string) overviewItem,
	adeTokens, vppTokens []tokenExpiry,
) []overviewItem {
	items := []overviewItem{
		item("ADE Instances", get("ade_instances")),
	}

	// Per-instance ADE token expiry rows
	for i, t := range adeTokens {
		label := ""
		if i == 0 {
			label = "ADE Token Expires"
		}
		display := t.Name + " — " + t.Value
		items = append(items, overviewItem{label, display, t.Color})
	}
	if len(adeTokens) == 0 {
		items = append(items, item("ADE Token Expires", "None configured"))
	}

	items = append(
		items,
		item("ADE Sync Status", get("ade_sync_status")),
		item("Computer Prestages", get("computer_prestages")),
		item("Mobile Device Prestages", get("md_prestages")),
	)

	// VPP / Volume Purchasing section
	items = append(items, item("VPP Locations", get("vpp_locations")))
	for i, t := range vppTokens {
		label := ""
		if i == 0 {
			label = "VPP Token Expires"
		}
		display := t.Name + " — " + t.Value
		items = append(items, overviewItem{label, display, t.Color})
	}

	items = append(
		items,
		overviewItem{}, // blank separator
		getItem("APNs Certificate", "apns_cert"),
		getItem("Built-in CA Expires", "ca_expires"),
		item("MDM Auto Renew (Computers)", get("mdm_renew_computer")),
		item("MDM Auto Renew (Mobile)", get("mdm_renew_mobile")),
	)

	return items
}

// printOverviewTable renders a grouped overview table with ANSI colors.
// The title parameter sets the header line (e.g., "INSTANCE OVERVIEW", "DEVICE DETAIL").
func printOverviewTable(w io.Writer, sections []overviewSection, useColor bool, title ...string) {
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

	const labelWidth = 34
	const totalWidth = 72

	// Title
	heading := "INSTANCE OVERVIEW"
	if len(title) > 0 && title[0] != "" {
		heading = title[0]
	}
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, colorize("  "+heading, bold))
	_, _ = fmt.Fprintln(w, colorize("  "+strings.Repeat("━", totalWidth), dim))

	for _, section := range sections {
		// Section header with dim separator
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintf(w, "  %s\n", colorize(section.Name, bold))
		_, _ = fmt.Fprintln(w, colorize("  "+strings.Repeat("─", totalWidth), dim))

		for _, item := range section.Items {
			// Empty item = blank separator line within a section
			if item.Resource == "" && item.Value == "" {
				_, _ = fmt.Fprintln(w)
				continue
			}

			displayValue := item.Value
			visibleLen := len(item.Value)

			switch {
			case item.ColorHint == "red":
				displayValue = colorize(item.Value+" ●", red)
				visibleLen += 2
			case item.ColorHint == "yellow":
				displayValue = colorize(item.Value+" ●", yellow)
				visibleLen += 2
			case item.Value == "ok" || item.Value == "OK" || item.Value == "ACCEPTED" || item.Value == "None":
				displayValue = colorize(item.Value+" ●", green)
				visibleLen += 2
			case item.Value == "enabled" || strings.HasPrefix(item.Value, "enabled ("):
				displayValue = colorize(item.Value+" ●", green)
				visibleLen += 2
			case strings.HasPrefix(item.Value, "SUCCESSFUL"):
				displayValue = colorize(item.Value+" ●", green)
				visibleLen += 2
			case item.Value == "disabled":
				displayValue = colorize(item.Value+" ○", dim)
				visibleLen += 2
			case item.Value == "offline" || strings.HasPrefix(item.Value, "HTTP"):
				displayValue = colorize(item.Value+" ●", red)
				visibleLen += 2
			case item.Value == "N/A" || item.Value == "Not configured" || item.Value == "None configured":
				displayValue = colorize(item.Value, dim)
			}

			// Right-align values; for long values or continuation lines
			// (empty resource label), left-align with label-width indent.
			padding := totalWidth - labelWidth - visibleLen
			if item.Resource == "" && padding < 1 {
				// Continuation line (e.g., alert types) — indent to label column
				_, _ = fmt.Fprintf(w, "  %*s%s\n", labelWidth, "", displayValue)
			} else if padding >= 1 {
				_, _ = fmt.Fprintf(w, "  %-*s%*s%s\n", labelWidth, item.Resource, padding, "", displayValue)
			} else {
				_, _ = fmt.Fprintf(w, "  %-*s %s\n", labelWidth, item.Resource, displayValue)
			}
		}
	}
	_, _ = fmt.Fprintln(w)
}

// overviewToRows flattens sections into []map[string]interface{} for structured output.
func overviewToRows(sections []overviewSection) []map[string]any {
	var rows []map[string]any
	for _, section := range sections {
		for _, item := range section.Items {
			row := map[string]any{
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

func newOverviewCmd(cliCtx *registry.CLIContext) *cobra.Command {
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

			// Table output uses custom grouped rendering (default for overview)
			if !cmd.Flags().Changed("output") || outputFmt == "table" {
				printOverviewTable(writerFor(cliCtx), sections, !noColor)
				return nil
			}

			// Structured formats delegate to the standard formatter
			rows := overviewToRows(sections)
			return printRows(cliCtx, rows)
		},
	}
}

// fetchPlatformOverview fetches Platform API metrics in parallel and returns
// a "Platform" overview section. Returns nil if all calls fail.
func fetchPlatformOverview(ctx context.Context, cliCtx *registry.CLIContext) *overviewSection {
	c := cliCtx.PlatformSDKClient
	bp := blueprints.New(c)
	cb := compliancebenchmarks.New(c)

	var mu sync.Mutex
	var wg sync.WaitGroup
	results := make(map[string]string)
	colorHints := make(map[string]string)

	send := func(key, value string) {
		mu.Lock()
		results[key] = value
		mu.Unlock()
	}
	sendColor := func(key, value, color string) {
		mu.Lock()
		results[key] = value
		if color != "" {
			colorHints[key] = color
		}
		mu.Unlock()
	}

	// Blueprints: list + count deployed
	wg.Go(func() {
		bps, err := bp.ListBlueprints(ctx, nil, "")
		if err != nil {
			send("bp_total", "N/A")
			return
		}
		deployed := 0
		for _, bp := range bps {
			if bp.DeploymentState != nil && bp.DeploymentState.State == "DEPLOYED" {
				deployed++
			}
		}
		send("bp_total", formatCount(float64(len(bps))))
		send("bp_deployed", formatCount(float64(deployed)))
	})

	// Compliance Benchmarks: list + average compliance
	wg.Go(func() {
		resp, err := cb.ListBenchmarks(ctx)
		if err != nil {
			send("cb_total", "N/A")
			return
		}
		benchmarks := resp.Benchmarks
		total := len(benchmarks)
		updatesAvailable := 0
		for _, b := range benchmarks {
			if b.UpdateAvailable {
				updatesAvailable++
			}
		}
		send("cb_total", formatCount(float64(total)))
		if updatesAvailable > 0 {
			sendColor("cb_updates", formatCount(float64(updatesAvailable)), "yellow")
		} else {
			send("cb_updates", "0")
		}

		// Fetch compliance percentage for each benchmark
		var totalPct float32
		var pctCount int
		for _, b := range benchmarks {
			pct, err := cb.GetBenchmarkCompliancePercentage(ctx, b.ID)
			if err != nil {
				continue
			}
			totalPct += pct.CompliancePercentage
			pctCount++
		}
		if pctCount > 0 {
			avgPct := totalPct / float32(pctCount)
			color := ""
			if avgPct < 80 {
				color = "red"
			} else if avgPct < 95 {
				color = "yellow"
			}
			sendColor("cb_compliance", fmt.Sprintf("%.1f%%", avgPct), color)
		} else {
			send("cb_compliance", "N/A")
		}
	})

	wg.Wait()

	get := func(key string) string {
		if v, ok := results[key]; ok {
			return v
		}
		return "N/A"
	}

	// Skip the section entirely if we got nothing
	if get("bp_total") == "N/A" && get("cb_total") == "N/A" {
		return nil
	}

	var items []overviewItem
	items = append(items, overviewItem{"Blueprints", get("bp_total"), ""})
	if get("bp_deployed") != "N/A" {
		items = append(items, overviewItem{"  Deployed", get("bp_deployed"), ""})
	}

	items = append(items, overviewItem{"Compliance Benchmarks", get("cb_total"), ""})
	if v := get("cb_updates"); v != "0" && v != "N/A" {
		items = append(items, overviewItem{"  Updates Available", v, colorHints["cb_updates"]})
	}
	if v := get("cb_compliance"); v != "N/A" {
		items = append(items, overviewItem{"  Overall Compliance", v, colorHints["cb_compliance"]})
	}

	return &overviewSection{
		Name:  "Platform",
		Items: items,
	}
}
