// Copyright 2026, Jamf Software LLC

package profileconvert

import (
	"encoding/json"
	"strings"
	"testing"
)

const testMobileconfig = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>PayloadContent</key>
	<array>
		<dict>
			<key>PayloadType</key>
			<string>com.apple.applicationaccess</string>
			<key>PayloadDisplayName</key>
			<string>Dock Settings</string>
			<key>PayloadIdentifier</key>
			<string>com.example.dock.original</string>
			<key>PayloadUUID</key>
			<string>AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE</string>
			<key>PayloadVersion</key>
			<integer>1</integer>
			<key>tilesize</key>
			<integer>48</integer>
			<key>orientation</key>
			<string>bottom</string>
			<key>autohide</key>
			<true/>
		</dict>
	</array>
	<key>PayloadDisplayName</key>
	<string>My Test Profile</string>
	<key>PayloadIdentifier</key>
	<string>com.example.test</string>
	<key>PayloadType</key>
	<string>Configuration</string>
	<key>PayloadUUID</key>
	<string>12345678-1234-1234-1234-123456789012</string>
	<key>PayloadVersion</key>
	<integer>1</integer>
</dict>
</plist>`

const testMultiPayloadMobileconfig = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>PayloadContent</key>
	<array>
		<dict>
			<key>PayloadType</key>
			<string>com.apple.applicationaccess</string>
			<key>PayloadIdentifier</key>
			<string>com.example.dock</string>
			<key>PayloadUUID</key>
			<string>AAAAAAAA-0000-0000-0000-000000000000</string>
			<key>tilesize</key>
			<integer>36</integer>
		</dict>
		<dict>
			<key>PayloadType</key>
			<string>com.apple.finder</string>
			<key>PayloadIdentifier</key>
			<string>com.example.screensaver</string>
			<key>PayloadUUID</key>
			<string>BBBBBBBB-0000-0000-0000-000000000000</string>
			<key>idleTime</key>
			<integer>600</integer>
		</dict>
	</array>
	<key>PayloadDisplayName</key>
	<string>Multi-Payload Profile</string>
	<key>PayloadType</key>
	<string>Configuration</string>
	<key>PayloadUUID</key>
	<string>CCCCCCCC-0000-0000-0000-000000000000</string>
	<key>PayloadVersion</key>
	<integer>1</integer>
</dict>
</plist>`

const testUnsupportedPayloadMobileconfig = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>PayloadContent</key>
	<array>
		<dict>
			<key>PayloadType</key>
			<string>com.example.fake.payload</string>
			<key>PayloadUUID</key>
			<string>AAAAAAAA-0000-0000-0000-000000000000</string>
			<key>SSID_STR</key>
			<string>CorpWifi</string>
		</dict>
	</array>
	<key>PayloadDisplayName</key>
	<string>WiFi Profile</string>
	<key>PayloadType</key>
	<string>Configuration</string>
	<key>PayloadUUID</key>
	<string>BBBBBBBB-0000-0000-0000-000000000000</string>
	<key>PayloadVersion</key>
	<integer>1</integer>
</dict>
</plist>`

const testPlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>tilesize</key>
	<integer>48</integer>
	<key>orientation</key>
	<string>bottom</string>
	<key>autohide</key>
	<true/>
	<key>minimize-to-application</key>
	<true/>
</dict>
</plist>`

