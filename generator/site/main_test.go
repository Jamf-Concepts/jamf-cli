// Copyright 2026, Jamf Software LLC

package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
)

func TestTransformCommands(t *testing.T) {
	input := []byte(`[
		{"command":"pro computers list","description":"List computers","aliases":"comp, computers","flags":"--all, --filter, --limit","product":"pro","group":"Computer Management"},
		{"command":"protect analytics list","description":"List analytics","aliases":"","flags":"--output","product":"protect","group":"Analytics"}
	]`)

	out, err := transformCommands(input, "1.0.0", nil, nil, "")
	if err != nil {
		t.Fatalf("transformCommands returned error: %v", err)
	}

	var data siteData
	if err := json.Unmarshal(out, &data); err != nil {
		t.Fatalf("failed to unmarshal output: %v", err)
	}

	if data.Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", data.Version, "1.0.0")
	}
	if data.CommandCount != 2 {
		t.Errorf("CommandCount = %d, want 2", data.CommandCount)
	}
	if len(data.Commands) != 2 {
		t.Fatalf("len(Commands) = %d, want 2", len(data.Commands))
	}

	first := data.Commands[0]
	if first.Command != "pro computers list" {
		t.Errorf("Command = %q, want %q", first.Command, "pro computers list")
	}
	if first.Product != "pro" {
		t.Errorf("Product = %q, want %q", first.Product, "pro")
	}
	if first.Group != "Computer Management" {
		t.Errorf("Group = %q, want %q", first.Group, "Computer Management")
	}
	if len(first.Aliases) != 2 || first.Aliases[0] != "comp" || first.Aliases[1] != "computers" {
		t.Errorf("Aliases = %v, want [comp computers]", first.Aliases)
	}
	if len(first.Flags) != 3 || first.Flags[0] != "--all" || first.Flags[1] != "--filter" || first.Flags[2] != "--limit" {
		t.Errorf("Flags = %v, want [--all --filter --limit]", first.Flags)
	}
}

func TestTransformCommands_EmptyAliasesAndFlags(t *testing.T) {
	input := []byte(`[
		{"command":"version","description":"Print version","aliases":"","flags":"","product":"","group":""}
	]`)

	out, err := transformCommands(input, "2.0.0", nil, nil, "")
	if err != nil {
		t.Fatalf("transformCommands returned error: %v", err)
	}

	var data siteData
	if err := json.Unmarshal(out, &data); err != nil {
		t.Fatalf("failed to unmarshal output: %v", err)
	}

	if len(data.Commands) != 1 {
		t.Fatalf("len(Commands) = %d, want 1", len(data.Commands))
	}

	cmd := data.Commands[0]
	if cmd.Aliases != nil {
		t.Errorf("Aliases = %v, want nil", cmd.Aliases)
	}
	if cmd.Flags != nil {
		t.Errorf("Flags = %v, want nil", cmd.Flags)
	}
}

func TestTransformCommands_NewCommands(t *testing.T) {
	input := []byte(`[
		{"command":"pro computers list","description":"List","aliases":"","flags":"","product":"pro","group":""},
		{"command":"pro report security","description":"Security","aliases":"","flags":"","product":"pro","group":""},
		{"command":"pro report policy-status","description":"Policy","aliases":"","flags":"","product":"pro","group":""}
	]`)

	previous := map[string]bool{
		"pro computers list":  true,
		"pro report security": true,
	}

	out, err := transformCommands(input, "1.1.0", previous, nil, "")
	if err != nil {
		t.Fatalf("transformCommands returned error: %v", err)
	}

	var data siteData
	if err := json.Unmarshal(out, &data); err != nil {
		t.Fatalf("failed to unmarshal output: %v", err)
	}

	if len(data.NewCommands) != 1 {
		t.Fatalf("len(NewCommands) = %d, want 1", len(data.NewCommands))
	}
	if data.NewCommands[0] != "pro report policy-status" {
		t.Errorf("NewCommands[0] = %q, want %q", data.NewCommands[0], "pro report policy-status")
	}
}

func TestTransformCommands_NoPrevious(t *testing.T) {
	input := []byte(`[
		{"command":"version","description":"Version","aliases":"","flags":"","product":"","group":""}
	]`)

	out, err := transformCommands(input, "1.0.0", nil, nil, "")
	if err != nil {
		t.Fatalf("transformCommands returned error: %v", err)
	}

	var data siteData
	if err := json.Unmarshal(out, &data); err != nil {
		t.Fatalf("failed to unmarshal output: %v", err)
	}

	if data.NewCommands != nil {
		t.Errorf("NewCommands should be nil when no previous, got %v", data.NewCommands)
	}
}

