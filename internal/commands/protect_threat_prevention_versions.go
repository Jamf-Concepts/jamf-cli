// Copyright 2026, Jamf Software LLC

package commands

import (
	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/protect"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

func newProtectThreatPreventionVersionsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "threat-prevention-versions",
		Short: "View Jamf Protect threat prevention versions",
	}

	cmd.AddCommand(newProtectThreatPreventionVersionsListCmd(cliCtx))

	return cmd
}

func newProtectThreatPreventionVersionsListCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List threat prevention versions",
		Long: `List the threat prevention versions published to the tenant, newest first.

A computer's signaturesVersion is one of these ids, so a computer reporting an
id below the first row has not picked up the latest threat prevention content.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			items, err := cliCtx.ProtectClient.ListThreatPreventionVersions(cmd.Context())
			if err != nil {
				return err
			}
			return protect.PrintList(cliCtx.Output, items)
		},
	}
}
