// Copyright 2026, Jamf Software LLC

package client

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/auth"
)

// verboseSecretBodies are request/response body shapes from specs/ that carry
// a credential. Each value is a unique sentinel so a leak names its own row.
var verboseSecretBodies = []struct {
	name, body, secret string
}{
	{"control/json clientSecret", `{"clientId":"x","clientSecret":"SENT-clientSecret"}`, "SENT-clientSecret"},
	{"control/json access_token", `{"access_token":"SENT-access_token","expires_in":60}`, "SENT-access_token"},
	{"control/xml password", `<smtp_server><password>SENT-xml-password</password></smtp_server>`, "SENT-xml-password"},
	{"control/form client_secret", `grant_type=client_credentials&client_secret=SENT-form-secret`, "SENT-form-secret"},

	{"pro gsx token", `{"enabled":true,"token":"SENT-gsx-token"}`, "SENT-gsx-token"},
	{"pro gsx keystoreBytes nested", `{"gsxKeystore":{"name":"k.p12","keystoreBytes":"SENT-keystoreBytes"}}`, "SENT-keystoreBytes"},
	{"pro auth token response", `{"token":"SENT-auth-token","expires":"2026-09-30T00:00:00Z"}`, "SENT-auth-token"},
	{"pro dep encodedToken", `{"encodedToken":"SENT-encodedToken"}`, "SENT-encodedToken"},
	{"pro mdm pin", `{"commandData":{"commandType":"DEVICE_LOCK","pin":"SENT-pin-123456"}}`, "SENT-pin-123456"},
	{"pro mdm unlockToken", `{"commandData":{"commandType":"CLEAR_PASSCODE","unlockToken":"SENT-unlockToken"}}`, "SENT-unlockToken"},
	{"pro venafi refreshToken", `{"name":"v","refreshToken":"SENT-refreshToken"}`, "SENT-refreshToken"},
	{"pro csa sessionToken", `{"sessionToken":"SENT-sessionToken"}`, "SENT-sessionToken"},
	{"pro inventory bootstrapToken", `{"returnToService":{"bootstrapToken":"SENT-bootstrapToken"}}`, "SENT-bootstrapToken"},
	{"pro teamviewer scriptToken", `{"scriptToken":"SENT-scriptToken"}`, "SENT-scriptToken"},
	{"pro appleCareToken", `{"appleCareToken":"SENT-appleCareToken"}`, "SENT-appleCareToken"},
	{"pro oauth accessToken", `{"accessToken":"SENT-accessToken"}`, "SENT-accessToken"},
	{"pro oauth idToken", `{"idToken":"SENT-idToken"}`, "SENT-idToken"},
	{"pro identityKeystore", `{"identityKeystore":"SENT-identityKeystore"}`, "SENT-identityKeystore"},
	{"platform ddm serverToken", `{"serverToken":"SENT-serverToken"}`, "SENT-serverToken"},
	{"xml token element", `<account><token>SENT-xml-token</token></account>`, "SENT-xml-token"},
	{"form token param", `token=SENT-form-token&other=1`, "SENT-form-token"},
}

func captureStderrConcurrently(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	_ = w.Close()
	os.Stderr = orig
	return <-done
}

// TestVerboseBodyLogRedactsEverySecretField drives Client.Do at -vvv against a
// server that echoes the request body, so both the request and response body
// logs are checked.
func TestVerboseBodyLogRedactsEverySecretField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(b)
	}))
	defer srv.Close()

	for _, tc := range verboseSecretBodies {
		t.Run(tc.name, func(t *testing.T) {
			c := New(srv.URL, auth.NewTokenProvider("test-token"), WithVerbose(3))
			out := captureStderrConcurrently(t, func() {
				resp, err := c.Do(context.Background(), http.MethodPost, "/v1/x", strings.NewReader(tc.body))
				if err != nil {
					t.Errorf("Do: %v", err)
					return
				}
				_, _ = io.ReadAll(resp.Body)
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