func TestTransformCommands_CarryForwardNewCommands(t *testing.T) {
	input := []byte(`[
		{"command":"pro computers list","description":"List","aliases":"","flags":"","product":"pro","group":""},
		{"command":"pro report policy-status","description":"Policy","aliases":"","flags":"","product":"pro","group":""}
	]`)

	// Both commands exist in the previous deploy — diff finds nothing new.
	// But the previous deploy had "pro report policy-status" marked as new.
	previous := map[string]bool{
		"pro computers list":       true,
		"pro report policy-status": true,
	}
	prevNew := []string{"pro report policy-status"}

	out, err := transformCommands(input, "1.1.0", previous, prevNew, "1.1.0")
	if err != nil {
		t.Fatalf("transformCommands returned error: %v", err)
	}

	var data siteData
	if err := json.Unmarshal(out, &data); err != nil {
		t.Fatalf("failed to unmarshal output: %v", err)
	}

	if len(data.NewCommands) != 1 {
		t.Fatalf("len(NewCommands) = %d, want 1 (should carry forward)", len(data.NewCommands))
	}
	if data.NewCommands[0] != "pro report policy-status" {
		t.Errorf("NewCommands[0] = %q, want %q", data.NewCommands[0], "pro report policy-status")
	}
}

func TestTransformCommands_CarryForwardFiltersRemovedCommands(t *testing.T) {
	input := []byte(`[
		{"command":"pro computers list","description":"List","aliases":"","flags":"","product":"pro","group":""}
	]`)

	previous := map[string]bool{
		"pro computers list": true,
	}
	// Previous deploy had a command marked new that no longer exists
	prevNew := []string{"pro removed-command"}

	out, err := transformCommands(input, "1.2.0", previous, prevNew, "1.2.0")
	if err != nil {
		t.Fatalf("transformCommands returned error: %v", err)
	}

	var data siteData
	if err := json.Unmarshal(out, &data); err != nil {
		t.Fatalf("failed to unmarshal output: %v", err)
	}

	if len(data.NewCommands) != 0 {
		t.Errorf("NewCommands = %v, want empty (removed command should not carry forward)", data.NewCommands)
	}
}

func TestTransformCommands_FreshNewCommandsOverridePrevious(t *testing.T) {
	input := []byte(`[
		{"command":"pro computers list","description":"List","aliases":"","flags":"","product":"pro","group":""},
		{"command":"pro report policy-status","description":"Policy","aliases":"","flags":"","product":"pro","group":""},
		{"command":"pro brand new","description":"Brand new","aliases":"","flags":"","product":"pro","group":""}
	]`)

	// "pro brand new" is genuinely new (not in previous commands)
	previous := map[string]bool{
		"pro computers list":       true,
		"pro report policy-status": true,
	}
	// Previous deploy had policy-status as new
	prevNew := []string{"pro report policy-status"}

	out, err := transformCommands(input, "1.2.0", previous, prevNew, "1.1.0")
	if err != nil {
		t.Fatalf("transformCommands returned error: %v", err)
	}

	var data siteData
	if err := json.Unmarshal(out, &data); err != nil {
		t.Fatalf("failed to unmarshal output: %v", err)
	}

	// Should use the fresh diff result, not the carry-forward
	if len(data.NewCommands) != 1 {
		t.Fatalf("len(NewCommands) = %d, want 1", len(data.NewCommands))
	}
	if data.NewCommands[0] != "pro brand new" {
		t.Errorf("NewCommands[0] = %q, want %q", data.NewCommands[0], "pro brand new")
	}
}

func TestTransformCommands_CarryForwardClearsOnNewVersion(t *testing.T) {
	input := []byte(`[
		{"command":"pro computers list","description":"List","aliases":"","flags":"","product":"pro","group":""},
		{"command":"pro report policy-status","description":"Policy","aliases":"","flags":"","product":"pro","group":""}
	]`)

	previous := map[string]bool{
		"pro computers list":       true,
		"pro report policy-status": true,
	}
	prevNew := []string{"pro report policy-status"}

	// Version changed from 1.1.0 to 1.2.0 — badges should NOT carry forward
	out, err := transformCommands(input, "1.2.0", previous, prevNew, "1.1.0")
	if err != nil {
		t.Fatalf("transformCommands returned error: %v", err)
	}

	var data siteData
	if err := json.Unmarshal(out, &data); err != nil {
		t.Fatalf("failed to unmarshal output: %v", err)
	}

	if len(data.NewCommands) != 0 {
		t.Errorf("NewCommands = %v, want empty (badges should clear on version change)", data.NewCommands)
	}
}

