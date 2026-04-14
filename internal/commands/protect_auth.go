// Copyright 2026, Jamf Software LLC

package commands

import (
	"encoding/json"
	"fmt"
	"time"

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
	var refresh bool
	cmd := &cobra.Command{
		Use:   "token",
		Short: "Print a valid access token",
		Long: `Print a valid Jamf Protect access token.
The token is automatically refreshed if expired.

Output is JSON with token and expires_at fields. Use --field token to
extract just the token string.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if refresh && cliCtx.ClearProtectToken != nil {
				cliCtx.ClearProtectToken()
			}
			t, err := cliCtx.ProtectClient.AccessToken(cmd.Context())
			if err != nil {
				return fmt.Errorf("obtaining access token: %w", err)
			}
			out, err := json.MarshalIndent(map[string]any{
				"token":      t.AccessToken,
				"expires_at": t.Expiry.UTC().Format(time.RFC3339),
			}, "", "  ")
			if err != nil {
				return err
			}
			return cliCtx.Output.PrintRaw(out)
		},
	}
	cmd.Flags().BoolVar(&refresh, "refresh", false, "Force a new token exchange, ignoring any cached token")
	return cmd
}