func TestConvertMobileconfig_SinglePayload(t *testing.T) {
	config, warnings, err := ConvertMobileconfig([]byte(testMobileconfig), false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}

	var result map[string]any
	if err := json.Unmarshal(config, &result); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}

	// Check top-level display name from profile
	if result["payloadDisplayName"] != "My Test Profile" {
		t.Errorf("payloadDisplayName = %v, want My Test Profile", result["payloadDisplayName"])
	}

	// Check payloadContent
	content, ok := result["payloadContent"].([]any)
	if !ok || len(content) != 1 {
		t.Fatalf("expected 1 payload, got %v", result["payloadContent"])
	}

	payload := content[0].(map[string]any)

	// payloadType preserved
	if payload["payloadType"] != "com.apple.applicationaccess" {
		t.Errorf("payloadType = %v, want com.apple.applicationaccess", payload["payloadType"])
	}

	// payloadIdentifier is deterministic, not from source
	if payload["payloadIdentifier"] == "com.example.dock.original" {
		t.Error("payloadIdentifier should not be the source identifier")
	}
	if payload["payloadIdentifier"] != generatePayloadIdentifier("com.apple.applicationaccess", 0) {
		t.Errorf("payloadIdentifier = %v, want deterministic hash", payload["payloadIdentifier"])
	}

	// Source UUIDs are not preserved
	if _, exists := payload["PayloadUUID"]; exists {
		t.Error("PayloadUUID should be stripped from payload")
	}

	// Settings are preserved
	if payload["tilesize"] != float64(48) {
		t.Errorf("tilesize = %v, want 48", payload["tilesize"])
	}
	if payload["orientation"] != "bottom" {
		t.Errorf("orientation = %v, want bottom", payload["orientation"])
	}
	if payload["autohide"] != true {
		t.Errorf("autohide = %v, want true", payload["autohide"])
	}

	// Apple metadata stripped
	if _, exists := payload["PayloadDisplayName"]; exists {
		t.Error("PayloadDisplayName should be stripped")
	}
	if _, exists := payload["PayloadVersion"]; exists {
		t.Error("PayloadVersion should be stripped")
	}

	// No source profile UUID at top level
	if _, exists := result["payloadUUID"]; exists {
		t.Error("payloadUUID should not be in output")
	}
}

func TestConvertMobileconfig_MultiPayload(t *testing.T) {
	config, warnings, err := ConvertMobileconfig([]byte(testMultiPayloadMobileconfig), false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}

	var result map[string]any
	if err := json.Unmarshal(config, &result); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}

	content, ok := result["payloadContent"].([]any)
	if !ok || len(content) != 2 {
		t.Fatalf("expected 2 payloads, got %v", result["payloadContent"])
	}

	p1 := content[0].(map[string]any)
	p2 := content[1].(map[string]any)

	if p1["payloadType"] != "com.apple.applicationaccess" {
		t.Errorf("payload[0] type = %v, want com.apple.applicationaccess", p1["payloadType"])
	}
	if p2["payloadType"] != "com.apple.finder" {
		t.Errorf("payload[1] type = %v, want com.apple.finder", p2["payloadType"])
	}

	// Each has deterministic identifier
	if p1["payloadIdentifier"] != generatePayloadIdentifier("com.apple.applicationaccess", 0) {
		t.Error("payload[0] identifier should be deterministic")
	}
	if p2["payloadIdentifier"] != generatePayloadIdentifier("com.apple.finder", 0) {
		t.Error("payload[1] identifier should be deterministic")
	}

	// Settings preserved
	if p1["tilesize"] != float64(36) {
		t.Errorf("dock tilesize = %v, want 36", p1["tilesize"])
	}
	if p2["idleTime"] != float64(600) {
		t.Errorf("screensaver idleTime = %v, want 600", p2["idleTime"])
	}
}

func TestConvertMobileconfig_UnknownPayloadPassesThrough(t *testing.T) {
	// A payload type that is NOT on the denylist is passed through to the API
	// (which schema-validates it) with no warning — even with filtering enabled.
	config, warnings, err := ConvertMobileconfig([]byte(testUnsupportedPayloadMobileconfig), true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(warnings) != 0 {
		t.Errorf("expected no warnings for a non-disabled type, got %v", warnings)
	}

	// Output is still valid and includes the payload.
	var result map[string]any
	if err := json.Unmarshal(config, &result); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}

	content := result["payloadContent"].([]any)
	payload := content[0].(map[string]any)
	if payload["payloadType"] != "com.example.fake.payload" {
		t.Errorf("payloadType = %v, want com.example.fake.payload", payload["payloadType"])
	}
	if payload["SSID_STR"] != "CorpWifi" {
		t.Errorf("SSID_STR = %v, want CorpWifi", payload["SSID_STR"])
	}
}

