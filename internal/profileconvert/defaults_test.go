// Copyright 2026, Jamf Software LLC

package profileconvert

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
)

const testSchemaYAML = `title: Dock
description: The payload that configures the Dock.
payload:
  payloadtype: com.apple.dock
payloadkeys:
- key: tilesize
  type: <integer>
  presence: optional
  content: The tile size.
- key: size-immutable
  type: <boolean>
  presence: optional
  default: false
  content: If true, locks the size slider.
- key: magnification
  type: <boolean>
  presence: optional
  default: false
  content: If true, enables magnification.
- key: orientation
  type: <string>
  presence: optional
  content: The orientation of the Dock.
- key: autohide
  type: <boolean>
  presence: optional
  default: false
  content: If true, automatically hides the Dock.
- key: largesize
  type: <integer>
  presence: optional
  content: The size of the largest magnification.
`

const testScreensaverSchemaYAML = `title: Screensaver
payload:
  payloadtype: com.apple.screensaver
payloadkeys:
- key: askForPassword
  type: <boolean>
  presence: optional
  default: false
- key: askForPasswordDelay
  type: <integer>
  presence: optional
- key: idleTime
  type: <integer>
  presence: optional
- key: moduleName
  type: <string>
  presence: required
`

const testRestrictionsSchemaYAML = `title: Restrictions
payload:
  payloadtype: com.apple.applicationaccess
payloadkeys:
- key: allowCamera
  type: <boolean>
  presence: optional
  default: true
  content: If false, disables the camera.
- key: allowSafari
  type: <boolean>
  presence: optional
  default: true
  content: If false, disables Safari.
- key: allowAirDrop
  type: <boolean>
  presence: optional
  default: true
  content: If false, disables AirDrop.
- key: safariAllowAutoFill
  type: <boolean>
  presence: optional
  default: true
  content: If false, disables auto-fill.
- key: forceEncryptedBackup
  type: <boolean>
  presence: optional
  default: false
  content: If true, forces encrypted backups.
- key: maxAllowedMajorOSVersion
  type: <integer>
  presence: optional
  content: Maximum major OS version.
`

func TestParseSchemaDefaults(t *testing.T) {
	defaults, err := ParseSchemaDefaults([]byte(testSchemaYAML))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Keys with defaults
	if v, ok := defaults.Defaults["size-immutable"]; !ok || v != false {
		t.Errorf("size-immutable default = %v (%T), want false", v, v)
	}
	if v, ok := defaults.Defaults["magnification"]; !ok || v != false {
		t.Errorf("magnification default = %v, want false", v)
	}
	if v, ok := defaults.Defaults["autohide"]; !ok || v != false {
		t.Errorf("autohide default = %v, want false", v)
	}

	// Keys without defaults should not be in the map
	if _, ok := defaults.Defaults["tilesize"]; ok {
		t.Error("tilesize should not have a default")
	}
	if _, ok := defaults.Defaults["orientation"]; ok {
		t.Error("orientation should not have a default")
	}
	if _, ok := defaults.Defaults["largesize"]; ok {
		t.Error("largesize should not have a default")
	}
}

func TestParseSchemaDefaults_BooleanTrue(t *testing.T) {
	defaults, err := ParseSchemaDefaults([]byte(testRestrictionsSchemaYAML))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if v := defaults.Defaults["allowCamera"]; v != true {
		t.Errorf("allowCamera default = %v, want true", v)
	}
	if v := defaults.Defaults["forceEncryptedBackup"]; v != false {
		t.Errorf("forceEncryptedBackup default = %v, want false", v)
	}
	if _, ok := defaults.Defaults["maxAllowedMajorOSVersion"]; ok {
		t.Error("maxAllowedMajorOSVersion should not have a default")
	}
}

