// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Jamf-Concepts/jamf-cli/internal/auth"
	"github.com/Jamf-Concepts/jamf-cli/internal/config"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// ─── Mock auth provider ──────────────────────────────────────────────────────

// mockAuthProvider implements auth.Provider for testing without network calls.
type mockAuthProvider struct {
	token string
	err   error
}

func (m *mockAuthProvider) GetToken(_ context.Context) (string, error) { return m.token, m.err }
func (m *mockAuthProvider) Name() string                               { return "mock" }

// ─── OAuth2 test server helpers ──────────────────────────────────────────────

// newOAuth2TestServer starts an httptest.Server that serves a token response,
// registers server.Close with t.Cleanup, and returns a configured OAuth2Provider.
func newOAuth2TestServer(t *testing.T, token string, expiresIn int) *auth.OAuth2Provider {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": token,
			"token_type":   "Bearer",
			"expires_in":   expiresIn,
		})
	}))
	t.Cleanup(srv.Close)
	return auth.NewOAuth2Provider(srv.URL, "test-client-id", "test-secret")
}

// newPlatformOAuth2TestServer starts an httptest.Server for the platform
// gateway token endpoint and returns a configured PlatformOAuth2Provider.
func newPlatformOAuth2TestServer(t *testing.T, token string, expiresIn int) *auth.PlatformOAuth2Provider {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": token,
			"token_type":   "Bearer",
			"expires_in":   expiresIn,
		})
	}))
	t.Cleanup(srv.Close)
	return auth.NewPlatformOAuth2Provider(srv.URL, "test-client-id", "test-secret", auth.TenantScope("tenant-uuid"))
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// parseAuthTokenOutput parses captured rawData bytes as a JSON object.
func parseAuthTokenOutput(t *testing.T, data []byte) map[string]any {
	t.Helper()
	if len(data) == 0 {
		t.Fatal("command produced no output")
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, data)
	}
	return m
}

// runProAuthToken builds a pro auth token command, runs RunE, and returns
// the parsed JSON output map.
func runProAuthToken(t *testing.T, provider auth.Provider) (map[string]any, error) {
	t.Helper()
	return runProAuthTokenCmd(t, provider, false)
}

// runProAuthTokenWithRefresh is like runProAuthToken but sets --refresh.
func runProAuthTokenWithRefresh(t *testing.T, provider auth.Provider) (map[string]any, error) {
	t.Helper()
	return runProAuthTokenCmd(t, provider, true)
}

func runProAuthTokenCmd(t *testing.T, provider auth.Provider, refresh bool) (map[string]any, error) {
	t.Helper()
	out := &captureOutput{}
	cliCtx := &registry.CLIContext{
		Output:       out,
		AuthProvider: provider,
	}
	cmd := newProAuthTokenCmd(cliCtx)
	cmd.SetContext(context.Background())
	if refresh {
		if err := cmd.Flags().Set("refresh", "true"); err != nil {
			t.Fatalf("setting --refresh flag: %v", err)
		}
	}
	if err := cmd.RunE(cmd, nil); err != nil {
		return nil, err
	}
	return parseAuthTokenOutput(t, out.rawData), nil
}

// runPlatformAuthToken mirrors runProAuthToken but uses the platform auth command.
func runPlatformAuthToken(t *testing.T, provider auth.Provider) (map[string]any, error) {
	t.Helper()
	return runPlatformAuthTokenCmd(t, provider, false)
}

// runPlatformAuthTokenWithRefresh is like runPlatformAuthToken but sets --refresh.
func runPlatformAuthTokenWithRefresh(t *testing.T, provider auth.Provider) (map[string]any, error) {
	t.Helper()
	return runPlatformAuthTokenCmd(t, provider, true)
}

func runPlatformAuthTokenCmd(t *testing.T, provider auth.Provider, refresh bool) (map[string]any, error) {
	t.Helper()
	out := &captureOutput{}
	cliCtx := &registry.CLIContext{
		Output:       out,
		AuthProvider: provider,
	}
	cmd := newPlatformAuthTokenCmd(cliCtx)
	cmd.SetContext(context.Background())
	if refresh {
		if err := cmd.Flags().Set("refresh", "true"); err != nil {
			t.Fatalf("setting --refresh flag: %v", err)
		}
	}
	if err := cmd.RunE(cmd, nil); err != nil {
		return nil, err
	}
	return parseAuthTokenOutput(t, out.rawData), nil
}

// ─── Structure tests ─────────────────────────────────────────────────────────