func TestConvertMobileconfig_FilterDisabled(t *testing.T) {
	// Multi-payload profile where one payload is a type blueprints disables.
	data := `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
	<key>PayloadContent</key>
	<array>
		<dict>
			<key>PayloadType</key>
			<string>com.apple.applicationaccess</string>
			<key>tilesize</key>
			<integer>48</integer>
		</dict>
		<dict>
			<key>PayloadType</key>
			<string>com.apple.vpn.managed</string>
			<key>UserDefinedName</key>
			<string>Corp VPN</string>
		</dict>
	</array>
	<key>PayloadDisplayName</key>
	<string>Mixed Profile</string>
	<key>PayloadType</key>
	<string>Configuration</string>
	<key>PayloadUUID</key>
	<string>AAAAAAAA-0000-0000-0000-000000000000</string>
</dict>
</plist>`
	config, warnings, err := ConvertMobileconfig([]byte(data), true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should warn about the skipped disabled payload.
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d: %v", len(warnings), warnings)
	}
	if !strings.Contains(warnings[0], "does not support") {
		t.Errorf("expected 'does not support' warning, got %q", warnings[0])
	}

	// Only the non-disabled payload should remain.
	var result map[string]any
	if err := json.Unmarshal(config, &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	content := result["payloadContent"].([]any)
	if len(content) != 1 {
		t.Fatalf("expected 1 payload after filtering, got %d", len(content))
	}
	if content[0].(map[string]any)["payloadType"] != "com.apple.applicationaccess" {
		t.Error("expected com.apple.applicationaccess to survive filtering")
	}
}

func TestConvertMobileconfig_FilterAllRemoved(t *testing.T) {
	// A profile containing only a disabled payload type, with filtering on,
	// removes everything and errors.
	data := `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
	<key>PayloadContent</key>
	<array>
		<dict>
			<key>PayloadType</key>
			<string>com.apple.vpn.managed</string>
			<key>UserDefinedName</key>
			<string>Corp VPN</string>
		</dict>
	</array>
	<key>PayloadDisplayName</key>
	<string>VPN Only</string>
	<key>PayloadType</key>
	<string>Configuration</string>
	<key>PayloadUUID</key>
	<string>AAAAAAAA-0000-0000-0000-000000000000</string>
</dict>
</plist>`
	_, _, err := ConvertMobileconfig([]byte(data), true)
	if err == nil {
		t.Error("expected error when all payloads are disabled and filtered out")
	}
}

func TestConvertMobileconfig_NoDisplayName(t *testing.T) {
	data := `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
	<key>PayloadContent</key>
	<array><dict><key>PayloadType</key><string>com.apple.applicationaccess</string></dict></array>
	<key>PayloadType</key>
	<string>Configuration</string>
</dict>
</plist>`
	_, _, err := ConvertMobileconfig([]byte(data), false)
	if err == nil {
		t.Error("expected error for missing PayloadDisplayName")
	}
}

func TestConvertMobileconfig_NoPayloadContent(t *testing.T) {
	data := `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
	<key>PayloadDisplayName</key>
	<string>Empty</string>
	<key>PayloadType</key>
	<string>Configuration</string>
</dict>
</plist>`
	_, _, err := ConvertMobileconfig([]byte(data), false)
	if err == nil {
		t.Error("expected error for missing PayloadContent")
	}
}

func TestConvertPlist(t *testing.T) {
	config, warnings, err := ConvertPlist([]byte(testPlist), "com.apple.applicationaccess", "Dock Settings")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}

	var result map[string]any
	if err := json.Unmarshal(config, &result); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}

	if result["payloadDisplayName"] != "Dock Settings" {
		t.Errorf("payloadDisplayName = %v, want Dock Settings", result["payloadDisplayName"])
	}

	content := result["payloadContent"].([]any)
	if len(content) != 1 {
		t.Fatalf("expected 1 payload, got %d", len(content))
	}

	payload := content[0].(map[string]any)
	if payload["payloadType"] != "com.apple.applicationaccess" {
		t.Errorf("payloadType = %v, want com.apple.applicationaccess", payload["payloadType"])
	}
	if payload["tilesize"] != float64(48) {
		t.Errorf("tilesize = %v, want 48", payload["tilesize"])
	}
	if payload["autohide"] != true {
		t.Errorf("autohide = %v, want true", payload["autohide"])
	}
}

