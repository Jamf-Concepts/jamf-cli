// Copyright 2026, Jamf Software LLC

package commands

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/auth"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

func newProAuthCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Authentication utilities",
	}

	cmd.AddCommand(newProAuthTokenCmd(cliCtx))

	return cmd
}

func newProAuthTokenCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var refresh bool
	cmd := &cobra.Command{
		Use:   "token",
		Short: "Print a valid access token",
		Long: `Print a valid Jamf Pro access token.
The token is automatically refreshed if expired.

Output is JSON with token and expires_at fields. For token auth
(pre-existing bearer token), expires_at is omitted since no expiry
information is available. Use --field token to extract just the token string.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p := cliCtx.AuthProvider
			var tok string
			var err error
			if refresh {
				if r, ok := p.(auth.Refresher); ok {
					tok, err = r.Refresh(cmd.Context())
				} else {
					tok, err = p.GetToken(cmd.Context())
				}
			} else {
				tok, err = p.GetToken(cmd.Context())
			}
			if err != nil {
				return fmt.Errorf("obtaining access token: %w", err)
			}

			m := map[string]any{"token": tok}

			// Expiry is only available for OAuth2-based providers.
			switch ap := p.(type) {
			case *auth.OAuth2Provider:
				if exp := ap.ExpiresAt(); !exp.IsZero() {
					m["expires_at"] = exp.UTC().Format(time.RFC3339)
				}
			case *auth.PlatformOAuth2Provider:
				if exp := ap.ExpiresAt(); !exp.IsZero() {
					m["expires_at"] = exp.UTC().Format(time.RFC3339)
				}
			}

			out, err := json.MarshalIndent(m, "", "  ")
			if err != nil {
				return err
			}
			return cliCtx.Output.PrintRaw(out)
		},
	}
	cmd.Flags().BoolVar(&refresh, "refresh", false, "Force a new token exchange, ignoring any cached token")
	return cmd
}