func TestProAuthCommandExists(t *testing.T) {
	root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")
	pro := findSubcommand(root, "pro")
	if pro == nil {
		t.Fatal("pro command not found")
		return
	}
	if findSubcommand(pro, "auth") == nil {
		t.Fatal("expected 'auth' subcommand under 'pro'")
		return
	}
}

func TestProAuthTokenCommandExists(t *testing.T) {
	root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")
	pro := findSubcommand(root, "pro")
	if pro == nil {
		t.Fatal("pro command not found")
		return
	}
	authCmd := findSubcommand(pro, "auth")
	if authCmd == nil {
		t.Fatal("auth command not found under pro")
		return
	}
	if findSubcommand(authCmd, "token") == nil {
		t.Fatal("expected 'token' subcommand under 'pro auth'")
		return
	}
}

func TestProAuthGroupedInCore(t *testing.T) {
	gid, ok := proGroupMap["auth"]
	if !ok {
		t.Fatal("'auth' not found in proGroupMap")
		return
	}
	if gid != groupCore {
		t.Errorf("proGroupMap[auth] = %q, want %q", gid, groupCore)
	}
}

func TestPlatformAuthCommandExists(t *testing.T) {
	root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")
	platform := findSubcommand(root, "platform")
	if platform == nil {
		t.Fatal("platform command not found")
		return
	}
	if findSubcommand(platform, "auth") == nil {
		t.Fatal("expected 'auth' subcommand under 'platform'")
		return
	}
}

func TestPlatformAuthTokenCommandExists(t *testing.T) {
	root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")
	platform := findSubcommand(root, "platform")
	if platform == nil {
		t.Fatal("platform command not found")
		return
	}
	authCmd := findSubcommand(platform, "auth")
	if authCmd == nil {
		t.Fatal("auth command not found under platform")
		return
	}
	if findSubcommand(authCmd, "token") == nil {
		t.Fatal("expected 'token' subcommand under 'platform auth'")
		return
	}
}

// ─── pro auth token ───────────────────────────────────────────────────────────

func TestProAuthToken_TokenProvider_NoExpiry(t *testing.T) {
	// TokenProvider has no expiry — output should have "token" but not "expires_at".
	m, err := runProAuthToken(t, auth.NewTokenProvider("tok-abc123"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m["token"] != "tok-abc123" {
		t.Errorf("token = %v, want %q", m["token"], "tok-abc123")
	}
	if _, ok := m["expires_at"]; ok {
		t.Error("expires_at must not be present for token auth (no expiry information)")
	}
}

func TestProAuthToken_OAuth2Provider_WithExpiry(t *testing.T) {
	// OAuth2Provider populates expiresAt after GetToken — output includes expires_at.
	p := newOAuth2TestServer(t, "oauth2-jwt-token", 3600)
	m, err := runProAuthToken(t, p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m["token"] != "oauth2-jwt-token" {
		t.Errorf("token = %v, want %q", m["token"], "oauth2-jwt-token")
	}
	expiresAt, ok := m["expires_at"]
	if !ok {
		t.Fatal("expires_at must be present for oauth2 auth")
		return
	}
	ts, ok := expiresAt.(string)
	if !ok {
		t.Fatalf("expires_at is not a string: %T", expiresAt)
	}
	parsed, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		t.Fatalf("expires_at %q is not valid RFC3339: %v", ts, err)
	}
	if !parsed.After(time.Now()) {
		t.Errorf("expires_at %v is not in the future", parsed)
	}
}

func TestProAuthToken_Error(t *testing.T) {
	p := &mockAuthProvider{err: fmt.Errorf("credential vault offline")}
	_, err := runProAuthToken(t, p)
	if err == nil {
		t.Fatal("expected error from failing provider, got nil")
		return
	}
}

// ─── platform auth token ─────────────────────────────────────────────────────

func TestPlatformAuthToken_PlatformOAuth2Provider_WithExpiry(t *testing.T) {
	p := newPlatformOAuth2TestServer(t, "platform-jwt", 3600)
	m, err := runPlatformAuthToken(t, p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m["token"] != "platform-jwt" {
		t.Errorf("token = %v, want %q", m["token"], "platform-jwt")
	}
	if _, ok := m["expires_at"]; !ok {
		t.Error("expires_at must be present for platform oauth2 auth")
	}
}

func TestPlatformAuthToken_Error(t *testing.T) {
	p := &mockAuthProvider{err: fmt.Errorf("gateway unreachable")}
	_, err := runPlatformAuthToken(t, p)
	if err == nil {
		t.Fatal("expected error from failing provider, got nil")
		return
	}
}

// ─── --refresh flag ───────────────────────────────────────────────────────────

// newCountingOAuth2TestServer starts an httptest.Server that tracks how many
// times it has been called, returning a distinct token each time.
func newCountingOAuth2TestServer(t *testing.T, expiresIn int) (*auth.OAuth2Provider, *int) {
	t.Helper()
	var count int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": fmt.Sprintf("token-%d", count),
			"token_type":   "Bearer",
			"expires_in":   expiresIn,
		})
	}))
	t.Cleanup(srv.Close)
	return auth.NewOAuth2Provider(srv.URL, "test-client-id", "test-secret"), &count
}

