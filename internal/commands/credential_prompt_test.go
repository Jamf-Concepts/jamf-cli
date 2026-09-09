// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
