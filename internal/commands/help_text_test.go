// Copyright 2026, Jamf Software LLC

package commands

import (
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestLongHelpDoesNotEnumerateSubcommands guards the fix for issue #327.
//
// A parent command that hand-writes its child list into `Long` makes the help
// output print two lists: cobra's auto-generated "Available Commands:" section
// and the hand-maintained copy. The copy has no test or CI reader, so it rots
// — `pro report` shipped a 10-entry block against 15 real subcommands, five of
// whose descriptions no longer matched the child's `Short`.
//
// The parent's `Long` is for prose only; cobra owns the inventory. This test
// flags any indented line in a `Long` string whose first token is one of that
// command's own subcommand names, which is the shape such a block always takes.
func TestLongHelpDoesNotEnumerateSubcommands(t *testing.T) {
	root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")

	walkCommands(root, func(cmd *cobra.Command) {
		if cmd.Long == "" {
			return
		}
		children := cmd.Commands()
		if len(children) == 0 {
			return
		}

		names := make(map[string]bool, len(children))
		for _, child := range children {
			names[child.Name()] = true
		}

		var enumerated []string
		for _, line := range strings.Split(cmd.Long, "\n") {
			if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
				continue // prose, not a list entry
			}
			fields := strings.Fields(line)
			if len(fields) == 0 {
				continue
			}
			if names[fields[0]] {
				enumerated = append(enumerated, fields[0])
			}
		}
		if len(enumerated) == 0 {
			return
		}

		sort.Strings(enumerated)
		t.Errorf("%q enumerates its own subcommands in Long (%s) — remove the list; "+
			"cobra renders the complete, current one under \"Available Commands:\" "+
			"from the AddCommand calls (see issue #327)",
			cmd.CommandPath(), strings.Join(enumerated, ", "))
	})
}
