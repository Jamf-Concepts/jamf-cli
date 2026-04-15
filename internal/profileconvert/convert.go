// Copyright 2026, Jamf Software LLC

package profileconvert

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"maps"
	"sort"
	"strings"

	"howett.net/plist"
)

// ConflictWarning is printed to stderr when converting profiles to blueprint components.
const ConflictWarning = `Warning: If you previously deployed a configuration profile to a device and then deploy
a blueprint that contains conflicting keys, unexpected behavior may occur. For example,
if keys within the Restrictions payload conflict, the most restrictive setting will take
precedence.`

// SupportedPayloadTypes lists legacy payload types supported by Jamf Platform blueprints.
// Sourced from https://learn.jamf.com/r/en-US/jamf-pro-blueprints-configuration-guide/Blueprints_Release_Notes_Pro
var SupportedPayloadTypes = map[string]bool{
	"com.apple.Dictionary":                       true, // Parental Controls: Dictionary
	"com.apple.DiscRecording":                    true, // Media Management: Disc Burning
	"com.apple.MCX.Accounts":                     true, // Accounts
	"com.apple.MCX.MobileAccounts":               true, // Mobile Accounts
	"com.apple.MCX.TimeMachine":                  true, // Time Machine
	"com.apple.MCX.TimeServer":                   true, // Time Server
	"com.apple.NSExtension":                      true, // NSExtension Management
	"com.apple.SystemConfiguration":              true, // Network Proxy Configuration
	"com.apple.TCC.configuration-profile-policy": true, // Privacy Preferences Policy Control
	"com.apple.airprint":                         true, // AirPrint
	"com.apple.app.lock":                         true, // App Lock
	"com.apple.applicationaccess":                true, // Restrictions
	"com.apple.appstore":                         true, // App Store
	"com.apple.asam":                             true, // Autonomous Single App Mode
	"com.apple.cellularprivatenetwork.managed":   true, // Cellular Private Network
	"com.apple.conferenceroomdisplay":            true, // Conference Room Display
	"com.apple.desktop":                          true, // Desktop
	"com.apple.dnsProxy.managed":                 true, // DNS Proxy
	"com.apple.domains":                          true, // Domains
	"com.apple.familycontrols.contentfilter":     true, // Parental Controls: Content Filter
	"com.apple.fileproviderd":                    true, // File Provider
	"com.apple.finder":                           true, // Finder
	"com.apple.gamed":                            true, // Parental Controls: Game Center
	"com.apple.loginitems.managed":               true, // Login Items: Managed Items
	"com.apple.loginwindow":                      true, // Login Window
	"com.apple.mcxprinting":                      true, // Printing
	"com.apple.notificationsettings":             true, // Notifications
	"com.apple.preference.security":              true, // Security Preferences
	"com.apple.preference.users":                 true, // User Preferences
	"com.apple.screensaver":                      true, // Screensaver
	"com.apple.screensaver.user":                 true, // Screensaver User
	"com.apple.security.firewall":                true, // Firewall
	"com.apple.security.smartcard":               true, // SmartCard
	"com.apple.servicemanagement":                true, // Service Management
	"com.apple.shareddeviceconfiguration":        true, // Lock Screen Message
	"com.apple.system.logging":                   true, // System Logging
	"com.apple.systempolicy.control":             true, // System Policy Control
	"com.apple.systempolicy.managed":             true, // System Policy Managed
	"com.apple.tvremote":                         true, // TV Remote
	"com.apple.universalaccess":                  true, // Accessibility
	"loginwindow":                                true, // Login Window: Login Items
}

// appleMetadataKeys are keys in a mobileconfig payload dict that represent
// Apple profile metadata rather than preference domain settings.
var appleMetadataKeys = map[string]bool{
	"PayloadType":                      true,
	"PayloadDisplayName":               true,
	"PayloadIdentifier":                true,
	"PayloadUUID":                      true,
	"PayloadVersion":                   true,
	"PayloadOrganization":              true,
	"PayloadDescription":               true,
	"PayloadRemovalDisallowed":         true,
	"PayloadScope":                     true,
	"PayloadEnabled":                   true,
	"PayloadContent":                   true,
	"PayloadExpirationDate":            true,
	"ConsentText":                      true,
	"DurationUntilRemoval":             true,
	"RemovalDate":                      true,
	"TargetDeviceType":                 true,
	"HasRemovalPasscode":               true,
	"IsEncrypted":                      true,
	"IsSupervised":                     true,
	"PayloadContentManagedPreferences": true,
}

