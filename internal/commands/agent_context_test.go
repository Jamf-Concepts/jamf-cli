// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"strings"
	"testing"
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
