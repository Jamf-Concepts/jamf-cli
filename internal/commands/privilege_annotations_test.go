// Copyright 2026, Jamf Software LLC

package commands

import (
	"testing"

	"github.com/spf13/cobra"
)

// TestGeneratedCommandsCarryPrivilegeAnnotation is a floor check: the modern
// generator must emit jamf:privileges from x-required-privileges (769
// occurrences across 146 specs), so a large number of commands carry it. A
// drop below the floor means the template emission regressed.
func TestGeneratedCommandsCarryPrivilegeAnnotation(t *testing.T) {
	root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")

	const minPrivilegedCommands = 100
	count := 0
	walkCommands(root, func(cmd *cobra.Command) {
		if cmd.Annotations["jamf:privileges"] != "" {
			count++
		}
	})
	if count < minPrivilegedCommands {
		t.Errorf("only %d commands carry jamf:privileges (expected >= %d) — the generator template emission likely regressed", count, minPrivilegedCommands)
	}
}
