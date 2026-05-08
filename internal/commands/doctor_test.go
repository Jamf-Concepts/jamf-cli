// Copyright 2026, Jamf Software LLC

package commands

import (
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
