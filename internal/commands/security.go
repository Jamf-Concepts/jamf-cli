// Copyright 2026, Jamf Software LLC

package commands

import (
	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

func newSecurityCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "security",
		Short: "Jamf Security Cloud commands",
		Long:  "Commands for interacting with Jamf Security Cloud (Radar) — device risk, device lifecycle, and Shared Signals & Events stream configuration.",
	}

	cmd.AddCommand(newSecuritySetupCmd())

	applySecurityAliases(cmd)
	applySecurityGroups(cmd)

	return cmd
}
