// Copyright 2026, Jamf Software LLC

package commands

import (
	"path/filepath"
	"strings"
	"testing"
)

func wifiProfilePlist(password string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
	<key>PayloadContent</key>
	<array>
		<dict>
			<key>PayloadType</key><string>com.apple.wifi.managed</string>
			<key>PayloadIdentifier</key><string>com.example.wifi</string>
			<key>SSID_STR</key><string>CorpWiFi</string>
			<key>Password</key><string>` + password + `</string>
		</dict>
	</array>
	<key>PayloadIdentifier</key><string>com.example.profile</string>
	<key>PayloadDisplayName</key><string>Corp Wi-Fi</string>
</dict>
</plist>
`
}

// writeProfileDiffSides writes two backups whose macOS and iOS profiles differ
// only in a Wi-Fi password, plus one iOS profile whose payloads do not decode.
func writeProfileDiffSides(t *testing.T) (src, tgt string) {
	t.Helper()
	src, tgt = t.TempDir(), t.TempDir()
	for _, side := range []struct{ dir, v string }{{src, "old"}, {tgt, "new"}} {
		writeBackupFileForTest(t, filepath.Join(side.dir, "profiles", "macos", "w.yaml"), map[string]any{
			"general": map[string]any{"name": "Wi-Fi mac", "payloads": wifiProfilePlist(diffSecretPrefix + "mac-" + side.v)},
		}, "yaml")
		writeBackupFileForTest(t, filepath.Join(side.dir, "profiles", "ios", "w.yaml"), map[string]any{
			"general": map[string]any{"name": "Wi-Fi ios", "payloads": wifiProfilePlist(diffSecretPrefix + "ios-" + side.v)},
		}, "yaml")
		writeBackupFileForTest(t, filepath.Join(side.dir, "profiles", "ios", "b.yaml"), map[string]any{
			"general": map[string]any{"name": "Broken", "payloads": "not a plist " + diffSecretPrefix + "broken-" + side.v},
		}, "yaml")
	}
	return src, tgt
}

func TestRunDiff_RedactsProfilePayloadSecretsInAnMCPChild(t *testing.T) {
	t.Setenv(mcpChildEnvVar, "1")
	src, tgt := writeProfileDiffSides(t)
	got := runDiffToString(t, src, tgt)
	if strings.Contains(got, diffSecretPrefix) {
		t.Errorf("pro diff prints a profile payload secret over MCP:\n%s", got)
	}
	for _, want := range []string{"Wi-Fi mac", "Wi-Fi ios", "Broken", "SSID_STR", "CorpWiFi", "redacted"} {
		if !strings.Contains(got, want) {
			t.Errorf("pro diff over MCP should still report %q:\n%s", want, got)
		}
	}
}

func TestRunDiff_PrintsProfilePayloadsOutsideMCP(t *testing.T) {
	t.Setenv(mcpChildEnvVar, "")
	src, tgt := writeProfileDiffSides(t)
	got := runDiffToString(t, src, tgt)
	for _, v := range []string{"mac-old", "ios-new", "broken-old"} {
		if !strings.Contains(got, diffSecretPrefix+v) {
			t.Errorf("outside MCP pro diff must print %s unchanged:\n%s", v, got)
		}
	}
}