func TestParseSchemaDefaults_IntegerDefault(t *testing.T) {
	yaml := `payloadkeys:
- key: maxGracePeriod
  type: <integer>
  presence: optional
  default: 0
- key: askForPasswordDelay
  type: <integer>
  presence: optional
  default: 60
`
	defaults, err := ParseSchemaDefaults([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v := defaults.Defaults["maxGracePeriod"]; v != int64(0) {
		t.Errorf("maxGracePeriod = %v (%T), want int64(0)", v, v)
	}
	if v := defaults.Defaults["askForPasswordDelay"]; v != int64(60) {
		t.Errorf("askForPasswordDelay = %v (%T), want int64(60)", v, v)
	}
}

func TestParseSchemaDefaults_StringDefault(t *testing.T) {
	yaml := `payloadkeys:
- key: orientation
  type: <string>
  presence: optional
  default: bottom
`
	defaults, err := ParseSchemaDefaults([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v := defaults.Defaults["orientation"]; v != "bottom" {
		t.Errorf("orientation = %v, want 'bottom'", v)
	}
}

func TestParseSchemaDefaults_InvalidYAML(t *testing.T) {
	_, err := ParseSchemaDefaults([]byte("{{not yaml"))
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestParseSchemaDefaults_EmptyPayloadKeys(t *testing.T) {
	defaults, err := ParseSchemaDefaults([]byte("title: Empty\npayloadkeys: []\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(defaults.Defaults) != 0 {
		t.Errorf("expected no defaults, got %d", len(defaults.Defaults))
	}
}

func TestStripDefaultKeys_StripsMatchingDefaults(t *testing.T) {
	entry := map[string]any{
		"payloadType":       "com.apple.dock",
		"payloadIdentifier": "abc-123",
		"size-immutable":    false,       // matches default
		"magnification":     false,       // matches default
		"autohide":          true,        // does NOT match default (false)
		"tilesize":          float64(48), // no default for this key
		"orientation":       "left",      // no default for this key
	}

	defaults := &SchemaDefaults{
		Defaults: map[string]any{
			"size-immutable": false,
			"magnification":  false,
			"autohide":       false,
		},
	}

	count, stripped := StripDefaultKeys(entry, defaults)

	if count != 2 {
		t.Errorf("stripped count = %d, want 2", count)
	}
	sort.Strings(stripped)
	if len(stripped) != 2 || stripped[0] != "magnification" || stripped[1] != "size-immutable" {
		t.Errorf("stripped = %v, want [magnification, size-immutable]", stripped)
	}

	// Verify entry was mutated
	if _, exists := entry["size-immutable"]; exists {
		t.Error("size-immutable should have been stripped")
	}
	if _, exists := entry["magnification"]; exists {
		t.Error("magnification should have been stripped")
	}
	if _, exists := entry["autohide"]; !exists {
		t.Error("autohide should NOT have been stripped (value differs from default)")
	}
	if _, exists := entry["tilesize"]; !exists {
		t.Error("tilesize should NOT have been stripped (no default)")
	}
	if entry["payloadType"] != "com.apple.dock" {
		t.Error("payloadType should never be stripped")
	}
	if entry["payloadIdentifier"] != "abc-123" {
		t.Error("payloadIdentifier should never be stripped")
	}
}

func TestStripDefaultKeys_NilDefaults(t *testing.T) {
	entry := map[string]any{
		"payloadType": "com.apple.dock",
		"tilesize":    float64(48),
	}
	count, stripped := StripDefaultKeys(entry, nil)
	if count != 0 || stripped != nil {
		t.Error("nil defaults should result in no stripping")
	}
}

func TestStripDefaultKeys_EmptyDefaults(t *testing.T) {
	entry := map[string]any{
		"payloadType": "com.apple.dock",
		"tilesize":    float64(48),
	}
	count, _ := StripDefaultKeys(entry, &SchemaDefaults{Defaults: map[string]any{}})
	if count != 0 {
		t.Error("empty defaults should result in no stripping")
	}
}

func TestStripDefaultKeys_IntegerComparison(t *testing.T) {
	// Plist values come through as float64 after JSON, YAML defaults are int64
	entry := map[string]any{
		"payloadType":   "test",
		"gracePeriod":   float64(60),  // matches int64(60)
		"customTimeout": float64(120), // does not match int64(60)
	}
	defaults := &SchemaDefaults{
		Defaults: map[string]any{
			"gracePeriod":   int64(60),
			"customTimeout": int64(60),
		},
	}
	count, _ := StripDefaultKeys(entry, defaults)
	if count != 1 {
		t.Errorf("stripped count = %d, want 1 (only gracePeriod)", count)
	}
	if _, exists := entry["gracePeriod"]; exists {
		t.Error("gracePeriod should have been stripped")
	}
	if _, exists := entry["customTimeout"]; !exists {
		t.Error("customTimeout should NOT have been stripped")
	}
}

func TestStripDefaultKeys_RestrictionsBooleans(t *testing.T) {
	// Simulate a typical Restrictions profile where most keys are at default (true)
	entry := map[string]any{
		"payloadType":       "com.apple.applicationaccess",
		"payloadIdentifier": "abc-123",
		"allowCamera":       true,  // default: true → strip
		"allowSafari":       true,  // default: true → strip
		"allowAirDrop":      false, // default: true → keep (user explicitly restricted)
	}
	defaults, _ := ParseSchemaDefaults([]byte(testRestrictionsSchemaYAML))
	count, _ := StripDefaultKeys(entry, defaults)
	if count != 2 {
		t.Errorf("stripped count = %d, want 2", count)
	}
	if _, exists := entry["allowAirDrop"]; !exists {
		t.Error("allowAirDrop should NOT have been stripped — it differs from default")
	}
}

func TestStripConfigDefaults_EndToEnd(t *testing.T) {
	// Build a config JSON with a payload that has some default-value keys
	config := json.RawMessage(`{
  "payloadDisplayName": "Test Profile",
  "payloadContent": [
    {
      "payloadType": "com.apple.dock",
      "payloadIdentifier": "abc-123",
      "size-immutable": false,
      "magnification": false,
      "autohide": true,
      "tilesize": 48
    }
  ]
}`)

	// Create a fetcher with pre-populated cache (avoids HTTP round-trip).
	// We test the schema parsing and stripping logic directly via
	// ParseSchemaDefaults and StripDefaultKeys; this test verifies the
	// StripConfigDefaults orchestration end-to-end.
	fetcher := NewSchemaFetcher(nil)
	defaults, _ := ParseSchemaDefaults([]byte(testSchemaYAML))
	fetcher.mu.Lock()
	fetcher.cache["com.apple.dock"] = &schemaResult{defaults: defaults}
	fetcher.mu.Unlock()

	result, msgs := StripConfigDefaults(config, fetcher)

	// Parse the result
	var parsed map[string]any
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	content := parsed["payloadContent"].([]any)
	payload := content[0].(map[string]any)

	// Default-value keys should be stripped
	if _, exists := payload["size-immutable"]; exists {
		t.Error("size-immutable should have been stripped (matches default: false)")
	}
	if _, exists := payload["magnification"]; exists {
		t.Error("magnification should have been stripped (matches default: false)")
	}

	// Non-default keys should remain
	if payload["autohide"] != true {
		t.Error("autohide should remain (value true != default false)")
	}
	if payload["tilesize"] != float64(48) {
		t.Error("tilesize should remain (no default in schema)")
	}

	// Should have a message about stripping
	if len(msgs) == 0 {
		t.Error("expected messages about stripped keys")
	}
}

func TestStripConfigDefaults_NoSchemaAvailable(t *testing.T) {
	config := json.RawMessage(`{
  "payloadDisplayName": "Test",
  "payloadContent": [
    {
      "payloadType": "com.example.unknown",
      "payloadIdentifier": "abc",
      "someKey": true
    }
  ]
}`)

	fetcher := NewSchemaFetcher(nil)
	// Pre-populate cache with nil (no schema available)
	fetcher.mu.Lock()
	fetcher.cache["com.example.unknown"] = &schemaResult{defaults: nil}
	fetcher.mu.Unlock()

	result, msgs := StripConfigDefaults(config, fetcher)

	// Nothing should be stripped
	var parsed map[string]any
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	content := parsed["payloadContent"].([]any)
	payload := content[0].(map[string]any)
	if _, exists := payload["someKey"]; !exists {
		t.Error("someKey should not have been stripped when no schema available")
	}

	// Should have a message about missing schema
	if len(msgs) != 1 {
		t.Errorf("expected 1 message about missing schema, got %d: %v", len(msgs), msgs)
	}
}

func TestStripConfigDefaults_RemovesEmptyPayload(t *testing.T) {
	// Profile with two payloads: one where ALL keys are defaults, one with real settings
	config := json.RawMessage(`{
  "payloadDisplayName": "Multi Profile",
  "payloadContent": [
    {
      "payloadType": "com.apple.applicationaccess",
      "payloadIdentifier": "abc-111",
      "allowCamera": true,
      "allowSafari": true
    },
    {
      "payloadType": "com.apple.dock",
      "payloadIdentifier": "abc-222",
      "size-immutable": false,
      "autohide": true,
      "tilesize": 48
    }
  ]
}`)

	// Restrictions: both keys default to true → all stripped → empty payload
	restrictionsDefaults, _ := ParseSchemaDefaults([]byte(testRestrictionsSchemaYAML))
	// Dock: size-immutable defaults to false (stripped), autohide defaults to false (kept), tilesize has no default (kept)
	dockDefaults, _ := ParseSchemaDefaults([]byte(testSchemaYAML))

	fetcher := NewSchemaFetcher(nil)
	fetcher.mu.Lock()
	fetcher.cache["com.apple.applicationaccess"] = &schemaResult{defaults: restrictionsDefaults}
	fetcher.cache["com.apple.dock"] = &schemaResult{defaults: dockDefaults}
	fetcher.mu.Unlock()

	result, msgs := StripConfigDefaults(config, fetcher)

	var parsed map[string]any
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	content := parsed["payloadContent"].([]any)

	// Restrictions payload should be removed entirely (all keys were defaults)
	if len(content) != 1 {
		t.Fatalf("expected 1 payload after removing empty, got %d", len(content))
	}

	// Remaining payload should be the dock
	remaining := content[0].(map[string]any)
	if remaining["payloadType"] != "com.apple.dock" {
		t.Errorf("expected remaining payload to be com.apple.dock, got %v", remaining["payloadType"])
	}

	// Check messages mention both stripping and removal
	hasRemoval := false
	for _, m := range msgs {
		if strings.Contains(m, "removed payload") && strings.Contains(m, "applicationaccess") {
			hasRemoval = true
		}
	}
	if !hasRemoval {
		t.Errorf("expected removal message for applicationaccess, got: %v", msgs)
	}
}

func TestStripConfigDefaults_AllPayloadsRemoved(t *testing.T) {
	// Single payload where every key is at default
	config := json.RawMessage(`{
  "payloadDisplayName": "All Defaults",
  "payloadContent": [
    {
      "payloadType": "com.apple.applicationaccess",
      "payloadIdentifier": "abc-111",
      "allowCamera": true,
      "allowSafari": true
    }
  ]
}`)

	defaults, _ := ParseSchemaDefaults([]byte(testRestrictionsSchemaYAML))
	fetcher := NewSchemaFetcher(nil)
	fetcher.mu.Lock()
	fetcher.cache["com.apple.applicationaccess"] = &schemaResult{defaults: defaults}
	fetcher.mu.Unlock()

	result, msgs := StripConfigDefaults(config, fetcher)

	var parsed map[string]any
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	content := parsed["payloadContent"].([]any)
	if len(content) != 0 {
		t.Errorf("expected 0 payloads after stripping all defaults, got %d", len(content))
	}

	// Should have messages for both stripping and removal
	if len(msgs) < 2 {
		t.Errorf("expected at least 2 messages (strip + remove), got %d: %v", len(msgs), msgs)
	}
}

func TestPayloadIsEmpty(t *testing.T) {
	if !payloadIsEmpty(map[string]any{"payloadType": "x", "payloadIdentifier": "y"}) {
		t.Error("structural-only entry should be empty")
	}
	if payloadIsEmpty(map[string]any{"payloadType": "x", "payloadIdentifier": "y", "setting": true}) {
		t.Error("entry with a setting key should not be empty")
	}
	if !payloadIsEmpty(map[string]any{"payloadType": "x"}) {
		t.Error("payloadType-only entry should be empty")
	}
	if !payloadIsEmpty(map[string]any{}) {
		t.Error("empty map should be empty")
	}
}

func TestParseSchemaDefaults_RequiredFields(t *testing.T) {
	defaults, err := ParseSchemaDefaults([]byte(testScreensaverSchemaYAML))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(defaults.Required) != 1 || defaults.Required[0] != "moduleName" {
		t.Errorf("Required = %v, want [moduleName]", defaults.Required)
	}
	// Optional fields should not be in Required
	for _, r := range defaults.Required {
		if r == "askForPassword" || r == "idleTime" {
			t.Errorf("optional field %q should not be in Required", r)
		}
	}
}

func TestMissingRequiredKeys(t *testing.T) {
	defaults, _ := ParseSchemaDefaults([]byte(testScreensaverSchemaYAML))

	// Payload with required field present
	entry := map[string]any{
		"payloadType": "com.apple.screensaver",
		"moduleName":  "Flurry",
		"idleTime":    float64(600),
	}
	if missing := MissingRequiredKeys(entry, defaults); len(missing) != 0 {
		t.Errorf("expected no missing keys, got %v", missing)
	}

	// Payload with required field absent (empty string was stripped)
	entry2 := map[string]any{
		"payloadType": "com.apple.screensaver",
		"idleTime":    float64(600),
	}
	missing := MissingRequiredKeys(entry2, defaults)
	if len(missing) != 1 || missing[0] != "moduleName" {
		t.Errorf("expected [moduleName] missing, got %v", missing)
	}

	// Nil defaults — no required info
	if missing := MissingRequiredKeys(entry2, nil); len(missing) != 0 {
		t.Error("nil defaults should return no missing keys")
	}
}

func TestStripConfigDefaults_RemovesMissingRequired(t *testing.T) {
	// Screensaver payload where moduleName was stripped (empty string) and is now absent
	config := json.RawMessage(`{
  "payloadDisplayName": "Login Window",
  "payloadContent": [
    {
      "payloadType": "com.apple.screensaver",
      "payloadIdentifier": "abc-111",
      "askForPassword": true,
      "idleTime": 600
    },
    {
      "payloadType": "com.apple.dock",
      "payloadIdentifier": "abc-222",
      "tilesize": 48,
      "autohide": true
    }
  ]
}`)

	screensaverDefaults, _ := ParseSchemaDefaults([]byte(testScreensaverSchemaYAML))
	dockDefaults, _ := ParseSchemaDefaults([]byte(testSchemaYAML))

	fetcher := NewSchemaFetcher(nil)
	fetcher.mu.Lock()
	fetcher.cache["com.apple.screensaver"] = &schemaResult{defaults: screensaverDefaults}
	fetcher.cache["com.apple.dock"] = &schemaResult{defaults: dockDefaults}
	fetcher.mu.Unlock()

	result, msgs := StripConfigDefaults(config, fetcher)

	var parsed map[string]any
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	content := parsed["payloadContent"].([]any)

	// Screensaver should be removed (missing required moduleName)
	if len(content) != 1 {
		t.Fatalf("expected 1 payload after removing broken screensaver, got %d", len(content))
	}
	if content[0].(map[string]any)["payloadType"] != "com.apple.dock" {
		t.Error("expected remaining payload to be com.apple.dock")
	}

	// Should have a message about the removal
	hasRemoval := false
	for _, m := range msgs {
		if strings.Contains(m, "removed payload") && strings.Contains(m, "screensaver") && strings.Contains(m, "moduleName") {
			hasRemoval = true
		}
	}
	if !hasRemoval {
		t.Errorf("expected removal message for screensaver with missing moduleName, got: %v", msgs)
	}
}

func TestValidatePayloads_RemovesMissingRequired(t *testing.T) {
	// Same scenario as StripConfigDefaults_RemovesMissingRequired but
	// through ValidatePayloads — no default stripping, just validation.
	config := json.RawMessage(`{
  "payloadDisplayName": "Login Window",
  "payloadContent": [
    {
      "payloadType": "com.apple.screensaver",
      "payloadIdentifier": "abc-111",
      "idleTime": 600,
      "loginWindowModulePath": "/System/Library/Screen Savers/Flurry.saver"
    },
    {
      "payloadType": "com.apple.loginwindow",
      "payloadIdentifier": "abc-222",
      "DisableConsoleAccess": true,
      "LoginwindowText": "Welcome"
    }
  ]
}`)

	screensaverDefaults, _ := ParseSchemaDefaults([]byte(testScreensaverSchemaYAML))
	// loginwindow has no required fields in our test schema
	loginwindowDefaults := &SchemaDefaults{Defaults: map[string]any{}, Required: nil}

	fetcher := NewSchemaFetcher(nil)
	fetcher.mu.Lock()
	fetcher.cache["com.apple.screensaver"] = &schemaResult{defaults: screensaverDefaults}
	fetcher.cache["com.apple.loginwindow"] = &schemaResult{defaults: loginwindowDefaults}
	fetcher.mu.Unlock()

	result, msgs := ValidatePayloads(config, fetcher)

	var parsed map[string]any
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	content := parsed["payloadContent"].([]any)

	if len(content) != 1 {
		t.Fatalf("expected 1 payload after validation, got %d", len(content))
	}
	if content[0].(map[string]any)["payloadType"] != "com.apple.loginwindow" {
		t.Error("expected loginwindow to survive validation")
	}

	// Loginwindow settings should be untouched (no stripping in validate-only mode)
	lw := content[0].(map[string]any)
	if lw["DisableConsoleAccess"] != true {
		t.Error("DisableConsoleAccess should be untouched by validation")
	}

	// Should mention the screensaver removal
	found := false
	for _, m := range msgs {
		if strings.Contains(m, "screensaver") && strings.Contains(m, "moduleName") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected removal message for screensaver, got: %v", msgs)
	}
}

func TestValuesEqual(t *testing.T) {
	tests := []struct {
		name     string
		plistVal any
		yamlVal  any
		want     bool
	}{
		{"bool match", true, true, true},
		{"bool mismatch", true, false, false},
		{"float64 match", float64(48), float64(48), true},
		{"float64 vs int64", float64(60), int64(60), true},
		{"float64 mismatch", float64(48), float64(36), false},
		{"string match", "bottom", "bottom", true},
		{"string mismatch", "left", "bottom", false},
		{"type mismatch bool/string", true, "true", false},
		{"type mismatch int/string", float64(1), "1", false},
		{"nil values", nil, nil, false}, // complex types not compared
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := valuesEqual(tc.plistVal, tc.yamlVal)
			if got != tc.want {
				t.Errorf("valuesEqual(%v, %v) = %v, want %v", tc.plistVal, tc.yamlVal, got, tc.want)
			}
		})
	}
}

func TestSchemaFetcher_CachesResults(t *testing.T) {
	fetcher := NewSchemaFetcher(nil)
	defaults, _ := ParseSchemaDefaults([]byte(testSchemaYAML))
	fetcher.mu.Lock()
	fetcher.cache["com.apple.dock"] = &schemaResult{defaults: defaults}
	fetcher.mu.Unlock()

	// First call — cache hit
	d1, _ := fetcher.FetchDefaults("com.apple.dock")
	// Second call — should also be cache hit
	d2, _ := fetcher.FetchDefaults("com.apple.dock")

	if d1 == nil || d2 == nil {
		t.Fatal("expected non-nil defaults")
	}
	if d1 != d2 {
		t.Error("expected same pointer from cache")
	}
}

func TestSchemaFilenameOverrides(t *testing.T) {
	// Verify the override map has entries for known problem types
	expected := map[string]string{
		"com.apple.MCX.Accounts":       "com.apple.MCX(Accounts)",
		"com.apple.MCX.MobileAccounts": "com.apple.MCX(Mobililty)",
		"com.apple.MCX.TimeServer":     "com.apple.MCX(TimeServer)",
		"com.apple.preference.users":   "com.apple.preferences.users",
	}
	for payloadType, wantFilename := range expected {
		got, ok := schemaFilenameOverrides[payloadType]
		if !ok {
			t.Errorf("missing override for %s", payloadType)
			continue
		}
		if got != wantFilename {
			t.Errorf("override for %s = %q, want %q", payloadType, got, wantFilename)
		}
	}
}
