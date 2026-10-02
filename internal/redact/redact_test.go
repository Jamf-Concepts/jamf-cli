// Copyright 2026, Jamf Software LLC

package redact

import (
	"net/url"
	"strings"
	"testing"
)

func TestBody_RedactsTokenPinPasscodeKeystoreAndAuthorization(t *testing.T) {
	for _, tc := range []struct{ in, secret string }{
		{`{"token":"S1"}`, "S1"},
		{`{"unlockToken":"S2"}`, "S2"},
		{`{"access_token":"S3"}`, "S3"},
		{`{"IDToken":"S4"}`, "S4"},
		{`{"pin":"S5"}`, "S5"},
		{`{"pin":515151}`, "515151"},
		{`{"devicePasscode":"S6"}`, "S6"},
		{`{"gsxKeystore":{"keystoreBytes":"S7"}}`, "S7"},
		{`{"identityKeystore":"S8"}`, "S8"},
		{`{"keystorePassword":"S14"}`, "S14"},
		{`{"Authorization":"Bearer S15"}`, "S15"},
		{`{"authorization_header":"Bearer S9"}`, "S9"},
		{`<account><token>S10</token></account>`, "S10"},
		{`<lock pin_code="x"><pin>S11</pin></lock>`, "S11"},
		{`a=1&token=S12&b=2`, "S12"},
		{`{"items[0].token":"S13"}`, "S13"},
	} {
		got := string(Body([]byte(tc.in)))
		if strings.Contains(got, tc.secret) {
			t.Errorf("Body(%s) = %s, secret survived", tc.in, got)
		}
		if !strings.Contains(got, placeholder) {
			t.Errorf("Body(%s) = %s, want %s", tc.in, got, placeholder)
		}
	}
}

// TestBody_LeavesNamesThatOnlyMentionAToken pins the last-word rule. Each of
// these starts with or contains a credential word without holding one, and a
// log that hides them is one an operator turns off.
func TestBody_LeavesNamesThatOnlyMentionAToken(t *testing.T) {
	for _, in := range []string{
		`{"tokenUrl":"https://idp.example.com/token"}`,
		`{"tokenEndpoint":"https://idp.example.com/oauth/token"}`,
		`{"tokenEndpointAuthMethod":"client_secret_basic"}`,
		`{"bootstrapTokenEscrowedStatus":"ESCROWED"}`,
		`{"token_type":"Bearer"}`,
		`{"pinned":"yes"}`,
		`{"pinned":true}`,
		`{"bootstrapTokenAllowed":true,"passcode":false}`,
		`{"mapping":"x","keychain":"y"}`,
		`{"keystoreFileName":"k.p12","keystoreSetupType":"UPLOADED"}`,
		`{"authorizationEndpoint":"https://idp.example.com/authorize"}`,
		`<token_url>https://idp.example.com</token_url>`,
		`token_type=bearer&tokenUrl=https://x`,
	} {
		if got := string(Body([]byte(in))); got != in {
			t.Errorf("Body(%s) = %s, want it unchanged", in, got)
		}
	}
}

func TestBody_KeepsTheRestOfTheBody(t *testing.T) {
	in := `{"commandData":{"commandType":"DEVICE_LOCK","message":"Call IT","pin":"123456"}}`
	want := `{"commandData":{"commandType":"DEVICE_LOCK","message":"Call IT","pin":"[REDACTED]"}}`
	if got := string(Body([]byte(in))); got != want {
		t.Errorf("Body = %s, want %s", got, want)
	}
}

func TestBody_AQueryStringInsideJSONKeepsTheDocument(t *testing.T) {
	in := `{"url":"https://x.example.com/cb?a=1&token=S1","name":"n"}`
	want := `{"url":"https://x.example.com/cb?a=1&token=[REDACTED]","name":"n"}`
	if got := string(Body([]byte(in))); got != want {
		t.Errorf("Body = %s, want %s", got, want)
	}
}

// redactCase asserts the secrets are gone, the placeholder is present and the
// named non-secret siblings survive, so a redactor that blanks the whole body
// cannot pass.
func redactCase(t *testing.T, in string, secrets, keep []string) {
	t.Helper()
	got := string(Body([]byte(in)))
	for _, s := range secrets {
		if strings.Contains(got, s) {
			t.Errorf("secret %q survived: %s", s, got)
		}
	}
	if !strings.Contains(got, placeholder) {
		t.Errorf("no %s in %s", placeholder, got)
	}
	for _, k := range keep {
		if !strings.Contains(got, k) {
			t.Errorf("non-secret %q was lost: %s", k, got)
		}
	}
}

