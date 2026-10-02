// Copyright 2026, Jamf Software LLC

package generated

import (
	"encoding/base64"
	"encoding/xml"
	"strings"
	"testing"
)

// payloadSecretPlist is a configuration profile carrying one secret of each
// kind a Wi-Fi, VPN, SCEP or identity payload holds. Every secret begins with
// classicSecretPrefix; a data secret is base64 on the wire, so it is checked
// in that form too.
var payloadSecretPlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>PayloadContent</key>
	<array>
		<dict>
			<key>PayloadType</key><string>com.apple.wifi.managed</string>
			<key>PayloadIdentifier</key><string>com.example.wifi</string>
			<key>SSID_STR</key><string>CorpWiFi</string>
			<key>Password</key><string>` + classicSecretPrefix + `wifi</string>
			<key>PayloadCertificatePassword</key><string>` + classicSecretPrefix + `certpass</string>
			<key>EAPClientConfiguration</key>
			<dict>
				<key>UserName</key><string>wifi-user</string>
				<key>UserPassword</key><string>` + classicSecretPrefix + `eap</string>
			</dict>
		</dict>
		<dict>
			<key>PayloadType</key><string>com.apple.vpn.managed</string>
			<key>PayloadIdentifier</key><string>com.example.vpn</string>
			<key>IPSec</key>
			<dict>
				<key>SharedSecret</key><data>` + payloadDataSecret("shared") + `</data>
				<key>XAuthPassword</key><string>` + classicSecretPrefix + `xauth</string>
			</dict>
		</dict>
		<dict>
			<key>PayloadType</key><string>com.apple.security.scep</string>
			<key>PayloadIdentifier</key><string>com.example.scep</string>
			<key>PayloadContent</key>
			<dict>
				<key>URL</key><string>https://scep.example.com</string>
				<key>Challenge</key><string>` + classicSecretPrefix + `challenge</string>
			</dict>
		</dict>
		<dict>
			<key>PayloadType</key><string>com.apple.security.pkcs12</string>
			<key>PayloadIdentifier</key><string>com.example.identity</string>
			<key>PayloadCertificateFileName</key><string>identity.p12</string>
			<key>Password</key><string>` + classicSecretPrefix + `p12pass</string>
			<key>PayloadContent</key><data>` + payloadDataSecret("p12") + `</data>
		</dict>
		<dict>
			<key>PayloadType</key><string>com.example.custom</string>
			<key>PayloadIdentifier</key><string>com.example.custom</string>
			<key>adminpassword</key><string>` + classicSecretPrefix + `lowercase</string>
		</dict>
	</array>
	<key>PayloadIdentifier</key><string>com.example.profile</string>
	<key>PayloadType</key><string>Configuration</string>
</dict>
</plist>
`

func payloadDataSecret(name string) string {
	return base64.StdEncoding.EncodeToString([]byte(classicSecretPrefix + name))
}

var payloadSecretSpellings = []string{classicSecretPrefix, payloadDataSecret("shared"), payloadDataSecret("p12")}

var payloadVisibleKeys = []string{"SSID_STR", "CorpWiFi", "com.example.wifi", "wifi-user", "https://scep.example.com", "identity.p12", "com.example.profile"}

// classicProfileBody is a Classic GET answer for one profile, with payloads as
// the escaped text the server sends.
func classicProfileBody(root, payloads string) string {
	var esc strings.Builder
	_ = xml.EscapeText(&esc, []byte(payloads))
	return `<?xml version="1.0" encoding="UTF-8"?><` + root + `><general><id>1</id><name>Corp</name><payloads>` + esc.String() + `</payloads></general><scope><all_computers>false</all_computers></scope></` + root + `>`
}

var classicProfileResources = []struct{ cli, root string }{
	{"classic-macos-config-profiles", "os_x_configuration_profile"},
	{"classic-mobile-config-profiles", "configuration_profile"},
}

var everyReadFormat = []string{"", "json", "yaml", "raw", "xml", "table", "plain", "ndjson"}

func TestClassicProfileGet_RedactsPayloadSecretsInAnMCPChild(t *testing.T) {
	t.Setenv(mcpChildEnv, "1")
	for _, r := range classicProfileResources {
		body := classicProfileBody(r.root, payloadSecretPlist)
		for _, format := range everyReadFormat {
			got := runClassicRead(t, body, format, r.cli, "get", "1")
			for _, s := range payloadSecretSpellings {
				if strings.Contains(got, s) {
					t.Errorf("%s get -o %q prints a payload secret (%s) over MCP:\n%s", r.cli, format, s, got)
				}
			}
			if !strings.Contains(got, "redacted") {
				t.Errorf("%s get -o %q should print the redaction marker in place of each payload secret:\n%s", r.cli, format, got)
			}
			if format == "table" || format == "plain" {
				continue
			}
			for _, k := range payloadVisibleKeys {
				if !strings.Contains(got, k) {
					t.Errorf("%s get -o %q dropped %q, which is not a secret:\n%s", r.cli, format, k, got)
				}
			}
		}
	}
}

func TestClassicProfileGet_UndecodablePayloadsFailClosedInAnMCPChild(t *testing.T) {
	t.Setenv(mcpChildEnv, "1")
	for _, r := range classicProfileResources {
		body := classicProfileBody(r.root, "not a plist <key>Password</key><string>"+classicSecretPrefix+"broken</string>")
		for _, format := range []string{"json", "raw"} {
			got := runClassicRead(t, body, format, r.cli, "get", "1")
			if strings.Contains(got, classicSecretPrefix) {
				t.Errorf("%s get -o %s prints an undecodable payload over MCP:\n%s", r.cli, format, got)
			}
			if !strings.Contains(got, "redacted") || !strings.Contains(got, "Corp") {
				t.Errorf("%s get -o %s should keep the record and print the marker for its payloads:\n%s", r.cli, format, got)
			}
		}
	}
}

func TestClassicProfileGet_OutsideMCPPrintsPayloadsUnchanged(t *testing.T) {
	t.Setenv(mcpChildEnv, "")
	for _, r := range classicProfileResources {
		body := classicProfileBody(r.root, payloadSecretPlist)
		if got := runClassicRead(t, body, "raw", r.cli, "get", "1"); strings.TrimSpace(got) != strings.TrimSpace(body) {
			t.Errorf("%s get -o raw outside MCP must print the wire bytes unchanged:\n got %s\nwant %s", r.cli, got, body)
		}
		got := runClassicRead(t, body, "json", r.cli, "get", "1")
		if !strings.Contains(got, classicSecretPrefix+"wifi") || strings.Contains(got, "redacted") {
			t.Errorf("%s get -o json outside MCP must print the payload unchanged:\n%s", r.cli, got)
		}
	}
}