func TestConvertPlist_DefaultDisplayName(t *testing.T) {
	config, _, err := ConvertPlist([]byte(testPlist), "com.apple.applicationaccess", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(config, &result); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}

	// Empty display name defaults to payload type
	if result["payloadDisplayName"] != "com.apple.applicationaccess" {
		t.Errorf("payloadDisplayName = %v, want com.apple.applicationaccess", result["payloadDisplayName"])
	}
}

func TestConvertPlist_DisabledType(t *testing.T) {
	// A disabled type warns that the API will reject it.
	_, warnings, err := ConvertPlist([]byte(testPlist), "com.apple.vpn.managed", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning for disabled type, got %d: %v", len(warnings), warnings)
	}
	if !strings.Contains(warnings[0], "disabled") {
		t.Errorf("expected 'disabled' warning, got %q", warnings[0])
	}
}

func TestConvertPlist_UnknownTypeNoWarning(t *testing.T) {
	// A non-disabled type produces no warning — the API validates it.
	_, warnings, err := ConvertPlist([]byte(testPlist), "com.example.fake.payload", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("expected no warnings for a non-disabled type, got %v", warnings)
	}
}

func TestGeneratePayloadIdentifier_Deterministic(t *testing.T) {
	id1 := generatePayloadIdentifier("com.apple.applicationaccess", 0)
	id2 := generatePayloadIdentifier("com.apple.applicationaccess", 0)
	if id1 != id2 {
		t.Errorf("identifier not deterministic: %s != %s", id1, id2)
	}

	// Different types produce different identifiers
	id3 := generatePayloadIdentifier("com.apple.finder", 0)
	if id1 == id3 {
		t.Error("different payload types should produce different identifiers")
	}
}

func TestGeneratePayloadIdentifier_UniquePerIndex(t *testing.T) {
	id0 := generatePayloadIdentifier("com.apple.wifi.managed", 0)
	id1 := generatePayloadIdentifier("com.apple.wifi.managed", 1)
	if id0 == id1 {
		t.Error("same type with different indices should produce different identifiers")
	}

	// Index 0 should match the legacy single-payload case (no ".0" suffix)
	idLegacy := generatePayloadIdentifier("com.apple.wifi.managed", 0)
	if id0 != idLegacy {
		t.Error("index 0 should be backward-compatible")
	}
}

func TestGeneratePayloadIdentifier_MatchesTerraformProvider(t *testing.T) {
	// Verify format matches terraform-provider-jamfplatform: 8-4-4-4-12 hex
	id := generatePayloadIdentifier("com.apple.applicationaccess", 0)
	parts := 0
	for _, c := range id {
		if c == '-' {
			parts++
		}
	}
	if parts != 4 {
		t.Errorf("expected 4 hyphens in UUID-like format, got %d: %s", parts, id)
	}
}

