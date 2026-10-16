// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/oauth2"

	"github.com/Jamf-Concepts/jamf-cli/internal/auth"
	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
)

func TestVerifyProfileCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parsing form: %v", err)
		}
		if r.PostForm.Get("client_secret") != "right" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 300})
	}))
	defer server.Close()

	cases := []struct {
		name       string
		authMethod string
		clientID   string
		secret     string
		noVerify   bool
		wantErr    bool
		wantOut    string
		wantSilent bool
	}{
		{name: "oauth2 good pair", authMethod: "oauth2", clientID: "cid", secret: "right", wantOut: "Verifying credentials"},
		{name: "oauth2 bad pair", authMethod: "oauth2", clientID: "cid", secret: "wrong", wantErr: true, wantOut: "Verifying credentials"},
		{name: "platform good pair", authMethod: "platform", clientID: "cid", secret: "right", wantOut: "Verifying credentials"},
		// A bearer token cannot be exchanged, so there is nothing to check.
		{name: "token auth is not verified", authMethod: "token", wantSilent: true},
		// config.ResolveSecret owns these, and a reference is routinely written
		// on a machine that cannot reach the server it names.
		{name: "env reference", authMethod: "oauth2", clientID: "env:CID", secret: "env:SECRET", wantOut: "Skipping verification"},
		{name: "file reference", authMethod: "oauth2", clientID: "file:/tmp/cid", secret: "file:/tmp/sec", wantOut: "Skipping verification"},
		{name: "no-verify skips a bad pair", authMethod: "oauth2", clientID: "cid", secret: "wrong", noVerify: true, wantOut: "--no-verify"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			err := verifyProfileCredentials(context.Background(), &out, tc.authMethod, server.URL, tc.clientID, tc.secret, "", "env-1", tc.noVerify)
			if tc.wantErr && err == nil {
				t.Fatalf("expected an error; output:\n%s", out.String())
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantOut != "" && !strings.Contains(out.String(), tc.wantOut) {
				t.Errorf("output = %q, want it to contain %q", out.String(), tc.wantOut)
			}
			if tc.wantSilent && out.String() != "" {
				t.Errorf("expected no output, got %q", out.String())
			}
		})
	}
}

// A failed verification must say that nothing was written and how to override
// it, or the operator is left unsure whether a half-saved profile exists.
func TestVerifyProfileCredentials_FailureNamesTheOverride(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	var out bytes.Buffer
	err := verifyProfileCredentials(context.Background(), &out, "oauth2", server.URL, "cid", "wrong", "", "", false)
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"did not write the profile", "--no-verify"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to contain %q", err.Error(), want)
		}
	}
}

// A platform credential the gateway refuses gets the same remedy in add-profile
// as in platform setup; an instance credential does not, because the causes it
// names (environment ID as client ID, region, beta revocation) are gateway
// mistakes and would mislead a Jamf Pro API client user.
func TestVerifyProfileCredentials_InvalidClientHintIsGatewayOnly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	for _, tc := range []struct {
		authMethod string
		wantHint   bool
	}{
		{"platform", true},
		{"oauth2", false},
	} {
		t.Run(tc.authMethod, func(t *testing.T) {
			var out bytes.Buffer
			err := verifyProfileCredentials(context.Background(), &out, tc.authMethod, server.URL, "cid", "wrong", "", "env-1", false)
			if err == nil {
				t.Fatal("expected an error")
			}
			var ee *exitcode.Error
			gotHint := errors.As(err, &ee) && ee.Hint == invalidPlatformClientHint
			if gotHint != tc.wantHint {
				t.Errorf("invalid-client hint present = %v, want %v (err %v)", gotHint, tc.wantHint, err)
			}
			// The hint is added beside the message, never in place of it.
			if !strings.Contains(err.Error(), "did not write the profile") {
				t.Errorf("error lost its message: %q", err.Error())
			}
		})
	}
}

func TestIsInvalidClient(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"jamf-cli exchange", fmt.Errorf("platform token exchange failed: %w", auth.ErrInvalidClientCredentials), true},
		{"sdk invalid_client body", fmt.Errorf("wrapped: %w", &oauth2.RetrieveError{ErrorCode: "invalid_client"}), true},
		{"sdk bare 401", &oauth2.RetrieveError{Response: &http.Response{StatusCode: http.StatusUnauthorized}}, true},
		{"sdk 500", &oauth2.RetrieveError{Response: &http.Response{StatusCode: http.StatusInternalServerError}}, false},
		{"network", errors.New("dial tcp: connection refused"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isInvalidClient(tc.err); got != tc.want {
				t.Errorf("isInvalidClient = %v, want %v", got, tc.want)
			}
		})
	}
}

// The lead-in printed ahead of "Client ID:" names the issuer the caller's
// auth method implies (#438). The gateway one has to rule out the two IDs an
// operator is most likely holding instead.
func TestClientCredentialSource(t *testing.T) {
	gw := clientCredentialSource("platform")
	for _, want := range []string{"Platform API integration", "Jamf Account", "environment ID", "tenant ID"} {
		if !strings.Contains(gw, want) {
			t.Errorf("platform lead-in %q does not mention %q", gw, want)
		}
	}
	if got := clientCredentialSource("oauth2"); !strings.Contains(got, "API roles and clients") {
		t.Errorf("oauth2 lead-in %q does not name Jamf Pro's API roles and clients", got)
	}
}
