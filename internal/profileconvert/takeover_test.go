// Copyright 2026, Jamf Software LLC

package profileconvert

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// takeoverPayload is one payload of a fixture profile.
type takeoverPayload struct {
	typ, id, uuid string
	body          string // extra plist key/value pairs
}

// takeoverProfile builds a mobileconfig. An empty topID/topUUID omits the key.
func takeoverProfile(topID, topUUID string, payloads ...takeoverPayload) []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict><key>PayloadContent</key><array>`)
	for _, p := range payloads {
		b.WriteString("<dict>")
		fmt.Fprintf(&b, "<key>PayloadType</key><string>%s</string>", p.typ)
		if p.id != "" {
			fmt.Fprintf(&b, "<key>PayloadIdentifier</key><string>%s</string>", p.id)
		}
		if p.uuid != "" {
			fmt.Fprintf(&b, "<key>PayloadUUID</key><string>%s</string>", p.uuid)
		}
		b.WriteString("<key>PayloadVersion</key><integer>1</integer>")
		b.WriteString(p.body)
		b.WriteString("</dict>")
	}
	b.WriteString("</array><key>PayloadDisplayName</key><string>Fixture</string>")
	b.WriteString("<key>PayloadType</key><string>Configuration</string>")
	if topID != "" {
		fmt.Fprintf(&b, "<key>PayloadIdentifier</key><string>%s</string>", topID)
	}
	if topUUID != "" {
		fmt.Fprintf(&b, "<key>PayloadUUID</key><string>%s</string>", topUUID)
	}
	b.WriteString("<key>PayloadVersion</key><integer>1</integer></dict></plist>")
	return []byte(b.String())
}

const (
	topUUID    = "AAFA0EDA-1978-4FCB-91E2-5CE3A72367D7"
	finderUUID = "26D7CC73-98B3-4ED1-8235-649E41003E36"
	loginUUID  = "EB93301F-580E-4593-9120-F744ECF27587"
)

var finderPayload = takeoverPayload{
	typ: "com.apple.finder", id: finderUUID, uuid: finderUUID,
	body: `<key>ProhibitBurn</key><false/><key>ProhibitEject</key><true/>`,
}

var loginPayload = takeoverPayload{
	typ: "com.apple.loginwindow", id: loginUUID, uuid: loginUUID,
	body: `<key>RestartDisabledWhileLoggedIn</key><false/><key>LoginwindowText</key><string>hi</string>`,
}

func convertForTakeover(t *testing.T, profile []byte) json.RawMessage {
	t.Helper()
	config, _, err := ConvertMobileconfig(profile, true)
	if err != nil {
		t.Fatalf("ConvertMobileconfig: %v", err)
	}
	return config
}

func decodeContent(t *testing.T, config json.RawMessage) (map[string]any, []map[string]any) {
	t.Helper()
	top, content, err := decodeConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	entries := make([]map[string]any, len(content))
	for i, c := range content {
		entries[i], _ = c.(map[string]any)
	}
	return top, entries
}

func TestApplyTakeoverIdentity_SupportedProfileKeepsItsIdentity(t *testing.T) {
	profile := takeoverProfile(topUUID, topUUID, finderPayload, loginPayload)
	config := convertForTakeover(t, profile)

	got, report, err := ApplyTakeoverIdentity(config, profile, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Supported || len(report.Reasons) != 0 {
		t.Fatalf("want supported, got %+v", report)
	}

	top, entries := decodeContent(t, got)
	if top["payloadIdentifier"] != topUUID || top["payloadUUID"] != topUUID {
		t.Errorf("top-level identity = %v / %v", top["payloadIdentifier"], top["payloadUUID"])
	}
	want := []struct{ typ, id string }{
		{"com.apple.finder", finderUUID},
		{"com.apple.loginwindow", loginUUID},
	}
	if len(entries) != len(want) {
		t.Fatalf("got %d payloads, want %d", len(entries), len(want))
	}
	for i, w := range want {
		if entries[i]["payloadType"] != w.typ {
			t.Errorf("payload %d type = %v, want %s (order must be preserved)", i, entries[i]["payloadType"], w.typ)
		}
		if entries[i]["payloadIdentifier"] != w.id || entries[i]["payloadUUID"] != w.id {
			t.Errorf("payload %d identity = %v / %v, want %s", i, entries[i]["payloadIdentifier"], entries[i]["payloadUUID"], w.id)
		}
	}
	// Stamping identity must not disturb the settings themselves.
	if entries[0]["ProhibitBurn"] != false || entries[0]["ProhibitEject"] != true {
		t.Errorf("finder settings changed: %v", entries[0])
	}
}

func TestApplyTakeoverIdentity_IntegersSurviveStamping(t *testing.T) {
	p := takeoverPayload{
		typ: "com.apple.finder", id: finderUUID, uuid: finderUUID,
		body: `<key>Timeout</key><integer>172800</integer>`,
	}
	profile := takeoverProfile(topUUID, topUUID, p)
	got, report, err := ApplyTakeoverIdentity(convertForTakeover(t, profile), profile, 0)
	if err != nil || !report.Supported {
		t.Fatalf("report %+v err %v", report, err)
	}
	if !strings.Contains(string(got), `"Timeout": 172800`) {
		t.Errorf("integer rewritten:\n%s", got)
	}
}

func TestApplyTakeoverIdentity_Refusals(t *testing.T) {
	cases := []struct {
		name    string
		profile []byte
		native  int
		reason  string
	}{
		{
			name: "payload type the API only takes wrapped as MCX",
			profile: takeoverProfile(topUUID, topUUID, finderPayload,
				takeoverPayload{typ: "com.apple.dashboard", id: "DDDD", uuid: "DDDD", body: `<key>whiteListEnabled</key><false/>`}),
			reason: "payload 2 (com.apple.dashboard) is delivered as com.apple.ManagedClient.preferences",
		},
		{
			name: "payload UUID differs from its identifier",
			profile: takeoverProfile(topUUID, topUUID,
				takeoverPayload{typ: "com.apple.finder", id: "com.example.finder", uuid: finderUUID, body: `<key>ProhibitBurn</key><true/>`}),
			reason: "has a PayloadUUID that differs from its PayloadIdentifier",
		},
		{
			name:    "payload with no identity",
			profile: takeoverProfile(topUUID, topUUID, takeoverPayload{typ: "com.apple.finder", body: `<key>ProhibitBurn</key><true/>`}),
			reason:  "payload 1 (com.apple.finder) has no PayloadIdentifier and PayloadUUID",
		},
		{
			name:    "no top-level identity",
			profile: takeoverProfile("", "", finderPayload),
			reason:  "no top-level PayloadIdentifier and PayloadUUID",
		},
		{
			name:    "native conversion splits the profile",
			profile: takeoverProfile(topUUID, topUUID, finderPayload),
			native:  1,
			reason:  "re-run with --legacy",
		},
		{
			name: "a skipped disabled payload changes the count",
			profile: takeoverProfile(topUUID, topUUID, finderPayload,
				takeoverPayload{typ: "com.apple.vpn.managed", id: "VVVV", uuid: "VVVV", body: `<key>VPNType</key><string>IKEv2</string>`}),
			reason: "the blueprint carries 1 payload(s) but the installed profile has 2",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			config := convertForTakeover(t, tc.profile)
			got, report, err := ApplyTakeoverIdentity(config, tc.profile, tc.native)
			if err != nil {
				t.Fatal(err)
			}
			if report.Supported {
				t.Fatalf("want takeover refused, got supported")
			}
			if !strings.Contains(strings.Join(report.Reasons, "|"), tc.reason) {
				t.Errorf("reasons %q lack %q", report.Reasons, tc.reason)
			}
			// Preserving identity on a profile that cannot be adopted makes the
			// declaration invalid, so the configuration must come back untouched.
			if string(got) != string(config) {
				t.Errorf("configuration was modified though takeover is not supported:\n%s", got)
			}
			if strings.Contains(string(got), "payloadUUID") {
				t.Errorf("identity stamped on an unsupported profile:\n%s", got)
			}
		})
	}
}

func TestApplyTakeoverIdentity_AllReasonsAreReported(t *testing.T) {
	profile := takeoverProfile("", "",
		takeoverPayload{typ: "com.apple.dashboard", id: "A", uuid: "A", body: `<key>whiteListEnabled</key><false/>`},
		takeoverPayload{typ: "com.apple.finder", id: "com.example.finder", uuid: finderUUID, body: `<key>ProhibitBurn</key><true/>`})
	_, report, err := ApplyTakeoverIdentity(convertForTakeover(t, profile), profile, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Reasons) != 3 {
		t.Errorf("want 3 reasons (top-level, dashboard type, finder UUID), got %d: %q", len(report.Reasons), report.Reasons)
	}
}

func TestImportDescription(t *testing.T) {
	supported := ImportDescription("computer", "Finder Takeover", "9696", TakeoverReport{Supported: true})
	if want := `Imported from computer profile "Finder Takeover" (ID 9696) by jamf-cli. Takeover supported.`; supported != want {
		t.Errorf("supported description = %q, want %q", supported, want)
	}

	unsupported := ImportDescription("mobile", "Passcode", "12", TakeoverReport{Reasons: []string{
		"payload 1 (com.apple.MCX) is delivered as com.apple.ManagedClient.preferences",
		"r2", "r3",
	}})
	want := `Imported from mobile device profile "Passcode" (ID 12) by jamf-cli. Takeover not supported: ` +
		`payload 1 (com.apple.MCX) is delivered as com.apple.ManagedClient.preferences (+2 more).`
	if unsupported != want {
		t.Errorf("unsupported description = %q, want %q", unsupported, want)
	}
}

func TestImportDescription_TrimsTheExplanatoryTail(t *testing.T) {
	for reason, want := range map[string]string{
		nativeConversionReason(2): "2 payload type(s) were converted to native DDM components",
		"payload 4 (com.apple.finder) has a PayloadUUID that differs from its PayloadIdentifier, and blueprints force them to match":  "payload 4 (com.apple.finder) has a PayloadUUID that differs from its PayloadIdentifier",
		"the blueprint carries 12 payload(s) but the installed profile has 14 (payloads were skipped, removed as empty or unwrapped)": "the blueprint carries 12 payload(s) but the installed profile has 14",
	} {
		d := ImportDescription("computer", "n", "1", TakeoverReport{Reasons: []string{reason}})
		if !strings.HasSuffix(d, "not supported: "+want+".") {
			t.Errorf("description %q does not end with %q", d, want)
		}
		if len(d) > 220 {
			t.Errorf("description is %d bytes, want a short line: %s", len(d), d)
		}
	}
}

func TestImportDescription_RespectsTheAPILimit(t *testing.T) {
	long := strings.Repeat("x", 1500)
	d := ImportDescription("computer", "n", "1", TakeoverReport{Reasons: []string{long, long, long}})
	if len(d) > maxDescriptionLength {
		t.Errorf("description is %d bytes, API limit is %d", len(d), maxDescriptionLength)
	}
}

func TestDroppedKeys(t *testing.T) {
	sent := json.RawMessage(`{"payloadContent":[
		{"payloadType":"com.apple.finder","payloadIdentifier":"A","InterfaceLevel":"Full","ProhibitBurn":false},
		{"payloadType":"com.apple.loginwindow","payloadIdentifier":"B","LoginwindowText":"hi"}]}`)
	stored := json.RawMessage(`{"payloadContent":[
		{"payloadType":"com.apple.finder","payloadIdentifier":"A","payloadUUID":"A","payloadVersion":1,"ProhibitBurn":false},
		{"payloadType":"com.apple.loginwindow","payloadIdentifier":"B","payloadUUID":"B","LoginwindowText":"hi"}]}`)

	got := DroppedKeys(sent, stored)
	if len(got) != 1 || got[0] != "payload 1 (com.apple.finder): InterfaceLevel" {
		t.Errorf("DroppedKeys = %q", got)
	}
	if extra := DroppedKeys(stored, stored); len(extra) != 0 {
		t.Errorf("identical configs reported drops: %q", extra)
	}
	// A reshaped payload list cannot be compared position by position.
	if mismatch := DroppedKeys(sent, json.RawMessage(`{"payloadContent":[]}`)); mismatch != nil {
		t.Errorf("mismatched lengths should report nothing, got %q", mismatch)
	}
}

func TestConvertVerbatim_KeepsTheShapeOfTheInstalledProfile(t *testing.T) {
	dash := takeoverPayload{typ: "com.apple.dashboard", id: "DDDD", uuid: "DDDD", body: `<key>whiteListEnabled</key><false/>`}
	empty := takeoverPayload{typ: "com.apple.desktop", id: "EEEE", uuid: "EEEE"}
	profile := takeoverProfile(topUUID, topUUID, finderPayload, dash, empty)

	wrapped, err := ConvertToDDMComponents(profile, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	verbatim, err := ConvertToDDMComponentsVerbatim(profile, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, wrappedContent := decodeContent(t, wrapped.ProfileConfig)
	_, verbatimContent := decodeContent(t, verbatim.ProfileConfig)

	if len(verbatimContent) != 3 {
		t.Fatalf("verbatim kept %d payloads, want 3 (empty one included)", len(verbatimContent))
	}
	if len(wrappedContent) != 2 {
		t.Errorf("the fallback drops the empty payload: got %d payloads", len(wrappedContent))
	}
	if verbatimContent[1]["payloadType"] != "com.apple.dashboard" {
		t.Errorf("verbatim changed dashboard's type to %v", verbatimContent[1]["payloadType"])
	}
	if wrappedContent[1]["payloadType"] != "com.apple.ManagedClient.preferences" {
		t.Errorf("the fallback should wrap dashboard, got %v", wrappedContent[1]["payloadType"])
	}

	// Same two behaviours on the legacy path, which has its own copy of the logic.
	legacyVerbatim, _, err := ConvertMobileconfigVerbatim(profile, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := UnlistedPayloadTypes(legacyVerbatim); len(got) != 1 || got[0] != "com.apple.dashboard" {
		t.Errorf("UnlistedPayloadTypes = %v, want [com.apple.dashboard]", got)
	}
	legacyWrapped, _, err := ConvertMobileconfig(profile, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := UnlistedPayloadTypes(legacyWrapped); len(got) != 0 {
		t.Errorf("the fallback carries no unlisted standalone type, got %v", got)
	}
}

func TestImportDescription_ALongReasonIsCutAtAWord(t *testing.T) {
	reason := "payload 1 (com.apple.TCC.configuration-profile-policy) has a PayloadUUID that differs from its PayloadIdentifier and then some"
	d := ImportDescription("computer", "n", "1", TakeoverReport{Reasons: []string{reason}})
	if !strings.HasSuffix(d, "…") || strings.Contains(d, "...") || strings.HasSuffix(d, "….") {
		t.Errorf("want a clean cut ending in an ellipsis, got %q", d)
	}
	if strings.Contains(d, "PayloadIdentifi…") {
		t.Errorf("cut mid-word: %q", d)
	}
}