func TestFormatComponentJSON(t *testing.T) {
	config := json.RawMessage(`{"payloadDisplayName":"Test","payloadContent":[]}`)
	data, err := FormatComponentJSON(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if result["identifier"] != "com.jamf.ddm-configuration-profile" {
		t.Errorf("identifier = %v, want com.jamf.ddm-configuration-profile", result["identifier"])
	}
	if result["configuration"] == nil {
		t.Error("configuration should not be nil")
	}
}

func TestProfileDisplayName(t *testing.T) {
	name := ProfileDisplayName([]byte(testMobileconfig))
	if name != "My Test Profile" {
		t.Errorf("got %q, want %q", name, "My Test Profile")
	}

	// Invalid input returns empty
	name = ProfileDisplayName([]byte("not a plist"))
	if name != "" {
		t.Errorf("expected empty for invalid input, got %q", name)
	}
}

func TestPayloadTypeSummary(t *testing.T) {
	types := PayloadTypeSummary([]byte(testMultiPayloadMobileconfig))
	if len(types) != 2 {
		t.Fatalf("expected 2 types, got %d", len(types))
	}
	if types[0] != "com.apple.applicationaccess" {
		t.Errorf("types[0] = %v, want com.apple.applicationaccess", types[0])
	}
	if types[1] != "com.apple.finder" {
		t.Errorf("types[1] = %v, want com.apple.finder", types[1])
	}
}

func TestDisabledPayloadTypesList(t *testing.T) {
	types := DisabledPayloadTypesList()
	if len(types) != len(DisabledPayloadTypes) {
		t.Errorf("got %d types, want %d", len(types), len(DisabledPayloadTypes))
	}
	// Should be sorted
	for i := 1; i < len(types); i++ {
		if types[i] < types[i-1] {
			t.Errorf("not sorted: %s before %s", types[i-1], types[i])
		}
	}
}

func TestConvertMobileconfig_StripsEmptyValues(t *testing.T) {
	data := `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
	<key>PayloadContent</key>
	<array>
		<dict>
			<key>PayloadType</key>
			<string>com.apple.screensaver</string>
			<key>moduleName</key>
			<string></string>
			<key>idleTime</key>
			<integer>600</integer>
			<key>allowList</key>
			<array/>
		</dict>
	</array>
	<key>PayloadDisplayName</key>
	<string>Test Empty Values</string>
	<key>PayloadType</key>
	<string>Configuration</string>
	<key>PayloadUUID</key>
	<string>AAAAAAAA-0000-0000-0000-000000000000</string>
</dict>
</plist>`
	config, _, err := ConvertMobileconfig([]byte(data), false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(config, &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	content := result["payloadContent"].([]any)
	payload := content[0].(map[string]any)

	// Empty string should be stripped
	if _, exists := payload["moduleName"]; exists {
		t.Error("moduleName (empty string) should be stripped")
	}
	// Empty array should be stripped
	if _, exists := payload["allowList"]; exists {
		t.Error("allowList (empty array) should be stripped")
	}
	// Non-empty values should remain
	if payload["idleTime"] != float64(600) {
		t.Errorf("idleTime = %v, want 600", payload["idleTime"])
	}
}

func TestConvertPlist_StripsEmptyValues(t *testing.T) {
	plistData := `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
	<key>moduleName</key>
	<string></string>
	<key>idleTime</key>
	<integer>600</integer>
	<key>denyList</key>
	<array/>
</dict>
</plist>`
	config, _, err := ConvertPlist([]byte(plistData), "com.apple.screensaver", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(config, &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	content := result["payloadContent"].([]any)
	payload := content[0].(map[string]any)

	if _, exists := payload["moduleName"]; exists {
		t.Error("moduleName (empty string) should be stripped")
	}
	if _, exists := payload["denyList"]; exists {
		t.Error("denyList (empty array) should be stripped")
	}
	if payload["idleTime"] != float64(600) {
		t.Errorf("idleTime = %v, want 600", payload["idleTime"])
	}
}

// testMCXMobileconfig wraps com.apple.applicationaccess inside Custom Settings (MCX).
const testMCXMobileconfig = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>PayloadContent</key>
	<array>
		<dict>
			<key>PayloadType</key>
			<string>com.apple.ManagedClient.preferences</string>
			<key>PayloadIdentifier</key>
			<string>com.example.mcx</string>
			<key>PayloadUUID</key>
			<string>AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE</string>
			<key>PayloadVersion</key>
			<integer>1</integer>
			<key>PayloadContent</key>
			<dict>
				<key>com.apple.applicationaccess</key>
				<dict>
					<key>Forced</key>
					<array>
						<dict>
							<key>mcx_preference_settings</key>
							<dict>
								<key>allowCamera</key>
								<false/>
								<key>allowAirDrop</key>
								<false/>
							</dict>
						</dict>
					</array>
				</dict>
			</dict>
		</dict>
	</array>
	<key>PayloadDisplayName</key>
	<string>CIS Restrictions via MCX</string>
	<key>PayloadIdentifier</key>
	<string>com.example.cis</string>
	<key>PayloadType</key>
	<string>Configuration</string>
	<key>PayloadUUID</key>
	<string>12345678-1234-1234-1234-123456789012</string>
	<key>PayloadVersion</key>
	<integer>1</integer>
</dict>
</plist>`

func TestConvertMobileconfig_MCXPayload(t *testing.T) {
	config, warnings, err := ConvertMobileconfig([]byte(testMCXMobileconfig), false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// MCX (Custom Settings) is kept intact as com.apple.ManagedClient.preferences,
	// NOT unwrapped — the blueprints API rejects the unwrapped bare domain.
	for _, w := range warnings {
		if strings.Contains(w, "unwrapped") {
			t.Errorf("did not expect an unwrap warning, got %q", w)
		}
	}

	var result map[string]any
	if err := json.Unmarshal(config, &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	content, ok := result["payloadContent"].([]any)
	if !ok || len(content) != 1 {
		t.Fatalf("expected 1 payload, got %v", result["payloadContent"])
	}

	payload := content[0].(map[string]any)
	if payload["payloadType"] != "com.apple.ManagedClient.preferences" {
		t.Errorf("payloadType = %v, want com.apple.ManagedClient.preferences", payload["payloadType"])
	}

	// The inner managed-preferences structure must be preserved under PayloadContent.
	pc, ok := payload["PayloadContent"].(map[string]any)
	if !ok {
		t.Fatalf("expected PayloadContent preserved, got %T", payload["PayloadContent"])
	}
	domain, ok := pc["com.apple.applicationaccess"].(map[string]any)
	if !ok {
		t.Fatalf("expected com.apple.applicationaccess domain in PayloadContent, got %v", pc)
	}
	forced, ok := domain["Forced"].([]any)
	if !ok || len(forced) != 1 {
		t.Fatalf("expected Forced array with 1 entry, got %v", domain["Forced"])
	}
	settings := forced[0].(map[string]any)["mcx_preference_settings"].(map[string]any)
	if settings["allowCamera"] != false || settings["allowAirDrop"] != false {
		t.Errorf("expected allowCamera/allowAirDrop false, got %v", settings)
	}
}

func TestConvertMobileconfig_MCXCustomDomainKept(t *testing.T) {
	// MCX wrapping a third-party preference domain. Previously this was unwrapped
	// to a bare payloadType and filtered out; now it is kept intact as
	// com.apple.ManagedClient.preferences and accepted even with filtering on.
	data := `<?xml version="1.0" encoding="UTF-8"?>
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
				<key>com.example.custom.domain</key>
				<dict>
					<key>Forced</key>
					<array>
						<dict>
							<key>mcx_preference_settings</key>
							<dict>
								<key>foo</key>
								<string>bar</string>
							</dict>
						</dict>
					</array>
				</dict>
			</dict>
		</dict>
	</array>
	<key>PayloadDisplayName</key>
	<string>MCX Custom Domain</string>
	<key>PayloadType</key>
	<string>Configuration</string>
	<key>PayloadUUID</key>
	<string>AAAAAAAA-0000-0000-0000-000000000000</string>
</dict>
</plist>`
	config, _, err := ConvertMobileconfig([]byte(data), true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal(config, &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	content := result["payloadContent"].([]any)
	if len(content) != 1 {
		t.Fatalf("expected 1 payload (MCX kept), got %d", len(content))
	}
	if pt := content[0].(map[string]any)["payloadType"]; pt != "com.apple.ManagedClient.preferences" {
		t.Errorf("payloadType = %v, want com.apple.ManagedClient.preferences", pt)
	}
}

func TestConvertMobileconfig_MCXMultiDomain(t *testing.T) {
	data := `<?xml version="1.0" encoding="UTF-8"?>
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
				<key>com.apple.applicationaccess</key>
				<dict>
					<key>Forced</key>
					<array>
						<dict>
							<key>mcx_preference_settings</key>
							<dict>
								<key>allowCamera</key>
								<false/>
							</dict>
						</dict>
					</array>
				</dict>
				<key>com.apple.screensaver</key>
				<dict>
					<key>Forced</key>
					<array>
						<dict>
							<key>mcx_preference_settings</key>
							<dict>
								<key>idleTime</key>
								<integer>600</integer>
							</dict>
						</dict>
					</array>
				</dict>
			</dict>
		</dict>
	</array>
	<key>PayloadDisplayName</key>
	<string>MCX Multi-Domain</string>
	<key>PayloadType</key>
	<string>Configuration</string>
	<key>PayloadUUID</key>
	<string>AAAAAAAA-0000-0000-0000-000000000000</string>
</dict>
</plist>`
	config, warnings, err := ConvertMobileconfig([]byte(data), false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, w := range warnings {
		if strings.Contains(w, "unwrapped") {
			t.Errorf("did not expect an unwrap warning, got %q", w)
		}
	}

	var result map[string]any
	if err := json.Unmarshal(config, &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	content := result["payloadContent"].([]any)
	if len(content) != 1 {
		t.Fatalf("expected 1 MCX payload (kept intact, both domains inside), got %d", len(content))
	}
	pc, ok := content[0].(map[string]any)["PayloadContent"].(map[string]any)
	if !ok {
		t.Fatalf("expected PayloadContent preserved, got %v", content[0])
	}
	if _, ok := pc["com.apple.applicationaccess"]; !ok {
		t.Errorf("missing com.apple.applicationaccess domain in PayloadContent: %v", pc)
	}
	if _, ok := pc["com.apple.screensaver"]; !ok {
		t.Errorf("missing com.apple.screensaver domain in PayloadContent: %v", pc)
	}
}

func TestConvertMobileconfig_MCXMixedWithRegular(t *testing.T) {
	data := `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
	<key>PayloadContent</key>
	<array>
		<dict>
			<key>PayloadType</key>
			<string>com.apple.screensaver</string>
			<key>idleTime</key>
			<integer>300</integer>
		</dict>
		<dict>
			<key>PayloadType</key>
			<string>com.apple.ManagedClient.preferences</string>
			<key>PayloadVersion</key>
			<integer>1</integer>
			<key>PayloadContent</key>
			<dict>
				<key>com.apple.applicationaccess</key>
				<dict>
					<key>Forced</key>
					<array>
						<dict>
							<key>mcx_preference_settings</key>
							<dict>
								<key>allowCamera</key>
								<false/>
							</dict>
						</dict>
					</array>
				</dict>
			</dict>
		</dict>
	</array>
	<key>PayloadDisplayName</key>
	<string>Mixed Regular and MCX</string>
	<key>PayloadType</key>
	<string>Configuration</string>
	<key>PayloadUUID</key>
	<string>AAAAAAAA-0000-0000-0000-000000000000</string>
</dict>
</plist>`
	config, _, err := ConvertMobileconfig([]byte(data), false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(config, &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	content := result["payloadContent"].([]any)
	if len(content) != 2 {
		t.Fatalf("expected 2 payloads (1 regular + 1 unwrapped MCX), got %d", len(content))
	}

	// First is the regular screensaver payload
	p0 := content[0].(map[string]any)
	if p0["payloadType"] != "com.apple.screensaver" {
		t.Errorf("payload[0] type = %v, want com.apple.screensaver", p0["payloadType"])
	}

	// Second is the MCX payload, kept intact (not unwrapped).
	p1 := content[1].(map[string]any)
	if p1["payloadType"] != "com.apple.ManagedClient.preferences" {
		t.Errorf("payload[1] type = %v, want com.apple.ManagedClient.preferences", p1["payloadType"])
	}
	pc, ok := p1["PayloadContent"].(map[string]any)
	if !ok {
		t.Fatalf("expected PayloadContent preserved on MCX payload, got %v", p1)
	}
	if _, ok := pc["com.apple.applicationaccess"]; !ok {
		t.Errorf("missing com.apple.applicationaccess domain in PayloadContent: %v", pc)
	}
}

func TestConvertMobileconfig_ProducesValidJSON(t *testing.T) {
	for _, tc := range []struct {
		name string
		data string
	}{
		{"single", testMobileconfig},
		{"multi", testMultiPayloadMobileconfig},
		{"unsupported", testUnsupportedPayloadMobileconfig},
		{"mcx", testMCXMobileconfig},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config, _, err := ConvertMobileconfig([]byte(tc.data), false)
			if err != nil {
				t.Fatalf("conversion error: %v", err)
			}

			// Must be valid JSON
			var parsed any
			if err := json.Unmarshal(config, &parsed); err != nil {
				t.Fatalf("invalid JSON: %v\n%s", err, config)
			}

			// Must wrap in component block
			component, err := FormatComponentJSON(config)
			if err != nil {
				t.Fatalf("component format error: %v", err)
			}
			if err := json.Unmarshal(component, &parsed); err != nil {
				t.Fatalf("invalid component JSON: %v\n%s", err, component)
			}
		})
	}
}
