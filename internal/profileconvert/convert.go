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

// mcxPayloadType is the Apple payload type for the "Application & Custom
// Settings" payload (Managed Client / MCX). Its managed settings live under a
// nested PayloadContent dictionary rather than as sibling keys.
const mcxPayloadType = "com.apple.ManagedClient.preferences"

// DisabledPayloadTypes lists the Apple MDM payload types that Jamf Platform
// blueprints explicitly refuses. Sending one of these in a
// com.jamf.ddm-configuration-profile component makes the API reject the whole
// blueprint with a 400 "Payload disabled: <type>" (independent of the payload's
// keys). Every other real Apple payload type is accepted and validated against
// Apple's published schema by the API itself, so this is a denylist rather than
// an allowlist.
//
// Wire-probed against the blueprints API by POSTing every Apple MDM payloadtype
// (apple/device-management/mdm/profiles) 2026-07-17. This set is
// instance/version-specific and may drift — the API is the ultimate authority.
var DisabledPayloadTypes = map[string]bool{
	"com.apple.ADCertificate.managed":    true, // Active Directory Certificate
	"com.apple.DirectoryService.managed": true, // Directory Service (AD bind)
	"com.apple.MCX.FileVault2":           true, // FileVault 2 (legacy MCX)
	"com.apple.airplay":                  true, // AirPlay
	"com.apple.airplay.security":         true, // AirPlay Security
	"com.apple.cellular":                 true, // Cellular
	"com.apple.dnsSettings.managed":      true, // DNS Settings
	"com.apple.education":                true, // Education
	"com.apple.ews.account":              true, // Exchange Web Services account
	"com.apple.extensiblesso":            true, // Extensible Single Sign-On
	"com.apple.font":                     true, // Font
	"com.apple.profileRemovalPassword":   true, // Profile Removal Password
	"com.apple.proxy.http.global":        true, // Global HTTP Proxy
	"com.apple.security.pem":             true, // Certificate (PEM)
	"com.apple.security.pkcs1":           true, // Certificate (PKCS1)
	"com.apple.security.pkcs12":          true, // Certificate (PKCS12)
	"com.apple.security.root":            true, // Certificate (root)
	"com.apple.security.scep":            true, // SCEP
	"com.apple.vpn.managed":              true, // VPN
	"com.apple.vpn.managed.appmapping":   true, // Per-App VPN mapping
	"com.apple.webClip.managed":          true, // Web Clip
	"com.apple.webcontent-filter":        true, // Web Content Filter
}