// ConvertMobileconfig parses a mobileconfig (XML plist) and returns a
// DDMProfileDto configuration suitable for a com.jamf.ddm-configuration-profile
// blueprint component. When filterUnsupported is true, payloads with unsupported
// types are silently removed (with a warning). Otherwise they are included and
// the API will validate them.
func ConvertMobileconfig(data []byte, filterUnsupported bool) (json.RawMessage, []string, error) {
	var profile map[string]any
	if _, err := plist.Unmarshal(data, &profile); err != nil {
		return nil, nil, fmt.Errorf("parsing mobileconfig: %w", err)
	}

	displayName, _ := profile["PayloadDisplayName"].(string)
	if displayName == "" {
		return nil, nil, fmt.Errorf("mobileconfig has no PayloadDisplayName")
	}

	payloadContent, ok := profile["PayloadContent"].([]any)
	if !ok || len(payloadContent) == 0 {
		return nil, nil, fmt.Errorf("mobileconfig has no PayloadContent array")
	}

	// Unwrap MCX (Custom Settings) payloads before processing
	payloadContent, mcxWarnings := unwrapMCXPayloads(payloadContent)

	warnings := append([]string(nil), mcxWarnings...)
	payloads := make([]map[string]any, 0, len(payloadContent))
	typeCount := make(map[string]int) // tracks per-type index for unique identifiers

	for i, item := range payloadContent {
		payload, ok := item.(map[string]any)
		if !ok {
			return nil, nil, fmt.Errorf("PayloadContent[%d] is not a dictionary", i)
		}

		payloadType, _ := payload["PayloadType"].(string)
		if payloadType == "" {
			return nil, nil, fmt.Errorf("PayloadContent[%d] has no PayloadType", i)
		}

		if !SupportedPayloadTypes[payloadType] {
			if filterUnsupported {
				warnings = append(warnings, fmt.Sprintf("removed unsupported payload type %q", payloadType))
				continue
			}
			warnings = append(warnings, fmt.Sprintf("payload type %q may not be supported — see https://github.com/apple/device-management/tree/release/mdm/profiles", payloadType))
		}

		idx := typeCount[payloadType]
		typeCount[payloadType]++
		entry := buildPayloadEntry(payloadType, payload, idx)
		payloads = append(payloads, entry)
	}

	if len(payloads) == 0 {
		return nil, warnings, fmt.Errorf("no supported payloads remain after filtering")
	}

	config := map[string]any{
		"payloadDisplayName": displayName,
		"payloadContent":     payloads,
	}

	result, err := marshalConfig(config)
	if err != nil {
		return nil, nil, err
	}
	return result, warnings, nil
}

// ConvertPlist parses a raw preference domain plist and wraps it as a
// single-payload DDMProfileDto configuration. The payloadType must be the
// Apple preference domain (e.g. "com.apple.dock").
func ConvertPlist(data []byte, payloadType, displayName string) (json.RawMessage, []string, error) {
	var settings map[string]any
	if _, err := plist.Unmarshal(data, &settings); err != nil {
		return nil, nil, fmt.Errorf("parsing plist: %w", err)
	}

	var warnings []string
	if !SupportedPayloadTypes[payloadType] {
		warnings = append(warnings, fmt.Sprintf("payload type %q may not be supported — see https://github.com/apple/device-management/tree/release/mdm/profiles", payloadType))
	}

	// All keys in a raw plist are settings (no Apple metadata to strip).
	// Empty values are removed since the DDM API rejects them.
	entry := map[string]any{
		"payloadType":       payloadType,
		"payloadIdentifier": generatePayloadIdentifier(payloadType, 0),
	}
	for k, v := range settings {
		converted := convertPlistValue(v)
		if isEmptyValue(converted) {
			continue
		}
		entry[k] = converted
	}

	if displayName == "" {
		displayName = payloadType
	}

	config := map[string]any{
		"payloadDisplayName": displayName,
		"payloadContent":     []map[string]any{entry},
	}

	result, err := marshalConfig(config)
	if err != nil {
		return nil, nil, err
	}
	return result, warnings, nil
}

