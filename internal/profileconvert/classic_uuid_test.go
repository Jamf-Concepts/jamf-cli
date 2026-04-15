// Copyright 2026, Jamf Software LLC

package profileconvert

import (
	"strings"
	"testing"

	"howett.net/plist"
)

// buildTestPlist creates an XML plist from the given map for use in tests.
func buildTestPlist(t *testing.T, m map[string]any) []byte {
	t.Helper()
	b, err := plist.MarshalIndent(m, plist.XMLFormat, "\t")
	if err != nil {
		t.Fatalf("building test plist: %v", err)
	}
	return b
}

func TestInjectIdentifiers_HappyPath(t *testing.T) {
	existing := buildTestPlist(t, map[string]any{
		"PayloadUUID":        "AAAA-BBBB-CCCC",
		"PayloadIdentifier":  "com.example.existing",
		"PayloadType":        "Configuration",
		"PayloadDisplayName": "Old Profile",
	})

	newP := buildTestPlist(t, map[string]any{
		"PayloadUUID":        "1111-2222-3333",
		"PayloadIdentifier":  "com.example.new",
		"PayloadType":        "Configuration",
		"PayloadDisplayName": "New Profile",
	})

	result, err := InjectIdentifiers(newP, existing)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out map[string]any
	if _, err := plist.Unmarshal(result, &out); err != nil {
		t.Fatalf("result is not valid plist: %v", err)
	}

	if got := out["PayloadUUID"]; got != "AAAA-BBBB-CCCC" {
		t.Errorf("PayloadUUID = %v, want AAAA-BBBB-CCCC", got)
	}
	if got := out["PayloadIdentifier"]; got != "com.example.existing" {
		t.Errorf("PayloadIdentifier = %v, want com.example.existing", got)
	}
	// Other fields from the new profile must be preserved.
	if got := out["PayloadDisplayName"]; got != "New Profile" {
		t.Errorf("PayloadDisplayName = %v, want New Profile", got)
	}
}

func TestInjectIdentifiers_EmptyExisting(t *testing.T) {
	newP := buildTestPlist(t, map[string]any{"PayloadUUID": "1111"})

	result, err := InjectIdentifiers(newP, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result) != string(newP) {
		t.Error("expected unchanged result for nil existing plist")
	}

	result2, err := InjectIdentifiers(newP, []byte{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result2) != string(newP) {
		t.Error("expected unchanged result for empty existing plist")
	}
}

func TestInjectIdentifiers_InvalidExisting(t *testing.T) {
	newP := buildTestPlist(t, map[string]any{"PayloadUUID": "1111"})

	result, err := InjectIdentifiers(newP, []byte("not a plist"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result) != string(newP) {
		t.Error("expected unchanged result for invalid existing plist")
	}
}

func TestInjectIdentifiers_NoUUIDsInExisting(t *testing.T) {
	existing := buildTestPlist(t, map[string]any{"PayloadType": "Configuration"})
	newP := buildTestPlist(t, map[string]any{"PayloadUUID": "1111", "PayloadIdentifier": "com.example.new"})

	result, err := InjectIdentifiers(newP, existing)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Returns new plist unchanged (no UUIDs in existing to inject).
	if string(result) != string(newP) {
		t.Error("expected unchanged result when existing has no UUIDs")
	}
}

func TestInjectIdentifiers_InvalidNewPlist(t *testing.T) {
	existing := buildTestPlist(t, map[string]any{"PayloadUUID": "AAAA"})

	_, err := InjectIdentifiers([]byte("not a plist"), existing)
	if err == nil {
		t.Fatal("expected error for invalid new plist")
		return
	}
	if !strings.Contains(err.Error(), "parsing new mobileconfig") {
		t.Errorf("error = %q, want to contain 'parsing new mobileconfig'", err.Error())
	}
}

func TestInjectIdentifiers_PreservesContent(t *testing.T) {
	existing := buildTestPlist(t, map[string]any{
		"PayloadUUID":       "SERVER-UUID",
		"PayloadIdentifier": "com.example.server",
	})

	// New profile with PayloadContent array (typical mobileconfig structure).
	newP := buildTestPlist(t, map[string]any{
		"PayloadUUID":        "CLIENT-UUID",
		"PayloadIdentifier":  "com.example.client",
		"PayloadDisplayName": "Wi-Fi Settings",
		"PayloadContent": []any{
			map[string]any{
				"PayloadType": "com.apple.wifi.managed",
				"SSID_STR":    "MyNetwork",
			},
		},
	})

	result, err := InjectIdentifiers(newP, existing)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out map[string]any
	if _, err := plist.Unmarshal(result, &out); err != nil {
		t.Fatalf("result is not valid plist: %v", err)
	}

	if got := out["PayloadUUID"]; got != "SERVER-UUID" {
		t.Errorf("PayloadUUID = %v, want SERVER-UUID", got)
	}
	if got := out["PayloadDisplayName"]; got != "Wi-Fi Settings" {
		t.Errorf("PayloadDisplayName = %v, want Wi-Fi Settings", got)
	}
	content, ok := out["PayloadContent"].([]any)
	if !ok || len(content) == 0 {
		t.Error("PayloadContent was not preserved")
	}
}

func TestExtractProfileIdentifiers(t *testing.T) {
	plistData := buildTestPlist(t, map[string]any{
		"PayloadUUID":       "EXTRACT-UUID",
		"PayloadIdentifier": "com.example.extract",
		"PayloadType":       "Configuration",
	})

	uuid, identifier, err := ExtractProfileIdentifiers(plistData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if uuid != "EXTRACT-UUID" {
		t.Errorf("uuid = %q, want EXTRACT-UUID", uuid)
	}
	if identifier != "com.example.extract" {
		t.Errorf("identifier = %q, want com.example.extract", identifier)
	}
}

func TestExtractProfileIdentifiers_MissingFields(t *testing.T) {
	plistData := buildTestPlist(t, map[string]any{"PayloadType": "Configuration"})

	uuid, identifier, err := ExtractProfileIdentifiers(plistData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if uuid != "" {
		t.Errorf("uuid = %q, want empty string", uuid)
	}
	if identifier != "" {
		t.Errorf("identifier = %q, want empty string", identifier)
	}
}

func TestExtractProfileIdentifiers_InvalidPlist(t *testing.T) {
	_, _, err := ExtractProfileIdentifiers([]byte("not a plist"))
	if err == nil {
		t.Fatal("expected error for invalid plist")
		return
	}
	if !strings.Contains(err.Error(), "parsing mobileconfig") {
		t.Errorf("error = %q, want to contain 'parsing mobileconfig'", err.Error())
	}
}