// SupportedPayloadTypes lists the payload types the blueprints
// com.jamf.ddm-configuration-profile component accepts as standalone payloads.
//
// The component matches payloadType against a fixed registry — it does NOT
// validate arbitrary Apple payloads. Wire-probed by POSTing a blueprint carrying
// one bare payload ({payloadType, payloadIdentifier}) per type: a type outside
// this set is rejected with the opaque error
//
//	steps[0].components[0].configuration: Failed to validate configuration.
//
// which is byte-for-byte what an invented payload type produces. Keys are barely
// validated by comparison — a made-up key, or a string where the schema wants a
// boolean, is accepted for most types — so an import that fails validation is
// almost always carrying a payload type from outside this set.
//
// A type outside this set is still deliverable: wrapped in a
// com.apple.ManagedClient.preferences (Custom Settings/MCX) payload, the API
// accepts any preference domain, including third-party ones. See
// wrapAsManagedPreferences — that is the recovery path, not a filter.
//
// To re-derive after an API change, probe each candidate type bare and treat
// "Failed to validate configuration." as unsupported. Types the API knows but
// deliberately blocks report "Payload disabled: <type>" instead and belong in
// DisabledPayloadTypes.
var SupportedPayloadTypes = map[string]bool{
	"com.apple.AssetCache.managed":                true,
	"com.apple.Dictionary":                        true,
	"com.apple.DiscRecording":                     true,
	"com.apple.MCX.Accounts":                      true,
	"com.apple.MCX.EnergySaver":                   true,
	"com.apple.MCX.MobileAccounts":                true,
	"com.apple.MCX.TimeMachine":                   true,
	"com.apple.MCX.TimeServer":                    true,
	"com.apple.ManagedClient.preferences":         true,
	"com.apple.NSExtension":                       true,
	"com.apple.SetupAssistant.managed":            true,
	"com.apple.SystemConfiguration":               true,
	"com.apple.TCC.configuration-profile-policy":  true,
	"com.apple.airprint":                          true,
	"com.apple.app.lock":                          true,
	"com.apple.applicationaccess":                 true,
	"com.apple.applicationaccess.new":             true,
	"com.apple.appstore":                          true,
	"com.apple.asam":                              true,
	"com.apple.associated-domains":                true,
	"com.apple.cellularprivatenetwork.managed":    true,
	"com.apple.conferenceroomdisplay":             true,
	"com.apple.desktop":                           true,
	"com.apple.dnsProxy.managed":                  true,
	"com.apple.dock":                              true,
	"com.apple.domains":                           true,
	"com.apple.familycontrols.contentfilter":      true,
	"com.apple.familycontrols.timelimits.v2":      true,
	"com.apple.fileproviderd":                     true,
	"com.apple.finder":                            true,
	"com.apple.firstactiveethernet.managed":       true,
	"com.apple.firstethernet.managed":             true,
	"com.apple.gamed":                             true,
	"com.apple.globalethernet.managed":            true,
	"com.apple.homescreenlayout":                  true,
	"com.apple.loginitems.managed":                true,
	"com.apple.loginwindow":                       true,
	"com.apple.lom":                               true,
	"com.apple.mcxMenuExtras":                     true,
	"com.apple.mcxprinting":                       true,
	"com.apple.networkusagerules":                 true,
	"com.apple.notificationsettings":              true,
	"com.apple.preference.security":               true,
	"com.apple.preference.users":                  true,
	"com.apple.relay.managed":                     true,
	"com.apple.screensaver":                       true,
	"com.apple.screensaver.user":                  true,
	"com.apple.secondactiveethernet.managed":      true,
	"com.apple.secondethernet.managed":            true,
	"com.apple.security.FDERecoveryKeyEscrow":     true,
	"com.apple.security.acme":                     true,
	"com.apple.security.certificatepreference":    true,
	"com.apple.security.certificaterevocation":    true,
	"com.apple.security.certificatetransparency":  true,
	"com.apple.security.firewall":                 true,
	"com.apple.security.identitypreference":       true,
	"com.apple.security.smartcard":                true,
	"com.apple.servicemanagement":                 true,
	"com.apple.shareddeviceconfiguration":         true,
	"com.apple.syspolicy.kernel-extension-policy": true,
	"com.apple.system-extension-policy":           true,
	"com.apple.systemmigration":                   true,
	"com.apple.systempolicy.control":              true,
	"com.apple.systempolicy.managed":              true,
	"com.apple.systempolicy.rule":                 true,
	"com.apple.thirdactiveethernet.managed":       true,
	"com.apple.thirdethernet.managed":             true,
	"com.apple.tvremote":                          true,
	"com.apple.universalaccess":                   true,
	"com.apple.vpn.managed.applayer":              true,
	"com.apple.wifi.managed":                      true,
	"com.apple.xsan":                              true,
	"com.apple.xsan.preferences":                  true,
	"loginwindow":                                 true,
}

// canonicalPayloadTypes maps payload type spellings Jamf Pro writes into Classic
// configuration profiles onto the spelling the blueprints registry keys on.
//
// Jamf Pro's User Preferences payload is written as com.apple.preferences.users,
// which is the *filename* Apple publishes that payload's schema under; Apple's
// declared payloadtype — and the only spelling the API accepts — is
// com.apple.preference.users (singular).
var canonicalPayloadTypes = map[string]string{
	"com.apple.preferences.users": "com.apple.preference.users",
}

// CanonicalPayloadType returns the payload type spelling the blueprints API
// expects, rewriting the Jamf Pro variants in canonicalPayloadTypes. Types with
// no known variant are returned unchanged.
func CanonicalPayloadType(payloadType string) string {
	if canonical, ok := canonicalPayloadTypes[payloadType]; ok {
		return canonical
	}
	return payloadType
}

// wrapAsManagedPreferences packages a preference domain's settings as a
// com.apple.ManagedClient.preferences (Custom Settings/MCX) payload, the form the
// blueprints API accepts for any domain — including the payload types outside
// SupportedPayloadTypes and third-party domains it has never heard of. This is
// also the correct legacy delivery for such domains: they are managed preferences
// rather than standalone MDM payloads.
//
// PayloadContent keeps Apple's capitalisation because the API stores it verbatim.
func wrapAsManagedPreferences(payloadType string, settings map[string]any, index int) map[string]any {
	return map[string]any{
		"payloadType":       mcxPayloadType,
		"payloadIdentifier": generatePayloadIdentifier(payloadType, index),
		"PayloadContent": map[string]any{
			payloadType: map[string]any{
				"Forced": []any{
					map[string]any{"mcx_preference_settings": settings},
				},
			},
		},
	}
}

