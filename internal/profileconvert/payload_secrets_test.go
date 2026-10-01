// Copyright 2026, Jamf Software LLC

package profileconvert

import (
	"strings"
	"testing"

	"howett.net/plist"
)

func decodePlist(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var v map[string]any
	if _, err := plist.Unmarshal(b, &v); err != nil {
		t.Fatalf("redacted output is not a plist: %v\n%s", err, b)
	}
	return v
}

func TestRedactPayloadSecrets_KeepsPayloadContentOutsidePKCS12(t *testing.T) {
	in := `<plist version="1.0"><dict>
<key>PayloadContent</key><array>
<dict><key>PayloadType</key><string>COM.APPLE.SECURITY.PKCS12</string><key>PayloadContent</key><data>c2VjcmV0</data></dict>
<dict><key>PayloadType</key><string>com.apple.security.root</string><key>PayloadContent</key><data>cHVibGlj</data></dict>
<dict><key>PayloadType</key><string>com.apple.wifi.managed</string><key>PASSWORD</key><string>s</string><key>AutoJoin</key><true/></dict>
</array></dict></plist>`
	out, err := RedactPayloadSecrets([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	payloads := decodePlist(t, out)["PayloadContent"].([]any)
	if got := payloads[0].(map[string]any)["PayloadContent"]; got != RedactedPayloadValue {
		t.Errorf("a pkcs12 PayloadContent should be redacted, got %v", got)
	}
	if got, ok := payloads[1].(map[string]any)["PayloadContent"].([]byte); !ok || string(got) != "public" {
		t.Errorf("a root certificate's PayloadContent is not a secret and must stay, got %v", payloads[1])
	}
	wifi := payloads[2].(map[string]any)
	if wifi["PASSWORD"] != RedactedPayloadValue || wifi["AutoJoin"] != true {
		t.Errorf("an upper-case password key should be redacted and a boolean kept, got %v", wifi)
	}
}

func TestRedactPayloadSecrets_ReturnsAProfileWithoutSecretsUnchanged(t *testing.T) {
	in := []byte(`<plist version="1.0"><dict><key>PayloadIdentifier</key><string>x</string><key>RequirePassword</key><true/></dict></plist>`)
	out, err := RedactPayloadSecrets(in)
	if err != nil || string(out) != string(in) {
		t.Errorf("a profile with no secret should come back byte-identical, got %q, %v", out, err)
	}
}

func TestRedactPayloadSecrets_RefusesWhatIsNotAProfile(t *testing.T) {
	for _, in := range []string{"not a plist <string>s</string>", "hello", `<plist version="1.0"><string>Password</string></plist>`} {
		if _, err := RedactPayloadSecrets([]byte(in)); err == nil {
			t.Errorf("RedactPayloadSecrets(%q) should fail so a caller can redact the whole payload", in)
		}
	}
}

func TestRedactClassicProfilePayloads_HandlesCDATAAndFailsClosed(t *testing.T) {
	profile := `<plist version="1.0"><dict><key>Password</key><string>S3CRET</string><key>SSID_STR</key><string>Corp</string></dict></plist>`
	for name, body := range map[string]string{
		"cdata":  `<p><general><payloads><![CDATA[` + profile + `]]></payloads></general></p>`,
		"nested": `<p><general><payloads><x>S3CRET</x></payloads></general></p>`,
		"broken": `<p><general><payloads>S3CRET</payloads></general></p>`,
	} {
		out, err := RedactClassicProfilePayloads([]byte(body))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if strings.Contains(string(out), "S3CRET") || !strings.Contains(string(out), "redacted") {
			t.Errorf("%s: the secret should be gone and the marker present:\n%s", name, out)
		}
		if !strings.HasPrefix(string(out), "<p><general><payloads>") || !strings.HasSuffix(string(out), "</payloads></general></p>") {
			t.Errorf("%s: the document around the payload should be untouched:\n%s", name, out)
		}
	}
	if _, err := RedactClassicProfilePayloads([]byte(`<p><payloads>`)); err == nil {
		t.Error("a truncated body should fail so the caller refuses it")
	}
	keep := `<p><general><payloads></payloads><name>n</name></general></p>`
	if out, _ := RedactClassicProfilePayloads([]byte(keep)); string(out) != keep {
		t.Errorf("an empty payloads element should be left alone, got %s", out)
	}
}

func TestRedactPayloadSecrets_MatchesTokenAndKeySuffixes(t *testing.T) {
	in := `<plist version="1.0"><dict>
<key>PayloadContent</key><array><dict>
<key>PayloadType</key><string>com.apple.ManagedClient.preferences</string>
<key>PayloadIdentifier</key><string>com.example.custom</string>
<key>SSID_STR</key><string>Corp</string>
<key>CloudManagementEnrollmentToken</key><string>S3CRET-cbcm</string>
<key>TailscaleAuthKey</key><string>S3CRET-tskey</string>
<key>APIKey</key><string>S3CRET-api</string>
<key>PrivateKey</key><data>UzNDUkVULXBr</data>
<key>AWSAccessKey</key><string>S3CRET-aws</string>
<key>SecretKey</key><string>S3CRET-sk</string>
<key>DevicePasscode</key><string>S3CRET-passcode</string>
<key>TokenURL</key><string>https://idp.example.com/token-url</string>
<key>TokenEndpoint</key><string>https://idp.example.com/token-endpoint</string>
<key>PIN</key><string>visible-pin</string>
<key>KeyID</key><string>visible-key-id</string>
</dict></array></dict></plist>`
	out, err := RedactPayloadSecrets([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	p := decodePlist(t, out)["PayloadContent"].([]any)[0].(map[string]any)
	for _, k := range []string{"CloudManagementEnrollmentToken", "TailscaleAuthKey", "APIKey", "PrivateKey", "AWSAccessKey", "SecretKey", "DevicePasscode"} {
		if p[k] != RedactedPayloadValue {
			t.Errorf("%s should be redacted, got %v", k, p[k])
		}
	}
	for k, want := range map[string]string{
		"TokenURL": "https://idp.example.com/token-url", "TokenEndpoint": "https://idp.example.com/token-endpoint",
		"PIN": "visible-pin", "KeyID": "visible-key-id", "SSID_STR": "Corp", "PayloadIdentifier": "com.example.custom",
	} {
		if p[k] != want {
			t.Errorf("%s does not end in a secret suffix and must stay %q, got %v", k, want, p[k])
		}
	}
}
