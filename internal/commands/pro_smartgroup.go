// Copyright 2026, Jamf Software LLC

package commands

import (
	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// newSmartGroupCmd is the entry point for the `pro smart-group` namespace.
// Subcommands are wired in subsequent tasks (templates, preview, apply,
// verify-templates).
func newSmartGroupCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "smart-group",
		Short: "Curated smart-group templates: list, preview, apply, verify",
		Long: `Create useful Jamf Pro smart groups from a curated library of templates.

Templates encode operationally-essential smart groups (devices not encrypted,
recovery keys invalid, OS versions behind, bootstrap tokens missing, etc.) so
admins don't have to assemble them by hand.

Templates are sourced from JSS canonical criterion-name strings. Run
'pro smart-group verify-templates' once against your tenant to confirm each
template matches as expected.`,
	}

	cmd.AddCommand(newSmartGroupTemplatesCmd(cliCtx))
	cmd.AddCommand(newSmartGroupPreviewCmd(cliCtx))
	cmd.AddCommand(newSmartGroupApplyCmd(cliCtx))
	cmd.AddCommand(newSmartGroupVerifyTemplatesCmd(cliCtx))

	return cmd
}

// Stubs so the skeleton compiles. Replaced in Tasks 12-15.

func newSmartGroupTemplatesCmd(_ *registry.CLIContext) *cobra.Command {
	return &cobra.Command{Use: "templates", Short: "List available smart-group templates (stub)"}
}

func newSmartGroupPreviewCmd(_ *registry.CLIContext) *cobra.Command {
	return &cobra.Command{Use: "preview", Short: "Preview a template (stub)"}
}

func newSmartGroupApplyCmd(_ *registry.CLIContext) *cobra.Command {
	return &cobra.Command{Use: "apply", Short: "Apply a template (stub)"}
}

func newSmartGroupVerifyTemplatesCmd(_ *registry.CLIContext) *cobra.Command {
	return &cobra.Command{Use: "verify-templates", Short: "Verify templates against the live tenant (stub)"}
}
