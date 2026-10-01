// Copyright 2026, Jamf Software LLC

package commands

import (
	"strings"
	"testing"
)

// verboseBodyForms are the spellings of a --verbose count of 3 or more, which
// logs response bodies to stderr before any MCP redaction sees them.
var verboseBodyForms = [][]string{
	{"-vvv", "pro", "classic-macos-config-profiles", "get", "1"},
	{"-v", "-v", "-v", "pro", "classic-macos-config-profiles", "get", "1"},
	{"--verbose=3", "pro", "classic-macos-config-profiles", "get", "1"},
	{"--verbose=4", "pro", "classic-macos-config-profiles", "get", "1"},
	{"-vv", "-v", "pro", "classic-macos-config-profiles", "get", "1"},
	{"pro", "classic-macos-config-profiles", "get", "1", "-vvv"},
	{"pro", "blueprints", "components", "configuration-profile", "--id", "1", "-v", "-vv"},
}

var verboseHeaderForms = [][]string{
	{"-v", "pro", "computers", "list"},
	{"-vv", "pro", "computers", "list"},
	{"pro", "computers", "list", "--verbose=2"},
	{"-vvv", "--verbose=2", "pro", "computers", "list"},
}

func TestMCP_RefusesVerboseBodyLogging(t *testing.T) {
	for _, args := range verboseBodyForms {
		_, err := buildChildArgs("prod", args)
		if !isMCPRefusal(err) {
			t.Errorf("run_command accepts %q (err %v); -vvv logs response bodies to stderr, which the model reads", args, err)
			continue
		}
		if !strings.Contains(err.Error(), "use -vv or less") {
			t.Errorf("refusal of %q should say to use -vv or less: %v", args, err)
		}
	}
	for _, args := range verboseHeaderForms {
		if _, err := buildChildArgs("prod", args); isMCPRefusal(err) {
			t.Errorf("run_command refuses %q: %v; -vv and less log no body", args, err)
		}
	}
}

func TestMCPChild_RefusesVerboseBodyLogging(t *testing.T) {
	for _, args := range verboseBodyForms {
		argv := append([]string{"--profile", "prod", "--no-input"}, args...)
		if err := executeAsMCPChild(t, "prod", argv...); !isMCPRefusal(err) {
			t.Errorf("MCP child ran %q (err %v); it must refuse body logging on its own parse", argv, err)
		}
	}
	for _, args := range verboseHeaderForms {
		argv := append([]string{"--profile", "prod", "--no-input"}, args...)
		if err := executeAsMCPChild(t, "prod", argv...); isMCPRefusal(err) {
			t.Errorf("MCP child refuses %q: %v; -vv and less log no body", argv, err)
		}
	}
}
