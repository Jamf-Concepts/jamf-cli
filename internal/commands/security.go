// Copyright 2026, Jamf Software LLC

package commands

import (
	"github.com/spf13/cobra"

	securitygen "github.com/Jamf-Concepts/jamf-cli/internal/commands/security/generated"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

func newSecurityCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "security",
		Short: "Jamf Security Cloud commands",
		Long:  "Commands for interacting with Jamf Security Cloud (Radar) — device risk, device lifecycle, and Shared Signals & Events stream configuration.",
	}

	// Hand-written: credential setup owns business logic (prompting for up
	// to three independent API credential pairs) that isn't a spec-driven
	// CRUD operation.
	cmd.AddCommand(newSecuritySetupCmd())

	// Generated: every Risk/Device Lifecycle/SSE operation maps cleanly to a
	// single HTTP call, so — per the same contract Platform commands follow —
	// none of it is hand-written.
	cmd.AddCommand(securitygen.NewRiskCmd(cliCtx))
	cmd.AddCommand(securitygen.NewDeviceLifecycleCmd(cliCtx))
	cmd.AddCommand(securitygen.NewStreamCmd(cliCtx))
	cmd.AddCommand(securitygen.NewStatusCmd(cliCtx))
	cmd.AddCommand(securitygen.NewVerificationCmd(cliCtx))
	cmd.AddCommand(securitygen.NewJwksCmd(cliCtx))
	cmd.AddCommand(securitygen.NewWellKnownCmd(cliCtx))

	applySecurityAliases(cmd)
	applySecurityGroups(cmd)

	return cmd
}