func TestBody_InstitutionalRecoveryKeyIsRedactedWhole(t *testing.T) {
	t.Run("classic xml", func(t *testing.T) {
		redactCase(t, `<disk_encryption_configuration><name>FV2</name><institutional_recovery_key>
  <key>SENT-irk-key</key><certificate_type>PKCS12</certificate_type><data>SENT-irk-data</data>
</institutional_recovery_key></disk_encryption_configuration>`,
			[]string{"SENT-irk-key", "SENT-irk-data", "PKCS12"}, []string{"<name>FV2</name>", "<key>[REDACTED]</key>", "<data>[REDACTED]</data>"})
	})
	t.Run("json", func(t *testing.T) {
		redactCase(t, `{"name":"FV2","institutional_recovery_key":{"data":"SENT-irk-data","key":"SENT-irk-key","nested":{"x":"SENT-irk-nested"}},"after":"kept"}`,
			[]string{"SENT-irk-key", "SENT-irk-data", "SENT-irk-nested"}, []string{`"name":"FV2"`, `"after":"kept"`})
	})
	t.Run("json flattened path", func(t *testing.T) {
		redactCase(t, `{"disk_encryption_configuration.institutional_recovery_key.key":"SENT-flat","name":"FV2"}`,
			[]string{"SENT-flat"}, []string{`"name":"FV2"`})
	})
}

// TestBody_InventoryRecoveryKeyStatusIsNotASecret pins the text-only form: on
// a computer's inventory the element is a status, not the keystore.
func TestBody_InventoryRecoveryKeyStatusIsNotASecret(t *testing.T) {
	for _, in := range []string{
		`<disk_encryption><institutional_recovery_key>Not Present</institutional_recovery_key></disk_encryption>`,
		`{"institutional_recovery_key":"Not Present"}`,
	} {
		if got := string(Body([]byte(in))); got != in {
			t.Errorf("Body(%s) = %s, want it unchanged", in, got)
		}
	}
}

func TestBody_PlistKeyStringPairs(t *testing.T) {
	t.Run("raw plist", func(t *testing.T) {
		redactCase(t, "<dict><key>SSID_STR</key><string>CorpWiFi</string><key>Password</key>\n\t<string>WifiPSK123</string><key>Challenge</key><string>SCEP-CHALLENGE-1</string></dict>",
			[]string{"WifiPSK123", "SCEP-CHALLENGE-1"}, []string{"<string>CorpWiFi</string>", "<key>Password</key>"})
	})
	t.Run("entity-escaped inside classic payloads", func(t *testing.T) {
		redactCase(t, `<os_x_configuration_profile><general><name>wifi</name><payloads>&lt;dict&gt;&lt;key&gt;SSID_STR&lt;/key&gt;&lt;string&gt;CorpWiFi&lt;/string&gt;&lt;key&gt;Password&lt;/key&gt;&lt;string&gt;Wifi&amp;PSK123&lt;/string&gt;&lt;/dict&gt;</payloads></general></os_x_configuration_profile>`,
			[]string{"PSK123"}, []string{"<name>wifi</name>", "CorpWiFi"})
	})
	t.Run("double-escaped", func(t *testing.T) {
		redactCase(t, `&amp;lt;key&amp;gt;Password&amp;lt;/key&amp;gt;&amp;lt;string&amp;gt;SENT-double&amp;lt;/string&amp;gt;`,
			[]string{"SENT-double"}, nil)
	})
	t.Run("plist inside a json string", func(t *testing.T) {
		redactCase(t, `{"name":"wifi","payload":"<dict>\n\t<key>Password</key>\n\t<string>SENT-json-plist</string>\n</dict>"}`,
			[]string{"SENT-json-plist"}, []string{`"name":"wifi"`})
	})
	t.Run("go-escaped plist inside a json string", func(t *testing.T) {
		redactCase(t, `{"payload":"<key>Password</key><string>SENT-u-plist</string>","name":"wifi"}`,
			[]string{"SENT-u-plist"}, []string{`"name":"wifi"`})
	})
}

func TestBody_StringArraysUnderACredentialName(t *testing.T) {
	redactCase(t, `{"password":["SENT-a","SENT-b"],"keystoreFile":["SENT-p12"],"keystoreFileName":"k.p12","names":["x"]}`,
		[]string{"SENT-a", "SENT-b", "SENT-p12"}, []string{`"keystoreFileName":"k.p12"`, `"names":["x"]`})
	if in := `{"password":[]}`; string(Body([]byte(in))) != in {
		t.Errorf("an empty array was rewritten: %s", Body([]byte(in)))
	}
}

