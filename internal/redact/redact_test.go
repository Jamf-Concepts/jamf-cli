// Copyright 2026, Jamf Software LLC

package redact

import (
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
