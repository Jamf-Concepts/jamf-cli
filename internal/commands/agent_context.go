// Copyright 2026, Jamf Software LLC

package commands

import (
	_ "embed"
	"fmt"

	"github.com/spf13/cobra"
)

// agentContextGuide is the embedded operating guide printed by `agent-context`.
// It duplicates facts that live in code (exit codes, global flags, the
// destructive-command rule). When you add or rename an agent-relevant global
// flag, an exit code, or a notable command — and especially when the local
// data layer lands — update agent_context.md to match. The TestAgentContextGuide*
// tests guard the documented set against renames/removals, but they cannot force
// a newly-added construct to be documented; that is on the contributor (and the
// PR-review documentation-currency check).
//
//go:embed agent_context.md
var agentContextGuide string

// newAgentContextCmd prints durable operating guidance for AI agents driving
// jamf-cli (auth, exit codes, output/agent flags, destructive-command rules,
// MCP). The content is a single embedded markdown document; it always prints
// markdown and ignores -o (like 'completion' always emitting a shell script).
// The live command list lives in 'commands'; per-command detail in '--help'.
func newAgentContextCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "agent-context",
		Short: "Print operating guidance for AI agents (auth, exit codes, flags, MCP)",
		Long: `Print durable operating guidance for AI agents driving jamf-cli.

Covers authentication, exit codes, output and agent flags, destructive-command
rules, and MCP usage. Output is plain markdown regardless of -o. For the live
command list run 'jamf-cli commands -o json'; for a command's flags run
'<command> --help'.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), agentContextGuide)
			return err
		},
	}
}