func TestParseVersion(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "strips git describe suffix",
			input: "jamf-cli v1.1.0-3-g6e19a4c\n  commit: 6e19a4c\n  built:  2026-04-04T00:27:01Z\n",
			want:  "1.1.0",
		},
		{
			name:  "strips dirty suffix",
			input: "jamf-cli v1.2.0-52-gffc0b5a-dirty\n  commit: ffc0b5a\n  built:  2026-04-05T03:47:51Z\n",
			want:  "1.2.0",
		},
		{
			name:  "exact tag unchanged",
			input: "jamf-cli v1.2.0\n  commit: abc1234\n",
			want:  "1.2.0",
		},
		{
			name:  "without v prefix",
			input: "jamf-cli 1.0.0\n  commit: abc1234\n",
			want:  "1.0.0",
		},
		{
			name:  "pre-release preserved",
			input: "jamf-cli v2.0.0-beta.1\n  commit: abc1234\n",
			want:  "2.0.0-beta.1",
		},
		{
			name: "json release (default -o json output)",
			input: `{
  "version": "1.18.0",
  "commit": "c71ad8a",
  "built": "2026-05-31T05:22:01Z",
  "specProVersion": "unknown"
}`,
			want: "1.18.0",
		},
		{
			name:  "json dirty dev build strips git-describe suffix",
			input: `{"version":"v1.17.0-25-g74846ff-dirty","commit":"74846ff"}`,
			want:  "1.17.0",
		},
		{
			name:  "json pre-release preserved",
			input: `{"version":"v2.0.0-beta.1"}`,
			want:  "2.0.0-beta.1",
		},
		{
			name:  "empty output",
			input: "",
			want:  "unknown",
		},
		{
			name:  "json default output strips git-describe suffix",
			input: "{\n  \"version\": \"v1.17.0-25-g74846ff\",\n  \"commit\": \"74846ff\",\n  \"built\": \"2026-05-31T04:31:46Z\"\n}\n",
			want:  "1.17.0",
		},
		{
			name:  "json exact tag",
			input: `{"version":"v1.2.0","commit":"abc1234"}`,
			want:  "1.2.0",
		},
		{
			name:  "json empty version falls back to unknown",
			input: `{"version":"","commit":"abc1234"}`,
			want:  "unknown",
		},
		{
			name:  "json exact tag dirty build strips dirty marker",
			input: `{"version":"v1.17.0-dirty","commit":"abc1234"}`,
			want:  "1.17.0",
		},
		{
			name:  "json describe with count and dirty marker",
			input: `{"version":"v1.17.0-25-g74846ff-dirty","commit":"74846ff"}`,
			want:  "1.17.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseVersion(tt.input)
			if got != tt.want {
				t.Errorf("parseVersion(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestRenderLLMSTxt_AgentContract guards the "For AI Agents" section. The
// exit-code table is the load-bearing part: it must stay in lockstep with
// internal/exitcode. We interpolate each constant's value here, so renaming
// or renumbering a code in exitcode.go without updating llms.txt fails CI.
func TestRenderLLMSTxt_AgentContract(t *testing.T) {
	out := renderLLMSTxt(siteData{Version: "1.0.0", CommandCount: 1200})

	if !strings.Contains(out, "## For AI Agents") {
		t.Fatalf("llms.txt is missing the \"For AI Agents\" section")
	}

	exitCodes := []struct {
		code  int
		label string
	}{
		{exitcode.Success, "success"},
		{exitcode.General, "general"},
		{exitcode.Usage, "usage"},
		{exitcode.Authentication, "auth"},
		{exitcode.NotFound, "not-found"},
		{exitcode.PermissionDenied, "permission"},
		{exitcode.RateLimited, "rate-limited"},
	}
	for _, ec := range exitCodes {
		want := fmt.Sprintf("%d %s", ec.code, ec.label)
		if !strings.Contains(out, want) {
			t.Errorf("exit-code table out of sync with internal/exitcode: missing %q", want)
		}
	}

	// Spot-check the rest of the contract so a silent deletion is caught.
	for _, want := range []string{
		"jamf-cli commands -o json",
		"--no-input",
		"--select",
		"--confirm-destructive",
		"apply",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("agent contract missing %q", want)
		}
	}
}