// ProfileDisplayName extracts the PayloadDisplayName from a mobileconfig
// without doing a full conversion. Useful for deriving blueprint names.
func ProfileDisplayName(data []byte) string {
	var profile map[string]any
	if _, err := plist.Unmarshal(data, &profile); err != nil {
		return ""
	}
	name, _ := profile["PayloadDisplayName"].(string)
	return name
}

// PayloadTypeSummary returns a human-readable summary of payload types in a mobileconfig.
func PayloadTypeSummary(data []byte) []string {
	var profile map[string]any
	if _, err := plist.Unmarshal(data, &profile); err != nil {
		return nil
	}
	content, ok := profile["PayloadContent"].([]any)
	if !ok {
		return nil
	}
	// Unwrap MCX payloads so the count reflects effective payloads
	unwrapped, _ := unwrapMCXPayloads(content)
	var types []string
	for _, item := range unwrapped {
		if payload, ok := item.(map[string]any); ok {
			if pt, ok := payload["PayloadType"].(string); ok {
				types = append(types, pt)
			}
		}
	}
	return types
}

// buildPayloadEntry constructs a single payload entry for the DDMProfileDto
// from a mobileconfig payload dictionary. Apple metadata keys are stripped,
// empty values (empty strings and empty arrays) are removed since the DDM API
// rejects them, and the payloadIdentifier is generated deterministically.
// The index disambiguates multiple payloads of the same type.
func buildPayloadEntry(payloadType string, payload map[string]any, index int) map[string]any {
	entry := map[string]any{
		"payloadType":       payloadType,
		"payloadIdentifier": generatePayloadIdentifier(payloadType, index),
	}

	for k, v := range payload {
		if appleMetadataKeys[k] {
			continue
		}
		converted := convertPlistValue(v)
		if isEmptyValue(converted) {
			continue
		}
		entry[k] = converted
	}

	return entry
}

// isEmptyValue returns true for values that are always invalid in DDM profile
// configuration: empty strings and empty arrays. These are artifacts of the
// Classic UI that the DDM API rejects with validation errors.
func isEmptyValue(v any) bool {
	switch val := v.(type) {
	case string:
		return val == ""
	case []any:
		return len(val) == 0
	default:
		return false
	}
}

// convertPlistValue normalises plist-decoded values for JSON marshalling.
// howett.net/plist decodes <data> as []byte which json.Marshal would base64-encode,
// but the API expects these as raw byte arrays rarely — most config profile
// settings are strings/ints/bools. We keep []byte as-is (base64 in JSON).
// uint64 values (from plist) are converted to float64 for JSON compatibility.
func convertPlistValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, inner := range val {
			out[k] = convertPlistValue(inner)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, inner := range val {
			out[i] = convertPlistValue(inner)
		}
		return out
	case uint64:
		return float64(val)
	default:
		return v
	}
}

// generatePayloadIdentifier creates a deterministic identifier from a payload type
// and index using SHA256. The index disambiguates multiple payloads of the same
// type within a single mobileconfig (e.g. two com.apple.wifi.managed payloads).
func generatePayloadIdentifier(payloadType string, index int) string {
	input := payloadType
	if index > 0 {
		input = fmt.Sprintf("%s.%d", payloadType, index)
	}
	hash := sha256.Sum256([]byte(input))
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		hash[0:4], hash[4:6], hash[6:8], hash[8:10], hash[10:16])
}

// marshalConfig marshals a configuration map to indented JSON.
func marshalConfig(config map[string]any) (json.RawMessage, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(config); err != nil {
		return nil, fmt.Errorf("encoding configuration: %w", err)
	}
	return json.RawMessage(bytes.TrimRight(buf.Bytes(), "\n")), nil
}