func TestProAuthToken_Refresh_ForcesNewExchange(t *testing.T) {
	p, calls := newCountingOAuth2TestServer(t, 3600)

	// Prime the in-memory cache with an initial token exchange.
	if _, err := p.GetToken(context.Background()); err != nil {
		t.Fatalf("priming GetToken: %v", err)
	}
	if *calls != 1 {
		t.Fatalf("expected 1 server call after priming, got %d", *calls)
	}

	// Without --refresh: returns the cached token, no extra server call.
	m1, err := runProAuthToken(t, p)
	if err != nil {
		t.Fatalf("without refresh: %v", err)
	}
	if *calls != 1 {
		t.Errorf("without refresh: expected 1 total server calls, got %d", *calls)
	}
	if m1["token"] != "token-1" {
		t.Errorf("without refresh: token = %v, want %q", m1["token"], "token-1")
	}

	// With --refresh: bypasses cache and exchanges a new token.
	m2, err := runProAuthTokenWithRefresh(t, p)
	if err != nil {
		t.Fatalf("with refresh: %v", err)
	}
	if *calls != 2 {
		t.Errorf("with refresh: expected 2 total server calls, got %d", *calls)
	}
	if m2["token"] != "token-2" {
		t.Errorf("with refresh: token = %v, want %q", m2["token"], "token-2")
	}
}

func TestProAuthToken_Refresh_TokenProvider_Noop(t *testing.T) {
	// TokenProvider doesn't implement Refresher — --refresh is accepted but has no effect.
	p := auth.NewTokenProvider("static-token")
	m, err := runProAuthTokenWithRefresh(t, p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m["token"] != "static-token" {
		t.Errorf("token = %v, want %q", m["token"], "static-token")
	}
}

func TestPlatformAuthToken_Refresh_ForcesNewExchange(t *testing.T) {
	var count int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": fmt.Sprintf("plat-token-%d", count),
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	t.Cleanup(srv.Close)
	p := auth.NewPlatformOAuth2Provider(srv.URL, "test-client-id", "test-secret", auth.TenantScope("tenant-uuid"))

	// Prime the in-memory cache.
	if _, err := p.GetToken(context.Background()); err != nil {
		t.Fatalf("priming GetToken: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 server call after priming, got %d", count)
	}

	// Without --refresh: returns the cached token.
	m1, err := runPlatformAuthToken(t, p)
	if err != nil {
		t.Fatalf("without refresh: %v", err)
	}
	if count != 1 {
		t.Errorf("without refresh: expected 1 total server calls, got %d", count)
	}
	if m1["token"] != "plat-token-1" {
		t.Errorf("without refresh: token = %v, want %q", m1["token"], "plat-token-1")
	}

	// With --refresh: forces a new exchange.
	m2, err := runPlatformAuthTokenWithRefresh(t, p)
	if err != nil {
		t.Fatalf("with refresh: %v", err)
	}
	if count != 2 {
		t.Errorf("with refresh: expected 2 total server calls, got %d", count)
	}
	if m2["token"] != "plat-token-2" {
		t.Errorf("with refresh: token = %v, want %q", m2["token"], "plat-token-2")
	}
}

// TestResolveAuthScopePrecedence pins that an explicitly supplied scope
// overrides the profile rather than colliding with it.
//
// Every other credential field here works that way, and the first version of
// this did not: a profile carrying tenant-id plus JAMF_ENVIRONMENT_ID merged
// into "both levels set" and was refused as a mutual-exclusion error, so a
// caller could not point a tenant profile at an environment without editing it.
// Only two explicitly supplied levels are a real conflict.
func TestResolveAuthScopePrecedence(t *testing.T) {
	cfg := &config.Config{
		DefaultProfile: "p",
		Profiles: map[string]config.Profile{
			"p": {
				URL:          "https://eu.api.jamfcloud.com",
				AuthMethod:   "platform",
				ClientID:     "env:TEST_SCOPE_ID",
				ClientSecret: "env:TEST_SCOPE_SECRET",
				TenantID:     "profile-tenant",
			},
			"envprofile": {
				URL:           "https://eu.api.jamfcloud.com",
				AuthMethod:    "platform",
				ClientID:      "env:TEST_SCOPE_ID",
				ClientSecret:  "env:TEST_SCOPE_SECRET",
				EnvironmentID: "profile-env",
			},
		},
	}

	cases := []struct {
		name       string
		profile    string
		params     AuthParams
		wantHeader string
		wantID     string
		wantErr    bool
	}{
		{
			name:       "profile tenant with no override",
			profile:    "p",
			wantHeader: "X-Tenant-Id",
			wantID:     "profile-tenant",
		},
		{
			name:       "environment override beats a tenant profile",
			profile:    "p",
			params:     AuthParams{EnvironmentID: "override-env"},
			wantHeader: "X-Environment-Id",
			wantID:     "override-env",
		},
		{
			name:       "tenant override beats an environment profile",
			profile:    "envprofile",
			params:     AuthParams{TenantID: "override-tenant"},
			wantHeader: "X-Tenant-Id",
			wantID:     "override-tenant",
		},
		{
			name:    "both supplied explicitly is a conflict",
			profile: "p",
			params:  AuthParams{EnvironmentID: "e", TenantID: "t"},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TEST_SCOPE_ID", "cid")
			t.Setenv("TEST_SCOPE_SECRET", "csecret")

			params := tc.params
			params.Profile = tc.profile
			_, provider, err := ResolveAuthForProfile(cfg, params)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected two explicitly supplied levels to be refused")
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveAuthForProfile: %v", err)
			}
			p, ok := provider.(*auth.PlatformOAuth2Provider)
			if !ok {
				t.Fatalf("provider is %T, want a platform provider", provider)
			}
			name, value := p.Scope().Header()
			if name != tc.wantHeader || value != tc.wantID {
				t.Errorf("scope header = (%q, %q), want (%q, %q)", name, value, tc.wantHeader, tc.wantID)
			}
		})
	}
}

