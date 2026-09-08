// Copyright 2026, Jamf Software LLC

package commands

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// scopeProbePath is the exact path the scope probe must request, registered
// exactly rather than as a prefix so this test is what catches the URL going
// wrong.
//
// The scope is not in the URL: it travels as an X-Tenant-Id or X-Environment-Id
// header, so the path is /{namespace}/{version}/{resource} and a tenant segment
// appearing anywhere in it is a regression, surfacing here as a handler the
// client never calls.
const scopeProbePath = "/pro/v1/jamf-pro-version"

// gatewayStub serves the gateway endpoints validatePlatformGatewayCredentials
// touches: the OAuth2 token endpoint, and the path the scope probe reads.
func gatewayStub(t *testing.T, tokenStatus int, scopeProbe func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/token", func(w http.ResponseWriter, r *http.Request) {
		if tokenStatus != http.StatusOK {
			w.WriteHeader(tokenStatus)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"access_token":"stub-token","token_type":"Bearer","expires_in":900}`)
	})
	if scopeProbe != nil {
		mux.HandleFunc(scopeProbePath, scopeProbe)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// The probe asks about the scope ID and nothing else, so a served read reports
// the ID accepted rather than any product's access.
func TestValidatePlatformGatewayCredentials_ReportsTheScopeIDAccepted(t *testing.T) {
	srv := gatewayStub(t, http.StatusOK, func(w http.ResponseWriter, _ *http.Request) {
		writeJSONStatus(w, http.StatusOK, map[string]any{"version": "11.31.0"})
	})

	var out bytes.Buffer
	creds := &platformGatewayCredentials{
		GatewayURL: srv.URL, ClientID: "id", ClientSecret: "secret", TenantID: "a-tenant",
	}
	rejected, err := validatePlatformGatewayCredentials(context.Background(), &out, creds)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if rejected {
		t.Error("a served read must not report the scope ID as rejected")
	}
	if !strings.Contains(out.String(), "Checking the scope ID... ok: the gateway accepts this tenant ID") {
		t.Errorf("output does not report the scope ID accepted:\n%s", out.String())
	}
	// Setup collects credentials and validates them. It must not report on any
	// product's entitlement: a capability permission is per operation, and the
	// gateway spells "no entitlement" and "this grant is missing" identically,
	// so a verdict here can only be less accurate than the 403 that names the
	// permission it wanted.
	for _, unwanted := range []string{"Security Cloud", "entitle"} {
		if strings.Contains(out.String(), unwanted) {
			t.Errorf("setup reports a product entitlement (%q):\n%s", unwanted, out.String())
		}
	}
}

// Organization scope has no ID to check, so the probe is skipped entirely
// rather than reporting a failure that says nothing about the profile. The stub
// serves no probe path, so a request would 404 and be visible as a rejection.
func TestValidatePlatformGatewayCredentials_OrganizationScopeSkipsTheProbe(t *testing.T) {
	srv := gatewayStub(t, http.StatusOK, nil)

	var out bytes.Buffer
	creds := &platformGatewayCredentials{GatewayURL: srv.URL, ClientID: "id", ClientSecret: "secret"}
	rejected, err := validatePlatformGatewayCredentials(context.Background(), &out, creds)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if rejected {
		t.Error("organization scope sends no scope header, so there is no ID to reject")
	}
	if strings.Contains(out.String(), "Checking the scope ID") {
		t.Errorf("organization scope has no scope ID to check:\n%s", out.String())
	}
}

func TestValidatePlatformGatewayCredentials_BadCredentialsAreFatal(t *testing.T) {
	srv := gatewayStub(t, http.StatusUnauthorized, nil)

	var out bytes.Buffer
	creds := &platformGatewayCredentials{
		GatewayURL: srv.URL, ClientID: "id", ClientSecret: "wrong", TenantID: "pro-tenant",
	}
	// Credentials that don't work are worth failing setup over: nothing the
	// profile could go on to do would succeed.
	if _, err := validatePlatformGatewayCredentials(context.Background(), &out, creds); err == nil {
		t.Fatal("expected credential validation to fail")
	}
}

// Only two codes reject a scope ID, and everything else must leave it unjudged.
// Every outcome still saves the profile: the probe answers "does the gateway
// know this ID", which is not a pass/fail for the credentials.
//
// The rows are the wire answers recorded on reportScopeIDProbe, probed
// 2026-09-08 against this same path at all three levels.
func TestValidatePlatformGatewayCredentials_ScopeIDVerdicts(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		body         string
		environment  bool
		wantText     string
		wantRejected bool
	}{
		{
			name:     "the gateway accepts the id",
			status:   http.StatusOK,
			body:     `{"version":"11.31.0"}`,
			wantText: "ok: the gateway accepts this tenant ID",
		},
		{
			// Also how it answers an environment or organization ID typed at
			// the tenant prompt.
			name:         "tenant id the gateway will not accept",
			status:       http.StatusForbidden,
			body:         `{"httpStatus":403,"errors":[{"code":"OWNERSHIP_FORBIDDEN"}]}`,
			wantText:     "will not accept this tenant ID",
			wantRejected: true,
		},
		{
			// The issue #354 mis-paste: a tenant ID typed at the environment
			// prompt. 404 rather than 403, and there is no TENANT_NOT_FOUND
			// twin at the other level.
			name:         "environment id the gateway does not know",
			status:       http.StatusNotFound,
			body:         `{"httpStatus":404,"errors":[{"code":"ENVIRONMENT_NOT_FOUND"}]}`,
			environment:  true,
			wantText:     "does not know this environment ID",
			wantRejected: true,
		},
		{
			// A capability refusal says nothing about the ID — it means the
			// header got as far as being resolved, since the gateway checks
			// ownership before capability. Reading it as a verdict is what
			// produced the entitlement claim this probe replaced.
			name:     "a capability refusal is not a verdict on the id",
			status:   http.StatusForbidden,
			body:     `{"httpStatus":403,"errors":[{"code":"BAD_PERMISSIONS"}]}`,
			wantText: "could not confirm (",
		},
		{
			name:     "a 5xx did not answer the question",
			status:   http.StatusInternalServerError,
			body:     `{"httpStatus":500,"errors":[{"code":"BOOM"}]}`,
			wantText: "could not confirm (",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := gatewayStub(t, http.StatusOK, func(w http.ResponseWriter, r *http.Request) {
				// Registered exactly, so a tenant segment creeping back into
				// the URL shows up here as a handler the client never calls.
				if r.URL.Path != scopeProbePath {
					t.Errorf("probed %q, want %q", r.URL.Path, scopeProbePath)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = fmt.Fprint(w, tc.body)
			})

			var out bytes.Buffer
			creds := &platformGatewayCredentials{
				GatewayURL: srv.URL, ClientID: "id", ClientSecret: "secret",
				TenantID: "a-tenant",
			}
			if tc.environment {
				creds.TenantID, creds.EnvironmentID = "", "an-environment"
			}
			rejected, err := validatePlatformGatewayCredentials(context.Background(), &out, creds)
			if err != nil {
				t.Fatalf("validate returned an error; a scope-probe outcome must report and save: %v", err)
			}
			if !strings.Contains(out.String(), tc.wantText) {
				t.Errorf("output missing %q:\n%s", tc.wantText, out.String())
			}
			if rejected != tc.wantRejected {
				t.Errorf("rejected = %v, want %v — the summary is suppressed on this",
					rejected, tc.wantRejected)
			}
		})
	}
}

// TestPromptScope pins that an environment answer ends the questioning.
//
// The three levels are mutually exclusive — an integration is created at one of
// them and its credential only works with that level's header — so prompting for
// a tenant after an environment ID has been given offers a combination that
// cannot work, and would then have to be rejected. Asking one question fewer is
// the whole fix.
func TestPromptScope(t *testing.T) {
	cases := []struct {
		name            string
		input           string
		wantEnvironment string
		wantTenant      string
		wantNoTenantAsk bool
	}{
		{
			name:            "environment answer skips the tenant prompt",
			input:           "env-123\n",
			wantEnvironment: "env-123",
			wantNoTenantAsk: true,
		},
		{
			name:       "blank environment falls through to tenant",
			input:      "\nten-456\n",
			wantTenant: "ten-456",
		},
		{
			name:  "both blank is organization scope",
			input: "\n\n",
		},
		{
			name:            "surrounding whitespace is trimmed",
			input:           "  env-789  \n",
			wantEnvironment: "env-789",
			wantNoTenantAsk: true,
		},
		{
			// A closed stdin must not hang or loop: ReadString returns io.EOF
			// with an empty line, which reads as "skipped".
			name:  "eof is treated as skipped",
			input: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			env, ten := promptScope(&out, bufio.NewReader(strings.NewReader(tc.input)))

			if env != tc.wantEnvironment {
				t.Errorf("environment = %q, want %q", env, tc.wantEnvironment)
			}
			if ten != tc.wantTenant {
				t.Errorf("tenant = %q, want %q", ten, tc.wantTenant)
			}
			if env != "" && ten != "" {
				t.Errorf("returned both levels (%q, %q); they are mutually exclusive", env, ten)
			}
			askedForTenant := strings.Contains(out.String(), "Tenant ID")
			if tc.wantNoTenantAsk && askedForTenant {
				t.Errorf("asked for a tenant ID after an environment ID was supplied:\n%s", out.String())
			}
			if !tc.wantNoTenantAsk && !askedForTenant {
				t.Errorf("did not ask for a tenant ID:\n%s", out.String())
			}
		})
	}
}
