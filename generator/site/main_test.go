// Copyright 2026, Jamf Software LLC

package main

import (
	"encoding/json"
	"testing"
)

func TestTransformCommands(t *testing.T) {
	input := []byte(`[
		{"command":"pro computers list","description":"List computers","aliases":"comp, computers","flags":"--all, --filter, --limit","product":"pro","group":"Computer Management"},
		{"command":"protect analytics list","description":"List analytics","aliases":"","flags":"--output","product":"protect","group":"Analytics"}
	]`)

	out, err := transformCommands(input, "1.0.0", nil)
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

	out, err := transformCommands(input, "2.0.0", nil)
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

	out, err := transformCommands(input, "1.1.0", previous)
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

	out, err := transformCommands(input, "1.0.0", nil)
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

func TestParseVersion(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "strips git describe suffix",
			input: "jamf-cli v1.1.0-3-g6e19a4c\n  commit: 6e19a4c\n  built:  2026-04-04T00:27:01Z\n",
			want:  "v1.1.0",
		},
		{
			name:  "strips dirty suffix",
			input: "jamf-cli v1.2.0-52-gffc0b5a-dirty\n  commit: ffc0b5a\n  built:  2026-04-05T03:47:51Z\n",
			want:  "v1.2.0",
		},
		{
			name:  "exact tag unchanged",
			input: "jamf-cli v1.2.0\n  commit: abc1234\n",
			want:  "v1.2.0",
		},
		{
			name:  "without v prefix",
			input: "jamf-cli 1.0.0\n  commit: abc1234\n",
			want:  "1.0.0",
		},
		{
			name:  "pre-release preserved",
			input: "jamf-cli v2.0.0-beta.1\n  commit: abc1234\n",
			want:  "v2.0.0-beta.1",
		},
		{
			name:  "empty output",
			input: "",
			want:  "unknown",
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
