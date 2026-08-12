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

// diffPayloadPaths is DiffPayloadValuesDetailed narrowed to just the paths,
// for the tests below that only care which keys diverged.
func diffPayloadPaths(intended, stored []byte) ([]string, error) {
	diffs, err := DiffPayloadValuesDetailed(intended, stored)
	if err != nil {
		return nil, err
	}
	paths := make([]string, len(diffs))
	for i, d := range diffs {
		paths[i] = d.Path
	}
	return paths, nil
}

func TestDiffPayloadValues_Identical(t *testing.T) {
	a := plistDoc(`<key>k</key><string>R&amp;D</string>`)
	diffs, err := diffPayloadPaths(a, a)
	if err != nil || len(diffs) != 0 {
		t.Fatalf("want no diffs, got %v (err %v)", diffs, err)
	}
}

func TestDiffPayloadValues_CorruptedValue(t *testing.T) {
	intended := plistDoc(`<key>LoginwindowText</key><string>Here is an &amp;</string>`)
	stored := plistDoc(`<key>LoginwindowText</key><string>Here is an &amp;amp;</string>`)
	diffs, err := diffPayloadPaths(intended, stored)
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
	diffs, err := diffPayloadPaths(intended, stored)
	if err != nil || len(diffs) != 0 {
		t.Fatalf("meta rewrites must be masked, got %v (err %v)", diffs, err)
	}
}

func TestDiffPayloadValues_EdgeWhitespaceTrimIgnored(t *testing.T) {
	intended := plistDoc(`<key>k</key><string> padded </string>`)
	stored := plistDoc(`<key>k</key><string>padded</string>`)
	diffs, err := diffPayloadPaths(intended, stored)
	if err != nil || len(diffs) != 0 {
		t.Fatalf("server edge-trim must not diff, got %v (err %v)", diffs, err)
	}
}

func TestDiffPayloadValues_NestedArrayAndDict(t *testing.T) {
	intended := plistDoc(`<key>a</key><array><dict><key>x</key><string>1</string></dict></array>`)
	stored := plistDoc(`<key>a</key><array><dict><key>x</key><string>2</string></dict></array>`)
	diffs, err := diffPayloadPaths(intended, stored)
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
	diffs, err := diffPayloadPaths(intended, stored)
	if err != nil || len(diffs) != 0 {
		t.Fatalf("missing empty containers must not diff, got %v (err %v)", diffs, err)
	}
}

func TestDiffPayloadValues_CarriageReturnStoredAsLineFeed(t *testing.T) {
	// "&#13;" is the only line break Jamf Pro stores, and it always reads back
	// as LF (MCX/mobile fragments normalise on store; a verbatim CR byte is
	// normalised by our own parse). That must not read as an unfaithful store.
	intended := plistDoc(`<key>ConsentText</key><string>Welcome to&#13;&#13;ACME</string>`)
	stored := plistDoc(`<key>ConsentText</key><string>Welcome to&#10;&#10;ACME</string>`)
	diffs, err := diffPayloadPaths(intended, stored)
	if err != nil || len(diffs) != 0 {
		t.Fatalf("CR/LF difference must be tolerated, got %v (err %v)", diffs, err)
	}
}

func TestDiffPayloadValues_LineSeparatorsStillCompared(t *testing.T) {
	// U+2028 round-trips byte-exact, so losing it is real corruption.
	intended := plistDoc(`<key>k</key><string>a&#8232;b</string>`)
	stored := plistDoc(`<key>k</key><string>ab</string>`)
	diffs, err := diffPayloadPaths(intended, stored)
	if err != nil || len(diffs) != 1 {
		t.Fatalf("want 1 diff for a dropped U+2028, got %v (err %v)", diffs, err)
	}
}

func TestDiffPayloadValuesDetailed_Classification(t *testing.T) {
	tests := []struct {
		name           string
		intended       string
		stored         string
		wantPath       string
		reasonContains string
	}{
		{
			name:           "line breaks deleted",
			intended:       `<key>ConsentText</key><string>EITHER&#10;&#9;EXPRESSED</string>`,
			stored:         `<key>ConsentText</key><string>EITHEREXPRESSED</string>`,
			wantPath:       "ConsentText",
			reasonContains: "&#13;",
		},
		{
			name:           "extra entity layer",
			intended:       `<key>LoginwindowText</key><string>Here is an &amp;</string>`,
			stored:         `<key>LoginwindowText</key><string>Here is an &amp;amp;</string>`,
			wantPath:       "LoginwindowText",
			reasonContains: "PI-827",
		},
		{
			name:           "non-BMP replaced",
			intended:       `<key>k</key><string>hi &#128512;</string>`,
			stored:         `<key>k</key><string>hi ` + "��" + `</string>`,
			wantPath:       "k",
			reasonContains: "non-BMP",
		},
		{
			name:           "value absent",
			intended:       `<key>k</key><string>v</string>`,
			stored:         `<key>other</key><string>v</string>`,
			wantPath:       "k",
			reasonContains: "not stored at all",
		},
		{
			name:           "unexplained",
			intended:       `<key>k</key><string>alpha</string>`,
			stored:         `<key>k</key><string>beta</string>`,
			wantPath:       "k",
			reasonContains: "value differs",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diffs, err := DiffPayloadValuesDetailed(plistDoc(tt.intended), plistDoc(tt.stored))
			if err != nil {
				t.Fatal(err)
			}
			if len(diffs) != 1 {
				t.Fatalf("want 1 diff, got %v", diffs)
			}
			if diffs[0].Path != tt.wantPath {
				t.Errorf("path = %q, want %q", diffs[0].Path, tt.wantPath)
			}
			if !strings.Contains(diffs[0].Reason, tt.reasonContains) {
				t.Errorf("reason %q does not mention %q", diffs[0].Reason, tt.reasonContains)
			}
		})
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