// FormatComponentJSON wraps a configuration in a complete component block
// with the com.jamf.ddm-configuration-profile identifier.
func FormatComponentJSON(config json.RawMessage) ([]byte, error) {
	block := map[string]any{
		"identifier":    "com.jamf.ddm-configuration-profile",
		"configuration": json.RawMessage(config),
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(block); err != nil {
		return nil, fmt.Errorf("encoding component: %w", err)
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// ValidatePayloads checks each payload in a DDMProfileDto configuration against
// Apple's schema and removes payloads that would be rejected by the DDM API
// (e.g. missing required fields, or payloads with no setting keys). This does
// not strip defaults — it only validates structural correctness.
//
// Call this before uploading to the API even when --strip-defaults is not used.
func ValidatePayloads(config json.RawMessage, fetcher *SchemaFetcher) (json.RawMessage, []string) {
	parsed, content, ok := parsePayloadContent(config)
	if !ok {
		return config, nil
	}

	var messages []string
	remaining := make([]any, 0, len(content))
	for _, item := range content {
		entry, payloadType, ok := extractPayloadEntry(item)
		if !ok {
			remaining = append(remaining, item)
			continue
		}

		defaults := fetchDefaultsQuiet(fetcher, payloadType)
		if defaults == nil {
			remaining = append(remaining, item)
			continue
		}

		if missing := MissingRequiredKeys(entry, defaults); len(missing) > 0 {
			them := "it"
			if len(missing) > 1 {
				them = "them"
			}
			messages = append(messages, fmt.Sprintf("removed payload %s — the DDM API requires %s but the source profile did not include %s",
				payloadType, strings.Join(missing, ", "), them))
			continue
		}

		remaining = append(remaining, item)
	}
	parsed["payloadContent"] = remaining

	result, err := marshalConfig(parsed)
	if err != nil {
		return config, messages
	}
	return result, messages
}

// StripConfigDefaults removes keys from each payload in a DDMProfileDto
// configuration whose values match Apple's published defaults. This reduces
// noise from profiles that set every key even when the value is the Apple
// default (common with Jamf Pro's UI). The fetcher is used to retrieve Apple's
// schema for each payload type. Also validates and removes broken payloads
// (empty payloads, missing required fields).
//
// Returns the modified configuration and a list of human-readable messages
// about what was stripped.
func StripConfigDefaults(config json.RawMessage, fetcher *SchemaFetcher) (json.RawMessage, []string) {
	parsed, content, ok := parsePayloadContent(config)
	if !ok {
		return config, nil
	}

	var messages []string
	remaining := make([]any, 0, len(content))
	for _, item := range content {
		entry, payloadType, ok := extractPayloadEntry(item)
		if !ok {
			remaining = append(remaining, item)
			continue
		}

		defaults, err := fetcher.FetchDefaults(payloadType)
		if err != nil {
			messages = append(messages, fmt.Sprintf("could not fetch Apple schema for %s: %v", payloadType, err))
			remaining = append(remaining, item)
			continue
		}
		if defaults == nil {
			messages = append(messages, fmt.Sprintf("no Apple schema available for %s — skipping default stripping", payloadType))
			remaining = append(remaining, item)
			continue
		}

		count, stripped := StripDefaultKeys(entry, defaults)
		if count > 0 {
			messages = append(messages, fmt.Sprintf("stripped %d default-value key(s) from %s: %s",
				count, payloadType, strings.Join(stripped, ", ")))
		}

		if payloadIsEmpty(entry) {
			messages = append(messages, fmt.Sprintf("removed payload %s — all keys were at default values", payloadType))
			continue
		}

		if missing := MissingRequiredKeys(entry, defaults); len(missing) > 0 {
			them := "it"
			if len(missing) > 1 {
				them = "them"
			}
			messages = append(messages, fmt.Sprintf("removed payload %s — the DDM API requires %s but the source profile did not include %s",
				payloadType, strings.Join(missing, ", "), them))
			continue
		}

		remaining = append(remaining, item)
	}
	parsed["payloadContent"] = remaining

	result, err := marshalConfig(parsed)
	if err != nil {
		return config, messages
	}
	return result, messages
}

// parsePayloadContent unmarshals config JSON and extracts the payloadContent array.
func parsePayloadContent(config json.RawMessage) (map[string]any, []any, bool) {
	var parsed map[string]any
	if err := json.Unmarshal(config, &parsed); err != nil {
		return nil, nil, false
	}
	contentRaw, ok := parsed["payloadContent"]
	if !ok {
		return nil, nil, false
	}
	content, ok := contentRaw.([]any)
	if !ok {
		return nil, nil, false
	}
	return parsed, content, true
}

// extractPayloadEntry extracts a typed payload entry from a content item.
func extractPayloadEntry(item any) (map[string]any, string, bool) {
	entry, ok := item.(map[string]any)
	if !ok {
		return nil, "", false
	}
	payloadType, _ := entry["payloadType"].(string)
	if payloadType == "" {
		return nil, "", false
	}
	return entry, payloadType, true
}

// fetchDefaultsQuiet fetches schema defaults, returning nil silently on errors.
func fetchDefaultsQuiet(fetcher *SchemaFetcher, payloadType string) *SchemaDefaults {
	defaults, err := fetcher.FetchDefaults(payloadType)
	if err != nil || defaults == nil {
		return nil
	}
	return defaults
}

// ConfigHasPayloads returns an error if the configuration has an empty
// payloadContent array, which can happen after StripConfigDefaults removes
// all payloads.
func ConfigHasPayloads(config json.RawMessage) error {
	var parsed map[string]any
	if err := json.Unmarshal(config, &parsed); err != nil {
		return nil // let downstream handle parse errors
	}
	content, ok := parsed["payloadContent"].([]any)
	if !ok {
		return nil
	}
	if len(content) == 0 {
		return fmt.Errorf("no payloads remain after stripping defaults — the profile only contained keys at their Apple default values")
	}
	return nil
}

// unwrapMCXPayloads expands any com.apple.ManagedClient.preferences payloads
// (Custom Settings / MCX) into synthetic payloads keyed by their inner
// preference domain. Each domain in the MCX wrapper becomes a standalone
// payload with PayloadType set to the domain identifier and settings extracted
// from the Forced / Set-Once mcx_preference_settings dictionaries.
//
// Non-MCX payloads pass through unchanged.
func unwrapMCXPayloads(payloads []any) ([]any, []string) {
	var result []any
	var warnings []string

	for _, item := range payloads {
		payload, ok := item.(map[string]any)
		if !ok {
			result = append(result, item)
			continue
		}

		payloadType, _ := payload["PayloadType"].(string)
		if payloadType != "com.apple.ManagedClient.preferences" {
			result = append(result, item)
			continue
		}

		// MCX payload — unwrap inner domains from PayloadContent dict
		content, ok := payload["PayloadContent"].(map[string]any)
		if !ok {
			warnings = append(warnings,
				"com.apple.ManagedClient.preferences payload has no PayloadContent dictionary — skipping")
			continue
		}

		unwrapped := 0
		domains := make([]string, 0, len(content))
		for domain := range content {
			domains = append(domains, domain)
		}
		sort.Strings(domains)
		for _, domain := range domains {
			domainData := content[domain]
			domainDict, ok := domainData.(map[string]any)
			if !ok {
				continue
			}

			settings := extractMCXSettings(domainDict)
			if len(settings) == 0 {
				continue
			}

			// Build a synthetic payload that looks like a native payload
			synthetic := map[string]any{
				"PayloadType": domain,
			}
			// Copy Apple metadata from the MCX wrapper (except PayloadType and PayloadContent)
			for k, v := range payload {
				if k == "PayloadType" || k == "PayloadContent" {
					continue
				}
				if appleMetadataKeys[k] {
					synthetic[k] = v
				}
			}
			maps.Copy(synthetic, settings)
			result = append(result, synthetic)
			unwrapped++
		}

		if unwrapped > 0 {
			warnings = append(warnings,
				fmt.Sprintf("unwrapped %d domain(s) from Custom Settings (com.apple.ManagedClient.preferences) payload", unwrapped))
		} else {
			warnings = append(warnings,
				"com.apple.ManagedClient.preferences payload had no extractable settings — skipping")
		}
	}

	return result, warnings
}

// extractMCXSettings extracts preference settings from an MCX domain
// dictionary. MCX stores settings under Forced and/or Set-Once arrays,
// each containing dicts with an mcx_preference_settings key.
func extractMCXSettings(domainDict map[string]any) map[string]any {
	settings := make(map[string]any)
	for _, arrayKey := range []string{"Set-Once", "Forced"} {
		arr, ok := domainDict[arrayKey].([]any)
		if !ok {
			continue
		}
		for _, entry := range arr {
			entryDict, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			prefs, ok := entryDict["mcx_preference_settings"].(map[string]any)
			if !ok {
				continue
			}
			maps.Copy(settings, prefs)
		}
	}
	return settings
}

// SupportedPayloadTypesList returns the supported payload types as a sorted slice
// for shell completion and help text.
func SupportedPayloadTypesList() []string {
	types := make([]string, 0, len(SupportedPayloadTypes))
	for t := range SupportedPayloadTypes {
		types = append(types, t)
	}
	sort.Strings(types)
	return types
}
