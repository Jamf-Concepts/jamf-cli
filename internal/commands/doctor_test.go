// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/config"
)

func TestFingerprint_LongString(t *testing.T) {
	got := fingerprint("abcdefghijk")
	if got != "abcd••••" {
		t.Errorf("expected abcd••••, got %q", got)
	}
}

func TestFingerprint_ShortString_AllRedacted(t *testing.T) {
	got := fingerprint("ab")
	if got != "••" {
		t.Errorf("short strings should be fully redacted, got %q", got)
	}
}

func TestFingerprint_Empty(t *testing.T) {
	if fingerprint("") != "" {
		t.Error("empty input should yield empty output")
	}
}

func TestResolveProfileNameForDoctor_Precedence(t *testing.T) {
	tests := []struct {
		name           string
		args           []string
		flagProfile    string
		envProfile     string
		defaultProfile string
		wantName       string
		wantSource     string
	}{
		{
			name:       "positional arg wins",
			args:       []string{"explicit"},
			wantName:   "explicit",
			wantSource: "positional argument",
		},
		{
			name:        "flag beats env and default",
			flagProfile: "from-flag",
			envProfile:  "from-env",
			wantName:    "from-flag",
			wantSource:  "--profile flag",
		},
		{
			name:           "env beats default",
			envProfile:     "from-env",
			defaultProfile: "from-config",
			wantName:       "from-env",
			wantSource:     "JAMF_PROFILE env var",
		},
		{
			name:           "config default fallback",
			defaultProfile: "from-config",
			wantName:       "from-config",
			wantSource:     "config default-profile",
		},
		{
			name: "nothing set returns empty",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			origProfile := profile
			t.Cleanup(func() { profile = origProfile })
			profile = tc.flagProfile

			if tc.envProfile != "" {
				t.Setenv("JAMF_PROFILE", tc.envProfile)
			} else {
				t.Setenv("JAMF_PROFILE", "")
			}

			cfg := &config.Config{
				DefaultProfile: tc.defaultProfile,
				Profiles:       map[string]config.Profile{},
			}
			gotName, gotSource := resolveProfileNameForDoctor(cfg, tc.args)
			if gotName != tc.wantName {
				t.Errorf("name: got %q, want %q", gotName, tc.wantName)
			}
			if gotSource != tc.wantSource {
				t.Errorf("source: got %q, want %q", gotSource, tc.wantSource)
			}
		})
	}
}

func TestProbeEnvVars_FingerprintsSecretsOnly(t *testing.T) {
	t.Setenv("JAMF_TOKEN", "supersecrettoken")
	t.Setenv("JAMF_URL", "https://example.jamfcloud.com")
	t.Setenv("JAMF_CLIENT_ID", "")

	entries := probeEnvVars()
	got := map[string]envEntry{}
	for _, e := range entries {
		got[e.Name] = e
	}

	tok, ok := got["JAMF_TOKEN"]
	if !ok || !tok.Set || tok.Value != "" || tok.Fingerprint != "supe••••" {
		t.Errorf("JAMF_TOKEN should be fingerprinted only, got %+v", tok)
	}
	url, ok := got["JAMF_URL"]
	if !ok || !url.Set || url.Value != "https://example.jamfcloud.com" || url.Fingerprint != "" {
		t.Errorf("JAMF_URL should expose value (non-secret), got %+v", url)
	}
	cid, ok := got["JAMF_CLIENT_ID"]
	if !ok || cid.Set {
		t.Errorf("JAMF_CLIENT_ID empty should report unset, got %+v", cid)
	}
}

func TestBuildDoctorReport_NoConfigNoProfile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // ensure no real config bleeds in
	t.Setenv("JAMF_PROFILE", "")
	origProfile := profile
	t.Cleanup(func() { profile = origProfile })
	profile = ""

	cfg := &config.Config{Profiles: map[string]config.Profile{}}
	report := buildDoctorReport(cfg, nil, "test-version")

	if report.Version != "test-version" {
		t.Errorf("expected version=test-version, got %q", report.Version)
	}
	if report.ConfigPresent {
		t.Errorf("expected ConfigPresent=false in fresh tmpdir, got true")
	}
	if report.Profile != nil {
		t.Errorf("expected no profile, got %+v", report.Profile)
	}
	if len(report.Notes) == 0 {
		t.Error("expected setup-suggestion note when no config and no profile")
	}
}

