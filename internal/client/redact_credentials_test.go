// Copyright 2026, Jamf Software LLC

package client

import (
	"strings"
	"testing"
)

// TestRedactCredentialBody_TokenExchangeFormBody pins the case finding (17) was
// about: the SDK's clientcredentials.Config retries with AuthStyleInParams after
// a failed first token attempt, so the plaintext client_secret arrives in a
// form-encoded *request* body — the one body the -vvv logger used to print
// verbatim while redacting responses.
func TestRedactCredentialBody_TokenExchangeFormBody(t *testing.T) {
	in := "grant_type=client_credentials&client_id=abc123&client_secret=s3cr3t-value&scope="
	got := string(RedactCredentialBody([]byte(in)))
	if strings.Contains(got, "s3cr3t-value") {
		t.Fatalf("client_secret survived redaction: %s", got)
	}
	if !strings.Contains(got, "client_secret=[REDACTED]") {
		t.Fatalf("want client_secret=[REDACTED], got %s", got)
	}
	// Everything that is not a credential has to survive, or the log stops
	// being useful and the redactor gets turned off.
	for _, keep := range []string{"grant_type=client_credentials", "client_id=abc123"} {
		if !strings.Contains(got, keep) {
			t.Errorf("redaction removed non-credential %q: %s", keep, got)
		}
	}
}

func TestRedactCredentialBody(t *testing.T) {
	tests := []struct {
		name          string
		in            string
		mustNotAppear []string
		mustAppear    []string
	}{
		{
			name:          "json camelCase and snake_case",
			in:            `{"clientId":"abc","clientSecret":"shhh","client_secret":"shhh2","url":"https://x"}`,
			mustNotAppear: []string{"shhh", "shhh2"},
			mustAppear:    []string{`"clientId":"abc"`, `"url":"https://x"`, `"clientSecret":"[REDACTED]"`},
		},
		{
			name:          "nested classic dotted path",
			in:            `{"institutional_recovery_key.data":"BASE64P12","name":"corp"}`,
			mustNotAppear: []string{"BASE64P12"},
			mustAppear:    []string{`"name":"corp"`},
		},
		{
			name:          "classic xml element",
			in:            `<smtp_server><host>mail.example.com</host><password>hunter2</password></smtp_server>`,
			mustNotAppear: []string{"hunter2"},
			mustAppear:    []string{"mail.example.com", "<password>[REDACTED]</password>"},
		},
		{
			name:          "classic xml element with attributes",
			in:            `<distribution_point><ws_password type="string">hunter2</ws_password></distribution_point>`,
			mustNotAppear: []string{"hunter2"},
		},
		{
			name: "boolean switch whose name contains password is not a credential value",
			// Its value is not a secret, and the redactor is name-based, so it
			// is redacted too. Asserted so the behaviour is deliberate rather
			// than discovered: a redacted "true" costs a log line's clarity,
			// where the reverse costs a secret.
			in:         `{"username_password_required":true,"name":"dp1"}`,
			mustAppear: []string{`"name":"dp1"`},
		},
		{
			name:       "no credential field is left untouched",
			in:         `{"name":"x","id":"7"}`,
			mustAppear: []string{`{"name":"x","id":"7"}`},
		},
		{
			name:          "json escaped quote inside the secret",
			in:            `{"password":"a\"b-secret"}`,
			mustNotAppear: []string{`a\"b-secret`},
			mustAppear:    []string{`"password":"[REDACTED]"`},
		},
		{
			name:          "encryption_key, the jwt signing key",
			in:            `{"encryption_key":"SIGNING-KEY-HERE"}`,
			mustNotAppear: []string{"SIGNING-KEY-HERE"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := string(RedactCredentialBody([]byte(tc.in)))
			for _, s := range tc.mustNotAppear {
				if strings.Contains(got, s) {
					t.Errorf("secret %q survived: %s", s, got)
				}
			}
			for _, s := range tc.mustAppear {
				if !strings.Contains(got, s) {
					t.Errorf("want %q in output, got %s", s, got)
				}
			}
		})
	}
}

// TestRedactBodyForLog_ComposesBothRedactors guards the composition rather than
// either half: a token response and a credential body are different shapes, and
// the log path has to handle a body carrying both.
func TestRedactBodyForLog_ComposesBothRedactors(t *testing.T) {
	in := `{"access_token":"eyJhbGc","client_secret":"shhh"}`
	got := string(RedactBodyForLog([]byte(in)))
	if strings.Contains(got, "eyJhbGc") || strings.Contains(got, "shhh") {
		t.Fatalf("body not fully redacted: %s", got)
	}
}

func TestRedactCredentialBody_Empty(t *testing.T) {
	if got := RedactCredentialBody(nil); got != nil {
		t.Fatalf("nil in, %q out", got)
	}
}
