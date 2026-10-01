// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/config"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

type basicLoginRecorder struct {
	mu   sync.Mutex
	hits []string
}

func (r *basicLoginRecorder) server(t *testing.T, name string) *url.URL {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if strings.HasPrefix(req.Header.Get("Authorization"), "Basic ") {
			r.mu.Lock()
			r.hits = append(r.hits, name+" "+req.Host+req.URL.Path)
			r.mu.Unlock()
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func (r *basicLoginRecorder) take() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	hits := r.hits
	r.hits = nil
	return hits
}

// hostRouter sends each request to the loopback server standing in for its
// host, so no request leaves the machine; an unrouted host is an error.
type hostRouter struct {
	routes map[string]*url.URL
	inner  http.RoundTripper
}

func (h hostRouter) RoundTrip(req *http.Request) (*http.Response, error) {
	target, ok := h.routes[req.URL.Hostname()]
	if !ok {
		return nil, fmt.Errorf("test router: no route for host %q", req.URL.Host)
	}
	out := req.Clone(req.Context())
	out.Host = req.URL.Host
	out.URL.Scheme, out.URL.Host = target.Scheme, target.Host
	return h.inner.RoundTrip(out)
}

// TestRadarBasicLoginOnlyReachesTheRadarHost holds that the Security Cloud
// Radar application secret goes to api.wandera.com, or the JAMFSECURITY_URL
// override, and never to a host the profile's URL or --url names for Jamf Pro
// or the platform gateway.
func TestRadarBasicLoginOnlyReachesTheRadarHost(t *testing.T) {
	rec := &basicLoginRecorder{}
	radar := rec.server(t, "radar")
	other := rec.server(t, "pro-or-gateway")

	inner := &http.Transport{}
	t.Cleanup(inner.CloseIdleConnections)
	router := hostRouter{inner: inner, routes: map[string]*url.URL{
		"api.wandera.com":      radar,
		"radar-sandbox.test":   radar,
		"eu.api.jamfcloud.com": other,
		"acme.jamfcloud.com":   other,
	}}

	radarPair := func(p config.Profile) config.Profile {
		p.RiskClientID = "env:RADAR_TEST_RISK_ID"
		p.RiskClientSecret = "env:RADAR_TEST_RISK_SECRET"
		return p
	}

	cases := []struct {
		name        string
		profiles    map[string]config.Profile
		envCreds    bool
		useProfile  string
		urlFlag     string
		securityURL string
		wantHost    string
	}{
		{
			name:       "security-only profile",
			profiles:   map[string]config.Profile{"sec": radarPair(config.Profile{Product: "security"})},
			useProfile: "sec",
			wantHost:   "api.wandera.com",
		},
		{
			name: "platform setup then security setup on one profile",
			profiles: map[string]config.Profile{"both": radarPair(config.Profile{
				URL:           "https://eu.api.jamfcloud.com",
				AuthMethod:    "platform",
				EnvironmentID: "env-1",
				ClientID:      "env:RADAR_TEST_GW_ID",
				ClientSecret:  "env:RADAR_TEST_GW_SECRET",
			})},
			useProfile: "both",
			wantHost:   "api.wandera.com",
		},
		{
			name:       "pro profile carrying radar keys",
			profiles:   map[string]config.Profile{"pro": radarPair(config.Profile{URL: "https://acme.jamfcloud.com"})},
			useProfile: "pro",
			wantHost:   "api.wandera.com",
		},
		{
			name:     "env radar creds with a pro default profile",
			profiles: map[string]config.Profile{"pro": {URL: "https://acme.jamfcloud.com"}},
			envCreds: true,
			wantHost: "api.wandera.com",
		},
		{
			name:       "--url naming a Jamf Pro host",
			profiles:   map[string]config.Profile{"sec": radarPair(config.Profile{Product: "security"})},
			useProfile: "sec",
			urlFlag:    "https://acme.jamfcloud.com",
			wantHost:   "api.wandera.com",
		},
		{
			name:        "JAMFSECURITY_URL override",
			profiles:    map[string]config.Profile{"pro": radarPair(config.Profile{URL: "https://acme.jamfcloud.com"})},
			useProfile:  "pro",
			securityURL: "https://radar-sandbox.test",
			wantHost:    "radar-sandbox.test",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", home)
			t.Setenv("XDG_CACHE_HOME", home)
			for _, k := range []string{
				"JAMF_PROFILE", "JAMF_URL", "JAMF_CLIENT_ID", "JAMF_CLIENT_SECRET",
				"JAMF_TENANT_ID", "JAMF_ENVIRONMENT_ID", "JAMFSECURITY_SSE_URL",
				"JAMFSECURITY_LIFECYCLE_CLIENT_ID", "JAMFSECURITY_LIFECYCLE_CLIENT_SECRET",
				"JAMFSECURITY_SSE_CLIENT_ID", "JAMFSECURITY_SSE_CLIENT_SECRET",
				"JAMFSECURITY_RISK_CLIENT_ID", "JAMFSECURITY_RISK_CLIENT_SECRET",
			} {
				t.Setenv(k, "")
			}
			t.Setenv("JAMFSECURITY_URL", tc.securityURL)
			t.Setenv("RADAR_TEST_RISK_ID", "fake-risk-id")
			t.Setenv("RADAR_TEST_RISK_SECRET", "fake-risk-secret")
			t.Setenv("RADAR_TEST_GW_ID", "fake-gw-id")
			t.Setenv("RADAR_TEST_GW_SECRET", "fake-gw-secret")
			if tc.envCreds {
				t.Setenv("JAMFSECURITY_RISK_CLIENT_ID", "fake-risk-id")
				t.Setenv("JAMFSECURITY_RISK_CLIENT_SECRET", "fake-risk-secret")
			}

			oldProfile, oldServerURL := profile, serverURL
			profile, serverURL = tc.useProfile, tc.urlFlag
			t.Cleanup(func() { profile, serverURL = oldProfile, oldServerURL })

			def := tc.useProfile
			if def == "" {
				def = "pro"
			}
			cfg := &config.Config{DefaultProfile: def, Profiles: tc.profiles}

			rec.take()
			cliCtx := &registry.CLIContext{}
			if err := buildSecurityClient(cfg, cliCtx, router); err != nil {
				t.Fatalf("buildSecurityClient: %v", err)
			}
			_ = cliCtx.SecurityClient.DoExpectRisk(context.Background(), http.MethodGet, "/v2/devices", nil, nil)

			hits := rec.take()
			if len(hits) == 0 {
				t.Fatalf("no server received the Basic login")
			}
			want := "radar " + tc.wantHost
			for _, h := range hits {
				if !strings.HasPrefix(h, want) {
					t.Errorf("Radar Basic credential sent as %q, want only %s", h, tc.wantHost)
				}
			}
		})
	}
}