// UIManageablePayloadTypes lists the legacy payload types the Jamf Pro UI can
// manage directly (the "Legacy payload" picker). The blueprints API accepts
// many more payload types than these, but any accepted type NOT in this set is
// delivered as an API-only component: it shows as a read-only "Legacy payload"
// item in the UI and can only be edited through the blueprints API.
//
// Sourced from the Jamf Pro blueprints UI legacy-payload list. Used only to
// tailor the API-only advisory on import — it is not a filter, so if it drifts
// the worst case is a slightly inaccurate warning, not a broken import.
var UIManageablePayloadTypes = map[string]bool{
	"com.apple.Dictionary":                        true, // Parental Controls: Dictionary
	"com.apple.DiscRecording":                     true, // Media Management: Disc Burning
	"com.apple.MCX.Accounts":                      true, // Accounts
	"com.apple.MCX.MobileAccounts":                true, // Mobile Accounts
	"com.apple.MCX.TimeMachine":                   true, // Time Machine
	"com.apple.MCX.TimeServer":                    true, // Time Server
	"com.apple.NSExtension":                       true, // NSExtension Management
	"com.apple.SystemConfiguration":               true, // Network Proxy Configuration
	"com.apple.TCC.configuration-profile-policy":  true, // Privacy Preferences Policy Control
	"com.apple.airprint":                          true, // AirPrint
	"com.apple.app.lock":                          true, // App Lock
	"com.apple.applicationaccess":                 true, // Restrictions
	"com.apple.appstore":                          true, // App Store
	"com.apple.asam":                              true, // Autonomous Single App Mode
	"com.apple.cellularprivatenetwork.managed":    true, // Cellular Private Network
	"com.apple.conferenceroomdisplay":             true, // Conference Room Display
	"com.apple.desktop":                           true, // Desktop
	"com.apple.dnsProxy.managed":                  true, // DNS Proxy
	"com.apple.domains":                           true, // Domains
	"com.apple.familycontrols.contentfilter":      true, // Parental Controls: Content Filter
	"com.apple.fileproviderd":                     true, // File Provider
	"com.apple.finder":                            true, // Finder
	"com.apple.gamed":                             true, // Parental Controls: Game Center
	"com.apple.loginitems.managed":                true, // Login Items: Managed Items
	"com.apple.loginwindow":                       true, // Login Window
	"com.apple.mcxprinting":                       true, // Printing
	"com.apple.notificationsettings":              true, // Notifications
	"com.apple.preference.security":               true, // Security Preferences
	"com.apple.preference.users":                  true, // User Preferences
	"com.apple.screensaver":                       true, // Screensaver
	"com.apple.screensaver.user":                  true, // Screensaver User
	"com.apple.security.firewall":                 true, // Firewall
	"com.apple.security.smartcard":                true, // SmartCard
	"com.apple.servicemanagement":                 true, // Service Management - Managed Login Items
	"com.apple.shareddeviceconfiguration":         true, // Lock Screen Message
	"com.apple.syspolicy.kernel-extension-policy": true, // System Policy - Kernel Extensions
	"com.apple.systempolicy.control":              true, // System Policy Control
	"com.apple.systempolicy.managed":              true, // System Policy Managed
	"com.apple.tvremote":                          true, // TV Remote
	"com.apple.universalaccess":                   true, // Accessibility
	"loginwindow":                                 true, // Login Window: Login Items
}