func TestBuildDoctorReport_ProfileNotFound_AddsNote(t *testing.T) {
	t.Setenv("JAMF_PROFILE", "")
	origProfile := profile
	t.Cleanup(func() { profile = origProfile })
	profile = ""

	cfg := &config.Config{Profiles: map[string]config.Profile{}}
	report := buildDoctorReport(cfg, []string{"missing-profile"}, "v1")

	if report.Profile != nil {
		t.Errorf("missing profile shouldn't populate Profile section, got %+v", report.Profile)
	}
	found := false
	for _, n := range report.Notes {
		if strings.Contains(n, "missing-profile") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected note about missing profile, got notes=%v", report.Notes)
	}
}

func TestProbeProfileCredentials_RecordsBadEnvRef(t *testing.T) {
	p := config.Profile{
		ClientID: "env:DEFINITELY_UNSET_VAR_ZZZ",
	}
	t.Setenv("DEFINITELY_UNSET_VAR_ZZZ", "")

	creds := probeProfileCredentials(p)
	if len(creds) != 1 {
		t.Fatalf("expected 1 credential, got %d", len(creds))
	}
	c := creds[0]
	if c.Field != "client-id" {
		t.Errorf("expected field=client-id, got %q", c.Field)
	}
	if c.Resolved {
		t.Errorf("unresolvable env ref should not be Resolved=true, got %+v", c)
	}
	if c.Error == "" {
		t.Errorf("expected error explaining missing env var, got %+v", c)
	}
}

func TestProbeProfileCredentials_ResolvesEnvRef(t *testing.T) {
	t.Setenv("DOCTOR_TEST_TOKEN", "tokendata12345")
	p := config.Profile{Token: "env:DOCTOR_TEST_TOKEN"}

	creds := probeProfileCredentials(p)
	if len(creds) != 1 {
		t.Fatalf("expected 1 credential, got %d", len(creds))
	}
	c := creds[0]
	if !c.Resolved {
		t.Errorf("expected Resolved=true, got %+v", c)
	}
	if c.Fingerprint != "toke••••" {
		t.Errorf("expected toke•••• fingerprint, got %q", c.Fingerprint)
	}
}

// TestBuildDoctorReport_FlagsEnvVarShadowingURL is the headline case the
// doctor command exists to diagnose: profile points at one host but
// JAMF_URL silently routes commands somewhere else.
func TestBuildDoctorReport_FlagsEnvVarShadowingURL(t *testing.T) {
	origProfile, origServerURL := profile, serverURL
	t.Cleanup(func() { profile, serverURL = origProfile, origServerURL })
	profile = "dev"
	serverURL = "" // exercise the env-var path, not --url

	t.Setenv("JAMF_URL", "https://override.example.jamfcloud.com")
	t.Setenv("JAMF_PROFILE", "")

	cfg := &config.Config{
		Profiles: map[string]config.Profile{
			"dev": {URL: "https://dev.example.jamfcloud.com"},
		},
	}
	report := buildDoctorReport(cfg, nil, "test")

	if report.Profile == nil {
		t.Fatalf("expected profile to populate, got nil")
	}
	if report.Profile.URL != "https://dev.example.jamfcloud.com" {
		t.Errorf("URL should still show profile value, got %q", report.Profile.URL)
	}
	if report.Profile.EffectiveURL != "https://override.example.jamfcloud.com" {
		t.Errorf("EffectiveURL should reflect JAMF_URL override, got %q", report.Profile.EffectiveURL)
	}
	if report.Profile.URLSource != "JAMF_URL env var" {
		t.Errorf("URLSource should label the env var, got %q", report.Profile.URLSource)
	}

	// The connectivity probe must hit the effective URL — that's the
	// host real commands would actually talk to.
	if report.Connectivity == nil {
		t.Fatalf("expected connectivity info, got nil")
	}
	if !strings.HasPrefix(report.Connectivity.URL, "https://override.example.jamfcloud.com") {
		t.Errorf("connectivity probe should target effective URL, got %q", report.Connectivity.URL)
	}
}

