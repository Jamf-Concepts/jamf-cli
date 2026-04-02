// Copyright 2026, Jamf Software LLC

package commands

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

func newProtectAuthCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Authentication utilities",
	}

	cmd.AddCommand(newProtectAuthTokenCmd(cliCtx))

	return cmd
}

func newProtectAuthTokenCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "token",
		Short: "Print a valid access token",
		Long: `Print a valid Jamf Protect access token to stdout.
The token is automatically refreshed if expired. Useful for
debugging API calls with curl or feeding into other tools.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			token, err := cliCtx.ProtectClient.AccessToken(cmd.Context())
			if err != nil {
				return fmt.Errorf("obtaining access token: %w", err)
			}
			_, err = fmt.Fprintln(os.Stdout, token.AccessToken)
			return err
		},
	}
}