// APIOnlyPayloadTypes returns, in sorted order, the payload types present in a
// DDMProfileDto configuration that the Jamf Pro UI cannot manage directly —
// i.e. accepted by the blueprints API but not in UIManageablePayloadTypes.
// These render as read-only "Legacy payload" items in the UI. Returns nil when
// every payload in the config is UI-manageable.
func APIOnlyPayloadTypes(config json.RawMessage) []string {
	_, content, ok := parsePayloadContent(config)
	if !ok {
		return nil
	}
	seen := make(map[string]bool)
	var apiOnly []string
	for _, item := range content {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		pt, _ := entry["payloadType"].(string)
		if pt == "" || UIManageablePayloadTypes[pt] || seen[pt] {
			continue
		}
		seen[pt] = true
		apiOnly = append(apiOnly, pt)
	}
	sort.Strings(apiOnly)
	return apiOnly
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

	var warnings []string
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
		if canonical := CanonicalPayloadType(payloadType); canonical != payloadType {
			warnings = append(warnings, fmt.Sprintf("rewrote payload type %q to %q — the blueprints API only accepts Apple's canonical spelling", payloadType, canonical))
			payloadType = canonical
		}

		if DisabledPayloadTypes[payloadType] {
			if filterUnsupported {
				warnings = append(warnings, fmt.Sprintf("skipped payload type %q — Jamf blueprints does not support it", payloadType))
				continue
			}
			warnings = append(warnings, fmt.Sprintf("payload type %q is disabled by Jamf blueprints — the API will reject the blueprint (remove --include-unsupported to skip it)", payloadType))
		}

		idx := typeCount[payloadType]
		typeCount[payloadType]++
		entry := buildPayloadEntry(payloadType, payload, idx)
		// Payload types outside the component's registry are rejected as standalone
		// payloads but accepted as Custom Settings, which is also their correct
		// legacy delivery. Wrap rather than lose the settings.
		if !SupportedPayloadTypes[payloadType] && !DisabledPayloadTypes[payloadType] {
			settings := settingsFromEntry(entry)
			if len(settings) == 0 {
				warnings = append(warnings, fmt.Sprintf("removed empty payload %q — no settings after metadata stripping", payloadType))
				typeCount[payloadType]--
				continue
			}
			warnings = append(warnings, fmt.Sprintf("payload type %q is not a standalone blueprints payload — delivering it as Custom Settings (MCX)", payloadType))
			entry = wrapAsManagedPreferences(payloadType, settings, idx)
		}
		payloads = append(payloads, entry)
	}

	if len(payloads) == 0 {
		return nil, warnings, fmt.Errorf("no payloads remain after skipping types blueprints does not support")
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
	if canonical := CanonicalPayloadType(payloadType); canonical != payloadType {
		warnings = append(warnings, fmt.Sprintf("rewrote payload type %q to %q — the blueprints API only accepts Apple's canonical spelling", payloadType, canonical))
		payloadType = canonical
	}
	if DisabledPayloadTypes[payloadType] {
		warnings = append(warnings, fmt.Sprintf("payload type %q is disabled by Jamf blueprints — the API will reject it", payloadType))
	}

	// All keys in a raw plist are settings (no Apple metadata to strip).
	// Empty values are removed since the DDM API rejects them.
	clean := make(map[string]any, len(settings))
	for k, v := range settings {
		converted := convertPlistValue(v)
		if isEmptyValue(converted) {
			continue
		}
		clean[k] = converted
	}

	// A domain the component's registry doesn't know — which is every third-party
	// domain, the common case for a raw plist — is only deliverable wrapped as
	// Custom Settings.
	var entry map[string]any
	if !SupportedPayloadTypes[payloadType] && !DisabledPayloadTypes[payloadType] {
		warnings = append(warnings, fmt.Sprintf("payload type %q is not a standalone blueprints payload — delivering it as Custom Settings (MCX)", payloadType))
		entry = wrapAsManagedPreferences(payloadType, clean, 0)
	} else {
		entry = map[string]any{
			"payloadType":       payloadType,
			"payloadIdentifier": generatePayloadIdentifier(payloadType, 0),
		}
		maps.Copy(entry, clean)
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
	var types []string
	for _, item := range content {
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
// settingsFromEntry returns the setting keys of a built payload entry, dropping
// the structural keys. Used when an already-built entry has to be re-packaged as
// a Custom Settings payload.
func settingsFromEntry(entry map[string]any) map[string]any {
	settings := make(map[string]any, len(entry))
	for k, v := range entry {
		if k == "payloadType" || k == "payloadIdentifier" {
			continue
		}
		settings[k] = v
	}
	return settings
}

func buildPayloadEntry(payloadType string, payload map[string]any, index int) map[string]any {
	entry := map[string]any{
		"payloadType":       payloadType,
		"payloadIdentifier": generatePayloadIdentifier(payloadType, index),
	}

	// com.apple.ManagedClient.preferences (the "Application & Custom Settings"
	// payload, a.k.a. MCX) carries its managed settings under PayloadContent —
	// which is otherwise an Apple metadata key stripped from every payload.
	// Preserve it intact so the API receives the full Managed Preferences
	// structure it expects. The blueprints API rejects the alternative of
	// unwrapping the inner preference domain into a bare payloadType.
	if payloadType == mcxPayloadType {
		if pc, ok := payload["PayloadContent"]; ok {
			if converted := convertPlistValue(pc); !isEmptyValue(converted) {
				entry["PayloadContent"] = converted
			}
		}
		return entry
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