func TestBuildDoctorReport_NoEffectiveOverride_WhenURLsAlign(t *testing.T) {
	origProfile, origServerURL := profile, serverURL
	t.Cleanup(func() { profile, serverURL = origProfile, origServerURL })
	profile = "dev"
	serverURL = ""
	t.Setenv("JAMF_URL", "")
	t.Setenv("JAMF_PROFILE", "")

	cfg := &config.Config{
		Profiles: map[string]config.Profile{
			"dev": {URL: "https://dev.example.jamfcloud.com"},
		},
	}
	report := buildDoctorReport(cfg, nil, "test")
	if report.Profile.EffectiveURL != report.Profile.URL {
		t.Errorf("effective should equal profile URL when nothing overrides, got %q vs %q",
			report.Profile.EffectiveURL, report.Profile.URL)
	}
	if report.Profile.URLSource != "profile" {
		t.Errorf("URLSource should be 'profile', got %q", report.Profile.URLSource)
	}
}

// TestProbeProfileCredentials_FlagsEnvOverride covers the symmetric case
// for credentials: profile says env:FOO but JAMF_TOKEN env wins at auth.
func TestProbeProfileCredentials_FlagsEnvOverride(t *testing.T) {
	t.Setenv("JAMF_TOKEN", "actual-token-from-env")
	p := config.Profile{Token: "keychain:foo/bar"}

	creds := probeProfileCredentials(p)
	if len(creds) != 1 {
		t.Fatalf("expected 1 credential, got %d", len(creds))
	}
	if creds[0].EnvOverride != "JAMF_TOKEN" {
		t.Errorf("expected EnvOverride=JAMF_TOKEN, got %q", creds[0].EnvOverride)
	}
}

func TestBuildDoctorReport_NoConfigButEnvAuth_NotesEnvMode(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	origProfile := profile
	t.Cleanup(func() { profile = origProfile })
	profile = ""
	t.Setenv("JAMF_PROFILE", "")
	t.Setenv("JAMF_URL", "https://ci.example.jamfcloud.com")
	t.Setenv("JAMF_TOKEN", "ci-token")

	cfg := &config.Config{Profiles: map[string]config.Profile{}}
	report := buildDoctorReport(cfg, nil, "test")

	found := false
	for _, n := range report.Notes {
		if strings.Contains(n, "env-var mode") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected env-var-mode note, got notes=%v", report.Notes)
	}
}

// TestPrintDoctorHuman_RoutesThroughInjectedWriter — fix (2): --out-file
// must capture the human-format output, not just JSON/YAML.
func TestPrintDoctorHuman_RoutesThroughInjectedWriter(t *testing.T) {
	var buf bytes.Buffer
	r := doctorReport{
		Version:    "1.2.3",
		ConfigPath: "/tmp/conf.yaml",
		Profile: &profileReport{
			Name:         "dev",
			Source:       "config default-profile",
			URL:          "https://dev.example",
			EffectiveURL: "https://prod.example",
			URLSource:    "JAMF_URL env var",
			AuthMethod:   "oauth2",
		},
	}
	if err := printDoctorHuman(&buf, r); err != nil {
		t.Fatalf("printDoctorHuman: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "jamf-cli 1.2.3") {
		t.Errorf("expected version header, got: %s", out)
	}
	if !strings.Contains(out, "https://prod.example") || !strings.Contains(out, "JAMF_URL env var") {
		t.Errorf("expected effective URL line, got: %s", out)
	}
}

// TestProbeConnectivity_SetsUserAgentAndStopsAtRedirect covers fix (3).
func TestProbeConnectivity_SetsUserAgentAndStopsAtRedirect(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		http.Redirect(w, r, "/login", http.StatusFound)
	}))
	t.Cleanup(srv.Close)

	ci := probeConnectivity(srv.URL, "9.9.9")
	if ci.Error != "" {
		t.Fatalf("unexpected error: %q", ci.Error)
	}
	if ci.StatusCode != http.StatusFound {
		t.Errorf("expected 302 (no redirect follow), got %d", ci.StatusCode)
	}
	if !strings.Contains(gotUA, "jamf-cli/9.9.9") {
		t.Errorf("expected User-Agent with version, got %q", gotUA)
	}
}
