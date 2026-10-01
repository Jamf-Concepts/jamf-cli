// Copyright 2026, Jamf Software LLC

package commands

import (
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamfprotect-go-sdk/jamfprotect"
)

func TestMCPServe_RefusesAnEmptyInputDir(t *testing.T) {
	resetGlobals()
	t.Cleanup(resetGlobals)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("JAMF_CLI_ARGS", "")
	t.Setenv(mcpChildEnvVar, "")
	for _, k := range []string{"JAMF_PROFILE", "JAMF_URL", "JAMF_TOKEN", "JAMF_CLIENT_ID", "JAMF_CLIENT_SECRET"} {
		t.Setenv(k, "")
	}
	for _, args := range [][]string{
		{"mcp", "serve", "--input-dir", ""},
		{"mcp", "serve", "--input-dir="},
	} {
		root := NewRootCmd("test", "t", "t", "t")
		root.SetArgs(args)
		root.SetOut(&strings.Builder{})
		root.SetErr(&strings.Builder{})
		err := root.Execute()
		if err == nil || !strings.Contains(err.Error(), "--input-dir") || !strings.Contains(err.Error(), "empty") {
			t.Errorf("%q = %v; an empty --input-dir (an unset $DIR) must be refused by name, not start a server that allows no reads", args, err)
		}
	}
}

func TestBuildChildArgs_LeafOutputRefusalPointsAtTheFormatFlag(t *testing.T) {
	_, err := buildChildArgs("prod", []string{"protect", "plans", "config-profile", "Default", "-O", modelChosenPath})
	if !isMCPRefusal(err) {
		t.Fatalf("a leaf's own --output must be refused: %v", err)
	}
	if !strings.Contains(err.Error(), "-o <format>") {
		t.Errorf("the refusal should say the global -o <format> flag is still accepted: %v", err)
	}
	_, err = buildChildArgs("prod", []string{"pro", "packages", "export", "-O", modelChosenPath})
	if err == nil || strings.Contains(err.Error(), "-o <format>") {
		t.Errorf("--save-to has no format alternative, so its refusal must not offer one: %v", err)
	}
}

func TestBuildChildArgs_RelativePathOutsideTheInputDirAsksForAnAbsoluteOne(t *testing.T) {
	fx := newInputDirFixture(t)
	t.Chdir(fx.adjacentDir)
	_, err := buildChildArgs("prod", []string{"pro", "classic-policies", "create", "--from-file", "../body.xml"})
	if !isMCPRefusal(err) {
		t.Fatalf("a relative path resolving outside the input directory must be refused: %v", err)
	}
	if !strings.Contains(err.Error(), "absolute path") || !strings.Contains(err.Error(), "started in") {
		t.Errorf("the refusal should say a relative path resolves against the server's start directory and ask for an absolute path: %v", err)
	}
	_, err = buildChildArgs("prod", []string{"pro", "classic-policies", "create", "--from-file", fx.outside})
	if err == nil || strings.Contains(err.Error(), "absolute path") {
		t.Errorf("an absolute path outside the input directory needs no hint about relative paths: %v", err)
	}
}

func TestPlanConfigProfileFileName_OverMCPNamesNoRefusedFlag(t *testing.T) {
	t.Setenv(mcpChildEnvVar, "1")
	_, err := planConfigProfileFileName("a/b")
	if err == nil {
		t.Fatal("a plan name that is not one path segment must still be refused")
	}
	if strings.Contains(err.Error(), "-O") {
		t.Errorf("over MCP the refusal must not suggest -O, which run_command refuses: %v", err)
	}
	if !strings.Contains(err.Error(), "run_command") {
		t.Errorf("over MCP the refusal should say the plan cannot be saved through run_command: %v", err)
	}
}

func TestRedactReportClientHeaders_MasksURLUserinfoAndQuery(t *testing.T) {
	for _, tc := range []struct{ in, want, secret string }{
		{"https://user:hunter2@hec.example.invalid:8088/services/collector?token=abc123", "https://<redacted>@hec.example.invalid:8088/services/collector?<redacted>", "hunter2"},
		{"https://teams.example.invalid/webhook?sig=abc123&x=1", "https://teams.example.invalid/webhook?<redacted>", "abc123"},
		{"https://siem.example.invalid/ingest", "https://siem.example.invalid/ingest", ""},
		{"", "", ""},
	} {
		a := jamfprotect.ActionConfig{Clients: []jamfprotect.ReportClient{{Params: jamfprotect.ReportClientParams{URL: tc.in}}}}
		got := redactReportClientHeaders(a).Clients[0].Params.URL
		if got != tc.want {
			t.Errorf("redacted URL %q = %q, want %q", tc.in, got, tc.want)
		}
		if tc.secret != "" && strings.Contains(got, tc.secret) {
			t.Errorf("redacted URL %q still carries %q", got, tc.secret)
		}
		if a.Clients[0].Params.URL != tc.in {
			t.Errorf("redaction wrote through to the caller's config: %q", a.Clients[0].Params.URL)
		}
	}
}
