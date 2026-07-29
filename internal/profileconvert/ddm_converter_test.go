// Copyright 2026, Jamf Software LLC

package profileconvert

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// --- Passcode converter tests ---

func TestConvertPasscode_AllKeys(t *testing.T) {
	settings := map[string]any{
		"forcePIN":                     true,
		"allowSimple":                  false,
		"requireAlphanumeric":          true,
		"minLength":                    float64(8),
		"minComplexChars":              float64(2),
		"maxFailedAttempts":            float64(5),
		"maxInactivity":                float64(10),
		"maxPINAgeInDays":              float64(90),
		"maxGracePeriod":               float64(5),
		"pinHistory":                   float64(10),
		"minutesUntilFailedLoginReset": float64(15),
		"changeAtNextAuth":             true,
	}

	config, remaining, warnings, err := convertPasscode(settings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if remaining != nil {
		t.Errorf("expected nil remaining, got %v", remaining)
	}
	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}

	var parsed map[string]any
	if err := json.Unmarshal(config, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// Check version
	if v, ok := parsed["version"].(string); !ok || v != "2" {
		t.Errorf("expected version=2, got %v", parsed["version"])
	}

	// Check a renamed key
	requirePasscode := parsed["RequirePasscode"].(map[string]any)
	if requirePasscode["Included"] != true {
		t.Error("expected RequirePasscode.Included=true")
	}
	if requirePasscode["Value"] != true {
		t.Error("expected RequirePasscode.Value=true")
	}

	// Check MinimumLength
	minLen := parsed["MinimumLength"].(map[string]any)
	if minLen["Value"] != float64(8) {
		t.Errorf("expected MinimumLength.Value=8, got %v", minLen["Value"])
	}
}

func TestConvertPasscode_BoolInversion(t *testing.T) {
	// allowSimple=true should become RequireComplexPasscode=false
	settings := map[string]any{"allowSimple": true}

	config, _, _, err := convertPasscode(settings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(config, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	rcp := parsed["RequireComplexPasscode"].(map[string]any)
	if rcp["Value"] != false {
		t.Errorf("expected RequireComplexPasscode.Value=false (inverted from allowSimple=true), got %v", rcp["Value"])
	}
}

func TestConvertPasscode_CustomRegex(t *testing.T) {
	settings := map[string]any{
		"customRegex": map[string]any{
			"passwordContentRegex":       "[A-Z]{2}[0-9]{6}",
			"passwordContentDescription": map[string]any{"en": "Must start with 2 letters"},
		},
	}

	config, _, _, err := convertPasscode(settings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(config, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	cr := parsed["CustomRegex"].(map[string]any)
	if cr["Included"] != true {
		t.Error("expected CustomRegex.Included=true")
	}
	if cr["Regex"] != "[A-Z]{2}[0-9]{6}" {
		t.Errorf("expected Regex, got %v", cr["Regex"])
	}
	desc := cr["Description"].(map[string]any)
	if desc["en"] != "Must start with 2 letters" {
		t.Errorf("expected Description.en, got %v", desc["en"])
	}
}

func TestConvertPasscode_NoKeys(t *testing.T) {
	settings := map[string]any{}

	config, remaining, _, err := convertPasscode(settings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config != nil {
		t.Error("expected nil config for empty settings")
	}
	if len(remaining) != 0 {
		t.Errorf("expected empty remaining, got %v", remaining)
	}
}

func TestConvertPasscode_UnknownKeys(t *testing.T) {
	settings := map[string]any{
		"forcePIN":         true,
		"unknownFutureKey": "something",
	}

	config, _, warnings, err := convertPasscode(settings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config == nil {
		t.Fatal("expected config")
		return
	}

	found := false
	for _, w := range warnings {
		if w == `passcode key "unknownFutureKey" has no DDM mapping — skipped` {
			found = true
		}
	}
	if !found {
		t.Errorf("expected warning about unknownFutureKey, got %v", warnings)
	}
}

// --- Safari converter tests ---

func TestConvertSafari_AllKeys(t *testing.T) {
	settings := map[string]any{
		"safariAcceptCookies":        float64(0), // Never
		"safariForceFraudWarning":    true,       // -> AllowDisablingFraudWarning=false
		"safariAllowPopups":          false,
		"safariAllowJavaScript":      false,
		"allowSafariPrivateBrowsing": false,
		"allowSafariHistoryClearing": false,
		"allowSafariSummary":         false,
		"allowCamera":                false, // non-safari key
	}

	config, remaining, warnings, err := convertSafari(settings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config == nil {
		t.Fatal("expected config")
		return
	}

	// Non-safari key should be in remaining
	if _, ok := remaining["allowCamera"]; !ok {
		t.Error("expected allowCamera in remaining")
	}

	var parsed map[string]any
	if err := json.Unmarshal(config, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// Check cookie policy conversion
	cookies := parsed["AcceptCookies"].(map[string]any)
	if cookies["Value"] != "Never" {
		t.Errorf("expected AcceptCookies=Never, got %v", cookies["Value"])
	}

	// Check fraud warning inversion
	fraud := parsed["AllowDisablingFraudWarning"].(map[string]any)
	if fraud["Value"] != false {
		t.Errorf("expected AllowDisablingFraudWarning=false (inverted), got %v", fraud["Value"])
	}

	// Check OS 26 warning
	hasOSWarning := false
	for _, w := range warnings {
		if len(w) > 10 && w[:10] == "safari-set" {
			hasOSWarning = true
		}
	}
	if !hasOSWarning {
		t.Error("expected OS 26+ compatibility warning")
	}
}

func TestConvertSafari_CookiePolicyValues(t *testing.T) {
	cases := []struct {
		input    float64
		expected string
	}{
		{0, "Never"},
		{1, "CurrentWebsite"},
		{1.5, "VisitedWebsites"},
		{2, "Always"},
		{3, "Always"}, // out of range → default
	}

	for _, tc := range cases {
		got := convertCookiePolicy(tc.input)
		if got != tc.expected {
			t.Errorf("convertCookiePolicy(%v) = %v, want %v", tc.input, got, tc.expected)
		}
	}
}

func TestConvertSafari_NoSafariKeys(t *testing.T) {
	settings := map[string]any{
		"allowCamera":     false,
		"allowScreenShot": false,
	}

	config, remaining, _, err := convertSafari(settings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config != nil {
		t.Error("expected nil config when no safari keys present")
	}
	if len(remaining) != 2 {
		t.Errorf("expected 2 remaining keys, got %d", len(remaining))
	}
}

func TestConvertSafari_KeysWithNoDDMEquivalent(t *testing.T) {
	settings := map[string]any{
		"safariAllowAutoFill": true,
		"allowSafari":         false,
		"safariAllowPopups":   false, // this one has a DDM mapping
	}

	config, remaining, warnings, err := convertSafari(settings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config == nil {
		t.Fatal("expected config from safariAllowPopups")
		return
	}

	// Keys without DDM equivalent should be in remaining
	if _, ok := remaining["safariAllowAutoFill"]; !ok {
		t.Error("expected safariAllowAutoFill in remaining")
	}
	if _, ok := remaining["allowSafari"]; !ok {
		t.Error("expected allowSafari in remaining")
	}

	hasWarning := false
	for _, w := range warnings {
		if w == `"safariAllowAutoFill" has no DDM safari equivalent — kept in profile wrapper` {
			hasWarning = true
		}
	}
	if !hasWarning {
		t.Error("expected warning about safariAllowAutoFill")
	}
}

// --- Software update converter tests ---

func TestConvertSoftwareUpdate_Deferrals(t *testing.T) {
	settings := map[string]any{
		"forceDelayedSoftwareUpdates":                       true,
		"enforcedSoftwareUpdateMinorOSDeferredInstallDelay": float64(14),
		"forceDelayedMajorSoftwareUpdates":                  true,
		"enforcedSoftwareUpdateMajorOSDeferredInstallDelay": float64(30),
		"allowCamera": false, // non-deferral key
	}

	config, remaining, _, err := convertSoftwareUpdate(settings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config == nil {
		t.Fatal("expected config")
		return
	}

	// Non-deferral key should be in remaining
	if _, ok := remaining["allowCamera"]; !ok {
		t.Error("expected allowCamera in remaining")
	}

	var parsed map[string]any
	if err := json.Unmarshal(config, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// The converter emits ONLY the Deferrals section — sibling sections are
	// intentionally absent so they don't clobber another converter's Included:true
	// during merge. ensureFullSoftwareUpdateSchema backfills them at the orchestrator
	// level (see TestConvertToDDMComponents_SoftwareUpdatePlusDeferralNoClobber).
	if _, ok := parsed["Deferrals"]; !ok {
		t.Error("expected Deferrals section in output")
	}
	for _, section := range []string{"AutomaticActions", "Notifications", "RapidSecurityResponse"} {
		if _, ok := parsed[section]; ok {
			t.Errorf("did not expect sibling section %q in standalone deferral output (would clobber on merge)", section)
		}
	}

	deferrals := parsed["Deferrals"].(map[string]any)

	combined := deferrals["CombinedPeriodInDays"].(map[string]any)
	if combined["Value"] != float64(14) {
		t.Errorf("expected CombinedPeriodInDays=14, got %v", combined["Value"])
	}
	if combined["Included"] != true {
		t.Error("expected CombinedPeriodInDays.Included=true")
	}

	minor := deferrals["MinorPeriodInDays"].(map[string]any)
	if minor["Value"] != float64(14) {
		t.Errorf("expected MinorPeriodInDays=14, got %v", minor["Value"])
	}

	major := deferrals["MajorPeriodInDays"].(map[string]any)
	if major["Value"] != float64(30) {
		t.Errorf("expected MajorPeriodInDays=30, got %v", major["Value"])
	}

	// Non-converted deferral keys should have Included=false (not managed)
	system := deferrals["SystemPeriodInDays"].(map[string]any)
	if system["Included"] != false {
		t.Error("expected SystemPeriodInDays.Included=false (not converted)")
	}
	if system["Value"] != float64(1) {
		t.Errorf("expected SystemPeriodInDays.Value=1 (base default), got %v", system["Value"])
	}
}

func TestConvertSoftwareUpdate_SharedDelay(t *testing.T) {
	// When minor-specific delay is absent, use the shared enforcedSoftwareUpdateDelay
	settings := map[string]any{
		"forceDelayedSoftwareUpdates": true,
		"enforcedSoftwareUpdateDelay": float64(7),
	}

	config, _, _, err := convertSoftwareUpdate(settings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(config, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	deferrals := parsed["Deferrals"].(map[string]any)
	combined := deferrals["CombinedPeriodInDays"].(map[string]any)
	if combined["Value"] != float64(7) {
		t.Errorf("expected CombinedPeriodInDays=7 (from shared delay), got %v", combined["Value"])
	}
}

func TestConvertSoftwareUpdate_DefaultDelay(t *testing.T) {
	// When no delay values are specified, default to 30 days
	settings := map[string]any{
		"forceDelayedSoftwareUpdates": true,
	}

	config, _, _, err := convertSoftwareUpdate(settings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(config, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	deferrals := parsed["Deferrals"].(map[string]any)
	combined := deferrals["CombinedPeriodInDays"].(map[string]any)
	if combined["Value"] != float64(30) {
		t.Errorf("expected CombinedPeriodInDays=30 (default), got %v", combined["Value"])
	}
}

func TestConvertSoftwareUpdate_AllForceFalse(t *testing.T) {
	settings := map[string]any{
		"forceDelayedSoftwareUpdates":      false,
		"forceDelayedMajorSoftwareUpdates": false,
		"forceDelayedAppSoftwareUpdates":   false,
		"enforcedSoftwareUpdateDelay":      float64(30),
	}

	config, remaining, _, err := convertSoftwareUpdate(settings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config != nil {
		t.Error("expected nil config when no force flags are true")
	}
	// All keys should be in remaining since nothing was converted
	if len(remaining) != 4 {
		t.Errorf("expected 4 remaining keys, got %d", len(remaining))
	}
}

func TestConvertSoftwareUpdate_NoDeferralKeys(t *testing.T) {
	settings := map[string]any{
		"allowCamera": false,
	}

	config, remaining, _, err := convertSoftwareUpdate(settings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config != nil {
		t.Error("expected nil config")
	}
	if len(remaining) != 1 {
		t.Errorf("expected 1 remaining key, got %d", len(remaining))
	}
}

func TestConvertSoftwareUpdate_AppDeferralWarning(t *testing.T) {
	settings := map[string]any{
		"forceDelayedAppSoftwareUpdates":                    true,
		"enforcedSoftwareUpdateNonOSDeferredInstallDelay":   float64(14),
		"forceDelayedSoftwareUpdates":                       true,
		"enforcedSoftwareUpdateMinorOSDeferredInstallDelay": float64(7),
	}

	config, remaining, warnings, err := convertSoftwareUpdate(settings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config == nil {
		t.Fatal("expected config from forceDelayedSoftwareUpdates")
		return
	}

	// App deferral keys should be returned to remaining
	if _, ok := remaining["forceDelayedAppSoftwareUpdates"]; !ok {
		t.Error("expected forceDelayedAppSoftwareUpdates in remaining")
	}

	hasWarning := false
	for _, w := range warnings {
		if w == "forceDelayedAppSoftwareUpdates has no DDM equivalent — kept in profile wrapper" {
			hasWarning = true
		}
	}
	if !hasWarning {
		t.Error("expected warning about app deferral")
	}
}

// --- RSR converter tests ---

func TestConvertRSR_BothKeys(t *testing.T) {
	settings := map[string]any{
		"allowRapidSecurityResponseInstallation": true,
		"allowRapidSecurityResponseRemoval":      false,
		"allowCamera":                            false, // non-RSR key
	}

	config, remaining, _, err := convertRSR(settings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config == nil {
		t.Fatal("expected config")
		return
	}

	// Non-RSR key should be in remaining
	if _, ok := remaining["allowCamera"]; !ok {
		t.Error("expected allowCamera in remaining")
	}
	// RSR keys should NOT be in remaining
	if _, ok := remaining["allowRapidSecurityResponseInstallation"]; ok {
		t.Error("RSR key should not be in remaining")
	}

	var parsed map[string]any
	if err := json.Unmarshal(config, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	rsr := parsed["RapidSecurityResponse"].(map[string]any)

	enable := rsr["Enable"].(map[string]any)
	if enable["Enabled"] != true {
		t.Errorf("expected Enable.Enabled=true, got %v", enable["Enabled"])
	}
	if enable["Included"] != true {
		t.Error("expected Enable.Included=true")
	}

	rollback := rsr["EnableRollback"].(map[string]any)
	if rollback["Enabled"] != false {
		t.Errorf("expected EnableRollback.Enabled=false, got %v", rollback["Enabled"])
	}
}

func TestConvertRSR_InstallOnly(t *testing.T) {
	settings := map[string]any{
		"allowRapidSecurityResponseInstallation": false,
	}

	config, remaining, _, err := convertRSR(settings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config == nil {
		t.Fatal("expected config")
		return
	}
	if remaining != nil {
		t.Errorf("expected nil remaining, got %v", remaining)
	}

	var parsed map[string]any
	if err := json.Unmarshal(config, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	rsr := parsed["RapidSecurityResponse"].(map[string]any)
	if _, ok := rsr["Enable"]; !ok {
		t.Error("expected Enable key")
	}
	// EnableRollback should NOT be present (not in source)
	if _, ok := rsr["EnableRollback"]; ok {
		t.Error("EnableRollback should not be set when key is absent from source")
	}
}

func TestConvertRSR_NoRSRKeys(t *testing.T) {
	settings := map[string]any{
		"allowCamera": false,
	}

	config, remaining, _, err := convertRSR(settings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config != nil {
		t.Error("expected nil config when no RSR keys")
	}
	if len(remaining) != 1 {
		t.Errorf("expected 1 remaining key, got %d", len(remaining))
	}
}

// --- Software update profile converter tests ---

func TestConvertSoftwareUpdateProfile_AllKeys(t *testing.T) {
	settings := map[string]any{
		"restrict-software-update-require-admin-to-install": false,
		"AutomaticDownload":                true,
		"AutomaticallyInstallMacOSUpdates": true,
		"CriticalUpdateInstall":            true,
		"AllowPreReleaseInstallation":      false,
		"AutomaticCheckEnabled":            true, // no DDM mapping
		"AutomaticallyInstallAppUpdates":   true, // no DDM mapping
		"ConfigDataInstall":                true, // no DDM mapping
	}

	config, remaining, warnings, err := convertSoftwareUpdateProfile(settings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config == nil {
		t.Fatal("expected config")
		return
	}

	// Keys without DDM mapping should be in remaining
	if _, ok := remaining["AutomaticCheckEnabled"]; !ok {
		t.Error("expected AutomaticCheckEnabled in remaining")
	}
	if _, ok := remaining["AutomaticallyInstallAppUpdates"]; !ok {
		t.Error("expected AutomaticallyInstallAppUpdates in remaining")
	}

	// Should have warnings about unmapped keys
	hasWarning := false
	for _, w := range warnings {
		if strings.Contains(w, "AutomaticCheckEnabled") {
			hasWarning = true
		}
	}
	if !hasWarning {
		t.Error("expected warning about AutomaticCheckEnabled")
	}

	var parsed map[string]any
	if err := json.Unmarshal(config, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// AllowStandardUserOSUpdates: restrict=false → Enabled=true
	asu := parsed["AllowStandardUserOSUpdates"].(map[string]any)
	if asu["Enabled"] != true {
		t.Errorf("expected AllowStandardUserOSUpdates.Enabled=true, got %v", asu["Enabled"])
	}
	if asu["Included"] != true {
		t.Error("expected AllowStandardUserOSUpdates.Included=true")
	}

	// AutomaticActions
	actions := parsed["AutomaticActions"].(map[string]any)
	dl := actions["Download"].(map[string]any)
	if dl["Value"] != "Allowed" {
		t.Errorf("expected Download=Allowed, got %v", dl["Value"])
	}
	install := actions["InstallOSUpdates"].(map[string]any)
	if install["Value"] != "Allowed" {
		t.Errorf("expected InstallOSUpdates=Allowed, got %v", install["Value"])
	}
	// CriticalUpdateInstall should map to InstallSecurityUpdate
	sec := actions["InstallSecurityUpdate"].(map[string]any)
	if sec["Value"] != "Allowed" {
		t.Errorf("expected InstallSecurityUpdate=Allowed (from CriticalUpdateInstall), got %v", sec["Value"])
	}

	// Beta
	beta := parsed["Beta"].(map[string]any)
	if beta["Included"] != true {
		t.Error("expected Beta.Included=true")
	}
	betaVal := beta["Value"].(map[string]any)
	if betaVal["ProgramEnrollment"] != "AlwaysOff" {
		t.Errorf("expected ProgramEnrollment=AlwaysOff, got %v", betaVal["ProgramEnrollment"])
	}

	// Only sections the converter modifies should be present (scaffold
	// backfill happens in the orchestrator, not the unit converter)
	for _, section := range []string{"AllowStandardUserOSUpdates", "AutomaticActions", "Beta"} {
		if _, ok := parsed[section]; !ok {
			t.Errorf("expected top-level section %q in output", section)
		}
	}
	// RapidSecurityResponse should NOT be set (RSR is in applicationaccess, not SoftwareUpdate)
	if _, ok := parsed["RapidSecurityResponse"]; ok {
		t.Error("RapidSecurityResponse should not be set by the SoftwareUpdate profile converter")
	}
}

func TestConvertSoftwareUpdateProfile_InvertedAdmin(t *testing.T) {
	// restrict=true → standard users CANNOT install → Enabled=false
	settings := map[string]any{
		"restrict-software-update-require-admin-to-install": true,
	}

	config, _, _, err := convertSoftwareUpdateProfile(settings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(config, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	asu := parsed["AllowStandardUserOSUpdates"].(map[string]any)
	if asu["Enabled"] != false {
		t.Errorf("expected Enabled=false when restrict=true, got %v", asu["Enabled"])
	}
}

func TestConvertSoftwareUpdateProfile_NoConvertibleKeys(t *testing.T) {
	settings := map[string]any{
		"AutomaticCheckEnabled": true,
	}

	config, remaining, _, err := convertSoftwareUpdateProfile(settings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config != nil {
		t.Error("expected nil config when no convertible keys")
	}
	if _, ok := remaining["AutomaticCheckEnabled"]; !ok {
		t.Error("expected AutomaticCheckEnabled in remaining")
	}
}

func TestConvertSoftwareUpdateProfile_BoolToAllowed(t *testing.T) {
	if got := boolToAllowed(true); got != "Allowed" {
		t.Errorf("boolToAllowed(true) = %q, want Allowed", got)
	}
	if got := boolToAllowed(false); got != "AlwaysOff" {
		t.Errorf("boolToAllowed(false) = %q, want AlwaysOff", got)
	}
	if got := boolToAllowed("not a bool"); got != "" {
		t.Errorf("boolToAllowed(string) = %q, want empty", got)
	}
}

// --- Orchestration: SoftwareUpdate profile end-to-end ---

const testSoftwareUpdateMobileconfig = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>PayloadContent</key>
	<array>
		<dict>
			<key>PayloadType</key>
			<string>com.apple.SoftwareUpdate</string>
			<key>PayloadIdentifier</key>
			<string>com.example.su</string>
			<key>PayloadUUID</key>
			<string>UUID-1</string>
			<key>PayloadVersion</key>
			<integer>1</integer>
			<key>AllowPreReleaseInstallation</key>
			<false/>
			<key>AutomaticDownload</key>
			<true/>
			<key>AutomaticallyInstallMacOSUpdates</key>
			<true/>
		</dict>
	</array>
	<key>PayloadDisplayName</key>
	<string>Software Update</string>
	<key>PayloadIdentifier</key>
	<string>com.example.su</string>
	<key>PayloadType</key>
	<string>Configuration</string>
	<key>PayloadVersion</key>
	<integer>1</integer>
</dict>
</plist>`

func TestConvertToDDMComponents_SoftwareUpdateProfile(t *testing.T) {
	result, err := ConvertToDDMComponents([]byte(testSoftwareUpdateMobileconfig), false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.NativeComponents) != 1 {
		t.Fatalf("expected 1 native component, got %d", len(result.NativeComponents))
	}
	if result.NativeComponents[0].Identifier != "com.jamf.ddm.software-update-settings" {
		t.Errorf("expected software-update-settings, got %s", result.NativeComponents[0].Identifier)
	}
	// All keys were convertible, so no profile wrapper needed
	if result.ProfileConfig != nil {
		t.Error("expected nil ProfileConfig when all payload keys converted natively")
	}
	if len(result.Conversions) != 1 {
		t.Fatalf("expected 1 conversion, got %d", len(result.Conversions))
	}
}

// --- Merged software-update-settings test ---

const testMixedDeferralsAndSoftwareUpdate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>PayloadContent</key>
	<array>
		<dict>
			<key>PayloadType</key>
			<string>com.apple.applicationaccess</string>
			<key>PayloadIdentifier</key>
			<string>com.example.restrictions</string>
			<key>PayloadUUID</key>
			<string>UUID-1</string>
			<key>PayloadVersion</key>
			<integer>1</integer>
			<key>forceDelayedSoftwareUpdates</key>
			<true/>
			<key>enforcedSoftwareUpdateDelay</key>
			<integer>14</integer>
			<key>allowCamera</key>
			<false/>
		</dict>
		<dict>
			<key>PayloadType</key>
			<string>com.apple.SoftwareUpdate</string>
			<key>PayloadIdentifier</key>
			<string>com.example.su</string>
			<key>PayloadUUID</key>
			<string>UUID-2</string>
			<key>PayloadVersion</key>
			<integer>1</integer>
			<key>AutomaticDownload</key>
			<true/>
			<key>CriticalUpdateInstall</key>
			<true/>
			<key>AllowPreReleaseInstallation</key>
			<false/>
		</dict>
	</array>
	<key>PayloadDisplayName</key>
	<string>Mixed Deferrals and SU</string>
	<key>PayloadIdentifier</key>
	<string>com.example.mixed</string>
	<key>PayloadType</key>
	<string>Configuration</string>
	<key>PayloadVersion</key>
	<integer>1</integer>
</dict>
</plist>`

func TestConvertToDDMComponents_MergedSoftwareUpdateSettings(t *testing.T) {
	result, err := ConvertToDDMComponents([]byte(testMixedDeferralsAndSoftwareUpdate), false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should produce exactly 1 software-update-settings component (merged from both payloads)
	var suComponent *DDMComponent
	for i, nc := range result.NativeComponents {
		if nc.Identifier == "com.jamf.ddm.software-update-settings" {
			suComponent = &result.NativeComponents[i]
		}
	}
	if suComponent == nil {
		t.Fatal("expected software-update-settings component")
		return
	}

	// Count occurrences — should be exactly 1
	count := 0
	for _, nc := range result.NativeComponents {
		if nc.Identifier == "com.jamf.ddm.software-update-settings" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 software-update-settings component, got %d", count)
	}

	var parsed map[string]any
	if err := json.Unmarshal(suComponent.Configuration, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// Deferrals from applicationaccess should be present
	deferrals := parsed["Deferrals"].(map[string]any)
	combined := deferrals["CombinedPeriodInDays"].(map[string]any)
	if combined["Included"] != true {
		t.Error("expected CombinedPeriodInDays.Included=true from deferral converter")
	}

	// AutomaticActions from SoftwareUpdate should be merged in
	actions := parsed["AutomaticActions"].(map[string]any)
	dl := actions["Download"].(map[string]any)
	if dl["Value"] != "Allowed" || dl["Included"] != true {
		t.Errorf("expected merged Download=Allowed, got %v", dl)
	}

	// Beta from SoftwareUpdate should be merged in
	beta := parsed["Beta"].(map[string]any)
	if beta["Included"] != true {
		t.Error("expected Beta.Included=true from profile converter")
	}

	// Should have a "(merged)" conversion entry
	hasMerged := false
	for _, c := range result.Conversions {
		if strings.Contains(c, "(merged)") {
			hasMerged = true
		}
	}
	if !hasMerged {
		t.Errorf("expected merged conversion entry, got %v", result.Conversions)
	}

	// All scaffold sections must be present (via scaffold backfill or deferral converter)
	for _, section := range []string{"RapidSecurityResponse", "Notifications", "RecommendedCadence"} {
		if _, ok := parsed[section]; !ok {
			t.Errorf("missing scaffold section %q — GUI will render blank", section)
		}
	}
	// RapidSecurityResponse from scaffold should have both sub-keys
	rsr := parsed["RapidSecurityResponse"].(map[string]any)
	if _, ok := rsr["Enable"]; !ok {
		t.Error("expected RapidSecurityResponse.Enable from scaffold")
	}
	if _, ok := rsr["EnableRollback"]; !ok {
		t.Error("expected RapidSecurityResponse.EnableRollback from scaffold")
	}

	// allowCamera from applicationaccess should be in the profile wrapper
	if result.ProfileConfig == nil {
		t.Fatal("expected ProfileConfig for remaining applicationaccess keys")
		return
	}
}

// --- Orchestration tests ---

const testPasscodeMobileconfig = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>PayloadContent</key>
	<array>
		<dict>
			<key>PayloadType</key>
			<string>com.apple.mobiledevice.passwordpolicy</string>
			<key>PayloadIdentifier</key>
			<string>com.example.passcode</string>
			<key>PayloadUUID</key>
			<string>UUID-1234</string>
			<key>PayloadVersion</key>
			<integer>1</integer>
			<key>forcePIN</key>
			<true/>
			<key>minLength</key>
			<integer>8</integer>
			<key>allowSimple</key>
			<false/>
		</dict>
	</array>
	<key>PayloadDisplayName</key>
	<string>Passcode Policy</string>
	<key>PayloadIdentifier</key>
	<string>com.example.test</string>
	<key>PayloadType</key>
	<string>Configuration</string>
	<key>PayloadVersion</key>
	<integer>1</integer>
</dict>
</plist>`

func TestConvertToDDMComponents_PasscodeOnly(t *testing.T) {
	result, err := ConvertToDDMComponents([]byte(testPasscodeMobileconfig), false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.NativeComponents) != 1 {
		t.Fatalf("expected 1 native component, got %d", len(result.NativeComponents))
	}
	if result.NativeComponents[0].Identifier != "com.jamf.ddm.passcode-settings" {
		t.Errorf("expected passcode-settings, got %s", result.NativeComponents[0].Identifier)
	}
	if result.ProfileConfig != nil {
		t.Error("expected nil ProfileConfig when all payloads converted natively")
	}
	if result.DisplayName != "Passcode Policy" {
		t.Errorf("expected DisplayName=Passcode Policy, got %s", result.DisplayName)
	}
	if len(result.Conversions) != 1 {
		t.Fatalf("expected 1 conversion, got %d", len(result.Conversions))
	}
	expected := "com.apple.mobiledevice.passwordpolicy -> com.jamf.ddm.passcode-settings"
	if result.Conversions[0] != expected {
		t.Errorf("expected %q, got %q", expected, result.Conversions[0])
	}

	// Verify the config has the right keys
	var parsed map[string]any
	if err := json.Unmarshal(result.NativeComponents[0].Configuration, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if parsed["version"] != "2" {
		t.Errorf("expected version=2, got %v", parsed["version"])
	}
	rp := parsed["RequirePasscode"].(map[string]any)
	if rp["Value"] != true {
		t.Error("expected RequirePasscode.Value=true")
	}
}

const testMixedMobileconfig = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>PayloadContent</key>
	<array>
		<dict>
			<key>PayloadType</key>
			<string>com.apple.mobiledevice.passwordpolicy</string>
			<key>PayloadIdentifier</key>
			<string>com.example.passcode</string>
			<key>PayloadUUID</key>
			<string>UUID-1</string>
			<key>PayloadVersion</key>
			<integer>1</integer>
			<key>forcePIN</key>
			<true/>
		</dict>
		<dict>
			<key>PayloadType</key>
			<string>com.apple.screensaver</string>
			<key>PayloadIdentifier</key>
			<string>com.example.screensaver</string>
			<key>PayloadUUID</key>
			<string>UUID-2</string>
			<key>PayloadVersion</key>
			<integer>1</integer>
			<key>idleTime</key>
			<integer>600</integer>
			<key>moduleName</key>
			<string>Flurry</string>
		</dict>
	</array>
	<key>PayloadDisplayName</key>
	<string>Mixed Profile</string>
	<key>PayloadIdentifier</key>
	<string>com.example.mixed</string>
	<key>PayloadType</key>
	<string>Configuration</string>
	<key>PayloadVersion</key>
	<integer>1</integer>
</dict>
</plist>`

func TestConvertToDDMComponents_MixedPayloads(t *testing.T) {
	result, err := ConvertToDDMComponents([]byte(testMixedMobileconfig), false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Passcode should be native DDM
	if len(result.NativeComponents) != 1 {
		t.Fatalf("expected 1 native component, got %d", len(result.NativeComponents))
	}
	if result.NativeComponents[0].Identifier != "com.jamf.ddm.passcode-settings" {
		t.Errorf("expected passcode-settings, got %s", result.NativeComponents[0].Identifier)
	}

	// Screensaver should be in the profile wrapper
	if result.ProfileConfig == nil {
		t.Fatal("expected ProfileConfig for screensaver payload")
		return
	}

	var parsed map[string]any
	if err := json.Unmarshal(result.ProfileConfig, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	content := parsed["payloadContent"].([]any)
	if len(content) != 1 {
		t.Fatalf("expected 1 wrapped payload, got %d", len(content))
	}
	entry := content[0].(map[string]any)
	if entry["payloadType"] != "com.apple.screensaver" {
		t.Errorf("expected screensaver payload, got %v", entry["payloadType"])
	}
}

const testApplicationAccessMobileconfig = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>PayloadContent</key>
	<array>
		<dict>
			<key>PayloadType</key>
			<string>com.apple.applicationaccess</string>
			<key>PayloadIdentifier</key>
			<string>com.example.restrictions</string>
			<key>PayloadUUID</key>
			<string>UUID-1</string>
			<key>PayloadVersion</key>
			<integer>1</integer>
			<key>safariAllowPopups</key>
			<false/>
			<key>safariAllowJavaScript</key>
			<false/>
			<key>forceDelayedSoftwareUpdates</key>
			<true/>
			<key>enforcedSoftwareUpdateDelay</key>
			<integer>14</integer>
			<key>allowCamera</key>
			<false/>
			<key>allowScreenShot</key>
			<false/>
		</dict>
	</array>
	<key>PayloadDisplayName</key>
	<string>Restrictions</string>
	<key>PayloadIdentifier</key>
	<string>com.example.restrictions</string>
	<key>PayloadType</key>
	<string>Configuration</string>
	<key>PayloadVersion</key>
	<integer>1</integer>
</dict>
</plist>`

func TestConvertToDDMComponents_ApplicationAccess(t *testing.T) {
	result, err := ConvertToDDMComponents([]byte(testApplicationAccessMobileconfig), false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should produce safari-settings and software-update-settings
	if len(result.NativeComponents) != 2 {
		t.Fatalf("expected 2 native components, got %d", len(result.NativeComponents))
	}

	ids := make(map[string]bool)
	for _, nc := range result.NativeComponents {
		ids[nc.Identifier] = true
	}
	if !ids["com.jamf.ddm.safari-settings"] {
		t.Error("expected safari-settings component")
	}
	if !ids["com.jamf.ddm.software-update-settings"] {
		t.Error("expected software-update-settings component")
	}

	// Non-safari, non-deferral keys should be in profile wrapper
	if result.ProfileConfig == nil {
		t.Fatal("expected ProfileConfig for remaining applicationaccess keys")
		return
	}
	var parsed map[string]any
	if err := json.Unmarshal(result.ProfileConfig, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	content := parsed["payloadContent"].([]any)
	if len(content) != 1 {
		t.Fatalf("expected 1 remaining payload entry, got %d", len(content))
	}
	entry := content[0].(map[string]any)
	if _, ok := entry["allowCamera"]; !ok {
		t.Error("expected allowCamera in remaining payload")
	}
	if _, ok := entry["allowScreenShot"]; !ok {
		t.Error("expected allowScreenShot in remaining payload")
	}
	// Safari and deferral keys should NOT be in remaining
	if _, ok := entry["safariAllowPopups"]; ok {
		t.Error("safariAllowPopups should have been consumed by safari converter")
	}
	if _, ok := entry["forceDelayedSoftwareUpdates"]; ok {
		t.Error("forceDelayedSoftwareUpdates should have been consumed by sw update converter")
	}
}

func TestConvertToDDMComponents_NoConverters(t *testing.T) {
	// Profile with only screensaver — no DDM converter exists
	mobileconfig := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>PayloadContent</key>
	<array>
		<dict>
			<key>PayloadType</key>
			<string>com.apple.screensaver</string>
			<key>PayloadIdentifier</key>
			<string>com.example.ss</string>
			<key>PayloadUUID</key>
			<string>UUID-1</string>
			<key>PayloadVersion</key>
			<integer>1</integer>
			<key>idleTime</key>
			<integer>300</integer>
		</dict>
	</array>
	<key>PayloadDisplayName</key>
	<string>Screensaver Only</string>
	<key>PayloadIdentifier</key>
	<string>com.example.ss</string>
	<key>PayloadType</key>
	<string>Configuration</string>
	<key>PayloadVersion</key>
	<integer>1</integer>
</dict>
</plist>`

	result, err := ConvertToDDMComponents([]byte(mobileconfig), false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.NativeComponents) != 0 {
		t.Errorf("expected 0 native components, got %d", len(result.NativeComponents))
	}
	if result.ProfileConfig == nil {
		t.Error("expected ProfileConfig for screensaver")
	}
}

func TestConvertToDDMComponents_FilterDisabled(t *testing.T) {
	// Profile with only a disabled payload type and no DDM converter.
	mobileconfig := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>PayloadContent</key>
	<array>
		<dict>
			<key>PayloadType</key>
			<string>com.apple.vpn.managed</string>
			<key>PayloadIdentifier</key>
			<string>com.example.vpn</string>
			<key>PayloadUUID</key>
			<string>UUID-1</string>
			<key>PayloadVersion</key>
			<integer>1</integer>
			<key>UserDefinedName</key>
			<string>Corp VPN</string>
		</dict>
	</array>
	<key>PayloadDisplayName</key>
	<string>Disabled Profile</string>
	<key>PayloadIdentifier</key>
	<string>com.example.disabled</string>
	<key>PayloadType</key>
	<string>Configuration</string>
	<key>PayloadVersion</key>
	<integer>1</integer>
</dict>
</plist>`

	_, err := ConvertToDDMComponents([]byte(mobileconfig), true, nil)
	if err == nil {
		t.Fatal("expected error when all payloads filtered")
		return
	}
	if err.Error() != "no payloads remain after filtering" {
		t.Errorf("unexpected error: %v", err)
	}
}

// mcxProfile builds a Custom Settings (MCX) mobileconfig wrapping one inner
// preference domain with the given forced settings block (raw XML).
func mcxProfile(domain, forcedSettingsXML string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>PayloadContent</key>
	<array>
		<dict>
			<key>PayloadType</key>
			<string>com.apple.ManagedClient.preferences</string>
			<key>PayloadVersion</key>
			<integer>1</integer>
			<key>PayloadContent</key>
			<dict>
				<key>` + domain + `</key>
				<dict>
					<key>Forced</key>
					<array>
						<dict>
							<key>mcx_preference_settings</key>
							<dict>` + forcedSettingsXML + `</dict>
						</dict>
					</array>
				</dict>
			</dict>
		</dict>
	</array>
	<key>PayloadDisplayName</key>
	<string>MCX Test</string>
	<key>PayloadType</key>
	<string>Configuration</string>
	<key>PayloadVersion</key>
	<integer>1</integer>
</dict>
</plist>`
}

func TestConvertToDDMComponents_MCXAppleDomainConverted(t *testing.T) {
	// MCX wrapping a real Apple domain that has a converter (applicationaccess)
	// is unwrapped so the converter runs. safariAllowPopups -> safari-settings;
	// allowCamera has no converter, so it lands in the config-profile wrapper as
	// a bare applicationaccess payload (NOT ManagedClient.preferences).
	mc := mcxProfile("com.apple.applicationaccess",
		`<key>safariAllowPopups</key><false/><key>allowCamera</key><false/>`)

	result, err := ConvertToDDMComponents([]byte(mc), true, nil) // nil fetcher: converter short-circuits, no network
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	hasSafari := false
	for _, c := range result.NativeComponents {
		if c.Identifier == "com.jamf.ddm.safari-settings" {
			hasSafari = true
		}
	}
	if !hasSafari {
		t.Error("expected safari-settings native component from unwrapped MCX applicationaccess")
	}

	if result.ProfileConfig == nil {
		t.Fatal("expected ProfileConfig for the unconverted applicationaccess key")
	}
	var cfg map[string]any
	if err := json.Unmarshal(result.ProfileConfig, &cfg); err != nil {
		t.Fatalf("invalid ProfileConfig JSON: %v", err)
	}
	p := cfg["payloadContent"].([]any)[0].(map[string]any)
	if p["payloadType"] != "com.apple.applicationaccess" {
		t.Errorf("expected bare com.apple.applicationaccess in wrapper, got %v", p["payloadType"])
	}
}

func TestConvertToDDMComponents_MCXThirdPartyKept(t *testing.T) {
	// MCX wrapping a third-party domain (no converter, no Apple schema) stays
	// wrapped in ManagedClient.preferences — the API accepts it as opaque custom
	// settings but rejects it as an unwrapped bare type.
	mc := mcxProfile("uk.co.datajar.Management",
		`<key>jssURL</key><string>https://x.example/</string>`)

	// Mock fetcher: classify the third-party domain as unknown (404 -> nil).
	fetcher := NewSchemaFetcher(nil)
	fetcher.mu.Lock()
	fetcher.cache["uk.co.datajar.Management"] = &schemaResult{defaults: nil}
	fetcher.mu.Unlock()

	result, err := ConvertToDDMComponents([]byte(mc), true, fetcher)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ProfileConfig == nil {
		t.Fatal("expected ProfileConfig containing the kept MCX payload")
	}
	var cfg map[string]any
	if err := json.Unmarshal(result.ProfileConfig, &cfg); err != nil {
		t.Fatalf("invalid ProfileConfig JSON: %v", err)
	}
	p := cfg["payloadContent"].([]any)[0].(map[string]any)
	if p["payloadType"] != "com.apple.ManagedClient.preferences" {
		t.Errorf("expected third-party domain kept in MCX, got payloadType %v", p["payloadType"])
	}
	pc, ok := p["PayloadContent"].(map[string]any)
	if !ok || pc["uk.co.datajar.Management"] == nil {
		t.Errorf("expected datajar domain preserved inside MCX PayloadContent, got %v", p["PayloadContent"])
	}
}

func TestConvertToDDMComponents_MCXAppleDomainNoConverterUnwrapped(t *testing.T) {
	// MCX wrapping a real Apple domain with no converter (e.g. com.apple.dock).
	// Apple publishes a schema for it, so it is unwrapped to a bare payload and
	// lands in the config-profile wrapper as com.apple.dock (not MCX).
	mc := mcxProfile("com.apple.dock", `<key>tilesize</key><integer>48</integer>`)

	// Mock fetcher: classify com.apple.dock as a known Apple payload (schema present).
	fetcher := NewSchemaFetcher(nil)
	fetcher.mu.Lock()
	fetcher.cache["com.apple.dock"] = &schemaResult{defaults: &SchemaDefaults{}}
	fetcher.mu.Unlock()

	result, err := ConvertToDDMComponents([]byte(mc), true, fetcher)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ProfileConfig == nil {
		t.Fatal("expected ProfileConfig containing the unwrapped dock payload")
	}
	var cfg map[string]any
	if err := json.Unmarshal(result.ProfileConfig, &cfg); err != nil {
		t.Fatalf("invalid ProfileConfig JSON: %v", err)
	}
	p := cfg["payloadContent"].([]any)[0].(map[string]any)
	if p["payloadType"] != "com.apple.dock" {
		t.Errorf("expected unwrapped bare com.apple.dock, got %v", p["payloadType"])
	}
	if p["tilesize"] != float64(48) {
		t.Errorf("expected tilesize 48 carried over, got %v", p["tilesize"])
	}
}

func TestConvertToDDMComponents_SoftwareUpdateLeftoverMCXWrapped(t *testing.T) {
	// A com.apple.SoftwareUpdate payload with mappable + unmappable keys.
	// AutomaticDownload converts to native software-update-settings; the
	// unmappable AutomaticCheckEnabled is delivered via MCX (Custom Settings)
	// rather than a bare com.apple.SoftwareUpdate payload (which the API refuses).
	// CatalogURL is an empty string and must be dropped.
	mc := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>PayloadContent</key>
	<array>
		<dict>
			<key>PayloadType</key>
			<string>com.apple.SoftwareUpdate</string>
			<key>PayloadVersion</key>
			<integer>1</integer>
			<key>AutomaticDownload</key>
			<true/>
			<key>AutomaticCheckEnabled</key>
			<true/>
			<key>CatalogURL</key>
			<string></string>
		</dict>
	</array>
	<key>PayloadDisplayName</key>
	<string>SU</string>
	<key>PayloadType</key>
	<string>Configuration</string>
	<key>PayloadVersion</key>
	<integer>1</integer>
</dict>
</plist>`

	result, err := ConvertToDDMComponents([]byte(mc), true, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Native software-update-settings from the mappable key.
	if _, idx := findComponent(result.NativeComponents, "com.jamf.ddm.software-update-settings"); idx < 0 {
		t.Fatal("expected native software-update-settings component")
	}

	// Leftover delivered via MCX, not a bare com.apple.SoftwareUpdate payload.
	if result.ProfileConfig == nil {
		t.Fatal("expected ProfileConfig with the MCX-wrapped leftover")
	}
	var cfg map[string]any
	if err := json.Unmarshal(result.ProfileConfig, &cfg); err != nil {
		t.Fatalf("invalid ProfileConfig JSON: %v", err)
	}
	p := cfg["payloadContent"].([]any)[0].(map[string]any)
	if p["payloadType"] != "com.apple.ManagedClient.preferences" {
		t.Fatalf("expected MCX-wrapped leftover, got payloadType %v", p["payloadType"])
	}
	pc := p["PayloadContent"].(map[string]any)["com.apple.SoftwareUpdate"].(map[string]any)
	settings := pc["Forced"].([]any)[0].(map[string]any)["mcx_preference_settings"].(map[string]any)
	if settings["AutomaticCheckEnabled"] != true {
		t.Errorf("expected AutomaticCheckEnabled preserved in MCX, got %v", settings["AutomaticCheckEnabled"])
	}
	if _, present := settings["CatalogURL"]; present {
		t.Error("empty-string CatalogURL should have been dropped")
	}
}

func TestConvertToDDMComponents_SoftwareUpdatePlusDeferralNoClobber(t *testing.T) {
	// A profile with BOTH a com.apple.SoftwareUpdate payload (AutomaticActions)
	// and applicationaccess deferral keys, both targeting software-update-settings.
	// The two converters must merge without clobbering each other: the deferral
	// converter must not flip the SoftwareUpdate-mapped AutomaticActions section
	// back to Included:false.
	mc := `<?xml version="1.0"?><plist version="1.0"><dict>
<key>PayloadContent</key><array>
<dict><key>PayloadType</key><string>com.apple.SoftwareUpdate</string>
<key>AutomaticDownload</key><true/><key>CriticalUpdateInstall</key><true/></dict>
<dict><key>PayloadType</key><string>com.apple.applicationaccess</string>
<key>forceDelayedSoftwareUpdates</key><true/>
<key>enforcedSoftwareUpdateMinorOSDeferredInstallDelay</key><integer>5</integer></dict>
</array>
<key>PayloadDisplayName</key><string>SU+Deferral</string>
<key>PayloadType</key><string>Configuration</string></dict></plist>`

	result, err := ConvertToDDMComponents([]byte(mc), true, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	comp, idx := findComponent(result.NativeComponents, "com.jamf.ddm.software-update-settings")
	if idx < 0 {
		t.Fatal("expected software-update-settings component")
	}
	var cfg map[string]any
	if err := json.Unmarshal(comp.Configuration, &cfg); err != nil {
		t.Fatalf("invalid component JSON: %v", err)
	}
	included := func(section, key string) any {
		sec, _ := cfg[section].(map[string]any)
		sub, _ := sec[key].(map[string]any)
		return sub["Included"]
	}
	// SoftwareUpdate-mapped section must survive the deferral merge.
	if included("AutomaticActions", "Download") != true {
		t.Errorf("AutomaticActions.Download.Included = %v, want true (clobbered by deferral merge)", included("AutomaticActions", "Download"))
	}
	if included("AutomaticActions", "InstallSecurityUpdate") != true {
		t.Errorf("AutomaticActions.InstallSecurityUpdate.Included = %v, want true", included("AutomaticActions", "InstallSecurityUpdate"))
	}
	// Deferral section must also be present.
	if included("Deferrals", "MinorPeriodInDays") != true {
		t.Errorf("Deferrals.MinorPeriodInDays.Included = %v, want true", included("Deferrals", "MinorPeriodInDays"))
	}
}

// --- Registry tests ---

func TestFindConverters(t *testing.T) {
	// Passcode has exactly one converter
	if got := findConverters("com.apple.mobiledevice.passwordpolicy"); len(got) != 1 {
		t.Errorf("expected 1 converter for passwordpolicy, got %d", len(got))
	}

	// applicationaccess has three converters (safari + software-update deferrals + RSR)
	if got := findConverters("com.apple.applicationaccess"); len(got) != 3 {
		t.Errorf("expected 3 converters for applicationaccess, got %d", len(got))
	}

	// SoftwareUpdate has one converter (software-update-profile)
	if got := findConverters("com.apple.SoftwareUpdate"); len(got) != 1 {
		t.Errorf("expected 1 converter for SoftwareUpdate, got %d", len(got))
	}

	// Unknown type has no converters
	if got := findConverters("com.apple.screensaver"); len(got) != 0 {
		t.Errorf("expected 0 converters for screensaver, got %d", len(got))
	}
}

func TestExtractSettingsKeys(t *testing.T) {
	payload := map[string]any{
		"PayloadType":        "com.apple.test",
		"PayloadIdentifier":  "com.example",
		"PayloadUUID":        "uuid",
		"PayloadVersion":     1,
		"PayloadDisplayName": "Test",
		"tilesize":           uint64(48),
		"name":               "test",
		"emptyStr":           "",
		"emptyArr":           []any{},
	}

	settings := extractSettingsKeys(payload)

	// Apple metadata should be stripped
	if _, ok := settings["PayloadType"]; ok {
		t.Error("PayloadType should be stripped")
	}

	// Valid settings should be present (uint64 converted to float64)
	if v, ok := settings["tilesize"]; !ok || v != float64(48) {
		t.Errorf("expected tilesize=48.0, got %v", v)
	}

	// Empty values should be stripped
	if _, ok := settings["emptyStr"]; ok {
		t.Error("empty string should be stripped")
	}
	if _, ok := settings["emptyArr"]; ok {
		t.Error("empty array should be stripped")
	}
}

// --- Duplicate component deduplication test ---

func TestConvertToDDMComponents_DuplicatePasscodePayloads(t *testing.T) {
	// Two passcode payloads in the same profile — second should be skipped
	mobileconfig := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>PayloadContent</key>
	<array>
		<dict>
			<key>PayloadType</key>
			<string>com.apple.mobiledevice.passwordpolicy</string>
			<key>PayloadIdentifier</key>
			<string>com.example.passcode1</string>
			<key>PayloadUUID</key>
			<string>UUID-1</string>
			<key>PayloadVersion</key>
			<integer>1</integer>
			<key>forcePIN</key>
			<true/>
		</dict>
		<dict>
			<key>PayloadType</key>
			<string>com.apple.mobiledevice.passwordpolicy</string>
			<key>PayloadIdentifier</key>
			<string>com.example.passcode2</string>
			<key>PayloadUUID</key>
			<string>UUID-2</string>
			<key>PayloadVersion</key>
			<integer>1</integer>
			<key>minLength</key>
			<integer>6</integer>
		</dict>
	</array>
	<key>PayloadDisplayName</key>
	<string>Duplicate Passcode</string>
	<key>PayloadIdentifier</key>
	<string>com.example.dup</string>
	<key>PayloadType</key>
	<string>Configuration</string>
	<key>PayloadVersion</key>
	<integer>1</integer>
</dict>
</plist>`

	result, err := ConvertToDDMComponents([]byte(mobileconfig), false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should produce exactly 1 native component (merged from both payloads)
	if len(result.NativeComponents) != 1 {
		t.Fatalf("expected 1 native component, got %d", len(result.NativeComponents))
	}
	if result.NativeComponents[0].Identifier != "com.jamf.ddm.passcode-settings" {
		t.Errorf("expected passcode-settings, got %s", result.NativeComponents[0].Identifier)
	}

	// Second payload should have been merged
	hasMerged := false
	for _, c := range result.Conversions {
		if strings.Contains(c, "(merged)") {
			hasMerged = true
		}
	}
	if !hasMerged {
		t.Error("expected merged conversion entry")
	}

	// Verify both payloads' keys are present in the merged config
	var parsed map[string]any
	if err := json.Unmarshal(result.NativeComponents[0].Configuration, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := parsed["RequirePasscode"]; !ok {
		t.Error("expected RequirePasscode from first payload")
	}
	if _, ok := parsed["MinimumLength"]; !ok {
		t.Error("expected MinimumLength from second payload")
	}
}

// --- Converter error fallback test ---

func TestConvertToDDMComponents_ConverterError(t *testing.T) {
	// Register a temporary converter that always fails, then verify the
	// orchestrator warns and continues
	origConverters := converters
	defer func() { converters = origConverters }()

	converters = []*ddmConverter{
		{
			componentID:  "com.test.failing-converter",
			payloadTypes: map[string]bool{"com.apple.mobiledevice.passwordpolicy": true},
			convert: func(_ map[string]any) (json.RawMessage, map[string]any, []string, error) {
				return nil, nil, nil, fmt.Errorf("intentional test failure")
			},
		},
	}

	result, err := ConvertToDDMComponents([]byte(testPasscodeMobileconfig), false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The failing converter should not produce native components
	if len(result.NativeComponents) != 0 {
		t.Errorf("expected 0 native components, got %d", len(result.NativeComponents))
	}

	// The payload should fall through to the profile wrapper
	if result.ProfileConfig == nil {
		t.Error("expected ProfileConfig when converter fails")
	}

	// Should have a warning about the failure
	hasFailWarning := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "intentional test failure") {
			hasFailWarning = true
		}
	}
	if !hasFailWarning {
		t.Error("expected warning about converter failure")
	}
}

// --- Software update base config error test ---

func TestSoftwareUpdateBaseConfig_Valid(t *testing.T) {
	config, err := softwareUpdateBaseConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := config["Deferrals"]; !ok {
		t.Error("expected Deferrals section in base config")
	}
	// All Included flags should be false
	if inc, ok := config["Deferrals"].(map[string]any)["Included"]; ok && inc == true {
		t.Error("expected Deferrals.Included=false in base config")
	}
}

// --- Cookie policy non-numeric test ---

func TestConvertCookiePolicy_NonNumeric(t *testing.T) {
	// Non-numeric input should return the default "Always"
	got := convertCookiePolicy("invalid")
	if got != "Always" {
		t.Errorf("convertCookiePolicy(\"invalid\") = %v, want Always", got)
	}
}

func TestConvertToDDMComponents_MCXAppleDomainAPIRejectsStaysWrapped(t *testing.T) {
	// Apple publishes a schema for com.apple.Safari, but the blueprints registry
	// has no such payload type — unwrapping it produced the opaque
	// "Failed to validate configuration." 400. It must stay wrapped in MCX.
	mc := mcxProfile("com.apple.Safari", `<key>AutoFillPasswords</key><false/>`)

	result, err := ConvertToDDMComponents([]byte(mc), true, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ProfileConfig == nil {
		t.Fatal("expected ProfileConfig containing the kept MCX payload")
	}
	var cfg map[string]any
	if err := json.Unmarshal(result.ProfileConfig, &cfg); err != nil {
		t.Fatalf("invalid ProfileConfig JSON: %v", err)
	}
	p := cfg["payloadContent"].([]any)[0].(map[string]any)
	if p["payloadType"] != mcxPayloadType {
		t.Fatalf("com.apple.Safari should stay MCX-wrapped, got payloadType %v", p["payloadType"])
	}
	if _, ok := p["PayloadContent"].(map[string]any)["com.apple.Safari"]; !ok {
		t.Errorf("expected com.apple.Safari preserved inside MCX, got %v", p["PayloadContent"])
	}
}

func TestConvertToDDMComponents_DirectUnsupportedPayloadWrapped(t *testing.T) {
	// The Jamf Pro Restrictions UI emits preference-domain payloads directly
	// (com.apple.ShareKitHelper, com.apple.systemuiserver, com.apple.MCX, …). The
	// blueprints registry refuses all of them standalone, which failed the whole
	// import; each is delivered as Custom Settings instead. com.apple.finder is a
	// supported type in the same profile and must stay bare.
	mc := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>PayloadContent</key>
	<array>
		<dict>
			<key>PayloadType</key>
			<string>com.apple.ShareKitHelper</string>
			<key>SHKAllowedShareServices</key>
			<array><string>com.apple.share.AirDrop</string></array>
		</dict>
		<dict>
			<key>PayloadType</key>
			<string>com.apple.MCX</string>
			<key>safariAllowAutoFill</key>
			<true/>
		</dict>
		<dict>
			<key>PayloadType</key>
			<string>com.apple.finder</string>
			<key>ProhibitBurn</key>
			<false/>
		</dict>
	</array>
	<key>PayloadDisplayName</key>
	<string>Restrictions</string>
	<key>PayloadType</key>
	<string>Configuration</string>
	<key>PayloadVersion</key>
	<integer>1</integer>
</dict>
</plist>`

	result, err := ConvertToDDMComponents([]byte(mc), true, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ProfileConfig == nil {
		t.Fatal("expected ProfileConfig")
	}
	var cfg map[string]any
	if err := json.Unmarshal(result.ProfileConfig, &cfg); err != nil {
		t.Fatalf("invalid ProfileConfig JSON: %v", err)
	}

	byType := map[string][]map[string]any{}
	for _, item := range cfg["payloadContent"].([]any) {
		p := item.(map[string]any)
		pt, _ := p["payloadType"].(string)
		byType[pt] = append(byType[pt], p)
	}

	if len(byType["com.apple.finder"]) != 1 {
		t.Errorf("supported com.apple.finder should stay bare, got %v", byType)
	}
	if len(byType[mcxPayloadType]) != 2 {
		t.Fatalf("expected the two unsupported payloads wrapped as MCX, got %v", byType)
	}
	wrapped := map[string]bool{}
	for _, p := range byType[mcxPayloadType] {
		for domain := range p["PayloadContent"].(map[string]any) {
			wrapped[domain] = true
		}
	}
	for _, want := range []string{"com.apple.ShareKitHelper", "com.apple.MCX"} {
		if !wrapped[want] {
			t.Errorf("expected %s delivered as Custom Settings, got domains %v", want, wrapped)
		}
	}
}

func TestConvertToDDMComponents_JamfUserPreferencesSpellingRewritten(t *testing.T) {
	// Jamf Pro writes com.apple.preferences.users; only Apple's declared
	// payloadtype (com.apple.preference.users) is accepted, and once rewritten it
	// is a supported standalone payload, so it must not be MCX-wrapped.
	mc := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>PayloadContent</key>
	<array>
		<dict>
			<key>PayloadType</key>
			<string>com.apple.preferences.users</string>
			<key>DisableUsingiCloudPassword</key>
			<false/>
		</dict>
	</array>
	<key>PayloadDisplayName</key>
	<string>User Prefs</string>
	<key>PayloadType</key>
	<string>Configuration</string>
	<key>PayloadVersion</key>
	<integer>1</integer>
</dict>
</plist>`

	result, err := ConvertToDDMComponents([]byte(mc), true, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(result.ProfileConfig, &cfg); err != nil {
		t.Fatalf("invalid ProfileConfig JSON: %v", err)
	}
	p := cfg["payloadContent"].([]any)[0].(map[string]any)
	if p["payloadType"] != "com.apple.preference.users" {
		t.Errorf("payloadType = %v, want com.apple.preference.users", p["payloadType"])
	}

	var rewrote bool
	for _, w := range result.Warnings {
		if strings.Contains(w, "canonical spelling") {
			rewrote = true
		}
	}
	if !rewrote {
		t.Errorf("expected a warning naming the rewrite, got %v", result.Warnings)
	}
}
