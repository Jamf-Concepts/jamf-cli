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

// securityCloudCategoriesProbePath is the exact path the Security Cloud tenant
// probe must request, registered exactly rather than as a prefix so this test is
// what catches the URL ordering going wrong.
//
// The scope is not in the URL any more: it travels as an X-Tenant-Id header, so
// the path is /{namespace}/{version}/{resource} and a tenant segment
// appearing anywhere in it is a regression. Registering the path exactly (rather
// than as a prefix) is what surfaces that — as a handler the client never calls.
const securityCloudCategoriesProbePath = "/securitycloud/v1/categories"

// gatewayStub serves the gateway endpoints validatePlatformGatewayCredentials
// touches: the OAuth2 token endpoint, and the Security Cloud categories
// collection it probes to check the Security Cloud tenant.
func gatewayStub(t *testing.T, tokenStatus int, categories func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
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
	if categories != nil {
		mux.HandleFunc(securityCloudCategoriesProbePath, categories)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestValidatePlatformGatewayCredentials_ReportsSecurityCloudAccess(t *testing.T) {
	srv := gatewayStub(t, http.StatusOK, func(w http.ResponseWriter, _ *http.Request) {
		writeJSONStatus(w, http.StatusOK, map[string]any{"results": []any{}, "totalCount": 0})
	})

	var out bytes.Buffer
	creds := &platformGatewayCredentials{
		GatewayURL: srv.URL, ClientID: "id", ClientSecret: "secret", TenantID: "a-tenant",
	}
	securityCloud, _, err := validatePlatformGatewayCredentials(context.Background(), &out, creds)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if securityCloud != securityCloudEntitled {
		t.Errorf("verdict = %v, want securityCloudEntitled: the gateway served the read", securityCloud)
	}
	if !strings.Contains(out.String(), "Checking Jamf Security Cloud access... yes") {
		t.Errorf("output does not report access:\n%s", out.String())
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
	if _, _, err := validatePlatformGatewayCredentials(context.Background(), &out, creds); err == nil {
		t.Fatal("expected credential validation to fail")
	}
}

// TestValidatePlatformGatewayCredentials_SecurityCloudTenant covers what the
// probe is for now that a profile carries one tenant: telling the operator which
// half of `security` this tenant serves. Every outcome still saves the profile —
// a Jamf Pro tenant legitimately has no Security Cloud entitlement, and the
// gateway's two rejections are indistinguishable in intent from here, so none of
// them can be treated as a setup failure.
func TestValidatePlatformGatewayCredentials_SecurityCloudTenant(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		body        string
		wantText    string
		wantVerdict securityCloudVerdict
	}{
		{
			name:        "reachable",
			status:      http.StatusOK,
			body:        `{"results":[],"totalCount":0}`,
			wantText:    "Checking Jamf Security Cloud access... yes",
			wantVerdict: securityCloudEntitled,
		},
		{
			// A tenant ID the gateway refuses, which is also how it answers an
			// environment ID typed at the tenant prompt. Not an entitlement
			// answer — see reportSecurityCloudProbe.
			name:        "tenant id the gateway will not accept",
			status:      http.StatusForbidden,
			body:        `{"httpStatus":403,"errors":[{"code":"OWNERSHIP_FORBIDDEN"}]}`,
			wantText:    "will not accept this tenant ID",
			wantVerdict: securityCloudUnknown,
		},
		{
			// Refused on grants. Deliberately not worded as a licensing
			// verdict: BAD_PERMISSIONS is the same code for no Security Cloud
			// entitlement and for an entitled tenant whose integration lacks
			// content-categories:read, and one read cannot separate them.
			name:        "refused on grants",
			status:      http.StatusForbidden,
			body:        `{"httpStatus":403,"errors":[{"code":"BAD_PERMISSIONS"}]}`,
			wantText:    "or the integration lacks this permission",
			wantVerdict: securityCloudUnentitled,
		},
		{
			// A 500 did not answer the question. It used to return the same
			// false a BAD_PERMISSIONS did, and the summary then told the
			// operator this scope lacks a Security Cloud entitlement — a
			// verdict nothing had established. Asserted on the wording *and*
			// the verdict, because either alone passes for the old behaviour.
			name:        "an inconclusive probe is not an entitlement verdict",
			status:      http.StatusInternalServerError,
			body:        `{"httpStatus":500,"errors":[{"code":"BOOM"}]}`,
			wantText:    "could not tell (",
			wantVerdict: securityCloudUnknown,
		},
		{
			// An environment ID the gateway does not know, which is the level
			// the ownership branch used to mis-name. 404 rather than 403.
			name:        "environment id the gateway does not know",
			status:      http.StatusNotFound,
			body:        `{"httpStatus":404,"errors":[{"code":"ENVIRONMENT_NOT_FOUND"}]}`,
			wantText:    "does not know this environment ID",
			wantVerdict: securityCloudUnknown,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := gatewayStub(t, http.StatusOK, func(w http.ResponseWriter, r *http.Request) {
				// Registered exactly, so a tenant segment creeping back into
				// the URL shows up here as a handler the client never calls.
				if r.URL.Path != securityCloudCategoriesProbePath {
					t.Errorf("probed %q, want %q", r.URL.Path, securityCloudCategoriesProbePath)
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
			// The ENVIRONMENT_NOT_FOUND row is the environment-level one, and
			// the level decides the wording of the refusals.
			if strings.Contains(tc.wantText, "environment ID") {
				creds.TenantID, creds.EnvironmentID = "", "an-environment"
			}
			verdict, _, err := validatePlatformGatewayCredentials(context.Background(), &out, creds)
			if err != nil {
				t.Fatalf("validate returned an error; a Security Cloud outcome must report and save: %v", err)
			}
			if !strings.Contains(out.String(), tc.wantText) {
				t.Errorf("output missing %q:\n%s", tc.wantText, out.String())
			}
			if verdict != tc.wantVerdict {
				t.Errorf("verdict = %v, want %v — the summary claims an entitlement from this",
					verdict, tc.wantVerdict)
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
