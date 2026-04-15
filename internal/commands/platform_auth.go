// Copyright 2026, Jamf Software LLC

package commands

import (
	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

func newPlatformAuthCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Authentication utilities",
	}

	cmd.AddCommand(newPlatformAuthTokenCmd(cliCtx))

	return cmd
}

func newPlatformAuthTokenCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var refresh bool
	cmd := &cobra.Command{
		Use:   "token",
		Short: "Print a valid platform gateway access token",
		Long: `Print a valid Jamf Platform Gateway access token.
The token is automatically refreshed if expired.

Output is JSON with token and expires_at fields. For token auth
(pre-existing bearer token), expires_at is omitted since no expiry
information is available. Use --field token to extract just the token string.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAuthToken(cmd, cliCtx, refresh)
		},
	}
	cmd.Flags().BoolVar(&refresh, "refresh", false, "Force a new token exchange, ignoring any cached token")
	return cmd
}