// TestBody_NumbersAreSecretOnlyForAPinOrPasscode keeps a password policy's
// counts readable while a numeric PIN is hidden.
func TestBody_NumbersAreSecretOnlyForAPinOrPasscode(t *testing.T) {
	in := `{"passwordMinLength":8,"passcodeMinimumLength":6,"maxPasswordAge":90,"pin":515151,"devicePasscode":4242}`
	want := `{"passwordMinLength":8,"passcodeMinimumLength":6,"maxPasswordAge":90,"pin":"[REDACTED]","devicePasscode":"[REDACTED]"}`
	if got := string(Body([]byte(in))); got != want {
		t.Errorf("Body = %s, want %s", got, want)
	}
}

func TestURL_RedactsCredentialQueryParameters(t *testing.T) {
	u, err := url.Parse("https://bucket.s3.amazonaws.com/f.pkg?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=AKIA%2Fscope&X-Amz-Security-Token=SENT-sts&X-Amz-Signature=SENT-sig&page=2")
	if err != nil {
		t.Fatal(err)
	}
	got := URL(u)
	for _, s := range []string{"SENT-sts", "SENT-sig", "AKIA"} {
		if strings.Contains(got, s) {
			t.Errorf("%q survived: %s", s, got)
		}
	}
	for _, k := range []string{"X-Amz-Algorithm=AWS4-HMAC-SHA256", "page=2", "https://bucket.s3.amazonaws.com/f.pkg?"} {
		if !strings.Contains(got, k) {
			t.Errorf("%q was lost: %s", k, got)
		}
	}
	if plain := "https://x.example.com/api/v1/computers?page=0&page-size=100"; URL(mustParse(t, plain)) != plain {
		t.Errorf("a URL with no credential parameter changed: %s", URL(mustParse(t, plain)))
	}
}

func mustParse(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestURL_MasksAUserinfoPassword(t *testing.T) {
	for _, raw := range []string{
		"https://svc:SENT-userinfo@x.example.com/api",
		"https://svc:SENT-userinfo@x.example.com/api?page=2",
	} {
		got := URL(mustParse(t, raw))
		if strings.Contains(got, "SENT-userinfo") || !strings.Contains(got, "svc:") {
			t.Errorf("URL(%s) = %s, want the password masked and the user kept", raw, got)
		}
	}
}

func TestBody_PlistPairSeparatedByCharacterReferences(t *testing.T) {
	for _, in := range []string{
		`<key>Password</key>&#13;&#10;<string>SENT-decimal-ref</string><key>SSID_STR</key><string>CorpWiFi</string>`,
		`<key>Password</key>&#xA;&#x9;<string>SENT-hex-ref</string><key>SSID_STR</key><string>CorpWiFi</string>`,
		`&lt;key&gt;Password&lt;/key&gt;&#13;&#10;&lt;string&gt;SENT-escaped-ref&lt;/string&gt;`,
	} {
		redactCase(t, in, []string{"SENT-decimal-ref", "SENT-hex-ref", "SENT-escaped-ref"}, nil)
	}
}

// TestBody_CDATAAndWhitespaceInsideACredentialElement pins the shape a Classic
// body uses for a password holding & or <: the value sits in a CDATA section,
// whose opening < the plain text run cannot cross.
func TestBody_CDATAAndWhitespaceInsideACredentialElement(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"cdata", `<password><![CDATA[x]]></password>`, `<password>[REDACTED]</password>`},
		{"cdata with metacharacters", `<smtp_server><password><![CDATA[p&ss<1]]></password></smtp_server>`,
			`<smtp_server><password>[REDACTED]</password></smtp_server>`},
		{"cdata across lines", "<password>\n  <![CDATA[line1\nline2]]>\n</password>", `<password>[REDACTED]</password>`},
		{"whitespace around plain text", "<password>\n  SENT-ws\n</password >", `<password>[REDACTED]</password>`},
		{"cdata beside a kept sibling", `<smtp_server><name><![CDATA[Mail & Co]]></name><password><![CDATA[SENT-1]]></password></smtp_server>`,
			`<smtp_server><name><![CDATA[Mail & Co]]></name><password>[REDACTED]</password></smtp_server>`},
		{"cdata inside a credential container", `<institutional_recovery_key><key><![CDATA[SENT-irk]]></key><certificate_type>PKCS12</certificate_type></institutional_recovery_key>`,
			`<institutional_recovery_key><key>[REDACTED]</key><certificate_type>[REDACTED]</certificate_type></institutional_recovery_key>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(Body([]byte(tc.in))); got != tc.want {
				t.Errorf("Body(%q)\n got %q\nwant %q", tc.in, got, tc.want)
			}
		})
	}
}
