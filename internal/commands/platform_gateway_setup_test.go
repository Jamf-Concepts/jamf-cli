// Copyright 2026, Jamf Software LLC

package commands

import (
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
// Security Cloud puts /tenant/{id} *before* the version, which every other Jamf
// namespace does the other way round. The generated commands build that shape
// from tenantFirstServices in generator/parser/platform.go while this probe gets
// it from the SDK's TenantPrefix, so the two copies of the rule have to agree —
// and this is where a divergence surfaces, as a handler the client never calls.
const securityCloudCategoriesProbePath = "/api/securitycloud/tenant/jsc-tenant/v1/categories"

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

func TestValidatePlatformGatewayCredentials_NoSecurityCloudTenant(t *testing.T) {
	srv := gatewayStub(t, http.StatusOK, func(w http.ResponseWriter, r *http.Request) {
		t.Error("Security Cloud was probed even though no tenant was supplied")
	})

	var out bytes.Buffer
	creds := &platformGatewayCredentials{
		GatewayURL: srv.URL, ClientID: "id", ClientSecret: "secret", TenantID: "pro-tenant",
	}
	if err := validatePlatformGatewayCredentials(context.Background(), &out, creds); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if strings.Contains(out.String(), "Security Cloud") {
		t.Errorf("mentioned Security Cloud with no tenant configured:\n%s", out.String())
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
	if err := validatePlatformGatewayCredentials(context.Background(), &out, creds); err == nil {
		t.Fatal("expected credential validation to fail")
	}
}

// TestValidatePlatformGatewayCredentials_SecurityCloudTenant covers the reason
// this validation exists: a Security Cloud tenant ID is easy to get wrong (it is
// not the Pro tenant, and not the client ID), and the gateway says *how* it is
// wrong. Every outcome still saves the profile — the entitlement may simply not
// be provisioned yet, and refusing would block a valid Pro-only profile.
func TestValidatePlatformGatewayCredentials_SecurityCloudTenant(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     string
		wantText string
	}{
		{
			name:     "reachable",
			status:   http.StatusOK,
			body:     `{"results":[],"totalCount":0}`,
			wantText: "Validating Security Cloud tenant... ok",
		},
		{
			name:     "wrong tenant",
			status:   http.StatusForbidden,
			body:     `{"httpStatus":403,"errors":[{"code":"OWNERSHIP_FORBIDDEN"}]}`,
			wantText: "rejected this tenant",
		},
		{
			name:     "not entitled",
			status:   http.StatusForbidden,
			body:     `{"httpStatus":403,"errors":[{"code":"BAD_PERMISSIONS"}]}`,
			wantText: "no Jamf Security Cloud entitlement",
		},
		{
			name:     "unexpected failure still warns",
			status:   http.StatusInternalServerError,
			body:     `{"httpStatus":500,"errors":[{"code":"BOOM"}]}`,
			wantText: "WARNING",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := gatewayStub(t, http.StatusOK, func(w http.ResponseWriter, r *http.Request) {
				// The probe must carry the Security Cloud tenant, not the Pro
				// one — resolving the wrong tenant is the failure this whole
				// check exists to catch — and it must carry it ahead of the
				// version, which is what puts Security Cloud mutations inside
				// the gateway's audit globs.
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
				TenantID: "pro-tenant", SecurityCloudTenantID: "jsc-tenant",
			}
			if err := validatePlatformGatewayCredentials(context.Background(), &out, creds); err != nil {
				t.Fatalf("validate returned an error; a Security Cloud problem must warn and save: %v", err)
			}
			if !strings.Contains(out.String(), tc.wantText) {
				t.Errorf("output missing %q:\n%s", tc.wantText, out.String())
			}
		})
	}
}
