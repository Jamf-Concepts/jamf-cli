// Copyright 2026, Jamf Software LLC

package profileconvert

import (
	"os"
	"strings"
	"testing"
)

func plistDoc(inner string) []byte {
	return []byte(`<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict>` + inner + `</dict></plist>`)
}

func TestDiffPayloadValues_Identical(t *testing.T) {
	a := plistDoc(`<key>k</key><string>R&amp;D</string>`)
	diffs, err := DiffPayloadValues(a, a)
	if err != nil || len(diffs) != 0 {
		t.Fatalf("want no diffs, got %v (err %v)", diffs, err)
	}
}

func TestDiffPayloadValues_CorruptedValue(t *testing.T) {
	intended := plistDoc(`<key>LoginwindowText</key><string>Here is an &amp;</string>`)
	stored := plistDoc(`<key>LoginwindowText</key><string>Here is an &amp;amp;</string>`)
	diffs, err := DiffPayloadValues(intended, stored)
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs) != 1 || diffs[0] != "LoginwindowText" {
		t.Fatalf("want [LoginwindowText], got %v", diffs)
	}
}

func TestDiffPayloadValues_MasksServerRewrittenMeta(t *testing.T) {
	intended := plistDoc(`<key>PayloadUUID</key><string>AAA</string><key>PayloadDisplayName</key><string>mine</string><key>k</key><string>v</string>`)
	stored := plistDoc(`<key>PayloadUUID</key><string>BBB</string><key>PayloadDisplayName</key><string>Custom Settings</string><key>PayloadOrganization</key><string>Org</string><key>k</key><string>v</string>`)
	diffs, err := DiffPayloadValues(intended, stored)
	if err != nil || len(diffs) != 0 {
		t.Fatalf("meta rewrites must be masked, got %v (err %v)", diffs, err)
	}
}

func TestDiffPayloadValues_EdgeWhitespaceTrimIgnored(t *testing.T) {
	intended := plistDoc(`<key>k</key><string> padded </string>`)
	stored := plistDoc(`<key>k</key><string>padded</string>`)
	diffs, err := DiffPayloadValues(intended, stored)
	if err != nil || len(diffs) != 0 {
		t.Fatalf("server edge-trim must not diff, got %v (err %v)", diffs, err)
	}
}

func TestDiffPayloadValues_NestedArrayAndDict(t *testing.T) {
	intended := plistDoc(`<key>a</key><array><dict><key>x</key><string>1</string></dict></array>`)
	stored := plistDoc(`<key>a</key><array><dict><key>x</key><string>2</string></dict></array>`)
	diffs, err := DiffPayloadValues(intended, stored)
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs) != 1 || !strings.Contains(diffs[0], "a[0].x") {
		t.Fatalf("want nested path diff, got %v", diffs)
	}
}

func TestDiffPayloadValues_MissingEmptyContainerIgnored(t *testing.T) {
	intended := plistDoc(`<key>AllowList</key><array/><key>k</key><string>v</string>`)
	stored := plistDoc(`<key>k</key><string>v</string>`)
	diffs, err := DiffPayloadValues(intended, stored)
	if err != nil || len(diffs) != 0 {
		t.Fatalf("missing empty containers must not diff, got %v (err %v)", diffs, err)
	}
}

func TestIsSignedProfile(t *testing.T) {
	if IsSignedProfile([]byte(`<?xml version="1.0"?>`)) {
		t.Fatal("XML misdetected as signed")
	}
	if IsSignedProfile([]byte("bplist00xyz")) {
		t.Fatal("binary plist misdetected as signed")
	}
	if !IsSignedProfile([]byte{0x30, 0x80, 0x06, 0x09}) {
		t.Fatal("BER SignedData not detected")
	}
	if !IsSignedProfile([]byte{0x30, 0x82, 0x0a, 0x00}) {
		t.Fatal("DER SignedData not detected")
	}
}

// TestExtractSignedProfile_RealFile exercises CMS extraction against a real
// signed mobileconfig when one is available locally; skipped otherwise.
func TestExtractSignedProfile_RealFile(t *testing.T) {
	path := os.Getenv("JAMF_CLI_TEST_SIGNED_PROFILE")
	if path == "" {
		t.Skip("set JAMF_CLI_TEST_SIGNED_PROFILE to a signed .mobileconfig to run")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	inner, err := ExtractSignedProfile(data)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(inner), "<plist") {
		t.Fatalf("extracted content is not a plist: %.80s", inner)
	}
}
