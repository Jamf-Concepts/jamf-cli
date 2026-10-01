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
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := &platformVerboseTransport{inner: echoRoundTripper{}, level: 3}
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