// TestResolveAuthRefusesTheRetiredGateway pins the migration off
// {region}.apigw.jamf.com by name.
//
// Every platform profile written before 2026-08-28 names that host, and it does
// not serve the GA path shape — the GA gateway at {region}.api.jamfcloud.com
// mounts each namespace at the root and answers 404 for anything under /api,
// which is the prefix the old host required. Left to fail on the wire, the
// symptom lands in the token exchange, before the command the user typed is
// sent, as an edge-level 403 with an HTML body that names neither the host nor
// the reason. So the refusal happens here and names the replacement URL.
func TestResolveAuthRefusesTheRetiredGateway(t *testing.T) {
	cfg := &config.Config{
		DefaultProfile: "old",
		Profiles: map[string]config.Profile{
			"old": {
				URL:          "https://eu.apigw.jamf.com",
				AuthMethod:   "platform",
				ClientID:     "env:TEST_RETIRED_ID",
				ClientSecret: "env:TEST_RETIRED_SECRET",
				TenantID:     "t",
			},
			"ga": {
				URL:          "https://eu.api.jamfcloud.com",
				AuthMethod:   "platform",
				ClientID:     "env:TEST_RETIRED_ID",
				ClientSecret: "env:TEST_RETIRED_SECRET",
				TenantID:     "t",
			},
		},
	}
	t.Setenv("TEST_RETIRED_ID", "cid")
	t.Setenv("TEST_RETIRED_SECRET", "csecret")

	_, _, err := ResolveAuthForProfile(cfg, AuthParams{Profile: "old"})
	if err == nil {
		t.Fatal("expected the retired gateway host to be refused")
	}
	// The message has to carry the replacement, not just the complaint.
	if !strings.Contains(err.Error(), "https://eu.api.jamfcloud.com") {
		t.Errorf("error does not name the GA URL to switch to: %v", err)
	}

	if _, _, err := ResolveAuthForProfile(cfg, AuthParams{Profile: "ga"}); err != nil {
		t.Errorf("the GA host must be accepted: %v", err)
	}
}

// TestPlatformGatewayURLForRegion covers the host rewrite behind that refusal,
// including the shapes that must NOT be rewritten — a GA URL passed through
// again would otherwise be reported as retired.
func TestPlatformGatewayURLForRegion(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://eu.apigw.jamf.com", "https://eu.api.jamfcloud.com"},
		{"https://us.apigw.jamf.com/", "https://us.api.jamfcloud.com"},
		{"apac.apigw.jamf.com", "https://apac.api.jamfcloud.com"},
		{"https://eu.api.jamfcloud.com", ""},
		{"https://mycompany.jamfcloud.com", ""},
		{"https://apigw.jamf.com", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := platformGatewayURLForRegion(c.in); got != c.want {
			t.Errorf("platformGatewayURLForRegion(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
