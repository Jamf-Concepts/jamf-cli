// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
)

func TestAgentContextCommand(t *testing.T) {
	root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"agent-context"})

	if err := root.Execute(); err != nil {
		t.Fatalf("agent-context failed (should run without auth): %v", err)
	}

	out := buf.String()
	for _, anchor := range []string{"# jamf-cli agent operating guide", "## Exit codes", "## Destructive commands"} {
		if !strings.Contains(out, anchor) {
			t.Errorf("agent-context output missing %q", anchor)
		}
	}
}

// TestAgentContextGuideCoversExitCodes guards against the embedded guide's
// exit-code table drifting out of sync with internal/exitcode. The guide
// duplicates facts that live in code; this fails if an exit code is added or
// renamed in the exitcode package without updating the guide.
func TestAgentContextGuideCoversExitCodes(t *testing.T) {
	for _, code := range []int{
		exitcode.Success,
		exitcode.General,
		exitcode.Usage,
		exitcode.Authentication,
		exitcode.NotFound,
		exitcode.PermissionDenied,
		exitcode.RateLimited,
		exitcode.PartialFailure,
	} {
		name := exitcode.CodeName(code)
		if !strings.Contains(agentContextGuide, name) {
			t.Errorf("agent-context guide is missing exit-code name %q (code %d) — keep the guide's exit-code table in sync with internal/exitcode", name, code)
		}
	}
}

// TestAgentContextGuideFlagsExist guards against the guide documenting a global
// flag that has since been renamed or removed. It pins the documented set to
// real root persistent flags; --yes is intentionally excluded because it is a
// per-command destructive gate, not a global flag.
func TestAgentContextGuideFlagsExist(t *testing.T) {
	root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")
	for _, name := range []string{
		"output", "compact", "select", "field",
		"quiet", "no-hints", "no-input", "dry-run", "out-file", "profile",
	} {
		if root.PersistentFlags().Lookup(name) == nil {
			t.Errorf("agent-context guide documents --%s, but it is no longer a global persistent flag — update the guide or the flag", name)
		}
	}
}
