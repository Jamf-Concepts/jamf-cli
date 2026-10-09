// Copyright 2026, Jamf Software LLC

package commands

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type echoRoundTripper struct{}

func (echoRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	b, _ := io.ReadAll(req.Body)
	rec := httptest.NewRecorder()
	rec.WriteHeader(http.StatusOK)
	_, _ = rec.Write(b)
	return rec.Result(), nil
}

// TestPlatformVerboseTransportRedactsEverySecretField covers the Platform SDK
// path, which also serves the gateway-routed Security Cloud commands.
func TestPlatformVerboseTransportRedactsEverySecretField(t *testing.T) {
	cases := []struct {
		name, body, secret string
	}{
		{"control/form client_secret", `grant_type=client_credentials&client_secret=SENT-form-secret`, "SENT-form-secret"},
		{"control/json secretValue", `{"secretName":"n","secretValue":"SENT-secretValue"}`, "SENT-secretValue"},

		{"platform ddm serverToken", `{"serverToken":"SENT-serverToken"}`, "SENT-serverToken"},
		{"platform mdm action pin", `{"commandType":"DEVICE_LOCK","pin":"SENT-pin-123456"}`, "SENT-pin-123456"},
		{"platform blueprint token", `{"token":"SENT-blueprint-token"}`, "SENT-blueprint-token"},
		{"security gateway token", `{"token":"SENT-security-token"}`, "SENT-security-token"},
		{"oauth accessToken", `{"accessToken":"SENT-accessToken"}`, "SENT-accessToken"},

		// Protect, School and Security Cloud share the transport.
		{"protect token exchange", `{"client_id":"abc","password":"SENT-protect-secret"}`, "SENT-protect-secret"},
		{"protect token response", `{"access_token":"SENT-protect-at","token_type":"Bearer"}`, "SENT-protect-at"},
		{"school user password", `{"username":"u","password":"SENT-school-password"}`, "SENT-school-password"},
		{"security login token", `{"token":"SENT-jwt"}`, "SENT-jwt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := &verboseTransport{inner: echoRoundTripper{}, level: 3}
			out := captureStderr(t, func() {
				req, err := http.NewRequest(http.MethodPost, "https://us.api.jamfcloud.com/x", strings.NewReader(tc.body))
				if err != nil {
					t.Errorf("request: %v", err)
					return
				}
				resp, err := tr.RoundTrip(req)
				if err != nil {
					t.Errorf("RoundTrip: %v", err)
					return
				}
				_ = resp.Body.Close()
			})
			if n := strings.Count(out, tc.secret); n > 0 {
				t.Errorf("LEAK %q: secret appeared %d time(s) in -vvv stderr (request+response)", tc.name, n)
			}
			if n := strings.Count(out, "[REDACTED]"); n < 2 {
				t.Errorf("%q: want [REDACTED] in both the request and response logs, found %d", tc.name, n)
			}
		})
	}
}

// The three non-Pro products authenticate in a header, not a body: Protect sends
// the raw access token as Authorization, School and Security Cloud send Basic.
// -vv prints headers, so each has to come out redacted, with the session cookie.
func TestVerboseTransportRedactsCredentialHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "APBALANCEID", Value: "SENT-set-cookie"})
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	for _, auth := range []string{"SENT-raw-protect-token", "Basic U0VOVC1iYXNpYw==", "Bearer SENT-bearer"} {
		tr := &verboseTransport{inner: http.DefaultTransport, level: 2}
		out := captureStderr(t, func() {
			req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
			if err != nil {
				t.Error(err)
				return
			}
			req.Header.Set("Authorization", auth)
			req.AddCookie(&http.Cookie{Name: "s", Value: "SENT-cookie"})
			resp, err := tr.RoundTrip(req)
			if err != nil {
				t.Error(err)
				return
			}
			_ = resp.Body.Close()
		})
		for _, secret := range []string{"SENT-raw-protect-token", "U0VOVC1iYXNpYw", "SENT-bearer", "SENT-cookie", "SENT-set-cookie"} {
			if strings.Contains(out, secret) {
				t.Errorf("LEAK with Authorization %q: %q appeared in -vv stderr:\n%s", auth, secret, out)
			}
		}
	}
}

func TestVerboseRoundTripper(t *testing.T) {
	orig := verboseLevel
	defer func() { verboseLevel = orig }()

	verboseLevel = 0
	if got := verboseRoundTripper(http.DefaultTransport, false); got != http.DefaultTransport {
		t.Errorf("at -v off the transport must be returned unwrapped, got %T", got)
	}
	verboseLevel = 2
	vt, ok := verboseRoundTripper(nil, true).(*verboseTransport)
	if !ok || vt.inner != http.DefaultTransport || vt.level != 2 || !vt.plainRetries {
		t.Errorf("nil transport must wrap http.DefaultTransport at the asked level: %+v", vt)
	}
}

// Protect is GraphQL: every call is a POST to one URL, so a different query after
// a failed one is not its retry.
func TestVerboseTransportPlainRetriesNeverLabels(t *testing.T) {
	tr := &verboseTransport{inner: &stubRoundTripper{status: 502}, level: 1, plainRetries: true}
	out := captureStderr(t, func() {
		for range 3 {
			mustRoundTrip(t, tr, http.MethodPost, "https://tenant.jamfcloud.com/graphql")
		}
	})
	if strings.Count(out, "--> POST") != 3 || strings.Contains(out, "retry") {
		t.Errorf("want three unlabelled requests, got:\n%s", out)
	}
}
